package editor

import (
	"strings"
	"unicode"

	"scihe/internal/buffer"
)

type Pos = buffer.Pos

// character classes for word motions
const (
	clsSpace = iota
	clsPunct
	clsWord
)

func charClass(r rune, bigWord bool) int {
	if r == ' ' || r == '\t' || r == '\n' || r == 0 {
		return clsSpace
	}
	if bigWord {
		return clsWord
	}
	if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
		return clsWord
	}
	return clsPunct
}

func isBlankLine(b *buffer.Buffer, l int) bool {
	return strings.TrimSpace(b.LineString(l)) == ""
}

func firstNonBlank(b *buffer.Buffer, l int) int {
	line := b.Line(l)
	for i, r := range line {
		if r != ' ' && r != '\t' {
			return i
		}
	}
	if len(line) == 0 {
		return 0
	}
	return len(line) - 1
}

func lastCol(b *buffer.Buffer, l int) int {
	if n := b.LineLen(l); n > 0 {
		return n - 1
	}
	return 0
}

// ---- word motions ----

// nextWordStart implements "w"/"W" once from p.
func nextWordStart(b *buffer.Buffer, p Pos, big bool) Pos {
	cls := charClass(b.RuneAt(p), big)
	startLine := p.Line
	// skip current word
	if cls != clsSpace {
		for {
			np, ok := b.Next(p)
			if !ok {
				return b.End()
			}
			p = np
			if charClass(b.RuneAt(p), big) != cls || p.Line != startLine {
				break
			}
		}
	}
	// skip whitespace; an empty line counts as a word
	for {
		r := b.RuneAt(p)
		if r == 0 {
			return p
		}
		if charClass(r, big) != clsSpace {
			return p
		}
		if p.Col == 0 && b.LineLen(p.Line) == 0 && p.Line != startLine {
			return p
		}
		np, ok := b.Next(p)
		if !ok {
			return p
		}
		p = np
	}
}

// wordEnd implements "e"/"E" once from p.
func wordEnd(b *buffer.Buffer, p Pos, big bool) Pos {
	np, ok := b.Next(p)
	if !ok {
		return p
	}
	p = np
	for charClass(b.RuneAt(p), big) == clsSpace {
		np, ok := b.Next(p)
		if !ok {
			return p
		}
		p = np
	}
	cls := charClass(b.RuneAt(p), big)
	for {
		np, ok := b.Next(p)
		if !ok || np.Line != p.Line || charClass(b.RuneAt(np), big) != cls {
			return p
		}
		p = np
	}
}

// prevWordStart implements "b"/"B" once from p.
func prevWordStart(b *buffer.Buffer, p Pos, big bool) Pos {
	pp, ok := b.Prev(p)
	if !ok {
		return p
	}
	p = pp
	for charClass(b.RuneAt(p), big) == clsSpace {
		if p.Col == 0 && b.LineLen(p.Line) == 0 {
			return p
		}
		pp, ok := b.Prev(p)
		if !ok {
			return p
		}
		p = pp
	}
	cls := charClass(b.RuneAt(p), big)
	for p.Col > 0 {
		pp := Pos{p.Line, p.Col - 1}
		if charClass(b.RuneAt(pp), big) != cls {
			break
		}
		p = pp
	}
	return p
}

// prevWordEnd implements "ge"/"gE" once from p.
func prevWordEnd(b *buffer.Buffer, p Pos, big bool) Pos {
	cls := charClass(b.RuneAt(p), big)
	// leave current word
	for {
		pp, ok := b.Prev(p)
		if !ok {
			return p
		}
		p = pp
		c := charClass(b.RuneAt(p), big)
		if c != cls || c == clsSpace {
			break
		}
	}
	for charClass(b.RuneAt(p), big) == clsSpace {
		if p.Col == 0 && b.LineLen(p.Line) == 0 {
			return p
		}
		pp, ok := b.Prev(p)
		if !ok {
			return p
		}
		p = pp
	}
	return p
}

func repeatPos(c *ctx, p Pos, f func(Pos) Pos) Pos {
	for i := 0; i < c.count; i++ {
		np := f(p)
		if np == p {
			break
		}
		p = np
	}
	return p
}

