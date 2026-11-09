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
| `sidebar.go` | Pinned tiles, space header, folder tree (open, rename, drag to move, menus), space bar |
| `icons.go` | Lucide icons |
| `palette.go` | Command bar: actions, fuzzy ranking, keyboard navigation |
| `dialogs.go` | New/edit space, plugins |
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
validated native fragment plus plain text and Markdown alternatives.

Markdown parsing is an interchange adapter in `internal/editor/markdown.go`.
Existing Markdown opens in memory. The first save after an edit publishes a
new, uniquely named `.arcmd` file in the same folder. The original Markdown is
retained, including a recovery copy in `importSource`. Navigation, pins and the
active filename follow the newly saved native document. A failed migration
leaves the original path and unsaved model available for retry.

Native decode rejects unknown versions, fields, block types and invalid IDs or
table shapes. The app displays a read error and disables editing for that file.
Exports create a separate `.md` with a unique name. Unsupported imported HTML
and extensions are opaque text blocks. Formulas currently expose TeX source.
Merged table cells and external HTML clipboard conversion are future work.

Autosave waits 700 ms using the frame clock. Writes use atomic replacement.
New files use atomic exclusive publication to avoid import/export collisions.
Switching documents, switching Spaces and closing the window flush first.

### Spaces

A space is a folder plus a name, glyph and color, kept in
`<config>/ArcMD/spaces.json` with the order, the active space, pins and the
last open document of each. Switching a space swaps the document list, the
theme's accent and the chrome tint, and reopens the space's last document.

## Plugins

See [plugins.md](plugins.md).
