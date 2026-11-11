# Architecture

ArcMD is one Go binary with no web stack: [MyGo](https://github.com/egoist/mygo)
opens the window and draws the interface on the GPU from Go (`package ui`).

```
cmd/arcmd ──▶ internal/app ──▶ internal/space    spaces and spaces.json
                   │      ├──▶ internal/vault    documents and import files in a space folder
                   │      ├──▶ internal/plugin   manifests, registry, install, Host contract
                   │      ├──▶ internal/document rich document model, codec and transactions
                   │      ├──▶ internal/editor   native layout, input and interchange
                   │      └──▶ internal/fuzzy    command bar ranking
                   ▼
            github.com/egoist/mygo (mygo, ui)
```

The domain packages never import MyGo, so they test without a window and can
serve a future CLI or plugin host.

## The UI layer

`internal/app` follows MyGo's immediate mode: `App` is the state, and
`App.View(c)` rebuilds the window from it every frame.

| File | Role |
| --- | --- |
| `app.go` | State, first run, window options, actions (open, new, switch space, pin, trash) |
| `view.go` | Window layout, shortcuts, sidebar folding and peeking, toolbar and address, the card, writing surface |
| `sidebar.go` | Space header, pinned rows, folder tree, drag/drop, space bar |
| `icons.go` | Lucide icons |
| `palette.go` | Command bar: actions, fuzzy ranking, keyboard navigation |
| `dialogs.go`, `spacedraft.go`, `spaceeditor.go`, `spaceicons.go` | Space drafts and anchored property editors |
| `favorites.go`, `library.go` | Global favorite tiles and the library |
| `openfile.go` | System file-open routing and external-folder spaces |
| `relocate.go` | Document moves with attachments, undo states and snapshots |
| `pdfexport.go` | Cancellable, page-by-page PDF export and printing |
| `document.go` | The open rich document and read-only export preview: dirty tracking, autosave, images, links |
| `split.go`, `splitview.go`, `splitstore.go` | Document pane lifecycle, native split cards and dividers, saved layout |
| `theme.go` | Design tokens: the space-tinted theme and chrome colors |

### Window

A hidden title bar with the traffic lights over the sidebar, and the
sidebar vibrancy material washed with the space's color. The document sits
in a rounded card, as Arc's web content does.

### Split documents

Each Space remembers one group of up to four panes. `splitGroup` owns each
pane's document, navigation history and size share; `App.doc/history/at` are
aliases for the focused pane so existing commands target it. Rendering uses
explicit document pointers, never temporary swaps of the active document.
Each editor retains its own caret, selection, scroll, composition and undo.
A document already visible is focused instead of receiving a second writer.

Cra's layout provides independent rounded cards, a 2 DIP active outline,
10 DIP gaps, 200 DIP minimum lengths where space permits, six-point equal-pair
snapping, and draggable/keyboard-accessible dividers. The toolbar adds an empty
pane with a document picker, flips orientation, reorders, separates and closes
panes. Sidebar document drops select the nearest edge; the file stays put.
Ordinary grouped documents occupy a segmented sidebar row; pins and favorites
keep their own rows. Opening an unrelated document suspends the group, which
its row or a return to the Space restores. Links and history stay in a pane.

All retained editors participate in saving, conflict detection, path changes
and link rewrites. File watches include suspended groups. Save failures keep
the group intact and reveal the affected document. `search.Index.WithLive`
provides an immutable overlay of open edits for search, tags and backlinks.
`space.Split` stores only pane paths, size shares, orientation, active pane and
whether the group was shown in `spaces.json`; missing files reopen as empty
panes. Editor undo history remains in memory only.

### Editing

The primary file is a `.arcmd` document, encoded as readable JSON with a
format marker, version and stable block IDs. `internal/document` contains the
model, validation, cloning, structural editing and one shared undo history.
It uses the standard library and can be tested without the UI toolkit.

Text blocks contain runs with bold, italic, underline, strike, code and link
attributes. Tables contain rows and cells. Column groups contain column
containers and their child blocks. UTF-16 ranges form the native text-service
view of the tree. A transaction changes the model and selection atomically.
Nested operations form one undo step. IDs survive ordinary edits and history,
and duplicates and pasted structures receive fresh IDs.

`internal/editor` adapts MyGo's native text-input client, shapes styled runs,
and draws the editor. Text layout is cached by block ID, runs, metrics and
width. Selection moves reuse layout geometry. Tables share the document's
input and undo path. Their column widths are stored in the model. Narrow
windows stack columns vertically. Toolbars, slash commands and block gestures
invoke the same engine operations as the command palette.

IME preedit edits a temporary model. The save path reads the committed snapshot
until the composition commits as one transaction. Clipboard data includes a
validated native fragment plus Markdown, semantic HTML and a Markdown text
alternative (verbatim selections keep their source). Whole selected tables and
columns retain their structure. Markdown pastes recognize headings, lists,
tables and code while literal editing contexts preserve source.

Markdown parsing is an interchange adapter in `internal/editor/markdown.go`.
Existing Markdown opens in memory. The first save after an edit publishes a
new, uniquely named `.arcmd` file in the same folder. The original Markdown is
retained, including a recovery copy in `importSource`. Navigation, pins and the
active filename follow the newly saved native document. A failed migration
leaves the original path and unsaved model available for retry.

Native decode rejects unknown versions, fields, block types and invalid IDs or
table shapes. The app displays a read error and disables editing for that file.
Exports create a separate `.md` with a unique name. Unsupported imported HTML
and extensions are opaque text blocks. The editor supports merged table cells,
cell backgrounds, and structured HTML clipboard imports. Block formulas use
`internal/tex` for layout with embedded KaTeX fonts. Following Textus, formulas
are centered without a background at rest; focusing one reveals a bordered
source panel and a separate live preview with reserved space below. Complete
`$$` or `\[...\]` clipboard formulas import as math, and a copied math block
retains its type. Typing `$$` then Enter opens a math block; Enter on a closing
`$$` line returns to a paragraph. Inline `$...$` formulas use the same fonts in
text style. A retained layout wrapper maps model UTF-16 ranges to native object
positions for selection, caret placement, search and print output. Clicking a
formula reveals source until the caret leaves. Escaped dollars retain a literal
mark. This is native
Go rendering of the supported TeX subset, not the KaTeX JavaScript engine.

Autosave waits 700 ms using the frame clock. Writes use atomic replacement.
New files use atomic exclusive publication to avoid import/export collisions.
Switching documents, switching Spaces and closing the window flush first.

### Spaces

A space is a folder plus a name, glyph and color, kept in
`<config>/ArcMD/spaces.json` with the order, the active space, pins and the
last open document of each. The same file keeps bookmarks: places marked in
documents by top-level block ID, with an excerpt that finds the block again
in a Markdown file read afresh. They live outside the document so marking a
place never edits or converts it. Switching a space swaps the document list, the
theme's accent and the chrome tint, and reopens the space's last document.

### Moving documents and links

`relocate.go` moves files and folders with their local image and cover resources.
Shared source attachments remain available. Conflicting destination pictures
receive distinct names, and URLs are rebased in current content, composition,
undo/redo states and saved versions. A resource failure rolls back the file move.
Markdown import sources retain their original bytes; rebasing image references
requires converting to the native format first. Existing broken image references
are not repaired by moving a document.

Local images resolve relative to the document's directory, including URL-encoded
names and `../images` resources outside its Space. This is a read-only exception:
resource writes remain inside the destination Space, using `assets/` and rebasing
native document URLs when a parent reference would escape. GIFs show their first
frame.

`editor/images.go` owns image layout, recovery cards, focus targets and the
image preview. Figures follow Textus: natural size, centered in the column,
rounded corners and a soft shadow. Alt text remains in the model and is shown
in a failed-image card or while explicitly editing the description. Recovery
cards distinguish missing files, unreadable files, decode failures and unsupported
references, wrapping their actions on narrow panes. `imageactions.go` binds the
file picker to the original document and stable block ID; a replacement retains
the description and is one undo transaction. File-manager actions locate the
nearest existing parent when a picture is missing. Printing omits recovery
actions and limits a tall picture to the page height.

History transfer merges timestamp collisions without overwriting snapshots.
Source history is removed only after the destination has been populated. Links
resolve relative to their source document's space, then the saved space order.
Rename captures that resolution first, so another space's same-named document
keeps its links. Unopened native documents receive conflict-checked saves.

### System file opening

MyGo's `OnOpenFile` handles installed file associations and Dock drops. A single
instance forwards later opens to the existing window. Development command-line
runs also accept document paths. The deepest matching Space owns an opened file;
an external file's parent folder is registered as a new Space. Opening alone
never converts Markdown or copies the file, and save conflicts block navigation.

### PDF export and printing

An isolated editor snapshots the document and paginates it on A4 paper. The UI
renders one page at a time; native runs encode and write it on a worker before
scheduling the next page. The progress dialog allows cancellation, which removes
its temporary output. A completed export is published under an exclusive,
numbered filename, never over an existing PDF. Printing opens a unique temporary
PDF in the system viewer.

`internal/pdf.Writer` streams JPEG pages and retains only object offsets and a
Unicode alphabet. An invisible Unicode text layer uses the editor's cluster
positions for selection, copying and search; formula artwork and image pixels
remain raster content. This is not a tagged PDF or a fully vector export.

## Plugins

See [plugins.md](plugins.md).

### Reading and inline editing

Document version 6 adds `Style.Script` (`sup`/`sub`), `Style.NoMath` for literal
Markdown dollars, `Block.ImageWidth`, and footnote blocks with `NoteLabel`.
Versions 1–5 migrate in memory; older versions claiming newer attributes are
rejected. Footnote definitions keep styled text, references stay lossless in
runs, and numbering follows first reference order. Jump/return remembers the
originating reference. Scripts use native inline objects per grapheme because
MyGo 0.7.4 measures baseline shifts but its glyph painter omits the displacement.
The layout mapping preserves selection boundaries and the PDF text layer.

`linkpopover.go` edits a captured range beside the link. A revision check
cancels stale edits. Local wiki previews wait 450ms, prefer retained live
editors, and disappear on scrolling or Escape. They never create documents.
`autopair.go` tracks only generated closers, wraps existing styled selections,
and pairs IME text only on commit. Paste bypasses pairing.

Image resizing previews a width outside the document and commits once on
release; arrow keys resize and Home restores natural size. The preview supports
5%–800% continuous zoom at the pointer, pan, fit and original-size controls.
Reading font size and line height are global preferences, separate from fixed
PDF paper metrics. Each retained editor has a session reading lock. The engine
rejects text and structural transactions, composition, undo and redo while
locked; selection, copy, search and navigation remain available.
