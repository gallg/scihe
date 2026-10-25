package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"

	"scihe/internal/buffer"
	"scihe/internal/keymap"
	"scihe/internal/syntax"
)

// Decorator lets features change how buffer lines are displayed.
type Decorator interface {
	// Header replaces the display of a line (notebook cell markers).
	Header(line int) (text string, style tcell.Style, ok bool)
	// Virtual returns extra display-only rows shown after a line.
	Virtual(line int) []vline
}

// framer is an optional Decorator interface that draws cells as boxes
// (notebooks): Header lines become the box top, Virtual rows can be box
// edges, and the lines of a cell get side borders.
type framer interface {
	// Frame returns the border style of the box containing line, if any.
	Frame(line int) (tcell.Style, bool)
}

// vline kinds
const (
	vPlain     = iota // text from the text column
	vBoxTop           // "╭─ text ──╮" (text is the label)
	vBoxLine          // "│ text   │"
	vBoxBottom        // "╰────────╯"
)

type vline struct {
	text  string
	style tcell.Style
	kind  int
	frame tcell.Style // border style of box rows
}

func (e *Editor) virtualRows(b *buffer.Buffer, l int) []vline {
	if d := e.decor[b.ID]; d != nil {
		return d.Virtual(l)
	}
	return nil
}

// draw renders the whole screen.
func (e *Editor) draw() {
	s := e.screen
	sw, sh := s.Size()
	if sw <= 0 || sh <= 0 {
		return
	}
	// previews depend on window sizes, so lay out first
	e.layout(0, 0, sw, max(sh-1, 1))
	e.updatePreviews()
	e.updateTables()
	e.updateBufLists()
	e.layout(0, 0, sw, max(sh-1, 1))
	// fill with an explicit style: tcell's Clear() uses StyleDefault, and
	// such cells are never resent when the theme (screen style) changes
	s.SetStyle(e.theme.Default)
	s.Fill(' ', e.theme.Default)
	for _, w := range e.windows() {
		e.drawWindow(w)
		if w.x > 0 {
			for y := w.y; y < w.y+w.h; y++ {
				s.SetContent(w.x-1, y, '│', nil, e.theme.Border.Background(e.theme.Background()))
			}
		}
	}
	e.drawCmdline(sw, sh)
	if e.whichKey {
		e.drawWhichKey(sw, sh)
	}
	if e.insert.compl != nil {
		e.drawCompletion(sw, sh)
	}
	if e.picker != nil {
		e.drawPicker(sw, sh)
	}
	e.placeCursor(sw, sh)
	s.Show()
}

func (e *Editor) placeCursor(sw, sh int) {
	s := e.screen
	switch {
	case e.picker != nil:
		return // placed by drawPicker
	case e.mode == ModeCmdline && e.cmdline != nil:
		c := e.cmdline
		x := runewidth.StringWidth(c.prompt) + runewidth.StringWidth(string(c.text[:c.cur]))
		s.ShowCursor(min(x, sw-1), sh-1)
		s.SetCursorStyle(tcell.CursorStyleSteadyBar)
		return
	}
	w := e.win
	if w.curX < 0 {
		s.HideCursor()
		return
	}
	s.ShowCursor(w.curX, w.curY)
	switch e.mode {
	case ModeInsert:
		s.SetCursorStyle(tcell.CursorStyleSteadyBar)
	case ModeReplace:
		s.SetCursorStyle(tcell.CursorStyleSteadyUnderline)
	default:
		s.SetCursorStyle(tcell.CursorStyleSteadyBlock)
	}
}

// cursorScreen returns the last drawn screen position of w's cursor.
func (e *Editor) cursorScreen(w *Window) (int, int) {
	return w.curX, w.curY
}

// gutterWidth is the width left of the text: line numbers plus, for
// boxed cells, the left border and a space.
func (e *Editor) gutterWidth(w *Window) int {
	return e.numberWidth(w) + e.frameWidth(w)
}

func (e *Editor) numberWidth(w *Window) int {
	o := e.options(w.buf)
	if !o.Number && !o.RelativeNumber {
		return 0
	}
	n := len(strconv.Itoa(w.buf.LineCount()))
	return max(n, 3) + 1
}

