package app

import (
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/mdz"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

const testBadgeSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="112" height="20" viewBox="0 0 112 20"><rect width="58" height="20" rx="3" fill="#555"/><rect x="58" width="54" height="20" rx="3" fill="#2563eb"/><text x="29" y="14" text-anchor="middle" fill="#fff" font-size="11" font-family="sans-serif">version</text><text x="85" y="14" text-anchor="middle" fill="#fff" font-size="11" font-family="sans-serif">0.5.1</text></svg>`

func TestInlineImageLocalRemoteSVGAndMDZRoundTrip(t *testing.T) {
	// 1. Local & remote SVG decoding (including extensionless shields.io-style URLs).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		_, _ = w.Write([]byte(testBadgeSVG))
	}))
	defer srv.Close()

	a, tt := newTestApp(t)
	a.pluginsUI.httpClient = srv.Client()
	sp := a.state.ActiveSpace()

	iconPNG := makeBookSnapshotPNG(t, 40, 40, color.RGBA{37, 99, 235, 255}, color.RGBA{239, 246, 255, 255}, color.RGBA{96, 165, 250, 255})
	rightPNG := makeBookSnapshotPNG(t, 240, 140, color.RGBA{16, 138, 92, 255}, color.RGBA{240, 253, 244, 255}, color.RGBA{52, 211, 153, 255})
	assetsPath := filepath.Join(sp.Root, "assets")
	if err := os.MkdirAll(assetsPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsPath, "icon.png"), iconPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsPath, "right.png"), rightPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assetsPath, "badge.svg"), []byte(testBadgeSVG), 0o644); err != nil {
		t.Fatal(err)
	}

	remoteURL := srv.URL + "/badge/version-0.5.1-2563eb"
	if err := a.fetchAndCacheRemoteImage(srv.Client(), remoteURL); err != nil {
		t.Fatalf("fetchAndCacheRemoteImage(SVG) failed: %v", err)
	}

	md := strings.Join([]string{
		`<img src="assets/right.png" width="200" align="right">`,
		`MDZip floated paragraph.`,
		``,
		`Inline icon <img src="assets/icon.png" width="20" height="20" alt="icon"> and local SVG <a href="https://example.com"><img src="assets/badge.svg" alt="v0.5.1"></a> and remote [![badge](` + remoteURL + `)](https://example.com/r).`,
		``,
		`<picture><source srcset="assets/missing.webp"><img src="assets/icon.png" width="32" alt="pic"></picture>`,
	}, "\n") + "\n"

	rel, err := vault.CreateFile(sp.Root, "", "InlineMedia", ".md", md)
	if err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open(rel)
	settle(tt)

	// Verify both local SVG and cached remote SVG resolve to valid *ui.SVG resources.
	localSVGRes := a.imageResourceFor(a.doc, "assets/badge.svg")
	if localSVGRes.SVG == nil || localSVGRes.Width != 112 || localSVGRes.Height != 20 {
		t.Fatalf("local SVG failed to decode: %+v", localSVGRes)
	}
	remoteSVGRes := a.imageResourceFor(a.doc, remoteURL)
	if remoteSVGRes.SVG == nil || remoteSVGRes.Width != 112 || remoteSVGRes.Height != 20 {
		t.Fatalf("cached remote SVG failed to decode: %+v", remoteSVGRes)
	}

	// 2. Exporting to .mdz must bundle inline local images (both PNG and SVG).
	a.exportMDZ()
	mdzPath := filepath.Join(sp.Root, "InlineMedia.mdz")
	arc, err := mdz.Open(mdzPath)
	if err != nil {
		t.Fatalf("mdz.Open(exported) failed: %v", err)
	}
	for _, want := range []string{"index.md", "assets/icon.png", "assets/badge.svg", "assets/right.png"} {
		if !arc.HasFile(want) {
			t.Fatalf(".mdz export missing %q; files=%v", want, arc.Files())
		}
	}
	_ = arc.Close()

	// 3. Open the exported .mdz inside the space and confirm SVG decodes cleanly from archive.
	a.refreshTree()
	a.open("InlineMedia.mdz")
	settle(tt)
	if !a.doc.isMDZ() {
		t.Fatal("expected opened .mdz viewer")
	}
	mdzSVGRes := a.mdzLoadImage(a.doc, "assets/badge.svg")
	if mdzSVGRes.SVG == nil || mdzSVGRes.Width != 112 || mdzSVGRes.Height != 20 {
		t.Fatalf(".mdz archive SVG failed to decode: %+v", mdzSVGRes)
	}
}

