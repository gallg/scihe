package editor

import (
	"errors"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/gdamore/tcell/v2"

	"scihe/internal/buffer"
	"scihe/internal/syntax"
)

// mdRender is a read-only view of a markdown buffer laid out as a page
// (mdlayout.go). It takes the place of the source in the window until
// rendering is switched off, and is laid out again when the source
// changes, the window is resized or 'wrap' is toggled: the view shares
// the options of its source.
type mdRender struct {
	src     *buffer.Buffer
	buf     *buffer.Buffer
	version int
	width   int
	wrap    bool
	theme   *syntax.Theme
	lines   []int           // the source line of each row
	text    []bool          // rows where a source line's text starts (not its raised scripts)
	imgs    map[int][]mdImg // images per row, in runes
}

func isMarkdown(b *buffer.Buffer) bool {
	return b.Filetype == "markdown"
}

// exMdRender toggles the rendered view in the current window.
func exMdRender(e *Editor, a *exArgs) error {
	w := e.win
	if r := e.renders[w.buf.ID]; r != nil {
		e.leaveRender(r)
		return nil
	}
	src := w.buf
	if !isMarkdown(src) {
		return errors.New("not a markdown buffer")
	}
	var r *mdRender
	for _, x := range e.renders {
		if x.src == src {
			r = x // already shown in another window
		}
	}
	if r == nil {
		rb := buffer.New("render: " + src.Name)
		rb.Kind = buffer.KindPreview
		rb.Filetype = src.Filetype
		rb.ReadOnly = true
		e.addBuffer(rb)
		e.opts[rb.ID] = e.options(src) // wrap and the rest follow the source
		r = &mdRender{src: src, buf: rb, version: -1}
		e.renders[rb.ID] = r
		e.refreshRender(r, e.textWidth(w))
	}
	cur, alt := w.cur, w.alt
	e.setBuffer(w, r.buf)
	w.alt = alt
	e.setCursor(w, Pos{r.rowOf(cur.Line), 0})
	e.infof("rendered view (read-only): <leader>mr to edit")
	return nil
}

// leaveRender puts the source back in every window showing the view.
func (e *Editor) leaveRender(r *mdRender) {
	e.forWindows(func(w *Window) {
		if w.buf == r.buf {
			l := r.srcOf(w.cur.Line)
			alt := w.alt
			e.setBuffer(w, r.src)
			w.alt = alt
			e.setCursor(w, r.src.Clamp(Pos{l, 0}))
		}
	})
	e.dropRender(r)
}

// linkRenderOptions gives each rendered view the options of its source
// again (after they were made anew, as :setfiletype does).
func (e *Editor) linkRenderOptions() {
	for _, r := range e.renders {
		e.opts[r.buf.ID] = e.options(r.src)
	}
}

func (e *Editor) dropRender(r *mdRender) {
	delete(e.renders, r.buf.ID)
	if e.bufferAlive(r.buf) {
		e.deleteBuffer(r.buf, true)
	}
}

// updateRenders follows changes of the sources (edits, reloads), of
// the width of the windows, of 'wrap' and of the theme.
func (e *Editor) updateRenders() {
	for _, r := range e.renders {
		if !e.bufferAlive(r.src) {
			e.dropRender(r)
			continue
		}
		width := 0
		e.forWindows(func(w *Window) {
			if w.buf == r.buf && (width == 0 || e.textWidth(w) < width) {
				width = e.textWidth(w)
			}
		})
		if width == 0 {
			width = r.width // not shown: keep the layout
		}
		if r.version != r.src.Version || width != r.width || e.options(r.src).Wrap != r.wrap || e.theme != r.theme {
			e.refreshRender(r, width)
		}
	}
}

// srcOf is the source line shown on row l.
func (r *mdRender) srcOf(l int) int {
	if len(r.lines) == 0 {
		return 0
	}
	return r.lines[min(max(l, 0), len(r.lines)-1)]
}

// rowOf is the row where the text of source line l (or of the line
// before it) starts.
func (r *mdRender) rowOf(l int) int {
	row := 0
	for i, s := range r.lines {
		if s > l {
			break
		}
		if r.text[i] {
			row = i
		}
	}
	return row
}

