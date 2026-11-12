# Editor algorithm optimization — 2026-11-13

This change removes repeated full-prefix UTF-16 conversion, makes footnote references incremental, and extends local layout to headings and top-level tables. The comparison below reruns the pre-change and post-change binaries in this session; its baseline is the user-optimized version reviewed earlier on November 13, not the older November 12 regression.

## Method

Apple M5, 24 GiB, macOS 26.5.1, Go 1.27.3, MyGo v0.7.4, CGO disabled. Identical fixture bytes and timing adapter; 1280 × 800 DIP, scale 2. Each case uses three fresh processes per version and 100 operations per action. Before and after alternate, reversing order in the second round. All 54 processes ran serially, separately from compilation, checks and profiling. Quantiles use round((n−1) × q), followed by the median of the three round quantiles. Normal desktop background activity remains; small differences are not statistically established regressions or gains.

Timing covers CPU event dispatch and scene construction. It excludes rasterization, GPU presentation and display latency. Allocations are decimal MB per operation, not resident memory.

## Million-character editing

All values are milliseconds. These diagnostics buffer samples, take no screenshots, and collect garbage before timing. They are compared only with the same diagnostic harness.

| Case | Action | Before total P50 | After total P50 | Reduction | Before total P95 | After total P95 | Before MB/op | After MB/op |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| control | type | 1.011 | 0.928 | 8.2% | 1.143 | 1.126 | 2.836 | 2.836 |
| control | backspace | 0.935 | 0.887 | 5.2% | 1.113 | 1.071 | 2.850 | 2.850 |
| tail | type | 2.698 | 0.913 | 66.2% | 3.024 | 1.061 | 2.914 | 2.914 |
| tail | backspace | 27.180 | 0.867 | 96.8% | 29.034 | 1.077 | 2.878 | 2.878 |
| footnote | type | 4.349 | 0.965 | 77.8% | 6.849 | 1.206 | 15.554 | 2.844 |
| footnote | backspace | 4.261 | 0.884 | 79.3% | 6.917 | 1.106 | 15.569 | 2.860 |
| heading | type | 49.203 | 1.546 | 96.9% | 53.007 | 1.777 | 116.434 | 2.842 |
| heading | backspace | 48.728 | 0.882 | 98.2% | 51.077 | 1.165 | 116.447 | 2.832 |
| cell | type | 59.709 | 1.499 | 97.5% | 76.409 | 2.083 | 116.495 | 2.949 |
| cell | backspace | 58.928 | 1.336 | 97.7% | 66.275 | 1.821 | 116.442 | 2.895 |
| pause | type | 1.216 | 0.960 | 21.0% | 2.013 | 1.673 | 19.667 | 19.667 |
| pause | backspace | 1.165 | 1.017 | 12.7% | 2.047 | 1.764 | 19.681 | 19.680 |

Control edits the first paragraph; tail edits the last paragraph; heading edits the first section heading, excluding the title; cell edits the first table cell; footnote adds one definition and one reference to the fixture. Pause advances virtual time 500 ms after each edit and measures one additional frame; it does not sleep. Its allocation includes the delayed frame, while the total timing above excludes it. The delayed frame remains about 1.37 ms after insertion and 1.36 ms after backspace.

The cell diagnostic showed substantial between-round CPU variation (post-change typing P50 2.028, 1.499 and 0.981 ms); the large allocation and latency reduction is consistent, but the absolute number should not be treated as a guarantee.

## Original matched workload

The existing harness inserts 100 characters at the end of the first paragraph, deletes them, and scrolls 100 × 40 DIP. Below are CPU-frame times, excluding event dispatch.

| Size | Action | Before frame P50 | After frame P50 | Before frame P95 | After frame P95 |
|---|---|---:|---:|---:|---:|
| 10k | type | 0.239 | 0.236 | 0.278 | 0.269 |
| 10k | backspace | 0.237 | 0.221 | 0.286 | 0.243 |
| 10k | scroll | 0.303 | 0.297 | 0.929 | 0.885 |
| 100k | type | 0.281 | 0.270 | 0.325 | 0.308 |
| 100k | backspace | 0.280 | 0.269 | 0.306 | 0.290 |
| 100k | scroll | 0.360 | 0.357 | 0.945 | 0.944 |
| 1m | type | 0.797 | 0.747 | 1.035 | 0.908 |
| 1m | backspace | 0.781 | 0.711 | 0.997 | 0.984 |
| 1m | scroll | 0.782 | 0.801 | 1.458 | 1.498 |

