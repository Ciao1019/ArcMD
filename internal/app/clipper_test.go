package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/agent"
	"github.com/Ciao1019/ArcMD/internal/clipper"
	"github.com/Ciao1019/ArcMD/internal/database"
	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

type stubClipAIProvider struct {
	reply string
	err   error
	got   agent.Request
}

func (s *stubClipAIProvider) Complete(ctx context.Context, req agent.Request, onDelta func(agent.Delta)) (agent.Response, error) {
	s.got = req
	if s.err != nil {
		return agent.Response{}, s.err
	}
	return agent.Response{Message: agent.Message{Role: agent.Assistant, Text: s.reply}}, nil
}

func mustMarshalClipParams(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestClipEndToEndContextUploadSaveCheckURLAndAI(t *testing.T) {
	a, tt := newTestApp(t)
	sp := a.state.ActiveSpace()

	// Create a folder and a database inside the active Space.
	if err := os.MkdirAll(filepath.Join(sp.Root, "Reading"), 0o755); err != nil {
		t.Fatal(err)
	}
	db := database.NewDefault("稍后阅读", database.SourceConfig{Type: database.SourceFolder, Dir: "Reading/Clips"})
	db.Properties = append(db.Properties,
		database.Property{ID: "prop_clip_url", Key: "url", Name: "来源", Type: database.PropURL},
		database.Property{ID: "prop_clip_stage", Key: "stage", Name: "进度", Type: database.PropSelect, Options: []database.SelectOption{{ID: "opt_todo", Name: "待精读", Color: "blue"}}},
		database.Property{ID: "prop_clip_topics", Key: "topics", Name: "主题", Type: database.PropMultiSelect},
	)
	dbRel := "Reading/稍后阅读.db.md"
	dbFull, err := vault.Resolve(sp.Root, dbRel)
	if err != nil {
		t.Fatal(err)
	}
	dbText, err := database.EncodeDatabaseMarkdown(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbFull, []byte(dbText), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()

	// 1. clip.ping
	pingAny, err := a.handleControl(ControlRequest{Action: "clip.ping"})
	if err != nil {
		t.Fatalf("clip.ping: %v", err)
	}
	ping := pingAny.(map[string]any)
	if ping["protocol"] != clipper.ProtocolVersion || ping["hostName"] != clipper.HostName {
		t.Fatalf("unexpected clip.ping result: %+v", ping)
	}

	// 2. clip.context returns spaces, folders, and database schemas.
	ctxAny, err := a.handleControl(ControlRequest{Action: "clip.context"})
	if err != nil {
		t.Fatalf("clip.context: %v", err)
	}
	ctxJSON, _ := json.Marshal(ctxAny)
	if !strings.Contains(string(ctxJSON), `"Reading"`) || !strings.Contains(string(ctxJSON), `"稍后阅读"`) || !strings.Contains(string(ctxJSON), `"待精读"`) {
		t.Fatalf("clip.context missing expected folder/database schema: %s", ctxJSON)
	}

	// 3. clip.upload_asset writes PNG atomically under Reading/assets/ and returns relative URL.
	pngBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R'}
	assetAny, err := a.handleControl(ControlRequest{
		Action: "clip.upload_asset",
		Params: mustMarshalClipParams(t, map[string]any{
			"space":      sp.ID,
			"docDir":     "Reading",
			"filename":   "diagram.jpg", // extension should normalize to .png from magic bytes
			"mimeType":   "image/png",
			"dataBase64": "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes),
		}),
	})
	if err != nil {
		t.Fatalf("clip.upload_asset: %v", err)
	}
	assetRes := assetAny.(map[string]any)
	relURL, _ := assetRes["relUrl"].(string)
	if !strings.HasPrefix(relURL, "assets/") || !strings.HasSuffix(relURL, ".png") {
		t.Fatalf("unexpected relUrl: %q (%+v)", relURL, assetRes)
	}
	storedFull, err := vault.Resolve(sp.Root, assetRes["storedPath"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storedFull); err != nil {
		t.Fatalf("uploaded asset missing on disk: %v", err)
	}

	// 4. clip.save (create) creates note with frontmatter source URL and localized image.
	clipURL := "https://mp.weixin.qq.com/s/arcmd-test?utm_source=wechat&id=42#top"
	saveAny, err := a.handleControl(ControlRequest{
		Action: "clip.save",
		Params: mustMarshalClipParams(t, map[string]any{
			"space":   sp.ID,
			"mode":    "create",
			"dir":     "Reading",
			"title":   "Web Clipper: Native Test / Demo",
			"content": "# Web Clipper Demo\n\n![diagram](" + relURL + ")\n",
			"cover":   relURL,
			"frontmatter": map[string]any{
				"source": clipURL,
				"tags":   []string{"clipper", "go"},
			},
			"open": true,
		}),
	})
	if err != nil {
		t.Fatalf("clip.save create: %v", err)
	}
	saveRes := saveAny.(map[string]any)
	createdPath := saveRes["path"].(string)
	if !strings.HasPrefix(createdPath, "Reading/") {
		t.Fatalf("expected note under Reading/, got %q", createdPath)
	}

	// 5. clip.save (database) adds a typed record to 稍后阅读.db.md.
	dbSaveAny, err := a.handleControl(ControlRequest{
		Action: "clip.save",
		Params: mustMarshalClipParams(t, map[string]any{
			"space":   sp.ID,
			"mode":    "database",
			"path":    dbRel,
			"title":   "Database Clipped Article",
			"content": "Clipped database body.",
			"fields": map[string]any{
				"url":    "https://mp.weixin.qq.com/s/arcmd-test?id=42",
				"stage":  "待精读",
				"topics": []string{"Go", "架构"},
			},
		}),
	})
	if err != nil {
		t.Fatalf("clip.save database: %v", err)
	}
	if dbSaveAny.(map[string]any)["recordId"] == "" {
		t.Fatalf("expected non-empty recordId: %+v", dbSaveAny)
	}

	// 6. clip.check_url detects BOTH the created Markdown note and the database record (ignoring utm_source/fragment).
	checkAny, err := a.handleControl(ControlRequest{
		Action: "clip.check_url",
		Params: mustMarshalClipParams(t, map[string]any{
			"url": "https://mp.weixin.qq.com/s/arcmd-test?id=42&fbclid=abc",
		}),
	})
	if err != nil {
		t.Fatalf("clip.check_url: %v", err)
	}
	checkRes := checkAny.(map[string]any)
	matches := checkRes["matches"].([]clipURLMatch)
	if len(matches) != 2 {
		t.Fatalf("expected 2 URL matches (doc + database), got %d: %+v", len(matches), matches)
	}

	// 7. clip.save (daily top & append bottom)
	if _, err := a.handleControl(ControlRequest{
		Action: "clip.save",
		Params: mustMarshalClipParams(t, map[string]any{
			"space":         sp.ID,
			"mode":          "daily",
			"dailyPosition": "top",
			"content":       "> First clipped highlight",
		}),
	}); err != nil {
		t.Fatalf("clip.save daily: %v", err)
	}
	if _, err := a.handleControl(ControlRequest{
		Action: "clip.save",
		Params: mustMarshalClipParams(t, map[string]any{
			"space":          sp.ID,
			"mode":           "append",
			"path":           createdPath,
			"appendPosition": "bottom",
			"content":        "## Appended Note\n\nExtra context.",
		}),
	}); err != nil {
		t.Fatalf("clip.save append: %v", err)
	}
	updated, err := readFile(sp, createdPath)
	if err != nil || !strings.Contains(updated, "Appended Note") {
		t.Fatalf("appended text missing: %v\n%s", err, updated)
	}

	// 8. clip.ai reuses ArcMD's configured AI settings and parses JSON reply cleanly.
	a.prefs.AI.Provider = "openai"
	a.prefs.AI.Model = "gpt-4.1-mini"
	a.qa.keyRead = true
	a.qa.key = "sk-test-clipper"
	stub := &stubClipAIProvider{
		reply: "```json\n{\"summary\":\"Native Go editor summary.\",\"tags\":[\"#Go\",\"NativeUI\",\"go\"]}\n```",
	}
	a.qa.provider = func(agent.Settings) agent.Provider { return stub }
	aiAny, err := a.handleClipAIAsync(context.Background(), mustMarshalClipParams(t, map[string]any{
		"title":   "Native Go Editor",
		"url":     clipURL,
		"content": "Full article text about pure Go native Markdown editing.",
		"mode":    "both",
	}))
	if err != nil {
		t.Fatalf("handleClipAIAsync: %v", err)
	}
	aiRes := aiAny.(map[string]any)
	if aiRes["summary"] != "Native Go editor summary." {
		t.Fatalf("unexpected AI summary: %+v", aiRes)
	}
	tags := aiRes["tags"].([]string)
	if len(tags) != 2 || tags[0] != "Go" || tags[1] != "NativeUI" {
		t.Fatalf("unexpected deduplicated AI tags: %+v", tags)
	}

	// 9. Settings › AI renders the Web Clipper section cleanly.
	a.openSettings(tabAI)
	tt.Frame()
	if !tt.HasText("浏览器剪藏 (Web Clipper)") || !tt.HasText("一键安装连接") {
		t.Fatalf("expected Web Clipper section in Settings › AI: %v", tt.Texts())
	}

	// 10. Clipped colored highlights import into ArcMD with native Highlight colors (not text color).
	hlDoc := editor.ImportMarkdown("> <mark>Yellow note</mark>\n\n> <mark style=\"background-color:#D8E4FF\" data-highlight=\"light-blue\">Blue note</mark>\n")
	if len(hlDoc.Blocks) != 2 {
		t.Fatalf("expected 2 highlight quote blocks, got %d", len(hlDoc.Blocks))
	}
	if got := hlDoc.Blocks[0].Runs[0].Style.Highlight; got != "light-yellow" {
		t.Fatalf("yellow mark highlight = %q, want light-yellow", got)
	}
	if got := hlDoc.Blocks[1].Runs[0].Style; got.Highlight != "light-blue" || got.Color != "" {
		t.Fatalf("blue mark style = %+v, want Highlight=light-blue Color=''", got)
	}
}

func TestClipperSettingsSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render Settings › AI Web Clipper")
	for _, variant := range []string{"light", "dark"} {
		t.Run(variant, func(t *testing.T) {
			a, tt := newTestApp(t)
			tt.SetSize(960, 680)
			if variant == "dark" {
				tt.SetDark(true)
			}
			a.openSettings(tabAI)
			a.clipperTargets = []clipper.BrowserTarget{
				{ID: "chrome", Name: "Google Chrome", Detected: true, Installed: true},
				{ID: "edge", Name: "Microsoft Edge", Detected: true, Installed: true},
			}
			a.clipperChecked = true
			for range 4 {
				tt.Frame()
			}
			// Scroll the Settings › AI pane to the bottom so the Web Clipper card is centered in view.
			for range 8 {
				tt.Scroll(560, 360, 0, 180)
			}
			for range 3 {
				tt.Frame()
			}
			file, err := os.Create(filepath.Join(dir, "settings-ai-clipper-"+variant+".png"))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if err := png.Encode(file, tt.Image()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
