package app

import (
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	model "github.com/Ciao1019/ArcMD/internal/document"
	"github.com/Ciao1019/ArcMD/internal/prefs"
	"github.com/Ciao1019/ArcMD/internal/vault"
	"github.com/egoist/mygo/ui"
)

func TestDarkPaperPresetsQuoteBarContrastOnBlockBackgrounds(t *testing.T) {
	srgbLin := func(c uint8) float64 {
		v := float64(c) / 255.0
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	over := func(fg, bg ui.Color) [3]uint8 {
		f, b := fg.SRGB(), bg.SRGB()
		a := float64(f.A) / 255.0
		blend := func(fc, bc uint8) uint8 {
			return uint8(math.Round(float64(fc)*a + float64(bc)*(1-a)))
		}
		return [3]uint8{blend(f.R, b.R), blend(f.G, b.G), blend(f.B, b.B)}
	}
	contrast := func(fg, bg ui.Color) float64 {
		fgRGB := over(fg, bg)
		b := bg.SRGB()
		l1 := 0.2126*srgbLin(fgRGB[0]) + 0.7152*srgbLin(fgRGB[1]) + 0.0722*srgbLin(fgRGB[2])
		l2 := 0.2126*srgbLin(b.R) + 0.7152*srgbLin(b.G) + 0.0722*srgbLin(b.B)
		if l1 < l2 {
			l1, l2 = l2, l1
		}
		return (l1 + 0.05) / (l2 + 0.05)
	}

	for _, preset := range []string{"", "warm", "sepia", "sage", "slate"} {
		cfg := prefs.BuiltinPaperStyle(preset)
		th := documentThemeWith(true, cfg)
		pg := pageStyleWith(true, cfg)
		surfaces := map[string]ui.Color{"paper": th.Background}
		for _, name := range model.TextHighlights {
			fill, ok := pg.CellFills[name]
			if !ok {
				t.Fatalf("preset %q missing dark CellFills[%q]", preset, name)
			}
			rgb := over(fill, th.Background)
			surfaces[name] = ui.RGBA(rgb[0], rgb[1], rgb[2], 255)
		}
		for name, bg := range surfaces {
			if r := contrast(th.Text, bg); r < 4.5 {
				t.Errorf("preset %q dark %s: body text contrast %.2f < 4.5", preset, name, r)
			}
			if r := contrast(pg.QuoteBar, bg); r < 3.0 {
				t.Errorf("preset %q dark %s: QuoteBar contrast %.2f < 3.0", preset, name, r)
			}
		}
	}
}

func TestPortableStyleSecuritySymlinkReadOnlyAndPathSwitchReload(t *testing.T) {
	a, tt := newTestApp(t)
	userPath := a.userStyleConfigPath()
	portDir := t.TempDir()
	portablePath := filepath.Join(portDir, prefs.StyleFileName)
	a.styleCfg.portablePath = portablePath

	fixedMtime := time.Now().Add(-5 * time.Second).Truncate(time.Second)
	writeWithFixedMtime := func(p, content string, perm os.FileMode) {
		t.Helper()
		if err := os.WriteFile(p, []byte(content), perm); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, fixedMtime, fixedMtime); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Both files have identical byte length (29 bytes) and identical mtime:
	//loadedPath tracking must still detect switching from userPath -> portablePath -> userPath.
	const userJSON = `{"light":{"paper":"#FAF8F5"}}`
	const portJSON = `{"light":{"paper":"#EEF6FF"}}`
	if len(userJSON) != len(portJSON) {
		t.Fatalf("expected equal byte lengths, got %d vs %d", len(userJSON), len(portJSON))
	}
	writeWithFixedMtime(userPath, userJSON, 0o644)
	tt.Advance(styleConfigPollInterval + 50*time.Millisecond)
	tt.Frame()
	if got := a.documentTheme(false).Background; got != ui.Hex("#FAF8F5") {
		t.Fatalf("expected user paper #FAF8F5, got %+v", got)
	}

	writeWithFixedMtime(portablePath, portJSON, 0o644)
	tt.Advance(styleConfigPollInterval + 50*time.Millisecond)
	tt.Frame()
	if got := a.documentTheme(false).Background; got != ui.Hex("#EEF6FF") {
		t.Fatalf("same-size/same-mtime portable override did not reload: got %+v", got)
	}

	// 2. Replacing portablePath with a symlink must be rejected by os.Lstat, falling back to userPath.
	if err := os.Remove(portablePath); err != nil {
		t.Fatal(err)
	}
	targetFile := filepath.Join(t.TempDir(), "evil-style.json")
	writeWithFixedMtime(targetFile, `{"light":{"paper":"#FFE4E6"}}`, 0o644)
	if err := os.Symlink(targetFile, portablePath); err == nil {
		if got := a.styleConfigPath(); got != userPath {
			t.Fatalf("symlink portable style.json was accepted: %q; want %q", got, userPath)
		}
		tt.Advance(styleConfigPollInterval + 50*time.Millisecond)
		tt.Frame()
		if got := a.documentTheme(false).Background; got != ui.Hex("#FAF8F5") {
			t.Fatalf("after symlink rejection, paper = %+v; want #FAF8F5", got)
		}
		_ = os.Remove(portablePath)
	}

	// 3. Switching to a malformed portable file clears cross-file stale active config.
	writeWithFixedMtime(portablePath, `{"light":{"paper":"invalid"}}`, 0o644)
	tt.Advance(styleConfigPollInterval + 50*time.Millisecond)
	tt.Frame()
	if got := a.documentTheme(false).Background; got != ui.Hex("#FFFFFF") {
		t.Fatalf("malformed portable file leaked stale user paper: got %+v; want #FFFFFF", got)
	}

	// 4. Read-only portable style.json falls back to opening writable user style.json.
	if err := os.Chmod(portablePath, 0o444); err == nil {
		t.Cleanup(func() { _ = os.Chmod(portablePath, 0o644) })
		// Ensure we aren't running as root where 0o444 is still writable.
		if f, werr := os.OpenFile(portablePath, os.O_WRONLY, 0); werr != nil {
			var opened string
			a.styleCfg.openFile = func(p string) { opened = p }
			a.openStyleConfigFile()
			if opened != userPath {
				t.Fatalf("read-only portable openStyleConfigFile opened %q; want %q", opened, userPath)
			}
		} else {
			_ = f.Close()
		}
	}
}

func TestRemoteImageRedirectSchemeGuardAndMalformedSVG(t *testing.T) {
	a, _ := newTestApp(t)
	var targetURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect-file":
			http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
		case "/redirect-http":
			http.Redirect(w, r, targetURL+"/ok.svg", http.StatusFound)
		case "/zero.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="0" height="0"></svg>`))
		case "/inf.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1e40" height="1e40"></svg>`))
		case "/ok.svg":
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20"/></svg>`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	targetURL = srv.URL

	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/ok.svg", http.StatusFound)
	}))
	defer tlsSrv.Close()

	// CheckRedirect mirroring remoteImageResourceWithScheme blocks file:// and https->http downgrade.
	baseClient := tlsSrv.Client()
	httpsOnlyClient := *baseClient
	httpsOnlyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL == nil || strings.ToLower(req.URL.Scheme) != "https" {
			return os.ErrPermission
		}
		return nil
	}
	if err := a.fetchAndCacheRemoteImageWithScheme(&httpsOnlyClient, tlsSrv.URL+"/downgrade", true); err == nil {
		t.Fatal("expected https->http downgrade redirect to be rejected for httpsOnly (.mdz)")
	}

	httpClient := *srv.Client()
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		s := strings.ToLower(req.URL.Scheme)
		if s != "http" && s != "https" {
			return os.ErrPermission
		}
		return nil
	}
	if err := a.fetchAndCacheRemoteImage(&httpClient, srv.URL+"/redirect-file"); err == nil {
		t.Fatal("expected redirect to file:// scheme to be rejected")
	}
	if err := a.fetchAndCacheRemoteImage(&httpClient, srv.URL+"/zero.svg"); err == nil {
		t.Fatal("expected 0x0 remote SVG to be rejected")
	}
	if err := a.fetchAndCacheRemoteImage(&httpClient, srv.URL+"/inf.svg"); err == nil {
		t.Fatal("expected +Inf remote SVG to be rejected")
	}
	if err := a.fetchAndCacheRemoteImage(&httpClient, srv.URL+"/ok.svg"); err != nil {
		t.Fatalf("valid remote SVG failed: %v", err)
	}
}

