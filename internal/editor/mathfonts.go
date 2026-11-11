package editor

import (
	"embed"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Textus uses these KaTeX faces. Embedding them keeps native formulas
// consistent on machines without mathematical fonts installed.
//
//go:embed fonts/KaTeX_Main-Regular.ttf fonts/KaTeX_Math-Italic.ttf
var mathFonts embed.FS

var loadMathFonts = sync.OnceValue(func() error {
	for _, name := range []string{"KaTeX_Main-Regular", "KaTeX_Math-Italic"} {
		data, err := mathFonts.ReadFile("fonts/" + name + ".ttf")
		if err != nil {
			return err
		}
		if err := ui.RegisterFont(data, ""); err != nil {
			return err
		}
	}
	return nil
})
