package pdfdoc

import (
	"encoding/binary"
	"math"
	"strings"
	"unicode/utf16"
)

type fontCMap struct {
	twoByte bool
	entries map[uint32]string
}

func (r *rawPDF) extractPageText(page int) string {
	if page < 0 || page >= len(r.pageDicts) {
		return ""
	}
	dict := r.pageDicts[page]
	fonts := r.collectFonts(dict, 0)
	contentsVal, ok := dict.dictGet("Contents")
	if !ok {
		return ""
	}
	var streams [][]byte
	contents := r.resolve(contentsVal, 1)
	switch contents.kind {
	case valDict:
		if dec, err := r.decodeStream(contents); err == nil && len(dec) > 0 {
			streams = append(streams, dec)
		}
	case valArray:
		for _, item := range contents.arr {
			obj := r.resolve(item, 1)
			if obj.kind == valDict {
				if dec, err := r.decodeStream(obj); err == nil && len(dec) > 0 {
					streams = append(streams, dec)
				}
			}
		}
	}
	if len(streams) == 0 {
		return ""
	}
	var out strings.Builder
	var curFont *fontCMap
	lastY := math.NaN()
	hasLastY := false

	emitBreak := func(newY float64) {
		if hasLastY && math.Abs(newY-lastY) > 2.5 {
			out.WriteByte('\n')
		}
		lastY = newY
		hasLastY = true
	}

	for _, stm := range streams {
		sc := pdfScanner{b: stm}
		var args []pdfVal
		textY := 0.0
		lineY := 0.0
		for {
			sc.skipWS()
			if sc.pos >= len(sc.b) {
				break
			}
			c := sc.b[sc.pos]
			if c == '/' || c == '(' || c == '<' || c == '[' || c == '+' || c == '-' || c == '.' || (c >= '0' && c <= '9') {
				v, ok := sc.readValue()
				if !ok {
					sc.pos++
					continue
				}
				args = append(args, v)
				if len(args) > 64 {
					args = args[len(args)-32:]
				}
				continue
			}
			// Operator keyword.
			start := sc.pos
			for sc.pos < len(sc.b) && !isPDFDelimOrWS(sc.b[sc.pos]) {
				sc.pos++
			}
			op := string(sc.b[start:sc.pos])
			switch op {
			case "BT":
				textY, lineY = 0, 0
			case "Tf":
				if len(args) >= 2 && args[len(args)-2].kind == valName {
					curFont = fonts[args[len(args)-2].str]
				}
			case "Td", "TD":
				if len(args) >= 2 && args[len(args)-1].kind == valNum {
					dy := args[len(args)-1].num
					dx := args[len(args)-2].num
					lineY += dy
					textY = lineY
					if math.Abs(dy) > 2.0 {
						emitBreak(textY)
					} else if dx > 6.0 && out.Len() > 0 {
						last := out.String()[out.Len()-1]
						if last != ' ' && last != '\n' {
							out.WriteByte(' ')
						}
					}
				}
			case "Tm":
				if len(args) >= 6 && args[len(args)-1].kind == valNum {
					ny := args[len(args)-1].num
					lineY = ny
					textY = ny
					emitBreak(textY)
				}
			case "T*":
				lineY -= 12
				textY = lineY
				out.WriteByte('\n')
			case "Tj":
				if len(args) >= 1 && args[len(args)-1].kind == valString {
					out.WriteString(decodeGlyphBytes([]byte(args[len(args)-1].str), curFont))
				}
			case "'":
				out.WriteByte('\n')
				if len(args) >= 1 && args[len(args)-1].kind == valString {
					out.WriteString(decodeGlyphBytes([]byte(args[len(args)-1].str), curFont))
				}
			case `"`:
				out.WriteByte('\n')
				if len(args) >= 1 && args[len(args)-1].kind == valString {
					out.WriteString(decodeGlyphBytes([]byte(args[len(args)-1].str), curFont))
				}
			case "TJ":
				if len(args) >= 1 && args[len(args)-1].kind == valArray {
					for _, elem := range args[len(args)-1].arr {
						switch elem.kind {
						case valString:
							out.WriteString(decodeGlyphBytes([]byte(elem.str), curFont))
						case valNum:
							if elem.num < -180 && out.Len() > 0 {
								last := out.String()[out.Len()-1]
								if last != ' ' && last != '\n' {
									out.WriteByte(' ')
								}
							}
						}
					}
				}
			case "BI":
				// Skip inline image until EI.
				for sc.pos+3 < len(sc.b) {
					if isPDFWS(sc.b[sc.pos]) && sc.b[sc.pos+1] == 'E' && sc.b[sc.pos+2] == 'I' && isPDFDelimOrWS(sc.b[sc.pos+3]) {
						sc.pos += 3
						break
					}
					sc.pos++
				}
			}
			args = args[:0]
		}
		out.WriteByte('\n')
	}
	return out.String()
}

