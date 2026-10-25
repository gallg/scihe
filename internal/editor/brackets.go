package editor

import (
	"github.com/gdamore/tcell/v2"

	"scihe/internal/buffer"
	"scihe/internal/syntax"
)

// matchParenLines bounds how far the matching-bracket highlight looks
// (it runs on every redraw, also while typing); % searches the whole
// buffer.
const matchParenLines = 3000

// cursorBracket returns the bracket whose pair is highlighted: in normal
// mode the one under the cursor; in insert / replace mode the one just
// before the cursor (what was just typed), else the one under it.
// Visual mode shows no pair; the command line keeps the normal one.
func (e *Editor) cursorBracket(b *buffer.Buffer, cur Pos) (Pos, bool) {
	isBr := func(p Pos) bool {
		_, ok := pairs[b.RuneAt(p)]
		return ok && p.Col < b.LineLen(p.Line)
	}
	switch e.mode {
	case ModeNormal, ModeCmdline: // typing a : command or a search
		return cur, isBr(cur)
	case ModeInsert, ModeReplace:
		if before := (Pos{cur.Line, cur.Col - 1}); cur.Col > 0 && isBr(before) {
			return before, true
		}
		return cur, isBr(cur)
	}
	return cur, false
}

// bracketDepths caches the nesting depth at the start of each line of a
// buffer for colorbrackets. It is filled lazily, only as far as a drawn
// line needs, and an edit invalidates it from the edited line on (see
// onChange), so typing in a big file does not rescan the whole file.
// New syntax highlighting (it decides what is a string or a comment)
// invalidates all of it.
type bracketDepths struct {
	hl        *syntax.Highlighter // the highlighter the depths came from
	hlVersion int
	depth     []int // depth at the start of line i, for i < valid
	valid     int
}

// invalidateBrackets drops the cached depths after line (an edit there
// changes the depth of every later line).
func (e *Editor) invalidateBrackets(b *buffer.Buffer, line int) {
	if bd := e.brackets[b.ID]; bd != nil {
		bd.valid = max(min(bd.valid, line+1), 0)
	}
}

// lineBrackets calls fn for each bracket of line l that is not inside a
// string or comment.
func lineBrackets(line []rune, spans []syntax.Span, fn func(col int, open bool)) {
	si := 0
	for i, r := range line {
		_, isPair := pairs[r]
		if !isPair {
			continue
		}
		for si < len(spans) && spans[si].End <= i {
			si++
		}
		if si < len(spans) && spans[si].Literal && spans[si].Start <= i {
			continue
		}
		fn(i, isOpenBr(r))
	}
}

// cellHeaders returns the "# %%" header lines of a notebook (nil for
// other buffers): their brackets don't count, and every cell starts at
// depth 0, so an unbalanced cell does not colour the ones after it.
func (e *Editor) cellHeaders(b *buffer.Buffer) map[int]bool {
	ks := e.kernels[b.ID]
	if ks == nil || ks.nb == nil {
		return nil
	}
	hs := map[int]bool{}
	for _, r := range ks.cellRanges() {
		hs[r.Header] = true
	}
	return hs
}

// depthAt returns the bracket depth at the start of line l.
func (e *Editor) depthAt(b *buffer.Buffer, hl *syntax.Highlighter, l int, headers map[int]bool, notify func()) int {
	n := b.LineCount()
	if l < 0 || l >= n {
		return 0
	}
	hl.Spans(b, b.Version, 0, notify) // bring the highlighting up to date first
	v := hl.Version()
	bd := e.brackets[b.ID]
	if bd == nil || bd.hl != hl || bd.hlVersion != v {
		bd = &bracketDepths{hl: hl, hlVersion: v}
		e.brackets[b.ID] = bd
	}
	if len(bd.depth) != n {
		d := make([]int, n)
		bd.valid = min(bd.valid, copy(d, bd.depth[:min(bd.valid, len(bd.depth))]))
		bd.depth = d
	}
	for i := bd.valid; i <= l; i++ {
		d := 0
		if i > 0 && !headers[i-1] && !headers[i] {
			d = bd.depth[i-1]
			lineBrackets(b.Line(i-1), hl.Spans(b, b.Version, i-1, notify), func(_ int, open bool) {
				switch {
				case open:
					d++
				case d > 0:
					d--
				}
			})
		}
		bd.depth[i] = d
		bd.valid = i + 1
	}
	return bd.depth[l]
}

// colorBrackets colours the brackets of line l by nesting depth; a
// closing bracket without an opening one gets the error colour.
func (e *Editor) colorBrackets(b *buffer.Buffer, hl *syntax.Highlighter, l int, spans []syntax.Span, styles []tcell.Style, notify func()) {
	th := e.theme
	if len(th.Brackets) == 0 {
		return
	}
	headers := e.cellHeaders(b)
	if headers[l] {
		return
	}
	d := e.depthAt(b, hl, l, headers, notify)
	lineBrackets(b.Line(l), spans, func(col int, open bool) {
		switch {
		case open:
			styles[col] = styles[col].Foreground(th.Brackets[d%len(th.Brackets)])
			d++
		case d > 0:
			d--
			styles[col] = styles[col].Foreground(th.Brackets[d%len(th.Brackets)])
		default:
			styles[col] = styles[col].Foreground(th.BracketError)
		}
	})
}
