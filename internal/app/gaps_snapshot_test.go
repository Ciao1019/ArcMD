package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Ciao1019/ArcMD/internal/editor"
	"github.com/Ciao1019/ArcMD/internal/vault"
)

func TestGapSnapshot(t *testing.T) {
	dir := snapshotDir(t, "set ARCMD_SNAPSHOT to render the gap below a closing block")
	for _, name := range []string{"light", "dark"} {
		t.Run(name, func(t *testing.T) {
			a, tt := newTestApp(t)
			doc := editor.ImportMarkdown("# Ideas\n\n> [!WARNING]\n> 注意\n\n```go\nfmt.Println(1)\n```\n")
			encoded, err := doc.Encode()
			if err != nil {
				t.Fatal(err)
			}
			if err := vault.Write(root(a), "Ideas.arcmd", encoded); err != nil {
				t.Fatal(err)
			}
			a.refreshTree()
			a.open("Ideas.arcmd")
			tt.SetSize(1240, 760)
			tt.SetDark(name == "dark")
			tt.Frame()
			tt.Advance(time.Second)
			footer, ok := tt.Find("反向链接")
			if !ok {
				t.Fatal("no footer to find the end of the page by")
			}
			tt.Move(footer.X+200, footer.Y-22)
			tt.Frame()
			savePNG(t, filepath.Join(dir, "gap-"+name+".png"), tt)
		})
	}
}