func (r *rawPDF) collectFonts(dict pdfVal, depth int) map[string]*fontCMap {
	out := map[string]*fontCMap{}
	if depth > 4 {
		return out
	}
	resVal, ok := dict.dictGet("Resources")
	if !ok {
		return out
	}
	res := r.resolve(resVal, 1)
	fontDictVal, ok := res.dictGet("Font")
	if !ok {
		return out
	}
	fontDict := r.resolve(fontDictVal, 1)
	if fontDict.kind != valDict {
		return out
	}
	for name, fRef := range fontDict.dict {
		fObj := r.resolve(fRef, 1)
		if fObj.kind != valDict {
			continue
		}
		cm := &fontCMap{entries: map[uint32]string{}}
		if enc, ok := fObj.dictGet("Encoding"); ok {
			enc = r.resolve(enc, 1)
			if enc.kind == valName && strings.HasPrefix(enc.str, "Identity-") {
				cm.twoByte = true
			}
		}
		if sub, ok := fObj.dictGet("Subtype"); ok && sub.str == "Type0" {
			cm.twoByte = true
		}
		if tu, ok := fObj.dictGet("ToUnicode"); ok {
			tuObj := r.resolve(tu, 1)
			if tuObj.kind == valDict {
				if data, err := r.decodeStream(tuObj); err == nil && len(data) > 0 {
					parseToUnicodeCMap(data, cm)
				}
			}
		}
		out[name] = cm
	}
	return out
}

