package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ciao1019/ArcMD/internal/vault"
)

// An exported page keeps the editor's code highlighting and draws Mermaid
// blocks as vector diagrams instead of their source.
func TestExportHTMLDrawsCodeAndDiagrams(t *testing.T) {
	a, tt := newTestApp(t)
	settle(tt)
	source := "# 测试\n\n```java\npublic static void main() {}\n```\n\n" +
		"```mermaid\nflowchart LR\n  A[开始] --> B[结束]\n```\n"
	if err := vault.Write(root(a), "Diagram.md", source); err != nil {
		t.Fatal(err)
	}
	a.open("Diagram.md")
	settle(tt)
	a.exportHTML()
	page, err := os.ReadFile(filepath.Join(root(a), "Diagram.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<span class="arcmd-tok-keyword">public</span>`,
		`<figure class="arcmd-mermaid"><span class="arcmd-diagram-light"><svg`,
		`<span class="arcmd-diagram-dark"><svg`,
		"开始",
		"结束",
		".arcmd-tok-keyword",
		"figure.arcmd-mermaid",
	} {
		if !strings.Contains(string(page), want) {
			t.Fatalf("exported page missing %q", want)
		}
	}
	if strings.Contains(string(page), "language-mermaid") {
		t.Fatal("exported page still shows the Mermaid source")
	}
}

func TestHTMLPageStylesCodeAndDiagrams(t *testing.T) {
	page := htmlPage("Title", `<figure class="arcmd-mermaid"><svg></svg></figure>`+
		`<pre><code class="language-go"><span class="arcmd-tok-keyword">func</span></code></pre>`)
	for _, want := range []string{
		"<title>Title</title>",
		".arcmd-tok-keyword { color: #b2565c; }",
		".arcmd-tok-keyword { color: #e8838a; }",
		"figure.arcmd-mermaid svg { max-width: 100%; height: auto; }",
		"figure.arcmd-mermaid .arcmd-diagram-dark { display: none; }",
		"figure.arcmd-mermaid .arcmd-diagram-light { display: none; }",
		`<span class="arcmd-tok-keyword">func</span>`,
		"prefers-color-scheme: dark",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("exported page missing %q", want)
		}
	}
}
