package editor

import (
	"strconv"
	"strings"
	"time"

	"scihe/internal/buffer"
	"scihe/internal/keymap"
)

type visualState struct {
	anchor Pos
	// last selection for gv
	lastBuf    *buffer.Buffer
	lastAnchor Pos
	lastCur    Pos
	lastMode   Mode
	lastWant   int
}

func (e *Editor) enterVisual(m Mode) {
	w := e.win
	if e.mode.visual() {
		if e.mode == m {
			e.exitVisual()
			return
		}
		e.mode = m
		return
	}
	e.mode = m
	e.visual.anchor = w.cur
}

func (e *Editor) setVisualMarks() {
	w := e.win
	s, en := buffer.OrderPos(e.visual.anchor, w.cur)
	if e.mode == ModeVisualLine {
		s.Col = 0
		en.Col = max(w.buf.LineLen(en.Line)-1, 0)
	}
	w.buf.Marks['<'] = s
	w.buf.Marks['>'] = en
	e.visual.lastBuf = w.buf
	e.visual.lastAnchor = e.visual.anchor
	e.visual.lastCur = w.cur
	e.visual.lastMode = e.mode
	e.visual.lastWant = w.want
}

func (e *Editor) exitVisual() {
	if !e.mode.visual() {
		return
	}
	e.setVisualMarks()
	e.mode = ModeNormal
	e.clampCursor(e.win)
}

func (e *Editor) reselect() {
	w := e.win
	if e.visual.lastBuf != w.buf {
		return
	}
	e.mode = e.visual.lastMode
	e.visual.anchor = w.buf.Clamp(e.visual.lastAnchor)
	w.cur = w.buf.Clamp(e.visual.lastCur)
	w.want = e.visual.lastWant
}

// visualRange converts the selection into an operator range.
func (e *Editor) visualRange() opRange {
	w := e.win
	b := w.buf
	s, en := buffer.OrderPos(e.visual.anchor, w.cur)
	switch e.mode {
	case ModeVisualLine:
		return opRange{start: Pos{s.Line, 0}, end: Pos{en.Line, b.LineLen(en.Line)}, linewise: true}
	case ModeVisualBlock:
		ts := e.options(b).TabStop
		a := displayCol(b.Line(e.visual.anchor.Line), e.visual.anchor.Col, ts)
		c := displayCol(b.Line(w.cur.Line), w.cur.Col, ts)
		// each corner covers the full width of its character (tabs, CJK)
		lo := min(a, c)
		hi := max(a+cellWidth(b, e.visual.anchor, a, ts), c+cellWidth(b, w.cur, c, ts))
		return opRange{
			start: Pos{min(e.visual.anchor.Line, w.cur.Line), 0},
			end:   Pos{max(e.visual.anchor.Line, w.cur.Line), 0},
			block: true, vcolStart: lo, vcolEnd: hi, toEOL: w.want == -1,
		}
	}
	// charwise, inclusive of the last character
	if en.Col < b.LineLen(en.Line) {
		en.Col++
	} else if en.Line < b.LineCount()-1 {
		en = Pos{en.Line + 1, 0}
	}
	return opRange{start: s, end: en}
}

func (e *Editor) visualKey(k Key) {
	if len(e.pending) == 0 {
		e.pendingSince = time.Now()
		if k == "<Esc>" || k == "<C-c>" {
			e.exitVisual()
			return
		}
	}
	e.pending = append(e.pending, k)
	keys := e.pending
	var p parsed
	st := parseInvalid
	for _, m := range e.mapsFor(e.visualMap) {
		p, st = e.parseKeys(keys, m, false)
		if st != parseInvalid {
			break
		}
	}
	switch st {
	case parseIncomplete, parseAmbiguous:
		if len(keys) > p.countEnd && keys[p.countEnd] == e.leader {
			e.whichKey = true
		}
		e.pendAmb = st == parseAmbiguous
		return
	case parseInvalid:
		e.resetPending()
		return
	}
	keys = append([]Key(nil), keys...)
	e.resetPending()
	e.execVisual(p, keys[:p.end])
	for _, k := range keys[p.end:] {
		e.handleKey(k)
	}
}

