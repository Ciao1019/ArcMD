The native mind map layout, branch palette, curves, and fold controls in
`internal/mindmap` are adapted from [mygo-native-ui/ui/mindmap](https://github.com/xingkaixin/mygo-native-ui/blob/022d1c661a7832dfc45350c24420eea7c03e53ec/ui/mindmap/mindmap.go).
ArcMD supplies stable document block IDs, rich node bodies, and cursor navigation.

[StackMind's Markdown map](https://github.com/kvaps/stackmind/blob/6e4f9b70f0be730242b52cb1a17e4cebbdb978c1/src/components/MindMap.vue)
informed the content-sized nodes, wrapping ceiling, and spacing. ArcMD draws
the rich content with its own editor and uses MyGo for the native controls.
