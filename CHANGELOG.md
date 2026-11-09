# Changelog

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
