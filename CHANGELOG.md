# Changelog

## 0.2.5

This release adds **Sidebar Copy, Paste, and Duplicate** for documents, folders, and multi-selections ([#10](https://github.com/Ciao1019/ArcMD/issues/10), [#39](https://github.com/Ciao1019/ArcMD/pull/39)), **streaming Responses & Chat Completions protocol support** for Space Q&A ([#23](https://github.com/Ciao1019/ArcMD/pull/23)), **crisp math formula rendering, content-aware expanding tables, narrow-window sidebar hysteresis, and an optional Autosave toggle** ([#32](https://github.com/Ciao1019/ArcMD/issues/32)), plus targeted fixes for table row selection crashes ([#34](https://github.com/Ciao1019/ArcMD/issues/34), [#35](https://github.com/Ciao1019/ArcMD/pull/35)) and literal plain-text clipboard copying.

### Sidebar, Space Q&A, and Writing Workflow

- **Sidebar Copy, Paste, and Duplicate (`internal/vault/copy.go`, `internal/app/copy.go`, `internal/app/sidebar.go`)**: Right-click or use `⌘C` / `⌘V` (`Ctrl+C` / `Ctrl+V`) in the sidebar to copy, paste, or duplicate documents, nested folders, multi-selections, and favorite tiles within or across Spaces. Duplicate enters inline rename and opens the copy focused in the body; cross-folder and cross-Space pastes preserve or rebase attachments and covers safely with one-click Undo via Trash ([#10](https://github.com/Ciao1019/ArcMD/issues/10), [#39](https://github.com/Ciao1019/ArcMD/pull/39), thanks to `@tajiaoyezi`).
- **Streaming Responses and Chat Completions for Space Q&A (`internal/agent/responses.go`, `internal/app/qasettings.go`, `cmd/arcmd/ask.go`)**: Choose **Automatic**, **Chat Completions**, or **Responses** in Settings → AI Q&A or via `arcmd ask -protocol`. Automatic selects stateless streaming Responses for recognized reasoning models on OpenAI and OpenRouter while preserving pending tool-call results and encrypted reasoning replay ([#23](https://github.com/Ciao1019/ArcMD/pull/23), thanks to `@hyskr`).
- **Optional Autosave toggle with unsaved-changes guard (`internal/prefs/prefs.go`, `internal/app/unsaved.go`, `internal/app/settings.go`)**: Toggle background autosave in Settings → General → Writing or the command bar (`⌘K`). When disabled, edits remain in memory until explicitly saved (`⌘S`), and leaving or closing modified documents prompts **Save / Don't Save / Cancel** ([#32](https://github.com/Ciao1019/ArcMD/issues/32)).

### Math Rendering, Tables, and Clipboard Fixes

- **Crisp scaled math, wrapping equations, and content-aware tables (`internal/editor/math.go`, `internal/tex/layout.go`, `internal/editor/virtualize.go`, `internal/app/app.go`)**: Paint inline and display formulas directly at their target font size instead of raster-scaling pre-shaped glyphs, wrap long inline equations before shrinking, size table columns by natural content width and let wide formula tables expand past the standard prose column as the window widens, and add widening hysteresis so dragging a narrow window wider does not prematurely unfold the sidebar and squeeze the document ([#32](https://github.com/Ciao1019/ArcMD/issues/32)).
- **Fix table row selection panic (`internal/editor/table.go`)**: Guard column-index access in the table toolbar so selecting a row whose index exceeds the column count no longer triggers an out-of-range panic ([#34](https://github.com/Ciao1019/ArcMD/issues/34), [#35](https://github.com/Ciao1019/ArcMD/pull/35), thanks to `@Zzzt2003`).
- **Literal plain-text copy and cleaner Markdown escaping (`internal/editor/clipboard.go`, `internal/editor/markdown.go`)**: Copy literal characters without Markdown backslash escapes, backticks, or `<p align=...>` wrappers from table cells, inline code, and prose, and leave intraword underscores unescaped when exporting Markdown.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates.

## 0.2.4

This patch release fixes unbounded memory accumulation when switching repeatedly across Markdown documents, sidebar hover previews, and Spaces ([#33](https://github.com/Ciao1019/ArcMD/issues/33)).

### Memory and Resource Lifecycle Fixes

- **Reclaim closed documents and decoded image bitmaps on switch (`internal/app/app.go`, `internal/app/document.go`, `internal/app/opendocs.go`, `internal/app/split.go`, `internal/app/windows.go`, `internal/editor/images.go`)**: Retire closed `*document` instances and evict non-resident entries from `imgResolved` and `search.liveDocs` whenever switching notes, closing split panes or secondary windows, reloading, or unpinning tabs so closed editors and multi-megabyte RGBA `*ui.Bitmap` buffers are immediately eligible for garbage collection ([#33](https://github.com/Ciao1019/ArcMD/issues/33)).
- **Bounded async image decode concurrency and CPU pixel release (`internal/app/document.go`, `internal/app/view.go`)**: Cap concurrent background image decoders to 4 goroutines, abort queued or in-flight decodes as soon as a superseded document closes, and call `Bitmap.ReleasePixels()` after GPU texture upload on live windows so uncompressed RGBA/mipmap buffers do not remain duplicated in system RAM ([#33](https://github.com/Ciao1019/ArcMD/issues/33)).
- **Bounded sidebar thumbnail LRU cache and scaled preview decoding (`internal/app/preview.go`, `internal/app/document.go`, `internal/app/links.go`)**: Bound `preview.thumbs` to an 8-entry LRU cache, drop transient `open/...` clones when previews close, decode sidebar thumbnail images via `ui.DecodeBitmapScaled(data, 640)` instead of full-resolution bitmaps, and read `[[wiki-link]]` hover previews directly from the search index / text extractor without allocating thumbnail editors ([#33](https://github.com/Ciao1019/ArcMD/issues/33)).
- **Active Space session slot, whiteboard cache, and modal cleanup (`internal/app/app.go`, `internal/app/whiteboard.go`, `internal/app/versions.go`, `internal/app/gitmerge.go`)**: Clear the active Space's suspended `session.doc` reference upon returning to a Space, bound `wbCache` to 16 entries, and release retained document/diff state when the Version History, Git Changes, or 3-Way Merge modals close ([#33](https://github.com/Ciao1019/ArcMD/issues/33)).

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates.

## 0.2.3

This release introduces a pure-Go WebAssembly plugin sandbox (`wazero`) with a bundled official **Cloud Sync** plugin (S3 SigV4 & WebDAV incremental bidirectional sync), an IDEA-style **3-Way Merge** and Git diff view, a navigable **Document Mind Map**, per-Space **Attachments Folders** with automatic relinking, native **`<kbd>` keycaps** and raw HTML / comment rendering, **Sidebar Multi-Select** batch file management, and targeted fixes across Mermaid sequence diagrams, TeX `\tag` numbering, Windows hardlinks, inline rename contrast, and macOS window lifecycle.

### Plugins, Cloud Sync, and 3-Way Merge

- **Pure-Go WASM plugin sandbox (`internal/plugin`, `internal/app/plugins.go`)**: Execute plugins safely inside a zero-cgo `wazero` WebAssembly sandbox with explicit permission confirmation (`document:read/write`, `space:read/write`, `network`, `secrets`, `ui:panel`), isolated local secret storage (`credentials/plugin-<id>-<key>`), declarative native settings forms, command-bar integration, and local `.zip` package installation.
- **Official Cloud Sync plugin (`plugins/cloud-sync`, `dev.arcmd.cloud-sync`)**: Built-in one-click installable WASM plugin providing three-way incremental bidirectional sync for Space documents and attachments over **S3** (AWS S3, Cloudflare R2, Alibaba Cloud OSS, Tencent Cloud COS, Backblaze B2, MinIO) and **WebDAV** (Jianguoyun, Nextcloud, Alist/OpenList), with 15-second post-save debounced sync and periodic background auto-sync.
- **Git-style Changes view and IDEA 3-Way Merge (`internal/merge`, `internal/app/gitmerge.go`)**: Inspect modified documents and sync conflicts as collapsible unified Git diff hunks with grapheme-level inline highlights, and resolve concurrent edits or cloud conflict copies in a three-pane **Yours / Result / Theirs** merge view with one-click connector arrows, auto-merge for non-conflicting hunks, and inline result fine-tuning.

### Document Mind Map, Raw HTML & `<kbd>`, and Space Management

- **Navigable document mind map (`internal/mindmap`, `internal/editor/mindmap.go`)**: Toggle a live, read-only mind map of any document (`⌥⌘M` / `Ctrl+Alt+M`, toolbar, or command bar) with single-sided and balanced two-sided tree layouts, smooth branch bezier curves, rich TeX/code/table node previews, viewport culling, zoom/pan controls, keyboard navigation, and click-to-jump editor synchronization ([#26](https://github.com/Ciao1019/ArcMD/pull/26), thanks to `@hyskr`).
- **Per-Space attachments folder and automatic relinking (`internal/app/attachmentdir.go`, `internal/space/space.go`)**: Configure a dedicated Space-wide attachments directory (`spaces.json` v10) from the Space menu or command bar, hide the attachments folder from the sidebar note tree, recalculate relative paths without duplicating files when moving notes within a Space, and automatically relink open and closed Markdown notes in the background when attachments are moved or renamed ([#27](https://github.com/Ciao1019/ArcMD/pull/27), [#25](https://github.com/Ciao1019/ArcMD/issues/25), thanks to `@Frieren924`).
- **Native `<kbd>` keycaps, raw HTML rendering, and HTML comments (`internal/document`, `internal/editor/mdstyle.go`, `internal/editor/markdown.go`, `internal/editor/livecompile.go`, `internal/editor/view.go`, `internal/editor/vectorpdf.go`)**: Render 3D rounded `<kbd>` keycaps across light/dark themes and vector PDF export (`Style.Kbd`, document format **9**), compile `<kbd>…</kbd>` and semantic inline HTML tags (`<u>`, `<ins>`, `<sup>`, `<sub>`, `<mark>`, `<br>`, `<q>`, `<var>`, `<dfn>`, `<cite>`, `<small>`, `<abbr>`, `<samp>`, `<tt>`, `<font color>`) live as you type with single-Backspace rollback, support `<div align="center|right">`, `<center>`, and `<hr>` blocks, and preserve both inline and standalone `<!-- ... -->` HTML comments losslessly while excluding hidden comments from HTML/PDF export, word counts, `#tags`, `[[wiki-links]]`, and mind maps ([#28](https://github.com/Ciao1019/ArcMD/pull/28), [#20](https://github.com/Ciao1019/ArcMD/issues/20), thanks to `@tajiaoyezi`).
- **Sidebar multi-select and batch file actions (`internal/app/multiselect.go`, `internal/app/treedrag.go`, `internal/app/crossspace.go`, `internal/app/palette.go`)**: Select multiple documents and folders with `⌘`-click (`Ctrl`-click on Windows/Linux) and `⇧`-click range selection, drag stacked selection cards with item-count pills to reorder, pin/unpin, move into folders, move across Spaces, add to Favorites, or open 2–4 selected notes in Split View, create a new folder containing the selected items, and undo batch moves or deletions in one click ([#21](https://github.com/Ciao1019/ArcMD/issues/21)).

### Mermaid, Math, Editor, and Platform Fixes

- **Full-width Mermaid sequence diagrams, `rect` shading, and vector zoom lightbox (`internal/mermaid/sequence.go`, `internal/editor/mermaid.go`)**: Lay out multi-phase `sequenceDiagram` blocks naturally across the text column without height-clamping shrinkage, render `rect rgb(...)` / `rgba(...)` phase background shading, and add a hover **Zoom** button (or double-click) opening a lossless vector zoom-and-pan diagram viewer ([#29](https://github.com/Ciao1019/ArcMD/issues/29)).
- **Display math `\tag` / `\tag*` equation numbering (`internal/tex`, `internal/editor/math.go`, `internal/editor/vectorpdf.go`)**: Parse and render right-aligned `\tag{...}` and unparenthesized `\tag*{...}` equation labels across single-line and multi-line display math blocks and vector PDF export ([#31](https://github.com/Ciao1019/ArcMD/issues/31)).
- **Preserve Windows and Unix hardlinks on save (`internal/fsutil`)**: Detect regular files with multiple hardlinks (`LinkCount > 1`) in `WriteFileAtomic` and fall back to in-place overwrite and truncation so saving hardlinked notes on Windows and Unix never breaks existing links ([#31](https://github.com/Ciao1019/ArcMD/issues/31)).
- **Readable inline rename contrast on selected items (`internal/app/sidebar.go`, `internal/app/favorites.go`)**: Clear selected-row light text overrides while renaming inline so document and folder names remain high-contrast and legible in both light and dark themes ([#30](https://github.com/Ciao1019/ArcMD/issues/30)).
- **Link boundary typing and macOS `⌘W` Dock lifecycle (`internal/document/engine.go`, `internal/editor/linkpopover.go`, `internal/app/app.go`)**: Avoid opening the link popover or extending link marks when clicking blank space after a link or typing at link start/end boundaries, and hide the main window on macOS `⌘W` / close button so ArcMD stays running in the Dock until `⌘Q` (restorable via Dock click or `⌘0`).

### Contributors

Special thanks to **`@hyskr`** ([#26](https://github.com/Ciao1019/ArcMD/pull/26)), **`@Frieren924`** ([#27](https://github.com/Ciao1019/ArcMD/pull/27)), and **`@tajiaoyezi`** ([#28](https://github.com/Ciao1019/ArcMD/pull/28)) for their merged pull requests in this release!

### Documentation

- Update `README.md`, `README.en.md`, `AGENTS.md`, `docs/architecture.md`, `docs/plugins.md`, and `tools/dev` for version `0.2.3`, WASM plugins and Cloud Sync, document mind maps, per-Space attachments folders, `<kbd>` / raw HTML support, and sidebar multi-selection.

### Data compatibility

Markdown (`.md`, `.markdown`, `.mdx`) remains the primary in-place format. Legacy `.arcmd` JSON advances to format **9** (adding optional `Style.Kbd` and `Block.Comments`, with automatic migration from formats **1–8**), and Space configuration (`spaces.json`) advances to format **10** (adding optional per-Space `attachmentsDir`, with automatic migration from earlier formats). Plugin configuration and state live under `plugins/`, while plugin secrets are stored separately in `credentials/` (`0600`).

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates. The pure-Go WASM plugin sandbox (`wazero`), local `.zip` plugin installer, and bundled official Cloud Sync plugin (`dev.arcmd.cloud-sync`) are included; the remote plugin marketplace index and `process` runtime remain in development.

## 0.2.2

This release introduces pure-Go Excalidraw-compatible hand-drawn whiteboards across standalone files and Markdown documents, upgrades the MyGo UI runtime to `v0.10.0`, and resolves key editing bugs covering code block indentation, hierarchical `⌘A` / `Ctrl+A` selection, nested list indentation with single-Backspace tabs, and `Space` key viewport jumping.

### Native hand-drawn whiteboards

- **Pure-Go Excalidraw whiteboard engine (`internal/whiteboard`, `internal/editor/board*.go`, `internal/app/whiteboard.go`)**: Create standalone amphibious `.excalidraw.svg` (and `.excalidraw`) whiteboard documents (`⌥⌘B` / `Ctrl+Alt+B`, sidebar, or command bar), insert inline ```` ```excalidraw ```` blocks from `/`, or embed `![[*.excalidraw.svg]]` boards inside Markdown documents with double-click / modal editing and one-click extraction to standalone files.
- **Hand-drawn shapes, connectors, libraries, and Workspace integration**: Draw with 19 shape and connector tools (including rectangle, diamond, ellipse, triangle, parallelogram, hexagon, cylinder, cloud, star, callout, document, stadium, actor, freedraw, text, sticky note, frame, arrow, and line plus eraser and laser pointer), Rough-style strokes with hachure/cross-hatch/solid fills, magnetic arrow bindings with sharp/curved/elbowed routing, smart snap guides, Mermaid-to-whiteboard conversion, built-in and importable `.excalidrawlib` shape libraries (`whiteboard-library.excalidrawlib`), `[[wiki-link]]` badges and backlinks, Space Q&A indexing and element-level citation jumps, version history/restore, and pure-Go vector PDF and standalone HTML export.

### Editor and keyboard fixes

- **Code block visual tabs, soft-wrap indentation, paste recovery, and Enter/Tab behavior (`internal/editor/input.go`, `internal/editor/clipboard.go`, `internal/editor/codewrap.go`, `internal/editor/style.go`, `internal/editor/vectorpdf.go`, `internal/document/engine.go`)**: Render leading `\t` tabs visually in code blocks and vector PDFs, preserve visual leading indentation across soft-wrapped long lines, preserve and recover leading indentation when pasting HTML code snippets (`<pre>` / `white-space: pre`), auto-indent on `Enter` in code blocks while cleaning abandoned blank-line indentation, and support literal `\t` insertion plus multi-line `Tab` / `Shift+Tab` indent/outdent ([#19](https://github.com/Ciao1019/ArcMD/issues/19)).
- **Hierarchical `⌘A` / `Ctrl+A` Select All (`internal/editor/input.go`)**: Expand selection progressively on repeated `⌘A` / `Ctrl+A`—inline math body → current block or table cell → enclosing column or table → full document (plus all top-level blocks in block selection mode)—without jumping the viewport scroll position ([#18](https://github.com/Ciao1019/ArcMD/issues/18)).
- **Nested list indentation and single-Backspace tabs (`internal/editor/input.go`, `internal/document/engine.go`, `internal/editor/markdown.go`)**: Indent and outdent single or multi-item bullet, numbered, and task lists cleanly with `Tab` / `Shift+Tab`, step down indentation before unstyling on `Backspace` or empty `Enter`, preserve nested ordered-list levels across Markdown round-trips, and insert `\t` in prose so a single `Backspace` deletes the whole tab ([#17](https://github.com/Ciao1019/ArcMD/issues/17)).
- **Prevent `Space` paging while editing (`internal/editor/input.go`, `internal/editor/view.go`, `internal/editor/buffer.go`, `internal/editor/reading.go`)**: Consume `Space` and `Shift+Space` key events in the editable writing surface so MyGo does not scroll the page while typing or composing IME text, while keeping `Space` / `Shift+Space` viewport paging working cleanly in read-only mode without triggering the locked-page notice ([#16](https://github.com/Ciao1019/ArcMD/pull/16), [#7](https://github.com/Ciao1019/ArcMD/issues/7), thanks to `@Raspercival`).

### Runtime and performance

- **MyGo `v0.10.0` and baseline tracking (`go.mod`, `go.sum`, `internal/document/engine.go`, `internal/app/document.go`, `tools/perf/overlay.go`)**: Upgrade `github.com/egoist/mygo` from `v0.7.4` to `v0.10.0`—measuring **0.248 ms P50** typing and **0.447 ms P50** scrolling on the 1M-character mixed benchmark—and add leaf-scoped baseline run tracking (`OnBeforeLeafChange`) to avoid full-document dirty recomputation on single-leaf edits.

### Documentation

- Update `README.md`, `README.en.md`, `AGENTS.md`, `docs/building.md`, `tools/dev.sh`, `tools/perf/README.md`, and `.github/workflows/build.yml` for version `0.2.2`, hand-drawn whiteboards, and MyGo `v0.10.0`.

### Data compatibility

Document (`.md` and legacy `.arcmd` format **8**) and Space configuration (`spaces.json` format **9**) formats are unchanged from 0.2.1. Spaces may also contain optional Excalidraw-compatible `.excalidraw.svg` / `.excalidraw` whiteboard files, and the configuration directory may store user whiteboard library items in `whiteboard-library.excalidrawlib`.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates. The plugin marketplace and plugin execution runtimes remain unavailable.

## 0.2.1

This release upgrades PDF export to a pure-Go vector pipeline with hierarchical outlines and clickable links, adds native Mermaid diagram rendering, `\boldsymbol` bold math fonts, and Obsidian `![[...]]` image embed compatibility, speeds up large-document editing and command-bar discovery with Pinyin/multi-keyword search and persistent caching, introduces named version milestones with grapheme-level inline diffs, and resolves Windows shortcut and path-handling issues.

### Vector PDF export and native Mermaid diagrams

- **Pure-Go vector PDF export (`internal/pdf`, `internal/editor/vectorpdf.go`)**: Export selectable, searchable PDFs directly as scalable vector paths, rectangles, and embedded Type 3 glyph subsets from KaTeX math (regular, bold, italic, and bold-italic) and system fonts with `/ToUnicode` mappings—eliminating full-page raster JPEG streams while embedding opaque images cleanly. Exported PDFs include hierarchical `/Outlines` bookmarks generated from document headings and clickable `/Link` annotations for external URLs and internal `#heading` anchors.
- **Native Mermaid diagrams (`internal/mermaid`, `internal/editor/mermaid.go`)**: Render `flowchart` / `graph` (layered layout, subgraphs, 11 node shapes, solid/dashed/thick labeled edges) and `sequenceDiagram` (participants, actors, sync/async arrows, activations, notes, and `alt`/`opt`/`loop`/`par` fragments) directly in Go with light/dark theme tokens—no JavaScript or webview. Focusing a Mermaid block opens an editing card with live diagram preview above a monospace source editor (`↵↵` or `Tab` exits below); diagrams insert from `/` or the command bar and export into both standalone HTML and vector PDFs.

### Search, code blocks, math, and version history

- **Mandarin Pinyin, multi-keyword search, and persistent index cache (`internal/pinyin`, `internal/fuzzy`, `internal/search`)**: Match Chinese document titles and Space/folder hints by full Pinyin or initials (e.g. `zb` or `zhoubao`), combine whitespace-separated tokens across titles and full-text search, jump directly to matched fallback terms in the editor, and persist an atomic `0600` disk index snapshot (`search-cache.bin`) for instant startup search.
- **Stateful incremental syntax highlighting and code soft-wrap toggle (`internal/editor/highlight.go`, `internal/editor/codewrap.go`)**: Upgrade code blocks to a line-cached stateful lexer supporting 17 languages (Go, Rust, C/C++/Java, JS/TS, Python, Ruby, PHP, Shell, JSON/JSONC, YAML, TOML, SQL, HTML, CSS, Markdown, and Mermaid), live ```` ```lang ```` fence conversion on Enter, a **Settings › General** default wrap preference (`codeNoWrap`), and per-block wrap toggles with horizontal scrolling.
- **Native `\boldsymbol` math rendering (`internal/tex`, `internal/editor/math.go`)**: Parse and render `\boldsymbol{...}` across display math, inline math, bookmark previews, and vector PDF export using embedded unmodified `KaTeX_Main-Bold.ttf` and `KaTeX_Math-BoldItalic.ttf` faces (`9b2280d`), carrying scoped bold styling and glyph cache keys while preserving operator spacing, limits, and integral upper-hook clearance ([#4](https://github.com/Ciao1019/ArcMD/pull/4), thanks to `@Raspercival`).
- **Version milestones, grapheme-level inline diffs, and Fenwick virtualization (`internal/history`, `internal/app/versions.go`, `internal/editor/fenwick.go`)**: Save, rename, and filter protected named milestones in `.arcmd/history` (`labels.json`), highlight exact intra-line character and word edits with Unicode grapheme-aware LCS diffing, and index virtualized row heights with an `O(log N)` Fenwick tree for large documents.

### Obsidian compatibility and Windows fixes

- **Obsidian `![[...]]` image embeds and Space attachment lookup (`internal/editor/markdown.go`, `internal/wiki/wiki.go`, `internal/app/document.go`, `internal/app/relocate.go`)**: Import standalone and back-to-back Obsidian image embeds (`![[image.png]]`, `![[folder/image.png|360]]`, `![[image.png|360x240]]`, and `![[image.png|caption|width]]`) into native image blocks while preserving untouched `![[...]]` source lines verbatim on save, excluding `![[...]]` embeds from note wiki-links and `[[` completion, and resolving Space-root or bare attachment filenames across non-hidden Space directories while keeping External Space isolation strict ([#6](https://github.com/Ciao1019/ArcMD/issues/6)).
- **Windows shortcut defaults, menu labels, and path error handling (`internal/editor/keys.go`, `internal/app/keymap.go`, `internal/fsutil/fsutil.go`, `internal/vault/vault.go`, `internal/prefs/secrets.go`, `internal/app/document.go`)**: Bind `overview`, `prev-bookmark`, and `next-bookmark` to `Ctrl+Alt+S` / `Ctrl+Alt+Up/Down` outside macOS so they do not collapse into `Ctrl+S` or `Ctrl+Up/Down`, format non-macOS `Combo.Label()` values as `Ctrl+Shift+K`, distinguish regular-file path blockers from missing files under Windows `ERROR_PATH_NOT_FOUND`, and run `go test ./...` on `windows-2022` in CI ([#5](https://github.com/Ciao1019/ArcMD/issues/5)).

### Documentation

- Update `README.md`, `README.en.md`, `AGENTS.md`, and `third_party/README.md` for version 0.2.1, vector PDF export, native Mermaid diagrams, `\boldsymbol`, Pinyin/cached search, version milestones, Obsidian `![[...]]` image embeds, and Windows fixes.

### Data compatibility

Document (`.md` and legacy `.arcmd` format **8**) and Space configuration (`spaces.json` format **9**) formats are unchanged from 0.2.0. Preferences (`prefs.json` v1) add an optional `codeNoWrap` boolean; document history adds an optional `.arcmd/history/<doc>/labels.json` milestone map; and the configuration directory stores an optional regenerable `search-cache.bin` snapshot.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates. The plugin marketplace and plugin execution runtimes remain unavailable.

## 0.2.0

This release introduces **Space Q&A**—a built-in, read-only AI research agent that searches and reads a Space's notes directly without a vector database—alongside viewport-aware floating menus, smarter live Markdown and display-math input, and large-document virtualization.

### Space Q&A

- **Zero-index agentic search and reading**: Press `⌘J` (or click the toolbar Q&A icon) to open a per-Space side panel. Instead of building a vector index, the agent explores a live, line-numbered Markdown projection of the Space—including unsaved edits in open documents and registered External space files—using parallel read-only tools (`glob`, case-insensitive and regex `grep`, `read`, `outline`, and `links` for wiki-links, backlinks, and `#tags`) within a bounded 16-step budget, treating note content strictly as untrusted data rather than instructions.
- **Interactive inline citations and gliding previews**: Answers cite exact `[path#L12-L18]` ranges rendered as inline monogram source chips with `+N` multi-source badges. Hovering or focusing a chip highlights the supporting claim in the answer and floats a gliding preview card beside the panel with the document icon, section heading, line range, and recalled excerpt; expanding the stacked-monogram **Sources** footer syncs hover highlights bidirectionally with inline claims, and clicking any chip or source jumps straight to the cited block in the editor with a highlight glow.
- **Paced streaming, exploration timeline, and native polish**: Streamed Markdown answers (headings, lists, blockquotes, code blocks, tables, and inline marks) use adaptive balanced pacing and per-character fade-in, paired with a collapsible step-by-step search/read timeline, a `Jump to latest` pill while reading earlier passages, localized error states with one-click retry (plus automatic retries on transient 429/5xx/stream cutoffs before text starts), and a pointer-tracking ghost avatar.
- **Bring-your-own Chat Completions model and CLI runner**: Configure **Settings › AI Q&A** with presets for **OpenAI**, **DeepSeek** (with automatic `reasoning_content` replay across tool calls), **OpenRouter**, local **Ollama** (keyless `localhost` HTTP supported; remote endpoints require `https://`), or any custom Chat Completions service, including one-click `/models` fetching and a searchable model picker. API keys are stored atomically outside `prefs.json` and Space folders in `credentials/ai-api-key` (`0600`). Run `arcmd ask` in the terminal to inspect tool traces and verify citations from the command line.

### Fixes and editor improvements

- **Viewport-aware floating menus and submenus**: Flip block-handle menus, `/`, `[[`, and `:`/`@` completion menus, callout color/icon pickers, and code-language pickers above the anchor when bottom viewport space is tight, clamp height with inner scrolling when neither side fits full height, keep side submenus (`Indent and Alignment`, `Color`) inside horizontal and vertical window bounds, and keep `/`, completion, and command-bar selections scrolled into view during arrow-key navigation ([#2](https://github.com/Ciao1019/ArcMD/issues/2)).
- **Reverse live Markdown and predictive display math**: Prepending block prefixes (`#`–`###### `, `> `, `- `, `1. `, `- [ ] `) or opening inline delimiters (`**`, `*`, `~~`, `` ` ``) before existing text now compiles in place while preserving trailing content, with single-Backspace rollback to literal syntax. Typing `$$` in an empty paragraph immediately opens a display-math card (`Backspace` or `Esc` restores literal `$$`), single-line `$$ … $$` compiles live, and `$$ ` opens display math even when auto-pairing is off.
- **Large-document virtualization and O(1) dirty tracking**: Virtualize offscreen paragraph shaping on documents exceeding 800 leaves or 120,000 characters with pointer-free 128-bit run fingerprints and bounded LRU caches; replace full-document JSON baseline clones with streaming digests and leaf-level dirty tracking; keep single-leaf IME composition and inline formatting clone-free; extract plain text directly from Markdown when indexing search; defer backlink/tag footer recomputation while actively typing in long documents; and ignore atomic-save temp-dotfile overwrites in the directory watcher.

### Documentation

- Refresh `README.md`, `README.en.md`, `AGENTS.md`, `docs/architecture.md`, `docs/plugins.md`, and `third_party/` notices for Space Q&A and version 0.2.0.

### Data compatibility

Document (`.md` and legacy `.arcmd` format **8**) and Space configuration (`spaces.json` format **9**) formats are unchanged from 0.1.0. Preferences (`prefs.json` v1) add optional `ai` service fields (`provider`, `baseURL`, `model`), while the API key is stored separately in `credentials/ai-api-key`. Older releases ignore unknown preference fields.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates. The plugin marketplace and plugin execution runtimes remain unavailable.

## 0.1.0

This release makes standard `.md` Markdown the primary document format, adds a built-in External space for standalone files, and brings fluid motion and tactile micro-interactions across typing, math, tables, images, and the sidebar.

### Native Markdown and external files

- Save and autosave `.md`, `.markdown`, and `.mdx` documents directly in place without creating sidecar `.arcmd` copies. Unmodified top-level blocks preserve their original Markdown formatting, while rich attributes—colors, highlights, hand-drawn annotations, callouts, toggles, merged or colored tables, custom two-column widths, and page icons, covers, and word goals—round-trip losslessly through standard Markdown, semantic HTML, and YAML frontmatter.
- Open standalone files outside any Space in a built-in **External** space (`Open File…` or OS file associations) without scanning parent folders. Edits save in place, images resolve relative to each file's folder, and removing an entry drops only the reference. Legacy `.arcmd` documents and snapshots remain supported.

### Fluid writing and micro-interactions

- Glide the caret smoothly across characters, lines, and math capsules; scroll with interruptible exponential damping for typewriter mode, edge tracking, and search or footnote jumps; and animate block, fold, indent, and table row/column displacement without disturbing document coordinates.
- Compile inline `**bold**`, `*italic*`, `~~strike~~`, `` `code` ``, `--`, `...`, and `---` live as you type, with one-keystroke Backspace rollback to literal text. Auto-pair brackets, smart curly quotes, and inline `$…$` math with soft scope highlights and `Tab` jump-out badges.
- Preview inline `$…$` TeX in a floating source capsule below the line without shifting baselines, and edit display math in a unified preview-and-source card (`↵↵` or `Tab` to continue prose).
- Unfold `| A | B |` pipe headers into native tables on Enter, drag table rows and columns to reorder, resize two-column seams alongside table columns and split panes with `50% · 50%` magnetic snap pills, and toggle task checkboxes with spring motion.
- Preview dragged images as tilted Polaroid cards that spring upright on drop, size local images immediately from file headers without layout jumps, open double-clicked images in a damped hero lightbox, and save documents and history snapshots asynchronously off the UI thread.
- Refine sidebar drag-and-drop with floating cards and dashed insertion slots, inline hover `+ New Document` seams, instant zero-blocking command bar opening, directional back/forward transitions, and synchronized rich-text ⇄ Markdown preview cross-fades.

### Documentation

- Refresh the Chinese and English READMEs around a top-of-page fluid writing showcase and rebuild all localized vector animations from a unified `tools/readme-art` generator.

### Data compatibility

ArcMD 0.1.0 writes Space configuration format **9** (adding the built-in External space map) and continues to read and write legacy `.arcmd` document format **8** alongside native `.md` files. Older releases cannot open `spaces.json` v9; keep a copy of your configuration directory if you need to return to an earlier version.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates. The plugin marketplace and plugin execution runtimes remain unavailable.

## 0.0.11

This release adds hand-drawn annotations, more ways to write and organize documents, and broad improvements to the editor's algorithms for long documents.

### Writing and expression

- Mark text with 12 hand-drawn styles, including circles, waves, highlights, arrows, brackets, and boxes. Choose colors, edit existing marks, and watch strokes draw themselves in.
- Select multiple blocks with the keyboard or margin, then move, indent, duplicate, convert, or delete them together. Add toggle blocks to fold a section of notes.
- Insert emoji with shortcodes such as `:smile:` and use `@` for dates and document links. Optional automatic spacing separates Chinese and Latin text.
- Set a document word goal and see estimated reading time. Choose Chinese or English UI, font family, page width, text size, and line height.
- Paste CSV or tab-separated content as tables and sort table rows by a column.

### Documents and media

- Create documents from Space templates, with date, time, weekday, and title placeholders. Open or create today's note with one command.
- Find and replace across documents, rename or merge tags, and sort folders by name or modification time. The command bar remembers recent commands.
- Move a document into its own native window while preserving its cursor, scroll position, and undo history.
- Play animated GIFs and crop still images into a new asset while keeping the original. Export documents as standalone HTML pages alongside Markdown and PDF.
- Undo document deletion and moves from the notification. Refine sidebar, Space, and document navigation behavior.

### Editor performance

- Index UTF-16 and UTF-8 positions with Fenwick trees, reuse stable block IDs, and read only the relevant text ranges for ordinary input and word navigation.
- Update local layouts, footnote references, word counts, and search results incrementally. Cull offscreen rows and decorations, virtualize long outlines, and avoid full-document snapshots during repeated IME candidate updates.
- On the million-character mixed fixture, CPU scene construction is **0.273 ms P50 / 0.291 ms P95** for typing and **0.271 ms / 0.306 ms** for backspace. Typing with search open takes **0.281 ms P50**, including event handling; repeated IME candidate updates take **0.264 ms P50**, excluding composition start and commit.
- These measurements cover native event handling and CPU scene construction as specified in the [performance report](https://github.com/Ciao1019/ArcMD/blob/v0.0.11/docs/performance.md), not GPU presentation or end-to-end display latency. Existing editing interactions and full-layout fallbacks are retained.

### Documentation

- Refresh both READMEs with the current feature set and optimized performance results, without test dates.
- Add smooth, looping Chinese and English annotation SVGs, original demo documents, and a Go artwork generator.

### Data compatibility

ArcMD 0.0.11 writes document format **8** and Space configuration format **8**, migrating older formats when saved. Documents gain annotations, toggle blocks, and word goals; Space configuration gains folder sorting. Older releases cannot open the upgraded formats. Keep a copy of documents and configuration if you need to return to an earlier version.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates. The plugin marketplace and plugin execution runtimes remain unavailable.

## 0.0.9

This update fixes jumping while selecting text across screens and replaces the README demonstrations with smooth vector animations.

### Fixes

- Keep the selection endpoint under the pointer while scrolling, even when the pointer stays still. Dragging and releasing no longer pull the page back to the caret, including in typewriter mode ([#1](https://github.com/Ciao1019/ArcMD/issues/1)).
- Scroll smoothly at the viewport edges, preserve manual mouse-wheel scrolling, and stop selection scrolling on release or window focus loss.
- Copy selected text without changing the viewport or selection.
- Add regression coverage for touchpad scrolling, mouse-wheel animation, reverse selection, viewport boundaries, multiple clicks, and inset editors with the outline open.

### Documentation

- Replace the Chinese and English theme and bookmark demonstrations with lightweight, self-contained SVG animations in the README hero's style.
- Animate theme colors, multi-color gradients, translucent frosted material, and light/dark appearance continuously. Animate bookmark placement, outline navigation, and the all-bookmarks command bar.
- Keep seamless loops and reduced-motion illustrations, and include a reproducible vector-art generator.

### Data compatibility

Document, Space configuration, and preference formats are unchanged from 0.0.8.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates.

## 0.0.8

This update fixes the cursor position after pasting rich content and adds native-client demonstrations to the README.

### Fixes

- Place the cursor at the end of the pasted content, including the final wrapped line, so continued typing appends to the paste instead of appearing at the start of its last block.
- Find the final editable leaf when pasting tables or columns, including merged table cells, while preserving the destination's surrounding text and undo/redo selection.
- Add regression coverage for native rich text, HTML, Markdown, plain text, CRLF line endings, emoji, and narrow and wide editor layouts.

### Documentation

- Update the Chinese and English READMEs with animated SVG demonstrations of space switching, theme colors and gradients, translucency, and bookmarks, captured from the locally installed client.
- Include a separately labeled material illustration for background blur and tint, plus reduced-motion support and capture provenance.

### Data compatibility

Document, Space configuration, and preference formats are unchanged from 0.0.7.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates.

## 0.0.7

This release adds a guided tour that introduces ArcMD the first time it opens.

### Guided tour

- A short tour walks through creating spaces, theme colors, swiping between spaces, bookmarks, pinned documents, and favorites. A spotlight glides to the part of the window each step is about, and every step has a small animated illustration in the space's own colors.
- The step on theme colors opens the real theme editor and gently moves its colors, so the whole window shows the live preview. Nothing is saved: the space keeps its theme when the step ends.
- The tour appears once: on the first launch of a new installation, and on the first launch after updating from an earlier version. Finishing or skipping it keeps it from appearing again; quitting halfway shows it on the next launch.
- Use ← and → to move between steps, Enter to continue, and Esc to skip. Replay it at any time from the command bar ("新手教程") or Settings › About.
- The tour only points: it does not change any space or document, and it honors the system's reduced-motion preference.

### Data compatibility

Document and Space configuration formats are unchanged from 0.0.6. Preferences gain a `tourSeen` flag; older ArcMD releases ignore it.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives and manifests support in-app updates from earlier releases.

ArcMD remains an early preview. macOS packages use ad-hoc signing and are not notarized; Windows packages are unsigned. The plugin marketplace and plugin execution runtimes remain unavailable.

## 0.0.6

This release brings the full set of new writing and reading features, together with all security and data-safety fixes from 0.0.5.

### Writing and navigation

- Bookmark passages, rename bookmarks, find them in the outline and global command bar, and jump between documents. Formula bookmarks include rendered previews.
- Write inline and block TeX with live source previews, `$$` input, and formula clipboard exchange. Native formula rendering includes embedded KaTeX fonts and their licenses; it supports ArcMD's TeX subset.
- Add footnotes with automatic numbering, jump to their definitions, and return to the reference.
- Use superscript and subscript, including Markdown and HTML interchange.
- Copy and paste rich Markdown and HTML content, including tables and columns.
- Edit links in an anchored popover and preview local wiki-link targets on hover.
- Resize images with the pointer or keyboard and save their widths with the document. The image viewer supports continuous zoom and panning.
- Automatically pair Chinese and English brackets and quotes, wrap selected text, and delete empty pairs together. Pairing can be disabled in settings.
- Adjust reading font size and line height in preferences.
- Lock individual editors or split panes for read-only viewing.
- Edit columns with clickable empty areas, Tab navigation, and responsive stacking in narrow layouts.
- Click gaps between or after closed blocks to insert a paragraph.
- Use reorganized More and settings menus. Rename sidebar documents and folders from their context menu; double-click no longer starts renaming.

### Security and data safety

All five fixes from 0.0.5 remain included: save-conflict read failures preserve unsaved edits; Space paths reject escaping symbolic links and stop folder-link cycles; malformed table cells produce load errors instead of panics; case-only renames cannot overwrite a different existing file; and plugin requests require HTTPS at every redirect. Regression tests, document-decoder fuzz coverage, and Linux case-sensitive filesystem checks are included.

### Data compatibility

ArcMD 0.0.6 writes document format **6** and Space configuration format **7**. It reads and migrates older formats. Documents are written in the new format when saved, and Space configuration is upgraded when persisted. Older ArcMD releases cannot read these upgraded files. Back up documents and configuration before upgrading if you need to return to an older release.

### Downloads

Packages are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`. Signed update archives, delta patches, and five `update-*.json` manifests support in-app updates.

ArcMD remains an early preview. macOS packages use ad-hoc signing and are not notarized; Windows packages are unsigned. Windows 32-bit, the plugin marketplace, and plugin execution runtimes remain unavailable. Keep `.arcmd` originals to preserve all rich-text features.

## 0.0.5

A security and data-safety patch release. Existing editing features, UI, and the document format remain unchanged from 0.0.4.

- Stop saving when the on-disk document cannot be read for conflict checking. Unsaved edits remain available, and external changes are not overwritten after a read failure.
- Reject document paths whose symbolic links resolve outside their Space, including paths for new files. Preserve valid links within a Space and stop recursive folder-link cycles. Relative Markdown images such as `../images/` remain supported.
- Reject malformed table cells with a document-load error instead of panicking. Damaged documents remain untouched on disk.
- Refuse case-only renames when the destination is a different existing file on a case-sensitive filesystem. Normal case-only renames remain supported.
- Require HTTPS for plugin index and package requests, including every redirect. The plugin marketplace and runtime remain unavailable in this release.
- Add regression coverage, document-decoder fuzz testing, and Linux CI coverage for case-sensitive file operations.

Downloads are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`.

## 0.0.4

A macOS application-icon correction for the hand-drawn beaver identity.

- Normalize the icon tile to the macOS grid and make its interior opaque, so macOS Tahoe no longer shrinks it inside an extra gray background.
- Preserve the approved beaver illustration and ivory paper background. The horizontal README artwork is unchanged.
- Application features remain the same as 0.0.2; unreleased plugin work is not included.

## 0.0.3

A logo-only release introducing ArcMD’s hand-drawn beaver identity.

- Replace the application icon with the new beaver artwork, including dedicated small-size macOS icons.
- Use the horizontal ArcMD logo in the Chinese and English READMEs.
- Application features remain the same as 0.0.2. The plugin marketplace/runtime work is not included.

Downloads are available for macOS (Apple Silicon and Intel), Windows x64, and Linux (x64 and ARM64). Verify downloads against `SHA256SUMS.txt`.

## 0.0.2

This release improves images, document moves, system file opening, and PDF export.

### Improvements

- Open ArcMD and Markdown documents through system file associations, Dock drops, or development command-line arguments. Files reuse the closest existing space, or add their parent folder as a space, without converting or copying the original on open.
- Display images at their natural proportions, with a full-size preview and editable descriptions. Missing or unreadable images now show recovery actions to choose a replacement or locate the original; replacements support undo and redo.
- Load local Markdown images from parent folders, including URL-encoded filenames and GIF first frames.
- Export PDFs page by page with progress and cancellation, keeping memory use bounded. Text and table content can now be selected, copied, and searched in the exported PDF.

### Fixes

- Keep images, covers, undo/redo states, and version history together when moving documents or folders, including across spaces. Shared images remain available, filename collisions are handled without overwriting, and resource failures roll back the move.
- Resolve wiki links in the source document's space first. Renaming a document updates links that resolved to it without changing links to same-named documents in other spaces.
- Keep toolbar controls clear of native window buttons in normal, focus, and split views, including narrow windows and long document titles.

### Downloads

| Platform | Architecture | Packages |
| --- | --- | --- |
| macOS | Apple Silicon | `arcmd-0.0.2-arm64.dmg` |
| macOS | Intel | `arcmd-0.0.2-amd64.dmg` |
| Windows | x64 | `arcmd-setup-0.0.2-amd64.exe`, `arcmd-0.0.2-windows-amd64.zip` |
| Linux | x64 | `arcmd_0.0.2_amd64.deb`, `arcmd-0.0.2-linux-amd64.tar.gz` |
| Linux | ARM64 | `arcmd_0.0.2_arm64.deb`, `arcmd-0.0.2-linux-arm64.tar.gz` |

Verify downloads against `SHA256SUMS.txt`. The additional `update-*.json` manifests and signed `.tar.gz` archives support application updates.

### Preview limitations

ArcMD remains an early preview. macOS packages use ad-hoc signing and are not notarized; Windows packages are unsigned. Windows 32-bit and the plugin marketplace/runtime are not yet available. GIFs display their first frame, and PDF formulas and images remain raster content. Keep `.arcmd` originals to preserve all rich-text features.

## 0.0.1

The first ArcMD release: a native rich-text editor built in Go, inspired by Arc.

- Organize local documents in colorful spaces with favorites, pinned documents, folders, and search.
- Reorder spaces by dragging their cards in the library or using the arrow keys; the space switcher follows the saved order.
- Write with headings, lists, tasks, tables, code blocks, images, callouts, and columns.
- Navigate with the command palette, document outlines, links, and up to four split panes.
- Import and export Markdown while keeping full document structure in local `.arcmd` files.
- Keep work with autosave, version history, external-change detection, and recently deleted items.
- Receive signed application updates through GitHub Releases.

### Downloads

| Platform | Architecture | Packages |
| --- | --- | --- |
| macOS | Apple Silicon | `arcmd-0.0.1-arm64.dmg` |
| macOS | Intel | `arcmd-0.0.1-amd64.dmg` |
| Windows | x64 | `arcmd-setup-0.0.1-amd64.exe`, `arcmd-0.0.1-windows-amd64.zip` |
| Linux | x64 | `arcmd_0.0.1_amd64.deb`, `arcmd-0.0.1-linux-amd64.tar.gz` |
| Linux | ARM64 | `arcmd_0.0.1_arm64.deb`, `arcmd-0.0.1-linux-arm64.tar.gz` |

Verify downloads against `SHA256SUMS.txt`. The additional `update-*.json` manifests and signed `.tar.gz` archives support application updates.

### Preview limitations

ArcMD is an early preview; macOS is the primary development platform. macOS packages use ad-hoc signing and are not notarized, and Windows packages are unsigned. Update signatures verify application updates; they do not replace operating-system code signing.

Windows 32-bit is unavailable because MyGo v0.7.4 has no working GUI backend for it. The plugin marketplace and plugin execution runtime are not yet available. Markdown export cannot preserve every rich-text feature; retain `.arcmd` files as the complete originals.
