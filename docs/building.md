# Building and releasing ArcMD

Run these commands from the repository root. For a quick start and feature
overview, see the [README](../README.md).

## Package locally

Build a production package for the current platform with the pinned MyGo CLI:

```sh
CGO_ENABLED=0 go run github.com/egoist/mygo/cmd/mygo@v0.7.4 build
```

On Apple Silicon Macs, this writes `ArcMD.app` and
`arcmd-0.0.1-arm64.dmg` into `dist/darwin-arm64/`. The default macOS
signature is ad hoc, for local testing; distribution requires a Developer ID
and notarization. Add `-platform darwin/universal` for a package supporting
both Apple Silicon and Intel Macs. Package metadata lives in `mygo.json`;
keep its version in sync with `internal/app.Version`.

When bumping the version, also update the version badge URL and its
alt text in both `README.md` (Chinese) and `README.en.md` (English). The README's build badge links
to the workflow configuration; it does not claim a passing CI run. Download cards
link to the releases page until versioned packages are published.

## GitHub Actions

[CI and packages](../.github/workflows/build.yml) runs on pull requests, pushes to
`main` / `master`, version tags (`v*`), and **Actions → CI and packages → Run
workflow**. It checks formatting, runs `go vet` and the full test suite on
macOS, then builds these packages with `CGO_ENABLED=0`:

| Platform | Architecture | Downloads |
| --- | --- | --- |
| macOS | Intel (`amd64`) | `.dmg` containing `ArcMD.app` |
| macOS | Apple Silicon / M series (`arm64`) | `.dmg` containing `ArcMD.app` |
| Windows | 64-bit (`amd64`) | NSIS installer `.exe` and portable `.zip` |
| Linux | x86-64 (`amd64`) | `.deb` and `.tar.gz` |
| Linux | ARM64 (`arm64`) | `.deb` and `.tar.gz` |

**Windows 32-bit is currently blocked by MyGo v0.7.4.** Its Windows backend
only implements `amd64` and `arm64`. A `windows/386` build selects an
unsupported backend and fails at startup, even though Go can compile it.
The packaging helper explicitly rejects that target until MyGo implements
it; CI does not publish an unusable 32-bit installer.

Download `packages-<platform>-<architecture>` from the run's **Artifacts**
section (retained for 14 days). Each includes SHA-256 checksums. macOS
packages use ad-hoc signing and are not notarized; Windows packages are
unsigned. Operating-system code signing and notarization require separate
certificates; they are independent of the update signing key below.

## Signed updates

`mygo.json` contains the public key used to verify application updates.
Keep the matching private key outside the repository and configure it as
the repository Actions secret `MYGO_UPDATER_PRIVATE_KEY`:

```sh
gh secret set MYGO_UPDATER_PRIVATE_KEY --repo Ciao1019/ArcMD < /secure/path/mygo-update.key
```

The command reads the key through standard input. Never commit the private
key or include it in release assets. Recreating the GitHub repository also
requires configuring this secret again. Local and pull-request builds can
run without it; tagged release builds require it and include signed update
archives and `update-<platform>.json` manifests.

## Publish a release

To publish a release, first set the same version in `mygo.json` and
`internal/app/app.go`, update both README badges, and add a `## X.Y.Z`
section to `CHANGELOG.md`. The changelog supplies the release notes both
on GitHub and in the application's updater. Commit the prepared source,
then push the matching tag:

```sh
git tag v0.0.1
git push origin v0.0.1
```

The workflow rejects mismatched tag/package/app versions. After every
package job succeeds, it verifies the downloads, creates a draft GitHub
Release, uploads the packages and `SHA256SUMS.txt`, then publishes it.
Tags such as `v0.2.0-beta.1` produce prereleases. A retry can finish a draft
but cannot replace an already published release. Only the release job has
write permission, using GitHub's built-in `GITHUB_TOKEN`.

For a validation run without publishing, push a preparation branch and use
**Run workflow** with that branch selected. Manual runs build all five
targets and upload Actions artifacts, but never create a Release. Do not
push a version tag until publication is intended.

To reproduce a CI build locally (Go required):

```sh
go run ./tools/ci build darwin/arm64
go run ./tools/ci build linux/amd64
```

Collected downloads are in `dist/release/`. Build macOS DMGs on macOS;
build Windows installers on Windows, where MyGo downloads NSIS if needed,
or install NSIS (`makensis`) on the cross-compilation host. The helper
fails when an expected installer is missing.
