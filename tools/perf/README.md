# Editor performance harness

This opt-in harness compares ArcMD's CPU scene construction with the operations
in Quiet.md's README benchmark. It does not change the shipping editor, go.mod,
or the installed MyGo module. All documents are synthetic and all application
state is isolated in temporary directories. All ArcMD tooling is written in Go
and uses the standard library and ArcMD's document model; no Python runtime is
required. The native comparison runner currently targets macOS.

## Prepare

Build a checked-out Quiet.md release with its script harness available. For the
tested commit `b9bed5513a222292d92601c50bbd4e6850882dea` (0.1.7):

```sh
cd /path/to/quiet-md
cargo build --release --locked --features devtools,gpui/runtime_shaders
```

The `runtime_shaders` feature compiles Metal shaders on application startup; it
allows the build to use Command Line Tools without an installed `metal` compiler.
Startup and shader compilation are outside the operation timing. The `devtools`
feature enables the script protocol, which ArcMD's Go runner drives directly.

Supply an existing **synthetic** Quiet.md library at
`/tmp/quiet-benchmark-library`, containing
`docs/d0c00006-0000-4000-8000-000000000006.xml`. The published measurements used
the fixture from Quiet.md's [upstream promo generator at the tested commit](https://github.com/s1dashu/quiet-md/blob/b9bed5513a222292d92601c50bbd4e6850882dea/tools/promo/gen_library.py).
That external fixture generator and its dependencies belong to Quiet.md; they
are not part of ArcMD's tooling. The fixture source SHA-256 is
`9aa051dd8d15f6a37e18d0f2a3ebd49fc3128c5ee645a33a5100709bd9ceabb7`.
Keep this input for comparable runs; a different document requires new results.

Set the synthetic library's preferences before measuring:

```sh
cat > /tmp/quiet-benchmark-library/preferences.json <<'EOF'
{"english":true,"theme":"craft","appearance":"light","seen_version":"0.1.7"}
EOF
```

Both applications use light appearance; `seen_version` dismisses the first-run
release notes. Craft remains the theme used by the upstream README benchmark.

## Measure

From the ArcMD repository, with a **new** output directory:

```sh
go run ./tools/perf run \
  --quiet-repo /path/to/quiet-md \
  --library /tmp/quiet-benchmark-library \
  --out /tmp/arcmd-comparison-100k \
  --size 100k --samples 100 --runs 3
```

Sizes are `10k`, `100k`, and `1m`. Prefix variants stop at whole XML blocks and
therefore slightly exceed their target character counts. The converter retains
all text and supported inline styles, list items, tables, code, and callouts;
it asserts exact concatenated-text equality. Callout orange maps to ArcMD's
yellow because ArcMD has no orange callout type. Manifest files record character
counts, block counts, and SHA-256 hashes. No Markdown round trip is involved.

The default is three rounds of 100 operations on the million-character fixture.
At the initial ArcMD baseline this is very slow (seconds per frame); a short
diagnostic can use `--size 1m --samples 5 --runs 3`. Short diagnostics are useful
for finding bottlenecks, **not for quoting a 100-operation P95**.

The runner compiles first, then runs the applications serially. Each round uses a
new process, inserts `x` at the end of the first paragraph, backspaces the inserted
characters, and scrolls 40 DIP per operation at (900, 500). ArcMD asserts the exact
inserted text, its restoration, and the final scroll offset. It writes individual
step/frame/combined samples to JSONL and JSON, allocations/GC counts, logs, and
untimed screenshots. Metadata also records the exact source-file hashes
(including uncommitted files), binary hashes, toolchains and preferences. Quiet.md's native script driver writes per-round quantiles and
screenshots. Its log does not expose individual samples.

## Convert and summarize

The conversion and report tools also run independently:

```sh
go run ./tools/perf convert /path/to/source.xml /tmp/arcmd-fixtures
go run ./tools/perf summarize --out /tmp/arcmd-report.json \
  /tmp/arcmd-comparison-100k
```

The report preserves raw evidence and takes the median of per-round quantiles;
it never pools the samples to manufacture a new P95.

## Timing boundary

Quiet.md's `window.draw` constructs a scene; `window.present` is separate. MyGo's
normal `ui.Tester` also software-rasterizes and settles up to 20 frames, so timing
`Tester.Type` would not be equivalent.

`go run ./tools/perf overlay <directory>` copies MyGo v0.7.4 and writes a temporary
modfile plus Go build overlay. The adapter skips `headless.present` rasterization,
turns off test-only label collection, dispatches one real input event, and calls
the normal runtime's `runFrame` exactly once. Rendering, layout, text shaping,
editor logic, shell, and undo remain unchanged. `PerfSnapshot` rasterizes outside
the timers. The source patch is version-guarded and inspectable in `overlay/`.

Both frame timers exclude event dispatch (reported separately), GPU presentation,
display latency, and initial loading. Neither measures key-to-photon latency.
ArcMD includes its runtime frame-finalization work. Native-host GPUI and headless
MyGo are not identical environments: fonts/layout, native services, themes, app
shells, and history policies differ. The adapter advances ArcMD's virtual clock
10 ms per operation without sleeping; the upstream runner waits 10 ms between
operations. GC remains enabled. Normal background desktop activity is not stopped.
These are application-level measurements, not a Go-vs-Rust language benchmark.

ArcMD's `open_cpu_ms` is a separate one-shot diagnostic: loading the `.arcmd` file
through `App.open` and constructing its first scene in an already initialized
process. It is not cold application startup and has no matched Quiet.md number.

## Profile or inspect one ArcMD run

After the runner has built `arcmd-perf.test`:

```sh
ARCMD_PERF_FIXTURE=/tmp/arcmd-comparison-100k/fixtures/1m.arcmd \
ARCMD_PERF_OUTPUT=/tmp/arcmd-million-profile \
ARCMD_PERF_SAMPLES=2 \
ARCMD_PERF_PROFILE=/tmp/arcmd-million.pprof \
/tmp/arcmd-comparison-100k/arcmd-perf.test \
  -test.run '^TestPerformance$' -test.v -test.timeout 10m
go tool pprof -top -cum /tmp/arcmd-comparison-100k/arcmd-perf.test /tmp/arcmd-million.pprof
```

Do not use profiled runs as the comparison numbers. The `perfbench` build tag
keeps this expensive test out of ordinary `go test ./...`; it intentionally
requires the adapter when enabled. The runner allows one hour per ArcMD round;
`--timeout` changes that limit in seconds.