func (e *Editor) refreshRender(r *mdRender, width int) {
	// keep each cursor on its source line (and row within it)
	type mark struct {
		w             *Window
		src, off, col int
	}
	var marks []mark
	if r.lines != nil {
		e.forWindows(func(w *Window) {
			if w.buf == r.buf {
				s := r.srcOf(w.cur.Line)
				marks = append(marks, mark{w, s, w.cur.Line - r.rowOf(s), w.cur.Col})
			}
		})
	}
	r.width, r.wrap, r.theme = width, e.options(r.src).Wrap, e.theme
	rows := layoutMarkdown(r.src.Text(), width, r.wrap)
	lines := make([]string, len(rows))
	spans := make([][]syntax.Span, len(rows))
	r.lines = make([]int, len(rows))
	r.text = make([]bool, len(rows))
	r.imgs = map[int][]mdImg{}
	for i, row := range rows {
		r.lines[i] = row.src
		// the first row of a source line not above its baseline
		if !row.above && (i == 0 || row.src != rows[i-1].src || !r.textBefore(i)) {
			r.text[i] = true
		}
		// trailing blanks go; a cell's rune index for spans and images
		cells := row.cells
		for len(cells) > 0 && cells[len(cells)-1] == blankCell {
			cells = cells[:len(cells)-1]
		}
		var sb strings.Builder
		runeAt := make([]int, len(row.cells)+1)
		n := 0
		for k, c := range cells {
			runeAt[k] = n
			if c.s == "" {
				continue
			}
			sb.WriteString(c.s)
			if c.st != 0 {
				st := e.mdTermStyle(c.st)
				if m := len(spans[i]); m > 0 && spans[i][m-1].End == n && spans[i][m-1].Style == st {
					spans[i][m-1].End = n + 1
				} else {
					spans[i] = append(spans[i], syntax.Span{Start: n, End: n + 1, Style: st})
				}
			}
			n++
		}
		for k := len(cells); k <= len(row.cells); k++ {
			runeAt[k] = n
		}
		lines[i] = sb.String()
		for _, im := range row.imgs {
			r.imgs[i] = append(r.imgs[i], mdImg{runeAt[min(im.start, len(row.cells))], runeAt[min(im.end, len(row.cells))], im.target})
		}
	}
	r.buf.SetText(strings.Join(lines, "\n"))
	e.static[r.buf.ID] = spans
	r.version = r.src.Version
	for _, m := range marks {
		first := r.rowOf(m.src)
		l := first
		for l+1 < len(r.lines) && l-first < m.off && r.lines[l+1] == r.lines[first] {
			l++
		}
		for l > 0 && l-first > m.off && r.lines[l-1] == r.lines[first] {
			l-- // it was on a raised row
		}
		m.w.cur = Pos{l, m.col}
	}
	e.forWindows(func(w *Window) {
		if w.buf == r.buf {
			e.clampCursor(w)
		}
	})
}

// textBefore reports a text row of the source line of row i before it.
func (r *mdRender) textBefore(i int) bool {
	for k := i - 1; k >= 0 && r.lines[k] == r.lines[i]; k-- {
		if r.text[k] {
			return true
		}
	}
	return false
}

// mdTermStyle turns the style flags of a cell into a terminal style.
func (e *Editor) mdTermStyle(st mdStyle) tcell.Style {
	th := e.theme
	fg := func(s tcell.Style) tcell.Style {
		f, _, _ := s.Decompose()
		return th.Default.Foreground(f)
	}
	s := th.Default
	switch {
	case st&msCode != 0:
		s = fg(th.TokenStyle(chroma.LiteralString))
	case st&msLink != 0:
		s = fg(th.TokenStyle(chroma.NameFunction)).Underline(true)
	case st&msError != 0:
		s = fg(th.Error)
	case st&msWarn != 0:
		s = fg(th.Warning)
	case st&msInfo != 0:
		s = fg(th.Info)
	case st&msHeading != 0:
		s = fg(th.TokenStyle(chroma.Keyword))
	case st&msTask != 0:
		s = fg(th.TokenStyle(chroma.NameBuiltin))
	case st&msBorder != 0:
		s = fg(th.Border)
	case st&msDim != 0:
		s = fg(th.Dim)
	}
	return s.Bold(st&msBold != 0).Italic(st&msItalic != 0).Underline(st&(msUnderline|msLink) != 0).StrikeThrough(st&msStrike != 0)
}
