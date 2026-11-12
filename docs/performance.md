# Editor performance

ArcMD 0.0.11 uses indexed text positions, local layout, incremental search, and
viewport culling to keep ordinary editing responsive in long documents. This
report replaces the earlier README measurements with the latest optimized
results. All times below are CPU measurements, not display latency.

## Million-character document

The synthetic fixture contains approximately one million characters, 549
chapters, 500 tables, and 9,912 table cells. Each operation runs 100 times per
process in three fresh processes. Values are the median of the three per-run
P50 or P95 values; samples are not pooled.

| Operation | CPU scene P50 | CPU scene P95 | Event + scene P50 | Event + scene P95 |
| --- | ---: | ---: | ---: | ---: |
| Typing | 0.273 ms | 0.291 ms | 0.275 ms | 0.294 ms |
| Backspace | 0.271 ms | 0.306 ms | 0.316 ms | 0.357 ms |
| Scrolling | 0.548 ms | 1.132 ms | 0.551 ms | 1.135 ms |

The scene timer excludes input dispatch; the combined timer includes it.
Neither includes software rasterization, GPU presentation, or display latency.
These figures do not establish end-to-end input latency or measured display FPS.
The fixture has many blocks and does not represent one million-character paragraph.

Allocation volume is approximately **0.067 MB per insertion**, **0.081 MB per
backspace**, and **0.122 MB per scroll**, in decimal MB. This includes event
handling, scene construction, and small harness overhead; it is not resident
memory. Opening the full document takes about **857 ms** of measured CPU time
and still requires whole-document work.

## Input with search and composition

These additional workloads use the same million-character fixture and the
same three-process, 100-operation sampling method. They include event handling
and scene construction.

| Workload | CPU P50 | CPU P95 | Allocation per update |
| --- | ---: | ---: | ---: |
| Typing with find query `text` open | 0.281 ms | 0.322 ms | 0.066 MB |
| Repeated IME candidate update | 0.264 ms | 0.307 ms | 0.059 MB |

IME measurement begins after composition starts and alternates candidate
strings through native `TextComposition` events. It verifies that committed
text stays unchanged and the candidate text is displayed. Composition start
and final commit are excluded and still retain full snapshots.

## Scaling across document sizes

CPU scene construction P50, using matching fixtures and operations:

| Characters | Typing | Backspace | Scrolling | Open CPU time |
| --- | ---: | ---: | ---: | ---: |
| 10k | 0.227 ms | 0.220 ms | 0.307 ms | 31.09 ms |
| 100k | 0.238 ms | 0.236 ms | 0.342 ms | 111.88 ms |
| 1m | 0.273 ms | 0.271 ms | 0.548 ms | 856.68 ms |

## Algorithms

- Two Fenwick trees index UTF-16 and UTF-8 leaf lengths. Text updates and
  prefix/containing-leaf lookups take O(log B), where B is the number of leaves.
  Stable block-ID maps avoid repeated scans. Structural edits rebuild the index.
- Native input ranges and word movement read the relevant leaves. Full text is
  built on demand, and ordinary input no longer flattens the entire document.
- Local edits reuse retained layout. Unchanged-height edits avoid translating
  following rows. Spatial trees prune offscreen rows, decorations, and hit-test
  candidates while preserving paint order and selection behavior.
- Unicode KMP handles initial find; consecutive edits rescan the affected leaf
  and shift later matches. Visible highlighting binary-searches relevant matches.
- Repeated IME preedit updates publish local display revisions without committed
  versions or undo entries. Dirty state distinguishes preedit from saved content.
- Footnote references, word counts, folding, outline rows, and undo storage reuse
  indexes and revisions. Long-document footer indexing waits for typing to settle.

Height-changing edits still translate subsequent geometry in O(B). A changed
paragraph is shaped in full, and a changed table rebuilds its own cells. Column
and structural edits, composition start/commit, and structural undo retain
full-layout or snapshot fallbacks. Export and full-document accessibility
requests necessarily read more content. These results do not imply uniform
improvements for every workload.

## Environment and method

Apple M5, 24 GiB, macOS 26.5.1, arm64; 1280 × 800 DIP at 2× scale; Go 1.27.3,
MyGo v0.7.4, `CGO_ENABLED=0`.

The headless MyGo timing adapter dispatches real input events and calls the
normal runtime frame pipeline, excluding software rasterization and test-only
label collection. Screenshots are generated outside timed runs. The adapter
lives in a temporary module copy; shipping dependencies are unchanged.

Runs are serial and alternate the before/after binaries, reversing their order
in the second round. Compilation, tests, profiling, and screenshot generation
are outside the measurements. Desktop background activity is uncontrolled, so
small differences should not be treated as statistically established gains.
Every ordinary typing/backspace run checks exact text restoration; scrolling
checks a 4,000-DIP displacement.

The archived source manifests match the shipping implementation before the
release-only version and documentation changes. The latest comparison is
between ArcMD builds; the earlier Quiet.md comparison has not been rerun for
this release and is not presented as current evidence.

## Validation and reproduction

Formatting, vet, the full Go test suite, and Linux/Windows cross-builds pass with
cgo disabled. Differential tests cover UTF-16 boundaries, text navigation,
Unicode find, local layout, hit testing, selection, undo, and IME save state.
The algorithm comparison checked 27 screenshot pairs: 21 are pixel-identical,
and the other six differ only in the blinking caret.

See the [benchmark harness](../tools/perf/README.md) for fixture generation and
normal scene measurements. The archived algorithm report contains the extra
find/IME harness and exact commands. Raw records retain their original metadata
for reproducibility.

- [Latest comparison, source hashes, and fixture hashes](performance/2026-11-13-algorithms/comparison.json)
- [Standard per-operation samples and logs](performance/2026-11-13-algorithms/samples.json)
- [Find and IME samples and logs](performance/2026-11-13-algorithms/algorithm-samples.json)
- [Detailed algorithm comparison and reproduction](performance/2026-11-13-algorithms/report.md)
- [Pixel comparison](performance/2026-11-13-algorithms/pixel-comparison.json)
- [Diagnostic CPU profile summary](performance/2026-11-13-algorithms/profile-control/cpu-top.txt)