func (e *Editor) execVisual(p parsed, keys []Key) {
	w := e.win
	b := w.buf
	count, has := p.count()
	c := &ctx{e: e, w: w, b: b, count: count, hasCount: has, reg: p.reg, char: p.char, keys: keys}
	cmd := p.cmd
	if cmd.Mapping != "" {
		e.runMapping(cmd.Mapping, c)
		return
	}
	// a change made from visual mode repeats on a same-sized selection
	dotPrefix := e.visualDotPrefix()
	version := b.Version
	defer func() {
		if e.dot.replay > 0 || cmd.Kind == KindMotion || cmd.Kind == KindTextObject {
			return
		}
		if cmd.Kind == KindOperator && (cmd.Op == OpYank || cmd.Op == OpEval) {
			return
		}
		if b.Version == version && e.mode != ModeInsert && e.mode != ModeReplace {
			return
		}
		e.setDot(append(dotPrefix, keys...), 0)
	}()
	switch cmd.Kind {
	case KindMotion:
		e.doMotion(c, cmd)
	case KindTextObject:
		s, en, mt, ok := cmd.TextObj(c)
		if !ok {
			return
		}
		if mt == Linewise && e.mode == ModeVisual {
			e.mode = ModeVisualLine
		}
		if en.Col > 0 {
			en.Col--
		} else if en.Line > s.Line {
			en = Pos{en.Line - 1, max(b.LineLen(en.Line-1)-1, 0)}
		}
		// extend an existing selection forward, or select the object
		if e.visual.anchor == w.cur || s.Less(e.visual.anchor) {
			e.visual.anchor = s
		}
		w.cur = en
	case KindOperator:
		r := e.visualRange()
		c.op = cmd.Op
		e.exitVisual()
		b.BeginUndo(w.cur)
		e.applyOperator(c, cmd.Op, r)
		if e.mode == ModeInsert {
			e.insert.undoBuf = b
		} else {
			b.EndUndo(w.cur)
		}
	case KindAction:
		b.BeginUndo(w.cur)
		cmd.Action(c)
		if e.mode == ModeInsert || e.mode == ModeReplace {
			e.insert.undoBuf = b
		} else {
			b.EndUndo(w.cur)
		}
	}
	if e.win == w && !e.mode.visual() {
		e.clampCursor(w)
	}
}

// visualDotPrefix returns keys that re-create a selection of the current
// size starting at the cursor (used when repeating a visual change).
func (e *Editor) visualDotPrefix() []Key {
	w := e.win
	s, en := buffer.OrderPos(e.visual.anchor, w.cur)
	n := func(k int, key string) string {
		if k <= 0 {
			return ""
		}
		return strconv.Itoa(k) + key
	}
	var str string
	dl := en.Line - s.Line
	switch e.mode {
	case ModeVisualLine:
		str = "V" + n(dl, "j")
	case ModeVisualBlock:
		r := e.visualRange()
		str = "<C-v>" + n(dl, "j") + n(r.vcolEnd-r.vcolStart-1, "l")
		if r.toEOL {
			str += "$"
		}
	default:
		if dl == 0 {
			str = "v" + n(en.Col-s.Col, "l")
		} else {
			str = "v" + n(dl, "j") + "0" + n(en.Col, "l")
		}
	}
	return keymap.Parse(str, e.leader)
}

// visual actions ------------------------------------------------------------

func vSwapEnds(c *ctx) {
	e := c.e
	e.visual.anchor, c.w.cur = c.w.cur, e.visual.anchor
}

func vSwapCorner(c *ctx) {
	e := c.e
	if e.mode != ModeVisualBlock {
		vSwapEnds(c)
		return
	}
	a, cur := e.visual.anchor, c.w.cur
	e.visual.anchor = Pos{a.Line, cur.Col}
	c.w.cur = Pos{cur.Line, a.Col}
}

