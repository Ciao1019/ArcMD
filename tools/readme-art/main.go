// Command readme-art builds the annotation promo SVGs from the existing README
// window illustration and the editor's own annotation paths. Run from the root.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/Ciao1019/ArcMD/internal/document"
)

const duration = 26.0

type stroke struct {
	d            string
	width, alpha float64
}
type look struct {
	name, color                                    string
	view                                           [2]float64
	pad, left, right, leftEm, rightEm, top, bottom float64
	under, dots, bar                               bool
	strokes                                        []stroke
}
type line struct {
	prefix, phrase, suffix, mark string
	width                        float64
}
type locale struct {
	code, title, intro, closing, panel, colors, remove, name string
	captions                                                 []string
	names                                                    []string
	lines                                                    []line
}

var colors = map[string]string{"gray": "#8F959E", "red": "#D83931", "orange": "#DE7802", "yellow": "#DC9B04", "green": "#2EA121", "blue": "#245BDB", "purple": "#6425D0"}
var locales = []locale{
	{code: "zh-CN", name: "灵感手记", title: "让重点，有一点温度", intro: "一页笔记，也可以有自己的语气。", closing: "不必等完美，先让想法发生。", panel: "手绘标注", colors: "颜色", remove: "移除标注", captions: []string{"选中文字", "挑一种喜欢的笔触", "让重点，自然被看见"}, names: []string{"波浪线", "圈出", "荧光笔", "手绘下划线", "直线", "点状下划线", "双下划线", "手绘删除线", "打叉", "箭头", "括号", "方框"}, lines: []line{
		{"留住 ", "一闪而过的灵感", "。", "wavy", 140},
		{"圈出 ", "真正重要的事", "。", "circle", 120},
		{"把 ", "喜欢的句子", "，留给未来的自己。", "highlight", 100},
		{"给 ", "今天的小目标", "，一个认真的承诺。", "double", 120},
		{"划掉 ", "等准备好了再开始", "。", "cross", 160},
		{"让 ", "下一步行动", "，变得清晰。", "arrow", 100},
		{"现在，", "写下第一句", "。", "box", 100},
	}},
	{code: "en", name: "Idea journal", title: "Give your ideas a human touch", intro: "A page of notes can have a voice of its own.", closing: "Start with a sentence. Let the rest follow.", panel: "Hand-drawn marks", colors: "Colors", remove: "Remove mark", captions: []string{"Select a few words", "Choose your stroke", "Let the important things stand out"}, names: []string{"Wavy", "Circle", "Highlight", "Underline", "Line", "Dotted", "Double", "Strike", "Cross", "Arrow", "Brackets", "Box"}, lines: []line{
		{"Keep ", "a fleeting idea", " before it slips away.", "wavy", 124},
		{"Circle ", "what matters", " most.", "circle", 120},
		{"Save ", "a favorite line", " for your future self.", "highlight", 124},
		{"Give ", "one small goal", " your full attention.", "double", 132},
		{"Cross out ", "waiting for perfect", ".", "cross", 164},
		{"Make ", "the next step", " a little clearer.", "arrow", 116},
		{"Now, ", "write the first line", ".", "box", 160},
	}},
}