// frameWidth is the room taken by each side border of boxed cells.
func (e *Editor) frameWidth(w *Window) int {
	if _, ok := e.decor[w.buf.ID].(framer); ok && w.w >= 20 {
		return 2
	}
	return 0
}

// textWidth is the number of text columns of w.
func (e *Editor) textWidth(w *Window) int {
	return max(w.w-e.gutterWidth(w)-e.frameWidth(w), 1)
}

// drawBoxEdge draws a horizontal box edge from x0 to x1 (inclusive) with
// an optional label after the left corner.
func drawBoxEdge(s tcell.Screen, x0, x1, y int, left, right rune, label string, labelSt, st tcell.Style) {
	if x1-x0 < 2 {
		return
	}
	s.SetContent(x0, y, left, nil, st)
	for x := x0 + 1; x < x1; x++ {
		s.SetContent(x, y, '─', nil, st)
	}
	s.SetContent(x1, y, right, nil, st)
	if label != "" && x1-x0 > 6 {
		x := x0 + 2
		x += drawText(s, x, y, 1, " ", st)
		x += drawText(s, x, y, x1-x-1, label, labelSt)
		if x < x1-1 {
			s.SetContent(x, y, ' ', nil, st)
		}
	}
}

// scrollToCursor adjusts w.top / w.left so the cursor is visible.
func (e *Editor) scrollToCursor(w *Window) {
	b := w.buf
	h := w.textHeight()
	if w.top >= b.LineCount() {
		w.top = max(b.LineCount()-1, 0)
	}
	cl := w.cur.Line
	wrap := e.wraps(w)
	// row of the cursor inside its (wrapped) line
	curSeg := 0
	if wrap {
		curSeg = segIndex(e.segments(w, cl), w.cur.Col)
	}
	if w.pinTop {
		w.pinTop = false
		if !wrap || w.top != cl {
			w.skip = 0
		}
	} else {
		w.skip = 0
		so := min(e.options(b).ScrollOff, (h-1)/2)
		if cl < w.top+so {
			w.top = max(cl-so, 0)
		}
		// rows from top through the cursor row plus scrolloff lines after it
		for w.top < cl {
			rows := 0
			last := min(cl+so, b.LineCount()-1)
			for l := w.top; l <= last; l++ {
				switch {
				case l < cl:
					rows += e.lineRows(w, l)
				case l == cl:
					rows += curSeg + 1
				default:
					rows++
				}
			}
			if rows <= h {
				break
			}
			w.top++
		}
		// a line taller than the window: scroll inside it
		if w.top == cl && curSeg+1 > h {
			w.skip = curSeg + 1 - h
		}
	}
	if wrap {
		w.left = 0
		return
	}
	// horizontal
	textW := e.textWidth(w)
	ts := e.options(b).TabStop
	vc := displayCol(b.Line(cl), w.cur.Col, ts)
	cw := cellWidth(b, w.cur, vc, ts)
	if vc < w.left {
		w.left = vc
	}
	if vc+cw > w.left+textW {
		w.left = vc + cw - textW
	}
}

type selRange struct {
	active     bool
	start, end Pos // inclusive start, exclusive end (charwise)
	lines      bool
	block      bool
	vs, ve     int
}

func (e *Editor) selectionFor(w *Window) selRange {
	if w != e.win || !e.mode.visual() {
		return selRange{}
	}
	r := e.visualRange()
	return selRange{active: true, start: r.start, end: r.end, lines: r.linewise, block: r.block, vs: r.vcolStart, ve: r.vcolEnd}
}

func mergeStyle(base, over tcell.Style) tcell.Style {
	fg, bg, attr := over.Decompose()
	_, bbg, battr := base.Decompose()
	st := base.Foreground(fg).Attributes(attr | battr&tcell.AttrUnderline)
	if bg != tcell.ColorDefault && bg != bbg {
		st = st.Background(bg)
	}
	return st
}