func TestImageHostUploadsLocalInlineImages(t *testing.T) {
	s3, srv := newMockImageS3(t)
	a, tt := newTestApp(t)
	a.pluginsUI.sync = true
	var trashed []string
	a.trashFile = func(p string) error {
		trashed = append(trashed, p)
		return os.RemoveAll(p)
	}
	sp := a.state.ActiveSpace()

	installImageHostViaUI(t, a, tt)
	configureImageHostS3(t, a, tt, srv.URL, "", false, true, true)

	assetsDir := filepath.Join(sp.Root, "assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	localBadge := filepath.Join(assetsDir, "badge.png")
	if err := os.WriteFile(localBadge, encodePNG(t, 24, 12), 0o644); err != nil {
		t.Fatal(err)
	}

	md := "# Inline Upload\n\nStatus <img src=\"assets/badge.png\" alt=\"CI\" width=\"48\"> ok\n"
	if err := vault.Write(sp.Root, "InlineHost.md", md); err != nil {
		t.Fatal(err)
	}
	a.refreshTree()
	a.open("InlineHost.md")
	settle(tt)

	// Trigger "图床上传：上传当前文档本地图片" via command palette on a document with ONLY inline local images.
	a.openPalette()
	a.palette.query = "上传当前文档本地图片"
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	settle(tt)

	s3.mu.Lock()
	puts := len(s3.putReqs)
	s3.mu.Unlock()
	if puts != 1 {
		t.Fatalf("expected 1 inline local image uploaded to S3, got %d", puts)
	}
	runs := a.doc.ed.Doc.Blocks[1].Runs
	foundRemote := false
	for _, r := range runs {
		if r.IsImage() && strings.HasPrefix(r.Image.URL, srv.URL+"/") {
			foundRemote = true
		}
	}
	if !foundRemote {
		t.Fatalf("inline image URL was not rewritten to remote S3 URL: %+v", runs)
	}
	if len(trashed) != 1 || !strings.HasSuffix(trashed[0], "/assets/badge.png") {
		t.Fatalf("expected unreferenced local inline image trashed, got %v", trashed)
	}
}
