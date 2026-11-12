# Performance follow-up — 2026-11-13

The latest optimization removes most of the 2026-11-12 regression in the saved workload. Million-character typing CPU-frame P50 falls from 15.467 to 0.831 ms (94.6% lower); backspace falls from 15.366 to 0.818 ms; scrolling falls from 5.298 to 0.788 ms. Remaining high-impact cases are heading/cell edits, backspace near the document end, and documents with footnotes. No application code was changed during this review.

## Matched benchmark

Same Apple M5 / 24 GiB / macOS 26.5.1 / Go 1.27.3 / MyGo v0.7.4 / CGO disabled; same fixture bytes, 1280 × 800 DIP, scale 2, three fresh processes and 100 operations per action. All runs were serial and separate from compilation and tests. Percentiles are the median of per-round quantiles, using the existing harness rule; they are not pooled. The 2026-11-08 and 2026-11-12 values are historical measurements, not same-session reruns. Normal desktop background activity remains.

CPU scene timings exclude GPU presentation, rasterization and display latency. They are not observed FPS or key-to-photon latency.

| Characters | Action | Nov 8 frame P50 | Nov 12 frame P50 | Current frame P50 | Current frame P95 | Current MB/op |
|---|---|---:|---:|---:|---:|---:|
| 10k | type | 0.160 | 0.325 | 0.238 | 0.277 | 0.247 |
| 10k | backspace | 0.152 | 0.325 | 0.229 | 0.257 | 0.261 |
| 10k | scroll | 0.244 | 0.331 | 0.313 | 0.955 | 0.119 |
| 100k | type | 0.191 | 1.371 | 0.277 | 0.316 | 0.346 |
| 100k | backspace | 0.188 | 1.337 | 0.275 | 0.307 | 0.360 |
| 100k | scroll | 0.268 | 0.577 | 0.356 | 0.878 | 0.134 |
| 1m | type | 0.589 | 15.467 | 0.831 | 1.088 | 2.836 |
| 1m | backspace | 0.572 | 15.366 | 0.818 | 1.281 | 2.851 |
| 1m | scroll | 0.489 | 5.298 | 0.788 | 1.473 | 0.290 |

All times are milliseconds. MB/op is decimal allocated bytes divided by operation count, not resident memory. Current million-character typing allocation is 2.836 MB/op, down from 37.594 MB/op on Nov 12. Current matched P50 values remain above the older Nov 8 baseline.

## Extended workloads

Each case below uses the million-character fixture and three fresh processes × 100 insertions, followed by 100 backspaces. These use a temporary diagnostic overlay with the same event/frame adapter. Results are buffered, screenshots are omitted, and an initial GC occurs before measurement. Therefore compare these cases against their own control, rather than treating them as identical to the matched harness. All inserted text and exact restoration are asserted.

- Control: end of the first paragraph, as in the saved workload.
- Tail: end of the last paragraph.
- Footnote: the fixture plus one definition at the end and one reference at the end of the first paragraph; edits occur after that reference.
- Heading: end of the first section heading, excluding the document title.
- Cell: end of the first table cell.
- Pause: after each event/frame, advance virtual time by 500 ms and time one additional frame. This exercises the 400 ms footer refresh while remaining below the 700 ms autosave delay. No sleeping occurs.

| Case | Action | Event P50 | Frame P50 | Event + frame P50 | Event + frame P95 | MB/op |
|---|---|---:|---:|---:|---:|---:|
| control | type | 0.241 | 0.845 | 1.094 | 1.370 | 2.836 |
| control | backspace | 0.138 | 0.767 | 0.953 | 1.130 | 2.850 |
| tail | type | 1.885 | 0.844 | 2.725 | 3.056 | 2.914 |
| tail | backspace | 26.725 | 0.990 | 27.744 | 29.648 | 2.878 |
| footnote | type | 0.092 | 4.254 | 4.342 | 7.120 | 15.554 |
| footnote | backspace | 0.171 | 4.105 | 4.276 | 6.874 | 15.569 |
| pause | type | 0.082 | 0.970 | 1.067 | 1.798 | 19.667 |
| pause | backspace | 0.149 | 0.945 | 1.091 | 1.850 | 19.680 |
| heading | type | 0.084 | 48.422 | 48.508 | 52.545 | 116.447 |
| heading | backspace | 0.198 | 48.412 | 48.605 | 52.930 | 116.430 |
| cell | type | 0.089 | 48.610 | 48.701 | 53.390 | 116.492 |
| cell | backspace | 0.276 | 48.248 | 48.519 | 51.134 | 116.435 |

Pause-case allocation includes both the event/frame and delayed frame; the event + frame columns exclude the delayed frame. The delayed-frame median is 1.368 ms after insertion and 1.331 ms after backspace.

## Recommended optimization order

### 1. Avoid repeated full-prefix UTF-16 scans near the document end

Last-paragraph backspace costs 27.744 ms total P50, including 26.725 ms in event dispatch. First-paragraph control backspace costs 0.953 ms total P50. This is predominantly an input problem rather than a frame problem.

`internal/editor/buffer.go:72` (`clamp`, `validRange`, `slice`) and `:178` (`lineRange`, `boundary`) repeatedly convert UTF-16 positions against the complete flat text. `internal/editor/autopair.go:32` validates the replacement range, then `Buffer.ReplaceText` validates it again. The tail profile spends 43.1% inclusive CPU in `UTF16ByteOffset`, 32.1% in `UTF16Len`, and 47.6% in `clamp`/`validRange`; inclusive call-tree percentages overlap.