func mWord(big bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		b := c.b
		cur := c.w.cur
		// "cw" on a non-blank behaves like "ce"
		if c.op == OpChange && charClass(b.RuneAt(cur), big) != clsSpace {
			p := cur
			for i := 0; i < c.count; i++ {
				// stay on the current word end for the first count when inside a word
				if i == 0 {
					np, ok := b.Next(p)
					if !ok || np.Line != p.Line || charClass(b.RuneAt(np), big) != charClass(b.RuneAt(p), big) {
						continue
					}
				}
				p = wordEnd(b, p, big)
			}
			return p, Inclusive, true
		}
		p := repeatPos(c, cur, func(p Pos) Pos { return nextWordStart(b, p, big) })
		if c.op != OpNone && p.Line > cur.Line {
			// the operator stops at the end of the last word's line
			midLine := p.Col > firstNonBlank(b, p.Line) || (p.Line == b.LineCount()-1 && p.Col >= b.LineLen(p.Line))
			if !midLine {
				last := max(p.Line-1, cur.Line)
				p = Pos{last, b.LineLen(last)}
			}
		}
		return p, Exclusive, true
	}
}

func mWordEnd(big bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		return repeatPos(c, c.w.cur, func(p Pos) Pos { return wordEnd(c.b, p, big) }), Inclusive, true
	}
}

func mWordBack(big bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		return repeatPos(c, c.w.cur, func(p Pos) Pos { return prevWordStart(c.b, p, big) }), Exclusive, true
	}
}

func mWordEndBack(big bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		return repeatPos(c, c.w.cur, func(p Pos) Pos { return prevWordEnd(c.b, p, big) }), Inclusive, true
	}
}

// ---- character and line motions ----

func mLeft(wrap bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		p := c.w.cur
		for i := 0; i < c.count; i++ {
			if p.Col > 0 {
				p.Col--
			} else if wrap && p.Line > 0 {
				p.Line--
				p.Col = lastCol(c.b, p.Line)
			} else {
				break
			}
		}
		return p, Exclusive, p != c.w.cur || c.op != OpNone
	}
}

func mRight(wrap bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		p := c.w.cur
		n := c.b.LineLen(p.Line)
		limit := n - 1
		if c.op != OpNone || c.e.mode.visual() {
			limit = n // operators may cover the last char
		}
		for i := 0; i < c.count; i++ {
			if p.Col < limit {
				p.Col++
			} else if wrap && p.Line < c.b.LineCount()-1 {
				p.Line++
				p.Col = 0
			} else {
				break
			}
		}
		if limit < 0 {
			p.Col = 0
		}
		return p, Exclusive, true
	}
}

func mDown(c *ctx) (Pos, MotionType, bool) {
	l := c.w.cur.Line + c.count
	if l >= c.b.LineCount() {
		if c.w.cur.Line == c.b.LineCount()-1 && c.op == OpNone {
			return c.w.cur, Linewise, false
		}
		l = c.b.LineCount() - 1
	}
	return c.e.wantPos(c.w, l), Linewise, true
}

func mUp(c *ctx) (Pos, MotionType, bool) {
	l := c.w.cur.Line - c.count
	if l < 0 {
		if c.w.cur.Line == 0 && c.op == OpNone {
			return c.w.cur, Linewise, false
		}
		l = 0
	}
	return c.e.wantPos(c.w, l), Linewise, true
}

// wantPos returns the position on line l closest to the window's desired column.
func (e *Editor) wantPos(w *Window, l int) Pos {
	line := w.buf.Line(l)
	if w.want < 0 {
		return Pos{l, max(len(line)-1, 0)}
	}
	col := colForDisplay(line, w.want, e.options(w.buf).TabStop)
	return Pos{l, col}
}

func mLineStart(c *ctx) (Pos, MotionType, bool) {
	return Pos{c.w.cur.Line, 0}, Exclusive, true
}

func mFirstNonBlank(c *ctx) (Pos, MotionType, bool) {
	return Pos{c.w.cur.Line, firstNonBlank(c.b, c.w.cur.Line)}, Exclusive, true
}

func mLineEnd(c *ctx) (Pos, MotionType, bool) {
	l := min(c.w.cur.Line+c.count-1, c.b.LineCount()-1)
	c.w.want = -1
	col := lastCol(c.b, l)
	if c.e.mode.visual() || c.op != OpNone {
		col = max(c.b.LineLen(l)-1, 0)
	}
	return Pos{l, col}, Inclusive, true
}

func mLastNonBlank(c *ctx) (Pos, MotionType, bool) {
	l := min(c.w.cur.Line+c.count-1, c.b.LineCount()-1)
	line := c.b.Line(l)
	col := len(line) - 1
	for col > 0 && (line[col] == ' ' || line[col] == '\t') {
		col--
	}
	return Pos{l, max(col, 0)}, Inclusive, true
}

