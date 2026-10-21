package editor

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"scihe/internal/buffer"
)

// opRange is the region an operator acts on.
type opRange struct {
	start, end Pos // end exclusive for charwise; for linewise only lines matter
	linewise   bool
	block      bool
	// block columns (display columns, end exclusive)
	vcolStart, vcolEnd int
	toEOL              bool // block selection extended with $
}

// rangeFromMotion converts a motion result into an operator range.
func (e *Editor) rangeFromMotion(c *ctx, target Pos, mt MotionType) opRange {
	b := c.b
	start, end := buffer.OrderPos(c.w.cur, target)
	if mt == Linewise {
		return opRange{start: Pos{start.Line, 0}, end: Pos{end.Line, b.LineLen(end.Line)}, linewise: true}
	}
	if mt == Inclusive {
		if end.Col < b.LineLen(end.Line) {
			end.Col++
		}
		return opRange{start: start, end: end}
	}
	// exclusive motion special cases (:help exclusive)
	if end.Col == 0 && end.Line > start.Line && c.op != OpNone {
		end = Pos{end.Line - 1, b.LineLen(end.Line - 1)}
		if start.Col <= firstNonBlank(b, start.Line) {
			return opRange{start: Pos{start.Line, 0}, end: end, linewise: true}
		}
	}
	return opRange{start: start, end: end}
}

// text returns the register content for a range.
func (e *Editor) rangeText(b *buffer.Buffer, r opRange) Register {
	if r.block {
		var lines []string
		ts := e.options(b).TabStop
		for l := r.start.Line; l <= r.end.Line; l++ {
			s, en := blockCols(b.Line(l), r, ts)
			lines = append(lines, string(b.Line(l)[s:en]))
		}
		return Register{Text: strings.Join(lines, "\n"), Type: RegBlock}
	}
	if r.linewise {
		var sb strings.Builder
		for l := r.start.Line; l <= r.end.Line; l++ {
			sb.WriteString(b.LineString(l))
			sb.WriteByte('\n')
		}
		return Register{Text: sb.String(), Type: RegLine}
	}
	return Register{Text: b.Slice(r.start, r.end), Type: RegChar}
}

// blockCols returns rune columns [s,e) of line covered by a block range.
func blockCols(line []rune, r opRange, ts int) (int, int) {
	s := colForDisplay(line, r.vcolStart, ts)
	e := colForDisplay(line, r.vcolEnd, ts)
	if r.toEOL {
		e = len(line)
	}
	if s > len(line) {
		s = len(line)
	}
	if e > len(line) {
		e = len(line)
	}
	if e < s {
		e = s
	}
	return s, e
}

// deleteRange removes the range text and returns the new cursor.
func (e *Editor) deleteRange(b *buffer.Buffer, r opRange) Pos {
	if r.block {
		ts := e.options(b).TabStop
		for l := r.start.Line; l <= r.end.Line; l++ {
			s, en := blockCols(b.Line(l), r, ts)
			b.Delete(Pos{l, s}, Pos{l, en})
		}
		return Pos{r.start.Line, colForDisplay(b.Line(r.start.Line), r.vcolStart, ts)}
	}
	if r.linewise {
		n := b.LineCount()
		switch {
		case r.end.Line < n-1:
			b.Delete(Pos{r.start.Line, 0}, Pos{r.end.Line + 1, 0})
		case r.start.Line > 0:
			b.Delete(Pos{r.start.Line - 1, b.LineLen(r.start.Line - 1)}, Pos{r.end.Line, b.LineLen(r.end.Line)})
		default:
			b.Delete(Pos{0, 0}, Pos{r.end.Line, b.LineLen(r.end.Line)})
		}
		l := min(r.start.Line, b.LineCount()-1)
		return Pos{l, firstNonBlank(b, l)}
	}
	b.Delete(r.start, r.end)
	return r.start
}

