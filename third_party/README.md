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

Icons are Lucide, under the ISC License. Attribution appears in
`internal/app/icons.go`; `internal/app/spacesymbols.go` is generated from
Lucide 1.70.0.
