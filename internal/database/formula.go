package database

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxFormulaSteps = 4096

var (
	formulaASTCache sync.Map // string -> compiledFormula
	formulaASTCount atomic.Int64
)

type compiledFormula struct {
	root fNode
	err  error
}

// --- Lexer ---

type fTokKind uint8

const (
	fTokEOF fTokKind = iota
	fTokNum
	fTokStr
	fTokIdent
	fTokBool
	fTokNull
	fTokLParen
	fTokRParen
	fTokLBrack
	fTokRBrack
	fTokComma
	fTokDot
	fTokQuestion
	fTokColon
	fTokPlus
	fTokMinus
	fTokMul
	fTokDiv
	fTokMod
	fTokPow
	fTokEq
	fTokNeq
	fTokLT
	fTokLTE
	fTokGT
	fTokGTE
	fTokAnd
	fTokOr
	fTokNot
)

type fToken struct {
	kind fTokKind
	text string
	num  float64
	b    bool
	pos  int
}

func tokenizeFormula(src string) ([]fToken, error) {
	var toks []fToken
	i := 0
	n := len(src)
	for i < n {
		r, w := utf8.DecodeRuneInString(src[i:])
		if unicode.IsSpace(r) {
			i += w
			continue
		}
		pos := i
		if r == '"' || r == '\'' {
			quote := r
			i += w
			var b strings.Builder
			closed := false
			for i < n {
				c, cw := utf8.DecodeRuneInString(src[i:])
				i += cw
				if c == quote {
					closed = true
					break
				}
				if c == '\\' && i < n {
					esc, ew := utf8.DecodeRuneInString(src[i:])
					i += ew
					switch esc {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case 'r':
						b.WriteByte('\r')
					case '\\', '"', '\'':
						b.WriteRune(esc)
					default:
						b.WriteRune(esc)
					}
					continue
				}
				b.WriteRune(c)
			}
			if !closed {
				return nil, fmt.Errorf("unterminated string at position %d", pos)
			}
			toks = append(toks, fToken{kind: fTokStr, text: b.String(), pos: pos})
			continue
		}

		if (r >= '0' && r <= '9') || (r == '.' && i+1 < n && src[i+1] >= '0' && src[i+1] <= '9') {
			j := i
			hasDot := false
			for j < n {
				ch := src[j]
				if ch >= '0' && ch <= '9' {
					j++
				} else if ch == '.' && !hasDot {
					hasDot = true
					j++
				} else {
					break
				}
			}
			if j < n && (src[j] == 'e' || src[j] == 'E') {
				k := j + 1
				if k < n && (src[k] == '+' || src[k] == '-') {
					k++
				}
				digits := 0
				for k < n && src[k] >= '0' && src[k] <= '9' {
					k++
					digits++
				}
				if digits > 0 {
					j = k
				}
			}
			num, err := strconv.ParseFloat(src[i:j], 64)
			if err != nil {
				return nil, fmt.Errorf("invalid number %q at position %d", src[i:j], pos)
			}
			toks = append(toks, fToken{kind: fTokNum, text: src[i:j], num: num, pos: pos})
			i = j
			continue
		}

		// Two-char operators.
		if i+1 < n {
			two := src[i : i+2]
			var tk fTokKind
			matched := true
			switch two {
			case "==":
				tk = fTokEq
			case "!=":
				tk = fTokNeq
			case "<=":
				tk = fTokLTE
			case ">=":
				tk = fTokGTE
			case "&&":
				tk = fTokAnd
			case "||":
				tk = fTokOr
			default:
				matched = false
			}
			if matched {
				toks = append(toks, fToken{kind: tk, text: two, pos: pos})
				i += 2
				continue
			}
		}

		// Single-char punctuation/operators.
		singleMatched := true
		var stk fTokKind
		switch r {
		case '(':
			stk = fTokLParen
		case ')':
			stk = fTokRParen
		case '[':
			stk = fTokLBrack
		case ']':
			stk = fTokRBrack
		case ',':
			stk = fTokComma
		case '.':
			stk = fTokDot
		case '?':
			stk = fTokQuestion
		case ':':
			stk = fTokColon
		case '+':
			stk = fTokPlus
		case '-':
			stk = fTokMinus
		case '*':
			stk = fTokMul
		case '/':
			stk = fTokDiv
		case '%':
			stk = fTokMod
		case '^':
			stk = fTokPow
		case '<':
			stk = fTokLT
		case '>':
			stk = fTokGT
		case '!':
			stk = fTokNot
		default:
			singleMatched = false
		}
		if singleMatched {
			toks = append(toks, fToken{kind: stk, text: string(r), pos: pos})
			i += w
			continue
		}

		if unicode.IsLetter(r) || r == '_' {
			j := i + w
			for j < n {
				nr, nw := utf8.DecodeRuneInString(src[j:])
				if unicode.IsLetter(nr) || unicode.IsDigit(nr) || nr == '_' {
					j += nw
				} else {
					break
				}
			}
			ident := src[i:j]
			switch ident {
			case "true":
				toks = append(toks, fToken{kind: fTokBool, text: ident, b: true, pos: pos})
			case "false":
				toks = append(toks, fToken{kind: fTokBool, text: ident, b: false, pos: pos})
			case "null":
				toks = append(toks, fToken{kind: fTokNull, text: ident, pos: pos})
			default:
				toks = append(toks, fToken{kind: fTokIdent, text: ident, pos: pos})
			}
			i = j
			continue
		}

		return nil, fmt.Errorf("unexpected character %q at position %d", r, pos)
	}
	toks = append(toks, fToken{kind: fTokEOF, pos: n})
	return toks, nil
}

// --- AST & Parser ---

type fNode interface{ fNodeMarker() }

type fLitNode struct{ val fVal }
type fIdentNode struct{ name string }
type fListNode struct{ elems []fNode }
type fUnaryNode struct {
	op   fTokKind
	expr fNode
}
type fBinaryNode struct {
	op    fTokKind
	left  fNode
	right fNode
}
type fTernaryNode struct {
	cond fNode
	yes  fNode
	no   fNode
}
type fCallNode struct {
	name string
	args []fNode
}
type fMethodNode struct {
	recv fNode
	name string
	args []fNode
	call bool
}
type fIndexNode struct {
	recv  fNode
	index fNode
}

func (fLitNode) fNodeMarker()     {}
func (fIdentNode) fNodeMarker()   {}
func (fListNode) fNodeMarker()    {}
func (fUnaryNode) fNodeMarker()   {}
func (fBinaryNode) fNodeMarker()  {}
func (fTernaryNode) fNodeMarker() {}
func (fCallNode) fNodeMarker()    {}
func (fMethodNode) fNodeMarker()  {}
func (fIndexNode) fNodeMarker()   {}

type fParser struct {
	toks []fToken
	pos  int
}

func (p *fParser) peek() fToken {
	if p.pos >= len(p.toks) {
		return fToken{kind: fTokEOF}
	}
	return p.toks[p.pos]
}

func (p *fParser) next() fToken {
	t := p.peek()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return t
}

func (p *fParser) expect(k fTokKind, msg string) (fToken, error) {
	t := p.peek()
	if t.kind != k {
		return t, fmt.Errorf("%s at position %d", msg, t.pos)
	}
	p.pos++
	return t, nil
}

