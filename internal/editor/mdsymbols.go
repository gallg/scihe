package editor

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// Writing scientific text: super- and subscripts typed as Unicode
// characters (x², H₂O), formulas opened around a word or as a block,
// and a picker of Greek letters and mathematical symbols (also in
// insert mode with <C-k>). The rendered view shows all of them in the
// size of the text.

// mdScript raises (sup) or lowers the text under the cursor: the
// selection, else the number (with its sign) or the character under
// the cursor. Text already raised (lowered) goes back to normal, text
// lowered (raised) turns the other way; characters with no Unicode
// form are wrapped in <sup>…</sup> or <sub>…</sub>, and such a span is
// unwrapped.
func (e *Editor) mdScript(c *ctx, sup bool) {
	b := c.b
	if !e.mdEditable(b) {
		return
	}
	var from, to Pos
	if e.mode.visual() {
		r := e.visualRange()
		e.exitVisual()
		if r.block || r.linewise || r.start.Line != r.end.Line {
			e.errorf("select text on one line")
			return
		}
		from, to = r.start, r.end
	} else {
		l, col := c.w.cur.Line, c.w.cur.Col
		line := b.Line(l)
		if col >= len(line) {
			e.errorf("no text under the cursor")
			return
		}
		if s, en, inner, ok := htmlScriptAt(string(line), col); ok { // unwrap <sup>x</sup>
			b.Replace(Pos{l, s}, Pos{l, en}, inner)
			c.w.cur = Pos{l, s}
			return
		}
		s, en := scriptRun(line, col)
		from, to = Pos{l, s}, Pos{l, en}
	}
	b.Replace(from, to, toggleScript(b.Slice(from, to), sup))
	c.w.cur = from
}

// scriptRun returns the run of the text at col a script command acts
// on: script characters (to turn back), a number with its sign, or the
// character itself.
func scriptRun(line []rune, col int) (int, int) {
	in := func(i int, f func(rune) bool) bool { return i >= 0 && i < len(line) && f(line[i]) }
	isScript := func(r rune) bool { return fScriptChars[r][0] != 0 }
	isNum := func(r rune) bool { return r >= '0' && r <= '9' }
	var f func(rune) bool
	switch {
	case isScript(line[col]):
		f = isScript
	case isNum(line[col]) || strings.ContainsRune("+-", line[col]) && in(col+1, isNum):
		f = isNum
	default:
		return col, col + 1
	}
	s, en := col, col+1
	for in(s-1, f) {
		s--
	}
	if f(line[s]) && in(s-1, func(r rune) bool { return r == '-' || r == '+' }) && !isScript(line[s]) {
		s-- // the sign of the exponent
	}
	for in(en, f) {
		en++
	}
	return s, en
}

// toggleScript turns s into super- or subscript characters, back to
// normal ones when s already is, or wraps it in an HTML tag.
func toggleScript(s string, sup bool) string {
	m, tag, kind := subscripts, "sub", '_'
	if sup {
		m, tag, kind = superscripts, "sup", '^'
	}
	same, other := s != "", s != "" // all raised (lowered) as asked, all the other way
	for _, r := range s {
		c := fScriptChars[r][0]
		same = same && c == kind
		other = other && c != 0 && c != kind
	}
	var sb strings.Builder
	if same || other { // back to normal characters, then the other way
		for _, r := range s {
			sb.WriteRune(fScriptChars[r][1])
		}
		if same {
			return sb.String()
		}
		s = sb.String()
		sb.Reset()
	}
	for _, r := range s {
		c, ok := m[r]
		if !ok {
			return "<" + tag + ">" + s + "</" + tag + ">"
		}
		sb.WriteRune(c)
	}
	return sb.String()
}

var mdHTMLScriptRe = regexp.MustCompile(`<(sup|sub)>(.*?)</(?:sup|sub)>`)

// htmlScriptAt finds a <sup>…</sup> or <sub>…</sub> holding rune
// column col: its rune span and its text.
func htmlScriptAt(line string, col int) (int, int, string, bool) {
	for _, m := range mdHTMLScriptRe.FindAllStringSubmatchIndex(line, -1) {
		s, en := len([]rune(line[:m[0]])), len([]rune(line[:m[1]]))
		if col >= s && col < en {
			return s, en, line[m[4]:m[5]], true
		}
	}
	return 0, 0, "", false
}