func withBg(st tcell.Style, from tcell.Style) tcell.Style {
	_, bg, _ := from.Decompose()
	return st.Background(bg)
}

func (e *Editor) drawWindow(w *Window) {
	s := e.screen
	b := w.buf
	th := e.theme
	o := e.options(b)
	ts := o.TabStop
	e.scrollToCursor(w)
	gw := e.gutterWidth(w)
	nw := e.numberWidth(w)
	textW := e.textWidth(w)
	fr, _ := e.decor[b.ID].(framer)
	if e.frameWidth(w) == 0 {
		fr = nil
	}
	x1 := w.x + w.w - 1 // right border column of boxes
	h := w.textHeight()
	sel := e.selectionFor(w)
	active := w == e.win
	w.curX, w.curY = -1, -1
	// the bracket pair at the cursor (normal and insert mode); in insert
	// mode the cursor is a bar, so the bracket it touches is marked too
	matchPos, hasMatch := Pos{}, false
	brPos, markBr := Pos{}, false
	if active {
		if p, ok := e.cursorBracket(b, w.cur); ok {
			matchPos, hasMatch = matchBracketWithin(b, p, matchParenLines)
			brPos, markBr = p, hasMatch && e.mode != ModeNormal
		}
	}
	notify := func() { e.Post(func() {}) }
	static := e.static[b.ID]
	hl := e.highlighter(b)
	dec := e.decor[b.ID]
	ann := e.annots[b.ID]
	tv := e.tables[b.ID]
	row := 0
	for l := w.top; l < b.LineCount() && row < h; l++ {
		y := w.y + row
		isCur := l == w.cur.Line
		base := th.Default
		if isCur && o.CursorLine && active && !sel.active {
			base = th.CursorLine
		}
		// gutter
		if nw > 0 {
			num := l + 1
			st := th.LineNr
			if o.RelativeNumber && !isCur {
				num = abs(l - w.cur.Line)
			}
			if isCur {
				st = th.CurLineNr
			}
			txt := fmt.Sprintf("%*d ", nw-1, num)
			if tv != nil { // data row numbers; the header has none
				txt = fmt.Sprintf("%*d ", nw-1, l)
				if l == 0 {
					txt = strings.Repeat(" ", nw)
				}
			}
			drawText(s, w.x, y, nw, txt, st)
		}
		if fr != nil {
			fillRow(s, w.x+nw, y, w.w-nw, base)
		}
		// header override (notebook cell markers)
		if dec != nil {
			if text, st, ok := dec.Header(l); ok && !(active && isCur && (e.mode == ModeInsert || e.mode == ModeReplace)) {
				if fs, boxed := frameOf(fr, l); boxed {
					drawBoxEdge(s, w.x+nw, x1, y, '╭', '╮', text, st.Background(bgOf(base)), fs.Background(bgOf(base)))
				} else {
					fillRow(s, w.x+gw, y, textW, st)
					drawText(s, w.x+gw, y, textW, text, st)
				}
				if active && isCur {
					w.curX, w.curY = w.x+gw, y
				}
				row++
				row = e.drawVirtual(w, b, l, row, gw, textW, h)
				continue
			}
		}
		line := b.Line(l)
		// per-rune styles
		var spans []syntax.Span
		if static != nil {
			if l < len(static) {
				spans = static[l]
			}
		} else {
			spans = hl.Spans(b, b.Version, l, notify)
		}
		styles := make([]tcell.Style, len(line)+1)
		for i := range styles {
			styles[i] = base
		}
		for _, sp := range spans {
			if sp.Literal && sp.Style == th.Default {
				continue // only marks a string/comment for colorbrackets
			}
			for i := max(sp.Start, 0); i < sp.End && i < len(line); i++ {
				styles[i] = mergeStyle(base, sp.Style)
			}
		}
		if static == nil && e.options(b).ColorBrackets {
			e.colorBrackets(b, hl, l, spans, styles, notify)
		}
		for _, m := range e.matchesInLine(string(line)) {
			for i := m[0]; i < m[1] && i < len(line); i++ {
				styles[i] = withBg(styles[i], th.Search)
			}
		}
		if f := e.flash; f != nil && f.buf == b {
			for i := 0; i <= len(line); i++ {
				p := Pos{l, i}
				if !p.Less(f.start) && p.Less(f.end) {
					styles[i] = withBg(styles[i], th.Selection)
				}
			}
		}
		if sel.active {
			switch {
			case sel.lines:
				if l >= sel.start.Line && l <= sel.end.Line {
					for i := range styles {
						styles[i] = withBg(styles[i], th.Selection)
					}
				}
			case sel.block:
				if l >= sel.start.Line && l <= sel.end.Line {
					cs, ce := blockCols(line, opRange{vcolStart: sel.vs, vcolEnd: sel.ve, toEOL: w.want == -1}, ts)
					for i := cs; i < ce; i++ {
						styles[i] = withBg(styles[i], th.Selection)
					}
				}
			default:
				for i := 0; i <= len(line); i++ {
					p := Pos{l, i}
					if !p.Less(sel.start) && p.Less(sel.end) {
						styles[i] = withBg(styles[i], th.Selection)
					}
				}
			}
		}
		if hasMatch && matchPos.Line == l && matchPos.Col < len(styles) {
			styles[matchPos.Col] = withBg(styles[matchPos.Col], th.MatchParen).Bold(true)
		}
		if markBr && brPos.Line == l && brPos.Col < len(styles) {
			styles[brPos.Col] = withBg(styles[brPos.Col], th.MatchParen).Bold(true)
		}
		// draw the line on one row, or on several when wrapping
		segs := e.segments(w, l)
		wrap := e.wraps(w)
		first := 0
		if l == w.top {
			first = min(w.skip, len(segs)-1)
		}
		fs, boxed := frameOf(fr, l)
		vc := 0
		for k := first; k < len(segs) && row < h; k++ {
			seg := segs[k]
			y := w.y + row
			if k > first {
				fillRow(s, w.x, y, nw, th.Default) // no number on continuation rows
				fillRow(s, w.x+nw, y, w.w-nw, base)
			}
			fillRow(s, w.x+gw, y, textW, base)
			left := w.left
			if wrap {
				left = seg.vc
			}
			put := func(col int, r rune, st tcell.Style) {
				if col >= left && col-left < textW {
					s.SetContent(w.x+gw+col-left, y, r, nil, st)
				}
			}
			vc = seg.vc
			for i := seg.start; i < seg.end; i++ {
				r := line[i]
				rw := runeWidth(r, vc, ts)
				st := styles[i]
				switch {
				case r == '\t':
					for k := 0; k < rw; k++ {
						put(vc+k, ' ', st)
					}
				case r < 32 || r == 0x7f:
					ctl := th.NonText.Background(bgOf(st))
					put(vc, '^', ctl)
					put(vc+1, r^0x40, ctl)
				default:
					if vc >= left && vc+rw-left <= textW {
						s.SetContent(w.x+gw+vc-left, y, r, nil, st)
					}
				}
				if active && isCur && i == w.cur.Col {
					w.curX, w.curY = w.x+gw+vc-left, y
				}
				vc += rw
			}
			if k == len(segs)-1 {
				if active && isCur && w.cur.Col >= len(line) {
					w.curX, w.curY = w.x+gw+vc-left, y
				}
				// selection past end of line shows one cell (the newline)
				if sel.active && !sel.block {
					p := Pos{l, len(line)}
					if (sel.lines && l >= sel.start.Line && l <= sel.end.Line) || (!sel.lines && !p.Less(sel.start) && p.Less(sel.end)) {
						if x := vc - left; x >= 0 && x < textW {
							s.SetContent(w.x+gw+x, y, ' ', nil, withBg(base, th.Selection))
						}
					}
				}
				// inline annotation (REPL result)
				if a, ok := ann[l]; ok {
					st := th.Virtual.Background(bgOf(base))
					if a.err {
						st = th.Error.Italic(true).Background(bgOf(base))
					}
					x := vc - left + 2
					if x < textW {
						drawText(s, w.x+gw+max(x, 0), y, textW-max(x, 0), a.text, st)
					}
				}
			}
			if boxed {
				s.SetContent(w.x+nw, y, '│', nil, fs.Background(bgOf(base)))
				s.SetContent(x1, y, '│', nil, fs.Background(bgOf(base)))
			}
			row++
		}
		row = e.drawVirtual(w, b, l, row, gw, textW, h)
	}
	for ; row < h; row++ {
		drawText(s, w.x, w.y+row, w.w, "~", th.NonText)
	}
	if w.curX >= w.x+w.w {
		w.curX = w.x + w.w - 1
	}
	e.drawStatus(w)
}