func TestSnapshotInlineHTMLImages(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render inline HTML images and README")

	// Render 1: Issue #94 6-pattern showcase.
	t.Run("issue94-showcase", func(t *testing.T) {
		a, tt := newTestApp(t)
		sp := a.state.ActiveSpace()
		assets := filepath.Join(sp.Root, "assets")
		if err := os.MkdirAll(assets, 0o755); err != nil {
			t.Fatal(err)
		}
		writeAsset := func(name string, data []byte) {
			if err := os.WriteFile(filepath.Join(assets, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		writeAsset("hero-right.png", makeBookSnapshotPNG(t, 360, 180, color.RGBA{37, 99, 235, 255}, color.RGBA{239, 246, 255, 255}, color.RGBA{96, 165, 250, 255}))
		writeAsset("icon.png", makeBookSnapshotPNG(t, 40, 40, color.RGBA{242, 17, 88, 255}, color.RGBA{255, 241, 242, 255}, color.RGBA{251, 113, 133, 255}))
		writeAsset("card-a.png", makeBookSnapshotPNG(t, 180, 90, color.RGBA{16, 138, 92, 255}, color.RGBA{240, 253, 244, 255}, color.RGBA{52, 211, 153, 255}))
		writeAsset("card-b.png", makeBookSnapshotPNG(t, 180, 90, color.RGBA{217, 119, 6, 255}, color.RGBA{255, 251, 235, 255}, color.RGBA{251, 191, 36, 255}))
		writeAsset("badge-go.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="136" height="22" viewBox="0 0 136 22"><rect width="68" height="22" rx="4" fill="#374151"/><rect x="68" width="68" height="22" rx="4" fill="#00ADD8"/><text x="34" y="15" text-anchor="middle" fill="#fff" font-size="11" font-family="sans-serif">built with</text><text x="102" y="15" text-anchor="middle" fill="#fff" font-size="11" font-family="sans-serif">Go + MyGo</text></svg>`))
		writeAsset("badge-ci.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="124" height="22" viewBox="0 0 124 22"><rect width="34" height="22" rx="4" fill="#374151"/><rect x="34" width="90" height="22" rx="4" fill="#2088ff"/><text x="17" y="15" text-anchor="middle" fill="#fff" font-size="11" font-family="sans-serif">CI</text><text x="79" y="15" text-anchor="middle" fill="#fff" font-size="11" font-family="sans-serif">GitHub Actions</text></svg>`))
		writeAsset("badge-mit.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="96" height="22" viewBox="0 0 96 22"><rect width="54" height="22" rx="4" fill="#374151"/><rect x="54" width="42" height="22" rx="4" fill="#C912D6"/><text x="27" y="15" text-anchor="middle" fill="#fff" font-size="11" font-family="sans-serif">license</text><text x="75" y="15" text-anchor="middle" fill="#fff" font-size="11" font-family="sans-serif">MIT</text></svg>`))

		showcase := strings.Join([]string{
			`<div align="center">`,
			``,
			`# HTML 与行内图片渲染验收`,
			``,
			`<a href="https://go.dev"><img src="assets/badge-go.svg" alt="Go + MyGo"></a>`,
			`<a href="https://github.com"><img src="assets/badge-ci.svg" alt="CI"></a>`,
			`[![MIT](assets/badge-mit.svg)](https://opensource.org/licenses/MIT)`,
			``,
			`</div>`,
			``,
			`句中行内图片：图标 <img src="assets/icon.png" width="20" height="20" alt="icon"> 跟随文字基线，支持保留 width 和 height。`,
			``,
			`同一行两张图片：<img src="assets/card-a.png" height="56"> <img src="assets/card-b.png" height="56">`,
			``,
			`<img src="assets/hero-right.png" width="260" align="right">`,
			`紧接在 align="right" 图片后的段落文本（MDZip 导出写法），图片按右对齐独立渲染，不再退化为源码文本。`,
			``,
			`<picture><source srcset="assets/card-a.webp"><img src="assets/card-a.png" width="180" alt="picture fallback"></picture>`,
		}, "\n") + "\n"

		rel, err := vault.CreateFile(sp.Root, "", "Issue94", ".md", showcase)
		if err != nil {
			t.Fatal(err)
		}
		a.refreshTree()
		a.open(rel)
		settle(tt)
		savePNG(t, filepath.Join(dir, "issue94-showcase.png"), tt)
	})

	// Render 2: Real repository README.md opened directly via local assets (remote badges
	// pre-seeded into cache as crisp SVGs so the offline snapshot shows the full header).
	t.Run("readme-header", func(t *testing.T) {
		repoRoot := filepath.Join("..", "..")
		readmeBytes, err := os.ReadFile(filepath.Join(repoRoot, "README.md"))
		if err != nil {
			t.Skipf("README.md not readable: %v", err)
		}
		a, tt := newTestApp(t)
		sp := a.state.ActiveSpace()
		for _, relAsset := range []string{
			"docs/images/readme/logo-horizontal.png",
			"docs/images/readme/flow-zh-CN.svg",
			"docs/images/download-macos-zh-CN.svg",
			"docs/images/download-windows-zh-CN.svg",
			"docs/images/download-linux-zh-CN.svg",
		} {
			srcBytes, rerr := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(relAsset)))
			if rerr != nil {
				t.Skipf("asset %s missing: %v", relAsset, rerr)
			}
			dst := filepath.Join(sp.Root, filepath.FromSlash(relAsset))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dst, srcBytes, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		seed := func(u, left, right, fill string) {
			svg := `<svg xmlns="http://www.w3.org/2000/svg" width="128" height="20" viewBox="0 0 128 20"><rect width="56" height="20" rx="3" fill="#555"/><rect x="56" width="72" height="20" rx="3" fill="` + fill + `"/><text x="28" y="14" text-anchor="middle" fill="#fff" font-size="11" font-family="sans-serif">` + left + `</text><text x="92" y="14" text-anchor="middle" fill="#fff" font-size="11" font-family="sans-serif">` + right + `</text></svg>`
			_, _ = a.cacheRemoteImageBytes(u, []byte(svg))
		}
		seed("https://img.shields.io/badge/version-0.5.1-F21158", "version", "0.5.1", "#F21158")
		seed("https://img.shields.io/badge/platform-macOS%20%7C%20Windows%20%7C%20Linux-F25F11", "platform", "mac|Win|Lin", "#F25F11")
		seed("https://img.shields.io/badge/built%20with-Go%20%2B%20MyGo-00ADD8?logo=go&logoColor=white", "built with", "Go + MyGo", "#00ADD8")
		seed("https://img.shields.io/badge/CI-GitHub%20Actions-2088ff?logo=githubactions&logoColor=white", "CI", "Actions", "#2088ff")
		seed("https://img.shields.io/badge/license-MIT-C912D6", "license", "MIT", "#C912D6")
		seed("https://awesomemygo.dev/badge.svg", "Awesome", "MyGo", "#16a34a")

		rel, err := vault.CreateFile(sp.Root, "", "README", ".md", string(readmeBytes))
		if err != nil {
			t.Fatal(err)
		}
		a.refreshTree()
		a.open(rel)
		settle(tt)
		savePNG(t, filepath.Join(dir, "issue94-readme-header.png"), tt)

		a.doc.ed.Scroll.Y = 580
		settle(tt)
		savePNG(t, filepath.Join(dir, "issue94-readme-downloads.png"), tt)
	})
}