func parseFormulaAST(expr string) (fNode, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return fLitNode{val: fEmptyVal()}, nil
	}
	if cached, ok := formulaASTCache.Load(expr); ok {
		cf := cached.(compiledFormula)
		return cf.root, cf.err
	}

	toks, err := tokenizeFormula(expr)
	var root fNode
	if err == nil {
		p := &fParser{toks: toks}
		root, err = p.parseTernary()
		if err == nil && p.peek().kind != fTokEOF {
			err = fmt.Errorf("unexpected token %q at position %d", p.peek().text, p.peek().pos)
		}
	}

	if formulaASTCount.Load() >= 512 {
		formulaASTCache.Clear()
		formulaASTCount.Store(0)
	}
	formulaASTCache.Store(expr, compiledFormula{root: root, err: err})
	formulaASTCount.Add(1)
	return root, err
}

func (p *fParser) parseTernary() (fNode, error) {
	cond, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind == fTokQuestion {
		p.next()
		yes, err := p.parseTernary()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(fTokColon, "expected ':' in ternary expression"); err != nil {
			return nil, err
		}
		no, err := p.parseTernary()
		if err != nil {
			return nil, err
		}
		return fTernaryNode{cond: cond, yes: yes, no: no}, nil
	}
	return cond, nil
}

func (p *fParser) isKeywordOp(kw string) bool {
	t := p.peek()
	return t.kind == fTokIdent && t.text == kw
}

func (p *fParser) parseOr() (fNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == fTokOr || p.isKeywordOp("or") {
		p.next()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = fBinaryNode{op: fTokOr, left: left, right: right}
	}
	return left, nil
}

func (p *fParser) parseAnd() (fNode, error) {
	left, err := p.parseEq()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == fTokAnd || p.isKeywordOp("and") {
		p.next()
		right, err := p.parseEq()
		if err != nil {
			return nil, err
		}
		left = fBinaryNode{op: fTokAnd, left: left, right: right}
	}
	return left, nil
}