func parseToUnicodeCMap(data []byte, cm *fontCMap) {
	sc := pdfScanner{b: data}
	var toks []pdfVal
	var ops []string
	for {
		sc.skipWS()
		if sc.pos >= len(sc.b) {
			break
		}
		c := sc.b[sc.pos]
		if c == '<' || c == '[' || c == '(' || c == '/' || (c >= '0' && c <= '9') {
			// Track byte length of hex codes so 1-byte vs 2-byte codes are known.
			if c == '<' && sc.pos+1 < len(sc.b) && sc.b[sc.pos+1] != '<' {
				hexStart := sc.pos + 1
				v, ok := sc.readValue()
				if ok {
					hexDigits := 0
					for i := hexStart; i < sc.pos && sc.b[i] != '>'; i++ {
						if hexNib(sc.b[i]) >= 0 {
							hexDigits++
						}
					}
					if hexDigits >= 4 {
						v.refGen = 2
					} else {
						v.refGen = 1
					}
					toks = append(toks, v)
					ops = append(ops, "")
				}
				continue
			}
			if v, ok := sc.readValue(); ok {
				toks = append(toks, v)
				ops = append(ops, "")
				continue
			}
			sc.pos++
			continue
		}
		start := sc.pos
		for sc.pos < len(sc.b) && !isPDFDelimOrWS(sc.b[sc.pos]) {
			sc.pos++
		}
		kw := string(sc.b[start:sc.pos])
		switch kw {
		case "begincodespacerange":
			toks = toks[:0]
			ops = ops[:0]
		case "endcodespacerange":
			for _, t := range toks {
				if t.kind == valString && len(t.str) >= 2 {
					cm.twoByte = true
				}
			}
			toks = toks[:0]
			ops = ops[:0]
		case "beginbfchar", "beginbfrange":
			toks = toks[:0]
			ops = ops[:0]
		case "endbfchar":
			for i := 0; i+1 < len(toks); i += 2 {
				src, dst := toks[i], toks[i+1]
				if src.kind != valString || dst.kind != valString {
					continue
				}
				if len(src.str) >= 2 || src.refGen == 2 {
					cm.twoByte = true
				}
				cm.entries[codeFromBytes([]byte(src.str))] = utf16BytesToString([]byte(dst.str))
			}
			toks = toks[:0]
			ops = ops[:0]
		case "endbfrange":
			for i := 0; i+2 < len(toks); i += 3 {
				lo, hi, target := toks[i], toks[i+1], toks[i+2]
				if lo.kind != valString || hi.kind != valString {
					continue
				}
				if len(lo.str) >= 2 || lo.refGen == 2 {
					cm.twoByte = true
				}
				c0 := codeFromBytes([]byte(lo.str))
				c1 := codeFromBytes([]byte(hi.str))
				if c1 < c0 || c1-c0 > 65535 {
					continue
				}
				switch target.kind {
				case valString:
					base := []byte(target.str)
					for step := uint32(0); step <= c1-c0; step++ {
						cur := incrementUTF16Bytes(base, step)
						cm.entries[c0+step] = utf16BytesToString(cur)
					}
				case valArray:
					for step, elem := range target.arr {
						if uint32(step) > c1-c0 {
							break
						}
						if elem.kind == valString {
							cm.entries[c0+uint32(step)] = utf16BytesToString([]byte(elem.str))
						}
					}
				}
			}
			toks = toks[:0]
			ops = ops[:0]
		default:
			_ = ops
		}
	}
}

func codeFromBytes(b []byte) uint32 {
	var v uint32
	for _, x := range b {
		v = (v << 8) | uint32(x)
	}
	return v
}

func incrementUTF16Bytes(base []byte, add uint32) []byte {
	out := make([]byte, len(base))
	copy(out, base)
	if len(out) >= 2 {
		last := uint32(binary.BigEndian.Uint16(out[len(out)-2:])) + add
		binary.BigEndian.PutUint16(out[len(out)-2:], uint16(last))
	} else if len(out) == 1 {
		out[0] = byte(uint32(out[0]) + add)
	}
	return out
}

func utf16BytesToString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) == 1 {
		return string(rune(b[0]))
	}
	if len(b)%2 == 0 {
		u16 := make([]uint16, len(b)/2)
		for i := range u16 {
			u16[i] = binary.BigEndian.Uint16(b[i*2 : i*2+2])
		}
		if len(u16) > 0 && u16[0] == 0xFEFF {
			u16 = u16[1:]
		}
		return string(utf16.Decode(u16))
	}
	return string(b)
}

func decodeGlyphBytes(raw []byte, cm *fontCMap) string {
	if len(raw) == 0 {
		return ""
	}
	if cm != nil && len(cm.entries) > 0 {
		var out strings.Builder
		if cm.twoByte && len(raw)%2 == 0 {
			for i := 0; i+1 < len(raw); i += 2 {
				code := uint32(raw[i])<<8 | uint32(raw[i+1])
				if s, ok := cm.entries[code]; ok {
					out.WriteString(s)
				} else if code >= 32 && code < 127 {
					out.WriteByte(byte(code))
				}
			}
			return out.String()
		}
		for _, b := range raw {
			if s, ok := cm.entries[uint32(b)]; ok {
				out.WriteString(s)
			} else if b >= 32 && b < 127 {
				out.WriteByte(b)
			}
		}
		return out.String()
	}
	return decodePDFTextString(string(raw))
}