func main() {
	looks, err := readLooks("internal/editor/annotate.go")
	check(err)
	for _, loc := range locales {
		check(build(loc, looks))
	}
}
func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func number(e ast.Expr) float64 {
	if u, ok := e.(*ast.UnaryExpr); ok {
		return -number(u.X)
	}
	v, err := strconv.ParseFloat(e.(*ast.BasicLit).Value, 64)
	check(err)
	return v
}
func literal(e ast.Expr) string {
	s, err := strconv.Unquote(e.(*ast.BasicLit).Value)
	check(err)
	return s
}
func fields(e ast.Expr, fn func(string, ast.Expr)) {
	for _, e := range e.(*ast.CompositeLit).Elts {
		kv := e.(*ast.KeyValueExpr)
		fn(kv.Key.(*ast.Ident).Name, kv.Value)
	}
}
func readLooks(path string) ([]look, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return nil, err
	}
	var out []look
	ast.Inspect(f, func(n ast.Node) bool {
		v, ok := n.(*ast.ValueSpec)
		if !ok || len(v.Names) != 1 || v.Names[0].Name != "annotationLooks" {
			return true
		}
		for _, e := range v.Values[0].(*ast.CompositeLit).Elts {
			var l look
			fields(e, func(k string, e ast.Expr) {
				switch k {
				case "name":
					l.name = literal(e)
				case "color":
					l.color = literal(e)
				case "view":
					for i, v := range e.(*ast.CompositeLit).Elts {
						l.view[i] = number(v)
					}
				case "under":
					l.under = true
				case "dots":
					l.dots = true
				case "bar":
					l.bar = true
				case "strokes":
					for _, s := range e.(*ast.CompositeLit).Elts {
						var p stroke
						fields(s, func(k string, e ast.Expr) {
							switch k {
							case "d":
								p.d = literal(e)
							case "width":
								p.width = number(e)
							case "alpha":
								p.alpha = number(e)
							}
						})
						l.strokes = append(l.strokes, p)
					}
				default:
					m := map[string]*float64{"pad": &l.pad, "left": &l.left, "right": &l.right, "leftEm": &l.leftEm, "rightEm": &l.rightEm, "top": &l.top, "bottom": &l.bottom}
					if p := m[k]; p != nil {
						*p = number(e)
					}
				}
			})
			out = append(out, l)
		}
		return false
	})
	if len(out) != 12 {
		return nil, fmt.Errorf("expected 12 annotation looks, got %d", len(out))
	}
	return out, nil
}
func esc(s string) string  { return html.EscapeString(s) }
func pct(t float64) string { return fmt.Sprintf("%.4f%%", t/duration*100) }
func width(s string, size float64) float64 {
	w := 0.0
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r) || r > 127:
			w += size
		case r == ' ':
			w += size * .27
		case strings.ContainsRune("ilI.,'", r):
			w += size * .25
		case strings.ContainsRune("mwMW", r):
			w += size * .78
		default:
			w += size * .51
		}
	}
	return w
}
func text(b *bytes.Buffer, x, y, size float64, ink, weight, s string, w float64) {
	fit := ""
	if w > 0 {
		fit = fmt.Sprintf(` textLength="%.2f" lengthAdjust="spacingAndGlyphs"`, w)
	}
	fmt.Fprintf(b, "<text class=\"t\" xml:space=\"preserve\" x=\"%.2f\" y=\"%.2f\" font-size=\"%.2f\" fill=\"%s\" font-weight=\"%s\"%s>%s</text>\n", x, y, size, ink, weight, fit, esc(s))
}