func mColumn(c *ctx) (Pos, MotionType, bool) {
	line := c.b.Line(c.w.cur.Line)
	col := colForDisplay(line, c.count-1, c.e.options(c.b).TabStop)
	return Pos{c.w.cur.Line, min(col, max(len(line)-1, 0))}, Exclusive, true
}

func mLineDownFirst(c *ctx) (Pos, MotionType, bool) {
	l := min(c.w.cur.Line+c.count, c.b.LineCount()-1)
	return Pos{l, firstNonBlank(c.b, l)}, Linewise, true
}

func mLineUpFirst(c *ctx) (Pos, MotionType, bool) {
	l := max(c.w.cur.Line-c.count, 0)
	return Pos{l, firstNonBlank(c.b, l)}, Linewise, true
}

func mCurLineFirst(c *ctx) (Pos, MotionType, bool) {
	l := min(c.w.cur.Line+c.count-1, c.b.LineCount()-1)
	return Pos{l, firstNonBlank(c.b, l)}, Linewise, true
}

func mGotoFirst(c *ctx) (Pos, MotionType, bool) {
	l := 0
	if c.hasCount {
		l = min(c.count-1, c.b.LineCount()-1)
	}
	return Pos{l, firstNonBlank(c.b, l)}, Linewise, true
}

func mGotoLast(c *ctx) (Pos, MotionType, bool) {
	l := c.b.LineCount() - 1
	if c.hasCount {
		l = min(c.count-1, l)
	}
	return Pos{l, firstNonBlank(c.b, l)}, Linewise, true
}

func mScreenTop(c *ctx) (Pos, MotionType, bool) {
	l := c.w.top + c.count - 1
	so := min(c.e.options(c.b).ScrollOff, c.w.textHeight()/2)
	if c.w.top > 0 && c.count-1 < so {
		l = c.w.top + so
	}
	l = min(l, c.b.LineCount()-1)
	return Pos{l, firstNonBlank(c.b, l)}, Linewise, true
}

func mScreenMiddle(c *ctx) (Pos, MotionType, bool) {
	last := c.e.lastVisibleLine(c.w)
	l := (c.w.top + last) / 2
	return Pos{l, firstNonBlank(c.b, l)}, Linewise, true
}

func mScreenBottom(c *ctx) (Pos, MotionType, bool) {
	last := c.e.lastVisibleLine(c.w)
	l := max(last-(c.count-1), c.w.top)
	return Pos{l, firstNonBlank(c.b, l)}, Linewise, true
}

// ---- find char ----

type findState struct {
	ch      rune
	forward bool
	till    bool
}

func (e *Editor) findChar(c *ctx, fs findState, repeat bool) (Pos, MotionType, bool) {
	line := c.b.Line(c.w.cur.Line)
	col := c.w.cur.Col
	n := c.count
	for i := 0; i < n; i++ {
		found := -1
		if fs.forward {
			start := col + 1
			if fs.till && repeat && i == 0 && start < len(line) && line[start] == fs.ch {
				start++
			}
			for j := start; j < len(line); j++ {
				if line[j] == fs.ch {
					found = j
					break
				}
			}
		} else {
			start := col - 1
			if fs.till && repeat && i == 0 && start >= 0 && line[start] == fs.ch {
				start--
			}
			for j := start; j >= 0; j-- {
				if line[j] == fs.ch {
					found = j
					break
				}
			}
		}
		if found < 0 {
			return c.w.cur, Exclusive, false
		}
		col = found
	}
	if fs.till {
		if fs.forward {
			col--
		} else {
			col++
		}
	}
	mt := Exclusive
	if fs.forward {
		mt = Inclusive
	}
	return Pos{c.w.cur.Line, col}, mt, true
}

func mFind(forward, till bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		fs := findState{c.char, forward, till}
		c.e.lastFind = fs
		return c.e.findChar(c, fs, false)
	}
}

func mRepeatFind(reverse bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		fs := c.e.lastFind
		if fs.ch == 0 {
			return c.w.cur, Exclusive, false
		}
		if reverse {
			fs.forward = !fs.forward
		}
		return c.e.findChar(c, fs, true)
	}
}

// ---- brackets and paragraphs ----

var pairs = map[rune]rune{'(': ')', '[': ']', '{': '}', ')': '(', ']': '[', '}': '{'}

func isOpenBr(r rune) bool { return r == '(' || r == '[' || r == '{' }

