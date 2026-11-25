// Package agentmcp provides ArcMD's rich format specification, local agent
// SKILL.md installer, and stdio Model Context Protocol (MCP) server.
// It depends only on the Go standard library and fsutil so both the UI layer
// and headless CLI commands can use it without circular imports.
package agentmcp

import "strings"

// FormatTopics lists the sections available in FormatGuide.
var FormatTopics = []string{"all", "frontmatter", "inline", "blocks", "tables", "whiteboard", "database"}

// FormatGuide returns ArcMD's authoritative Markdown & amphibious format
// reference for local AI agents. Topic may be "", "all", or one of FormatTopics.
func FormatGuide(topic string) string {
	switch strings.ToLower(strings.TrimSpace(topic)) {
	case "frontmatter", "meta", "metadata":
		return sectionHeader + "\n\n" + sectionFrontmatter
	case "inline", "marks", "annotations", "highlight":
		return sectionHeader + "\n\n" + sectionInline
	case "blocks", "callout", "columns", "toggle", "math", "mermaid":
		return sectionHeader + "\n\n" + sectionBlocks
	case "table", "tables":
		return sectionHeader + "\n\n" + sectionTables
	case "whiteboard", "board", "excalidraw":
		return sectionHeader + "\n\n" + sectionWhiteboard
	case "database", "db":
		return sectionHeader + "\n\n" + sectionDatabase
	default:
		return strings.Join([]string{
			sectionHeader,
			sectionFrontmatter,
			sectionInline,
			sectionBlocks,
			sectionTables,
			sectionWhiteboard,
			sectionDatabase,
		}, "\n\n")
	}
}

const sectionHeader = `# ArcMD Rich Format Specification

ArcMD stores documents directly as standard UTF-8 Markdown (.md) files with lossless semantic HTML, YAML frontmatter, amphibious Excalidraw whiteboards (.excalidraw.svg), and amphibious multi-view databases (.db.md). Every construct below degrades gracefully in GitHub/Obsidian/VS Code while rendering as native GPU widgets in ArcMD.`

const sectionFrontmatter = `## 1. Page Metadata (YAML Frontmatter)

Place optional YAML frontmatter at the very top of a .md file. ArcMD recognizes four page chrome keys and preserves all custom YAML keys (including database record properties):

` + "```yaml" + `
---
icon: "🚀"
cover: "gradient:dawn"
coverY: 0
goal: 1500
status: 进行中
tags: [架构, Q4]
---
` + "```" + `

- **icon**: Single emoji displayed above the H1 page title and in the sidebar.
- **cover**: Built-in gradient (` + "`gradient:dawn`" + `, ` + "`gradient:sea`" + `, ` + "`gradient:forest`" + `, ` + "`gradient:dusk`" + `, ` + "`gradient:sand`" + `, ` + "`gradient:ink`" + `) or a document-relative image path (` + "`assets/banner.png`" + `).
- **coverY**: Vertical crop offset for image covers (` + "`-1`" + ` top, ` + "`0`" + ` center, ` + "`1`" + ` bottom).
- **goal**: Target word/character count (positive integer) shown as a progress bar.`