Million-character scroll P50 increased by 0.019 ms (2.4%), with P95 up 0.040 ms. This small difference remains within the observed desktop-run variation; it is retained here rather than reported as an improvement. Initial million-character open plus first scene is 848.81 → 845.14 ms; this is not cold application startup. Normal typing allocation remains 2.836 MB/op.

## Implementation and limits

- The engine leaf cache stores both UTF-16 and byte coordinates. Range conversion and buffer splicing use the containing leaf, retaining surrogate-pair clamping and grapheme deletion. Text edits update the cache; structural changes and composition rebuild as needed.
- Footnote definitions and effective references are cached by block identity. Only the edited block is parsed; numbering changes when reference order changes. Formatting, structural edits and missed revisions trigger a full rebuild.
- Heading edits reshape the heading and update its outline label. Table-cell edits rerun the existing merged-cell layouter for that top-level table and translate subsequent geometry. Columns, nested tables, hidden targets, structural changes and incompatible layout state retain full layout.
- Saved document format, input composition and undo semantics are unchanged. No dependencies were added.

## Remaining algorithm work

A separate 1,000-insertion + 1,000-backspace control profile attributes **93.8% of sampled allocated bytes to `Buffer.sync`**, which still copies the complete flat text after every edit. Allocation profiles subtract the setup profile; these profiled timings are excluded from all tables.

1. Replace the eager flat-string mirror with a block-backed text view. Serve native text ranges from the affected leaves and materialize full text only when required, caching it by committed revision. MyGo’s `TextInputClient` requests ranges, so full-document text is not required for each native input event. A bounded chunk/piece structure within very long blocks can follow after measuring that workload.
2. Replace eagerly updated absolute leaf offsets with aggregate byte/UTF-16 lengths. A Fenwick tree can accelerate stable-block text edits; a balanced sequence tree can support frequent structural insertion. Both need position APIs so consumers do not retain stale absolute fields.
3. Store block-local layout coordinates plus indexed block heights. Update height aggregates after edits and traverse only visible top-level containers for painting/hit testing, with container-local indexes for tables and columns. This removes the remaining full tail-coordinate update and full-row scans.
4. Measure huge single paragraphs, large tables, frequent structural edits and IME-heavy input before further changes. Paragraph line reuse must respect shaping, bidirectional text and grapheme boundaries. Structural undo could eventually share unchanged subtrees; ordinary text undo already records only the affected runs.

## Validation

- `CGO_ENABLED=0 go test ./...` and `CGO_ENABLED=0 go vet ./...` passed.
- `CGO_ENABLED=0 GOOS=linux go build ./...` and `CGO_ENABLED=0 GOOS=windows go build ./...` passed.
- `gofmt -l .` returned no paths. Two existing ignored size-analysis helpers in `dist/` were formatted without behavioral changes.
- Tests compare cached positions with serialized text during randomized edits and nested transactions, including undo/redo and native round trips. Native input tests cover long-document emoji/combining-character deletion and IME commit/undo.
- Footnote tests cover reference reorder, deletion, literal/code/link exclusions, new definitions and randomized edits against a complete index.
- Incremental layouts are compared with complete layouts, including byte/UTF-16 positions, controls and rendered pixels. Empty/wrapped headings and cells spanning rows and columns also compare geometry; a 0.001 DIP tolerance allows float32 translation rounding.
- All 54 benchmark processes verified exact inserted text and restoration; matched runs verified 4,000 DIP scrolling.
- The complete app Snapshot suite passed. Representative light/dark headings, merged tables and footnotes were inspected.

## Evidence and reproduction

- [Summary, source hashes, binary hashes and fixture hashes](comparison.json).
- [All individual samples and test logs](samples.json).
- [Post-change allocation profile](profile-control/alloc-top.txt) and [CPU profile](profile-control/cpu-top.txt), with raw profiles alongside.
- [Merged table snapshot](table-merge-dark.png), [heading/table snapshot](writing-wide.png), [footnote snapshot](reading-light.png).
- Reuse [the prior diagnostic recipe](../2026-11-13/report.md#reproduction) and [the standard harness](../../../tools/perf/README.md). The diagnostic test and timing adapter are unchanged; only the production sources differ. Build both binaries first, then run three fresh processes per version per case, alternating order as described above.
