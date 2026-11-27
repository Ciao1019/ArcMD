package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/agentmcp"
	"github.com/Ciao1019/ArcMD/internal/app"
	"github.com/Ciao1019/ArcMD/internal/clipper"
	"github.com/Ciao1019/ArcMD/internal/i18n"
	"github.com/Ciao1019/ArcMD/internal/prefs"
)

const cliUsage = `Usage:
  arcmd [file.md ...]                  Open ArcMD desktop app
  arcmd status [-json]                 Show ArcMD status and active Space
  arcmd space <subcommand> [flags]     Manage Spaces (list, create, switch, update, remove)
  arcmd doc <subcommand> [flags]       Manage documents (list, search, read, create, daily, edit, organize, delete, trash-list, restore)
  arcmd board <subcommand> [flags]     Create or update hand-drawn Excalidraw whiteboards (.excalidraw.svg)
  arcmd db <subcommand> [flags]        Create, query, or update multi-view databases (.db.md)
  arcmd format-guide [topic]           Print ArcMD rich Markdown/HTML/Whiteboard/Database format spec
  arcmd skill [print|install|mcp]      Print or install SKILL.md and MCP config for local AI agents
  arcmd clipper [status|install|uninstall]  Manage browser Web Clipper Native Messaging host
  arcmd native-host                    Run Chrome/Chromium Native Messaging stdio host
  arcmd mcp                            Run stdio Model Context Protocol (MCP) server
  arcmd ask [flags] "question"         Ask the read-only Space QA agent a question
`

func dispatchCLI(args []string, cfg app.Config, stdin io.Reader, stdout, stderr io.Writer) (int, bool) {
	if len(args) == 0 {
		return 0, false
	}
	if clipper.IsNativeMessagingInvocation(args) {
		return runNativeHost(cfg, stdin, stdout, stderr), true
	}
	switch args[0] {
	case "ask":
		return ask(args[1:], cfg, stdout, stderr), true
	case "-h", "--help", "help":
		fmt.Fprint(stdout, cliUsage)
		return 0, true
	case "mcp":
		return runMCP(cfg, stdin, stdout, stderr), true
	case "clipper":
		return runClipperCLI(args[1:], cfg, stdout, stderr), true
	case "format-guide", "formats":
		topic := "all"
		if len(args) > 1 && !strings.HasPrefix(args[1], "-") {
			topic = args[1]
		}
		fmt.Fprintln(stdout, agentmcp.FormatGuide(topic))
		return 0, true
	case "skill":
		return runSkill(args[1:], stdout, stderr), true
	case "status":
		return runStatus(args[1:], cfg, stdout, stderr), true
	case "space", "spaces":
		return runSpace(args[1:], cfg, stdout, stderr), true
	case "doc", "docs":
		return runDoc(args[1:], cfg, stdin, stdout, stderr), true
	case "board", "whiteboard":
		return runBoard(args[1:], cfg, stdin, stdout, stderr), true
	case "db", "database":
		return runDB(args[1:], cfg, stdin, stdout, stderr), true
	default:
		return 0, false
	}
}

func runMCP(cfg app.Config, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := agentmcp.Serve(ctx, stdin, stdout, app.Version, func(callCtx context.Context, action string, params json.RawMessage) (any, bool, error) {
		return app.ExecuteControl(callCtx, cfg, app.ControlRequest{
			Action: action,
			Params: params,
		})
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "arcmd mcp:", err)
		return 1
	}
	return 0
}

func runNativeHost(cfg app.Config, stdin io.Reader, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := clipper.Serve(ctx, stdin, stdout, func(callCtx context.Context, action string, params json.RawMessage) (any, bool, error) {
		return app.ExecuteControl(callCtx, cfg, app.ControlRequest{
			Action: action,
			Params: params,
		})
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "arcmd native-host:", err)
		return 1
	}
	return 0
}