const sectionInline = `## 2. Inline Formatting, Highlights & Hand-Drawn Annotations

Standard Markdown inline syntax works everywhere (` + "`**bold**`" + `, ` + "`*italic*`" + `, ` + "`~~strike~~`" + `, ` + "`` `code` ``" + `, ` + "`[text](url)`" + `). ArcMD extends inline prose with:

### 2.1 Text Colors & Background Highlights
- **Default yellow highlight**: ` + "`<mark>重点内容</mark>`" + `
- **Text color** (` + "`gray`" + `, ` + "`red`" + `, ` + "`orange`" + `, ` + "`yellow`" + `, ` + "`green`" + `, ` + "`blue`" + `, ` + "`purple`" + `):
  ` + "`<span style=\"color:#D83931\" data-color=\"red\">红色重点</span>`" + `
- **Background highlight** (` + "`light-gray`" + `, ` + "`light-red`" + `, ` + "`light-orange`" + `, ` + "`light-yellow`" + `, ` + "`light-green`" + `, ` + "`light-blue`" + `, ` + "`light-purple`" + `, ` + "`medium-gray`" + `, ` + "`gray`" + `, ` + "`red`" + `, ` + "`orange`" + `, ` + "`yellow`" + `, ` + "`green`" + `, ` + "`blue`" + `, ` + "`purple`" + `):
  ` + "`<span style=\"background-color:#D8E4FF\" data-highlight=\"light-blue\">浅蓝高亮</span>`" + `
- **Combined color + highlight**:
  ` + "`<span style=\"color:#245BDB;background-color:#D8E4FF\" data-color=\"blue\" data-highlight=\"light-blue\">蓝字浅蓝底</span>`" + `

Palette hex reference (for CSS fallback in ` + "`style=\"...\"`" + `):
- Text colors: ` + "`gray:#8F959E`" + `, ` + "`red:#D83931`" + `, ` + "`orange:#DE7802`" + `, ` + "`yellow:#DC9B04`" + `, ` + "`green:#2EA121`" + `, ` + "`blue:#245BDB`" + `, ` + "`purple:#6425D0`" + `
- Light highlights: ` + "`light-gray:#F2F3F5`" + `, ` + "`light-red:#FBBFBC`" + `, ` + "`light-orange:#FFE4C4`" + `, ` + "`light-yellow:#FFF67A`" + `, ` + "`light-green:#CCF3C8`" + `, ` + "`light-blue:#D8E4FF`" + `, ` + "`light-purple:#E2D2FC`" + `

### 2.2 Hand-Drawn Annotations (12 Styles)
ArcMD natively draws Rough/SVG animated ink strokes over or under text via ` + "`data-mark`" + ` and optional ` + "`data-mark-color`" + ` (` + "`gray|red|orange|yellow|green|blue|purple`" + `; omit ` + "`data-mark-color`" + ` to use each mark's default ink):

- Styles (` + "`data-mark`" + `):
  - ` + "`wavy`" + ` (波浪线), ` + "`circle`" + ` (手绘圈出), ` + "`box`" + ` (手绘方框), ` + "`bracket`" + ` (手绘括号)
  - ` + "`highlight`" + ` (手绘荧光笔刷), ` + "`underline`" + ` (手绘下划线), ` + "`line`" + ` (直尺线), ` + "`dotted`" + ` (点状下划线), ` + "`double`" + ` (双下划线)
  - ` + "`strike`" + ` (手绘删除线), ` + "`cross`" + ` (手绘打叉), ` + "`arrow`" + ` (底部手绘箭头)
- Examples:
  - Circle in red: ` + "`<span style=\"border-bottom:1.5px solid #D83931\" data-mark=\"circle\" data-mark-color=\"red\">核心指标</span>`" + `
  - Wavy in blue: ` + "`<span style=\"text-decoration:underline wavy #245BDB\" data-mark=\"wavy\" data-mark-color=\"blue\">待确认假设</span>`" + `
  - Hand-drawn marker in green: ` + "`<span style=\"background-color:#CCF3C8\" data-mark=\"highlight\" data-mark-color=\"green\">已验证通过</span>`" + `
  - Box in orange: ` + "`<span style=\"border-bottom:1.5px solid #DE7802\" data-mark=\"box\" data-mark-color=\"orange\">里程碑节点</span>`" + `

### 2.3 Keycaps, Sub/Superscript, Math, Wikilinks, Tags & Footnotes
- **Keyboard keycap**: ` + "`<kbd>⌘K</kbd>`" + `, ` + "`<kbd>Shift</kbd>`" + `
- **Underline / Super / Subscript**: ` + "`<u>下划线</u>`" + `, ` + "`x<sup>2</sup>`" + `, ` + "`H<sub>2</sub>O`" + `
- **Inline LaTeX math**: ` + "`$\\alpha + \\beta = \\gamma$`" + ` (use ` + "`\\$`" + ` for literal dollar sign)
- **Wikilinks**: ` + "`[[目标文档]]`" + `, ` + "`[[目标文档|显示文字]]`" + `, ` + "`[[目标文档#二级标题]]`" + `
- **Tags**: ` + "`#产品`" + `, ` + "`#研发/后端`" + ` (preceded by start-of-line, whitespace, or punctuation)
- **Footnotes**: Inline ` + "`[^1]`" + ` paired with a bottom definition ` + "`[^1]: 脚注详细解释`" + `
- **Hidden inline comment**: ` + "`正文<!-- 审阅备注，阅读态隐藏但无损保留 -->后续`" + ``