func lineLayout(i int, ln line, looks []look) (x, y float64, l look) {
	for _, v := range looks {
		if v.name == ln.mark {
			l = v
			break
		}
	}
	// Match the editor's extra room on either side of circled/boxed text.
	x, y = 302+width(ln.prefix, 20)+l.pad*20, 230+float64(i)*62
	if i == 6 {
		y += 16
	}
	return
}
func rect(b *bytes.Buffer, x, y, w, h, r float64, attrs string) {
	fmt.Fprintf(b, "<rect x=\"%.2f\" y=\"%.2f\" width=\"%.2f\" height=\"%.2f\" rx=\"%.2f\" %s/>\n", x, y, w, h, r, attrs)
}
func mark(b *bytes.Buffer, l look, x, y, w, size float64, id string, animated bool) {
	// Reuse annotationBox's geometry, including the side room for circles/boxes.
	mid := y - size*.32
	ww := w + 2*l.pad*size
	xx := x - l.pad*size
	left, right := l.left*ww+l.leftEm*size, l.right*ww+l.rightEm*size
	xx -= left
	ww += left + right
	yy, hh := mid+l.top*size, (l.bottom-l.top)*size
	color := colors[l.color]
	class := ""
	if animated {
		class = ` class="anim draw ` + id + `"`
	}
	fmt.Fprintf(b, "<g data-mark=\"%s\"%s>\n", l.name, func() string {
		if animated {
			return ` class="anim marks"`
		}
		return ""
	}())
	if l.bar {
		rect(b, xx, yy+hh/2-1, ww, 2, 1, `fill="`+color+`"`)
		fmt.Fprintln(b, "</g>")
		return
	}
	if l.dots {
		for at := 0.0; at < ww; at += 5 {
			fmt.Fprintf(b, `<circle cx="%.2f" cy="%.2f" r="1.1" fill="%s"/>`, xx+at, yy+hh*.5, color)
		}
		fmt.Fprintln(b, "</g>")
		return
	}
	if l.under && animated {
		fmt.Fprintf(b, `<defs><clipPath id="clip-%s"><rect width="170" height="26" class="anim %s"/></clipPath></defs>`, id, id)
	}
	fmt.Fprintf(b, `<svg x="%.2f" y="%.2f" width="%.2f" height="%.2f" viewBox="0 0 %.2f %.2f" preserveAspectRatio="none" overflow="visible">`, xx, yy, ww, hh, l.view[0], l.view[1])
	if l.under {
		if animated {
			fmt.Fprintf(b, `<g clip-path="url(#clip-%s)">`, id)
		}
		fmt.Fprint(b, `<g opacity=".45">`)
	}
	for _, s := range l.strokes {
		attrs := fmt.Sprintf(`fill="none" stroke="%s" stroke-width="%.2f" stroke-opacity="%.2f" stroke-linecap="round" stroke-linejoin="round"`, color, s.width, s.alpha)
		c := class
		if s.width == 0 {
			attrs = fmt.Sprintf(`fill="%s" fill-opacity="%.2f"`, color, s.alpha)
			c = ""
		}
		if animated && s.width > 0 {
			attrs += ` pathLength="1" stroke-dasharray="1"`
		}
		fmt.Fprintf(b, "<path d=\"%s\" %s%s/>\n", s.d, attrs, c)
	}
	if l.under {
		fmt.Fprint(b, "</g>")
		if animated {
			fmt.Fprint(b, "</g>")
		}
	}
	fmt.Fprintln(b, "</svg></g>")
}
func build(loc locale, looks []look) error {
	ref, err := os.ReadFile("docs/images/readme/bookmarks-" + loc.code + ".svg")
	if err != nil {
		return err
	}
	source := string(ref)
	a := strings.Index(source, "<defs>")
	z := strings.Index(source, `<text x="256" y="78"`)
	if a < 0 || z < a {
		return fmt.Errorf("README reference structure changed")
	}
	shell := source[a:z]
	if loc.code == "zh-CN" {
		shell = strings.NewReplacer("读书笔记", loc.name, ">读</text>", ">灵</text>", "142 字", "103 字").Replace(shell)
	} else {
		shell = strings.NewReplacer("Reading notes", loc.name, ">R</text>", ">I</text>", "142 words", "69 words").Replace(shell)
	}
	var b, css bytes.Buffer
	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 1280 820\" width=\"1280\" height=\"820\" role=\"img\" aria-labelledby=\"promo-title promo-desc\">\n<title id=\"promo-title\">ArcMD · %s</title>\n<desc id=\"promo-desc\">%s</desc>\n", esc(loc.title), esc(loc.intro+" "+strings.Join(loc.captions, " · ")))
	license, err := os.ReadFile("third_party/opensourceui/LICENSE")
	if err != nil {
		return err
	}
	fmt.Fprintf(&b, "<!-- Generated by go run ./tools/readme-art. Annotation paths from internal/editor/annotate.go.\n%s\n-->\n", license)
	fmt.Fprintln(&css, `.t{font-family:-apple-system,BlinkMacSystemFont,"SF Pro Text","Segoe UI","PingFang SC","Hiragino Sans GB","Microsoft YaHei","Noto Sans CJK SC",Helvetica,Arial,sans-serif}.anim{animation-duration:26s;animation-iteration-count:infinite;animation-fill-mode:both;animation-timing-function:cubic-bezier(.65,0,.35,1)}.draw{stroke-dashoffset:1}.selection,.toolbar,.picker,.cursor,.click{opacity:0}`)
	fmt.Fprintln(&css, `@keyframes marks{0%,90%{opacity:1}96%,100%{opacity:0}}.marks{animation-name:marks}.cursor{animation-name:pointer,pointerVisible}.click{transform-box:fill-box;transform-origin:center}`)
	fmt.Fprintf(&css, "@keyframes pointerVisible{0%%{opacity:0}%s,85%%{opacity:1}89%%,100%%{opacity:0}}\n", pct(.7))
	var pointer, picker bytes.Buffer
	fmt.Fprint(&pointer, "@keyframes pointer{0%{transform:translate(670px,182px)}")
	fmt.Fprint(&picker, "@keyframes picker{0%{opacity:0;transform:translateY(6px)}")
	for i, ln := range loc.lines {
		x, y, _ := lineLayout(i, ln, looks)
		start := 1 + float64(i)*3
		pick := start + 1.5
		fmt.Fprintf(&picker, "%s{opacity:0;transform:translateY(6px)}%s,%s{opacity:1;transform:translateY(0)}%s{opacity:0;transform:translateY(6px)}", pct(start+.92), pct(start+1.12), pct(start+2.1), pct(start+2.4))
		fmt.Fprintf(&css, "@keyframes draw%d{0%%,%s{stroke-dashoffset:1}%s,100%%{stroke-dashoffset:0}}.draw%d{animation-name:draw%d;animation-timing-function:cubic-bezier(.16,1,.3,1)}\n", i, pct(pick), pct(pick+.55), i, i)
		if ln.mark == "highlight" {
			fmt.Fprintf(&css, "@keyframes reveal%d{0%%,%s{transform:scaleX(0)}%s,100%%{transform:scaleX(1)}}.draw%d{animation-name:reveal%d;transform-origin:0 0}\n", i, pct(pick), pct(pick+.55), i, i)
		}
		fmt.Fprintf(&css, "@keyframes selection%d{0%%,%s{opacity:0;transform:scaleX(0)}%s{opacity:1;transform:scaleX(0)}%s,%s{opacity:1;transform:scaleX(1)}%s,100%%{opacity:0;transform:scaleX(1)}}.selection%d{animation-name:selection%d;transform-origin:%.2fpx %.2fpx}\n", i, pct(start), pct(start+.08), pct(start+.48), pct(pick+.35), pct(pick+.7), i, i, x, y-21)
		fmt.Fprintf(&css, "@keyframes toolbar%d{0%%,%s{opacity:0;transform:translateY(4px)}%s,%s{opacity:1;transform:translateY(0)}%s,100%%{opacity:0;transform:translateY(4px)}}.toolbar%d{animation-name:toolbar%d}\n", i, pct(start+.4), pct(start+.65), pct(pick+.3), pct(pick+.65), i, i)
		li := 0
		for j, l := range looks {
			if l.name == ln.mark {
				li = j
			}
		}
		tx, ty := 900+10+float64(li%3)*94+44, 248+40+float64(li/3)*52+23
		fmt.Fprintf(&css, "@keyframes active%d{0%%,%s{opacity:0}%s,%s{opacity:1}%s,100%%{opacity:0}}.active%d{animation-name:active%d}\n", i, pct(pick-.15), pct(pick+.05), pct(start+2.6), pct(start+2.9), i, i)
		fmt.Fprintf(&css, "@keyframes click%d{0%%,%s{opacity:0;transform:scale(.6)}%s{opacity:.7;transform:scale(.6)}%s{opacity:0;transform:scale(1.8)}100%%{opacity:0;transform:scale(1.8)}}.click%d{animation-name:click%d}\n", i, pct(pick-.04), pct(pick+.01), pct(pick+.45), i, i)
		for _, pt := range []struct{ t, x, y float64 }{{start, x, y - 9}, {start + .5, x + ln.width, y - 9}, {start + .92, x + 178, y - 51}, {start + 1.12, x + 178, y - 51}, {pick - .15, tx, ty}, {pick + .25, tx, ty}, {start + 2.3, x + ln.width + 42, y + 16}} {
			fmt.Fprintf(&pointer, "%s{transform:translate(%.2fpx,%.2fpx)}", pct(pt.t), pt.x, pt.y)
		}
	}
	fmt.Fprintln(&pointer, "92%,100%{transform:translate(670px,182px)}}")
	css.Write(pointer.Bytes())
	fmt.Fprintln(&picker, "100%{opacity:0;transform:translateY(6px)}}.picker{animation-name:picker}")
	css.Write(picker.Bytes())
	for i := 0; i < 3; i++ {
		fmt.Fprintf(&css, ".cap%d{opacity:.4}\n", i)
	}
	fmt.Fprintln(&css, `@keyframes cap0{0%,10%{opacity:1}15%,92%{opacity:.4}97%,100%{opacity:1}}@keyframes cap1{0%,8%{opacity:.4}12%,76%{opacity:1}81%,100%{opacity:.4}}@keyframes cap2{0%,75%{opacity:.4}80%,92%{opacity:1}98%,100%{opacity:.4}}.cap0{animation-name:cap0}.cap1{animation-name:cap1}.cap2{animation-name:cap2}`)
	fmt.Fprintln(&css, `@media(prefers-reduced-motion:reduce){.anim{animation:none!important}.cursor,.click,.selection,.toolbar{display:none}.picker,.marks,.cap0,.cap1,.cap2{opacity:1;transform:none}.draw{stroke-dashoffset:0}.active{opacity:0}}`)
	fmt.Fprintf(&b, "<style>\n%s</style>\n%s", css.String(), shell)
	fmt.Fprintln(&b, `<g clip-path="url(#card)">`)
	text(&b, 302, 124, 34, "#1f2329", "500", loc.title, 0)
	text(&b, 302, 170, 17, "#646a73", "400", loc.intro, 0)
	for i, ln := range loc.lines {
		x, y, l := lineLayout(i, ln, looks)
		rect(&b, x-2, y-22, ln.width+4, 30, 3, fmt.Sprintf(`fill="#BACEFD" fill-opacity=".65" class="anim selection selection%d"`, i))
		if l.under {
			mark(&b, l, x, y, ln.width, 20, fmt.Sprintf("draw%d", i), true)
		}
		text(&b, 302, y, 20, "#30343b", "400", ln.prefix, width(ln.prefix, 20))
		text(&b, x, y, 20, "#1f2329", "500", ln.phrase, ln.width)
		text(&b, x+ln.width+l.pad*20, y, 20, "#30343b", "400", ln.suffix, 0)
		if !l.under {
			mark(&b, l, x, y, ln.width, 20, fmt.Sprintf("draw%d", i), true)
		}
	}
	text(&b, 302, 697, 16, "#8f959e", "400", loc.closing, 0)
	fmt.Fprintln(&b, "</g>")
	// Each selection shows the same compact native toolbar, above its words.
	for i, ln := range loc.lines {
		x, y, _ := lineLayout(i, ln, looks)
		y -= 70
		fmt.Fprintf(&b, `<g class="anim toolbar toolbar%d">`, i)
		rect(&b, x-4, y+4, 204, 40, 10, `fill="#4b3654" opacity=".06"`)
		rect(&b, x, y, 196, 34, 7, `fill="#fff" stroke="#e2dde5" stroke-width="1"`)
		for j, label := range []string{"B", "I", "U", "S", "↗"} {
			text(&b, x+12+float64(j)*28, y+23, 15, "#535662", "500", label, 0)
		}
		fmt.Fprintf(&b, `<path d="M%.1f %.1fv20" stroke="#e2dde5"/>`, x+150, y+7)
		text(&b, x+160, y+23, 16, "#6425D0", "500", "A", 0)
		mark(&b, looks[0], x+160, y+23, 10, 12, "", false)
		fmt.Fprintf(&b, `<path d="M%.1f %.1fl4 4 4-4" fill="none" stroke="#646a73" stroke-width="1.2" stroke-linecap="round"/>`, x+178, y+14)
		fmt.Fprintln(&b, "</g>")
	}
	fmt.Fprintln(&b, `<g class="anim picker">`)
	for j := 3; j > 0; j-- {
		rect(&b, 900-float64(j)*2, 248+float64(j)*2, 300+float64(j)*4, 336+float64(j)*2, 10+float64(j)*2, `fill="#3d2a48" opacity=".025"`)
	}
	rect(&b, 900, 248, 300, 336, 10, `fill="#fff" stroke="#e0dbe4" stroke-width="1"`)
	text(&b, 911, 273, 12, "#30343b", "500", loc.panel, 0)
	for i, l := range looks {
		x, y := 910+float64(i%3)*94, 288+float64(i/3)*52
		for j, ln := range loc.lines {
			if ln.mark == l.name {
				rect(&b, x, y, 88, 46, 6, fmt.Sprintf(`fill="#f1eef8" stroke="#c5b4e8" class="anim active active%d"`, j))
			}
		}
		label := loc.names[i]
		size := 12.0
		w := width(label, size)
		if w > 78 {
			w = 78
		}
		tx := x + (88-w)/2
		ty := y + 28
		if l.under {
			mark(&b, l, tx, ty, w, size, "", false)
		}
		text(&b, tx, ty, size, "#454851", "400", label, w)
		if !l.under {
			mark(&b, l, tx, ty, w, size, "", false)
		}
	}
	text(&b, 911, 519, 12, "#30343b", "400", loc.colors, 0)
	for i, c := range []string{"", "gray", "red", "orange", "yellow", "green", "blue", "purple"} {
		x := 924 + float64(i)*36
		ink := colors[c]
		if c == "" {
			ink = "#fff"
		}
		fmt.Fprintf(&b, `<circle cx="%.1f" cy="541" r="9" fill="%s" stroke="#ddd8e4" stroke-width=".7"/>`, x, ink)
		if c == "" {
			fmt.Fprintf(&b, `<circle cx="%.1f" cy="541" r="12" fill="none" stroke="#9585af" stroke-width="1.3"/><path d="M919 541l3 3 6-6" fill="none" stroke="#80758b" stroke-width="1.5"/>`, x)
		}
	}
	rect(&b, 911, 559, 278, 18, 5, `fill="none" stroke="#ebe7ee"`)
	text(&b, 900+(300-width(loc.remove, 11))/2, 572, 11, "#73717b", "400", loc.remove, 0)
	fmt.Fprintln(&b, "</g>")
	for i, ln := range loc.lines {
		li := 0
		for j, l := range looks {
			if l.name == ln.mark {
				li = j
			}
		}
		fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="11" fill="none" stroke="%s" stroke-width="1.6" class="anim click click%d"/>`, 954+float64(li%3)*94, 311+float64(li/3)*52, colors[looks[li].color], i)
	}
	fmt.Fprintln(&b, `<g class="anim cursor"><use href="#cursor"/></g></g></g>`)
	for i, label := range loc.captions {
		x := []float64{150, 492, 848}[i]
		fmt.Fprintf(&b, `<g class="anim cap%d">`, i)
		rect(&b, x, 760, 22, 22, 6, `fill="#b96e9c" fill-opacity=".14"`)
		text(&b, x+7, 776, 12, "#6b4161", "600", strconv.Itoa(i+1), 0)
		text(&b, x+31, 776, 14, "#513c4c", "500", label, 0)
		fmt.Fprintln(&b, "</g>")
	}
	fmt.Fprintln(&b, "</g></svg>")
	out := filepath.Join("docs/images/readme", "annotations-"+loc.code+".svg")
	if err := os.WriteFile(out, b.Bytes(), 0644); err != nil {
		return err
	}
	// Keep the editorial sample openable in ArcMD, independently of the artwork.
	d := document.New()
	title := document.NewBlock("heading", loc.title)
	title.Level = 1
	d.Blocks = []*document.Block{title, document.NewBlock("paragraph", loc.intro)}
	for _, ln := range loc.lines {
		p := document.NewBlock("paragraph", "")
		p.Runs = []document.Run{{Text: ln.prefix}, {Text: ln.phrase, Style: document.Style{Annotation: ln.mark}}, {Text: ln.suffix}}
		d.Blocks = append(d.Blocks, p)
	}
	d.Blocks = append(d.Blocks, document.NewBlock("paragraph", loc.closing))
	// Stable fixture IDs keep repeated generation deterministic.
	for i, p := range d.Blocks {
		p.ID = fmt.Sprintf("promo-%s-%02d", loc.code, i)
	}
	encoded, err := d.Encode()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join("docs/images/readme", "annotations-demo-"+loc.code+".arcmd"), []byte(encoded), 0644); err != nil {
		return err
	}
	fmt.Println(out, b.Len(), "bytes")
	return nil
}