// mdFormula puts the selection or the word under the cursor between
// dollars; with neither, a pair is inserted and insert mode starts
// between them. display opens a $$ block below the line (around the
// lines of a linewise selection).
func (e *Editor) mdFormula(c *ctx, display bool) {
	b, w := c.b, c.w
	if !e.mdEditable(b) {
		return
	}
	if display {
		if e.mode.visual() {
			from, to := e.selectedLines(c)
			b.Insert(Pos{to, b.LineLen(to)}, "\n$$")
			b.Insert(Pos{from, 0}, "$$\n")
			w.cur = Pos{from, 0}
			return
		}
		l := w.cur.Line
		b.Insert(Pos{l, b.LineLen(l)}, "\n$$\n\n$$")
		w.cur = Pos{l + 2, 0}
		e.enterInsert()
		return
	}
	if e.mode.visual() {
		r := e.visualRange()
		e.exitVisual()
		if r.block || r.linewise || r.start.Line != r.end.Line {
			e.errorf("select text on one line")
			return
		}
		b.Insert(r.end, "$")
		b.Insert(r.start, "$")
		w.cur = r.start
		return
	}
	l := w.cur.Line
	s, en := wordBounds(b.Line(l), w.cur.Col)
	if s == en {
		p := b.Insert(w.cur, "$$")
		w.cur = Pos{p.Line, p.Col - 1}
		e.enterInsert()
		return
	}
	b.Insert(Pos{l, en}, "$")
	b.Insert(Pos{l, s}, "$")
	w.cur = Pos{l, s}
}

// mdSymbolItems are the entries of the symbol picker: a symbol and its
// names as written in formulas, Greek first.
var mdSymbolItems = func() []pickItem {
	names := map[string][]string{}
	add := func(sym, name string) {
		for _, n := range names[sym] {
			if n == name {
				return
			}
		}
		names[sym] = append(names[sym], name)
	}
	for n, s := range mathSymbols {
		r := []rune(s)
		if s == n || len(r) != 1 || r[0] < 128 {
			continue // function names and ASCII escapes
		}
		add(s, n)
	}
	for n, s := range fAliases {
		if r := []rune(s); len(r) == 1 && r[0] >= 128 {
			add(s, n)
		}
	}
	for n, s := range fBigOps {
		add(s, n)
	}
	for r, s := range superscripts {
		if r != s {
			add(string(s), "superscript "+string(r))
		}
	}
	for r, s := range subscripts {
		if r != s {
			add(string(s), "subscript "+string(r))
		}
	}
	for _, l := range "CHNPQRZ" {
		if c, ok := mathbb(l); ok {
			add(string(c), "set "+string(l))
		}
	}
	syms := make([]string, 0, len(names))
	for s := range names {
		syms = append(syms, s)
	}
	greek := func(s string) bool { return unicode.Is(unicode.Greek, []rune(s)[0]) }
	sort.Slice(syms, func(i, j int) bool {
		if gi, gj := greek(syms[i]), greek(syms[j]); gi != gj {
			return gi
		}
		return syms[i] < syms[j]
	})
	items := make([]pickItem, len(syms))
	for i, s := range syms {
		sort.Strings(names[s])
		items[i] = pickItem{label: s + "  " + strings.Join(names[s], " "), data: s}
	}
	return items
}()

// pickSymbol inserts a symbol chosen by name at the cursor (and stays
// in insert mode when called from it).
func (e *Editor) pickSymbol() {
	w := e.win
	b := w.buf
	if b.ReadOnly {
		e.errorf("E21: Cannot make changes, buffer is read-only")
		return
	}
	insert := e.mode == ModeInsert || e.mode == ModeReplace
	p := &picker{title: "Symbols", all: mdSymbolItems}
	p.onSelect = func(it pickItem) {
		if !e.bufferAlive(b) || w.buf != b {
			return
		}
		at := b.Clamp(w.cur)
		end := b.Insert(at, it.data.(string))
		if insert {
			w.cur = end
			if e.mode == ModeInsert || e.mode == ModeReplace {
				e.recordInsertKey(Key("\x00paste:" + it.data.(string))) // . repeats it
			}
		} else {
			w.cur = at
		}
	}
	e.openPicker(p)
}
