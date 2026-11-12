# Editor performance regression check — 2026-11-12

All nine measured CPU-frame P50 values increased against the optimized results saved on 2026-11-08. The largest regressions are on the million-character document: typing 26.2×, backspace 26.8× and scrolling 10.8× the previous frame time. An independent fourth million-character run reproduced the slowdown. No production code was changed.

## Comparable workload

- Same Apple M5, 24 GiB, macOS 26.5.1, Go 1.27.3, MyGo v0.7.4 and `CGO_ENABLED=0` as the saved baseline.
- Same source XML SHA-256 (`9aa051dd8d15f6a37e18d0f2a3ebd49fc3128c5ee645a33a5100709bd9ceabb7`) and complete fixture manifest, including all text, block and table counts. The original temporary fixture was regenerated from the upstream `b9bed5513a222292d92601c50bbd4e6850882dea` generator. Its unrelated chart call was omitted because Pillow was unavailable; exact source XML and text hashes match.
- Current working tree at HEAD `753a99b`, including existing uncommitted changes. Source hashes and binary hash are in `comparison.json`; source hashes were verified unchanged after measurement.
- Existing `TestPerformance` and timing adapter: 1280 × 800 DIP, scale 2, default light appearance; three fresh processes per size, 100 insertions, 100 backspaces, 100 downward 40-DIP scrolls. Runs were serial, with no concurrent builds or benchmarks from this task.
- Each reported P50/P95 is the median of the three per-round quantiles, using the harness's rounded `(n-1) × q` index. It is not a pooled percentile. Profiling and the independent confirmation are excluded.
- The baseline is historical, not a simultaneous rerun of the previous binary. Normal background desktop activity remained; the fourth run and allocation increases support a real regression, but the table is not a statistical confidence interval.
- CPU scene construction excludes rasterization, GPU presentation and display latency. Event handling is measured separately. Product features and default UI have evolved since the baseline; this compares the normal current product against its prior saved workload.

## CPU frame time

Lower is better. All times are milliseconds. Ratio is current / previous.

| Characters | Operation | Previous P50 | Current P50 | Ratio | Previous P95 | Current P95 |
|---|---|---:|---:|---:|---:|---:|
| 10k | type | 0.160 | 0.325 | 2.04× | 0.185 | 0.472 |
| 10k | backspace | 0.152 | 0.325 | 2.14× | 0.174 | 0.413 |
| 10k | scroll | 0.244 | 0.331 | 1.35× | 0.597 | 1.007 |
| 100k | type | 0.191 | 1.371 | 7.20× | 0.211 | 2.207 |
| 100k | backspace | 0.188 | 1.337 | 7.10× | 0.211 | 2.081 |
| 100k | scroll | 0.268 | 0.577 | 2.15× | 0.621 | 1.185 |
| 1m | type | 0.589 | 15.467 | 26.25× | 1.105 | 21.588 |
| 1m | backspace | 0.572 | 15.366 | 26.85× | 1.002 | 20.119 |
| 1m | scroll | 0.489 | 5.298 | 10.83× | 1.136 | 6.724 |

## Million-character event + frame and allocations

Allocation volume is decimal MB per operation, including event, frame and small harness bookkeeping. It is not resident memory.

| Operation | Previous total P50 | Current total P50 | Previous total P95 | Current total P95 | Previous MB/op | Current MB/op |
|---|---:|---:|---:|---:|---:|---:|
| type | 0.833 | 15.579 | 1.428 | 21.704 | 2.823 | 37.594 |
| backspace | 0.711 | 15.607 | 1.247 | 20.391 | 2.837 | 37.608 |
| scroll | 0.490 | 5.428 | 1.137 | 6.843 | 0.059 | 4.645 |

Independent confirmation frame P50: typing 16.045 ms, backspace 15.779 ms, scrolling 5.711 ms.

## Profile findings

The separate CPU profile identifies current hotspots; it does not prove which commit introduced them. Inclusive percentages overlap and must not be added. The profile also includes the harness's untimed snapshots between operations.

- `Editor.collectNotes` (`internal/editor/inline.go:324`) accounts for 20.0% inclusive CPU samples. On each document revision it rebuilds leaves, reads every block's text and scans inline tokens, even when the fixture has no footnotes. This adds document-wide work to ordinary typing and backspace.
- `App.liveIndex` → `document.plainText` → `Document.PlainText` (`internal/app/search.go:183`, `:106`) accounts for about 13.5% inclusive CPU samples. The footer requests the live search index, and each text revision rebuilds full-document plaintext.
- `Document.Leaves` accounts for 17.2% inclusive samples across callers; background GC workers account for about 17.6%. The allocation increase is consistent with full-document temporary data being rebuilt.
- `Editor.outlineList` (`internal/editor/outline.go:272`) constructs every heading row each frame; this fixture has 2,175 headings. `outlineView` has 0.40 s inclusive samples in this profile, and UI finalization/layout adds work outside that function. This is a candidate for the scrolling regression; it has not been isolated with an A/B patch.
- Likely next optimization targets: incremental note indexing, incremental/lazy live plaintext indexing, and viewport-limited outline row construction. These are diagnosis and recommendations, not implemented fixes.

## Validation and evidence

- All nine measured rounds, the independent confirmation and the profile run passed exact typed-text, backspace-restoration and 4,000-DIP scroll assertions.
- No application, dependency, benchmark or user-document source was modified. Generated artifacts are confined to this dated results directory and the temporary run directory.
- [Full samples, logs, summaries, source hashes and environment](comparison.json)
- [CPU profile](cpu.pprof), [cumulative CPU hotspots](profile-top-cum.txt), [flat CPU hotspots](profile-top-flat.txt)
- [Annotated note scan](profile-notes.txt), [annotated live index](profile-footer.txt)
- Previous results: [2026-11-08 optimized JSON](../2026-11-08/optimized.json) and [performance methodology](../../performance.md).

## Reproduction

Generate the fixture and timing overlay as documented in `tools/perf/README.md`, compile `internal/app` with the `perfbench` tag and the overlay, then run this command in a fresh process three times per fixture size (10k, 100k, 1m):

```sh
ARCMD_PERF_FIXTURE=/path/to/fixtures/1m.arcmd \
ARCMD_PERF_OUTPUT=/path/to/new-round-output \
ARCMD_PERF_SAMPLES=100 \
/path/to/arcmd-perf.test -test.run '^TestPerformance$' -test.v -test.timeout 10m
```

For an independent profile, add `ARCMD_PERF_PROFILE=/path/to/cpu.pprof`; do not include that run in comparison statistics.
