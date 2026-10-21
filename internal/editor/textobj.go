package editor

import (
	"scihe/internal/buffer"
	"scihe/internal/repl"
)

func toWord(big, around bool) TextObjFunc {
	return func(c *ctx) (Pos, Pos, MotionType, bool) {
		b := c.b
		line := b.Line(c.w.cur.Line)
		if len(line) == 0 {
			// empty line: an empty object (ciw still enters insert mode)
			return c.w.cur, c.w.cur, Exclusive, true
		}
		col := min(c.w.cur.Col, len(line)-1)
		cls := func(i int) int { return charClass(line[i], big) }
		start, end := col, col
		for start > 0 && cls(start-1) == cls(col) {
			start--
		}
		for end < len(line)-1 && cls(end+1) == cls(col) {
			end++
		}
		end++ // exclusive
		for i := 1; i < c.count && end < len(line); i++ {
			k := cls(end)
			for end < len(line) && cls(end) == k {
				end++
			}
		}
		if around {
			if cls(col) == clsSpace {
				// whitespace + following word
				if end < len(line) {
					k := cls(end)
					for end < len(line) && cls(end) == k {
						end++
					}
				}
			} else if end < len(line) && cls(end) == clsSpace {
				for end < len(line) && cls(end) == clsSpace {
					end++
				}
			} else {
				for start > 0 && cls(start-1) == clsSpace {
					start--
				}
			}
		}
		l := c.w.cur.Line
		return Pos{l, start}, Pos{l, end}, Exclusive, true
	}
}

// toPair selects the innermost (count-th) enclosing open/close pair.
func toPair(open, close rune, around bool) TextObjFunc {
	return func(c *ctx) (Pos, Pos, MotionType, bool) {
		b := c.b
		p := c.w.cur
		var o Pos
		found := false
		// cursor on the open/close char itself counts as enclosed
		if r := b.RuneAt(p); r == open {
			o, found = p, true
		} else if r == close {
			if q, ok := matchPair(b, p, open, close, false); ok {
				o, found = q, true
			}
		}
		if !found {
			o, found = enclosingOpen(b, p, open, close)
		}
		for i := 1; found && i < c.count; i++ {
			pp, ok := b.Prev(o)
			if !ok {
				found = false
				break
			}
			if b.RuneAt(pp) == open {
				o = pp // directly nested "(("
				continue
			}
			o, found = enclosingOpen(b, pp, open, close)
		}
		if !found {
			return p, p, Exclusive, false
		}
		cl, ok := matchPair(b, o, open, close, true)
		if !ok {
			return p, p, Exclusive, false
		}
		if around {
			end, _ := b.Next(cl)
			if cl.Col == b.LineLen(cl.Line)-1 && cl.Line == b.LineCount()-1 {
				end = Pos{cl.Line, cl.Col + 1}
			}
			return o, end, Exclusive, true
		}
		start, _ := b.Next(o)
		// "(\n ... \n  )": select the lines in between, linewise
		if o.Col == b.LineLen(o.Line)-1 && cl.Line > o.Line && firstNonBlank(b, cl.Line) == cl.Col {
			if cl.Line-1 < o.Line+1 {
				return start, start, Exclusive, true
			}
			last := cl.Line - 1
			return Pos{o.Line + 1, 0}, Pos{last, b.LineLen(last)}, Linewise, true
		}
		return start, cl, Exclusive, true
	}
}

// enclosingOpen scans backwards from p for an unmatched open char.
func enclosingOpen(b *buffer.Buffer, p Pos, open, close rune) (Pos, bool) {
	depth := 0
	q := p
	if b.RuneAt(q) == close {
		depth++ // our own close does not count
	}
	for {
		pq, ok := b.Prev(q)
		if !ok {
			return p, false
		}
		q = pq
		switch b.RuneAt(q) {
		case close:
			depth++
		case open:
			if depth == 0 {
				return q, true
			}
			depth--
		}
	}
}

func matchPair(b *buffer.Buffer, p Pos, open, close rune, forward bool) (Pos, bool) {
	depth := 0
	for q, ok := p, true; ok; {
		switch b.RuneAt(q) {
		case open:
			if forward {
				depth++
			} else {
				depth--
			}
		case close:
			if forward {
				depth--
			} else {
				depth++
			}
		}
		if depth == 0 {
			return q, true
		}
		if forward {
			q, ok = b.Next(q)
		} else {
			q, ok = b.Prev(q)
		}
	}
	return p, false
}

