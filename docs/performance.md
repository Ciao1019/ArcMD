# Editor performance — 2026-11-08

ArcMD now has lower CPU-frame P50 and P95 than Quiet.md 0.1.7 in all nine measured
cases: typing, backspace and scrolling at approximately 10k, 100k and 1m characters.
This result covers the README-style workload below. It does not establish that
Go is faster than Rust, or that every ArcMD feature is faster than its counterpart.

## Reproduced comparison

Each case uses three fresh-process rounds, each with 100 insertions of `x` at the
end of the first paragraph, 100 backspaces, then 100 downward 40-DIP scrolls.
The table shows **the median of the three per-round quantiles**, in milliseconds;
it is not a pooled P95. Smaller is better. The last column divides Quiet.md's P50
by ArcMD's P50.

| Characters | Operation | ArcMD P50 | Quiet.md P50 | ArcMD P95 | Quiet.md P95 | P50 ratio |
|---|---|---:|---:|---:|---:|---:|
| 10k | Typing | 0.160 | 3.110 | 0.185 | 3.690 | 19.5× |
| 10k | Backspace | 0.152 | 3.030 | 0.174 | 3.650 | 19.9× |
| 10k | Scrolling | 0.244 | 5.020 | 0.597 | 8.500 | 20.5× |
| 100k | Typing | 0.191 | 3.670 | 0.211 | 4.620 | 19.3× |
| 100k | Backspace | 0.188 | 3.720 | 0.211 | 4.550 | 19.7× |
| 100k | Scrolling | 0.268 | 5.640 | 0.621 | 8.700 | 21.0× |
| 1m | Typing | 0.589 | 5.390 | 1.105 | 6.140 | 9.1× |
| 1m | Backspace | 0.572 | 5.000 | 1.002 | 6.420 | 8.7× |
| 1m | Scrolling | 0.489 | 4.630 | 1.136 | 8.250 | 9.5× |

The million-character fixture contains 1,001,307 body characters plus a
19-character title, 549 chapters, 16,935 top-level ArcMD blocks, 500 tables and
9,912 table cells. Smaller fixtures end at complete XML blocks: 10,023 and
100,006 characters. The converter asserts exact text preservation and retains
supported inline marks, lists, code and tables. Orange callouts map to yellow.

Event dispatch is timed separately, so a faster frame cannot hide work moved
into event handling. For the million-character case:

| Operation | ArcMD event P50 | Quiet.md event P50 | ArcMD event + frame P95 |
|---|---:|---:|---:|
| Typing | 0.240 | 0.970 | 1.428 |
| Backspace | 0.132 | 0.910 | 1.247 |
| Scrolling | 0.001 | 0.010 | 1.137 |

Quiet.md exposes event and frame quantiles but not individual samples, so its
combined P95 cannot be reconstructed by adding those quantiles. ArcMD preserves
all individual event/frame/combined samples.

## Improvement against ArcMD's baseline

These are ArcMD CPU-frame P50 values on the same machine and fixture. The old
million-character case used only **5 operations per round**, because frames took
several seconds; its numbers are diagnostic medians, not reliable tail estimates.
The old 10k/100k and all optimized cases use 100 operations per round.

| Characters | Operation | Before, ms | After, ms |
|---|---|---:|---:|
| 10k | Typing | 1.879 | 0.160 |
| 10k | Backspace | 2.037 | 0.152 |
| 10k | Scrolling | 1.053 | 0.244 |
| 100k | Typing | 56.730 | 0.191 |
| 100k | Backspace | 56.327 | 0.188 |
| 100k | Scrolling | 40.527 | 0.268 |
| 1m | Typing | 4133.358 | 0.589 |
| 1m | Backspace | 4021.641 | 0.572 |
| 1m | Scrolling | 3238.968 | 0.489 |

Allocation volume per operation on the million-character case, in decimal MB
(event plus frame and small harness bookkeeping; **not resident memory**):

| Operation | Before MB/op | After MB/op |
|---|---:|---:|
| Typing | 9,200.573 | 2.823 |
| Backspace | 9,240.988 | 2.837 |
| Scrolling | 8,836.899 | 0.059 |

## Changes responsible for the improvement

- `Engine.LeafAt` caches the leaf index per document revision and uses binary
  search. Table controls no longer rebuild every document leaf for every table.
  Queries inside structural transactions still see fresh intermediate state.
- Offscreen table controls, decorations and cell rules are culled before expensive
  work. Code line-number rendering no longer scans all rows for offscreen code.
  Fold controls are collected once per layout instead of searching by each ID.
- Text-only edits and their undo entries copy the affected runs. Structural edits
  retain full snapshots, and the existing 200-step history policy is preserved.
