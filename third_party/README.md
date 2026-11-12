# Dependencies and attribution

Prism uses `github.com/egoist/mygo` v0.7.4 for native windows, UI primitives,
text shaping and system input transport. Its module cache stays untouched.
The Markdown editor is implemented in `internal/editor` and has no vendored
editor component or TeX engine.

The Space theme algorithms and theme editor follow Cra, under the MIT License,
Copyright (c) 2026 Victor Kildahl. Their implementation lives in
`internal/space/theme.go` and `internal/app/spaceeditor.go`. The space icon
picker (`internal/app/spaceicons.go`) follows Cra's, and its emoji table
(`internal/app/emojidata.go`) is generated from Cra's emoji catalog, which Cra
makes from Unicode's emoji-test.txt.
The sidebar's favorites (`internal/app/favorites.go`), its list dragging
(`internal/app/treedrag.go`), moving rows to another space through its
icon (`internal/app/crossspace.go`) and the paging between spaces
(`internal/app/spaceswitch.go`) port Cra's FavoritesGridView,
SpaceListView and SpacePagerView / PagedGradientView, with their
measurements of Arc.

The split layout, pane toolbar, divider behavior and segmented group row
(`internal/app/split.go`, `splitview.go`) follow Cra's SplitPaneLayout,
SplitDraggerHandleView, PaneToolbarView and split sidebar interaction.
The software update banners, toast and update window
(`internal/app/updateview.go`, `internal/update`) port Cra's UpdateBanner,
UpdateNotice, ShinyToast and UpdaterWindow, with their measurements of Arc.
The full MIT notice is in `cra/LICENSE`.

[Cra source](https://github.com/VictorKildahl/cra)

The image figures and missing-image recovery cards (`internal/editor/images.go`)
and formula presentation (`internal/editor/math.go`) follow Textus's image view,
missing-image cards, unboxed display math, source panel and live preview design,
under the MIT License,
Copyright (c) 2026 CatVinci Studio. They are implemented with native Go drawing
and controls. Inline math, clipboard formats, link popovers, image resizing,
bracket pairing, footnotes and reading controls also use Textus as a behavior
reference, implemented against ArcMD’s own document model. The MIT notice is
in `textus/LICENSE`.

[Textus source](https://github.com/CatVinci-Studio/Textus)

The hand-drawn text annotations (`internal/editor/annotate.go`) port Opensource
UI's AnnotatedText, under the MIT License, Copyright (c) 2026 Bidyut Kundu.
Its SVG paths are kept; its turbulence filter is replaced by a seeded
displacement of the flattened strokes, drawn natively. The MIT notice is in
`opensourceui/LICENSE`.

[Opensource UI source](https://github.com/bidyut10/opensourceui)

Formula presentation was checked against Textus commit
`21dc743b84c5c8f518cfdc95c9654fe31b61507e` (its math preview plugin and editor CSS).
The native renderer embeds the unmodified `KaTeX_Main-Regular.ttf` and
`KaTeX_Math-Italic.ttf` faces from KaTeX commit
`9b2280dee4acb4da7d866dbaccad120b00949c27` in `internal/editor/fonts/`.
These fonts are under the SIL Open Font License 1.1, with copyright and reserved
font names retained in `katex/OFL.txt` and in the font metadata. KaTeX's upstream
MIT notice is also retained in `katex/LICENSE`; its JavaScript renderer is not
included. Formula parsing and layout remain in ArcMD's `internal/tex` package.

[KaTeX source](https://github.com/KaTeX/KaTeX)

Icons are Lucide, under the ISC License. Attribution appears in
`internal/app/icons.go`; `internal/app/spacesymbols.go` is generated from
Lucide 1.70.0.
