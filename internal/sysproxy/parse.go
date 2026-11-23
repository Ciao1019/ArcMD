package sysproxy

import (
	"net"
	"strconv"
	"strings"
)

// parseScutilProxy parses the output of macOS `scutil --proxy`.
func parseScutilProxy(out string) Config {
	vals := make(map[string]string)
	var exceptions []string
	inExceptions := false

	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if inExceptions {
			if line == "}" {
				inExceptions = false
				continue
			}
			if _, val, ok := strings.Cut(line, ":"); ok {
				if v := strings.TrimSpace(val); v != "" {
					exceptions = append(exceptions, v)
				}
			}
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "ExceptionsList" && strings.HasPrefix(val, "<array>") {
			inExceptions = true
			continue
		}
		vals[key] = val
	}

	var httpProxy, httpsProxy, socksProxy string
	if vals["HTTPEnable"] == "1" {
		httpProxy = formatEndpoint("http", vals["HTTPProxy"], vals["HTTPPort"])
	}
	if vals["HTTPSEnable"] == "1" {
		httpsProxy = formatEndpoint("http", vals["HTTPSProxy"], vals["HTTPSPort"])
	}
	if vals["SOCKSEnable"] == "1" {
		socksProxy = formatEndpoint("socks5", vals["SOCKSProxy"], vals["SOCKSPort"])
	}

	if httpsProxy == "" {
		httpsProxy = httpProxy
	}
	if httpsProxy == "" {
		httpsProxy = socksProxy
	}
	if httpProxy == "" {
		httpProxy = socksProxy
	}

	noProxy, bypassLocal := buildNoProxy(exceptions)
	return Config{
		HTTPProxy:   httpProxy,
		HTTPSProxy:  httpsProxy,
		NoProxy:     noProxy,
		BypassLocal: bypassLocal,
	}
}

// parseWindowsProxy parses Windows Internet Settings ProxyServer and
// ProxyOverride registry values (assuming ProxyEnable == 1).
func parseWindowsProxy(server, override string) Config {
	server = strings.TrimSpace(server)
	if server == "" {
		return Config{}
	}

	var httpProxy, httpsProxy, socksProxy string
	if strings.Contains(server, "=") {
		for _, part := range strings.Split(server, ";") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			scheme, addr, ok := strings.Cut(part, "=")
			if !ok {
				continue
			}
			scheme = strings.ToLower(strings.TrimSpace(scheme))
			addr = strings.TrimSpace(addr)
			switch scheme {
			case "http":
				httpProxy = normalizeProxyURL("http", addr)
			case "https":
				httpsProxy = normalizeProxyURL("http", addr)
			case "socks", "socks5":
				socksProxy = normalizeProxyURL("socks5", addr)
			}
		}
	} else {
		p := normalizeProxyURL("http", server)
		httpProxy = p
		httpsProxy = p
	}

	if httpsProxy == "" {
		httpsProxy = httpProxy
	}
	if httpsProxy == "" {
		httpsProxy = socksProxy
	}
	if httpProxy == "" {
		httpProxy = socksProxy
	}

	var exceptions []string
	for _, entry := range strings.Split(override, ";") {
		if e := strings.TrimSpace(entry); e != "" {
			exceptions = append(exceptions, e)
		}
	}
	noProxy, bypassLocal := buildNoProxy(exceptions)
	return Config{
		HTTPProxy:   httpProxy,
		HTTPSProxy:  httpsProxy,
		NoProxy:     noProxy,
		BypassLocal: bypassLocal,
	}
}

func formatEndpoint(scheme, host, port string) string {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if host == "" {
		return ""
	}
	if strings.Contains(host, "://") {
		return host
	}
	if port != "" && port != "0" {
		return scheme + "://" + net.JoinHostPort(host, port)
	}
	return scheme + "://" + host
}

func normalizeProxyURL(defaultScheme, addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if strings.Contains(addr, "://") {
		return addr
	}
	return defaultScheme + "://" + addr
}

func buildNoProxy(entries []string) (string, bool) {
	var out []string
	bypassLocal := false
	for _, raw := range entries {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		if strings.EqualFold(e, "<local>") {
			bypassLocal = true
			continue
		}
		if norm := normalizeNoProxyEntry(e); norm != "" {
			out = append(out, norm)
		}
	}
	return strings.Join(out, ","), bypassLocal
}

func normalizeNoProxyEntry(e string) string {
	if e == "*" {
		return "*"
	}
	// Convert wildcard IPv4 patterns like "192.168.*" or "10.*" to CIDR.
	if strings.HasSuffix(e, ".*") {
		if cidr, ok := wildcardIPv4ToCIDR(e); ok {
			return cidr
		}
	}
	// Convert domain wildcards like "*.local" to ".local" for httpproxy.
	if strings.HasPrefix(e, "*.") {
		return e[1:]
	}
	return e
}

func wildcardIPv4ToCIDR(pattern string) (string, bool) {
	parts := strings.Split(pattern, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return "", false
	}
	var octets [4]string
	prefixBits := 0
	wildcardSeen := false
	for i, p := range parts {
		if p == "*" {
			wildcardSeen = true
			continue
		}
		if wildcardSeen {
			return "", false
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return "", false
		}
		octets[i] = strconv.Itoa(n)
		prefixBits += 8
	}
	if !wildcardSeen || prefixBits == 0 {
		return "", false
	}
	for i := prefixBits / 8; i < 4; i++ {
		octets[i] = "0"
	}
	return strings.Join(octets[:], ".") + "/" + strconv.Itoa(prefixBits), true
}
