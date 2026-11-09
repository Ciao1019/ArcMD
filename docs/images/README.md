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
- `download-{macos,windows,linux}-{en,zh-CN}.svg` are the platform download
  buttons: plain white, a thin border and a monochrome glyph, 200×56. The Apple glyph path is adapted from
  [Quiet.md](https://github.com/s1dashu/quiet-md/tree/main/docs/images) (MIT,
  preserved in [LICENSE-quiet-md.txt](LICENSE-quiet-md.txt)). Platform names and
  logos belong to their respective owners.

The README badges use [Shields.io](https://shields.io/). Download buttons link to
ArcMD Releases until the first packages are published.
