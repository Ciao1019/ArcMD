package clipper

import (
	"net/url"
	"sort"
	"strings"
	"unicode"
)

var trackingQueryKeys = map[string]bool{
	"fbclid":         true,
	"gclid":          true,
	"dclid":          true,
	"msclkid":        true,
	"yclid":          true,
	"igshid":         true,
	"spm":            true,
	"scm":            true,
	"ref_src":        true,
	"ref_url":        true,
	"mc_cid":         true,
	"mc_eid":         true,
	"chksm":          true,
	"scene":          true,
	"poc_token":      true,
	"from":           true,
	"isappinstalled": true,
}

// NormalizeURL canonicalizes a web page URL for duplicate clipping detection:
// lowercases scheme/host, strips default ports and fragments, removes common
// tracking parameters, sorts remaining query parameters, and trims trailing slash.
func NormalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return strings.TrimRight(strings.ToLower(raw), "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		if strings.Contains(host, ":") {
			u.Host = "[" + host + "]:" + port
		} else {
			u.Host = host + ":" + port
		}
	} else if strings.Contains(host, ":") {
		u.Host = "[" + host + "]"
	} else {
		u.Host = host
	}
	u.Fragment = ""
	if u.Path != "/" {
		u.Path = strings.TrimRight(u.Path, "/")
	}
	if u.Path == "" {
		u.Path = "/"
	}
	if u.RawQuery != "" {
		q := u.Query()
		clean := url.Values{}
		keys := make([]string, 0, len(q))
		for k := range q {
			lk := strings.ToLower(k)
			if strings.HasPrefix(lk, "utm_") || trackingQueryKeys[lk] {
				continue
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			vals := append([]string(nil), q[k]...)
			sort.Strings(vals)
			clean[k] = vals
		}
		u.RawQuery = clean.Encode()
	}
	return u.String()
}

// SafeDocTitle turns an arbitrary webpage title into a safe cross-platform
// Markdown filename stem (no path separators, Windows reserved chars, or control
// characters), capping length to 120 runes.
func SafeDocTitle(raw string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range raw {
		if unicode.IsControl(r) || r == '\uFFFD' {
			continue
		}
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		if unicode.IsSpace(r) {
			if !prevSpace && b.Len() > 0 {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	out := strings.Trim(b.String(), " .")
	if out == "" {
		return "Untitled Clip"
	}
	runes := []rune(out)
	if len(runes) > 120 {
		out = strings.Trim(string(runes[:120]), " .")
	}
	if out == "" {
		return "Untitled Clip"
	}
	return out
}
