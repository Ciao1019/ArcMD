// Package sysproxy resolves HTTP/HTTPS/SOCKS5 proxy settings from the
// environment first and falls back to the operating system's proxy
// configuration when no proxy environment variables are set.
package sysproxy

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http/httpproxy"
)

// Config holds normalized system proxy settings.
type Config struct {
	HTTPProxy   string
	HTTPSProxy  string
	NoProxy     string
	BypassLocal bool // true when <local> is present in bypass rules
}

// Empty reports whether no proxy endpoints are configured.
func (c Config) Empty() bool {
	return c.HTTPProxy == "" && c.HTTPSProxy == ""
}

// ProxyFunc builds a proxy resolver for c.
func (c Config) ProxyFunc() func(*url.URL) (*url.URL, error) {
	if c.Empty() {
		return func(*url.URL) (*url.URL, error) { return nil, nil }
	}
	hp := (&httpproxy.Config{
		HTTPProxy:  c.HTTPProxy,
		HTTPSProxy: c.HTTPSProxy,
		NoProxy:    c.NoProxy,
	}).ProxyFunc()
	bypassLocal := c.BypassLocal
	return func(u *url.URL) (*url.URL, error) {
		if u == nil {
			return nil, nil
		}
		if bypassLocal && isLocalHostname(u.Hostname()) {
			return nil, nil
		}
		return hp(u)
	}
}

func isLocalHostname(host string) bool {
	return host != "" && !strings.Contains(host, ".") && !strings.Contains(host, ":")
}

const defaultCacheTTL = 5 * time.Second

type resolver struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	load    func() Config
	expires time.Time
	fn      func(*url.URL) (*url.URL, error)
}

var defaultResolver = &resolver{
	ttl:  defaultCacheTTL,
	now:  time.Now,
	load: loadOSConfig,
}

func (r *resolver) proxyFor(req *http.Request) (*url.URL, error) {
	if req == nil || req.URL == nil {
		return nil, nil
	}
	if hasEnvProxy() {
		return http.ProxyFromEnvironment(req)
	}
	fn := r.cachedFunc()
	return fn(req.URL)
}

func (r *resolver) cachedFunc() func(*url.URL) (*url.URL, error) {
	now := r.now()
	r.mu.Lock()
	if r.fn != nil && now.Before(r.expires) {
		fn := r.fn
		r.mu.Unlock()
		return fn
	}
	r.mu.Unlock()

	cfg := r.load()
	fn := cfg.ProxyFunc()

	r.mu.Lock()
	r.fn = fn
	r.expires = r.now().Add(r.ttl)
	r.mu.Unlock()
	return fn
}

func hasEnvProxy() bool {
	for _, k := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if strings.TrimSpace(os.Getenv(k)) != "" {
			return true
		}
	}
	return false
}

// Proxy returns the proxy URL for req, honoring HTTP_PROXY/HTTPS_PROXY
// environment variables first and falling back to OS proxy settings.
func Proxy(req *http.Request) (*url.URL, error) {
	return defaultResolver.proxyFor(req)
}

// Install configures http.DefaultTransport to use Proxy so that all standard
// HTTP clients in the process respect system proxy settings.
func Install() {
	if tr, ok := http.DefaultTransport.(*http.Transport); ok && tr != nil {
		tr.Proxy = Proxy
	}
}