func runClipperCLI(args []string, cfg app.Config, stdout, stderr io.Writer) int {
	sub := "status"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		rest = args[1:]
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "arcmd clipper:", err)
		return 1
	}
	switch sub {
	case "status":
		fs := flag.NewFlagSet("clipper status", flag.ContinueOnError)
		fs.SetOutput(stderr)
		asJSON := fs.Bool("json", false, "output JSON")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		targets := clipper.DetectBrowsers(home, cfg.ConfigDir)
		if *asJSON {
			writeJSON(stdout, map[string]any{
				"hostName":           clipper.HostName,
				"defaultExtensionId": clipper.DefaultExtensionID,
				"browsers":           targets,
			})
			return 0
		}
		for _, t := range targets {
			state := "not detected"
			if t.Installed {
				state = "installed"
			} else if t.Detected {
				state = "detected (not installed)"
			}
			fmt.Fprintf(stdout, "%-10s %s\n", t.Name, state)
		}
		return 0

	case "install":
		fs := flag.NewFlagSet("clipper install", flag.ContinueOnError)
		fs.SetOutput(stderr)
		extIDs := fs.String("ext-id", "", "optional comma-separated extra Chrome extension IDs")
		bin := fs.String("bin", "", "optional explicit ArcMD executable path")
		asJSON := fs.Bool("json", false, "output JSON")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		binPath := strings.TrimSpace(*bin)
		if binPath == "" {
			binPath = currentBinaryCommand()
		}
		var extra []string
		if p, pErr := (prefs.Store{Path: cfg.ConfigDir + string(os.PathSeparator) + "prefs.json"}).Load(); pErr == nil && p != nil {
			extra = append(extra, p.ClipperExtensionIDs...)
		}
		if strings.TrimSpace(*extIDs) != "" {
			for _, part := range strings.Split(*extIDs, ",") {
				if id, ok := clipper.ParseExtensionID(part); ok {
					extra = append(extra, id)
				}
			}
		}
		res, instErr := clipper.InstallHost(home, cfg.ConfigDir, binPath, extra)
		if instErr != nil {
			fmt.Fprintln(stderr, "arcmd clipper install:", instErr)
			return 1
		}
		if *asJSON {
			writeJSON(stdout, res)
			return 0
		}
		fmt.Fprintf(stdout, "Installed %s for %d browser(s): %s\n", clipper.HostName, res.InstalledCount, res.ManifestPath)
		for _, w := range res.Warnings {
			fmt.Fprintln(stderr, "warning:", w)
		}
		return 0

	case "uninstall":
		fs := flag.NewFlagSet("clipper uninstall", flag.ContinueOnError)
		fs.SetOutput(stderr)
		asJSON := fs.Bool("json", false, "output JSON")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		removed, rmErr := clipper.UninstallHost(home, cfg.ConfigDir)
		if rmErr != nil {
			fmt.Fprintln(stderr, "arcmd clipper uninstall:", rmErr)
			return 1
		}
		if *asJSON {
			writeJSON(stdout, map[string]any{"removed": removed})
			return 0
		}
		fmt.Fprintf(stdout, "Removed %d browser Native Messaging registration(s).\n", removed)
		return 0

	default:
		fmt.Fprintf(stderr, "arcmd clipper: unknown subcommand %q (use status, install, or uninstall)\n", sub)
		return 2
	}
}

func currentBinaryCommand() string {
	return agentmcp.ResolveBinaryCommand()
}

func runSkill(args []string, stdout, stderr io.Writer) int {
	sub := "print"
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		rest = args[1:]
	}
	switch sub {
	case "print":
		fmt.Fprint(stdout, agentmcp.SkillMarkdown(currentBinaryCommand()))
		return 0
	case "mcp", "mcp-config":
		fmt.Fprint(stdout, agentmcp.MCPConfigJSON(currentBinaryCommand()))
		return 0
	case "install":
		fs := flag.NewFlagSet("skill install", flag.ContinueOnError)
		fs.SetOutput(stderr)
		dir := fs.String("dir", "", "explicit directory to write arcmd/SKILL.md into")
		asJSON := fs.Bool("json", false, "output JSON")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		bin := currentBinaryCommand()
		var paths []string
		if strings.TrimSpace(*dir) != "" {
			p, err := agentmcp.InstallSkillToDir(*dir, bin)
			if err != nil {
				fmt.Fprintln(stderr, "arcmd:", err)
				return 1
			}
			paths = []string{p}
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				fmt.Fprintln(stderr, "arcmd:", err)
				return 1
			}
			paths, err = agentmcp.InstallSkills(home, bin)
			if err != nil {
				fmt.Fprintln(stderr, "arcmd:", err)
				return 1
			}
		}
		if *asJSON {
			writeJSON(stdout, map[string]any{"installed": paths})
			return 0
		}
		for _, p := range paths {
			fmt.Fprintln(stdout, p)
		}
		return 0
	default:
		fmt.Fprintf(stderr, "arcmd skill: unknown subcommand %q (use print, install, or mcp-config)\n", sub)
		return 2
	}
}

func runStatus(args []string, cfg app.Config, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print JSON output")
	if err := parseFlexible(fs, args); err != nil {
		return 2
	}
	return execAndPrint(cfg, "status", nil, *asJSON, stdout, stderr)
}