// toQuote selects a quoted string on the current line.
func toQuote(q rune, around bool) TextObjFunc {
	return func(c *ctx) (Pos, Pos, MotionType, bool) {
		line := c.b.Line(c.w.cur.Line)
		col := c.w.cur.Col
		var quotes []int
		for i := 0; i < len(line); i++ {
			if line[i] == '\\' {
				i++
				continue
			}
			if line[i] == q {
				quotes = append(quotes, i)
			}
		}
		s, e := -1, -1
		for i := 0; i+1 < len(quotes); i += 2 {
			if col >= quotes[i] && col <= quotes[i+1] {
				s, e = quotes[i], quotes[i+1]
				break
			}
		}
		if s < 0 {
			// first pair after the cursor
			for i := 0; i+1 < len(quotes); i += 2 {
				if quotes[i] > col {
					s, e = quotes[i], quotes[i+1]
					break
				}
			}
		}
		if s < 0 {
			return c.w.cur, c.w.cur, Exclusive, false
		}
		l := c.w.cur.Line
		if !around {
			return Pos{l, s + 1}, Pos{l, e}, Exclusive, true
		}
		end := e + 1
		if end < len(line) && (line[end] == ' ' || line[end] == '\t') {
			for end < len(line) && (line[end] == ' ' || line[end] == '\t') {
				end++
			}
		} else {
			for s > 0 && (line[s-1] == ' ' || line[s-1] == '\t') {
				s--
			}
		}
		return Pos{l, s}, Pos{l, end}, Exclusive, true
	}
}

func toParagraph(around bool) TextObjFunc {
	return func(c *ctx) (Pos, Pos, MotionType, bool) {
		b := c.b
		l := c.w.cur.Line
		blank := isBlankLine(b, l)
		s, e := l, l
		for s > 0 && isBlankLine(b, s-1) == blank {
			s--
		}
		for i := 0; i < c.count; i++ {
			if i > 0 {
				if e+1 >= b.LineCount() {
					break
				}
				e++
				blank = isBlankLine(b, e)
			}
			for e+1 < b.LineCount() && isBlankLine(b, e+1) == blank {
				e++
			}
		}
		if around {
			if e+1 < b.LineCount() {
				nb := isBlankLine(b, e+1)
				e++
				for e+1 < b.LineCount() && isBlankLine(b, e+1) == nb {
					e++
				}
			} else {
				for s > 0 && isBlankLine(b, s-1) != blank {
					s--
				}
			}
		}
		return Pos{s, 0}, Pos{e, b.LineLen(e)}, Linewise, true
	}
}

func toEntire(c *ctx) (Pos, Pos, MotionType, bool) {
	return Pos{}, c.b.End(), Linewise, true
}

// toCell selects a notebook / percent-script cell.
func toCell(around bool) TextObjFunc {
	return func(c *ctx) (Pos, Pos, MotionType, bool) {
		b := c.b
		l := c.w.cur.Line
		s := l
		for s > 0 && !cellMarker(b, s) {
			s--
		}
		e := l + 1
		for e < b.LineCount() && !cellMarker(b, e) {
			e++
		}
		e--
		if !around && cellMarker(b, s) {
			s++
		}
		if s > e {
			return c.w.cur, c.w.cur, Linewise, false
		}
		return Pos{s, 0}, Pos{e, b.LineLen(e)}, Linewise, true
	}
}

// toForm selects a Lisp form (af) or its contents (if).
func toForm(around bool) TextObjFunc {
	return func(c *ctx) (Pos, Pos, MotionType, bool) {
		d, ok := repl.DialectFor(c.b.Filetype)
		if !ok {
			d = repl.Scheme
		}
		text := []rune(c.b.Text())
		off := c.b.Offset(c.w.cur)
		f, ok := repl.FormAt(text, off, d)
		if !ok {
			return c.w.cur, c.w.cur, Exclusive, false
		}
		s, e := f.Start, f.End
		if !around && f.List {
			for s < e && !isOpenBr(text[s]) {
				s++
			}
			s++
			e--
		}
		if s > e {
			s = e
		}
		return c.b.PosAt(s), c.b.PosAt(e), Exclusive, true
	}
}