// applyOperator executes op over r.
func (e *Editor) applyOperator(c *ctx, op Operator, r opRange) {
	b, w := c.b, c.w
	if b.ReadOnly && op != OpYank && op != OpEval {
		e.errorf("E21: Cannot make changes, buffer is read-only")
		return
	}
	b.Marks['['] = r.start
	b.Marks[']'] = r.end
	switch op {
	case OpYank:
		e.regs.Yank(c.reg, e.rangeText(b, r))
		if r.block {
			w.cur = Pos{r.start.Line, colForDisplay(b.Line(r.start.Line), r.vcolStart, e.options(b).TabStop)}
		} else if r.linewise {
			if w.cur.Line > r.start.Line || e.mode.visual() {
				w.cur = Pos{r.start.Line, min(w.cur.Col, lastCol(b, r.start.Line))}
				if e.mode.visual() {
					w.cur.Col = 0
				}
			}
		} else {
			w.cur = r.start
		}
		if n := r.end.Line - r.start.Line + 1; n > 2 {
			e.infof("%d lines yanked", n)
		}
	case OpDelete:
		e.regs.Delete(c.reg, e.rangeText(b, r))
		w.cur = e.deleteRange(b, r)
		if n := r.end.Line - r.start.Line + 1; r.linewise && n > 2 {
			e.infof("%d fewer lines", n)
		}
	case OpChange:
		e.regs.Delete(c.reg, e.rangeText(b, r))
		if r.block {
			w.cur = e.deleteRange(b, r)
			e.startBlockInsert(r, false)
			return
		}
		if r.linewise {
			indent := leadingWS(b.LineString(r.start.Line))
			if !e.options(b).AutoIndent {
				indent = ""
			}
			b.Delete(Pos{r.start.Line, 0}, Pos{r.end.Line, b.LineLen(r.end.Line)})
			b.Insert(Pos{r.start.Line, 0}, indent)
			w.cur = Pos{r.start.Line, utf8.RuneCountInString(indent)}
		} else {
			b.Delete(r.start, r.end)
			w.cur = r.start
		}
		e.enterInsert()
	case OpIndent, OpDedent:
		count := 1
		if c.hasCount && e.mode.visual() {
			count = c.count
		}
		for l := r.start.Line; l <= r.end.Line; l++ {
			for i := 0; i < count; i++ {
				e.shiftLine(b, l, op == OpIndent)
			}
		}
		w.cur = Pos{r.start.Line, firstNonBlank(b, r.start.Line)}
		if n := r.end.Line - r.start.Line + 1; n > 2 {
			e.infof("%d lines %s", n, map[bool]string{true: ">ed", false: "<ed"}[op == OpIndent])
		}
	case OpLower, OpUpper, OpToggleCase:
		e.mapRange(b, r, func(s string) string {
			switch op {
			case OpLower:
				return strings.ToLower(s)
			case OpUpper:
				return strings.ToUpper(s)
			}
			return toggleCase(s)
		})
		w.cur = r.start
		if r.linewise {
			w.cur.Col = min(w.cur.Col, lastCol(b, r.start.Line))
		}
	case OpComment:
		e.toggleComment(b, r.start.Line, r.end.Line)
		w.cur = Pos{r.start.Line, firstNonBlank(b, r.start.Line)}
	case OpFormat:
		e.formatLines(b, r.start.Line, r.end.Line, 80)
		w.cur = Pos{min(r.end.Line, b.LineCount()-1), 0}
		w.cur.Line = min(w.cur.Line, b.LineCount()-1)
	case OpEval:
		text := e.rangeText(b, r).Text
		e.evalText(b, text, r.end.Line, r.start, r.end)
	}
	e.clampCursor(w)
}

func (e *Editor) mapRange(b *buffer.Buffer, r opRange, f func(string) string) {
	if r.block {
		ts := e.options(b).TabStop
		for l := r.start.Line; l <= r.end.Line; l++ {
			s, en := blockCols(b.Line(l), r, ts)
			t := string(b.Line(l)[s:en])
			b.Replace(Pos{l, s}, Pos{l, en}, f(t))
		}
		return
	}
	if r.linewise {
		for l := r.start.Line; l <= r.end.Line; l++ {
			old := b.LineString(l)
			if nw := f(old); nw != old {
				b.Replace(Pos{l, 0}, Pos{l, b.LineLen(l)}, nw)
			}
		}
		return
	}
	old := b.Slice(r.start, r.end)
	if nw := f(old); nw != old {
		b.Replace(r.start, r.end, nw)
	}
}