const sectionBlocks = `## 3. Block-Level Rich Components

### 3.1 Callout / Alert Blocks (5 Semantic Colors + Custom Emoji)
Mapped to GitHub Alerts:
- ` + "`> [!NOTE]`" + ` (Blue / 说明)
- ` + "`> [!TIP]`" + ` (Green / 提示)
- ` + "`> [!IMPORTANT]`" + ` (Purple / 重要)
- ` + "`> [!WARNING]`" + ` (Yellow / 警告)
- ` + "`> [!CAUTION]`" + ` (Red / 危险)

Optional custom emoji icon via leading ` + "`<span data-emoji=\"...\"></span>`" + `:
` + "```markdown\n> [!TIP]\n> <span data-emoji=\"💡\"></span>**架构建议**：优先复用现有的双栖 `.db.md` 快照格式，保证 Git diff 可读。\n```" + `

### 3.2 Toggle / Disclosure Blocks (` + "`<details>`" + `)
Use ` + "`<details open>`" + ` for initially expanded or ` + "`<details>`" + ` for initially collapsed. Leave blank lines around inner Markdown blocks:
` + "```markdown\n<details open>\n<summary>展开查看压测数据</summary>\n\n- P99 延迟：`12.4 ms`\n- 吞吐量：`18,500 req/s`\n\n</details>\n```" + `

### 3.3 Multi-Column Layouts (2–5 Columns)
Wrap 2 to 5 ` + "`<div class=\"arcmd-column\">`" + ` blocks inside ` + "`<div class=\"arcmd-columns\">`" + `. Optional ` + "`data-widths=\"60,40\"`" + ` sets custom width percentages:
` + "```markdown" + `
<div class="arcmd-columns" data-widths="55,45" style="display:flex;gap:24px">

<div class="arcmd-column" style="flex:55;min-width:0">

### 方案优势

- 纯本地 Markdown 存储，零云端锁定
- GPU 原生排版，启动帧率稳定 120 Hz

</div>

<div class="arcmd-column" style="flex:45;min-width:0">

> [!IMPORTANT]
> 所有跨目录移动都会自动改写双链与附件路径。

</div>

</div>
` + "```" + `

### 3.4 Block Math & Native Vector Mermaid Diagrams
- **Display LaTeX block**:
` + "```markdown" + `
$$
\mathcal{L}(\theta) = \frac{1}{N}\sum_{i=1}^{N} \left\| f_\theta(x_i) - y_i \right\|_2^2
$$
` + "```" + `
- **Native Mermaid** (` + "`flowchart`" + ` / ` + "`graph`" + ` and ` + "`sequenceDiagram`" + ` are rendered natively as crisp vector graphics):
` + "````markdown\n```mermaid\nflowchart LR\n  A[本地 Agent] -->|MCP / CLI| B{ArcMD 控制层}\n  B -->|窗口运行中| C([实时 IPC 刷新 UI])\n  B -->|无窗口| D[(原子写盘与索引同步)]\n```\n````" + `

### 3.5 Alignment, Indentation & Rich Media Embeds
- **Aligned / indented paragraph or heading**:
  ` + "`<p align=\"center\">居中副标题</p>`" + ` or ` + "`<p style=\"margin-left:24px\" data-indent=\"1\">缩进一段</p>`" + `
- **Images**: ` + "`![图片说明](assets/arch.png)`" + `, resized ` + "`<img src=\"assets/arch.png\" alt=\"图片说明\" width=\"520\">`" + `, or Obsidian style ` + "`![[arch.png|520]]`" + `
- **PDF & EPUB / HTML embeds**:
  - ` + "`![设计规范](assets/spec.pdf#page=2)`" + `
  - ` + "`![技术手册](assets/manual.epub#chapter=3)`" + ``