func vInsertBlock(appendMode bool) ActionFunc {
	return func(c *ctx) {
		e := c.e
		r := e.visualRange()
		mode := e.mode
		e.exitVisual()
		b := c.b
		ts := e.options(b).TabStop
		if mode != ModeVisualBlock {
			// I / A on a char/line selection: insert at start / append at end
			if appendMode {
				end := r.end
				if r.linewise {
					end = Pos{r.end.Line, b.LineLen(r.end.Line)}
				}
				c.w.cur = end
			} else {
				c.w.cur = r.start
				if r.linewise {
					c.w.cur.Col = firstNonBlank(b, r.start.Line)
				}
			}
			e.enterInsert()
			return
		}
		l := r.start.Line
		if appendMode {
			line := b.Line(l)
			if r.toEOL {
				c.w.cur = Pos{l, len(line)}
			} else {
				if cur := displayCol(line, len(line), ts); cur < r.vcolEnd {
					b.Insert(Pos{l, len(line)}, strings.Repeat(" ", r.vcolEnd-cur))
				}
				c.w.cur = Pos{l, colForDisplay(b.Line(l), r.vcolEnd, ts)}
			}
		} else {
			c.w.cur = Pos{l, colForDisplay(b.Line(l), r.vcolStart, ts)}
		}
		e.startBlockInsert(r, appendMode)
	}
}

func vReplaceChar(c *ctx) {
	e := c.e
	r := e.visualRange()
	e.exitVisual()
	ch := string(c.char)
	if c.char == '\n' {
		return
	}
	e.mapRange(c.b, r, func(s string) string {
		var sb strings.Builder
		for _, x := range s {
			if x == '\n' {
				sb.WriteRune(x)
			} else {
				sb.WriteString(ch)
			}
		}
		return sb.String()
	})
	c.w.cur = r.start
	if r.block {
		c.w.cur.Col = colForDisplay(c.b.Line(r.start.Line), r.vcolStart, e.options(c.b).TabStop)
	}
}

func vJoin(spaces bool) ActionFunc {
	return func(c *ctx) {
		e := c.e
		r := e.visualRange()
		e.exitVisual()
		n := r.end.Line - r.start.Line + 1
		c.w.cur = Pos{r.start.Line, 0}
		e.joinLines(c.w, max(n, 2), spaces)
	}
}

// vPut replaces the selection with a register (p / P).
func vPut(keepReg bool) ActionFunc {
	return func(c *ctx) {
		e := c.e
		reg, ok := e.regs.Get(c.reg)
		if !ok {
			e.exitVisual()
			return
		}
		r := e.visualRange()
		e.exitVisual()
		old := e.rangeText(c.b, r)
		c.w.cur = e.deleteRange(c.b, r)
		if r.linewise {
			if reg.Type != RegLine {
				// put charwise text on its own line
				reg = Register{Text: reg.Text + "\n", Type: RegLine}
			}
			wasAll := c.b.LineCount() == 1 && c.b.LineLen(0) == 0
			if r.start.Line >= c.b.LineCount() {
				c.w.cur = Pos{c.b.LineCount() - 1, 0}
				e.put(c, reg, true, false)
			} else {
				c.w.cur = Pos{r.start.Line, 0}
				e.put(c, reg, false, false)
				if last := c.b.LineCount() - 1; wasAll && last > 0 && c.b.LineLen(last) == 0 {
					c.b.Delete(Pos{last - 1, c.b.LineLen(last - 1)}, Pos{last, 0})
				}
			}
		} else {
			if reg.Type == RegLine {
				c.b.Insert(c.w.cur, "\n")
				c.w.cur = Pos{c.w.cur.Line + 1, 0}
				e.put(c, Register{Text: strings.TrimSuffix(reg.Text, "\n") + "\n", Type: RegLine}, false, false)
			} else {
				e.put(c, reg, false, false)
			}
		}
		if !keepReg {
			e.regs.Delete(0, old)
			// vim keeps the put register intact in "0 etc.
		}
	}
}