func frameOf(fr framer, l int) (tcell.Style, bool) {
	if fr == nil {
		return tcell.Style{}, false
	}
	return fr.Frame(l)
}

func (e *Editor) drawVirtual(w *Window, b *buffer.Buffer, l, row, gw, textW, h int) int {
	s := e.screen
	nw := e.numberWidth(w)
	x1 := w.x + w.w - 1
	boxes := e.frameWidth(w) > 0
	for _, v := range e.virtualRows(b, l) {
		if row >= h {
			break
		}
		y := w.y + row
		fillRow(s, w.x, y, w.w, e.theme.Default)
		switch {
		case !boxes || v.kind == vPlain:
			fillRow(s, w.x+gw, y, textW, v.style)
			drawText(s, w.x+gw, y, textW, v.text, v.style)
		case v.kind == vBoxTop:
			drawBoxEdge(s, w.x+nw, x1, y, '╭', '╮', v.text, v.style, v.frame)
		case v.kind == vBoxBottom:
			drawBoxEdge(s, w.x+nw, x1, y, '╰', '╯', v.text, v.style, v.frame)
		default:
			fillRow(s, w.x+gw, y, textW, v.style)
			drawText(s, w.x+gw, y, textW, v.text, v.style)
			s.SetContent(w.x+nw, y, '│', nil, v.frame)
			s.SetContent(x1, y, '│', nil, v.frame)
		}
		row++
	}
	return row
}

