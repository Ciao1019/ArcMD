package agentmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// ToolCaller executes an ArcMD control action and returns a JSON-serializable
// result plus whether the action ran against a live ArcMD GUI window.
type ToolCaller func(ctx context.Context, action string, params json.RawMessage) (result any, live bool, err error)

// Tool describes an MCP tool exposed by ArcMD.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// toolAction maps MCP tool names (snake_case, valid across all LLM APIs) to
// internal control action names.
var toolAction = map[string]string{
	"arcmd_status":       "status",
	"arcmd_format_guide": "format_guide",
	"space_list":         "space.list",
	"space_create":       "space.create",
	"space_update":       "space.update",
	"space_switch":       "space.switch",
	"space_remove":       "space.remove",
	"doc_list":           "doc.list",
	"doc_search":         "doc.search",
	"doc_read":           "doc.read",
	"doc_create":         "doc.create",
	"doc_edit":           "doc.edit",
	"doc_organize":       "doc.organize",
	"doc_delete":         "doc.delete",
	"whiteboard_write":   "whiteboard.write",
	"database_schema":    "database.schema",
	"database_records":   "database.records",
}

// Tools returns the full list of MCP tools exposed by ArcMD.
func Tools() []Tool {
	return []Tool{
		{
			Name:        "arcmd_status",
			Description: "Get ArcMD's current status: whether the desktop window is live, the active Space, open documents, and configuration paths.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "arcmd_format_guide",
			Description: "Read ArcMD's rich format specification before authoring complex documents (YAML frontmatter, highlights, 12 hand-drawn annotations, 5-color callouts, toggles, multi-column layouts, rich HTML tables, Excalidraw whiteboards, and .db.md databases).",
			InputSchema: objSchema(map[string]any{
				"topic": map[string]any{
					"type":        "string",
					"enum":        FormatTopics,
					"description": "Section to read: all (default), frontmatter, inline, blocks, tables, whiteboard, or database.",
				},
			}),
		},
		{
			Name:        "space_list",
			Description: "List all Spaces in ArcMD with their ID, name, icon/symbol, theme, folder path, attachments folder, pinned items, and global favorites.",
			InputSchema: objSchema(nil),
		},
		{
			Name:        "space_create",
			Description: "Create a new Space in ArcMD with optional folder path, emoji icon or Lucide symbol, theme colors, appearance (auto/light/dark), and switch to it.",
			InputSchema: objSchema(map[string]any{
				"name":       strProp("Display name of the new Space."),
				"root":       strProp("Optional folder path on disk (defaults to ~/Documents/ArcMD/<name>)."),
				"icon":       strProp("Optional emoji glyph, e.g. '🚀'."),
				"symbol":     strProp("Optional Lucide symbol name (mutually exclusive with icon)."),
				"colors":     arrStrProp("Optional 1-3 hex colors (#RRGGBB) for the Space theme."),
				"appearance": enumProp("Optional theme appearance.", "auto", "light", "dark"),
				"intensity":  numProp("Optional color saturation intensity (0..1, default 0.8)."),
				"starter":    boolProp("Write a starter guide document when the folder is empty."),
				"switch":     boolProp("Switch ArcMD's active Space to the newly created Space (default true)."),
			}, "name"),
		},
		{
			Name:        "space_update",
			Description: "Update an existing Space's name, emoji icon or Lucide symbol, theme colors/appearance/intensity/noise, or attachments folder.",
			InputSchema: objSchema(map[string]any{
				"space":          strProp("Space ID or name (defaults to the active Space)."),
				"name":           strProp("New name for the Space."),
				"icon":           strProp("New emoji icon (pass '-' to clear icon)."),
				"symbol":         strProp("New Lucide symbol name."),
				"colors":         arrStrProp("1-3 hex colors (#RRGGBB) for the Space theme."),
				"appearance":     enumProp("Theme appearance.", "auto", "light", "dark"),
				"intensity":      numProp("Theme intensity (0..1)."),
				"noise":          numProp("Theme grain/noise (0..1)."),
				"attachmentsDir": strProp("Space-relative folder for new attachments ('' for per-doc assets/, '.' for Space root)."),
				"switch":         boolProp("Also make this Space active."),
			}),
		},
		{
			Name:        "space_switch",
			Description: "Switch ArcMD's active Space by name or ID.",
			InputSchema: objSchema(map[string]any{
				"space": strProp("Target Space name or ID."),
			}, "space"),
		},
		{
			Name:        "space_remove",
			Description: "Remove a Space from ArcMD's Space list. Its physical folder and documents on disk are never deleted.",
			InputSchema: objSchema(map[string]any{
				"space": strProp("Space name or ID to remove from ArcMD."),
			}, "space"),
		},
		{
			Name:        "doc_list",
			Description: "List documents and folders inside an ArcMD Space, with document kinds (markdown, whiteboard, database, pdf, book), pins, and favorites.",
			InputSchema: objSchema(map[string]any{
				"space": strProp("Space name, ID, or folder (defaults to the active Space)."),
				"dir":   strProp("Optional subfolder path inside the Space."),
				"kind":  enumProp("Optional filter by document kind.", "", "markdown", "whiteboard", "database", "pdf", "book"),
			}),
		},
		{
			Name:        "doc_search",
			Description: "Search documents in a Space using BM25 ranked passage search, exact/regex line grep, or link/backlink inspection.",
			InputSchema: objSchema(map[string]any{
				"space":   strProp("Space name or ID (defaults to the active Space)."),
				"query":   strProp("Natural-language query (for ranked search) or text/regex pattern (when mode='grep')."),
				"mode":    enumProp("Search mode: 'search' (BM25 ranked passages, default), 'grep' (line-by-line text/regex), 'links' (wikilinks/backlinks/tags for path), or 'overview'.", "search", "grep", "links", "overview"),
				"regex":   boolProp("Treat query as regular expression when mode='grep'."),
				"scope":   strProp("Optional folder or glob filter, or document path when mode='links'."),
				"context": intProp("Context lines around grep matches (0-5)."),
				"limit":   intProp("Max results for ranked search (1-25)."),
			}),
		},
		{
			Name:        "doc_read",
			Description: "Read an ArcMD document (.md, .db.md, .excalidraw.svg, .pdf, .epub) including live unsaved edits, metadata, outline, and links.",
			InputSchema: objSchema(map[string]any{
				"space":           strProp("Space name or ID (defaults to the active Space)."),
				"path":            strProp("Document path relative to the Space root (e.g. 'Notes/Design.md')."),
				"startLine":       intProp("Optional 1-based start line."),
				"endLine":         intProp("Optional 1-based inclusive end line."),
				"withLineNumbers": boolProp("Prefix returned lines with 'L12: ' line numbers."),
				"includeOutline":  boolProp("Include heading outline."),
				"includeLinks":    boolProp("Include outgoing wikilinks, backlinks, and #tags."),
			}, "path"),
		},
		{
			Name:        "doc_create",
			Description: "Create a new rich Markdown document (.md) or Today's Note in a Space. Supports YAML frontmatter chrome (icon, cover, goal), templates, rich ArcMD Markdown (highlights, hand-drawn annotations, callouts, toggles, columns, rich tables, math, mermaid), pinning, favoriting, and opening in the live window.",
			InputSchema: objSchema(map[string]any{
				"space":       strProp("Space name or ID (defaults to the active Space)."),
				"path":        strProp("Optional exact relative file path (e.g. 'Specs/Auth.md'). If omitted, dir + title chooses an unused filename."),
				"dir":         strProp("Optional subfolder inside the Space when path is omitted."),
				"title":       strProp("Document title (used for filename and leading '# Title' when content has none)."),
				"content":     strProp("Markdown body content (supports all ArcMD rich inline & block formats)."),
				"template":    strProp("Optional template name from .arcmd/templates/."),
				"daily":       boolProp("Open or create Today's Note (Daily/YYYY-MM-DD.md) instead of a generic note."),
				"icon":        strProp("Optional page emoji icon in frontmatter (e.g. '✨')."),
				"cover":       strProp("Optional page cover ('gradient:dawn|sea|forest|dusk|sand|ink' or relative image path)."),
				"coverY":      numProp("Optional cover vertical offset (-1..1)."),
				"goal":        intProp("Optional word count goal."),
				"frontmatter": map[string]any{"type": "object", "description": "Optional custom YAML frontmatter key-value pairs."},
				"overwrite":   boolProp("Allow overwriting if path already exists (default false)."),
				"pin":         boolProp("Pin the created document in the Space."),
				"favorite":    boolProp("Add the created document to global Favorites."),
				"open":        boolProp("Open and focus the document in the ArcMD window."),
			}),
		},
		{
			Name:        "doc_edit",
			Description: "Edit an existing Markdown document (.md). When open in ArcMD, edits apply as a live undoable (⌘Z) editor transaction without conflict banners. Call arcmd_format_guide if you need exact syntax for highlights, hand-drawn marks, callouts, columns, or rich tables.",
			InputSchema: objSchema(map[string]any{
				"space":       strProp("Space name or ID (defaults to the active Space)."),
				"path":        strProp("Document path relative to the Space root."),
				"mode":        enumProp("Edit mode: 'replace' (full content), 'patch' (oldText -> newText), 'append' (add at end or end of heading section), 'prepend' (add at start or top of heading section), or 'frontmatter' (update metadata only).", "replace", "patch", "append", "prepend", "frontmatter"),
				"content":     strProp("New Markdown content for replace, append, or prepend modes."),
				"oldText":     strProp("Exact existing substring to replace when mode='patch'."),
				"newText":     strProp("Replacement substring when mode='patch'."),
				"replaceAll":  boolProp("Replace every occurrence of oldText when mode='patch'."),
				"heading":     strProp("Optional section heading (e.g. '## 进展' or '进展') under which append/prepend inserts content."),
				"icon":        strProp("Optional page emoji icon update ('-' clears it)."),
				"cover":       strProp("Optional page cover update ('-' clears it)."),
				"coverY":      numProp("Optional page coverY update (-1..1)."),
				"goal":        intProp("Optional word goal update (-1 clears it)."),
				"frontmatter": map[string]any{"type": "object", "description": "Optional YAML frontmatter keys to set/merge (null value deletes a key)."},
				"open":        boolProp("Open and focus the edited document in ArcMD."),
			}, "path", "mode"),
		},
		{
			Name:        "doc_organize",
			Description: "Create folders, rename or move documents/folders (automatically rewriting [[wikilinks]], image attachments, and history), pin/unpin, favorite/unfavorite, or open a document in the ArcMD window.",
			InputSchema: objSchema(map[string]any{
				"space":   strProp("Source Space name or ID (defaults to the active Space)."),
				"op":      enumProp("Operation to perform.", "open", "mkdir", "rename", "move", "pin", "unpin", "favorite", "unfavorite"),
				"path":    strProp("Relative path of the document or folder."),
				"newName": strProp("New name when op='rename'."),
				"toSpace": strProp("Optional destination Space name or ID when op='move'."),
				"toDir":   strProp("Destination folder path inside destination Space when op='move' ('' for root)."),
			}, "op", "path"),
		},
		{
			Name:        "doc_delete",
			Description: "Safely move a document or folder to the system Trash (with a 30-day restorable backup in ArcMD's Recently Deleted bin), list Recently Deleted items, or restore a deleted item. Never permanently deletes files.",
			InputSchema: objSchema(map[string]any{
				"space":   strProp("Space name or ID (defaults to the active Space)."),
				"op":      enumProp("Trash operation: 'trash' (default), 'list_trash', or 'restore'.", "trash", "list_trash", "restore"),
				"path":    strProp("Document or folder relative path when op='trash' (or fallback match when op='restore')."),
				"trashID": strProp("Trash item ID from list_trash when op='restore'."),
			}),
		},
		{
			Name:        "whiteboard_write",
			Description: "Create or update an amphibious hand-drawn Excalidraw whiteboard (.excalidraw.svg) from a Mermaid diagram or declarative shape/arrow JSON elements, and optionally embed it inline in a Markdown document.",
			InputSchema: objSchema(map[string]any{
				"space":        strProp("Space name or ID (defaults to the active Space)."),
				"path":         strProp("Target whiteboard path (e.g. 'Diagrams/Arch.excalidraw.svg' or 'assets/arch.excalidraw.svg'). Automatically appends .excalidraw.svg if needed."),
				"mermaid":      strProp("Optional Mermaid flowchart/graph or sequenceDiagram to auto-layout as hand-drawn Excalidraw elements."),
				"elements":     map[string]any{"type": "array", "description": "Optional declarative elements: [{id, type: rectangle|diamond|ellipse|stadium|cylinder|hexagon|cloud|star|callout|note|frame|text|arrow|line, text, name, x, y, width, height, color: ink|gray|red|orange|green|blue|purple, fillColor, fillStyle: hachure|cross-hatch|solid|none, strokeStyle: solid|dashed|dotted, from, to, link, frameId}].", "items": map[string]any{"type": "object"}},
				"append":       boolProp("Append/merge elements into an existing whiteboard instead of replacing all elements."),
				"embedInDoc":   strProp("Optional .md document path in the same Space where ![caption](relURL) will be inserted if not already present."),
				"embedHeading": strProp("Optional heading in embedInDoc under which to insert the whiteboard embed."),
				"caption":      strProp("Caption/alt text for the embedded whiteboard (default '手绘白板')."),
				"open":         boolProp("Open the whiteboard (or embedInDoc) in the ArcMD window."),
			}),
		},
		{
			Name:        "database_schema",
			Description: "Inspect, create, or modify an amphibious multi-view database (.db.md): manage all 18 property types (status, select, multi_select, number/bar, date, person, checkbox, relation, rollup, Formula 2.0, etc.), manage all 9 views (table, board, timeline, calendar, list, gallery, chart, feed, form), or embed a Linked View into a Markdown document.",
			InputSchema: objSchema(map[string]any{
				"space":      strProp("Space name or ID (defaults to the active Space)."),
				"op":         enumProp("Schema operation.", "inspect", "create", "add_property", "update_property", "delete_property", "add_view", "update_view", "delete_view", "embed_view"),
				"path":       strProp("Database path relative to Space root (e.g. 'Tasks.db.md')."),
				"title":      strProp("Database title (for create or inspect)."),
				"icon":       strProp("Optional emoji icon for the database."),
				"cover":      strProp("Optional cover for the database."),
				"sourceType": enumProp("Record storage mode when op='create': 'folder' (backing .md pages with frontmatter sync, default), 'inline' (rows inside .db.md), or 'space'.", "folder", "inline", "space"),
				"sourceDir":  strProp("Optional folder scanned when sourceType='folder' or 'space'."),
				"properties": map[string]any{"type": "array", "description": "Initial properties array when op='create'.", "items": map[string]any{"type": "object"}},
				"property":   map[string]any{"type": "object", "description": "Property spec for add_property/update_property/delete_property: {id, key, name, type, options:[{name,color,group}], number:{format,showAs,barColor}, date:{includeTime}, formula:{expression}, uniqueID:{prefix}, relation:{targetDB}, rollup:{relationPropID,targetPropID,calculate}}."},
				"views":      map[string]any{"type": "array", "description": "Initial views array when op='create'.", "items": map[string]any{"type": "object"}},
				"view":       map[string]any{"type": "object", "description": "View spec for add_view/update_view/delete_view/embed_view: {id, name, type: table|board|timeline|calendar|list|gallery|chart|feed|form, groupBy, sorts:[{property,desc}], startProp, endProp, dateProp, chart:{kind,xProp,yMode,yProp,yAgg}}."},
				"records":    map[string]any{"type": "array", "description": "Optional initial records [{title, fields:{...}, body}] when op='create'.", "items": map[string]any{"type": "object"}},
				"overwrite":  boolProp("Allow overwriting an existing .db.md database at path when op='create' (default false)."),
				"embedInDoc": strProp("Target .md document path when op='embed_view'."),
				"height":     numProp("Optional inline height (220-760) when op='embed_view'."),
				"open":       boolProp("Open the database (or embedInDoc) in ArcMD."),
			}, "op"),
		},
		{
			Name:        "database_records",
			Description: "Query, add, update, or safely delete records in an ArcMD .db.md database. Property values in 'fields' can be keyed by property Name, Key, or ID.",
			InputSchema: objSchema(map[string]any{
				"space":  strProp("Space name or ID (defaults to the active Space)."),
				"op":     enumProp("Record operation.", "query", "add", "update", "delete"),
				"path":   strProp("Database path relative to Space root (e.g. 'Tasks.db.md')."),
				"view":   strProp("Optional view ID or name for op='query'."),
				"search": strProp("Optional search filter for op='query'."),
				"record": strProp("Record ID, backing .md path, or exact record title (for update/delete)."),
				"title":  strProp("Record title (for add, or new title for update)."),
				"fields": map[string]any{"type": "object", "description": "Map of property Name/Key/ID -> value (string, number, boolean, string array, or {start,end} date object)."},
				"body":   strProp("Optional Markdown body for page-backed record creation or update."),
			}, "op", "path"),
		},
	}
}

