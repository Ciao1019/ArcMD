package pdf

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"strconv"
	"strings"
	"testing"
)

func TestWrite(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 60))
	img.Set(1, 1, color.Black)
	var out bytes.Buffer
	if err := Write(&out, "计划 🚀", []Page{{img, A4Width, A4Height}, {img, A4Width, A4Height}}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"%PDF-1.4", "/Count 2", "/MediaBox [0 0 595.28 841.89]", "/DCTDecode", "<FEFF8BA152120020D83DDE80>", "%%EOF"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	// Every object the xref names starts where it says.
	xref := strings.Index(s, "\nxref\n") + 1
	lines := strings.Split(s[xref:], "\n")[3:]
	for i, l := range lines[:9] {
		var off int
		if _, err := fmtSscan(l, &off); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(s[off:], itoa(i+1)+" 0 obj") {
			t.Errorf("object %d not at %d", i+1, off)
		}
	}
	if err := Write(&out, "", nil); err == nil {
		t.Error("wrote no pages")
	}
}

func fmtSscan(s string, off *int) (int, error) {
	n, err := strconv.Atoi(strings.Fields(s)[0])
	*off = n
	return 1, err
}
func itoa(n int) string { return fmt.Sprint(n) }
