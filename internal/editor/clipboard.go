package editor

import (
	"github.com/Ciao1019/ArcMD/internal/document"
	"github.com/egoist/mygo/transfer"
)

const fragmentFormat transfer.Format = "application/vnd.arcmd.document+json"

func (e *Editor) copySelection(write func(string)) {
	r := e.Selection().Range
	if r.Start == r.End {
		return
	}
	text := e.slice(r.Start, r.End)
	fragment := e.Engine.Fragment()
	fragment.ImportSource = ""
	data, err := fragment.Encode()
	if err != nil {
		write(text)
		return
	}
	e.services.WriteClipboardData(transfer.New(transfer.NewItem(
		transfer.Bytes(fragmentFormat, []byte(data)),
		transfer.Bytes(transfer.Text, []byte(text)),
		transfer.Bytes(transfer.Markdown, []byte(ExportMarkdown(fragment))),
	)))
}

func (e *Editor) pasteSelection(read func() string) {
	clipboard := e.services.ReadClipboardData(fragmentFormat, transfer.FileList, transfer.PNG, transfer.HTML, transfer.Text)
	if data, err := clipboard.Read(fragmentFormat); err == nil && len(data) < 16<<20 {
		if doc, err := document.Decode(string(data)); err == nil {
			e.UnmarkText()
			e.Engine.Paste(doc)
			e.sync()
			return
		}
	}
	// Files copied in Finder go in as dropped at the caret; a picture
	// copied from an app or a screenshot is PNG. Both come with text (the
	// files' names), which is pasted only when the host takes neither.
	if e.dropFiles != nil {
		if files, err := clipboard.Files(); err == nil && len(files) > 0 {
			e.UnmarkText()
			e.dropFiles(files, -1)
			return
		}
	}
	if e.pasteImage != nil {
		if png, err := clipboard.Read(transfer.PNG); err == nil && len(png) > 0 {
			e.UnmarkText()
			e.pasteImage(png)
			return
		}
	}
	// Rich text from a browser or another editor keeps its structure;
	// HTML that holds only plain text pastes as the text.
	if data, err := clipboard.Read(transfer.HTML); err == nil && len(data) > 0 && len(data) < 8<<20 {
		if doc := ImportHTML(string(data)); rich(doc) && e.Engine.LeafAt(e.Caret).Block.Kind != "code" {
			e.UnmarkText()
			e.Engine.Paste(doc)
			e.sync()
			return
		}
	}
	e.ReplaceText(nil, read())
}

// rich reports whether a document holds more than plain paragraphs of
// plain text, worth pasting as structure.
func rich(doc *document.Document) bool {
	for _, b := range doc.Blocks {
		if b.Kind != "paragraph" {
			return true
		}
		for _, r := range b.Runs {
			if r.Style != (document.Style{}) {
				return true
			}
		}
	}
	return false
}
