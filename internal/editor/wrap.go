package editor

// Soft wrap: with 'wrap' set a buffer line is shown on as many screen
// rows as it needs; with 'linebreak' rows break after blanks instead of
// in the middle of a word. The buffer text is never changed.

// wrapSeg is the part of a line shown on one screen row: runes
// [start, end), starting at display column vc of the line.
type wrapSeg struct {
	start, end, vc int
}

// wraps reports whether w shows its buffer soft-wrapped. Table views
// and directory listings are column layouts and never wrap.
func (e *Editor) wraps(w *Window) bool {
	b := w.buf
	if e.tables[b.ID] != nil || e.direds[b.ID] != nil {
		return false
	}
	return e.options(b).Wrap
}

// segments returns the screen rows of line l in w: one segment covering
// the whole line when not wrapping.
func (e *Editor) segments(w *Window, l int) []wrapSeg {
	line := w.buf.Line(l)
	if !e.wraps(w) {
		return []wrapSeg{{0, len(line), 0}}
	}
	o := e.options(w.buf)
	return segmentsOf(line, e.textWidth(w), o.TabStop, o.LineBreak)
}

// segmentsOf splits line into rows of at most width display columns.
// With linebreak a row ends after the last blank that fits, unless the
// row has no blank (a long word is then cut where it overflows).
func segmentsOf(line []rune, width, ts int, linebreak bool) []wrapSeg {
	width = max(width, 1)
	var segs []wrapSeg
	start, startVC, vc := 0, 0, 0
	lastBreak, breakVC := -1, 0 // rune index after a blank, and its column
	for i := 0; i < len(line); i++ {
		r := line[i]
		rw := runeWidth(r, vc, ts)
		if vc+rw-startVC > width && i > start {
			cut, cutVC := i, vc
			if linebreak && lastBreak > start {
				cut, cutVC = lastBreak, breakVC
			}
			segs = append(segs, wrapSeg{start, cut, startVC})
			start, startVC = cut, cutVC
			lastBreak = -1
			if cut < i {
				// re-measure the runes moved to the new row
				i = cut - 1
				vc = cutVC
				continue
			}
		}
		vc += rw
		if r == ' ' || r == '\t' {
			lastBreak, breakVC = i+1, vc
		}
	}
	return append(segs, wrapSeg{start, len(line), startVC})
}

// segIndex returns the segment containing rune column col; the end of
// the line belongs to the last segment.
func segIndex(segs []wrapSeg, col int) int {
	for k := len(segs) - 1; k > 0; k-- {
		if col >= segs[k].start {
			return k
		}
	}
	return 0
}

// textRows is the number of screen rows the text of line l takes in w,
// not counting virtual rows; the top line loses the rows scrolled past.
func (e *Editor) textRows(w *Window, l int) int {
	n := len(e.segments(w, l))
	if l == w.top {
		n -= min(w.skip, n-1)
	}
	return n
}

// lineRows is textRows plus the virtual rows (notebook outputs) after l.
func (e *Editor) lineRows(w *Window, l int) int {
	return e.textRows(w, l) + len(e.virtualRows(w.buf, l))
}

// mDisplayDown / mDisplayUp implement gj / gk: move by screen row in a
// wrapped window, by line otherwise.
func mDisplayDown(c *ctx) (Pos, MotionType, bool) { return c.e.displayMove(c, 1) }
func mDisplayUp(c *ctx) (Pos, MotionType, bool)   { return c.e.displayMove(c, -1) }

func (e *Editor) displayMove(c *ctx, dir int) (Pos, MotionType, bool) {
	w := c.w
	if !e.wraps(w) {
		if dir > 0 {
			return mDown(c)
		}
		return mUp(c)
	}
	ts := e.options(c.b).TabStop
	p := w.cur
	segs := e.segments(w, p.Line)
	k := segIndex(segs, p.Col)
	off := displayCol(c.b.Line(p.Line), p.Col, ts) - segs[k].vc
	moved := false
rows:
	for i := 0; i < c.count; i++ {
		switch {
		case dir > 0 && k+1 < len(segs):
			k++
		case dir > 0 && p.Line+1 < c.b.LineCount():
			p.Line++
			segs, k = e.segments(w, p.Line), 0
		case dir < 0 && k > 0:
			k--
		case dir < 0 && p.Line > 0:
			p.Line--
			segs = e.segments(w, p.Line)
			k = len(segs) - 1
		default:
			break rows // at the first / last row
		}
		moved = true
	}
	if !moved {
		return w.cur, Exclusive, false
	}
	line := c.b.Line(p.Line)
	s := segs[k]
	col := colForDisplay(line, s.vc+off, ts)
	if k+1 < len(segs) && col >= s.end {
		col = max(s.end-1, s.start) // stay on this row
	}
	p.Col = min(max(col, s.start), len(line))
	return p, Exclusive, true
}
