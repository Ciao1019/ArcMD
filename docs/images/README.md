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
- `readme/themes-*.svg` and `readme/bookmarks-*.svg` use the same vector
  illustration style, 1280 × 820 canvas, window proportions, system fonts,
  and CSS easing as the hero. The installed `/Applications/ArcMD.app` is
  the UI reference. Color stops, handles, pointer, opacity, scroll position,
  and bookmark entries animate continuously; there are no embedded bitmap
  frames, scripts, external resources, or SVG filters. Frosted material is
  illustrated with soft radial gradients; the intensity control changes
  the tint's opacity. It does not imply an independent blur control.
  Both loops return to their starting state and provide a composed static
  illustration under `prefers-reduced-motion`.
  Preview the localized files as `<img>` elements, as GitHub does.
  The older `TestReadmeArt` renderer is only a raster
  reference utility; direct its output to a temporary folder rather than
  replacing these vector assets.
- `readme/annotations-{zh-CN,en}.svg` illustrate hand-drawn text annotations in
  the same window style. The 26-second loop selects seven phrases, opens the
  12-style picker, and draws a wave, circle, highlight, double underline,
  cross, arrow, and box. The completed page rests before fading back to the
  start. It is a vector illustration, not a screen recording. The picker is
  placed beside the page so the strokes remain visible.
  The paths and annotation geometry come from `internal/editor/annotate.go`;
  their Opensource UI MIT notice is included in each SVG. There are no filters,
  raster frames, scripts, fonts to download, or external resources. Reduced
  motion shows all completed marks and the picker without a pointer or selection.
  The accompanying `readme/annotations-demo-{zh-CN,en}.arcmd` files contain
  the original sample copy and marks, ready to open in ArcMD.
  Regenerate both languages and sample documents from the repository root:

  ```sh
  CGO_ENABLED=0 go run ./tools/readme-art
  ```

  The generator reads the existing localized bookmark illustrations for their
  window shell, and uses only Go's standard library and the document model.
- `download-{macos,windows,linux}-{en,zh-CN}.svg` are the platform download
  buttons: plain white, a thin border and a monochrome glyph, 200×56. The Apple glyph path is adapted from
  [Quiet.md](https://github.com/s1dashu/quiet-md/tree/main/docs/images) (MIT,
  preserved in [LICENSE-quiet-md.txt](LICENSE-quiet-md.txt)). Platform names and
  logos belong to their respective owners.

The README badges use [Shields.io](https://shields.io/). Download buttons link to
ArcMD Releases until the first packages are published.
