package sysproxy

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

func mustProxy(t *testing.T, fn func(*url.URL) (*url.URL, error), raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	got, err := fn(u)
	if err != nil {
		t.Fatalf("proxy(%q): %v", raw, err)
	}
	if got == nil {
		return ""
	}
	return got.String()
}

func TestParseScutilProxy(t *testing.T) {
	sample := `<dictionary> {
  ExceptionsList : <array> {
    0 : 115.190.254.114
    1 : 192.168.*
    2 : *.xailab.cc
    3 : <local>
  }
  FTPPassive : 1
  HTTPEnable : 1
  HTTPPort : 7897
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 1
  HTTPSPort : 7897
  HTTPSProxy : 127.0.0.1
  ProxyAutoConfigEnable : 0
  SOCKSEnable : 1
  SOCKSPort : 7897
  SOCKSProxy : 127.0.0.1
}`
	cfg := parseScutilProxy(sample)
	if cfg.HTTPProxy != "http://127.0.0.1:7897" {
		t.Fatalf("HTTPProxy = %q, want http://127.0.0.1:7897", cfg.HTTPProxy)
	}
	if cfg.HTTPSProxy != "http://127.0.0.1:7897" {
		t.Fatalf("HTTPSProxy = %q, want http://127.0.0.1:7897", cfg.HTTPSProxy)
	}

	fn := cfg.ProxyFunc()
	cases := []struct {
		target string
		want   string
	}{
		{"https://github.com/Ciao1019/arcmd-plugins/releases/download/catalog/index.json", "http://127.0.0.1:7897"},
		{"http://example.com/", "http://127.0.0.1:7897"},
		{"https://127.0.0.1:8080/api", ""},
		{"https://localhost:3000/", ""},
		{"https://115.190.254.114/test", ""},
		{"https://192.168.2.100/test", ""},
		{"https://api.xailab.cc/v1", ""},
		{"http://intranet/docs", ""},
	}
	for _, tc := range cases {
		if got := mustProxy(t, fn, tc.target); got != tc.want {
			t.Errorf("proxy(%q) = %q, want %q", tc.target, got, tc.want)
		}
	}
}

func TestParseScutilSOCKSFallback(t *testing.T) {
	sample := `<dictionary> {
  HTTPEnable : 0
  HTTPSEnable : 0
  SOCKSEnable : 1
  SOCKSPort : 1080
  SOCKSProxy : 127.0.0.1
}`
	cfg := parseScutilProxy(sample)
	fn := cfg.ProxyFunc()
	if got := mustProxy(t, fn, "https://github.com/"); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("https proxy = %q, want socks5://127.0.0.1:1080", got)
	}
	if got := mustProxy(t, fn, "http://example.com/"); got != "socks5://127.0.0.1:1080" {
		t.Fatalf("http proxy = %q, want socks5://127.0.0.1:1080", got)
	}
}

func TestParseScutilDisabled(t *testing.T) {
	sample := `<dictionary> {
  HTTPEnable : 0
  HTTPPort : 7897
  HTTPProxy : 127.0.0.1
  HTTPSEnable : 0
  SOCKSEnable : 0
}`
	cfg := parseScutilProxy(sample)
	if !cfg.Empty() {
		t.Fatalf("expected empty config when all proxies disabled, got %+v", cfg)
	}
}

func TestParseWindowsProxy(t *testing.T) {
	cfg := parseWindowsProxy("127.0.0.1:7897", "localhost;127.*;192.168.*;*.corp.local;<local>")
	fn := cfg.ProxyFunc()
	if got := mustProxy(t, fn, "https://github.com/"); got != "http://127.0.0.1:7897" {
		t.Fatalf("https proxy = %q, want http://127.0.0.1:7897", got)
	}
	if got := mustProxy(t, fn, "https://a.corp.local/"); got != "" {
		t.Fatalf("bypassed host got %q", got)
	}
	if got := mustProxy(t, fn, "https://192.168.1.10/"); got != "" {
		t.Fatalf("bypassed CIDR host got %q", got)
	}
	if got := mustProxy(t, fn, "http://fileserver/share"); got != "" {
		t.Fatalf("bypassed <local> host got %q", got)
	}

	multi := parseWindowsProxy("http=127.0.0.1:8001;https=127.0.0.1:8002;socks=127.0.0.1:1080", "")
	mfn := multi.ProxyFunc()
	if got := mustProxy(t, mfn, "http://example.com/"); got != "http://127.0.0.1:8001" {
		t.Fatalf("http = %q, want http://127.0.0.1:8001", got)
	}
	if got := mustProxy(t, mfn, "https://example.com/"); got != "http://127.0.0.1:8002" {
		t.Fatalf("https = %q, want http://127.0.0.1:8002", got)
	}
}

func TestResolverPrecedenceAndCache(t *testing.T) {
	// Clear proxy env vars for deterministic testing.
	for _, k := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy"} {
		t.Setenv(k, "")
	}

	now := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	loads := 0
	current := Config{HTTPProxy: "http://127.0.0.1:7897", HTTPSProxy: "http://127.0.0.1:7897"}
	r := &resolver{
		ttl: 5 * time.Second,
		now: func() time.Time { return now },
		load: func() Config {
			loads++
			return current
		},
	}

	req, _ := http.NewRequest(http.MethodGet, "https://github.com/Ciao1019/arcmd-plugins", nil)
	got, err := r.proxyFor(req)
	if err != nil || got == nil || got.String() != "http://127.0.0.1:7897" {
		t.Fatalf("first proxyFor = (%v, %v), want http://127.0.0.1:7897", got, err)
	}
	if loads != 1 {
		t.Fatalf("loads = %d, want 1", loads)
	}

	// Within TTL, cached config is reused.
	now = now.Add(2 * time.Second)
	current = Config{}
	got, err = r.proxyFor(req)
	if err != nil || got == nil || got.String() != "http://127.0.0.1:7897" {
		t.Fatalf("cached proxyFor = (%v, %v), want cached http://127.0.0.1:7897", got, err)
	}
	if loads != 1 {
		t.Fatalf("loads within TTL = %d, want 1", loads)
	}

	// After TTL expires, OS config is reloaded.
	now = now.Add(4 * time.Second)
	got, err = r.proxyFor(req)
	if err != nil || got != nil {
		t.Fatalf("post-TTL proxyFor = (%v, %v), want (nil, nil)", got, err)
	}
	if loads != 2 {
		t.Fatalf("loads after TTL = %d, want 2", loads)
	}

	// Explicit env var takes precedence over OS config.
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9999")
	got, err = r.proxyFor(req)
	if err != nil || got == nil || got.String() != "http://127.0.0.1:9999" {
		t.Fatalf("env override proxyFor = (%v, %v), want http://127.0.0.1:9999", got, err)
	}
}