func objSchema(props map[string]any, required ...string) map[string]any {
	if props == nil {
		props = map[string]any{}
	}
	s := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
func numProp(desc string) map[string]any {
	return map[string]any{"type": "number", "description": desc}
}
func arrStrProp(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}
func enumProp(desc string, vals ...string) map[string]any {
	return map[string]any{"type": "string", "enum": vals, "description": desc}
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve runs the MCP JSON-RPC 2.0 server over in and out until EOF or ctx cancellation.
// It transparently supports both newline-delimited JSON-RPC (the MCP stdio standard)
// and Content-Length header framing.
func Serve(ctx context.Context, in io.Reader, out io.Writer, version string, caller ToolCaller) error {
	if version == "" {
		version = "0.3.0"
	}
	br := bufio.NewReaderSize(in, 64*1024)
	var writeMu sync.Mutex
	writeReply := func(framed bool, rep rpcReply) error {
		rep.JSONRPC = "2.0"
		raw, err := json.Marshal(rep)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		if framed {
			if _, err := fmt.Fprintf(out, "Content-Length: %d\r\n\r\n", len(raw)); err != nil {
				return err
			}
			_, err = out.Write(raw)
			return err
		}
		raw = append(raw, '\n')
		_, err = out.Write(raw)
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		payload, framed, err := readRPCFrame(br)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if len(bytes.TrimSpace(payload)) == 0 {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal(payload, &msg); err != nil {
			_ = writeReply(framed, rpcReply{
				ID:    json.RawMessage("null"),
				Error: &rpcError{Code: -32700, Message: "parse error: " + err.Error()},
			})
			continue
		}
		// Notifications have no ID and expect no response.
		if len(msg.ID) == 0 || string(bytes.TrimSpace(msg.ID)) == "" {
			continue
		}
		res, rpcErr := dispatchMCP(ctx, msg, version, caller)
		rep := rpcReply{ID: msg.ID, Result: res, Error: rpcErr}
		if err := writeReply(framed, rep); err != nil {
			return err
		}
	}
}

const maxRPCFrameBytes = 64 << 20

func readRPCFrame(br *bufio.Reader) (payload []byte, framed bool, err error) {
	for {
		b, peekErr := br.Peek(1)
		if peekErr != nil {
			return nil, false, peekErr
		}
		if b[0] == '\r' || b[0] == '\n' || b[0] == ' ' || b[0] == '\t' {
			_, _ = br.ReadByte()
			continue
		}
		if b[0] == '{' || b[0] == '[' {
			var line []byte
			for {
				frag, readErr := br.ReadSlice('\n')
				if len(line)+len(frag) > maxRPCFrameBytes {
					return nil, false, fmt.Errorf("mcp: message exceeds maximum size (%d bytes)", maxRPCFrameBytes)
				}
				line = append(line, frag...)
				if errors.Is(readErr, bufio.ErrBufferFull) {
					continue
				}
				if readErr != nil && !errors.Is(readErr, io.EOF) {
					return nil, false, readErr
				}
				if len(line) == 0 && errors.Is(readErr, io.EOF) {
					return nil, false, io.EOF
				}
				return bytes.TrimSpace(line), false, nil
			}
		}
		// Header-framed message (Content-Length: N).
		contentLen := -1
		for {
			line, readErr := br.ReadString('\n')
			if readErr != nil {
				return nil, true, readErr
			}
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed == "" {
				break
			}
			k, v, ok := strings.Cut(trimmed, ":")
			if ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
				n, convErr := strconv.Atoi(strings.TrimSpace(v))
				if convErr != nil || n < 0 || n > maxRPCFrameBytes {
					return nil, true, fmt.Errorf("mcp: invalid Content-Length %q", v)
				}
				contentLen = n
			}
		}
		if contentLen < 0 {
			return nil, true, errors.New("mcp: missing Content-Length header")
		}
		buf := make([]byte, contentLen)
		if _, err := io.ReadFull(br, buf); err != nil {
			return nil, true, err
		}
		return buf, true, nil
	}
}

func dispatchMCP(ctx context.Context, msg rpcMessage, version string, caller ToolCaller) (any, *rpcError) {
	switch msg.Method {
	case "initialize":
		var initParams struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(msg.Params, &initParams)
		proto := "2024-11-05"
		if initParams.ProtocolVersion != "" {
			proto = initParams.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": proto,
			"capabilities": map[string]any{
				"tools":     map[string]any{},
				"resources": map[string]any{},
				"prompts":   map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "arcmd",
				"version": version,
			},
			"instructions": "Operate local ArcMD Spaces, rich Markdown notes (highlights, hand-drawn annotations, callouts, toggles, columns, rich HTML tables, LaTeX, Mermaid), hand-drawn Excalidraw whiteboards (.excalidraw.svg), and multi-view databases (.db.md). Call arcmd_format_guide before writing advanced ArcMD rich formatting.",
		}, nil

	case "ping":
		return map[string]any{}, nil

	case "tools/list":
		return map[string]any{"tools": Tools()}, nil

	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid tools/call params: " + err.Error()}
		}
		action, ok := toolAction[p.Name]
		if !ok {
			return nil, &rpcError{Code: -32602, Message: fmt.Sprintf("unknown tool %q", p.Name)}
		}
		if p.Name == "arcmd_format_guide" {
			var args struct {
				Topic string `json:"topic"`
			}
			_ = json.Unmarshal(p.Arguments, &args)
			return map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": FormatGuide(args.Topic)},
				},
			}, nil
		}
		if caller == nil {
			return nil, &rpcError{Code: -32603, Message: "tool caller is not configured"}
		}
		args := p.Arguments
		if len(bytes.TrimSpace(args)) == 0 {
			args = json.RawMessage("{}")
		}
		res, live, err := caller(ctx, action, args)
		if err != nil {
			return map[string]any{
				"isError": true,
				"content": []map[string]any{
					{"type": "text", "text": err.Error()},
				},
			}, nil
		}
		envelope := map[string]any{
			"liveWindow": live,
			"result":     res,
		}
		textBytes, _ := json.MarshalIndent(envelope, "", "  ")
		return map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": string(textBytes)},
			},
		}, nil

	case "resources/list":
		return map[string]any{
			"resources": []map[string]any{
				{
					"uri":         "arcmd://format-guide",
					"name":        "ArcMD Rich Format Specification",
					"description": "Complete guide to ArcMD's rich Markdown, highlights, hand-drawn annotations, callouts, multi-column blocks, rich tables, whiteboards (.excalidraw.svg), and databases (.db.md).",
					"mimeType":    "text/markdown",
				},
				{
					"uri":         "arcmd://skill",
					"name":        "ArcMD Local Agent Skill (SKILL.md)",
					"description": "Agent workflow and CLI/MCP reference for operating ArcMD.",
					"mimeType":    "text/markdown",
				},
			},
		}, nil

	case "resources/read":
		var p struct {
			URI string `json:"uri"`
		}
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid resources/read params"}
		}
		var text string
		switch p.URI {
		case "arcmd://format-guide":
			text = FormatGuide("all")
		case "arcmd://skill":
			text = SkillMarkdown(ResolveBinaryCommand())
		default:
			return nil, &rpcError{Code: -32602, Message: fmt.Sprintf("unknown resource %q", p.URI)}
		}
		return map[string]any{
			"contents": []map[string]any{
				{
					"uri":      p.URI,
					"mimeType": "text/markdown",
					"text":     text,
				},
			},
		}, nil

	case "prompts/list":
		return map[string]any{
			"prompts": []map[string]any{
				{
					"name":        "arcmd-rich-author",
					"description": "Load ArcMD's rich formatting, whiteboard, and database authoring conventions for the current task.",
				},
			},
		}, nil

	case "prompts/get":
		var p struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(msg.Params, &p)
		if p.Name != "arcmd-rich-author" {
			return nil, &rpcError{Code: -32602, Message: fmt.Sprintf("unknown prompt %q", p.Name)}
		}
		return map[string]any{
			"description": "ArcMD rich authoring guide",
			"messages": []map[string]any{
				{
					"role": "user",
					"content": map[string]any{
						"type": "text",
						"text": FormatGuide("all"),
					},
				},
			},
		}, nil

	default:
		return nil, &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", msg.Method)}
	}
}
