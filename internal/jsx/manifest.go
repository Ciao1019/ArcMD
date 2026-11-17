// Package jsx compiles block-level MDX islands for ArcMD's native editor.
// The compiler does not rewrite the user's source file; hosts project the
// compiled markdown into the editor and restore JSX fences when saving.
package jsx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Manifest is the compiler's JSON output (version 1).
type Manifest struct {
	Namespace    string   `json:"namespace"`
	Version      int      `json:"version"`
	SourceSHA256 string   `json:"sourceSha256"`
	Markdown     string   `json:"markdown"`
	Blocks       []Block  `json:"blocks"`
	Diagnostics  []string `json:"diagnostics"`
}

// Block is one compiled JSX island. Height is only a pre-measure placeholder.
type Block struct {
	Language string  `json:"language"`
	Compiled string  `json:"compiled,omitempty"`
	Source   string  `json:"source"`
	HTML     string  `json:"html"`
	Error    string  `json:"error"`
	Height   float32 `json:"height"`
}

// ErrUnsupportedManifest is returned when the compiler output cannot be used.
var ErrUnsupportedManifest = errors.New("unsupported JSX manifest")

func ParseManifest(r io.Reader) (Manifest, error) {
	var m Manifest
	data, err := io.ReadAll(io.LimitReader(r, (16<<20)+1))
	if err != nil {
		return m, err
	}
	if len(data) > 16<<20 {
		return m, errors.New("JSX manifest exceeds 16 MiB")
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.Version != 1 {
		return m, fmt.Errorf("%w: version %d", ErrUnsupportedManifest, m.Version)
	}
	namespace, err := hex.DecodeString(m.Namespace)
	if err != nil || len(namespace) != 16 {
		return m, fmt.Errorf("%w: invalid namespace", ErrUnsupportedManifest)
	}
	if len(m.Blocks) > 32 {
		return m, fmt.Errorf("%w: too many islands", ErrUnsupportedManifest)
	}
	for i, block := range m.Blocks {
		if block.Language != fmt.Sprintf("arcmd-jsx-%s-%d", m.Namespace, i) {
			return m, fmt.Errorf("%w: invalid island identity", ErrUnsupportedManifest)
		}
	}
	return m, nil
}

func SourceSHA256(source string) string {
	sum := sha256.Sum256([]byte(source))
	return hex.EncodeToString(sum[:])
}

func IsMDX(name string) bool {
	return strings.EqualFold(ext(name), ".mdx")
}

func ext(name string) string {
	i := strings.LastIndex(name, ".")
	if i < 0 {
		return ""
	}
	return name[i:]
}

func (m Manifest) HasHTML() bool {
	for _, b := range m.Blocks {
		if b.Error == "" && b.HTML != "" {
			return true
		}
	}
	return false
}

func (m Manifest) LanguageOf(i int) string {
	if i < 0 || i >= len(m.Blocks) {
		return ""
	}
	return m.Blocks[i].Language
}

func IsIslandLanguage(language, namespace string) bool {
	if namespace == "" || language == "" {
		return false
	}
	if language == "mdx-source-only-"+namespace {
		return true
	}
	prefix := "arcmd-jsx-" + namespace + "-"
	if !strings.HasPrefix(language, prefix) {
		return false
	}
	n := language[len(prefix):]
	if n == "" {
		return false
	}
	for _, r := range n {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
