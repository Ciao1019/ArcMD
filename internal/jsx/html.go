package jsx

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
)

//go:embed assets/react-runtime.js
var reactRuntime string

//go:embed assets/island-wrapper.js
var islandWrapper string

//go:embed assets/style.css
var islandStyle string

func cspHash(s string) string {
	v := sha256.Sum256([]byte(s))
	return base64.StdEncoding.EncodeToString(v[:])
}
func islandPage(source, compiled string) string {
	raw, _ := json.Marshal(source)
	// Insert compiled code last: source text cannot expand a template token.
	js := strings.Replace(islandWrapper, "__SOURCE_JSON__", string(raw), 1)
	js = strings.Replace(js, "__COMPILED_BODY__", compiled, 1)
	js = reactRuntime + "\n" + js
	js = regexp.MustCompile(`(?i)</script`).ReplaceAllStringFunc(js, func(s string) string { return "<\\/" + s[2:] })
	policy := "default-src 'none'; script-src 'sha256-" + cspHash(js) + "'; style-src 'sha256-" + cspHash(islandStyle) + "'; base-uri 'none'; form-action 'none'; frame-src 'none'; connect-src 'none'; object-src 'none'"
	return "<!doctype html><html><head><meta charset=\"utf-8\"><meta http-equiv=\"Content-Security-Policy\" content=\"" + policy + "\"><style>" + islandStyle + "</style></head><body><div id=\"root\"></div><script>" + js + "</script></body></html>"
}
