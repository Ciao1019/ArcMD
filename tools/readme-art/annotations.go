package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Ciao1019/ArcMD/internal/document"
)

const annotDuration = 26.0

type annotStroke struct {
	d            string
	width, alpha float64
}

type annotLook struct {
	name, color                                    string
	view                                           [2]float64
	pad, left, right, leftEm, rightEm, top, bottom float64
	under, dots, bar                               bool
	strokes                                        []annotStroke
}

type annotLine struct {
	prefix, phrase, suffix, mark string
	width                        float64
}

type annotLocale struct {
	code, title, intro, closing, panel, colors, remove, name, glyph, wc string
	zh                                                                  bool
	captions                                                            []string
	capX                                                                []float64
	names                                                               []string
	lines                                                               []annotLine
}

var annotColors = map[string]string{
	"gray": "#8F959E", "red": "#D83931", "orange": "#DE7802", "yellow": "#DC9B04",
	"green": "#2EA121", "blue": "#245BDB", "purple": "#6425D0",
}

var annotLocales = []annotLocale{
	{
		code: "zh-CN", zh: true, name: "灵感手记", glyph: "灵", wc: "103 字",
		title: "让重点，有一点温度", intro: "一页本地 .md 笔记，也可以拥有手写的语气。", closing: "不必等完美，先让想法发生。",
		panel: "手绘标注", colors: "颜色", remove: "移除标注",
		captions: []string{"选中一段文字", "挑一种喜欢的笔触", "让重点，自然被看见"},
		capX:     []float64{156, 496, 846},
		names:    []string{"波浪线", "圈出", "荧光笔", "手绘下划线", "直线", "点状下划线", "双下划线", "手绘删除线", "打叉", "箭头", "括号", "方框"},
		lines: []annotLine{
			{"留住 ", "一闪而过的灵感", "。", "wavy", 140},
			{"圈出 ", "真正重要的事", "。", "circle", 120},
			{"把 ", "喜欢的句子", "，留给未来的自己。", "highlight", 100},
			{"给 ", "今天的小目标", "，一个认真的承诺。", "double", 120},
			{"划掉 ", "等准备好了再开始", "。", "cross", 160},
			{"让 ", "下一步行动", "，变得清晰。", "arrow", 100},
			{"现在，", "写下第一句", "。", "box", 100},
		},
	},
	{
		code: "en", zh: false, name: "Idea journal", glyph: "I", wc: "69 words",
		title: "Give your ideas a human touch", intro: "A plain .md page can still carry a voice of its own.", closing: "Start with a sentence. Let the rest follow.",
		panel: "Hand-drawn marks", colors: "Colors", remove: "Remove mark",
		captions: []string{"Select a few words", "Choose your stroke", "Let what matters stand out"},
		capX:     []float64{150, 494, 836},
		names:    []string{"Wavy", "Circle", "Highlight", "Underline", "Line", "Dotted", "Double", "Strike", "Cross", "Arrow", "Brackets", "Box"},
		lines: []annotLine{
			{"Keep ", "a fleeting idea", " before it slips away.", "wavy", 124},
			{"Circle ", "what matters", " most.", "circle", 120},
			{"Save ", "a favorite line", " for your future self.", "highlight", 124},
			{"Give ", "one small goal", " your full attention.", "double", 132},
			{"Cross out ", "waiting for perfect", ".", "cross", 164},
			{"Make ", "the next step", " a little clearer.", "arrow", 116},
			{"Now, ", "write the first line", ".", "box", 160},
		},
	},
}

func buildAnnotations() error {
	looks, err := readAnnotLooks("internal/editor/annotate.go")
	if err != nil {
		return err
	}
	for _, loc := range annotLocales {
		if err := buildAnnotLocale(loc, looks); err != nil {
			return err
		}
	}
	return nil
}

func astNumber(e ast.Expr) float64 {
	if u, ok := e.(*ast.UnaryExpr); ok {
		return -astNumber(u.X)
	}
	v, err := strconv.ParseFloat(e.(*ast.BasicLit).Value, 64)
	if err != nil {
		panic(err)
	}
	return v
}

func astLiteral(e ast.Expr) string {
	s, err := strconv.Unquote(e.(*ast.BasicLit).Value)
	if err != nil {
		panic(err)
	}
	return s
}

func astFields(e ast.Expr, fn func(string, ast.Expr)) {
	for _, el := range e.(*ast.CompositeLit).Elts {
		kv := el.(*ast.KeyValueExpr)
		fn(kv.Key.(*ast.Ident).Name, kv.Value)
	}
}

