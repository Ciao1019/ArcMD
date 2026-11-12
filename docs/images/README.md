# README artwork

- `readme/hero-*.svg`, `readme/library-*.svg`, `readme/sidebar-*.svg` are hand-drawn
  animated SVGs of ArcMD's interface, using the colors of two space themes
  (`#F21158 · #F25F11 · #F211EF` and `#F2410F · #F2D80F`). Motion is plain CSS
  `@keyframes` inside each file, so it plays wherever the SVG is shown as an
  image (including GitHub), and stops under `prefers-reduced-motion`. Text is
  set in the system font; English and Simplified Chinese have separate files.
  They use no SVG filters (no blur, drop shadow, or turbulence grain): an
  animated image is repainted every frame, and filters made the animations
  stutter. Glows are radial gradients and shadows are stacked translucent shapes.
- `readme/spaces-*.svg`, `readme/themes-*.svg`, and
  `readme/bookmarks-*.svg` use screenshots captured on 2026-11-12 from the
  installed `/Applications/ArcMD.app` client (0.0.7). No development client,
  `ARCMD_HOME`, or headless rendering was used for these captures. Public demo
  text was entered into the native client; the original spaces and themes were
  restored after capture. English captions accompany the same Chinese UI.
  Captures are cropped and downsampled, then embedded as JPEGs. CSS cross-fades
  and step indicators work in an SVG image without JavaScript or external assets.
  Reduced motion keeps the first frame and its caption visible.
  The space demo switches between the two captured spaces; the theme demo shows
  three single colors, two- and three-color gradients, intensity changes, and
  dark appearance. The bookmark demo shows adding, listing, jumping, and searching.
- The theme graphic's **Material illustration** panel is separate vector art.
  Window-only capture did not preserve the desktop behind the macOS material,
  so the panel compares a clear background with a blurred, tinted version.
  Only this small illustration uses an SVG blur filter; the app screenshots
  are not blurred or recolored. ArcMD has an intensity control, not an independent
  blur-radius or opacity slider. The illustrated single-color coverage uses
  the application's `0.19 + 0.55 * intensity` relationship.
  Source-frame hashes, crop rectangles, dimensions, and animation durations are
  recorded in [`readme/live-capture.json`](readme/live-capture.json).
  With the named 2480×1600 native captures in a folder, regenerate using Python 3
  and ImageMagick:

  ```sh
  python3 scripts/readme_live_art.py --capture-dir /path/to/native-captures
  ```

  `internal/app/readme_art_test.go` remains a separate fixture-rendering utility;
  point its `ARCMD_README` output at a temporary directory, not these published
  assets.
- `download-{macos,windows,linux}-{en,zh-CN}.svg` are the platform download
  buttons: plain white, a thin border and a monochrome glyph, 200×56. The Apple glyph path is adapted from
  [Quiet.md](https://github.com/s1dashu/quiet-md/tree/main/docs/images) (MIT,
  preserved in [LICENSE-quiet-md.txt](LICENSE-quiet-md.txt)). Platform names and
  logos belong to their respective owners.

The README badges use [Shields.io](https://shields.io/). Download buttons link to
ArcMD Releases until the first packages are published.