func bgOf(st tcell.Style) tcell.Color {
	_, bg, _ := st.Decompose()
	return bg
}

func fillRow(s tcell.Screen, x, y, w int, st tcell.Style) {
	for i := 0; i < w; i++ {
		s.SetContent(x+i, y, ' ', nil, st)
	}
}

// drawText draws text clipped to width columns; returns columns used.
func drawText(s tcell.Screen, x, y, width int, text string, st tcell.Style) int {
	col := 0
	for _, r := range text {
		if r == '\t' {
			r = ' '
		}
		if r < 32 || r == 0x7f || r >= 0x80 && r < 0xa0 {
			r = '?' // control characters never reach the terminal
		}
		rw := runewidth.RuneWidth(r)
		if rw == 0 {
			rw = 1
		}
		if col+rw > width {
			break
		}
		s.SetContent(x+col, y, r, nil, st)
		col += rw
	}
	return col
}

var spinner = []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")

func (e *Editor) drawStatus(w *Window) {
	s := e.screen
	th := e.theme
	y := w.y + w.h - 1
	if w.h <= 1 {
		return
	}
	active := w == e.win
	st := th.StatusInactive
	if active {
		st = th.Status
	}
	fillRow(s, w.x, y, w.w, st)
	x := w.x
	if active {
		mode := " " + e.mode.String() + " "
		if e.mode == ModeCmdline && e.cmdline != nil {
			mode = " " + e.cmdline.prevMode.String() + " "
		}
		x += drawText(s, x, y, w.w, mode, th.StatusMode)
	}
	b := w.buf
	name := e.displayName(b)
	flags := ""
	if e.modified(b) {
		flags += " [+]"
	}
	if b.ReadOnly {
		flags += " [RO]"
	}
	// right side
	var parts []string
	if k := e.kernels[b.ID]; k != nil {
		parts = append(parts, k.status())
	}
	if name, _, ok := e.replEntry(b); ok {
		if sess := e.sessions[name]; sess != nil && sess.Alive() {
			parts = append(parts, "λ "+sess.Name())
		}
	}
	if tv := e.tables[b.ID]; tv != nil {
		parts = append(parts, e.tableStatus(w, tv))
	}
	if b.Kind == buffer.KindFile || b.Kind == buffer.KindNotebook || e.explorer[b.ID] != "" {
		if br := e.gitBranch(e.bufferDir(b)); br != "" {
			parts = append(parts, "⎇ "+br)
		}
	}
	if b.Filetype != "" {
		parts = append(parts, b.Filetype)
	}
	if b.CRLF {
		parts = append(parts, "crlf")
	}
	pct := "Top"
	switch {
	case b.LineCount() <= w.textHeight():
		pct = "All"
	case w.top+w.textHeight() >= b.LineCount():
		pct = "Bot"
	case w.top > 0:
		pct = fmt.Sprintf("%d%%", w.top*100/max(b.LineCount()-w.textHeight(), 1))
	}
	if e.tables[b.ID] == nil { // the table part already gives row and column
		parts = append(parts, fmt.Sprintf("%d:%d", w.cur.Line+1, w.cur.Col+1))
	}
	parts = append(parts, pct)
	right := strings.Join(parts, " │ ") + " "
	rw := runewidth.StringWidth(right)
	// the name gets what is left, truncated from the left
	avail := w.w - (x - w.x) - rw - 2 - runewidth.StringWidth(flags)
	if avail < 8 {
		// very narrow window: drop the right side
		right, rw = "", 0
		avail = w.w - (x - w.x) - 2 - runewidth.StringWidth(flags)
	}
	if runewidth.StringWidth(name) > avail && avail > 1 {
		rs := []rune(name)
		for len(rs) > 0 && runewidth.StringWidth(string(rs))+1 > avail {
			rs = rs[1:]
		}
		name = "…" + string(rs)
	}
	x += drawText(s, x, y, w.w-(x-w.x), " "+name+flags+" ", st.Bold(active))
	if rx := w.x + w.w - rw; rw > 0 && rx >= x {
		drawText(s, rx, y, rw, right, st)
	}
}

