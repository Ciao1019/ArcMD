# Lobe UI streaming Markdown

The Q&A stream in `internal/app/qastream.go`, `qamarkdown.go` and
`qamarkdownview.go` ports the default `StreamingPlayground` behavior to MyGo's
native drawing and incremental CommonMark parser. No JavaScript, React,
webview, or additional runtime dependency is shipped.

Sources (MIT, copyright 2023 LobeHub):

- [Lobe UI](https://github.com/lobehub/lobe-ui/tree/b261c3b680a3f7b5787a0aca246e52078e75c6fd),
  `src/Markdown/demos/StreamingPlayground.tsx`, `src/Markdown/style.css`,
  `src/Markdown/SyntaxMarkdown/StreamdownRender.tsx`.
- [Streamdown](https://github.com/lobehub/streamdown/tree/a1fdebda9be6efcceb9a0e4bbafafd99f3501f3e),
  `src/useSmoothStreamContent.ts`, `src/useStreamQueue.ts`, `src/Streamdown.tsx`,
  `src/rehypeStreamAnimated.ts`, `src/StreamTail.tsx`, `src/styles.tsx`.

The port uses the balanced preset (48–96 ms commits, 120 ms target buffer,
220 ms input window, 360 ms settling, the original speed limits/EMA), character
birth timestamps, a 180 ms fade with cubic-bezier(0.33, 0, 0.67, 1), and the
chat variant's 14 px / 1.6 typography. Previously shown characters retain
their births, code skips character fades, and completed blocks arriving in
a batch fade as blocks. Citations remain native interactive links.

Native adaptations: MyGo's streaming parser heals incomplete Markdown instead
of remend/marked; list items and table rows stay individually virtualized;
colors and fonts come from ArcMD's theme; reduce motion disables buffering
and fading. Network completion leaves the display buffer and final fades
running; cancellation/error flush the authoritative raw answer. The upstream
large-append, completed-prefix and HTML bypasses are intentionally disabled:
real SSE providers can deliver an entire answer before a UI frame, whereas
the playground produces small packets. New live turns start with an empty
display buffer even when their request has already finished. The stop control
remains available while the display catches up.
The website's demo controls and HTML/React artifact execution are not included.
This is a native behavioral port, not the browser renderer or a pixel-identical
copy of its font rasterization.