// matchBracket finds the bracket matching the one at p.
func matchBracket(b *buffer.Buffer, p Pos) (Pos, bool) {
	r := b.RuneAt(p)
	m, ok := pairs[r]
	if !ok {
		return p, false
	}
	depth := 0
	if isOpenBr(r) {
		for q, ok := p, true; ok; q, ok = b.Next(q) {
			switch b.RuneAt(q) {
			case r:
				depth++
			case m:
				depth--
				if depth == 0 {
					return q, true
				}
			}
		}
	} else {
		for q, ok := p, true; ok; q, ok = b.Prev(q) {
			switch b.RuneAt(q) {
			case r:
				depth++
			case m:
				depth--
				if depth == 0 {
					return q, true
				}
			}
		}
	}
	return p, false
}

func mPercent(c *ctx) (Pos, MotionType, bool) {
	if c.hasCount {
		l := (c.count*c.b.LineCount() + 99) / 100
		l = min(max(l-1, 0), c.b.LineCount()-1)
		return Pos{l, firstNonBlank(c.b, l)}, Linewise, true
	}
	line := c.b.Line(c.w.cur.Line)
	for col := c.w.cur.Col; col < len(line); col++ {
		if _, ok := pairs[line[col]]; ok {
			if q, ok := matchBracket(c.b, Pos{c.w.cur.Line, col}); ok {
				return q, Inclusive, true
			}
			return c.w.cur, Inclusive, false
		}
	}
	return c.w.cur, Inclusive, false
}

func mParagraph(forward bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		l := c.w.cur.Line
		n := c.b.LineCount()
		for i := 0; i < c.count; i++ {
			if forward {
				for l < n-1 && isBlankLine(c.b, l) {
					l++
				}
				for l < n-1 && !isBlankLine(c.b, l) {
					l++
				}
			} else {
				for l > 0 && isBlankLine(c.b, l) {
					l--
				}
				for l > 0 && !isBlankLine(c.b, l) {
					l--
				}
			}
		}
		if forward && l == n-1 && !isBlankLine(c.b, l) {
			return Pos{l, max(c.b.LineLen(l)-1, 0)}, Inclusive, true
		}
		return Pos{l, 0}, Exclusive, true
	}
}

// mSection jumps to lines starting with an opening bracket in column 0
// (top-level forms in Lisps, function bodies in C-like code).
func mSection(forward bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		l := c.w.cur.Line
		n := c.b.LineCount()
		for i := 0; i < c.count; i++ {
			for {
				if forward {
					l++
				} else {
					l--
				}
				if l < 0 || l >= n {
					break
				}
				line := c.b.Line(l)
				if len(line) > 0 && (line[0] == '(' || line[0] == '{' || line[0] == '[') {
					break
				}
			}
		}
		if l < 0 {
			l = 0
		}
		if l >= n {
			l = n - 1
		}
		return Pos{l, 0}, Exclusive, true
	}
}

// ---- marks ----

func (e *Editor) markPos(b *buffer.Buffer, r rune) (Pos, bool) {
	switch r {
	case '`':
		r = '\''
	case '[', ']', '<', '>', '.', '\'', '^':
	default:
		if !(r >= 'a' && r <= 'z') {
			return Pos{}, false
		}
	}
	p, ok := b.Marks[r]
	return b.Clamp(p), ok
}

func mMark(exact bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		p, ok := c.e.markPos(c.b, c.char)
		if !ok {
			c.e.errorf("E20: Mark not set")
			return c.w.cur, Exclusive, false
		}
		if exact {
			return p, Exclusive, true
		}
		return Pos{p.Line, firstNonBlank(c.b, p.Line)}, Linewise, true
	}
}

// ---- notebook / percent-script cells ----

func cellMarker(b *buffer.Buffer, l int) bool {
	return strings.HasPrefix(b.LineString(l), "# %%")
}

func mCell(forward bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		b := c.b
		n := b.LineCount()
		l := c.w.cur.Line
		if !forward {
			// start from the header of the current cell
			for l > 0 && !cellMarker(b, l) {
				l--
			}
		}
		for i := 0; i < c.count; i++ {
			for {
				if forward {
					l++
				} else {
					l--
				}
				if l < 0 || l >= n || cellMarker(b, l) {
					break
				}
			}
		}
		if l < 0 || l >= n {
			if !forward && l < 0 && !cellMarker(b, 0) && c.w.cur.Line > 0 {
				return Pos{0, 0}, Linewise, true // leading text before the first marker
			}
			return c.w.cur, Linewise, false
		}
		// land on the first content line of the cell
		if l+1 < n && !cellMarker(b, l+1) {
			l++
		}
		return Pos{l, 0}, Linewise, true
	}
}
