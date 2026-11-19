# Plugins and the marketplace

> Status: **active (`wasm` runtime + native Plugins UI)**. The manifest (`arcmd-plugin.json`),
> registry index, installing/enabling, local `.zip` archive installation (`InspectArchive` /
> `InstallArchive`), permission confirmation prompt, declarative `contributes.settings` UI,
> sandboxed `wazero` WASM host (`internal/plugin/wasm.go`), and the bundled official **Cloud Sync**
> plugin (`dev.arcmd.cloud-sync` in `plugins/cloud-sync/`) are implemented. The `process` runtime
> and hosted remote marketplace registry remain planned.

ArcMD's core edits Markdown, organizes it into spaces, and answers
questions about a space with a read-only agent (`internal/agent`,
`internal/spacefs`: the model searches and reads the documents with tools,
no vector index) on the model the user sets up. Everything else — syncing,
AI writing tools, etc. — comes from plugins that users download from the
marketplace or install locally; a plugin may also give the agent tools of its own.
This keeps the core small, lets features ship on their own schedule, and lets
users choose what code, and which providers, touch their notes.

## A plugin

A plugin is a folder (distributed as a zip) with `arcmd-plugin.json` at its
root:

```json
{
  "id": "dev.arcmd.cloud-sync",
  "name": "云端同步",
  "version": "0.1.0",
  "description": "Synchronize space documents and attachments with S3 or WebDAV.",
  "author": "ArcMD",
  "license": "MIT",
  "icon": "cloud",
  "engine": "0.2.3",
  "runtime": "wasm",
  "main": "plugin.wasm",
  "permissions": ["space:read", "space:write", "network", "secrets", "ui:panel"],
  "contributes": {
    "commands": [
      { "id": "sync-now", "title": "云端同步：立即同步", "primary": true, "inSettings": true },
      { "id": "test-connection", "title": "云端同步：测试连接", "inSettings": true },
      { "id": "sync-status", "title": "云端同步：查看同步记录" }
    ],
    "panels": [{ "id": "status", "title": "云端同步", "icon": "☁" }],
    "settings": [
      {
        "id": "provider",
        "title": "同步方式",
        "type": "segmented",
        "default": "s3",
        "options": [
          { "value": "s3", "label": "S3 对象存储" },
          { "value": "webdav", "label": "WebDAV" }
        ]
      }
    ]
  }
}
```

| Field | Rule |
| --- | --- |
| `id` | Reverse-DNS, lowercase, unique in the marketplace; also the plugin's folder name |
| `version`, `engine` | Semantic versions; `engine` is the lowest ArcMD version supported |
| `icon` | Optional Lucide icon name rendered in Library › Plugins |
| `runtime` | `wasm` (default, sandboxed via `wazero`) or `process` |
| `main` | Path inside the plugin: the `.wasm` module, or the executable |
| `permissions` | Only those listed in `plugin.Permissions`; shown to the user in the install confirmation dialog |
| `contributes.commands` | Appear in the command bar; `when` is `""`, `document` or `selection`; `primary` surfaces the card's primary action button; `inSettings` renders a button in the settings modal |
| `contributes.panels` | Sidebar panels / sheets whose content the plugin returns as Markdown; need `ui:panel` |
| `contributes.settings` | Declarative native settings rendered by ArcMD (`segmented`, `select`, `string`, `secret`, `bool`, `spaces`), with optional `group`, `presets` menu, and conditional `when: { "field": …, "equals": … }` |

Note: any setting with `"type": "secret"` requires `"secrets"` in `permissions`. Secrets are stored separately via `prefs.Secrets` under `<config dir>/ArcMD/credentials/plugin-<pluginID>-<fieldID>` (`0600`), never inside `settings.json` or space folders.

## Plugins don't draw

ArcMD's UI is native, drawn by MyGo. Plugins never get a canvas or a
webview: they **contribute** commands, panels and declarative settings,
which ArcMD draws in its own style, and **answer** invocations with a `Response`:

- `edits` — replacements in the open document, applied as one undo step
  (`document:write`);
- `markdown` — shown in a sheet for a command or panel (`pluginMarkdownView`);
- `toast` — a short notification message;
- `status` — an optional short status line surfaced on the plugin card.

This keeps every plugin consistent with the Arc/Zen look, accessible, and
unable to spoof ArcMD's own UI. Streaming (an AI reply arriving token by
token) can be added as sequential updates rendered by `ui/markdown`.

## Runtimes & WASM Host ABI

