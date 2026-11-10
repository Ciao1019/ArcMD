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
	if err := Write(&out, "计划 🚀", []Page{{Image: img, Width: A4Width, Height: A4Height}, {Image: img, Width: A4Width, Height: A4Height}}); err != nil {
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
	for i, l := range lines[:13] {
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

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, fmt.Errorf("disk full") }

func TestStreamReportsFailureAndUnicode(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	w := NewWriter(failingWriter{}, "test")
	if err := w.AddPage(Page{Image: img, Width: 100, Height: 100}); err == nil {
		t.Fatal("write failure hidden")
	}
	var out bytes.Buffer
	w = NewWriter(&out, "test")
	for i := 0; i < 3; i++ {
		before := out.Len()
		if err := w.AddPage(Page{Image: img, Width: 100, Height: 100, Text: []Text{{Value: "中", X: 10, Y: 10, Width: 12, Height: 12}, {Value: "🚀", X: 22, Y: 10, Width: 12, Height: 12}}}); err != nil {
			t.Fatal(err)
		}
		if out.Len() <= before {
			t.Fatal("page was buffered until close")
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<0001> <4E2D>", "<0002> <D83DDE80>", "/Count 3"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal("missing", want)
		}
	}
}