// displayName returns a short name for a buffer: relative to the working
// directory, or with the home directory abbreviated to ~.
func (e *Editor) displayName(b *buffer.Buffer) string {
	if b.Path == "" {
		return b.Name
	}
	if rel, err := filepath.Rel(e.cwd, b.Path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(b.Path, home+string(filepath.Separator)) {
		return "~" + b.Path[len(home):]
	}
	return b.Path
}

func (e *Editor) drawCmdline(sw, sh int) {
	s := e.screen
	th := e.theme
	y := sh - 1
	fillRow(s, 0, y, sw, th.Default)
	if e.mode == ModeCmdline && e.cmdline != nil {
		c := e.cmdline
		x := drawText(s, 0, y, sw, c.prompt, th.Default.Bold(true))
		drawText(s, x, y, sw-x, string(c.text), th.Default)
		return
	}
	st := th.Default
	switch e.msgStyle {
	case 1:
		st = th.Error
	case 2:
		st = th.Warning
	}
	msg := strings.ReplaceAll(e.msg, "\n", " ")
	if e.mode == ModeInsert && msg == "" {
		msg = "-- INSERT --"
		st = th.Default.Bold(true)
		if e.insert.oneShot {
			msg = "-- (insert) --"
		}
	} else if e.mode == ModeReplace && msg == "" {
		msg, st = "-- REPLACE --", th.Default.Bold(true)
	} else if e.mode.visual() && msg == "" {
		msg, st = "-- "+e.mode.String()+" --", th.Default.Bold(true)
		if e.mode.visual() {
			r := e.visualRange()
			n := r.end.Line - r.start.Line + 1
			if !r.linewise && !r.block && n == 1 {
				msg += fmt.Sprintf("  %d chars", r.end.Col-r.start.Col)
			} else {
				msg += fmt.Sprintf("  %d lines", n)
			}
		}
	}
	right := keymap.Format(e.pending)
	if e.macro.recording {
		right = "recording @" + string(e.macro.reg) + "  " + right
	}
	rw := runewidth.StringWidth(right) + 1
	drawText(s, 0, y, sw-rw-1, msg, st)
	if right != "" {
		drawText(s, sw-rw, y, rw, right, th.Dim)
	}
}

// maxKeyLabel is the widest key label the which-key popup shows; labels
// are kept within it (TestKeyLabelsFitPopup) so none is cut off.
const maxKeyLabel = 32

func (e *Editor) drawWhichKey(sw, sh int) {
	node := e.whichKeyNode()
	if node == nil {
		return
	}
	ents := node.Entries()
	if len(ents) == 0 {
		return
	}
	s := e.screen
	th := e.theme
	// column layout
	keyW, labW := 0, 0
	for _, en := range ents {
		keyW = max(keyW, runewidth.StringWidth(keymap.Format([]keymap.Key{en.Key})))
		labW = max(labW, runewidth.StringWidth(en.Label))
	}
	labW = min(labW, maxKeyLabel)
	colW := keyW + 3 + labW + 3
	cols := max(1, (sw-2)/colW)
	rows := (len(ents) + cols - 1) / cols
	maxRows := max(sh/2-2, 3)
	if rows > maxRows {
		rows = maxRows
	}
	top := sh - 1 - rows - 2
	if top < 0 {
		top = 0
	}
	for y := top; y < sh-1; y++ {
		fillRow(s, 0, y, sw, th.Popup)
	}
	title := " " + keymap.Format(e.pending) + " "
	for x := 0; x < sw; x++ {
		s.SetContent(x, top, '─', nil, th.Border)
	}
	drawText(s, 2, top, sw-4, title, th.PopupKey)
	for i, en := range ents {
		c, r := i/rows, i%rows
		if c >= cols {
			break
		}
		x := 1 + c*colW
		y := top + 1 + r
		k := keymap.Format([]keymap.Key{en.Key})
		drawText(s, x+keyW-runewidth.StringWidth(k), y, keyW, k, th.PopupKey)
		drawText(s, x+keyW+1, y, 2, "→", th.Border)
		st := th.Popup
		if en.Group {
			st = th.PopupGroup
		}
		drawText(s, x+keyW+3, y, labW, en.Label, st)
	}
}

func (e *Editor) drawPicker(sw, sh int) {
	p := e.picker
	s := e.screen
	th := e.theme
	w := min(110, sw-4)
	h := min(30, sh-4)
	if w < 20 || h < 5 {
		w, h = sw, sh
	}
	x0 := (sw - w) / 2
	y0 := (sh - h) / 2
	for y := y0; y < y0+h; y++ {
		fillRow(s, x0, y, w, th.Popup)
	}
	// border
	for x := x0; x < x0+w; x++ {
		s.SetContent(x, y0, '─', nil, th.Border)
		s.SetContent(x, y0+h-1, '─', nil, th.Border)
	}
	for y := y0; y < y0+h; y++ {
		s.SetContent(x0, y, '│', nil, th.Border)
		s.SetContent(x0+w-1, y, '│', nil, th.Border)
	}
	s.SetContent(x0, y0, '╭', nil, th.Border)
	s.SetContent(x0+w-1, y0, '╮', nil, th.Border)
	s.SetContent(x0, y0+h-1, '╰', nil, th.Border)
	s.SetContent(x0+w-1, y0+h-1, '╯', nil, th.Border)
	drawText(s, x0+2, y0, w-4, " "+p.title+" ", th.PopupKey)
	count := fmt.Sprintf(" %d/%d ", len(p.shown), len(p.all))
	if p.loading {
		count = " " + string(spinner[int(time.Since(e.started)/(80*time.Millisecond))%len(spinner)]) + count
	}
	drawText(s, x0+w-2-runewidth.StringWidth(count), y0, w, count, th.Border)
	// input
	ix := drawText(s, x0+2, y0+1, w-4, "> ", th.PopupKey)
	drawText(s, x0+2+ix, y0+1, w-4-ix, string(p.input), th.Popup)
	s.ShowCursor(x0+2+ix+runewidth.StringWidth(string(p.input)), y0+1)
	s.SetCursorStyle(tcell.CursorStyleSteadyBar)
	for x := x0 + 1; x < x0+w-1; x++ {
		s.SetContent(x, y0+2, '─', nil, th.Border)
	}
	listH := h - 4
	if p.sel < p.scroll {
		p.scroll = p.sel
	}
	if p.sel >= p.scroll+listH {
		p.scroll = p.sel - listH + 1
	}
	for i := 0; i < listH && p.scroll+i < len(p.shown); i++ {
		it := p.shown[p.scroll+i]
		y := y0 + 3 + i
		st := th.Popup
		if p.scroll+i == p.sel {
			st = th.PopupSel
			fillRow(s, x0+1, y, w-2, st)
		}
		avail := w - 4
		detailW := 0
		if it.detail != "" {
			detailW = runewidth.StringWidth(it.detail) + 2
			drawText(s, x0+w-2-detailW+1, y, detailW, it.detail, th.Dim.Background(bgOf(st)))
		}
		matched := map[int]bool{}
		for _, m := range it.match {
			matched[m] = true
		}
		col := 0
		for ri, r := range []rune(it.label) {
			rw := max(runewidth.RuneWidth(r), 1)
			if col+rw > avail-detailW {
				break
			}
			cs := st
			if matched[ri] {
				cs = th.PopupKey.Background(bgOf(st))
			}
			if r < 32 {
				r = ' '
			}
			s.SetContent(x0+2+col, y, r, nil, cs)
			col += rw
		}
	}
}

func (e *Editor) drawCompletion(sw, sh int) {
	c := e.insert.compl
	w := e.win
	if w.curX < 0 {
		return
	}
	s := e.screen
	th := e.theme
	n := min(len(c.words), 10)
	width := 0
	for _, wd := range c.words {
		width = max(width, utf8.RuneCountInString(wd))
	}
	width = min(width+2, 40)
	y := w.curY + 1
	if y+n >= sh-1 {
		y = w.curY - n
	}
	x := min(w.curX, sw-width)
	start := 0
	if c.idx >= n {
		start = c.idx - n + 1
	}
	for i := 0; i < n; i++ {
		st := th.Popup
		if start+i == c.idx {
			st = th.PopupSel
		}
		fillRow(s, x, y+i, width, st)
		drawText(s, x+1, y+i, width-1, c.words[start+i], st)
	}
}

// ---------------------------------------------------------------- mouse

func (e *Editor) handleMouse(ev *tcell.EventMouse) {
	mx, my := ev.Position()
	btn := ev.Buttons()
	if e.picker != nil {
		return
	}
	var target *Window
	for _, w := range e.windows() {
		if mx >= w.x && mx < w.x+w.w && my >= w.y && my < w.y+w.h {
			target = w
		}
	}
	if target == nil {
		return
	}
	switch {
	case btn&tcell.WheelUp != 0:
		e.scrollLines(target, -3)
	case btn&tcell.WheelDown != 0:
		e.scrollLines(target, 3)
	case btn&tcell.Button1 != 0:
		if e.mode == ModeCmdline {
			return
		}
		if target != e.win {
			if e.mode == ModeInsert || e.mode == ModeReplace {
				e.exitInsert()
			}
			e.focus(target)
		}
		if my >= target.y+target.textHeight() {
			return
		}
		// map the row to a buffer line, skipping virtual rows
		row := my - target.y
		l := target.top
		for l < target.buf.LineCount() {
			rows := e.lineRows(target, l)
			if row < rows {
				break
			}
			row -= rows
			l++
		}
		left := target.left
		if l >= target.buf.LineCount() {
			l = target.buf.LineCount() - 1
		} else if e.wraps(target) {
			segs := e.segments(target, l)
			k := row
			if l == target.top {
				k += min(target.skip, len(segs)-1)
			}
			left = segs[min(k, len(segs)-1)].vc
		}
		vc := mx - target.x - e.gutterWidth(target) + left
		col := colForDisplay(target.buf.Line(l), max(vc, 0), e.options(target.buf).TabStop)
		e.setCursor(target, Pos{l, col})
	}
}
