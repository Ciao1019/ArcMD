# Changelog

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
