package document

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestInlineImageModelMigrationAndValidation(t *testing.T) {
	v10 := `{"format":"arcmd","version":10,"blocks":[{"id":"p","kind":"paragraph","runs":[{"text":"Hi"}]}]}`
	migrated, err := Decode(v10)
	if err != nil || migrated.Version != CurrentVersion {
		t.Fatalf("v10 migration failed: doc=%+v err=%v", migrated, err)
	}

	p := NewBlock("paragraph", "")
	p.Runs = Compact([]Run{
		{Text: "Before "},
		InlineImageRun(InlineImage{URL: "assets/icon.png", Alt: "icon", Width: 20, Height: 20}, Style{}),
		InlineImageRun(InlineImage{URL: "assets/icon.png", Alt: "icon", Width: 20, Height: 20}, Style{}),
		{Text: " "},
		InlineImageRun(InlineImage{URL: "assets/badge.svg", Alt: "CI", Height: 20}, Style{Link: "https://example.com/ci"}),
		{Text: " after"},
	})
	// Identical adjacent inline image runs must stay distinct (never merged by Compact).
	if len(p.Runs) != 6 {
		t.Fatalf("Compact merged inline image runs: got %d runs %+v", len(p.Runs), p.Runs)
	}

	img := NewBlock("image", "Banner")
	img.URL = "assets/banner.png"
	img.ImageWidth = 400
	img.ImageHeight = 120
	img.Align = "right"

	d := &Document{Format: "arcmd", Version: CurrentVersion, Blocks: []*Block{p, img}}
	s, err := d.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}
	decoded, err := Decode(s)
	if err != nil || !reflect.DeepEqual(decoded, d) || !SameDocument(decoded, d) {
		t.Fatalf("v11 round-trip mismatch: err=%v", err)
	}
	if d1, err1 := d.Digest(); err1 != nil {
		t.Fatalf("Digest failed: %v", err1)
	} else {
		mod := d.Clone()
		mod.Blocks[0].Runs[1].Image.Width = 24
		d2, err2 := mod.Digest()
		if err2 != nil || d1 == d2 {
			t.Fatalf("Digest did not reflect inline image change: err=%v", err2)
		}
		modH := d.Clone()
		modH.Blocks[1].ImageHeight = 140
		d3, err3 := modH.Digest()
		if err3 != nil || d1 == d3 {
			t.Fatalf("Digest did not reflect block imageHeight change: err=%v", err3)
		}
	}

	// Older v10 payload must reject v11 fields.
	if _, err := Decode(strings.Replace(s, fmt.Sprintf(`"version": %d`, CurrentVersion), `"version": 10`, 1)); err == nil {
		t.Fatal("accepted v11 inline image / imageHeight in v10 document")
	}

	// Validation checks.
	for name, mutate := range map[string]func(*Document){
		"non-placeholder text": func(d *Document) { d.Blocks[0].Runs[1].Text = "x" },
		"empty image url":      func(d *Document) { d.Blocks[0].Runs[1].Image.URL = "" },
		"negative width":       func(d *Document) { d.Blocks[0].Runs[1].Image.Width = -1 },
		"huge height":          func(d *Document) { d.Blocks[0].Runs[1].Image.Height = 20000 },
		"image in code block": func(d *Document) {
			c := NewBlock("code", "")
			c.Runs = []Run{InlineImageRun(InlineImage{URL: "a.png"}, Style{})}
			d.Blocks = append(d.Blocks, c)
		},
		"height on paragraph": func(d *Document) { d.Blocks[0].ImageHeight = 50 },
	} {
		bad := d.Clone()
		mutate(bad)
		if bad.Validate() == nil {
			t.Errorf("Validate accepted invalid case %q", name)
		}
	}
}

func TestInlineImageEditingRelocationAndUndo(t *testing.T) {
	p := NewBlock("paragraph", "")
	p.Runs = []Run{
		{Text: "Hi "},
		InlineImageRun(InlineImage{URL: "assets/a.png", Alt: "A", Width: 20}, Style{Link: "https://example.com"}),
		{Text: " end"},
	}
	d := &Document{Format: "arcmd", Version: CurrentVersion, Blocks: []*Block{p}}
	e := NewEngine(d)

	// Typing immediately after a linked inline image must not inherit Image or Link.
	e.SetSelection(4, 4)
	e.Replace(4, 4, "!")
	if got := e.Doc.Blocks[0].Runs; len(got) != 3 || !got[1].IsImage() || got[2].IsImage() || got[2].Style.Link != "" || got[2].Text != "! end" {
		t.Fatalf("typing after linked inline image leaked style/image: %+v", got)
	}

	// Updating Alt via SetInlineImageAlt keeps URL/Width/Link intact and is undoable.
	if !e.SetInlineImageAlt(3, "NewAlt") {
		t.Fatal("SetInlineImageAlt returned false")
	}
	if e.Doc.Blocks[0].Runs[1].Image.Alt != "NewAlt" || e.Doc.Blocks[0].Runs[1].Style.Link != "https://example.com" {
		t.Fatalf("SetInlineImageAlt corrupted run: %+v", e.Doc.Blocks[0].Runs[1])
	}

	// Deleting the single placeholder removes the inline image atomically; Undo restores it.
	e.Replace(3, 4, "")
	if len(e.Doc.Blocks[0].Runs) != 1 || e.Doc.Blocks[0].Text() != "Hi ! end" {
		t.Fatalf("deleting inline image failed: %+v", e.Doc.Blocks[0].Runs)
	}
	// ImageURLs still finds assets/a.png in undo history.
	if urls := e.ImageURLs(); len(urls) != 1 || urls[0] != "assets/a.png" {
		t.Fatalf("ImageURLs missed inline image in undo history: %v", urls)
	}
	// RelocateImages rewrites inline images in both live doc and undo stack.
	e.RelocateImages(map[string]string{"assets/a.png": "../assets/a.png"})
	e.Undo()
	if got := e.Doc.Blocks[0].Runs[1]; !got.IsImage() || got.Image.URL != "../assets/a.png" || got.Image.Alt != "NewAlt" {
		t.Fatalf("undo after RelocateImages did not preserve relocated inline image: %+v", got)
	}

	// Pressing Enter after the inline image preserves it on the first paragraph.
	e.SetSelection(4, 4)
	e.Enter(false)
	if len(e.Doc.Blocks) != 2 || !e.Doc.Blocks[0].Runs[1].IsImage() || e.Doc.Blocks[1].Text() != "! end" {
		t.Fatalf("Enter split dropped inline image: %+v", e.Doc.Blocks)
	}
	if err := e.Doc.Validate(); err != nil {
		t.Fatalf("doc invalid after Enter: %v", err)
	}

	// Switching kind to code strips inline images into alt text cleanly so Validate succeeds.
	e.SetSelection(0, 0)
	e.SetKind("code", 0)
	if err := e.Doc.Validate(); err != nil {
		t.Fatalf("doc invalid after SetKind(code): %v", err)
	}
	if e.Doc.Blocks[0].Text() != "Hi NewAlt" {
		t.Fatalf("SetKind(code) alt fallback unexpected: %q", e.Doc.Blocks[0].Text())
	}
}
