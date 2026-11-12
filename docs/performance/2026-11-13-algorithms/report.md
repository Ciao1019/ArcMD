# Editor algorithm optimization — 2026-11-13

This round compares against the already optimized editor from [the preceding round](../2026-11-13-optimized/report.md), using its exact binary and source hashes. Existing unrelated workspace edits are retained. No document format, dependency, UI design, shortcut or intended interaction changed.

## Measurement

Apple M5, 24 GiB, macOS 26.5.1, Go 1.27.3, MyGo v0.7.4, `CGO_ENABLED=0`. Identical synthetic fixtures, 1280 × 800 DIP, scale 2. Three fresh processes per version per case, 100 operations per action, serial runs alternating before/after (reversed in round 2). Each statistic is the median of the three per-round quantiles, using `floor((n-1)*q + .5)`; samples are not pooled. Compilation, tests, profiling and screenshot rasterization are outside the timed runs. Desktop background activity is uncontrolled.

Timings measure native event handling plus CPU scene construction; they do not measure GPU presentation or end-to-end display latency. The fixtures contain many blocks; results do not represent a single million-character paragraph. All ordinary typing/backspace runs assert exact text and restoration; matched scrolling asserts 4,000 DIP.

## Million-character results

Combined event + scene CPU time, milliseconds. The first six workloads use the unchanged diagnostic harness.

| Workload | Input P50 before → after | Input P95 before → after | Backspace P50 before → after | Input allocation MB/op before → after |
| --- | ---: | ---: | ---: | ---: |
| Body | 0.914 → 0.275 | 1.074 → 0.319 | 0.862 → 0.311 | 2.836 → 0.067 |
| Document tail | 0.963 → 0.327 | 1.157 → 0.381 | 0.871 → 0.385 | 2.914 → 0.145 |
| Body with footnote | 0.959 → 0.298 | 1.278 → 0.362 | 0.887 → 0.354 | 2.844 → 0.075 |
| Heading | 1.273 → 0.594 | 1.483 → 0.670 | 0.854 → 0.318 | 2.843 → 0.073 |
| Table cell | 0.901 → 0.288 | 1.056 → 0.592 | 0.814 → 0.311 | 2.949 → 0.180 |
| Input with 500 ms idle frame | 0.964 → 0.290 | 1.525 → 0.549 | 0.997 → 0.327 | 19.666 → 5.405 |

Body input CPU P50 falls by 69.9%; allocation falls by 97.6%. The pause row's allocation includes its delayed frame. Delayed-frame CPU time is recorded separately, rather than hidden inside the input statistic.

| Additional workload | CPU P50 before → after (ms) | CPU P95 before → after (ms) | Allocation MB/update before → after |
| --- | ---: | ---: | ---: |
| Typing with find query `text` open | 15.745 → 0.281 | 19.237 → 0.322 | 29.449 → 0.066 |
| Repeated IME candidate update | 91.048 → 0.264 | 96.943 → 0.307 | 273.012 → 0.059 |

IME measurement begins after composition starts and alternates two candidate strings through native `TextComposition` events. It asserts that committed text stays unchanged and marked display text is present. The two JSON batches retain the harness keys `type` / `backspace`, but both are candidate-update batches. These figures exclude composition start and final commit, which still retain full snapshots.

## Matched workloads across sizes

CPU scene time only, matching the preceding reports.

| Size | Input P50 before → after (ms) | Backspace P50 before → after (ms) | Scroll P50 before → after (ms) | Open CPU before → after (ms) |
| --- | ---: | ---: | ---: | ---: |
| 10k | 0.240 → 0.227 | 0.234 → 0.220 | 0.312 → 0.307 | 33.03 → 31.09 |
| 100k | 0.273 → 0.238 | 0.270 → 0.236 | 0.363 → 0.342 | 111.25 → 111.88 |
| 1m | 0.749 → 0.273 | 0.703 → 0.271 | 0.786 → 0.548 | 852.15 → 856.68 |

Small-document timing is broadly similar; opening remains a whole-document operation and gains no material speedup. Full P95, per-round samples, allocations and GC counts are in the JSON artifacts. Small changes in low-latency tails should not be interpreted as a guaranteed improvement in every workload.

## Algorithms changed

- **Text positions:** two Fenwick trees store UTF-16 and UTF-8 leaf lengths. Text updates and prefix/containing-leaf lookups are O(log B), where B is the number of leaves. Stable-ID maps avoid repeated scans. Structural transactions rebuild the index so intermediate edits remain correct.
- **Text access:** native input ranges and word movement read only relevant leaves. Ordinary typing no longer materializes the flattened document. Full text is cached on demand. MyGo's lazy accessibility callback still supplies current display/preedit text; its internal node type is handled by a narrowly scoped, typed and tested reflection bridge.
- **Layout and painting:** retained rows resolve text offsets lazily. Unchanged-height edits skip suffix geometry translation. Bounding trees prune visible rows/decorations and nearest-row candidates while retaining paint order, column/merged-cell behavior, exact leaf distances and tie ordering. Height-changing edits refit the displayed float32 rectangles to avoid accumulated hit-test rounding differences.
- **Find:** Unicode KMP replaces repeated-prefix matching and per-character position arrays. A consecutive text edit rescans the changed leaf and shifts later matches, preserving case folding and nonoverlapping matches. Visible-row highlighting binary-searches relevant matches. Updating later matches is still O(M) in the number of subsequent matches.
- **IME and save state:** repeated preedit updates publish local display revisions without committed versions or undo entries. The dirty-state cache distinguishes committed versions from display revisions, including saving while composition is active.
- **History, folding and outline:** bounded undo eviction clears/reslices instead of copying every entry; block IDs locate text history. Folding visibility is cached across text edits. The long outline stays virtualized during drag, with the keyed offscreen source retained until drop/cancel. Handle and decoration lookup use ID maps.

