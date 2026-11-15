# README artwork

All seven localized animated README illustrations (`zh-CN` and `en`) and the
accompanying annotation demo documents are generated deterministically from the
repository root:

```sh
CGO_ENABLED=0 go run ./tools/readme-art
```

## Assets in `readme/`

- `flow-{zh-CN,en}.svg` — **top-of-page hero animation** in `README.md` and
  `README.en.md`: gliding caret, live inline Markdown and `$…$` TeX capsule
  compilation, pipe-table unfolding with smooth lower-block displacement and
  `50% · 50%` magnetic snap, springy task checkboxes, and tilted Polaroid image
  drop.
- `themes-{zh-CN,en}.svg` — live Space Theme Studio: single-handle recoloring,
  1–3 stop vertical gradients, translucent frosted material, and Feishu-style
  dark paper (`#1F2023`).
- `library-{zh-CN,en}.svg` — `⇧⌘L` Library drawer with Personal, Work, and
  Reading themed Space cards.
- `sidebar-{zh-CN,en}.svg` — `⌘\` full-window focus, left-edge floating sidebar
  peek, and live hover miniature page thumbnail preview.
- `annotations-{zh-CN,en}.svg` & `annotations-demo-{zh-CN,en}.arcmd` — 12
  hand-drawn text mark styles read directly from `internal/editor/annotate.go`
  (retaining the Opensource UI MIT notice inside each SVG comment).
- `bookmarks-{zh-CN,en}.svg` — margin bookmarks, synced Outline entries, smooth
  jump highlight, and `⇧⌘D` cross-space search.
- `hero-{zh-CN,en}.svg` — Personal ⇄ Reading two-finger Space swipe and opening
  a global Favorite in place (kept generated so external links continue to
  resolve).

## Technical conventions

- **1280 × 820 vector stage**, system font stack, and pure CSS `@keyframes` so
  animations play inside `<img>` tags on GitHub without scripts, external fonts,
  or embedded raster frames.
- **No SVG `<filter>` elements** (blur, drop-shadow, or turbulence): glows use
  radial gradients and elevation shadows use stacked translucent rounded shapes
  so browser frame repaint stays stutter-free.
- **`prefers-reduced-motion`**: every SVG provides a composed, static showcase
  frame when motion is disabled.
- `download-{macos,windows,linux}-{en,zh-CN}.svg` are 200 × 56 monochrome
  platform download buttons; the Apple glyph path is adapted from
  [Quiet.md](https://github.com/s1dashu/quiet-md/tree/main/docs/images) (MIT,
  preserved in [LICENSE-quiet-md.txt](LICENSE-quiet-md.txt)).
