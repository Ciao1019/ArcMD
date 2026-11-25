package agentmcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ciao1019/ArcMD/internal/fsutil"
)

// SkillTarget describes a local coding agent's skill directory on disk.
type SkillTarget struct {
	Agent     string
	RootDir   string
	SkillFile string
	Detected  bool
}

// ResolveBinaryCommand returns the CLI command path to embed in SKILL.md and
// MCP configs, falling back to "arcmd" when running from an ephemeral `go run`
// build cache, test binary, or macOS AppTranslocation path.
func ResolveBinaryCommand() string {
	exe, err := os.Executable()
	if err != nil {
		return "arcmd"
	}
	return resolveBinaryPath(exe)
}

func resolveBinaryPath(exe string) string {
	exe = strings.TrimSpace(exe)
	if exe == "" {
		return "arcmd"
	}
	slash := filepath.ToSlash(exe)
	if strings.Contains(slash, "go-build") ||
		strings.Contains(slash, "/AppTranslocation/") ||
		strings.HasSuffix(slash, ".test") ||
		strings.HasSuffix(slash, ".test.exe") {
		return "arcmd"
	}
	return exe
}

// MCPConfigJSON returns a ready-to-paste mcpServers JSON configuration for
// Claude Desktop, Cursor, Windsurf, Antigravity, and other MCP hosts.
func MCPConfigJSON(binPath string) string {
	binPath = strings.TrimSpace(binPath)
	if binPath == "" {
		binPath = "arcmd"
	}
	cfg := map[string]any{
		"mcpServers": map[string]any{
			"arcmd": map[string]any{
				"command": binPath,
				"args":    []string{"mcp"},
			},
		},
	}
	raw, _ := json.MarshalIndent(cfg, "", "  ")
	return string(raw) + "\n"
}

// DetectSkillTargets checks standard local coding agent configuration folders
// under homeDir and reports where the ArcMD SKILL.md can be installed.
func DetectSkillTargets(homeDir string) []SkillTarget {
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" {
		return nil
	}
	candidates := []struct {
		agent string
		root  string
		skill string
	}{
		{"Claude Code", filepath.Join(homeDir, ".claude"), filepath.Join(homeDir, ".claude", "skills", "arcmd", "SKILL.md")},
		{"Codex", filepath.Join(homeDir, ".codex"), filepath.Join(homeDir, ".codex", "skills", "arcmd", "SKILL.md")},
		{"Cursor", filepath.Join(homeDir, ".cursor"), filepath.Join(homeDir, ".cursor", "skills", "arcmd", "SKILL.md")},
		{"Antigravity", filepath.Join(homeDir, ".gemini", "config"), filepath.Join(homeDir, ".gemini", "config", "skills", "arcmd", "SKILL.md")},
		{"Agents", filepath.Join(homeDir, ".agents"), filepath.Join(homeDir, ".agents", "skills", "arcmd", "SKILL.md")},
	}
	out := make([]SkillTarget, 0, len(candidates))
	for _, c := range candidates {
		info, err := os.Stat(c.root)
		out = append(out, SkillTarget{
			Agent:     c.agent,
			RootDir:   c.root,
			SkillFile: c.skill,
			Detected:  err == nil && info.IsDir(),
		})
	}
	return out
}

// InstallSkills writes ArcMD's SKILL.md into every detected local agent skill
// directory under homeDir. When no agent directory exists yet, it installs to
// ~/.agents/skills/arcmd/SKILL.md as the universal fallback.
func InstallSkills(homeDir, binPath string) ([]string, error) {
	targets := DetectSkillTargets(homeDir)
	if len(targets) == 0 {
		return nil, fmt.Errorf("home directory is empty")
	}
	content := []byte(SkillMarkdown(binPath))
	var installed []string
	for _, t := range targets {
		if !t.Detected {
			continue
		}
		if err := fsutil.WriteFileAtomic(t.SkillFile, content, 0o644); err != nil {
			return installed, err
		}
		installed = append(installed, t.SkillFile)
	}
	if len(installed) == 0 {
		fallback := filepath.Join(homeDir, ".agents", "skills", "arcmd", "SKILL.md")
		if err := fsutil.WriteFileAtomic(fallback, content, 0o644); err != nil {
			return nil, err
		}
		installed = append(installed, fallback)
	}
	return installed, nil
}