- A consecutive text-only edit reshapes its top-level text block and translates
  subsequent geometry. Containers and headings, missed revisions, width changes
  and theme changes use the complete layout path.
- Plain text, UTF-16 positions, word count and the dirty indicator reuse revision
  information. Dirty comparisons remain exact, including returning to the saved
  content without undo. Saving still verifies the entire committed model.

These changes retain the normal layout and input code. The timing adapter is
confined to a temporary module copy; the shipping MyGo dependency is unchanged.
No new production dependency is required.

## Environment and timing boundary

- Apple M5, 24 GiB, macOS 26.5.1, arm64; 1280 × 800 DIP at 2× scale.
- Go 1.27.3, `CGO_ENABLED=0`; MyGo v0.7.4.
- Rust 1.99.0; Quiet.md **0.1.7**, upstream commit
  [`b9bed5513a222292d92601c50bbd4e6850882dea`](https://github.com/s1dashu/quiet-md/commit/b9bed5513a222292d92601c50bbd4e6850882dea),
  `cargo build --release --locked --features devtools,gpui/runtime_shaders`.
  Runtime shader compilation happens before the measurements.
- Quiet.md uses the README's Craft theme with light appearance and its release
  notes dismissed; ArcMD uses its normal light page. Their fonts, layout and
  application shells still differ.
- ArcMD's baseline is commit `0d2ca5dcd4ffdbabc4d454d982db2c078f61cd0b`.
  Optimized build inputs, including uncommitted Go files and tests, have manifest
  SHA-256 `55b542156c011ad39cc519dd00501eedb9c7f893bd49015ec82a9c98ddeafdc8`.
  Per-file and binary hashes are in the results JSON.

Quiet.md times `window.draw()` (CPU scene construction), separately from GPU
presentation. The MyGo test adapter dispatches a real input event and invokes the
normal runtime's `runFrame()` once, skipping software rasterization and test-only
label collection. Screenshots are rasterized outside the timers. Native GPUI and
a headless MyGo host are not identical environments; these results are CPU scene
measurements, **not key-to-photon latency or measured display FPS**.

Both applications run serially, with GC enabled and no simultaneous build or
benchmark. MyGo's virtual clock advances 10 ms per operation; Quiet.md's harness
waits 10 ms. Normal desktop background activity remains. Individual round logs
and outliers are retained, rather than discarding slow samples.

Quiet.md's published README numbers (5.06/4.99/2.08 ms P50) were recorded on an
M5 Pro with 48 GiB at an earlier commit. They are context, not the denominator
for this comparison. Preliminary local baseline runs used Quiet.md 0.1.4
(`33b9ae6`) in system dark appearance; those old competitor values remain only in
`baseline.json`. The final comparison above rebuilds current upstream 0.1.7.
A separate three-round system-dark run also has lower ArcMD P50/P95 for all three
million-character operations; it is preserved in `system-appearance.json`.

Cold startup, native GPU submission, RSS, file saving, IME performance, complex
structural edits and editing every document position were not compared. Container
and heading edits still take the full layout path. Correctness tests cover those
fallback paths; this report does not claim performance leadership for them.

## Validation and reproduction

- Full `CGO_ENABLED=0 go test ./...` and `go vet ./...` pass; `gofmt -l .` is empty.
- `CGO_ENABLED=0 GOOS=linux go build ./...` and the Windows equivalent pass.
- A 600-step randomized comparison checks the text fast path against complete
  transactions, including Unicode, selection, undo/redo, model validity and save
  round trips. Existing random structural-edit tests also pass.
- UI tests compare incremental and complete layout pixels and geometry for
  paragraphs, lists, quotes, callouts and code, plus heading/cell fallbacks,
  wrapping, merged tables, columns, undo/redo and IME composition.
- Dirty-state tests cover several edited blocks, exact restoration, mixed
  structural edits and saving committed content during composition.
- All 58 before/after application snapshots were compared: 56 are pixel-identical;
  the other two differ only in the blinking caret. Relevant screenshots were
  visually inspected. Bounds and hashes are in `validation.json`.
- ArcMD asserts exact insertion, deletion restoration and scroll displacement in
  every measured run. Quiet.md's final saved text was also checked against each
  input after ignoring only structural XML pretty-print whitespace.

See [the harness instructions](../tools/perf/README.md) to reproduce all sizes.
Evidence retained in the repository:

- [Optimized runs and current Quiet.md comparison](performance/2026-11-08/optimized.json)
- [Original ArcMD baseline and preliminary Quiet.md 0.1.4 runs](performance/2026-11-08/baseline.json)
- [Supplementary system-appearance run](performance/2026-11-08/system-appearance.json)
- [Text restoration and snapshot verification](performance/2026-11-08/validation.json)