- **`wasm`** — a WASI (`GOOS=wasip1 GOARCH=wasm`) module run in ArcMD's process by
  [wazero](https://wazero.io) (pure Go, zero cgo, matching MyGo's rule). The
  module gets no filesystem, clock override, or socket access of its own: ArcMD
  passes a JSON `Request` via `stdin`, collects the JSON `Response` from `stdout`,
  and exposes two imports under the `"arcmd"` host module: `call(reqPtr, reqLen) uint32`
  and `read_result(dstPtr, dstLen) uint32` using length-prefixed binary frames (`EncodeFrame` /
  `DecodeFrame`). Host operations enforce `Manifest.Permissions`:
  - `config.get` — read the plugin's non-secret `settings.json` (secrets stripped);
  - `state.get` / `state.set` / `state.delete` — isolated per-plugin JSON/binary state under `plugins-data/<id>/`;
  - `secrets.get` (requires `secrets`) — read scoped `plugin-<id>-<key>` credentials only;
  - `spaces.list`, `space.list_files`, `space.read_file` (require `space:read`) — list spaces, enumerate syncable files (skipping `.arcmd/history`, `.git`, `node_modules`), and read file bytes + SHA-256 metadata (including unsaved live editor buffers marked `dirty`);
  - `space.write_file`, `space.write_conflict`, `space.delete_file` (require `space:write`) — optimistic-concurrency writes (`expectedSha256`), non-destructive conflict copies, and safe deletes routed through ArcMD's `trashbin` + OS Trash (never `os.Remove`);
  - `http.request` (requires `network`) — outbound HTTP client routed through ArcMD (`https://` enforced by default; `http://` only for loopback hosts or explicit `allowInsecureHttp`; cross-origin redirects strip `Authorization` and `Cookie`).
- **`process`** — (planned) an executable speaking JSON-RPC 2.0 over stdin/stdout,
  as language servers do, for plugins that need a native toolchain or a
  local model.

Both implement `plugin.Host`; `Instance.Invoke(Request) → Response` is the
protocol surface for v1.

## Bundled Official Plugin: Cloud Sync (`dev.arcmd.cloud-sync`)

Located in `plugins/cloud-sync/` (built into `plugins/cloud-sync/bundle/cloud-sync.zip` via `go generate ./plugins/cloud-sync` with `GOOS=wasip1 GOARCH=wasm`):

- **Backends**: Pure-Go **S3** (AWS Signature V4 `AWS4-HMAC-SHA256`, Virtual-Hosted & Path-Style, presets for Cloudflare R2, AWS S3, Aliyun OSS, Tencent COS, Backblaze B2, local MinIO) and **WebDAV** (`PROPFIND` `Depth: 1` / `0`, `MKCOL`, `GET`/`PUT`/`DELETE`, Basic Auth, presets for 坚果云 / Nextcloud / Alist).
- **3-Way Incremental Sync**: Compares `Local × Remote × Baseline` (`baseline_<spaceID>.json`), supports selective per-Space opt-in, dual-edit / dirty-buffer conflict copies (`<stem> (冲突副本 YYYY-MM-DD HHmm)<ext>`), sync history (`history.json`), post-save debounced sync (15s), and frame-clock periodic auto-sync (`autoSync`).

## The marketplace & Plugins UI

Library › Plugins (`插件`) lets users install the bundled Cloud Sync plugin with one click or install any local `.zip` package (`Manager.InstallArchive`), inspect requested permissions before enabling, toggle enable/disable, configure declarative settings, trigger commands (also available via `⌘K`), or uninstall cleanly.

The marketplace registry format is a static `index.json`:

```json
{
  "version": 1,
  "plugins": [
    {
      "id": "dev.arcmd.ai-writer",
      "name": "AI Writer",
      "version": "0.1.0",
      "description": "Rewrite, summarize and continue your writing.",
      "tags": ["ai", "writing"],
      "permissions": ["document:read", "document:write", "network", "secrets", "ui:panel"],
      "download": "https://github.com/arcmd-md/ai-writer/releases/download/v0.1.0/ai-writer.zip",
      "sha256": "…"
    }
  ]
}
```

Installing (`Manager.Install` / `Manager.InstallArchive`) enforces:

1. Remote downloads over HTTPS only, size-capped (`Manager.Install`).
2. Remote zip SHA-256 must match the index digest (`Manager.Install`).
3. Entries with absolute paths, `..`, backslashes or symbolic links are
   refused; the unpacked size is capped (`InspectArchive` / `installArchive`).
4. The manifest must parse, validate, and match the expected `id` and `version`.
5. The new version replaces the old atomically (staged, then renamed);
   the user's enabled/disabled choice is kept.

Plugins live in `<config dir>/ArcMD/plugins/<id>/`; `plugins.json` there lists the disabled ones, and `plugins-data/<id>/` stores non-secret settings and plugin state.

## Roadmap

1. Hosted official registry repository with CI validation & marketplace browser feed.
2. `process` runtime (stdio JSON-RPC 2.0) and first-party AI Writer plugin.
3. Agent tool registration from plugins (`aiTools`) and signed releases (minisign/sigstore).