const sectionTables = `## 4. Tables (Standard Pipe Tables & Rich HTML Tables)

1. **Simple tables**: Use standard GFM pipe tables (` + "`| 列1 | 列2 |`" + ` with ` + "`:---`" + `, ` + "`:---:`" + `, ` + "`---:`" + `).
2. **Rich tables** (custom column widths, ` + "`colspan`" + ` / ` + "`rowspan`" + ` merged cells, and 16 cell background colors via ` + "`data-bg`" + `):

` + "```html" + `
<table>
<colgroup><col width="180"><col width="260"><col width="160"></colgroup>
<thead>
<tr><th style="background-color:#F0F4FF" data-bg="light-blue">模块</th><th style="background-color:#F0F4FF" data-bg="light-blue">职责说明</th><th align="center" style="background-color:#F0F4FF" data-bg="light-blue">状态</th></tr>
</thead>
<tbody>
<tr><td rowspan="2"><strong>存储引擎</strong></td><td>Markdown 双栖无损编解码</td><td align="center" style="background-color:#F0FBEF" data-bg="light-green">已完成</td></tr>
<tr><td>跨空间附件与双链重写</td><td align="center" style="background-color:#F0FBEF" data-bg="light-green">已完成</td></tr>
<tr><td colspan="2" align="right"><em>整体进度</em></td><td align="center" style="background-color:#FEFFF0" data-bg="light-yellow"><strong>100%</strong></td></tr>
</tbody>
</table>
` + "```" + `

Allowed ` + "`data-bg`" + ` names (same 15 names as ` + "`data-highlight`" + `):
` + "`light-gray`" + ` (` + "`#F5F6F7`" + `), ` + "`light-red`" + ` (` + "`#FEF1F1`" + `), ` + "`light-orange`" + ` (` + "`#FFF5EB`" + `), ` + "`light-yellow`" + ` (` + "`#FEFFF0`" + `), ` + "`light-green`" + ` (` + "`#F0FBEF`" + `), ` + "`light-blue`" + ` (` + "`#F0F4FF`" + `), ` + "`light-purple`" + ` (` + "`#F6F1FE`" + `), ` + "`medium-gray`" + ` (` + "`#F2F3F5`" + `), ` + "`gray`" + ` (` + "`#EFF0F1`" + `), ` + "`red`" + ` (` + "`#FDE2E2`" + `), ` + "`orange`" + ` (` + "`#FEEAD2`" + `), ` + "`yellow`" + ` (` + "`#FFFFCC`" + `), ` + "`green`" + ` (` + "`#D9F5D6`" + `), ` + "`blue`" + ` (` + "`#E1EAFF`" + `), ` + "`purple`" + ` (` + "`#ECE2FE`" + `).`

const sectionWhiteboard = `## 5. Hand-Drawn Excalidraw Whiteboards (.excalidraw.svg)

ArcMD whiteboards are amphibious ` + "`.excalidraw.svg`" + ` files: valid standalone vector SVGs (viewable in GitHub/Typora/browsers) carrying a lossless Excalidraw v2 JSON payload in ` + "`<metadata>`" + ` so ArcMD opens and edits every shape interactively.

- **Standalone whiteboard file**: ` + "`Diagrams/系统架构.excalidraw.svg`" + `
- **Inline embed inside a .md document**: ` + "`![系统架构](assets/arch.excalidraw.svg)`" + `

> **Best Practice for Agents**: Do NOT hand-encode base64 SVG metadata. Always call ` + "`whiteboard_write`" + ` (MCP) or ` + "`arcmd board write`" + ` (CLI), which generates Rough hand-drawn SVG paths, computes geometry bindings, writes the ` + "`.excalidraw.svg`" + ` file, and optionally embeds it into a Markdown document!

Two authoring inputs are supported by ` + "`whiteboard_write`" + ` / ` + "`arcmd board write`" + `:
1. **Mermaid-to-Whiteboard** (` + "`mermaid`" + `): Pass any ` + "`flowchart`" + ` / ` + "`graph`" + ` or ` + "`sequenceDiagram`" + `; ArcMD automatically lays out hand-drawn boxes, diamonds, cylinders, subgraphs (frames), sticky notes, and bound arrows.
2. **Declarative Elements JSON** (` + "`elements`" + `):
   - **Element types**: ` + "`rectangle`" + `, ` + "`diamond`" + `, ` + "`ellipse`" + `, ` + "`stadium`" + `, ` + "`cylinder`" + `, ` + "`hexagon`" + `, ` + "`parallelogram`" + `, ` + "`triangle`" + `, ` + "`cloud`" + `, ` + "`star`" + `, ` + "`callout`" + `, ` + "`document`" + `, ` + "`actor`" + `, ` + "`note`" + ` (sticky note), ` + "`frame`" + `, ` + "`text`" + `, ` + "`arrow`" + `, ` + "`line`" + `.
   - **Semantic colors** (auto-adapt to light/dark paper): ` + "`ink`" + `, ` + "`gray`" + `, ` + "`red`" + `, ` + "`orange`" + `, ` + "`green`" + `, ` + "`blue`" + `, ` + "`purple`" + `.
   - **Fill styles**: ` + "`hachure`" + ` (hand-drawn diagonal lines, default), ` + "`cross-hatch`" + `, ` + "`solid`" + `, ` + "`none`" + `.
   - **Connector binding**: On ` + "`arrow`" + ` / ` + "`line`" + `, set ` + "`\"from\": \"nodeA\"`" + ` and ` + "`\"to\": \"nodeB\"`" + ` (referencing element ` + "`id`" + `s) plus optional ` + "`\"text\": \"调用\"`" + `; ArcMD automatically snaps endpoints to shape perimeters!`

