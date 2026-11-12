# Changelog

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