func (p *fParser) parseEq() (fNode, error) {
	left, err := p.parseCmp()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == fTokEq || p.peek().kind == fTokNeq {
		op := p.next().kind
		right, err := p.parseCmp()
		if err != nil {
			return nil, err
		}
		left = fBinaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *fParser) parseCmp() (fNode, error) {
	left, err := p.parseAdd()
	if err != nil {
		return nil, err
	}
	for {
		k := p.peek().kind
		if k != fTokLT && k != fTokLTE && k != fTokGT && k != fTokGTE {
			break
		}
		p.next()
		right, err := p.parseAdd()
		if err != nil {
			return nil, err
		}
		left = fBinaryNode{op: k, left: left, right: right}
	}
	return left, nil
}

func (p *fParser) parseAdd() (fNode, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == fTokPlus || p.peek().kind == fTokMinus {
		op := p.next().kind
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		left = fBinaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *fParser) parseMul() (fNode, error) {
	left, err := p.parsePow()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == fTokMul || p.peek().kind == fTokDiv || p.peek().kind == fTokMod {
		op := p.next().kind
		right, err := p.parsePow()
		if err != nil {
			return nil, err
		}
		left = fBinaryNode{op: op, left: left, right: right}
	}
	return left, nil
}

func (p *fParser) parsePow() (fNode, error) {
	base, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if p.peek().kind == fTokPow {
		p.next()
		exp, err := p.parsePow()
		if err != nil {
			return nil, err
		}
		return fBinaryNode{op: fTokPow, left: base, right: exp}, nil
	}
	return base, nil
}

func (p *fParser) parseUnary() (fNode, error) {
	t := p.peek()
	if t.kind == fTokNot || t.kind == fTokMinus || t.kind == fTokPlus || (t.kind == fTokIdent && t.text == "not") {
		if t.kind == fTokIdent && p.pos+1 < len(p.toks) && p.toks[p.pos+1].kind == fTokLParen {
			return p.parsePostfix()
		}
		p.next()
		op := t.kind
		if t.kind == fTokIdent {
			op = fTokNot
		}
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return fUnaryNode{op: op, expr: inner}, nil
	}
	return p.parsePostfix()
}

func (p *fParser) parseArgList(closeTok fTokKind, errMsg string) ([]fNode, error) {
	var args []fNode
	if p.peek().kind == closeTok {
		p.next()
		return args, nil
	}
	for {
		arg, err := p.parseTernary()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.peek().kind == fTokComma {
			p.next()
			if p.peek().kind == closeTok {
				p.next()
				return args, nil
			}
			continue
		}
		if _, err := p.expect(closeTok, errMsg); err != nil {
			return nil, err
		}
		return args, nil
	}
}

func (p *fParser) parsePostfix() (fNode, error) {
	expr, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.peek().kind {
		case fTokDot:
			p.next()
			id, err := p.expect(fTokIdent, "expected method or property name after '.'")
			if err != nil {
				return nil, err
			}
			if p.peek().kind == fTokLParen {
				p.next()
				args, err := p.parseArgList(fTokRParen, "expected ')' after arguments")
				if err != nil {
					return nil, err
				}
				expr = fMethodNode{recv: expr, name: id.text, args: args, call: true}
			} else {
				expr = fMethodNode{recv: expr, name: id.text, call: false}
			}
		case fTokLBrack:
			p.next()
			idx, err := p.parseTernary()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(fTokRBrack, "expected ']' after index"); err != nil {
				return nil, err
			}
			expr = fIndexNode{recv: expr, index: idx}
		default:
			return expr, nil
		}
	}
}

func (p *fParser) parsePrimary() (fNode, error) {
	t := p.peek()
	switch t.kind {
	case fTokNum:
		p.next()
		return fLitNode{val: fNumVal(t.num)}, nil
	case fTokStr:
		p.next()
		return fLitNode{val: fStrVal(t.text)}, nil
	case fTokBool:
		p.next()
		return fLitNode{val: fBoolVal(t.b)}, nil
	case fTokNull:
		p.next()
		return fLitNode{val: fEmptyVal()}, nil
	case fTokLParen:
		p.next()
		inner, err := p.parseTernary()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(fTokRParen, "expected ')'"); err != nil {
			return nil, err
		}
		return inner, nil
	case fTokLBrack:
		p.next()
		elems, err := p.parseArgList(fTokRBrack, "expected ']' after list literal")
		if err != nil {
			return nil, err
		}
		return fListNode{elems: elems}, nil
	case fTokIdent:
		p.next()
		if p.peek().kind == fTokLParen {
			p.next()
			args, err := p.parseArgList(fTokRParen, "expected ')' after arguments")
			if err != nil {
				return nil, err
			}
			return fCallNode{name: t.text, args: args}, nil
		}
		return fIdentNode{name: t.text}, nil
	default:
		return nil, fmt.Errorf("unexpected token %q at position %d", t.text, t.pos)
	}
}

// --- Runtime Values ---

type fKind uint8

const (
	fKindEmpty fKind = iota
	fKindNum
	fKindStr
	fKindBool
	fKindDate
	fKindList
	fKindRec
)

type fVal struct {
	kind  fKind
	num   float64
	str   string
	b     bool
	date  DateValue
	list  []fVal
	recDB *Database
	rec   *Record
}

func fEmptyVal() fVal           { return fVal{kind: fKindEmpty} }
func fNumVal(n float64) fVal    { return fVal{kind: fKindNum, num: n} }
func fStrVal(s string) fVal     { return fVal{kind: fKindStr, str: s} }
func fBoolVal(b bool) fVal      { return fVal{kind: fKindBool, b: b} }
func fDateVal(d DateValue) fVal { return fVal{kind: fKindDate, date: d} }
func fListVal(xs []fVal) fVal   { return fVal{kind: fKindList, list: xs} }

func fValFromValue(v Value) fVal {
	if v.IsEmpty() || v.Kind == ValError {
		return fEmptyVal()
	}
	switch v.Kind {
	case ValNumber:
		return fNumVal(v.Number)
	case ValBool:
		return fBoolVal(v.Bool)
	case ValText:
		return fStrVal(v.Text)
	case ValDate:
		return fDateVal(v.Date)
	case ValStringList:
		xs := make([]fVal, len(v.List))
		for i, s := range v.List {
			xs[i] = fStrVal(s)
		}
		return fListVal(xs)
	default:
		return fEmptyVal()
	}
}

func (v fVal) isEmpty() bool {
	switch v.kind {
	case fKindEmpty:
		return true
	case fKindStr:
		return v.str == ""
	case fKindDate:
		return v.date.IsEmpty()
	case fKindList:
		return len(v.list) == 0
	case fKindRec:
		return v.rec == nil && v.str == ""
	default:
		return false
	}
}

func (v fVal) truthy() bool {
	switch v.kind {
	case fKindEmpty:
		return false
	case fKindBool:
		return v.b
	case fKindNum:
		return v.num != 0 && !math.IsNaN(v.num)
	case fKindStr:
		return v.str != ""
	case fKindDate:
		return !v.date.IsEmpty()
	case fKindList:
		return len(v.list) > 0
	case fKindRec:
		return v.rec != nil || v.str != ""
	default:
		return false
	}
}

func (v fVal) asString() string {
	switch v.kind {
	case fKindEmpty:
		return ""
	case fKindStr:
		return v.str
	case fKindNum:
		return formatFloatCompact(v.num)
	case fKindBool:
		if v.b {
			return "true"
		}
		return "false"
	case fKindDate:
		if v.date.End != "" {
			return v.date.Start + " → " + v.date.End
		}
		return v.date.Start
	case fKindRec:
		if v.rec != nil && v.rec.Title != "" {
			return v.rec.Title
		}
		return v.str
	case fKindList:
		parts := make([]string, len(v.list))
		for i, el := range v.list {
			parts[i] = el.asString()
		}
		return strings.Join(parts, ", ")
	default:
		return ""
	}
}

func (v fVal) asNum() (float64, bool) {
	switch v.kind {
	case fKindEmpty:
		return 0, false
	case fKindNum:
		return v.num, !math.IsNaN(v.num) && !math.IsInf(v.num, 0)
	case fKindBool:
		if v.b {
			return 1, true
		}
		return 0, true
	case fKindStr:
		return parseNumericText(v.str)
	case fKindDate:
		if t, ok := v.date.StartTime(); ok {
			return float64(t.UnixMilli()), true
		}
		return 0, false
	default:
		return 0, false
	}
}

func (v fVal) asDate(loc *time.Location) (time.Time, DateValue, bool) {
	if loc == nil {
		loc = time.UTC
	}
	switch v.kind {
	case fKindDate:
		t, ok := v.date.parseBoundary(v.date.Start, loc)
		return t, v.date, ok
	case fKindStr:
		t, inc, ok := ParseDateString(v.str, loc)
		if !ok {
			return time.Time{}, DateValue{}, false
		}
		return t, DateValue{Start: formatNormalizedDate(v.str, t, inc), IncludeTime: inc}, true
	case fKindNum:
		t := time.UnixMilli(int64(v.num)).In(loc)
		return t, DateValue{Start: t.Format("2006-01-02T15:04"), IncludeTime: true}, true
	default:
		return time.Time{}, DateValue{}, false
	}
}

func (v fVal) toValue() Value {
	switch v.kind {
	case fKindEmpty:
		return EmptyValue()
	case fKindNum:
		if math.IsNaN(v.num) || math.IsInf(v.num, 0) {
			return EmptyValue()
		}
		return NumberValue(v.num)
	case fKindStr:
		return TextValue(v.str)
	case fKindBool:
		return BoolValue(v.b)
	case fKindDate:
		if v.date.IsEmpty() {
			return EmptyValue()
		}
		return Value{Kind: ValDate, Date: v.date.Clone()}
	case fKindRec:
		return TextValue(v.asString())
	case fKindList:
		items := make([]string, 0, len(v.list))
		for _, item := range v.list {
			if s := item.asString(); s != "" {
				items = append(items, s)
			}
		}
		if len(items) == 0 {
			return EmptyValue()
		}
		return ListValue(items...)
	default:
		return EmptyValue()
	}
}

func fValEqual(a, b fVal) bool {
	if a.kind == fKindEmpty || b.kind == fKindEmpty {
		return a.isEmpty() && b.isEmpty()
	}
	if a.kind == fKindNum && b.kind == fKindNum {
		return a.num == b.num
	}
	if a.kind == fKindBool && b.kind == fKindBool {
		return a.b == b.b
	}
	if a.kind == fKindDate && b.kind == fKindDate {
		ta, _, oka := a.asDate(time.UTC)
		tb, _, okb := b.asDate(time.UTC)
		if oka && okb {
			return ta.Equal(tb)
		}
		return a.date.Equal(b.date)
	}
	if a.kind == fKindList && b.kind == fKindList {
		if len(a.list) != len(b.list) {
			return false
		}
		for i := range a.list {
			if !fValEqual(a.list[i], b.list[i]) {
				return false
			}
		}
		return true
	}
	if (a.kind == fKindNum && b.kind == fKindStr) || (a.kind == fKindStr && b.kind == fKindNum) {
		an, aok := a.asNum()
		bn, bok := b.asNum()
		if aok && bok {
			return an == bn
		}
	}
	return a.asString() == b.asString()
}

func fValCompare(a, b fVal) (int, error) {
	if a.isEmpty() && b.isEmpty() {
		return 0, nil
	}
	if a.kind == fKindEmpty {
		return -1, nil
	}
	if b.kind == fKindEmpty {
		return 1, nil
	}
	if a.kind == fKindDate || b.kind == fKindDate {
		ta, _, oka := a.asDate(time.UTC)
		tb, _, okb := b.asDate(time.UTC)
		if oka && okb {
			switch {
			case ta.Before(tb):
				return -1, nil
			case ta.After(tb):
				return 1, nil
			default:
				return 0, nil
			}
		}
	}
	if a.kind == fKindNum && b.kind == fKindNum {
		return cmp.Compare(a.num, b.num), nil
	}
	if (a.kind == fKindNum || a.kind == fKindBool) && (b.kind == fKindNum || b.kind == fKindBool) {
		an, aok := a.asNum()
		bn, bok := b.asNum()
		if aok && bok {
			return cmp.Compare(an, bn), nil
		}
	}
	return cmp.Compare(a.asString(), b.asString()), nil
}

// --- Evaluator ---

type fEnv struct {
	ctx     *evalContext
	db      *Database
	propMap map[string]Property
	rec     *Record
	depth   int
	steps   *int
	vars    map[string]fVal
	parent  *fEnv
}

func (e *fEnv) child(vars map[string]fVal) *fEnv {
	return &fEnv{
		ctx:     e.ctx,
		db:      e.db,
		propMap: e.propMap,
		rec:     e.rec,
		depth:   e.depth,
		steps:   e.steps,
		vars:    vars,
		parent:  e,
	}
}

func (e *fEnv) lookupVar(name string) (fVal, bool) {
	for cur := e; cur != nil; cur = cur.parent {
		if cur.vars != nil {
			if v, ok := cur.vars[name]; ok {
				return v, true
			}
		}
	}
	return fVal{}, false
}

func (e *fEnv) tick() error {
	*e.steps++
	if *e.steps > maxFormulaSteps {
		return errors.New("#LIMIT!")
	}
	return nil
}

// ValidateFormula checks whether expr parses without syntax errors.
func ValidateFormula(expr string) error {
	_, err := parseFormulaAST(expr)
	return err
}

// EvaluateFormula parses and evaluates a Formula 2.0 expression against rec.
func EvaluateFormula(s *Store, rec *Record, expr string, now time.Time) Value {
	var ctx *evalContext
	if s != nil {
		s.mu.RLock()
		ctx = newEvalContext(s, s.db, s.propMap, s.records, now)
		s.mu.RUnlock()
	} else {
		ctx = newEvalContext(nil, nil, map[string]Property{}, nil, now)
	}
	return evaluateFormulaWithContext(ctx, rec, expr, 0)
}

func evaluateFormulaWithContext(ctx *evalContext, rec *Record, expr string, depth int) Value {
	if depth > maxComputedEvalDepth {
		return ErrorValue("#CYCLE!")
	}
	root, err := parseFormulaAST(expr)
	if err != nil {
		return ErrorValue("#SYNTAX!")
	}
	if ctx == nil {
		ctx = newEvalContext(nil, nil, map[string]Property{}, nil, time.Now())
	}
	steps := 0
	env := &fEnv{
		ctx:     ctx,
		db:      ctx.db,
		propMap: ctx.propMap,
		rec:     rec,
		depth:   depth,
		steps:   &steps,
	}
	res, evalErr := env.eval(root)
	if evalErr != nil {
		msg := evalErr.Error()
		if strings.HasPrefix(msg, "#") {
			return ErrorValue(msg)
		}
		return ErrorValue(msg)
	}
	return res.toValue()
}

func (e *fEnv) resolvePropValue(nameOrID string) (fVal, error) {
	if e.rec == nil || e.db == nil {
		return fEmptyVal(), nil
	}
	var found *Property
	for i := range e.db.Properties {
		p := &e.db.Properties[i]
		if p.ID == nameOrID || strings.EqualFold(p.Name, nameOrID) || strings.EqualFold(p.Key, nameOrID) {
			found = p
			break
		}
	}
	if found == nil {
		return fEmptyVal(), nil
	}
	if found.Type == PropRollup || found.Type == PropFormula {
		var v Value
		if e.ctx != nil && e.ctx.db == e.db {
			v = e.ctx.propValue(e.rec, *found, e.depth+1)
		} else {
			subCtx := newEvalContext(nil, e.db, e.propMap, nil, time.Now())
			if e.ctx != nil {
				subCtx.store = e.ctx.store
				subCtx.now = e.ctx.now
			}
			v = subCtx.propValue(e.rec, *found, e.depth+1)
		}
		if v.Kind == ValError {
			return fVal{}, errors.New(v.Error)
		}
		return fValFromValue(v), nil
	}
	if found.Type == PropRelation {
		refs := e.rec.ValueFor(*found).List
		if len(refs) == 0 {
			return fListVal(nil), nil
		}
		tDB, linked := resolveLinkedRecords(e.ctx, e.rec, *found)
		if tDB != nil {
			out := make([]fVal, 0, len(refs))
			matchedRefs := map[string]bool{}
			for _, tr := range linked {
				matchedRefs[tr.ID] = true
				out = append(out, fVal{
					kind:  fKindRec,
					recDB: tDB,
					rec:   tr,
					str:   RecordLinkRef(tr),
				})
			}
			if len(out) == 0 {
				for _, ref := range refs {
					out = append(out, fStrVal(ref))
				}
			}
			return fListVal(out), nil
		}
		return fValFromValue(e.rec.ValueFor(*found)), nil
	}
	return fValFromValue(e.rec.ValueFor(*found)), nil
}

func (e *fEnv) eval(node fNode) (fVal, error) {
	if err := e.tick(); err != nil {
		return fVal{}, err
	}
	switch n := node.(type) {
	case fLitNode:
		return n.val, nil

	case fIdentNode:
		if v, ok := e.lookupVar(n.name); ok {
			return v, nil
		}
		switch n.name {
		case "pi":
			return fNumVal(math.Pi), nil
		case "e":
			return fNumVal(math.E), nil
		}
		// Fall back to direct record property reference by name/key/id.
		if v, err := e.resolvePropValue(n.name); err == nil && !v.isEmpty() {
			return v, nil
		}
		return fEmptyVal(), nil

	case fListNode:
		out := make([]fVal, len(n.elems))
		for i, el := range n.elems {
			v, err := e.eval(el)
			if err != nil {
				return fVal{}, err
			}
			out[i] = v
		}
		return fListVal(out), nil

	case fUnaryNode:
		v, err := e.eval(n.expr)
		if err != nil {
			return fVal{}, err
		}
		switch n.op {
		case fTokNot:
			return fBoolVal(!v.truthy()), nil
		case fTokMinus:
			x, ok := v.asNum()
			if !ok {
				return fEmptyVal(), nil
			}
			return fNumVal(-x), nil
		case fTokPlus:
			x, ok := v.asNum()
			if !ok {
				return fEmptyVal(), nil
			}
			return fNumVal(x), nil
		}
		return fEmptyVal(), nil

	case fTernaryNode:
		c, err := e.eval(n.cond)
		if err != nil {
			return fVal{}, err
		}
		if c.truthy() {
			return e.eval(n.yes)
		}
		return e.eval(n.no)

	case fBinaryNode:
		return e.evalBinary(n)

	case fIndexNode:
		recv, err := e.eval(n.recv)
		if err != nil {
			return fVal{}, err
		}
		idxVal, err := e.eval(n.index)
		if err != nil {
			return fVal{}, err
		}
		idxF, ok := idxVal.asNum()
		if !ok {
			return fEmptyVal(), nil
		}
		idx := int(idxF)
		if recv.kind == fKindList {
			if idx < 0 {
				idx = len(recv.list) + idx
			}
			if idx >= 0 && idx < len(recv.list) {
				return recv.list[idx], nil
			}
			return fEmptyVal(), nil
		}
		if recv.kind == fKindStr {
			runes := []rune(recv.str)
			if idx < 0 {
				idx = len(runes) + idx
			}
			if idx >= 0 && idx < len(runes) {
				return fStrVal(string(runes[idx])), nil
			}
			return fStrVal(""), nil
		}
		return fEmptyVal(), nil

	case fMethodNode:
		recv, err := e.eval(n.recv)
		if err != nil {
			return fVal{}, err
		}
		if !n.call {
			if recv.kind == fKindRec && recv.recDB != nil && recv.rec != nil {
				recPM := map[string]Property{}
				for _, p := range recv.recDB.Properties {
					recPM[p.ID] = p
				}
				recEnv := &fEnv{
					ctx:     e.ctx,
					db:      recv.recDB,
					propMap: recPM,
					rec:     recv.rec,
					depth:   e.depth + 1,
					steps:   e.steps,
				}
				return recEnv.resolvePropValue(n.name)
			}
			// Allow 0-arg helper methods as getters (e.g. s.length vs s.length()).
			return e.evalCallWithFirst(n.name, recv, nil)
		}
		if n.name == "prop" && len(n.args) == 1 && recv.kind == fKindRec && recv.recDB != nil && recv.rec != nil {
			nameVal, err := e.eval(n.args[0])
			if err != nil {
				return fVal{}, err
			}
			recPM := map[string]Property{}
			for _, p := range recv.recDB.Properties {
				recPM[p.ID] = p
			}
			recEnv := &fEnv{
				ctx:     e.ctx,
				db:      recv.recDB,
				propMap: recPM,
				rec:     recv.rec,
				depth:   e.depth + 1,
				steps:   e.steps,
			}
			return recEnv.resolvePropValue(nameVal.asString())
		}
		return e.evalCallWithFirst(n.name, recv, n.args)

	case fCallNode:
		return e.evalCall(n.name, n.args)
	}
	return fEmptyVal(), nil
}

func (e *fEnv) evalBinary(n fBinaryNode) (fVal, error) {
	if n.op == fTokAnd {
		l, err := e.eval(n.left)
		if err != nil {
			return fVal{}, err
		}
		if !l.truthy() {
			return fBoolVal(false), nil
		}
		r, err := e.eval(n.right)
		if err != nil {
			return fVal{}, err
		}
		return fBoolVal(r.truthy()), nil
	}
	if n.op == fTokOr {
		l, err := e.eval(n.left)
		if err != nil {
			return fVal{}, err
		}
		if l.truthy() {
			return fBoolVal(true), nil
		}
		r, err := e.eval(n.right)
		if err != nil {
			return fVal{}, err
		}
		return fBoolVal(r.truthy()), nil
	}

	l, err := e.eval(n.left)
	if err != nil {
		return fVal{}, err
	}
	r, err := e.eval(n.right)
	if err != nil {
		return fVal{}, err
	}

	switch n.op {
	case fTokEq:
		return fBoolVal(fValEqual(l, r)), nil
	case fTokNeq:
		return fBoolVal(!fValEqual(l, r)), nil
	case fTokLT, fTokLTE, fTokGT, fTokGTE:
		c, err := fValCompare(l, r)
		if err != nil {
			return fVal{}, err
		}
		switch n.op {
		case fTokLT:
			return fBoolVal(c < 0), nil
		case fTokLTE:
			return fBoolVal(c <= 0), nil
		case fTokGT:
			return fBoolVal(c > 0), nil
		case fTokGTE:
			return fBoolVal(c >= 0), nil
		}
	case fTokPlus:
		if l.kind == fKindStr || r.kind == fKindStr {
			return fStrVal(l.asString() + r.asString()), nil
		}
		if l.kind == fKindList && r.kind == fKindList {
			joined := make([]fVal, 0, len(l.list)+len(r.list))
			joined = append(joined, l.list...)
			joined = append(joined, r.list...)
			return fListVal(joined), nil
		}
		ln, lok := l.asNum()
		rn, rok := r.asNum()
		if !lok && !rok {
			return fEmptyVal(), nil
		}
		return fNumVal(ln + rn), nil
	case fTokMinus, fTokMul, fTokDiv, fTokMod, fTokPow:
		ln, lok := l.asNum()
		rn, rok := r.asNum()
		if !lok && !rok {
			return fEmptyVal(), nil
		}
		switch n.op {
		case fTokMinus:
			return fNumVal(ln - rn), nil
		case fTokMul:
			return fNumVal(ln * rn), nil
		case fTokDiv:
			if rn == 0 {
				return fVal{}, errors.New("#DIV/0!")
			}
			return fNumVal(ln / rn), nil
		case fTokMod:
			if rn == 0 {
				return fVal{}, errors.New("#DIV/0!")
			}
			return fNumVal(math.Mod(ln, rn)), nil
		case fTokPow:
			return fNumVal(math.Pow(ln, rn)), nil
		}
	}
	return fEmptyVal(), nil
}

func (e *fEnv) evalCallWithFirst(name string, first fVal, rest []fNode) (fVal, error) {
	switch name {
	case "map", "filter", "find", "findIndex", "every", "some", "count":
		if len(rest) == 0 && name != "count" {
			return fVal{}, fmt.Errorf("#ARGS:%s", name)
		}
		var body fNode
		if len(rest) > 0 {
			body = rest[0]
		}
		return e.evalHigherOrder(name, first, body)
	}
	evaled := make([]fVal, 1+len(rest))
	evaled[0] = first
	for i, arg := range rest {
		v, err := e.eval(arg)
		if err != nil {
			return fVal{}, err
		}
		evaled[i+1] = v
	}
	return e.dispatchBuiltin(name, evaled)
}

func (e *fEnv) evalCall(name string, args []fNode) (fVal, error) {
	// Lazy / special forms.
	switch name {
	case "if":
		if len(args) < 2 {
			return fVal{}, errors.New("#ARGS:if")
		}
		c, err := e.eval(args[0])
		if err != nil {
			return fVal{}, err
		}
		if c.truthy() {
			return e.eval(args[1])
		}
		if len(args) >= 3 {
			return e.eval(args[2])
		}
		return fEmptyVal(), nil

	case "ifs":
		for i := 0; i+1 < len(args); i += 2 {
			c, err := e.eval(args[i])
			if err != nil {
				return fVal{}, err
			}
			if c.truthy() {
				return e.eval(args[i+1])
			}
		}
		if len(args)%2 == 1 {
			return e.eval(args[len(args)-1])
		}
		return fEmptyVal(), nil

	case "and":
		for _, a := range args {
			v, err := e.eval(a)
			if err != nil {
				return fVal{}, err
			}
			if !v.truthy() {
				return fBoolVal(false), nil
			}
		}
		return fBoolVal(true), nil

	case "or":
		for _, a := range args {
			v, err := e.eval(a)
			if err != nil {
				return fVal{}, err
			}
			if v.truthy() {
				return fBoolVal(true), nil
			}
		}
		return fBoolVal(false), nil

	case "let":
		if len(args) != 3 {
			return fVal{}, errors.New("#ARGS:let")
		}
		ident, ok := args[0].(fIdentNode)
		if !ok {
			return fVal{}, errors.New("#ARGS:let")
		}
		val, err := e.eval(args[1])
		if err != nil {
			return fVal{}, err
		}
		return e.child(map[string]fVal{ident.name: val}).eval(args[2])

	case "lets":
		if len(args) < 3 || len(args)%2 == 0 {
			return fVal{}, errors.New("#ARGS:lets")
		}
		child := e.child(map[string]fVal{})
		for i := 0; i+1 < len(args)-1; i += 2 {
			ident, ok := args[i].(fIdentNode)
			if !ok {
				return fVal{}, errors.New("#ARGS:lets")
			}
			val, err := child.eval(args[i+1])
			if err != nil {
				return fVal{}, err
			}
			child.vars[ident.name] = val
		}
		return child.eval(args[len(args)-1])

	case "map", "filter", "find", "findIndex", "every", "some", "count":
		if len(args) == 0 {
			return fVal{}, fmt.Errorf("#ARGS:%s", name)
		}
		seq, err := e.eval(args[0])
		if err != nil {
			return fVal{}, err
		}
		var body fNode
		if len(args) > 1 {
			body = args[1]
		}
		return e.evalHigherOrder(name, seq, body)
	}

	evaled := make([]fVal, len(args))
	for i, arg := range args {
		v, err := e.eval(arg)
		if err != nil {
			return fVal{}, err
		}
		evaled[i] = v
	}
	return e.dispatchBuiltin(name, evaled)
}

func (e *fEnv) evalHigherOrder(name string, seq fVal, body fNode) (fVal, error) {
	items := seq.list
	if seq.kind != fKindList {
		if seq.isEmpty() {
			items = nil
		} else {
			items = []fVal{seq}
		}
	}
	if name == "count" && body == nil {
		return fNumVal(float64(len(items))), nil
	}
	switch name {
	case "map":
		out := make([]fVal, len(items))
		for idx, it := range items {
			sub := e.child(map[string]fVal{"current": it, "index": fNumVal(float64(idx))})
			v, err := sub.eval(body)
			if err != nil {
				return fVal{}, err
			}
			out[idx] = v
		}
		return fListVal(out), nil
	case "filter":
		var out []fVal
		for idx, it := range items {
			sub := e.child(map[string]fVal{"current": it, "index": fNumVal(float64(idx))})
			v, err := sub.eval(body)
			if err != nil {
				return fVal{}, err
			}
			if v.truthy() {
				out = append(out, it)
			}
		}
		return fListVal(out), nil
	case "find":
		for idx, it := range items {
			sub := e.child(map[string]fVal{"current": it, "index": fNumVal(float64(idx))})
			v, err := sub.eval(body)
			if err != nil {
				return fVal{}, err
			}
			if v.truthy() {
				return it, nil
			}
		}
		return fEmptyVal(), nil
	case "findIndex":
		for idx, it := range items {
			sub := e.child(map[string]fVal{"current": it, "index": fNumVal(float64(idx))})
			v, err := sub.eval(body)
			if err != nil {
				return fVal{}, err
			}
			if v.truthy() {
				return fNumVal(float64(idx)), nil
			}
		}
		return fNumVal(-1), nil
	case "every":
		for idx, it := range items {
			sub := e.child(map[string]fVal{"current": it, "index": fNumVal(float64(idx))})
			v, err := sub.eval(body)
			if err != nil {
				return fVal{}, err
			}
			if !v.truthy() {
				return fBoolVal(false), nil
			}
		}
		return fBoolVal(true), nil
	case "some":
		for idx, it := range items {
			sub := e.child(map[string]fVal{"current": it, "index": fNumVal(float64(idx))})
			v, err := sub.eval(body)
			if err != nil {
				return fVal{}, err
			}
			if v.truthy() {
				return fBoolVal(true), nil
			}
		}
		return fBoolVal(false), nil
	case "count":
		cnt := 0
		for idx, it := range items {
			sub := e.child(map[string]fVal{"current": it, "index": fNumVal(float64(idx))})
			v, err := sub.eval(body)
			if err != nil {
				return fVal{}, err
			}
			if v.truthy() {
				cnt++
			}
		}
		return fNumVal(float64(cnt)), nil
	}
	return fEmptyVal(), nil
}

func (e *fEnv) nowTime() time.Time {
	if e.ctx != nil && !e.ctx.now.IsZero() {
		return e.ctx.now
	}
	return time.Now()
}

func (e *fEnv) dispatchBuiltin(name string, args []fVal) (fVal, error) {
	firstNum := func(idx int) float64 {
		if idx >= len(args) {
			return 0
		}
		n, _ := args[idx].asNum()
		return n
	}
	firstStr := func(idx int) string {
		if idx >= len(args) {
			return ""
		}
		return args[idx].asString()
	}

	switch name {
	// Property & Logic
	case "prop":
		if len(args) < 1 {
			return fEmptyVal(), nil
		}
		return e.resolvePropValue(args[0].asString())
	case "empty":
		if len(args) == 0 {
			return fBoolVal(true), nil
		}
		return fBoolVal(args[0].isEmpty()), nil
	case "not":
		if len(args) == 0 {
			return fBoolVal(true), nil
		}
		return fBoolVal(!args[0].truthy()), nil
	case "equal":
		if len(args) < 2 {
			return fBoolVal(false), nil
		}
		return fBoolVal(fValEqual(args[0], args[1])), nil
	case "unequal":
		if len(args) < 2 {
			return fBoolVal(false), nil
		}
		return fBoolVal(!fValEqual(args[0], args[1])), nil

	// Math
	case "toNumber":
		if len(args) == 0 {
			return fEmptyVal(), nil
		}
		n, ok := args[0].asNum()
		if !ok {
			return fEmptyVal(), nil
		}
		return fNumVal(n), nil
	case "abs":
		return fNumVal(math.Abs(firstNum(0))), nil
	case "ceil":
		return fNumVal(math.Ceil(firstNum(0))), nil
	case "floor":
		return fNumVal(math.Floor(firstNum(0))), nil
	case "round":
		x := firstNum(0)
		dec := 0
		if len(args) >= 2 {
			dec = int(firstNum(1))
		}
		if dec <= 0 {
			return fNumVal(math.Round(x)), nil
		}
		pow := math.Pow(10, float64(min(dec, 12)))
		return fNumVal(math.Round(x*pow) / pow), nil
	case "sqrt":
		x := firstNum(0)
		if x < 0 {
			return fVal{}, errors.New("#MATH!")
		}
		return fNumVal(math.Sqrt(x)), nil
	case "pow":
		return fNumVal(math.Pow(firstNum(0), firstNum(1))), nil
	case "exp":
		return fNumVal(math.Exp(firstNum(0))), nil
	case "ln":
		x := firstNum(0)
		if x <= 0 {
			return fVal{}, errors.New("#MATH!")
		}
		return fNumVal(math.Log(x)), nil
	case "log10":
		x := firstNum(0)
		if x <= 0 {
			return fVal{}, errors.New("#MATH!")
		}
		return fNumVal(math.Log10(x)), nil
	case "log2":
		x := firstNum(0)
		if x <= 0 {
			return fVal{}, errors.New("#MATH!")
		}
		return fNumVal(math.Log2(x)), nil
	case "mod":
		d := firstNum(1)
		if d == 0 {
			return fVal{}, errors.New("#DIV/0!")
		}
		return fNumVal(math.Mod(firstNum(0), d)), nil
	case "sign":
		x := firstNum(0)
		switch {
		case x > 0:
			return fNumVal(1), nil
		case x < 0:
			return fNumVal(-1), nil
		default:
			return fNumVal(0), nil
		}
	case "pi":
		return fNumVal(math.Pi), nil
	case "e":
		return fNumVal(math.E), nil
	case "min":
		nums := flattenNums(args)
		if len(nums) == 0 {
			return fEmptyVal(), nil
		}
		return fNumVal(slices.Min(nums)), nil
	case "max":
		nums := flattenNums(args)
		if len(nums) == 0 {
			return fEmptyVal(), nil
		}
		return fNumVal(slices.Max(nums)), nil
	case "sum":
		nums := flattenNums(args)
		total := 0.0
		for _, v := range nums {
			total += v
		}
		return fNumVal(total), nil
	case "mean", "average":
		nums := flattenNums(args)
		if len(nums) == 0 {
			return fEmptyVal(), nil
		}
		total := 0.0
		for _, v := range nums {
			total += v
		}
		return fNumVal(total / float64(len(nums))), nil
	case "median":
		nums := flattenNums(args)
		if len(nums) == 0 {
			return fEmptyVal(), nil
		}
		slices.Sort(nums)
		m := len(nums) / 2
		if len(nums)%2 == 1 {
			return fNumVal(nums[m]), nil
		}
		return fNumVal((nums[m-1] + nums[m]) / 2), nil

	// Strings
	case "format":
		return fStrVal(firstStr(0)), nil
	case "concat":
		if len(args) > 0 && args[0].kind == fKindList {
			var out []fVal
			for _, a := range args {
				if a.kind == fKindList {
					out = append(out, a.list...)
				} else {
					out = append(out, a)
				}
			}
			return fListVal(out), nil
		}
		var b strings.Builder
		for _, a := range args {
			b.WriteString(a.asString())
		}
		return fStrVal(b.String()), nil
	case "join":
		sep := ""
		if len(args) >= 2 {
			sep = args[1].asString()
		}
		if len(args) == 0 {
			return fStrVal(""), nil
		}
		if args[0].kind == fKindList {
			parts := make([]string, len(args[0].list))
			for i, it := range args[0].list {
				parts[i] = it.asString()
			}
			return fStrVal(strings.Join(parts, sep)), nil
		}
		return fStrVal(args[0].asString()), nil
	case "split":
		parts := strings.Split(firstStr(0), firstStr(1))
		out := make([]fVal, len(parts))
		for i, p := range parts {
			out[i] = fStrVal(p)
		}
		return fListVal(out), nil
	case "length":
		if len(args) == 0 {
			return fNumVal(0), nil
		}
		if args[0].kind == fKindList {
			return fNumVal(float64(len(args[0].list))), nil
		}
		return fNumVal(float64(utf8.RuneCountInString(args[0].asString()))), nil
	case "contains":
		if len(args) < 2 {
			return fBoolVal(false), nil
		}
		if args[0].kind == fKindList {
			for _, el := range args[0].list {
				if fValEqual(el, args[1]) {
					return fBoolVal(true), nil
				}
			}
			return fBoolVal(false), nil
		}
		return fBoolVal(strings.Contains(args[0].asString(), args[1].asString())), nil
	case "test":
		re, err := regexp.Compile(firstStr(1))
		if err != nil {
			return fVal{}, errors.New("#REGEX!")
		}
		return fBoolVal(re.MatchString(firstStr(0))), nil
	case "match":
		re, err := regexp.Compile(firstStr(1))
		if err != nil {
			return fVal{}, errors.New("#REGEX!")
		}
		hits := re.FindAllString(firstStr(0), -1)
		out := make([]fVal, len(hits))
		for i, h := range hits {
			out[i] = fStrVal(h)
		}
		return fListVal(out), nil
	case "replace":
		re, err := regexp.Compile(firstStr(1))
		if err != nil {
			return fVal{}, errors.New("#REGEX!")
		}
		s := firstStr(0)
		rep := firstStr(2)
		loc := re.FindStringIndex(s)
		if loc == nil {
			return fStrVal(s), nil
		}
		replaced := re.ReplaceAllString(s[:loc[1]], rep) + s[loc[1]:]
		return fStrVal(replaced), nil
	case "replaceAll":
		re, err := regexp.Compile(firstStr(1))
		if err != nil {
			return fVal{}, errors.New("#REGEX!")
		}
		return fStrVal(re.ReplaceAllString(firstStr(0), firstStr(2))), nil
	case "slice", "substring":
		if len(args) > 0 && args[0].kind == fKindList {
			start := int(firstNum(1))
			end := len(args[0].list)
			if len(args) >= 3 {
				end = int(firstNum(2))
			}
			s, eIdx := clampSliceBounds(len(args[0].list), start, end)
			return fListVal(slices.Clone(args[0].list[s:eIdx])), nil
		}
		runes := []rune(firstStr(0))
		start := int(firstNum(1))
		end := len(runes)
		if len(args) >= 3 {
			end = int(firstNum(2))
		}
		s, eIdx := clampSliceBounds(len(runes), start, end)
		return fStrVal(string(runes[s:eIdx])), nil
	case "lower":
		return fStrVal(strings.ToLower(firstStr(0))), nil
	case "upper":
		return fStrVal(strings.ToUpper(firstStr(0))), nil
	case "trim":
		return fStrVal(strings.TrimSpace(firstStr(0))), nil
	case "trimStart":
		return fStrVal(strings.TrimLeftFunc(firstStr(0), unicode.IsSpace)), nil
	case "trimEnd":
		return fStrVal(strings.TrimRightFunc(firstStr(0), unicode.IsSpace)), nil
	case "startsWith":
		return fBoolVal(strings.HasPrefix(firstStr(0), firstStr(1))), nil
	case "endsWith":
		return fBoolVal(strings.HasSuffix(firstStr(0), firstStr(1))), nil
	case "repeat":
		cnt := int(firstNum(1))
		if cnt <= 0 {
			return fStrVal(""), nil
		}
		if cnt > 4096 {
			cnt = 4096
		}
		return fStrVal(strings.Repeat(firstStr(0), cnt)), nil
	case "padStart":
		return fStrVal(padRunes(firstStr(0), int(firstNum(1)), firstStr(2), true)), nil
	case "padEnd":
		return fStrVal(padRunes(firstStr(0), int(firstNum(1)), firstStr(2), false)), nil

	// Dates
	case "now":
		t := e.nowTime()
		return fDateVal(DateValue{Start: t.Format("2006-01-02T15:04"), IncludeTime: true}), nil
	case "today":
		t := e.nowTime()
		return fDateVal(DateValue{Start: t.Format("2006-01-02"), IncludeTime: false}), nil
	case "parseDate":
		if len(args) == 0 {
			return fEmptyVal(), nil
		}
		_, dv, ok := args[0].asDate(e.nowTime().Location())
		if !ok {
			return fEmptyVal(), nil
		}
		return fDateVal(dv), nil
	case "fromTimestamp":
		ms := int64(firstNum(0))
		t := time.UnixMilli(ms).In(e.nowTime().Location())
		return fDateVal(DateValue{Start: t.Format("2006-01-02T15:04"), IncludeTime: true}), nil
	case "timestamp":
		if len(args) == 0 {
			return fEmptyVal(), nil
		}
		t, _, ok := args[0].asDate(e.nowTime().Location())
		if !ok {
			return fEmptyVal(), nil
		}
		return fNumVal(float64(t.UnixMilli())), nil
	case "dateStart":
		if len(args) == 0 || args[0].isEmpty() {
			return fEmptyVal(), nil
		}
		t, dv, ok := args[0].asDate(e.nowTime().Location())
		if !ok {
			return fEmptyVal(), nil
		}
		_ = t
		return fDateVal(DateValue{Start: dv.Start, IncludeTime: dv.IncludeTime, TimeZone: dv.TimeZone}), nil
	case "dateEnd":
		if len(args) == 0 || args[0].isEmpty() {
			return fEmptyVal(), nil
		}
		if args[0].kind == fKindDate && args[0].date.End != "" {
			return fDateVal(DateValue{Start: args[0].date.End, IncludeTime: args[0].date.IncludeTime, TimeZone: args[0].date.TimeZone}), nil
		}
		return args[0], nil
	case "dateRange":
		if len(args) < 2 {
			return fEmptyVal(), nil
		}
		_, d1, ok1 := args[0].asDate(e.nowTime().Location())
		_, d2, ok2 := args[1].asDate(e.nowTime().Location())
		if !ok1 || !ok2 {
			return fEmptyVal(), nil
		}
		return fDateVal(DateValue{
			Start:       d1.Start,
			End:         d2.Start,
			IncludeTime: d1.IncludeTime || d2.IncludeTime,
		}), nil
	case "dateAdd":
		return formulaDateShift(args, e.nowTime().Location(), 1)
	case "dateSubtract":
		return formulaDateShift(args, e.nowTime().Location(), -1)
	case "dateBetween":
		return formulaDateBetween(args, e.nowTime().Location())
	case "formatDate":
		if len(args) == 0 {
			return fStrVal(""), nil
		}
		t, _, ok := args[0].asDate(e.nowTime().Location())
		if !ok {
			return fStrVal(""), nil
		}
		layout := "2006-01-02"
		if len(args) >= 2 && args[1].asString() != "" {
			layout = translateDateFormatTokens(args[1].asString())
		}
		return fStrVal(t.Format(layout)), nil
	case "year":
		t, _, ok := argsDate0(args, e.nowTime().Location())
		if !ok {
			return fEmptyVal(), nil
		}
		return fNumVal(float64(t.Year())), nil
	case "month":
		t, _, ok := argsDate0(args, e.nowTime().Location())
		if !ok {
			return fEmptyVal(), nil
		}
		return fNumVal(float64(t.Month())), nil
	case "date":
		t, _, ok := argsDate0(args, e.nowTime().Location())
		if !ok {
			return fEmptyVal(), nil
		}
		return fNumVal(float64(t.Day())), nil
	case "day":
		t, _, ok := argsDate0(args, e.nowTime().Location())
		if !ok {
			return fEmptyVal(), nil
		}
		return fNumVal(float64(t.Weekday())), nil
	case "hour":
		t, _, ok := argsDate0(args, e.nowTime().Location())
		if !ok {
			return fEmptyVal(), nil
		}
		return fNumVal(float64(t.Hour())), nil
	case "minute":
		t, _, ok := argsDate0(args, e.nowTime().Location())
		if !ok {
			return fEmptyVal(), nil
		}
		return fNumVal(float64(t.Minute())), nil
	case "week":
		t, _, ok := argsDate0(args, e.nowTime().Location())
		if !ok {
			return fEmptyVal(), nil
		}
		_, wk := t.ISOWeek()
		return fNumVal(float64(wk)), nil

	// Lists
	case "at":
		if len(args) < 2 || args[0].kind != fKindList {
			return fEmptyVal(), nil
		}
		idx := int(firstNum(1))
		if idx < 0 {
			idx = len(args[0].list) + idx
		}
		if idx >= 0 && idx < len(args[0].list) {
			return args[0].list[idx], nil
		}
		return fEmptyVal(), nil
	case "first":
		if len(args) == 0 {
			return fEmptyVal(), nil
		}
		if args[0].kind == fKindList {
			if len(args[0].list) == 0 {
				return fEmptyVal(), nil
			}
			return args[0].list[0], nil
		}
		return args[0], nil
	case "last":
		if len(args) == 0 {
			return fEmptyVal(), nil
		}
		if args[0].kind == fKindList {
			if len(args[0].list) == 0 {
				return fEmptyVal(), nil
			}
			return args[0].list[len(args[0].list)-1], nil
		}
		return args[0], nil
	case "reverse":
		if len(args) == 0 {
			return fListVal(nil), nil
		}
		if args[0].kind == fKindList {
			out := slices.Clone(args[0].list)
			slices.Reverse(out)
			return fListVal(out), nil
		}
		runes := []rune(args[0].asString())
		slices.Reverse(runes)
		return fStrVal(string(runes)), nil
	case "sort":
		if len(args) == 0 || args[0].kind != fKindList {
			return fListVal(nil), nil
		}
		out := slices.Clone(args[0].list)
		slices.SortStableFunc(out, func(a, b fVal) int {
			c, _ := fValCompare(a, b)
			return c
		})
		return fListVal(out), nil
	case "unique":
		if len(args) == 0 || args[0].kind != fKindList {
			return fListVal(nil), nil
		}
		var out []fVal
		for _, el := range args[0].list {
			seen := false
			for _, ex := range out {
				if fValEqual(el, ex) {
					seen = true
					break
				}
			}
			if !seen {
				out = append(out, el)
			}
		}
		return fListVal(out), nil
	case "flat":
		if len(args) == 0 || args[0].kind != fKindList {
			return fListVal(nil), nil
		}
		var out []fVal
		for _, el := range args[0].list {
			if el.kind == fKindList {
				out = append(out, el.list...)
			} else {
				out = append(out, el)
			}
		}
		return fListVal(out), nil
	case "includes":
		if len(args) < 2 {
			return fBoolVal(false), nil
		}
		if args[0].kind == fKindList {
			for _, el := range args[0].list {
				if fValEqual(el, args[1]) {
					return fBoolVal(true), nil
				}
			}
			return fBoolVal(false), nil
		}
		return fBoolVal(strings.Contains(args[0].asString(), args[1].asString())), nil
	}

	return fVal{}, fmt.Errorf("#FN:%s", name)
}

func flattenNums(args []fVal) []float64 {
	var out []float64
	var walk func(fVal)
	walk = func(v fVal) {
		switch v.kind {
		case fKindList:
			for _, el := range v.list {
				walk(el)
			}
		default:
			if n, ok := v.asNum(); ok {
				out = append(out, n)
			}
		}
	}
	for _, a := range args {
		walk(a)
	}
	return out
}

func clampSliceBounds(n, start, end int) (int, int) {
	if start < 0 {
		start += n
	}
	if end < 0 {
		end += n
	}
	start = min(max(start, 0), n)
	end = min(max(end, start), n)
	return start, end
}

func padRunes(s string, width int, pad string, left bool) string {
	if pad == "" {
		pad = " "
	}
	runes := []rune(s)
	if len(runes) >= width {
		return s
	}
	need := width - len(runes)
	padRunes := []rune(pad)
	var extra strings.Builder
	for i := 0; i < need; i++ {
		extra.WriteRune(padRunes[i%len(padRunes)])
	}
	if left {
		return extra.String() + s
	}
	return s + extra.String()
}

func argsDate0(args []fVal, loc *time.Location) (time.Time, DateValue, bool) {
	if len(args) == 0 {
		return time.Time{}, DateValue{}, false
	}
	return args[0].asDate(loc)
}

func formulaDateShift(args []fVal, loc *time.Location, dir int) (fVal, error) {
	if len(args) < 3 {
		return fEmptyVal(), nil
	}
	t, dv, ok := args[0].asDate(loc)
	if !ok {
		return fEmptyVal(), nil
	}
	amountF, _ := args[1].asNum()
	amount := int(amountF) * dir
	unit := strings.ToLower(strings.TrimSpace(args[2].asString()))
	incTime := dv.IncludeTime
	switch unit {
	case "years", "year":
		t = t.AddDate(amount, 0, 0)
	case "quarters", "quarter":
		t = t.AddDate(0, amount*3, 0)
	case "months", "month":
		t = t.AddDate(0, amount, 0)
	case "weeks", "week":
		t = t.AddDate(0, 0, amount*7)
	case "days", "day":
		t = t.AddDate(0, 0, amount)
	case "hours", "hour":
		t = t.Add(time.Duration(amount) * time.Hour)
		incTime = true
	case "minutes", "minute":
		t = t.Add(time.Duration(amount) * time.Minute)
		incTime = true
	case "seconds", "second":
		t = t.Add(time.Duration(amount) * time.Second)
		incTime = true
	}
	layout := "2006-01-02"
	if incTime {
		layout = "2006-01-02T15:04"
	}
	return fDateVal(DateValue{Start: t.Format(layout), IncludeTime: incTime, TimeZone: dv.TimeZone}), nil
}

func formulaDateBetween(args []fVal, loc *time.Location) (fVal, error) {
	if len(args) < 3 {
		return fEmptyVal(), nil
	}
	t1, _, ok1 := args[0].asDate(loc)
	t2, _, ok2 := args[1].asDate(loc)
	if !ok1 || !ok2 {
		return fEmptyVal(), nil
	}
	diff := t1.Sub(t2)
	unit := strings.ToLower(strings.TrimSpace(args[2].asString()))
	switch unit {
	case "years", "year":
		return fNumVal(float64(t1.Year() - t2.Year())), nil
	case "months", "month":
		return fNumVal(float64((t1.Year()-t2.Year())*12 + int(t1.Month()-t2.Month()))), nil
	case "weeks", "week":
		return fNumVal(math.Trunc(diff.Hours() / (24 * 7))), nil
	case "days", "day":
		return fNumVal(math.Trunc(diff.Hours() / 24)), nil
	case "hours", "hour":
		return fNumVal(math.Trunc(diff.Hours())), nil
	case "minutes", "minute":
		return fNumVal(math.Trunc(diff.Minutes())), nil
	case "seconds", "second":
		return fNumVal(math.Trunc(diff.Seconds())), nil
	default:
		return fNumVal(math.Trunc(diff.Hours() / 24)), nil
	}
}

func translateDateFormatTokens(fmtStr string) string {
	// Support both standard Moment-style tokens and native Go reference layouts.
	if strings.Contains(fmtStr, "2006") || strings.Contains(fmtStr, "15:04") {
		return fmtStr
	}
	rep := strings.NewReplacer(
		"YYYY", "2006",
		"YY", "06",
		"MMMM", "January",
		"MMM", "Jan",
		"MM", "01",
		"DD", "02",
		"HH", "15",
		"mm", "04",
		"ss", "05",
		"dddd", "Monday",
		"ddd", "Mon",
	)
	return rep.Replace(fmtStr)
}