const sectionDatabase = `## 6. Amphibious Multi-View Databases (.db.md)

ArcMD databases are ` + "`.db.md`" + ` files containing a schema comment (` + "`<!-- arcmd-db:schema ... -->`" + `) and a live GFM table snapshot (` + "`<!-- arcmd-db:snapshot:start -->`" + ` ... ` + "`<!-- arcmd-db:snapshot:end -->`" + `).

> **Best Practice for Agents**: Always use ` + "`database_schema`" + ` and ` + "`database_records`" + ` (MCP) or ` + "`arcmd db ...`" + ` (CLI) to create databases, manage columns/views, add/update/delete rows, and embed Linked Views. ArcMD automatically validates IDs, syncs backing ` + "`.md`" + ` record frontmatter, evaluates Formula 2.0 / Rollups, and re-renders the GFM snapshot table!

- **Record Sources** (` + "`sourceType`" + `):
  - ` + "`folder`" + ` (default): Each row is a full Markdown note (` + "`.md`" + `) inside a folder, with properties synced two-way in YAML frontmatter!
  - ` + "`inline`" + `: Lightweight rows stored directly inside the ` + "`.db.md`" + ` file without separate ` + "`.md`" + ` files.
  - ` + "`space`" + `: Indexes Markdown notes across the Space (optionally filtered by ` + "`tagFilter`" + `).
- **18 Property Types**:
  ` + "`title`" + `, ` + "`text`" + `, ` + "`number`" + ` (supports ` + "`percent`" + `, ` + "`yuan`" + `, ` + "`dollar`" + `, ` + "`bar`" + ` / ` + "`ring`" + ` progress), ` + "`select`" + `, ` + "`multi_select`" + `, ` + "`status`" + ` (` + "`todo`" + ` / ` + "`in_progress`" + ` / ` + "`complete`" + ` groups), ` + "`date`" + ` (single date or start/end range), ` + "`checkbox`" + `, ` + "`url`" + `, ` + "`email`" + `, ` + "`phone`" + `, ` + "`files`" + `, ` + "`person`" + `, ` + "`created_time`" + `, ` + "`edited_time`" + `, ` + "`unique_id`" + ` (e.g. ` + "`TASK-1`" + `), ` + "`relation`" + `, ` + "`rollup`" + `, ` + "`formula`" + ` (Formula 2.0, e.g. ` + "`if(prop(\"完成度\") >= 100, \"✅ 已完成\", \"🚀 推进中\")`" + `).
- **9 Native Views**:
  ` + "`table`" + `, ` + "`board`" + ` (Kanban), ` + "`timeline`" + ` (Gantt), ` + "`calendar`" + `, ` + "`list`" + `, ` + "`gallery`" + `, ` + "`chart`" + ` (` + "`bar`" + ` / ` + "`hbar`" + ` / ` + "`line`" + ` / ` + "`donut`" + `), ` + "`feed`" + `, ` + "`form`" + `.
- **Embedded Linked View in any .md document** (via ` + "`database_schema`" + ` ` + "`embed_view`" + ` or ` + "`arcmd db embed`" + `):
  ` + "`<!-- arcmd-db:view {\"target\":\"Tasks.db.md\",\"view\":\"v_board\",\"height\":380} -->`" + ` ... ` + "`<!-- /arcmd-db:view -->`" + ``
