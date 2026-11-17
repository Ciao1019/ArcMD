package jsx

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Theme is the CSS custom-property set injected into each JSX WebView.
// Paper and body text follow the editor theme (and therefore the page), while
// controls still receive Surface, Border, Accent and Danger.
type Theme struct {
	Dark bool              `json:"dark"`
	Vars map[string]string `json:"vars"`
}

func cssColor(c ui.Color) string {
	if c.A == 0 {
		return "transparent"
	}
	if c.A == 255 {
		return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	}
	return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", c.R, c.G, c.B, float32(c.A)/255)
}

func NewTheme(t *ui.Theme) Theme {
	if t == nil {
		t = ui.LightTheme()
	}
	font := strings.TrimSpace(t.Font)
	if font == "" {
		font = "system-ui, sans-serif"
	}
	size := t.FontSize
	if size <= 0 {
		size = 13
	}
	// Paper and body text follow the editor theme (Background/Text).
	return Theme{
		Dark: t.Dark,
		Vars: map[string]string{
			"--bg":          cssColor(t.Background),
			"--text":        cssColor(t.Text),
			"--text-muted":  cssColor(t.TextMuted),
			"--surface":     cssColor(t.Surface),
			"--border":      cssColor(t.Border),
			"--accent":      cssColor(t.Accent),
			"--accent-text": cssColor(t.AccentText),
			"--danger":      cssColor(t.Danger),
			"--font":        font,
			"--font-size":   fmt.Sprintf("%.0fpx", size),
		},
	}
}

func (t Theme) Key() string {
	return t.Script()
}

func (t Theme) Background() string {
	if bg := t.Vars["--bg"]; bg != "" {
		return bg
	}
	return "transparent"
}

// Script is evaluated with Page.Eval, which is not subject to the page CSP.
// It only writes CSS custom properties, so React state in the same document
// is not remounted.
func (t Theme) Script() string {
	payload, err := json.Marshal(t)
	if err != nil {
		return "false"
	}
	return `(function(t){var r=document.documentElement;if(!r)return false;r.style.setProperty("color-scheme",t.dark?"dark":"light");if(t.vars){for(var k in t.vars){r.style.setProperty(k,t.vars[k]);}}return true})(` + string(payload) + `)`
}