// InstallSkillToDir writes SKILL.md inside targetDir (creating arcmd/SKILL.md
// unless targetDir is already named "arcmd" or ends with SKILL.md).
func InstallSkillToDir(targetDir, binPath string) (string, error) {
	targetDir = strings.TrimSpace(targetDir)
	if targetDir == "" {
		return "", fmt.Errorf("target directory is empty")
	}
	dest := targetDir
	switch {
	case strings.EqualFold(filepath.Base(dest), "SKILL.md"):
	case strings.EqualFold(filepath.Base(dest), "arcmd"):
		dest = filepath.Join(dest, "SKILL.md")
	default:
		dest = filepath.Join(dest, "arcmd", "SKILL.md")
	}
	if err := fsutil.WriteFileAtomic(dest, []byte(SkillMarkdown(binPath)), 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

// SkillMarkdown renders the complete SKILL.md file teaching local AI agents
// how to operate ArcMD Spaces, documents, rich annotations, whiteboards, and databases.
func SkillMarkdown(binPath string) string {
	cmd := strings.TrimSpace(binPath)
	if cmd == "" {
		cmd = "arcmd"
	} else if strings.ContainsAny(cmd, " \t") {
		cmd = `"` + cmd + `"`
	}
	var b strings.Builder
	b.WriteString(`---
name: arcmd
description: Operate the user's local ArcMD app, Spaces, rich Markdown notes, hand-drawn Excalidraw whiteboards (.excalidraw.svg), and multi-view databases (.db.md). Activate whenever the user mentions ArcMD, asks to manage Spaces, or wants to read, create, edit, highlight, annotate, draw whiteboards, or manage databases in ArcMD.
---

# Operating ArcMD

ArcMD is a native rich-document workspace. When the ArcMD desktop app is open, CLI and MCP commands automatically synchronize with the live window over local IPC (updating the sidebar, switching Spaces, preserving ` + "`⌘Z`" + ` undo on open documents, and avoiding file conflicts). When the desktop app is closed, the exact same commands operate safely on disk.

If the ` + "`arcmd`" + ` MCP server is connected in your environment, prefer its structured MCP tools (` + "`space_*`" + `, ` + "`doc_*`" + `, ` + "`whiteboard_write`" + `, ` + "`database_*`" + `). Otherwise run the CLI commands below (add ` + "`-json`" + ` whenever you want machine-readable JSON output).

## 1. Quick CLI Reference

### Status & Spaces
` + "```sh" + `
` + cmd + ` status -json
` + cmd + ` space list -json
` + cmd + ` space create -name "研发规范" -icon "🛠️" -color "#245BDB" -switch
` + cmd + ` space switch "研发规范"
` + cmd + ` space update -space "研发规范" -appearance dark -attachments "assets"
` + cmd + ` space remove "旧空间"   # Removes from ArcMD list only; physical folder stays untouched
` + "```" + `

### Documents (List, Search, Read, Create, Edit, Organize, Safe Trash)
` + "```sh" + `
` + cmd + ` doc list -space "研发规范" -json
` + cmd + ` doc search -space "研发规范" "延迟优化"
` + cmd + ` doc read -space "研发规范" -lines "架构设计.md"
` + cmd + ` doc create -space "研发规范" -title "Q4 规划" -icon "🎯" -cover "gradient:dawn" -open -stdin <<'EOF'
# Q4 规划

> [!IMPORTANT]
> <span data-emoji="🚀"></span>本季度核心目标是完成 <mark>本地 Agent 深度协同</mark>。
EOF

# Edit modes: replace | patch | append | prepend | frontmatter
` + cmd + ` doc edit -space "研发规范" -mode append -heading "## 待办" "Q4 规划.md" -stdin <<'EOF'
- [ ] 完成 <span style="border-bottom:1.5px solid #D83931" data-mark="circle" data-mark-color="red">MCP 与 CLI 双通道验证</span>
EOF

` + cmd + ` doc edit -space "研发规范" -mode patch -old "待确认" -new "已排期" "Q4 规划.md"
` + cmd + ` doc organize -space "研发规范" -op pin "Q4 规划.md"
` + cmd + ` doc organize -space "研发规范" -op move -to-dir "归档" "旧方案.md"
` + cmd + ` doc delete -space "研发规范" "草稿.md"     # Moves to OS Trash + 30-day ArcMD Recently Deleted backup
` + cmd + ` doc trash-list
` + cmd + ` doc restore -id "<trashID>"
` + "```" + `

### Hand-Drawn Excalidraw Whiteboards (` + "`.excalidraw.svg`" + `)
Never hand-craft base64 SVG metadata. Let ArcMD synthesize Rough hand-drawn strokes and connector bindings from Mermaid or declarative JSON, and optionally embed right into a document:
` + "```sh" + `
# 1) Convert Mermaid flowchart/sequenceDiagram into an editable hand-drawn whiteboard + embed into a doc:
` + cmd + ` board write -space "研发规范" -path "assets/agent-flow.excalidraw.svg" -embed "Q4 规划.md" -caption "Agent 调用链路" -mermaid <<'EOF'
flowchart LR
  A[本地 Agent] -->|MCP / CLI| B{ArcMD 控制层}
  B -->|实时 IPC| C([桌面端窗口])
  B -->|离线回退| D[(本地 Vault)]
EOF

# 2) Or pass declarative JSON elements (auto-sizes boxes and snaps "from" -> "to" arrows):
` + cmd + ` board write -space "研发规范" -path "路线图.excalidraw.svg" -elements-stdin <<'EOF'
[
  {"id": "n1", "type": "rectangle", "text": "需求调研", "color": "blue"},
  {"id": "n2", "type": "diamond", "text": "架构评审", "color": "orange"},
  {"id": "n3", "type": "stadium", "text": "正式发布", "color": "green"},
  {"id": "n4", "type": "note", "text": "支持 ⌘Z 撤销与深浅纸面自适应"},
  {"type": "arrow", "from": "n1", "to": "n2", "text": "输出方案"},
  {"type": "arrow", "from": "n2", "to": "n3", "text": "通过", "color": "green"}
]
EOF
` + "```" + `

### Amphibious Multi-View Databases (` + "`.db.md`" + `)
Always use ` + "`arcmd db`" + ` (or ` + "`database_schema`" + ` / ` + "`database_records`" + `) so schema JSON, formula/rollup evaluation, backing ` + "`.md`" + ` frontmatter, and GFM snapshot tables stay in sync:
` + "```sh" + `
# Create a database with typed properties, views, and initial records from JSON:
` + cmd + ` db create -space "研发规范" -path "任务看板.db.md" -spec-stdin <<'EOF'
{
  "title": "任务看板",
  "icon": "📋",
  "sourceType": "folder",
  "properties": [
    {"name": "状态", "type": "status"},
    {"name": "优先级", "type": "select", "options": [{"name": "P0", "color": "red"}, {"name": "P1", "color": "orange"}, {"name": "P2", "color": "blue"}]},
    {"name": "负责人", "type": "person"},
    {"name": "截止日期", "type": "date"},
    {"name": "进度", "type": "number", "number": {"format": "percent", "showAs": "bar", "barColor": "green"}},
    {"name": "健康度", "type": "formula", "formula": {"expression": "if(prop(\"进度\") >= 1, \"✅ 完成\", \"🚀 推进中\")"}}
  ],
  "views": [
    {"name": "全部任务", "type": "table"},
    {"name": "状态看板", "type": "board", "groupBy": "状态"},
    {"name": "排期甘特", "type": "timeline", "startProp": "截止日期"}
  ],
  "records": [
    {"title": "接入 MCP Server", "fields": {"状态": "进行中", "优先级": "P0", "进度": 0.8, "截止日期": "2026-12-01"}, "body": "# 接入 MCP Server\n\n支持 stdio JSON-RPC 调用。"}
  ]
}
EOF

` + cmd + ` db query -space "研发规范" -db "任务看板.db.md" -json
` + cmd + ` db add-record -space "研发规范" -db "任务看板.db.md" -title "补充单元测试" -fields '{"状态":"未开始","优先级":"P1","进度":0.2}'
` + cmd + ` db update-record -space "研发规范" -db "任务看板.db.md" -record "接入 MCP Server" -fields '{"进度":1}'
` + cmd + ` db embed -space "研发规范" -db "任务看板.db.md" -view "状态看板" -doc "Q4 规划.md"
` + "```" + `

---

`)
	b.WriteString(FormatGuide("all"))
	b.WriteByte('\n')
	return b.String()
}
