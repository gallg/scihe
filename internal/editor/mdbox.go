package editor

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// Boxes for the rendered markdown view: a rectangle of terminal cells
// with a baseline row, put side by side (aligned on the baseline) or
// stacked. Formulas (mdformula.go) and the text around them
// (mdlayout.go) are laid out with them, as TeX does with its boxes.

// mdStyle is a set of style flags of a cell, turned into colours and
// attributes with the theme when the view is drawn (mdrender.go).
type mdStyle uint32

const (
	msBold mdStyle = 1 << iota
	msItalic
	msUnderline
	msStrike
	msCode
	msLink
	msMath
	msHeading
	msDim
	msBorder
	msInfo
	msWarn
	msError
	msTask
)

// mcell is one terminal cell; a wide character is followed by a cell
// with s == "" (its right half).
type mcell struct {
	s  string
	st mdStyle
}

var blankCell = mcell{" ", 0}

// box is a rectangle of cells; every row has the same width. base is
// the row aligned with the text line around it.
type box struct {
	rows [][]mcell
	base int
}

func (b box) height() int { return len(b.rows) }

func (b box) width() int {
	if len(b.rows) == 0 {
		return 0
	}
	return len(b.rows[0])
}

// cellsOf splits s into cells (control characters as blanks).
func cellsOf(s string, st mdStyle) []mcell {
	var out []mcell
	for _, r := range s {
		if r < 32 || r == 0x7f || r >= 0x80 && r < 0xa0 {
			r = ' ' // control characters (C1 too) never reach the terminal
		}
		w := runewidth.RuneWidth(r)
		if w == 0 {
			w = 1 // as the editor draws them (runeWidth)
		}
		out = append(out, mcell{string(r), st})
		if w == 2 {
			out = append(out, mcell{"", st})
		}
	}
	return out
}

// textBox is one row of text.
func textBox(s string, st mdStyle) box {
	return box{rows: [][]mcell{cellsOf(s, st)}}
}

func blankBox(w, h int) box {
	rows := make([][]mcell, h)
	for i := range rows {
		rows[i] = blankRow(w)
	}
	return box{rows: rows}
}

func blankRow(w int) []mcell {
	row := make([]mcell, max(w, 0))
	for i := range row {
		row[i] = blankCell
	}
	return row
}

// placed is a box at column x whose baseline is y rows below the
// baseline of the result (negative is above).
type placed struct {
	b    box
	x, y int
}

// compose draws boxes on one canvas; the canvas baseline is y = 0.
// Later boxes cover earlier ones only where they have a non-blank cell.
func compose(items []placed) box {
	top, bottom, width := 0, 0, 0
	for _, p := range items {
		if p.b.height() == 0 {
			continue
		}
		top = min(top, p.y-p.b.base)
		bottom = max(bottom, p.y-p.b.base+p.b.height()-1)
		width = max(width, p.x+p.b.width())
	}
	out := blankBox(width, bottom-top+1)
	out.base = -top
	for _, p := range items {
		for i, row := range p.b.rows {
			y := p.y - p.b.base + i - top
			for j, c := range row {
				if c != blankCell || out.rows[y][p.x+j] == blankCell {
					out.rows[y][p.x+j] = c
				}
			}
		}
	}
	return out
}

// hcat puts boxes side by side, aligned on their baselines.
func hcat(bs ...box) box {
	var items []placed
	x := 0
	for _, b := range bs {
		items = append(items, placed{b, x, 0})
		x += b.width()
	}
	if len(items) == 0 {
		return box{rows: [][]mcell{{}}}
	}
	return compose(items)
}

// vstack stacks boxes centred ('c'), left ('l') or right ('r') aligned;
// the baseline is that of box baseOf.
func vstack(bs []box, baseOf int, align byte) box {
	w := 0
	for _, b := range bs {
		w = max(w, b.width())
	}
	out := box{}
	for i, b := range bs {
		if i == baseOf {
			out.base = len(out.rows) + b.base
		}
		for _, r := range padBox(b, w, align).rows {
			out.rows = append(out.rows, r)
		}
	}
	return out
}

// padBox widens b to w columns.
func padBox(b box, w int, align byte) box {
	extra := w - b.width()
	if extra <= 0 {
		return b
	}
	left := 0
	switch align {
	case 'c':
		left = extra / 2
	case 'r':
		left = extra
	}
	out := box{base: b.base, rows: make([][]mcell, len(b.rows))}
	for i, r := range b.rows {
		row := blankRow(left)
		row = append(row, r...)
		out.rows[i] = append(row, blankRow(extra-left)...)
	}
	return out
}

// restyle adds st to every non-blank cell of b.
func restyle(b box, st mdStyle) box {
	out := box{base: b.base, rows: make([][]mcell, len(b.rows))}
	for i, r := range b.rows {
		out.rows[i] = make([]mcell, len(r))
		for j, c := range r {
			if c != blankCell {
				c.st |= st
			}
			out.rows[i][j] = c
		}
	}
	return out
}

// rowString is the text of a row of cells.
func rowString(row []mcell) string {
	var sb strings.Builder
	for _, c := range row {
		sb.WriteString(c.s)
	}
	return sb.String()
}

// boxString is the text of b, one line per row, trailing blanks cut
// (for tests and the linear fallback).
func boxString(b box) string {
	lines := make([]string, len(b.rows))
	for i, r := range b.rows {
		lines[i] = strings.TrimRight(rowString(r), " ")
	}
	return strings.Join(lines, "\n")
}