func toggleCase(s string) string {
	rs := []rune(s)
	for i, r := range rs {
		if unicode.IsUpper(r) {
			rs[i] = unicode.ToLower(r)
		} else if unicode.IsLower(r) {
			rs[i] = unicode.ToUpper(r)
		}
	}
	return string(rs)
}

func leadingWS(s string) string {
	for i, r := range s {
		if r != ' ' && r != '\t' {
			return s[:i]
		}
	}
	return s
}

// indentUnit returns one level of indentation for b.
func (e *Editor) indentUnit(b *buffer.Buffer) string {
	o := e.options(b)
	if o.ExpandTab {
		return strings.Repeat(" ", o.ShiftWidth)
	}
	return "\t"
}

func (e *Editor) shiftLine(b *buffer.Buffer, l int, right bool) {
	line := b.LineString(l)
	if right {
		if strings.TrimSpace(line) == "" {
			return
		}
		b.Insert(Pos{l, 0}, e.indentUnit(b))
		return
	}
	o := e.options(b)
	ws := leadingWS(line)
	width := 0
	for _, r := range ws {
		if r == '\t' {
			width += o.TabStop - width%o.TabStop
		} else {
			width++
		}
	}
	newWidth := max(0, width-o.ShiftWidth)
	var nws string
	if o.ExpandTab {
		nws = strings.Repeat(" ", newWidth)
	} else {
		nws = strings.Repeat("\t", newWidth/o.TabStop) + strings.Repeat(" ", newWidth%o.TabStop)
	}
	if nws != ws {
		b.Replace(Pos{l, 0}, Pos{l, utf8.RuneCountInString(ws)}, nws)
	}
}

// commentString returns the line comment prefix for a filetype.
func commentString(ft string) string {
	switch ft {
	case "go", "c", "c++", "java", "javascript", "typescript", "rust", "swift", "kotlin", "scala", "dart", "zig", "protocol buffer", "json", "jsonc", "groovy", "php", "c#", "objective-c", "glsl", "hlsl", "verilog", "d":
		return "//"
	case "clojure", "scheme", "racket", "common lisp", "emacslisp", "newlisp", "fennel", "ini", "assembly", "gas", "nasm":
		return ";"
	case "lua", "sql", "mysql", "postgresql sql dialect", "haskell", "elm", "ada", "vhdl", "plpgsql", "transact-sql", "sqlite":
		return "--"
	case "vim", "viml":
		return "\""
	case "tex", "latex", "matlab", "octave", "erlang", "prolog", "postscript":
		return "%"
	case "fortran", "fortranfixed":
		return "!"
	case "html", "xml", "markdown", "svg":
		return "<!--"
	}
	return "#"
}

func (e *Editor) toggleComment(b *buffer.Buffer, from, to int) {
	cs := commentString(b.Filetype)
	closer := ""
	if cs == "<!--" {
		closer = " -->"
	}
	// all non-blank lines commented? then uncomment
	allCommented := true
	minIndent := -1
	for l := from; l <= to; l++ {
		s := b.LineString(l)
		t := strings.TrimLeft(s, " \t")
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, cs) {
			allCommented = false
		}
		if ind := utf8.RuneCountInString(s) - utf8.RuneCountInString(t); minIndent < 0 || ind < minIndent {
			minIndent = ind
		}
	}
	if minIndent < 0 {
		return
	}
	for l := from; l <= to; l++ {
		s := b.LineString(l)
		t := strings.TrimLeft(s, " \t")
		if t == "" {
			continue
		}
		ind := utf8.RuneCountInString(s) - utf8.RuneCountInString(t)
		if allCommented {
			rest := strings.TrimPrefix(t, cs)
			rest = strings.TrimPrefix(rest, " ")
			if closer != "" {
				rest = strings.TrimSuffix(strings.TrimSuffix(rest, closer), strings.TrimSpace(closer))
			}
			b.Replace(Pos{l, ind}, Pos{l, b.LineLen(l)}, rest)
		} else {
			b.Insert(Pos{l, minIndent}, cs+" ")
			if closer != "" {
				b.Insert(Pos{l, b.LineLen(l)}, closer)
			}
		}
	}
}

