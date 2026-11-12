# Changelog

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
