<div align="center">

<img src="docs/images/readme/logo-horizontal.png" width="420" alt="ArcMD logo">

**A fresh take on the Markdown editor. Native, expressive, and built around your spaces.**

[简体中文](README.md) · English

<a href="mygo.json"><img src="https://img.shields.io/badge/version-0.0.11-F21158" alt="Version 0.0.11"></a>
<img src="https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux-F25F11" alt="macOS, Windows and Linux">
<a href="https://github.com/egoist/mygo"><img src="https://img.shields.io/badge/built%20with-Go%20%2B%20MyGo-00ADD8?logo=go&amp;logoColor=white" alt="Built natively with Go and MyGo"></a>
<a href=".github/workflows/build.yml"><img src="https://img.shields.io/badge/CI-GitHub%20Actions-2088ff?logo=githubactions&amp;logoColor=white" alt="CI workflow configuration"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-C912D6" alt="MIT License"></a>
<img src="https://img.shields.io/badge/status-preview-F2410F" alt="Preview">

[![Listed on Awesome MyGo](https://awesomemygo.dev/badge.svg)](https://awesomemygo.dev/apps/arcmd)

<br><br>

<img src="docs/images/readme/hero-en.svg" width="100%" alt="ArcMD swiping from a pink Personal space to an orange Reading space: each space has its own colors, files and open page">

<br><br>

<a href="https://github.com/Ciao1019/ArcMD/releases"><img src="docs/images/download-macos-en.svg" height="56" alt="Download ArcMD for macOS — Apple Silicon and Intel, .dmg"></a>
<a href="https://github.com/Ciao1019/ArcMD/releases"><img src="docs/images/download-windows-en.svg" height="56" alt="Download ArcMD for Windows — x64 installer and ZIP"></a>
<a href="https://github.com/Ciao1019/ArcMD/releases"><img src="docs/images/download-linux-en.svg" height="56" alt="Download ArcMD for Linux — x64 and ARM64, DEB and TAR.GZ"></a>

<sub>The current preview version is 0.0.11. The buttons open the releases page; you can also <a href="#build-from-source">build from source</a>.</sub>

<br><br>

[Highlights](#highlights) · [Features](#features) · [Performance](#performance) · [Build from source](#build-from-source) · [Contributing](#contributing)

</div>

<br>

> **Tribute & Origin**
>
> The day The Browser Company announced Arc was entering maintenance mode, countless voices on X mourned the twilight of a piece of software crafted with so much taste, tactile delight, and soul.
>
> As a longtime devotee of Arc, I couldn't bear to let go of what had fundamentally reshaped my workflow: the vertical sidebar, vibrant Spaces, effortless Favorites, and that calm, breathing canvas. Since Arc has paused its journey, we chose to carry its spirit forward into document editing with pure Go and Native UI — no Electron, no Webview, just pure native performance and the uncompromised joy of Arc's interactions.
>
> ArcMD is dedicated to every creator who still believes that software should have craft, feel, and soul.

ArcMD brings Markdown's familiar writing flow to a rich-text canvas, with a
workspace inspired by [Arc](https://arc.net/): a vertical sidebar, colorful
spaces, favorites, and a quiet page that puts your words first. Keep a journal,
develop an idea, or organize an entire project in a place that feels like yours.

**Built entirely in Go with [MyGo](https://github.com/egoist/mygo)'s Native UI.**
MyGo provides native windows, GPU drawing, text shaping, and system input;
ArcMD builds its own document engine and rich-text editor on top. The interface
is drawn directly from Go, with no HTML, JavaScript, WebView, Electron, or cgo.

> [!NOTE]
> **ArcMD is still young.** It's an early preview, and plenty is unfinished — the editor in particular is still fairly basic and improving one step at a time.
> If you like where it's heading, share bugs and ideas in [Issues](https://github.com/Ciao1019/ArcMD/issues) or send a pull request. We'd love to build it together.

## Highlights

### A space for every part of your life

Each space is a folder with its own name, icon, and color theme — one, two, or
three colors with a touch of grain. Swipe with two fingers on the sidebar, or
press <kbd>⌃1</kbd>…<kbd>⌃9</kbd>, and the whole window glides into the next
space. Every space remembers the page you left open there; favorites stay with
you wherever you go.

### Colors, gradients, translucency and grain, previewed live

<p align="center"><img src="docs/images/readme/themes-en.svg" width="100%" alt="Smooth vector animation of ArcMD themes: drag a color handle, blend two or three colors, lower the intensity for translucent frosted material, and transition between light and dark"></p>

Choose “编辑主题颜色…” (Edit Theme Colors) from the space name's ••• menu. Drag
the dot on the color pad to change the color, and use − / + to blend one, two or
three colors. Lower the intensity of a single-color theme and the window turns
translucent, softly blurring the desktop behind it; turn the grain dial, or pick
Auto, Light or Dark.

### Hand-drawn marks: give your ideas a human touch

<p align="center"><img src="docs/images/readme/annotations-en.svg" width="100%" alt="Smooth vector animation of ArcMD hand-drawn marks: select words and draw a wave, circle, highlight, double underline, cross, arrow and box as an idea journal takes shape"></p>

Select a few words and open the dropdown beside the annotation button in the
floating toolbar. Choose from 12 hand-drawn styles to circle what matters,
cross out hesitation, or point to the next step. Each stroke draws itself in,
and you can choose its color. Marks are saved in your `.arcmd` document.
Try the [idea journal](docs/images/readme/annotations-demo-en.arcmd), or start
with a sentence of your own.

### Bookmarks: pick up where you left off

<p align="center"><img src="docs/images/readme/bookmarks-en.svg" width="100%" alt="Smooth vector animation of ArcMD bookmarks: click the margin to add bookmarks, watch their outline entries appear, jump smoothly back to a passage, and open all bookmarks with Shift Command D"></p>

Hover any paragraph and a bookmark appears in the margin; click it (or press
<kbd>⌘D</kbd>) to mark the spot. The outline lists the document's bookmarks,
<kbd>⌃⌘↑</kbd> / <kbd>⌃⌘↓</kbd> jump between them, and <kbd>⇧⌘D</kbd> lists
the bookmarks of every document. Bookmarks never change the document itself.

### The Library: all your spaces at a glance

<p align="center"><img src="docs/images/readme/library-en.svg" width="100%" alt="Clicking the Library button widens the sidebar into a row of space cards, each drawn in its own theme; clicking a page in the Reading space opens it there"></p>

Click the Library button in the corner, or press <kbd>⇧⌘L</kbd>. The sidebar
widens into a card for every space, each painted in its own colors, with its
folders ready to open. Pick any page to jump straight into it. Plugins and
recently deleted files live here too.

### A sidebar that steps aside

<p align="center"><img src="docs/images/readme/sidebar-en.svg" width="100%" alt="Pressing Command-Backslash collapses the sidebar so the page fills the window; moving the pointer to the left edge slides the sidebar out as a floating panel, and it tucks away again when the pointer leaves"></p>

Press <kbd>⌘ &#92;</kbd> and the page takes the whole window. When you need your
files, move the pointer to the left edge: the sidebar slides out over the page,
and tucks itself away as soon as you move on. In narrow windows it collapses on
its own.

## Features

### Write on a rich-text canvas

- **Rich formatting** — headings, bold, italic, underline, strikethrough, links,
  inline code, text colors, and highlights. Select text to reveal a floating
  toolbar; use `/` to insert a block.
- **Hand-drawn marks** — 12 styles with selectable colors, including circles,
  waves, highlights, arrows, and boxes, with animated strokes.
- **More than paragraphs** — nested lists, tasks, quotes, colored callouts,
  code blocks, images, inline and block TeX, footnotes, superscript/subscript,
  editable tables with merged cells, and two-column layouts.
- **Comfortable input** — Markdown and HTML clipboard exchange, bracket and
  quote pairing, and optional Chinese–Latin spacing. Type `:sm` to search emoji,
  or use `@` for dates and document links.
- **A document with structure** — a navigable outline, collapsible headings
  and lists, and dedicated toggle blocks. Select multiple blocks to move,
  indent, duplicate, or delete them together. Add page icons and image or
  gradient covers.
- **Tables and images** — paste CSV or tab-separated text as a table and sort
  rows by a column. Resize, zoom, and crop images while retaining the original;
  animated GIFs play in the page.
- **Bring your content along** — import and export Markdown, paste rich content
  from webpages and document apps, and export pages to PDF or HTML.

### Give every project its own space

- **Spaces** — separate folders for work, life, and projects, each with its own
  name, icon, and light or dark color theme. Switch with a trackpad swipe or a
  keyboard shortcut.
- **Favorites and pins** — keep favorites across spaces and pin documents or
  folders within a space. Drag to reorder, organize, or move between spaces.
- **Bookmarks** — hover a passage and click the bookmark beside it (or press
  ⌘D) to mark it, jump between marks with ⌃⌘↑ / ⌃⌘↓,
  and find them all in the outline or the command bar. Bookmarks never change
  the document itself.
- **Split views and windows** — work in up to four resizable panes, or move a
  document into its own native window, preserving its cursor, scroll position,
  and undo history.
- **Navigation and organization** — recent commands, full-text search,
  cross-document replacement, document and folder previews, and back/forward
  history. Sort folders by name or modification time.
- **Templates and daily notes** — reuse Space templates with date, time,
  weekday, and title placeholders; open or create today's note in one command.

### Connect ideas and stay in flow

- **Linked notes** — connect documents with `[[wiki links]]`, see backlinks,
  and browse, rename, or merge `#tags`. Renaming a document updates links to it.
- **Focused writing** — a distraction-free mode, optional paragraph dimming,
  typewriter scrolling, word goals, estimated reading time, and customizable
  keyboard shortcuts.
- **Your reading preferences** — Chinese and English UI, a choice of fonts,
  page width, text size, and line height, plus a per-editor reading lock.
- **Local ownership** — documents and attachments stay in your folders.
  Autosave, version history, external-change detection, and a 30-day recently
  deleted collection help you recover your work. Undo deletion and moves from
  their notifications.

### A small core, room to extend

AI writing, translation, and sync are planned as optional plugins. The manifest,
installer, and host interfaces are in place; plugin runtimes and the marketplace
are still in development. See the [plugin roadmap](docs/plugins.md).

## Performance

CPU frame processing time over **100 consecutive operations** of each type in a
mixed document of **1 million characters and 549 chapters**, including 500 tables
and 9,912 table cells. Each case runs in three fresh processes; the table reports
the median of the three per-run P50 and P95 values.

| Operation | P50 | P95 |
| --- | ---: | ---: |
| ⌨️ Typing | **0.273 ms** | 0.291 ms |
| ⌫ Backspace | **0.271 ms** | 0.306 ms |
| 🖱️ Scrolling | **0.548 ms** | 1.132 ms |

A 120 Hz display gives each frame **8.3 ms**. In this workload, CPU frame
construction takes less than **1.2 ms at P95**, leaving room in that budget for
input handling and presentation.

Including event handling, body typing in the same million-character document
takes **0.275 ms P50**, typing with find open takes **0.281 ms**, and repeated IME
candidate updates take **0.264 ms** (excluding composition start and commit).
Position indexes, local layout, incremental find, and viewport culling reduce
whole-document scans. Ordinary typing allocates about **0.067 MB per operation**;
this is allocation volume, not resident memory.

Apple M5 / 24 GiB, macOS 26.5.1, Go 1.27.3, MyGo v0.7.4, `CGO_ENABLED=0`.
The benchmark runs the normal frame pipeline through a headless
host and measures CPU scene construction. Input dispatch, rasterization, GPU
presentation, and display latency are outside the frame timer; these numbers
are not end-to-end input latency or measured display FPS.

Methodology, raw measurements, and reproduction scripts:
[Performance report](docs/performance.md) · [Benchmark harness](tools/perf/README.md).

Native Go + MyGo; no WebView, Electron, or separate background service. See
[GitHub Releases](https://github.com/Ciao1019/ArcMD/releases/latest) for actual
package sizes on each platform.

## Markdown and your files

ArcMD uses **`.arcmd`**, a versioned, readable JSON format, to preserve rich
document structure. Each document is a local file, with its images in a nearby
`assets/` folder.

Opening an existing `.md`, `.markdown`, or `.mdx` file imports it in memory.
The first edit saves a new `.arcmd` document beside it and keeps the original
Markdown file. Markdown export creates a separate `.md` file for sharing and
use in other tools.

Markdown cannot represent every rich-text feature: text colors, hand-drawn marks, some table
formatting, and merged-cell structure are not preserved on export. Keep the
`.arcmd` file for a complete copy of your document.

## Get ArcMD

Check [GitHub Releases](https://github.com/Ciao1019/ArcMD/releases) for published builds.
The [packaging workflow](.github/workflows/build.yml) targets:

| Platform | Architectures | Packages |
| --- | --- | --- |
| macOS | Apple Silicon, Intel | `.dmg` |
| Windows | x64 | Installer `.exe`, portable `.zip` |
| Linux | x64, ARM64 | `.deb`, `.tar.gz` |

macOS is the primary development platform. Current macOS packages are ad-hoc
signed and not notarized; Windows packages are unsigned.

## Build from source

Requires **Go 1.27.3** or newer. With `GOTOOLCHAIN=auto`, an older Go installation
downloads the required toolchain automatically.

```sh
git clone https://github.com/Ciao1019/ArcMD.git
cd ArcMD
CGO_ENABLED=0 go run ./cmd/arcmd
```

To try ArcMD with isolated development data:

```sh
ARCMD_HOME=/tmp/arcmd-dev CGO_ENABLED=0 go run ./cmd/arcmd
```

Build a native application package with the pinned MyGo CLI:

```sh
CGO_ENABLED=0 go run github.com/egoist/mygo/cmd/mygo@v0.7.4 build
```

See [Building and releasing](docs/building.md) for cross-platform packages,
CI artifacts, signing, and the release workflow.

## Contributing

Bug reports, ideas, and pull requests are welcome. Start with the
[architecture guide](docs/architecture.md) and [contributor conventions](AGENTS.md).
Report issues through [GitHub Issues](https://github.com/Ciao1019/ArcMD/issues).

```sh
gofmt -l .
CGO_ENABLED=0 go vet ./...
CGO_ENABLED=0 go test ./...
CGO_ENABLED=0 GOOS=linux go build ./...
CGO_ENABLED=0 GOOS=windows go build ./...
```

For UI changes, render and inspect the native snapshots:

```sh
ARCMD_SNAPSHOT=/tmp/arcmd-shots CGO_ENABLED=0 go test -run Snapshot ./internal/app
```

<div align="center">

If ArcMD is useful to you, a **⭐ Star** in the top-right corner means a lot!

<a href="https://github.com/Ciao1019/ArcMD/stargazers"><img src="docs/images/readme/star.gif" width="600" alt="Clicking the Star button on GitHub to star ArcMD"></a>

</div>

## Acknowledgements

Thanks to the [LinuxDo](https://linux.do/) community for its support.

## License

ArcMD is open source under the [MIT License](LICENSE).

Third-party notices and licenses are preserved in [third_party](third_party/).
