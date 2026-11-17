# Partial MDX support: native Markdown with JSX block islands

ArcMD keeps ordinary Markdown in its native editor. When a local `.mdx` file
is opened through a space, sidebar or system file-open action, top-level JSX
blocks are compiled into individual embedded WebViews. This is an optional
extension of the native-only architecture, not a replacement of the document
with a web page. It is **not full MDX compatibility**.

## Using it

Open `tools/mdx/example.mdx`, or create a local file containing:

```mdx
# Native heading

This paragraph stays native.

<Callout title="Example">

This **Markdown** and the nested component are inside one island.

<Counter initial={2 + 3} />

</Callout>
```

The built-ins are `Counter` (`initial`, `label`) and `Callout` (`title`,
`children`); ordinary JSX/HTML inside an island is handled by MDX/React.
Only `.mdx` invokes the compiler. Ordinary `.md`, `.markdown` and `.arcmd`
behavior is unchanged. Imports/exports, shared top-level expressions,
inline JSX, JSX nested outside a top-level island and file module resolution
are unsupported. Unknown components produce a visible rendering error and
source, not a claim of successful rendering.

Node/npm are **not application runtime requirements**: the Go binary embeds
moejs, the compiler and the browser runtime. Finder startup does not look for
Node or a compiler script. No local server is started.

The content height follows the rendered content. Theme changes update CSS
custom properties with an expression IIFE through `Page.Eval`; they do not
reload HTML, recompile or reset React state. Switching editor/source mode,
reopening or reloading a changed document can recompile it; interactive state
is not promised to survive a save/reload. Edit source and reload to recompile
changed JSX. Normal success has no technical status banner or empty status
placeholder. Compilation, per-island compilation and WebView failures retain
visible diagnostics; a clean main-window document offers reload/retry.

```sh
# Compile a file without opening a window; stdout is a JSON manifest.
go run ./cmd/arcmd --jsx-check tools/mdx/example.mdx
```

The diagnostic command only reads the source, never writes an output file,
and returns nonzero on compilation or partial/source-only diagnostics. A
partial manifest can still be emitted on stdout. Unknown component errors
occur later in the browser and must be inspected there.

## Document lifecycle and source integrity

`internal/jsx` creates a random, per-compilation namespace and a source SHA256.
`internal/app` projects only those matching JSX fences into the native model;
`internal/editor.EmbeddedBlock` supplies placement without a second editor.
The host creates views once per document/island, not per frame, and destroys
them on document replacement/close. The existing Markdown exporter is retained;
`RestoreSource` reverses only the exact namespaced projection fences before
saving or entering source mode. Original island source is preserved, and
rendering alone never writes the file. Ordinary native Markdown serialization
can still normalize Markdown as before; this is not a byte-for-byte serializer
for arbitrary whole-document Markdown.

There is no persistent manifest/cache lookup or external compiler cache in
this change: every opened/reloaded source is compiled afresh, and the manifest
contains its actual source SHA256. No previously compiled output is reused
for another source hash. Existing native code fences cannot impersonate an
island without the fresh namespace and matching source.

## Security boundary and limits

Opening a local `.mdx` opts into rendering its document expressions. Treat it
like active document content, not inert Markdown:

- Only the **trusted embedded compiler** runs in moejs. Document text is a
  string input; its expressions are parsed into output, not executed there.
  No host methods, module importer, filesystem, Node, fetch or OS API are bound.
  Dynamic code generation is disabled.
- Runtime evaluation has a deadline capped at 5 seconds and a 256 MiB JS
  allocation budget. Input is capped at 512 KiB, with 32 islands and a 16 MiB /
  200,000-node decoded result limit. Initial parsing of the fixed trusted
  compiler module occurs once, before the evaluation deadline. These are not
  process-wide RSS or OS isolation guarantees.
- **Document expressions execute only in an Untrusted WebView**, with no Go
  bindings. HTML uses hash-only script/style CSP, `default-src 'none'`,
  `connect-src 'none'`, no frames/forms/base/object permissions and cancelled
  navigation except `about:blank`. External imports, network resources and
  document filesystem access are not supplied. Do not relax CSP to load assets.
- Compiler budgets do not cap CPU/memory use of arbitrary browser expressions.
  Rendering untrusted active content remains a security/availability boundary;
  this feature does not claim a complete hostile-JavaScript sandbox.
- Theme and measurement scripts are valid expression IIFEs. Observer callbacks
  catch errors; sanitized `Script error.` events without an Error object must
  not tear down a successfully mounted component. Genuine failures stay visible.

## Rebuilding and testing

Normal `go build ./cmd/arcmd` uses the checked-in `go:embed` assets and needs no
Node. To change compiler or React dependencies, use a modern Node supporting
JSDOM 27 (Node 22.12+ or Node 24), then from the repository root:

```sh
npm ci --prefix tools/mdx
npm run build --prefix tools/mdx
npm test --prefix tools/mdx

go test ./internal/jsx ./internal/editor ./cmd/arcmd
go test ./internal/app -run 'Test(JSX|OpenMDX|OpenMoe|OpenMarkdownWithoutJSX)'
go vet ./...
```

`tools/mdx/package-lock.json` pins build/test dependencies. `build.mjs` emits
`internal/jsx/assets/mdx-compiler.js` and `react-runtime.js`; both are required
by `go:embed` and marked as generated for code review. `compiler-entry.mjs` is
the MDX AST projection/compiler source. There is no second Node runtime
compiler path. Update retained dependency notices/version inventory in
`third_party/mdx` when changing the lockfile. moejs/moego licenses and required
third-party notices are under `third_party/moejs` and `third_party/moego`.

Browser tests build the real `cmd/arcmd`, execute newly written files with
`PATH=/no-node`, and exercise its generated HTML in JSDOM. They do not substitute
precompiled examples or a parallel Node compiler. Go tests cover projection,
source restoration, limits/interruption, EmbeddedBlock integration, diagnostics
and valid theme/height expression scripts. JSDOM does not enforce the native
WebView's CSP/navigation or layout, so hash checks and mocked geometry are not
real WKWebView/platform acceptance.

## Platform/validation status

The fork implementation was reported working by its user on a real Mac before
this extraction. The newly arranged upstream branch has not itself been
validated on a real Mac. Real Linux/Windows WebView rendering has not been
validated. The upstream `mygo.json` identity, version, release/update settings
and `linux.webview: false` remain unchanged; Linux WebView support is not
silently enabled. Failed views are diagnosed rather than reported as successful
rendering; the original JSX remains available in source mode.

Upstream baseline `d8f9ee7` already has six `internal/app` test failures in the
Linux test environment: `TestBottomBlockHandleMenuAndIndentSubmenu`,
`TestRebindShortcuts`, `TestLibrarySpaces`, `TestLibraryCardDrag`,
`TestQACitationPreviewDoesNotOccludeAnswerText`, `TestMoreMenuAndSettings`.
They are not fixes included in this feature; report full-suite results rather
than describing that baseline as green.