// formatLines reflows paragraphs in [from,to] to width (gq).
func (e *Editor) formatLines(b *buffer.Buffer, from, to, width int) {
	var out []string
	var para []string
	indent := ""
	flush := func() {
		if len(para) == 0 {
			return
		}
		words := strings.Fields(strings.Join(para, " "))
		line := indent
		for _, wd := range words {
			if line != indent && utf8.RuneCountInString(line)+1+utf8.RuneCountInString(wd) > width {
				out = append(out, line)
				line = indent
			}
			if line != indent {
				line += " "
			}
			line += wd
		}
		out = append(out, line)
		para = nil
	}
	for l := from; l <= to; l++ {
		s := b.LineString(l)
		if strings.TrimSpace(s) == "" {
			flush()
			out = append(out, "")
			continue
		}
		if len(para) == 0 {
			indent = leadingWS(s)
		}
		para = append(para, strings.TrimSpace(s))
	}
	flush()
	b.Replace(Pos{from, 0}, Pos{to, b.LineLen(to)}, strings.Join(out, "\n"))
}

// ---------------------------------------------------------------- put

// put inserts register reg (count times). after selects p vs P.
func (e *Editor) put(c *ctx, reg Register, after bool, cursorAfter bool) {
	b, w := c.b, c.w
	if b.ReadOnly {
		e.errorf("E21: Cannot make changes, buffer is read-only")
		return
	}
	count := c.count
	switch reg.Type {
	case RegLine:
		text := strings.TrimSuffix(reg.Text, "\n")
		text = strings.Repeat(text+"\n", count)
		text = strings.TrimSuffix(text, "\n")
		var line int
		if after {
			line = w.cur.Line + 1
			b.Insert(Pos{w.cur.Line, b.LineLen(w.cur.Line)}, "\n"+text)
		} else {
			line = w.cur.Line
			b.Insert(Pos{line, 0}, text+"\n")
		}
		n := strings.Count(text, "\n") + 1
		b.Marks['['] = Pos{line, 0}
		b.Marks[']'] = Pos{line + n - 1, 0}
		if cursorAfter {
			w.cur = Pos{min(line+n, b.LineCount()-1), 0}
		} else {
			w.cur = Pos{line, firstNonBlank(b, line)}
		}
	case RegBlock:
		lines := strings.Split(reg.Text, "\n")
		ts := e.options(b).TabStop
		vcol := displayCol(b.Line(w.cur.Line), w.cur.Col, ts)
		if after && b.LineLen(w.cur.Line) > 0 {
			vcol += runeWidth(b.RuneAt(w.cur), vcol, ts)
		}
		widest := 0
		for _, l := range lines {
			widest = max(widest, utf8.RuneCountInString(l))
		}
		for i, l := range lines {
			ln := w.cur.Line + i
			if ln >= b.LineCount() {
				b.Insert(b.End(), "\n")
			}
			line := b.Line(ln)
			cur := displayCol(line, len(line), ts)
			if cur < vcol {
				b.Insert(Pos{ln, len(line)}, strings.Repeat(" ", vcol-cur))
			}
			col := colForDisplay(b.Line(ln), vcol, ts)
			piece := strings.Repeat(l, count)
			if col < b.LineLen(ln) {
				piece += strings.Repeat(" ", (widest-utf8.RuneCountInString(l))*count)
			}
			b.Insert(Pos{ln, col}, piece)
		}
		w.cur = Pos{w.cur.Line, colForDisplay(b.Line(w.cur.Line), vcol, ts)}
	default:
		text := strings.Repeat(reg.Text, count)
		p := w.cur
		if after && b.LineLen(p.Line) > 0 {
			p.Col++
		}
		end := b.Insert(p, text)
		b.Marks['['] = p
		b.Marks[']'] = end
		if cursorAfter {
			w.cur = end
		} else if strings.Contains(text, "\n") {
			w.cur = p
		} else {
			w.cur = Pos{end.Line, max(end.Col-1, 0)}
		}
	}
	e.clampCursor(w)
}