## Compatibility validation

- `gofmt -l .` produced no paths; `CGO_ENABLED=0 go vet ./...` and `CGO_ENABLED=0 go test ./...` passed.
- Linux and Windows cross-builds passed with `CGO_ENABLED=0`.
- The app Snapshot suite passed and generated 239 PNGs. Light footnotes, dark merged tables, narrow writing and dark columns were inspected. Representative images are archived below.
- Across 27 matched before/after screenshot pairs, 21 are pixel-identical. The remaining six typed screenshots differ only in the blinking caret: a 3 × 52 pixel rectangle at `(1276,645)-(1279,697)`. All initial and scrolled pairs are identical.
- Model tests cover ranges, UTF-16 surrogate boundaries, byte offsets, structural round trips, nested transactions, randomized operations, and the newest 200 undo entries.
- Differential tests compare word movement, line ranges and double-click selection against the old flattened-string algorithms, including CRLF, emoji, combining marks, empty leaves and table separators.
- Spatial tests compare indexed queries with exhaustive scanning, repeated wrapping edits, merged tables/columns, fractional and large coordinates. Existing incremental-layout tests compare geometry and pixels with complete layouts.
- UI tests cover cross-viewport selection, long-outline drag/drop/cancel/undo, preedit commit/cancel/undo/save, and the full 26 DIP footnote return target with small fonts. Accessibility callback tests verify current preedit text without forcing whole-document text on ordinary frames.
- KMP and incremental-find tests compare against the previous matcher/full search over randomized Unicode queries, edits, undo/redo and repeated-prefix adversarial input.

## Remaining costs

Height-changing edits still translate following cached geometry in O(B). A long changed paragraph is shaped in full; a changed table rebuilds its own cells. Column edits, structural transactions, composition start/commit and structural undo retain full-layout/snapshot fallbacks. Initial find, full-text export and an accessibility request for the entire document necessarily read more content. These costs were preserved rather than changing interaction semantics. The separate post-change profile is diagnostic only and is excluded from comparison statistics.

In the separate 1,000-insertion / 1,000-backspace allocation profile, MyGo's text shaping accounts for approximately 63% of cumulative allocated bytes. Native shaping/boundary arrays, outline controls and page-style construction remain visible costs; the former per-keystroke whole-document flattening and offset rewrites are no longer the dominant path.

## Evidence and reproduction

- [Comparison and source/binary/fixture hashes](comparison.json), [standard raw samples/logs](samples.json), [find/IME raw samples/logs](algorithm-samples.json).
- [Only this round's source/test changes](changes.patch), relative to the pre-round dirty workspace.
- [Pixel comparisons](pixel-comparison.json), [dark merged table](table-merge-dark.png), [light footnotes](reading-light.png), [narrow writing](writing-narrow.png), [dark columns](columns-typing-dark.png).
- [CPU profile](profile-control/cpu-top.txt), [allocation profile with setup subtracted](profile-control/alloc-top.txt), and raw profiles in the same directory.
- [Exact additional benchmark harness](benchmark-harness.go.txt) and [temporary MyGo timing-adapter patch](timing-adapter.patch). Apply the adapter patch only to a temporary MyGo copy, use a temporary modfile/replace and a Go build overlay; do not change the installed module or shipping dependencies. The harness replaces `internal/app/performance_test.go` in that overlay.

Follow [the standard fixture/timing preparation](../../../tools/perf/README.md) and [the earlier diagnostic recipe](../2026-11-13/report.md#reproduction). Compile both source versions with `CGO_ENABLED=0`, `-tags perfbench`, the temporary `-modfile` and `-overlay`. Use the same fixture hashes recorded here. Standard matched tests use `TestPerformance`; diagnostic and additional workloads use `TestPerformanceReview`. The latter accepts `ARCMD_REVIEW_MODE=control|tail|footnote|heading|cell|pause|find|ime`:

```sh
ARCMD_PERF_FIXTURE=/path/to/fixtures/1m.arcmd \
ARCMD_PERF_OUTPUT=/path/to/new/run \
ARCMD_PERF_SAMPLES=100 ARCMD_REVIEW_MODE=find \
  /path/to/benchmark.test -test.run '^TestPerformanceReview$' -test.v
```

Run each version three times in fresh processes, with the alternating order above. Keep profiling, compilation and tests separate. For a separate profile use `ARCMD_PERF_SAMPLES=1000`, `ARCMD_REVIEW_MODE=control` and `ARCMD_PERF_PROFILE=/path/to/profile/cpu.pprof`.