func readAnnotLooks(path string) ([]annotLook, error) {
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		return nil, err
	}
	var out []annotLook
	ast.Inspect(f, func(n ast.Node) bool {
		v, ok := n.(*ast.ValueSpec)
		if !ok || len(v.Names) != 1 || v.Names[0].Name != "annotationLooks" {
			return true
		}
		for _, el := range v.Values[0].(*ast.CompositeLit).Elts {
			var l annotLook
			astFields(el, func(k string, e ast.Expr) {
				switch k {
				case "name":
					l.name = astLiteral(e)
				case "color":
					l.color = astLiteral(e)
				case "view":
					for i, vv := range e.(*ast.CompositeLit).Elts {
						l.view[i] = astNumber(vv)
					}
				case "under":
					l.under = true
				case "dots":
					l.dots = true
				case "bar":
					l.bar = true
				case "strokes":
					for _, s := range e.(*ast.CompositeLit).Elts {
						var p annotStroke
						astFields(s, func(k string, e ast.Expr) {
							switch k {
							case "d":
								p.d = astLiteral(e)
							case "width":
								p.width = astNumber(e)
							case "alpha":
								p.alpha = astNumber(e)
							}
						})
						l.strokes = append(l.strokes, p)
					}
				default:
					m := map[string]*float64{"pad": &l.pad, "left": &l.left, "right": &l.right, "leftEm": &l.leftEm, "rightEm": &l.rightEm, "top": &l.top, "bottom": &l.bottom}
					if ptr := m[k]; ptr != nil {
						*ptr = astNumber(e)
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

func annotLineLayout(i int, ln annotLine, looks []annotLook) (x, y float64, l annotLook) {
	for _, v := range looks {
		if v.name == ln.mark {
			l = v
			break
		}
	}
	x, y = 302+approxWidth(ln.prefix, 20)+l.pad*20, 232+float64(i)*62
	if i == 6 {
		y += 16
	}
	return
}

func drawAnnotMark(b *bytes.Buffer, l annotLook, x, y, w, size float64, id string, animated bool) {
	mid := y - size*.32
	ww := w + 2*l.pad*size
	xx := x - l.pad*size
	left, right := l.left*ww+l.leftEm*size, l.right*ww+l.rightEm*size
	xx -= left
	ww += left + right
	yy, hh := mid+l.top*size, (l.bottom-l.top)*size
	color := annotColors[l.color]
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
		b.WriteString("</g>\n")
		return
	}
	if l.dots {
		for at := 0.0; at < ww; at += 5 {
			fmt.Fprintf(b, `<circle cx="%.2f" cy="%.2f" r="1.1" fill="%s"/>`, xx+at, yy+hh*.5, color)
		}
		b.WriteString("</g>\n")
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
		b.WriteString(`<g opacity=".45">`)
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
		b.WriteString(`</g>`)
		if animated {
			b.WriteString(`</g>`)
		}
	}
	b.WriteString("</svg></g>\n")
}

func writeFittedText(b *bytes.Buffer, x, y, size float64, ink, weight, s string, fitW float64) {
	extra := ""
	if fitW > 0 {
		extra = fmt.Sprintf(`textLength="%.2f" lengthAdjust="spacingAndGlyphs"`, fitW)
	}
	text(b, x, y, size, ink, weight, s, extra)
}

func buildAnnotLocale(loc annotLocale, looks []annotLook) error {
	p := func(t float64) string { return pct(t, annotDuration) }
	license, err := os.ReadFile("third_party/opensourceui/LICENSE")
	if err != nil {
		return err
	}

	var b, css bytes.Buffer
	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0 0 1280 820\" width=\"1280\" height=\"820\" role=\"img\" aria-labelledby=\"promo-title promo-desc\">\n<title id=\"promo-title\">ArcMD · %s</title>\n<desc id=\"promo-desc\">%s</desc>\n",
		esc(loc.title), esc(loc.intro+" "+loc.captions[0]+" · "+loc.captions[1]+" · "+loc.captions[2]))
	fmt.Fprintf(&b, "<!-- Generated by go run ./tools/readme-art. Annotation paths from internal/editor/annotate.go.\n%s\n-->\n", license)

	css.WriteString(baseFontCSS())
	css.WriteString(`.anim{animation-duration:26s;animation-iteration-count:infinite;animation-fill-mode:both;animation-timing-function:cubic-bezier(.65,0,.35,1)}.draw{stroke-dashoffset:1}.selection,.toolbar,.picker,.cursor,.click{opacity:0}` + "\n")
	css.WriteString(`@keyframes marks{0%,90%{opacity:1}96%,100%{opacity:0}}.marks{animation-name:marks}.cursor{animation-name:pointer,pointerVisible}.click{transform-box:fill-box;transform-origin:center}` + "\n")
	fmt.Fprintf(&css, "@keyframes pointerVisible{0%%{opacity:0}%s,85%%{opacity:1}89%%,100%%{opacity:0}}\n", p(.7))

	var pointer, picker bytes.Buffer
	pointer.WriteString("@keyframes pointer{0%{transform:translate(670px,182px)}")
	picker.WriteString("@keyframes picker{0%{opacity:0;transform:translateY(6px)}")

	for i, ln := range loc.lines {
		x, y, _ := annotLineLayout(i, ln, looks)
		start := 1 + float64(i)*3
		pick := start + 1.5
		fmt.Fprintf(&picker, "%s{opacity:0;transform:translateY(6px)}%s,%s{opacity:1;transform:translateY(0)}%s{opacity:0;transform:translateY(6px)}", p(start+.92), p(start+1.12), p(start+2.1), p(start+2.4))
		fmt.Fprintf(&css, "@keyframes draw%d{0%%,%s{stroke-dashoffset:1}%s,100%%{stroke-dashoffset:0}}.draw%d{animation-name:draw%d;animation-timing-function:cubic-bezier(.16,1,.3,1)}\n", i, p(pick), p(pick+.55), i, i)
		if ln.mark == "highlight" {
			fmt.Fprintf(&css, "@keyframes reveal%d{0%%,%s{transform:scaleX(0)}%s,100%%{transform:scaleX(1)}}.draw%d{animation-name:reveal%d;transform-origin:0 0}\n", i, p(pick), p(pick+.55), i, i)
		}
		fmt.Fprintf(&css, "@keyframes selection%d{0%%,%s{opacity:0;transform:scaleX(0)}%s{opacity:1;transform:scaleX(0)}%s,%s{opacity:1;transform:scaleX(1)}%s,100%%{opacity:0;transform:scaleX(1)}}.selection%d{animation-name:selection%d;transform-origin:%.2fpx %.2fpx}\n",
			i, p(start), p(start+.08), p(start+.48), p(pick+.35), p(pick+.7), i, i, x, y-21)
		fmt.Fprintf(&css, "@keyframes toolbar%d{0%%,%s{opacity:0;transform:translateY(4px)}%s,%s{opacity:1;transform:translateY(0)}%s,100%%{opacity:0;transform:translateY(4px)}}.toolbar%d{animation-name:toolbar%d}\n",
			i, p(start+.4), p(start+.65), p(pick+.3), p(pick+.65), i, i)
		li := 0
		for j, l := range looks {
			if l.name == ln.mark {
				li = j
			}
		}
		tx, ty := 900+10+float64(li%3)*94+44, 248+40+float64(li/3)*52+23
		fmt.Fprintf(&css, "@keyframes active%d{0%%,%s{opacity:0}%s,%s{opacity:1}%s,100%%{opacity:0}}.active%d{animation-name:active%d}\n", i, p(pick-.15), p(pick+.05), p(start+2.6), p(start+2.9), i, i)
		fmt.Fprintf(&css, "@keyframes click%d{0%%,%s{opacity:0;transform:scale(.6)}%s{opacity:.7;transform:scale(.6)}%s{opacity:0;transform:scale(1.8)}100%%{opacity:0;transform:scale(1.8)}}.click%d{animation-name:click%d}\n", i, p(pick-.04), p(pick+.01), p(pick+.45), i, i)
		for _, pt := range []struct{ t, x, y float64 }{{start, x, y - 9}, {start + .5, x + ln.width, y - 9}, {start + .92, x + 178, y - 51}, {start + 1.12, x + 178, y - 51}, {pick - .15, tx, ty}, {pick + .25, tx, ty}, {start + 2.3, x + ln.width + 42, y + 16}} {
			fmt.Fprintf(&pointer, "%s{transform:translate(%.2fpx,%.2fpx)}", p(pt.t), pt.x, pt.y)
		}
	}
	pointer.WriteString("92%,100%{transform:translate(670px,182px)}}\n")
	css.Write(pointer.Bytes())
	picker.WriteString("100%{opacity:0;transform:translateY(6px)}}.picker{animation-name:picker}\n")
	css.Write(picker.Bytes())
	css.WriteString(`@keyframes cap0{0%,10%{opacity:1}15%,92%{opacity:.38}97%,100%{opacity:1}}@keyframes cap1{0%,8%{opacity:.38}12%,76%{opacity:1}81%,100%{opacity:.38}}@keyframes cap2{0%,75%{opacity:.38}80%,92%{opacity:1}98%,100%{opacity:.38}}.cap0{animation-name:cap0}.cap1{animation-name:cap1}.cap2{animation-name:cap2}` + "\n")
	css.WriteString(`@media(prefers-reduced-motion:reduce){.anim{animation:none!important}.cursor,.click,.selection,.toolbar{display:none}.picker,.marks,.cap0,.cap1,.cap2{opacity:1!important;transform:none!important}.draw{stroke-dashoffset:0!important}.active{opacity:0!important}}` + "\n")

	fmt.Fprintf(&b, "<style>\n%s</style>\n", css.String())
	writeCommonDefs(&b, "")
	writeStageBackdrop(&b, false)

	spec := defaultRoseSpec(loc.zh, loc.name, loc.glyph, "#6425d0", loc.wc)
	b.WriteString(`<g clip-path="url(#win)">` + "\n")
	rect(&b, 0, 0, 1240, 760, 0, `fill="url(#sh-rose)"`)
	writeToolbarChrome(&b, spec, false)
	writeSidebarContent(&b, spec, false)
	rect(&b, 230, 46, 1002, 706, 10, `fill="#fff" stroke="#000" stroke-opacity=".06"`)

	b.WriteString(`<g clip-path="url(#card)">` + "\n")
	b.WriteString(`<g stroke="#8f959e" stroke-width="1.5" stroke-linecap="round" opacity=".72"><path d="M254 71h14M254 76h10M254 81h14"/></g>` + "\n")
	writeFittedText(&b, 302, 126, 33, "#1f2329", "600", loc.title, 0)
	writeFittedText(&b, 302, 168, 16.5, "#646a73", "400", loc.intro, 0)

	for i, ln := range loc.lines {
		x, y, l := annotLineLayout(i, ln, looks)
		// Rounded 3.5px capsule selection matching Round 3 polish.
		rect(&b, x-3, y-22, ln.width+6, 30, 4.5, fmt.Sprintf(`fill="#BACEFD" fill-opacity=".68" class="anim selection selection%d"`, i))
		if l.under {
			drawAnnotMark(&b, l, x, y, ln.width, 20, fmt.Sprintf("draw%d", i), true)
		}
		writeFittedText(&b, 302, y, 20, "#30343b", "400", ln.prefix, approxWidth(ln.prefix, 20))
		writeFittedText(&b, x, y, 20, "#1f2329", "500", ln.phrase, ln.width)
		writeFittedText(&b, x+ln.width+l.pad*20, y, 20, "#30343b", "400", ln.suffix, 0)
		if !l.under {
			drawAnnotMark(&b, l, x, y, ln.width, 20, fmt.Sprintf("draw%d", i), true)
		}
	}
	writeFittedText(&b, 302, 698, 15.5, "#8f959e", "400", loc.closing, 0)
	b.WriteString("</g>\n") // card

	for i, ln := range loc.lines {
		x, y, _ := annotLineLayout(i, ln, looks)
		y -= 70
		fmt.Fprintf(&b, `<g class="anim toolbar toolbar%d">`+"\n", i)
		rect(&b, x-4, y+4, 204, 40, 10, `fill="#4b3654" opacity=".06"`)
		rect(&b, x, y, 196, 34, 8, `fill="#fff" stroke="#e2dde5" stroke-width="1"`)
		for j, label := range []string{"B", "I", "U", "S", "↗"} {
			writeFittedText(&b, x+12+float64(j)*28, y+23, 15, "#535662", "500", label, 0)
		}
		fmt.Fprintf(&b, `<path d="M%.1f %.1fv20" stroke="#e2dde5"/>`, x+150, y+7)
		writeFittedText(&b, x+160, y+23, 16, "#6425D0", "500", "A", 0)
		drawAnnotMark(&b, looks[0], x+160, y+23, 10, 12, "", false)
		fmt.Fprintf(&b, `<path d="M%.1f %.1fl4 4 4-4" fill="none" stroke="#646a73" stroke-width="1.2" stroke-linecap="round"/>`+"\n", x+178, y+14)
		b.WriteString("</g>\n")
	}

	b.WriteString(`<g class="anim picker">` + "\n")
	for j := 3; j > 0; j-- {
		rect(&b, 900-float64(j)*2, 248+float64(j)*2, 300+float64(j)*4, 336+float64(j)*2, 12+float64(j)*2, `fill="#3d2a48" opacity=".028"`)
	}
	rect(&b, 900, 248, 300, 336, 12, `fill="#fff" stroke="#e0dbe4" stroke-width="1"`)
	writeFittedText(&b, 912, 273, 12.5, "#30343b", "600", loc.panel, 0)
	for i, l := range looks {
		x, y := 910+float64(i%3)*94, 288+float64(i/3)*52
		rect(&b, x, y, 88, 46, 7, `fill="#faf9fc" stroke="#eeebf2" stroke-width=".8"`)
		for j, ln := range loc.lines {
			if ln.mark == l.name {
				rect(&b, x, y, 88, 46, 7, fmt.Sprintf(`fill="#f1eef8" stroke="#c5b4e8" class="anim active active%d"`, j))
			}
		}
		label := loc.names[i]
		size := 12.0
		w := approxWidth(label, size)
		if w > 78 {
			w = 78
		}
		tx := x + (88-w)/2
		ty := y + 28
		if l.under {
			drawAnnotMark(&b, l, tx, ty, w, size, "", false)
		}
		writeFittedText(&b, tx, ty, size, "#454851", "400", label, w)
		if !l.under {
			drawAnnotMark(&b, l, tx, ty, w, size, "", false)
		}
	}
	writeFittedText(&b, 912, 519, 12, "#30343b", "500", loc.colors, 0)
	for i, c := range []string{"", "gray", "red", "orange", "yellow", "green", "blue", "purple"} {
		x := 924 + float64(i)*36
		ink := annotColors[c]
		if c == "" {
			ink = "#fff"
		}
		fmt.Fprintf(&b, `<circle cx="%.1f" cy="541" r="9" fill="%s" stroke="#ddd8e4" stroke-width=".7"/>`, x, ink)
		if c == "" {
			fmt.Fprintf(&b, `<circle cx="%.1f" cy="541" r="12" fill="none" stroke="#9585af" stroke-width="1.3"/><path d="M919 541l3 3 6-6" fill="none" stroke="#80758b" stroke-width="1.5"/>`, x)
		}
	}
	rect(&b, 911, 559, 278, 19, 5.5, `fill="none" stroke="#ebe7ee"`)
	writeFittedText(&b, 900+(300-approxWidth(loc.remove, 11))/2, 572.5, 11, "#73717b", "400", loc.remove, 0)
	b.WriteString("</g>\n")

	for i, ln := range loc.lines {
		li := 0
		for j, l := range looks {
			if l.name == ln.mark {
				li = j
			}
		}
		fmt.Fprintf(&b, `<circle cx="%.1f" cy="%.1f" r="11" fill="none" stroke="%s" stroke-width="1.6" class="anim click click%d"/>`+"\n",
			954+float64(li%3)*94, 311+float64(li/3)*52, annotColors[looks[li].color], i)
	}
	b.WriteString(`<g class="anim cursor"><use href="#cursor"/></g>` + "\n")
	b.WriteString("</g></g>\n") // win & transform

	writeCaptionBar(&b, "anim", loc.captions, loc.capX)
	b.WriteString("</g></svg>\n")

	out := filepath.Join("docs/images/readme", "annotations-"+loc.code+".svg")
	if err := os.WriteFile(out, b.Bytes(), 0644); err != nil {
		return err
	}

	// Keep the accompanying .arcmd demo sample deterministic.
	d := document.New()
	title := document.NewBlock("heading", loc.title)
	title.Level = 1
	d.Blocks = []*document.Block{title, document.NewBlock("paragraph", loc.intro)}
	for _, ln := range loc.lines {
		para := document.NewBlock("paragraph", "")
		para.Runs = []document.Run{{Text: ln.prefix}, {Text: ln.phrase, Style: document.Style{Annotation: ln.mark}}, {Text: ln.suffix}}
		d.Blocks = append(d.Blocks, para)
	}
	d.Blocks = append(d.Blocks, document.NewBlock("paragraph", loc.closing))
	for i, blk := range d.Blocks {
		blk.ID = fmt.Sprintf("promo-%s-%02d", loc.code, i)
	}
	encoded, err := d.Encode()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join("docs/images/readme", "annotations-demo-"+loc.code+".arcmd"), []byte(encoded), 0644); err != nil {
		return err
	}
	fmt.Printf("%s (%d bytes)\n", out, b.Len())
	return nil
}