Use a block/leaf index with cached byte and UTF-16 offsets so conversions examine the current block. Reuse current shaped-row grapheme boundaries when valid, retain correct composition and stale-layout fallbacks, and avoid validating an already normalized internal edit range twice. Preserve surrogate-pair, combining-mark and emoji behavior; do not replace grapheme deletion with rune deletion.

### 2. Extend incremental layout to headings and table containers

Heading insertion costs 48.508 ms total P50; cell insertion costs 48.701 ms. Both allocate about 116 MB per edit. `internal/editor/incremental.go:27` explicitly rejects headings and leaves owned by containers, so `internal/editor/view.go:layout` reconstructs the whole document layout. These fallbacks were already documented; this review measures their cost and does not attribute them to the latest optimization.

Handle text-only heading edits by reshaping that heading, updating its outline text and shifting subsequent geometry without rebuilding unrelated blocks. For a table edit, recalculate the affected table/row geometry and shift subsequent top-level blocks; preserve merged-cell and column constraints. Start with headings because their dependency set is smaller.

### 3. Make the footnote index incremental when footnotes exist

One footnote raises first-paragraph insertion to 4.342 ms total P50 and 15.554 MB/op, compared with 1.094 ms and 2.836 MB/op in the control. The new no-footnote fast path is effective, but `internal/editor/inline.go:324` still rebuilds `Document.Leaves` and reads all block text after every revision when any footnote exists. The isolated footnote profile assigns 45.0% inclusive CPU and roughly 81% of sampled allocations to `collectNotes`.

Cache definitions and references by stable block ID; for `TextChangeSince`, inspect only the changed block. Recompute numbering when references/definitions or structural ordering change, and update target positions through the existing leaf index. Cache the absence of footnotes across safe text-only edits as a smaller improvement.

### 4. Reduce steady-state text-copy allocation

Even without footnotes, `internal/editor/buffer.go:36` builds a new full-document string on every edit. In the isolated 1,000-operation-per-action control allocation profile, `Buffer.sync` accounts for 93.2% of sampled allocated bytes after subtracting setup. A chunked text representation, gap buffer or piece table with byte/UTF-16 indexes could reduce both copying and position lookup; this is a larger refactor, so prioritize the input and layout hotspots first. Maintain the current IME, undo and exported-text contracts.

## Lower-priority observations

- The delayed footer refresh is now about 1.3 ms median rather than repeated per-keystroke work, so it is lower priority. It still causes about 16.8 MB extra allocation per pause cycle. `Document.PlainText` appends to a builder without a capacity hint; capacity planning or reusing a safe committed-text representation is worth measuring before a larger indexing redesign.
- `internal/editor/outline.go:310` disables virtualization for the duration of a drag. This is a source-review observation, not a measured drag benchmark: retain visible rows plus the drag source with stable identity instead of rebuilding all 2,175 headings.
- Regular painting and hit testing still scan full row/decorations arrays. They are candidates for indexed visible ranges, but are smaller than the measured 28–49 ms editing paths.

## Validation and retained evidence

- Nine matched benchmark rounds and eighteen extended-workload rounds passed. Two initial 20-operation heading/cell diagnostics were excluded from reported statistics; the final figures use the full three × 100 runs.
- Existing tests for plaintext equivalence, nested block-kind lookup, delayed footers, outline behavior/virtualization, footnotes and inline math passed with `CGO_ENABLED=0` in the document, editor and app packages. This review did not rerun the entire repository suite.
- Production source hashes were checked unchanged after profiling. Only this results directory was added to the repository.
- [Full samples, logs, source manifest and comparisons](comparison.json).
- [Diagnostic test appended through a temporary Go build overlay](diagnostic-test.go.txt). It uses the existing `tools/perf` adapter plus `func (t *Tester) PerfReviewIdle(d time.Duration) time.Duration { t.now = t.now.Add(d); return t.PerfFrame() }` in the temporary headless adapter.
- Separate CPU and allocation profiles: [tail](profile-tail/cpu-top.txt), [footnotes](profile-footnote/cpu-top.txt), [control allocations](profile-control/alloc-top.txt), [footer pause](profile-pause/cpu-top.txt). Each directory contains raw CPU and before/after heap profiles.
- Profiles exclude setup, snapshots, per-sample disk output and final serialization. Control uses 1,000 operations per action, tail 100, footnote/pause 300; none of these profiled runs are included in the comparison tables. Allocation profiles subtract the setup profile and remain statistical samples.
- The preliminary legacy 1,000-operation profile is retained only in the temporary run directory. Its per-sample JSONL writes dominated CPU; the isolated diagnostic profiles above avoid that harness overhead.

## Reproduction

Use `tools/perf/README.md` for fixture conversion, the timing overlay and the unchanged matched benchmark. For diagnostics, append `diagnostic-test.go.txt` to a temporary copy of `internal/app/performance_test.go`, add the `PerfReviewIdle` method to the temporary headless adapter, and map both through a Go build overlay. Compile with `CGO_ENABLED=0`, the `perfbench` tag and the temporary modfile, then run fresh processes with:

```sh
ARCMD_PERF_FIXTURE=/path/to/fixtures/1m.arcmd \
ARCMD_PERF_OUTPUT=/path/to/new-round \
ARCMD_PERF_SAMPLES=100 ARCMD_REVIEW_MODE=tail \
/path/to/review.test -test.run '^TestPerformanceReview$' -test.v -test.timeout 10m
```

Modes: `control`, `tail`, `footnote`, `heading`, `cell`, `pause`. Add `ARCMD_PERF_PROFILE=/path/to/cpu.pprof` only for a separate profile run.