func runSpace(args []string, cfg app.Config, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(stderr, "usage: arcmd space <list|create|switch|update|remove> [flags]")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list", "ls":
		fs := flag.NewFlagSet("space list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		return execAndPrint(cfg, "space.list", nil, *asJSON, stdout, stderr)

	case "create", "add":
		fs := flag.NewFlagSet("space create", flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("name", "", "Space name")
		root := fs.String("root", "", "Optional folder path")
		icon := fs.String("icon", "", "Optional emoji icon")
		symbol := fs.String("symbol", "", "Optional Lucide symbol")
		color := fs.String("color", "", "Comma-separated hex color(s), e.g. #245BDB")
		appearance := fs.String("appearance", "", "Theme appearance: auto, light, or dark")
		starter := fs.Bool("starter", false, "Write starter guide when folder is empty")
		doSwitch := fs.Bool("switch", true, "Switch to the new Space")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *name == "" && fs.NArg() > 0 {
			*name = strings.Join(fs.Args(), " ")
		}
		params := map[string]any{
			"name":       *name,
			"root":       *root,
			"icon":       *icon,
			"symbol":     *symbol,
			"appearance": *appearance,
			"starter":    *starter,
			"switch":     *doSwitch,
		}
		if strings.TrimSpace(*color) != "" {
			params["colors"] = splitComma(*color)
		}
		return execAndPrint(cfg, "space.create", params, *asJSON, stdout, stderr)

	case "switch", "use":
		fs := flag.NewFlagSet("space switch", flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *spaceArg == "" && fs.NArg() > 0 {
			*spaceArg = strings.Join(fs.Args(), " ")
		}
		return execAndPrint(cfg, "space.switch", map[string]any{"space": *spaceArg}, *asJSON, stdout, stderr)

	case "update", "set":
		fs := flag.NewFlagSet("space update", flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Target Space name or ID (default: active Space)")
		name := fs.String("name", "", "New Space name")
		icon := fs.String("icon", "", "New emoji icon ('-' clears)")
		symbol := fs.String("symbol", "", "New Lucide symbol ('-' clears)")
		color := fs.String("color", "", "Comma-separated 1-3 hex colors (#RRGGBB)")
		appearance := fs.String("appearance", "", "auto, light, or dark")
		attachments := fs.String("attachments", "\x00", "Relative attachments folder ('-' resets to per-doc assets/)")
		doSwitch := fs.Bool("switch", false, "Switch to this Space")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *spaceArg == "" && fs.NArg() > 0 {
			*spaceArg = fs.Arg(0)
		}
		params := map[string]any{"space": *spaceArg, "switch": *doSwitch}
		if *name != "" {
			params["name"] = *name
		}
		if *icon != "" {
			params["icon"] = *icon
		}
		if *symbol != "" {
			params["symbol"] = *symbol
		}
		if *color != "" {
			params["colors"] = splitComma(*color)
		}
		if *appearance != "" {
			params["appearance"] = *appearance
		}
		if *attachments != "\x00" {
			params["attachmentsDir"] = *attachments
		}
		return execAndPrint(cfg, "space.update", params, *asJSON, stdout, stderr)

	case "remove", "rm":
		fs := flag.NewFlagSet("space remove", flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID to remove from ArcMD list")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *spaceArg == "" && fs.NArg() > 0 {
			*spaceArg = strings.Join(fs.Args(), " ")
		}
		return execAndPrint(cfg, "space.remove", map[string]any{"space": *spaceArg}, *asJSON, stdout, stderr)

	default:
		fmt.Fprintf(stderr, "arcmd space: unknown subcommand %q\n", sub)
		return 2
	}
}

func runDoc(args []string, cfg app.Config, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(stderr, "usage: arcmd doc <list|search|read|create|daily|edit|organize|open|mkdir|rename|move|pin|unpin|favorite|unfavorite|delete|trash-list|restore> [flags]")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "list", "ls":
		fs := flag.NewFlagSet("doc list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID")
		dir := fs.String("dir", "", "Subfolder path")
		kind := fs.String("kind", "", "Filter by kind: markdown, whiteboard, database, pdf, book")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *dir == "" && fs.NArg() > 0 {
			*dir = fs.Arg(0)
		}
		return execAndPrint(cfg, "doc.list", map[string]any{
			"space": *spaceArg,
			"dir":   *dir,
			"kind":  *kind,
		}, *asJSON, stdout, stderr)

	case "search", "grep":
		fs := flag.NewFlagSet("doc search", flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID")
		mode := fs.String("mode", "", "search, grep, links, or overview")
		regex := fs.Bool("regex", false, "Regular expression match (grep mode)")
		scope := fs.String("scope", "", "Optional folder/glob scope")
		ctxLines := fs.Int("C", 1, "Context lines for grep")
		limit := fs.Int("limit", 10, "Max results")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		m := *mode
		if m == "" {
			if sub == "grep" {
				m = "grep"
			} else {
				m = "search"
			}
		}
		query := strings.TrimSpace(strings.Join(fs.Args(), " "))
		return execAndPrint(cfg, "doc.search", map[string]any{
			"space":   *spaceArg,
			"query":   query,
			"mode":    m,
			"regex":   *regex,
			"scope":   *scope,
			"context": *ctxLines,
			"limit":   *limit,
		}, *asJSON, stdout, stderr)

	case "read", "cat":
		fs := flag.NewFlagSet("doc read", flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID")
		docPath := fs.String("path", "", "Document relative path")
		from := fs.Int("from", 0, "1-based start line")
		to := fs.Int("to", 0, "1-based end line")
		lines := fs.Bool("lines", false, "Prefix lines with L<n>: numbers")
		outline := fs.Bool("outline", false, "Include heading outline")
		links := fs.Bool("links", false, "Include wikilinks/backlinks/tags")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *docPath == "" && fs.NArg() > 0 {
			*docPath = strings.Join(fs.Args(), " ")
		}
		res, _, err := app.ExecuteControl(context.Background(), cfg, app.ControlRequest{
			Action: "doc.read",
			Params: mustJSON(map[string]any{
				"space":           *spaceArg,
				"path":            *docPath,
				"startLine":       *from,
				"endLine":         *to,
				"withLineNumbers": *lines,
				"includeOutline":  *outline,
				"includeLinks":    *links,
			}),
		})
		if err != nil {
			fmt.Fprintln(stderr, "arcmd:", err)
			return 1
		}
		if *asJSON {
			writeJSON(stdout, res)
			return 0
		}
		if m, ok := res.(map[string]any); ok {
			if c, ok := m["content"].(string); ok {
				fmt.Fprint(stdout, c)
				if !strings.HasSuffix(c, "\n") {
					fmt.Fprintln(stdout)
				}
				return 0
			}
			if s, ok := m["snapshot"].(string); ok && s != "" {
				fmt.Fprint(stdout, s)
				return 0
			}
			if s, ok := m["summary"].(string); ok {
				fmt.Fprintln(stdout, s)
				return 0
			}
		}
		writeJSON(stdout, res)
		return 0

	case "create", "new", "daily":
		fs := flag.NewFlagSet("doc create", flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID")
		docPath := fs.String("path", "", "Optional relative path")
		dir := fs.String("dir", "", "Optional subfolder")
		title := fs.String("title", "", "Document title")
		content := fs.String("content", "", "Markdown content")
		fromStdin := fs.Bool("stdin", false, "Read Markdown content from stdin")
		tmpl := fs.String("template", "", "Template name from .arcmd/templates")
		daily := fs.Bool("daily", sub == "daily", "Create/open Today's Note")
		icon := fs.String("icon", "", "Page emoji icon")
		cover := fs.String("cover", "", "Page cover (gradient:dawn or image path)")
		goal := fs.Int("goal", 0, "Word count goal")
		fmJSON := fs.String("frontmatter", "", "Optional JSON object of frontmatter keys")
		overwrite := fs.Bool("overwrite", false, "Overwrite if path exists")
		pin := fs.Bool("pin", false, "Pin document in Space")
		fav := fs.Bool("favorite", false, "Add document to global Favorites")
		open := fs.Bool("open", false, "Open document in ArcMD window")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *docPath == "" && *title == "" && fs.NArg() > 0 {
			arg := strings.Join(fs.Args(), " ")
			lower := strings.ToLower(arg)
			if strings.Contains(arg, "/") ||
				strings.HasSuffix(lower, ".md") ||
				strings.HasSuffix(lower, ".arcmd") ||
				strings.HasSuffix(lower, ".svg") ||
				strings.HasSuffix(lower, ".excalidraw") ||
				strings.HasSuffix(lower, ".pdf") ||
				strings.HasSuffix(lower, ".epub") ||
				strings.HasSuffix(lower, ".html") ||
				strings.HasSuffix(lower, ".htm") {
				*docPath = arg
			} else {
				*title = arg
			}
		}
		body := *content
		if *fromStdin {
			b, err := io.ReadAll(stdin)
			if err != nil {
				fmt.Fprintln(stderr, "arcmd:", err)
				return 1
			}
			body = string(b)
		}
		params := map[string]any{
			"space":     *spaceArg,
			"path":      *docPath,
			"dir":       *dir,
			"title":     *title,
			"content":   body,
			"template":  *tmpl,
			"daily":     *daily,
			"icon":      *icon,
			"cover":     *cover,
			"overwrite": *overwrite,
			"pin":       *pin,
			"favorite":  *fav,
			"open":      *open,
		}
		if *goal > 0 {
			params["goal"] = *goal
		}
		if strings.TrimSpace(*fmJSON) != "" {
			var fm map[string]any
			if err := json.Unmarshal([]byte(*fmJSON), &fm); err != nil {
				fmt.Fprintln(stderr, "arcmd: invalid -frontmatter JSON:", err)
				return 2
			}
			params["frontmatter"] = fm
		}
		return execAndPrint(cfg, "doc.create", params, *asJSON, stdout, stderr)

	case "edit", "write", "append", "prepend", "patch":
		fs := flag.NewFlagSet("doc edit", flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID")
		docPath := fs.String("path", "", "Document relative path")
		defaultMode := "replace"
		if sub == "append" || sub == "prepend" || sub == "patch" {
			defaultMode = sub
		}
		mode := fs.String("mode", defaultMode, "replace, patch, append, prepend, or frontmatter")
		content := fs.String("content", "", "Markdown content")
		fromStdin := fs.Bool("stdin", false, "Read Markdown content from stdin")
		oldText := fs.String("old", "", "Exact substring to replace (patch mode)")
		newText := fs.String("new", "", "Replacement substring (patch mode)")
		replaceAll := fs.Bool("all", false, "Replace all occurrences (patch mode)")
		heading := fs.String("heading", "", "Section heading target for append/prepend")
		icon := fs.String("icon", "\x00", "Page emoji icon ('-' clears)")
		cover := fs.String("cover", "\x00", "Page cover ('-' clears)")
		goal := fs.Int("goal", -999, "Word goal (-1 clears)")
		fmJSON := fs.String("frontmatter", "", "JSON object of frontmatter keys")
		open := fs.Bool("open", false, "Open document in ArcMD window")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *docPath == "" && fs.NArg() > 0 {
			*docPath = strings.Join(fs.Args(), " ")
		}
		body := *content
		if *fromStdin {
			b, err := io.ReadAll(stdin)
			if err != nil {
				fmt.Fprintln(stderr, "arcmd:", err)
				return 1
			}
			body = string(b)
		}
		params := map[string]any{
			"space":      *spaceArg,
			"path":       *docPath,
			"mode":       *mode,
			"content":    body,
			"oldText":    *oldText,
			"newText":    *newText,
			"replaceAll": *replaceAll,
			"heading":    *heading,
			"open":       *open,
		}
		if *icon != "\x00" {
			params["icon"] = *icon
		}
		if *cover != "\x00" {
			params["cover"] = *cover
		}
		if *goal != -999 {
			params["goal"] = *goal
		}
		if strings.TrimSpace(*fmJSON) != "" {
			var fm map[string]any
			if err := json.Unmarshal([]byte(*fmJSON), &fm); err != nil {
				fmt.Fprintln(stderr, "arcmd: invalid -frontmatter JSON:", err)
				return 2
			}
			params["frontmatter"] = fm
		}
		return execAndPrint(cfg, "doc.edit", params, *asJSON, stdout, stderr)

	case "organize", "open", "mkdir", "rename", "move", "pin", "unpin", "favorite", "unfavorite":
		fs := flag.NewFlagSet("doc organize", flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Source Space name or ID")
		defaultOp := ""
		if sub != "organize" {
			defaultOp = sub
		}
		op := fs.String("op", defaultOp, "open, mkdir, rename, move, pin, unpin, favorite, or unfavorite")
		docPath := fs.String("path", "", "Target document or folder path")
		newName := fs.String("name", "", "New name (for rename)")
		toSpace := fs.String("to-space", "", "Destination Space (for move)")
		toDir := fs.String("to-dir", "", "Destination folder (for move)")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *docPath == "" && fs.NArg() > 0 {
			*docPath = fs.Arg(0)
			if *newName == "" && *op == "rename" && fs.NArg() > 1 {
				*newName = strings.Join(fs.Args()[1:], " ")
			}
		}
		return execAndPrint(cfg, "doc.organize", map[string]any{
			"space":   *spaceArg,
			"op":      *op,
			"path":    *docPath,
			"newName": *newName,
			"toSpace": *toSpace,
			"toDir":   *toDir,
		}, *asJSON, stdout, stderr)

	case "delete", "rm", "trash", "trash-list", "restore":
		fs := flag.NewFlagSet("doc delete", flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID")
		defaultOp := "trash"
		if sub == "trash-list" {
			defaultOp = "list_trash"
		} else if sub == "restore" {
			defaultOp = "restore"
		}
		op := fs.String("op", defaultOp, "trash, list_trash, or restore")
		docPath := fs.String("path", "", "Document or folder path")
		trashID := fs.String("id", "", "Trash item ID (for restore)")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *docPath == "" && fs.NArg() > 0 {
			*docPath = strings.Join(fs.Args(), " ")
		}
		return execAndPrint(cfg, "doc.delete", map[string]any{
			"space":   *spaceArg,
			"op":      *op,
			"path":    *docPath,
			"trashID": *trashID,
		}, *asJSON, stdout, stderr)

	default:
		fmt.Fprintf(stderr, "arcmd doc: unknown subcommand %q\n", sub)
		return 2
	}
}

func runBoard(args []string, cfg app.Config, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(stderr, "usage: arcmd board <read|write> [flags]")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "read":
		return runDoc(append([]string{"read", "-json"}, rest...), cfg, stdin, stdout, stderr)

	case "write", "create":
		fs := flag.NewFlagSet("board write", flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID")
		boardPath := fs.String("path", "", "Target .excalidraw.svg path")
		mermaidStdin := fs.Bool("mermaid", false, "Read Mermaid diagram source from stdin")
		mermaidStr := fs.String("mermaid-src", "", "Inline Mermaid diagram source")
		elemsStdin := fs.Bool("elements-stdin", false, "Read declarative elements JSON array from stdin")
		elemsStr := fs.String("elements", "", "Inline declarative elements JSON array")
		appendMode := fs.Bool("append", false, "Append/merge into existing whiteboard")
		embedDoc := fs.String("embed", "", "Optional .md document path to embed this whiteboard into")
		embedHeading := fs.String("heading", "", "Optional heading in -embed doc")
		caption := fs.String("caption", i18n.T("手绘白板"), "Embed caption")
		open := fs.Bool("open", false, "Open in ArcMD window")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *boardPath == "" && fs.NArg() > 0 {
			*boardPath = strings.Join(fs.Args(), " ")
		}
		mSrc := *mermaidStr
		if *mermaidStdin {
			b, err := io.ReadAll(stdin)
			if err != nil {
				fmt.Fprintln(stderr, "arcmd:", err)
				return 1
			}
			mSrc = string(b)
		}
		var elems any
		rawElems := strings.TrimSpace(*elemsStr)
		if *elemsStdin {
			b, err := io.ReadAll(stdin)
			if err != nil {
				fmt.Fprintln(stderr, "arcmd:", err)
				return 1
			}
			rawElems = strings.TrimSpace(string(b))
		}
		if rawElems != "" {
			if err := json.Unmarshal([]byte(rawElems), &elems); err != nil {
				fmt.Fprintln(stderr, "arcmd: invalid whiteboard elements JSON:", err)
				return 2
			}
		}
		params := map[string]any{
			"space":        *spaceArg,
			"path":         *boardPath,
			"mermaid":      mSrc,
			"append":       *appendMode,
			"embedInDoc":   *embedDoc,
			"embedHeading": *embedHeading,
			"caption":      *caption,
			"open":         *open,
		}
		if elems != nil {
			params["elements"] = elems
		}
		return execAndPrint(cfg, "whiteboard.write", params, *asJSON, stdout, stderr)

	default:
		fmt.Fprintf(stderr, "arcmd board: unknown subcommand %q (use read or write)\n", sub)
		return 2
	}
}

func runDB(args []string, cfg app.Config, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(stderr, "usage: arcmd db <inspect|create|query|add-record|update-record|delete-record|add-prop|update-prop|delete-prop|add-view|update-view|delete-view|embed> [flags]")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "inspect":
		fs := flag.NewFlagSet("db inspect", flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID")
		dbPath := fs.String("db", "", "Database .db.md path")
		asJSON := fs.Bool("json", true, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *dbPath == "" && fs.NArg() > 0 {
			*dbPath = strings.Join(fs.Args(), " ")
		}
		return execAndPrint(cfg, "database.schema", map[string]any{
			"space": *spaceArg,
			"op":    "inspect",
			"path":  *dbPath,
		}, *asJSON, stdout, stderr)

	case "create":
		fs := flag.NewFlagSet("db create", flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID")
		dbPath := fs.String("path", "", "Target .db.md path")
		title := fs.String("title", "", "Database title")
		sourceType := fs.String("source", "folder", "folder, inline, or space")
		sourceDir := fs.String("source-dir", "", "Optional folder scanned for records")
		icon := fs.String("icon", "", "Optional emoji icon")
		specStdin := fs.Bool("spec-stdin", false, "Read full JSON create spec from stdin")
		specJSON := fs.String("spec", "", "Inline JSON create spec")
		overwrite := fs.Bool("overwrite", false, "Overwrite if database path already exists")
		open := fs.Bool("open", false, "Open database in ArcMD window")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *dbPath == "" && fs.NArg() > 0 {
			*dbPath = strings.Join(fs.Args(), " ")
		}
		params := map[string]any{
			"op":         "create",
			"space":      *spaceArg,
			"path":       *dbPath,
			"title":      *title,
			"sourceType": *sourceType,
			"sourceDir":  *sourceDir,
			"icon":       *icon,
			"overwrite":  *overwrite,
			"open":       *open,
		}
		rawSpec := strings.TrimSpace(*specJSON)
		if *specStdin {
			b, err := io.ReadAll(stdin)
			if err != nil {
				fmt.Fprintln(stderr, "arcmd:", err)
				return 1
			}
			rawSpec = strings.TrimSpace(string(b))
		}
		if rawSpec != "" {
			var extra map[string]any
			if err := json.Unmarshal([]byte(rawSpec), &extra); err != nil {
				fmt.Fprintln(stderr, "arcmd: invalid -spec JSON:", err)
				return 2
			}
			for k, v := range extra {
				if v != nil {
					params[k] = v
				}
			}
			params["op"] = "create"
			if *spaceArg != "" {
				params["space"] = *spaceArg
			}
			if *dbPath != "" {
				params["path"] = *dbPath
			}
			if *overwrite {
				params["overwrite"] = true
			}
		}
		return execAndPrint(cfg, "database.schema", params, *asJSON, stdout, stderr)

	case "add-prop", "update-prop", "delete-prop":
		fs := flag.NewFlagSet("db "+sub, flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID")
		dbPath := fs.String("db", "", "Database .db.md path")
		name := fs.String("name", "", "Property name")
		pType := fs.String("type", "", "Property type")
		propJSON := fs.String("prop", "", "Full property JSON spec")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *dbPath == "" && fs.NArg() > 0 {
			*dbPath = fs.Arg(0)
		}
		opMap := map[string]string{
			"add-prop":    "add_property",
			"update-prop": "update_property",
			"delete-prop": "delete_property",
		}
		propObj := map[string]any{}
		if strings.TrimSpace(*propJSON) != "" {
			if err := json.Unmarshal([]byte(*propJSON), &propObj); err != nil {
				fmt.Fprintln(stderr, "arcmd: invalid -prop JSON:", err)
				return 2
			}
		}
		if *name != "" {
			propObj["name"] = *name
		}
		if *pType != "" {
			propObj["type"] = *pType
		}
		if sub == "add-prop" {
			if t, _ := propObj["type"].(string); strings.TrimSpace(t) == "" {
				propObj["type"] = "text"
			}
		}
		return execAndPrint(cfg, "database.schema", map[string]any{
			"space":    *spaceArg,
			"op":       opMap[sub],
			"path":     *dbPath,
			"property": propObj,
		}, *asJSON, stdout, stderr)

	case "add-view", "update-view", "delete-view", "embed":
		fs := flag.NewFlagSet("db "+sub, flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID")
		dbPath := fs.String("db", "", "Database .db.md path")
		viewName := fs.String("view", "", "View name or ID")
		viewType := fs.String("type", "", "table, board, timeline, calendar, list, gallery, chart, feed, form")
		groupBy := fs.String("group-by", "", "Property name/ID for grouping")
		viewJSON := fs.String("spec", "", "Full view JSON spec")
		docPath := fs.String("doc", "", "Target .md document path for embed")
		height := fs.Float64("height", 0, "Optional inline embed height")
		open := fs.Bool("open", false, "Open in ArcMD window")
		asJSON := fs.Bool("json", false, "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *dbPath == "" && fs.NArg() > 0 {
			*dbPath = fs.Arg(0)
		}
		opMap := map[string]string{
			"add-view":    "add_view",
			"update-view": "update_view",
			"delete-view": "delete_view",
			"embed":       "embed_view",
		}
		viewObj := map[string]any{}
		if strings.TrimSpace(*viewJSON) != "" {
			if err := json.Unmarshal([]byte(*viewJSON), &viewObj); err != nil {
				fmt.Fprintln(stderr, "arcmd: invalid -spec JSON:", err)
				return 2
			}
		}
		if *viewName != "" {
			viewObj["name"] = *viewName
		}
		if *viewType != "" {
			viewObj["type"] = *viewType
		}
		if *groupBy != "" {
			viewObj["groupBy"] = *groupBy
		}
		if sub == "add-view" {
			if t, _ := viewObj["type"].(string); strings.TrimSpace(t) == "" {
				viewObj["type"] = "table"
			}
		}
		return execAndPrint(cfg, "database.schema", map[string]any{
			"space":      *spaceArg,
			"op":         opMap[sub],
			"path":       *dbPath,
			"view":       viewObj,
			"embedInDoc": *docPath,
			"height":     *height,
			"open":       *open,
		}, *asJSON, stdout, stderr)

	case "query", "add-record", "update-record", "delete-record":
		fs := flag.NewFlagSet("db "+sub, flag.ContinueOnError)
		fs.SetOutput(stderr)
		spaceArg := fs.String("space", "", "Space name or ID")
		dbPath := fs.String("db", "", "Database .db.md path")
		view := fs.String("view", "", "Optional view name or ID (query)")
		search := fs.String("search", "", "Optional search query")
		record := fs.String("record", "", "Record ID, path, or title (update/delete)")
		title := fs.String("title", "", "Record title")
		fieldsJSON := fs.String("fields", "", "JSON object of property values")
		body := fs.String("body", "\x00", "Optional Markdown body for page-backed record")
		bodyStdin := fs.Bool("stdin", false, "Read record Markdown body from stdin")
		asJSON := fs.Bool("json", sub == "query", "print JSON output")
		if err := parseFlexible(fs, rest); err != nil {
			return 2
		}
		if *dbPath == "" && fs.NArg() > 0 {
			*dbPath = fs.Arg(0)
		}
		opMap := map[string]string{
			"query":         "query",
			"add-record":    "add",
			"update-record": "update",
			"delete-record": "delete",
		}
		params := map[string]any{
			"space":  *spaceArg,
			"op":     opMap[sub],
			"path":   *dbPath,
			"view":   *view,
			"search": *search,
			"record": *record,
			"title":  *title,
		}
		if strings.TrimSpace(*fieldsJSON) != "" {
			var fields map[string]any
			if err := json.Unmarshal([]byte(*fieldsJSON), &fields); err != nil {
				fmt.Fprintln(stderr, "arcmd: invalid -fields JSON:", err)
				return 2
			}
			params["fields"] = fields
		}
		if *bodyStdin {
			b, err := io.ReadAll(stdin)
			if err != nil {
				fmt.Fprintln(stderr, "arcmd:", err)
				return 1
			}
			params["body"] = string(b)
		} else if *body != "\x00" {
			params["body"] = *body
		}
		return execAndPrint(cfg, "database.records", params, *asJSON, stdout, stderr)

	default:
		fmt.Fprintf(stderr, "arcmd db: unknown subcommand %q\n", sub)
		return 2
	}
}

func execAndPrint(cfg app.Config, action string, params any, asJSON bool, stdout, stderr io.Writer) int {
	res, live, err := app.ExecuteControl(context.Background(), cfg, app.ControlRequest{
		Action: action,
		Params: mustJSON(params),
	})
	if err != nil {
		fmt.Fprintln(stderr, "arcmd:", err)
		return 1
	}
	if asJSON {
		writeJSON(stdout, map[string]any{"ok": true, "liveWindow": live, "result": res})
		return 0
	}
	// Human-friendly output for doc.search when output string is present.
	if m, ok := res.(map[string]any); ok {
		if outStr, ok := m["output"].(string); ok {
			fmt.Fprintln(stdout, outStr)
			return 0
		}
	}
	writeJSON(stdout, res)
	return 0
}

func mustJSON(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func splitComma(s string) []string {
	var out []string
	for part := range strings.SplitSeq(s, ",") {
		if t := strings.TrimSpace(part); t != "" {
			out = append(out, t)
		}
	}
	return out
}

type boolFlag interface {
	IsBoolFlag() bool
}

// parseFlexible allows flags to appear either before or after positional
// arguments, matching how local CLI agents invoke subcommands.
func parseFlexible(fs *flag.FlagSet, args []string) error {
	var flags []string
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		if strings.Contains(a, "=") {
			continue
		}
		name := strings.TrimLeft(a, "-")
		f := fs.Lookup(name)
		if f == nil {
			continue
		}
		if bf, ok := f.Value.(boolFlag); ok && bf.IsBoolFlag() {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return fs.Parse(append(flags, pos...))
}
