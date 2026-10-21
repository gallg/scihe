// Package markdown renders Markdown into styled terminal lines for the
// live preview window.
package markdown

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"

	"scihe/internal/syntax"
)

// Line is one rendered line.
type Line struct {
	Text  []rune
	Spans []syntax.Span
}

// Doc is a rendered document.
type Doc struct {
	Lines []Line
	// Source maps rendered line index to the source line it came from.
	Source []int
}

// String returns the plain text of the document.
func (d *Doc) String() string {
	var sb strings.Builder
	for i, l := range d.Lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(string(l.Text))
	}
	return sb.String()
}

// RenderedLineFor returns the first rendered line that comes from source
// line src or a later one (source lines that render to nothing, such as
// blank lines, map to the next rendered block).
func (d *Doc) RenderedLineFor(src int) int {
	for i, s := range d.Source {
		if s >= src {
			return i
		}
	}
	return max(len(d.Source)-1, 0)
}

type run struct {
	text  string
	style tcell.Style
}

type renderer struct {
	src        []byte
	theme      *syntax.Theme
	doc        *Doc
	lineStarts []int
	curSrc     int

	sHeading [6]tcell.Style
	sCode    tcell.Style
	sLink    tcell.Style
	sDim     tcell.Style
	sQuote   tcell.Style
	sBullet  tcell.Style
}

var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

// Render parses src and lays it out for the given width.
func Render(src []byte, width int, theme *syntax.Theme) *Doc {
	if width < 20 {
		width = 20
	}
	r := &renderer{src: src, theme: theme, doc: &Doc{}}
	r.lineStarts = []int{0}
	for i, c := range src {
		if c == '\n' {
			r.lineStarts = append(r.lineStarts, i+1)
		}
	}
	r.sHeading[0] = theme.Accent(chroma.Keyword).Bold(true).Underline(true)
	r.sHeading[1] = theme.Accent(chroma.NameFunction).Bold(true)
	r.sHeading[2] = theme.Accent(chroma.NameClass).Bold(true)
	for i := 3; i < 6; i++ {
		r.sHeading[i] = theme.Accent(chroma.NameTag).Bold(true)
	}
	r.sCode = theme.Accent(chroma.LiteralString)
	r.sLink = theme.Accent(chroma.NameAttribute).Underline(true)
	r.sDim = theme.Dim
	r.sQuote = theme.Dim.Italic(true)
	r.sBullet = theme.Accent(chroma.Keyword)
	doc := md.Parser().Parse(text.NewReader(src))
	r.blocks(doc, width, nil, nil)
	// drop trailing blank lines
	for len(r.doc.Lines) > 0 && len(r.doc.Lines[len(r.doc.Lines)-1].Text) == 0 {
		r.doc.Lines = r.doc.Lines[:len(r.doc.Lines)-1]
		r.doc.Source = r.doc.Source[:len(r.doc.Source)-1]
	}
	if len(r.doc.Lines) == 0 {
		r.doc.Lines = []Line{{}}
		r.doc.Source = []int{0}
	}
	return r.doc
}

func (r *renderer) srcLine(off int) int {
	return sort.SearchInts(r.lineStarts, off+1) - 1
}

// startOf finds the source offset of a block node.
func (r *renderer) startOf(n ast.Node) (int, bool) {
	if l := n.Lines(); n.Type() == ast.TypeBlock && l != nil && l.Len() > 0 {
		return l.At(0).Start, true
	}
	if t, ok := n.(*ast.Text); ok {
		return t.Segment.Start, true
	}
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if off, ok := r.startOf(c); ok {
			return off, true
		}
	}
	return 0, false
}

func (r *renderer) emit(l Line) {
	r.doc.Lines = append(r.doc.Lines, l)
	r.doc.Source = append(r.doc.Source, r.curSrc)
}

func (r *renderer) blank(prefix []run) {
	if len(r.doc.Lines) == 0 {
		return
	}
	if last := r.doc.Lines[len(r.doc.Lines)-1]; strings.TrimSpace(string(last.Text)) == strings.TrimSpace(runsText(prefix)) {
		return
	}
	r.emit(r.line(prefix))
}

func runsText(rs []run) string {
	var sb strings.Builder
	for _, x := range rs {
		sb.WriteString(x.text)
	}
	return sb.String()
}

func (r *renderer) line(rs []run) Line {
	var l Line
	for _, x := range rs {
		start := len(l.Text)
		l.Text = append(l.Text, []rune(x.text)...)
		if x.style != r.theme.Default {
			l.Spans = append(l.Spans, syntax.Span{Start: start, End: len(l.Text), Style: x.style})
		}
	}
	return l
}

// runeCells is the width of a rune as the editor window draws it:
// zero-width runes (combining marks, joiners) take one cell. Control
// characters never reach here (cleanText removes them).
func runeCells(r rune) int {
	if w := runewidth.RuneWidth(r); w > 0 {
		return w
	}
	return 1
}

func strWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeCells(r)
	}
	return w
}

// cleanText makes inline text safe to lay out: tabs become spaces and
// control characters are dropped, so measured and drawn widths agree.
func cleanText(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < 32 || r == 0x7f || r >= 0x80 && r < 0xa0 }) {
		return s
	}
	var sb strings.Builder
	for _, r := range s {
		switch {
		case r == '\t':
			sb.WriteString("    ")
		case r == '\n':
			sb.WriteRune(r)
		case r < 32 || r == 0x7f || r >= 0x80 && r < 0xa0:
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func width(rs []run) int {
	w := 0
	for _, x := range rs {
		w += strWidth(x.text)
	}
	return w
}

// blocks renders the children of n. first/rest are the line prefixes for
// the first line and subsequent lines.
func (r *renderer) blocks(n ast.Node, w int, first, rest []run) {
	pre := first
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		r.block(c, w, pre, rest)
		pre = rest
	}
}

func (r *renderer) block(n ast.Node, w int, first, rest []run) {
	if off, ok := r.startOf(n); ok {
		r.curSrc = r.srcLine(off)
	}
	avail := w - width(rest)
	switch n := n.(type) {
	case *ast.Heading:
		lvl := n.Level
		if lvl < 1 {
			lvl = 1
		}
		if lvl > 6 {
			lvl = 6
		}
		st := r.sHeading[lvl-1]
		hdr := []run{{strings.Repeat("#", lvl) + " ", r.sDim}}
		runs := r.inlines(n, st)
		r.wrap(runs, avail, append(append([]run{}, first...), hdr...), rest)
		if lvl <= 2 {
			ch := "━"
			if lvl == 2 {
				ch = "─"
			}
			r.emit(r.line(append(append([]run{}, rest...), run{strings.Repeat(ch, max(avail, 1)), r.sDim})))
		}
		r.blank(rest)
	case *ast.Paragraph, *ast.TextBlock:
		r.wrap(r.inlines(n, r.theme.Default), avail, first, rest)
		if _, tight := n.(*ast.TextBlock); !tight {
			r.blank(rest)
		}
	case *ast.ThematicBreak:
		r.emit(r.line(append(append([]run{}, first...), run{strings.Repeat("─", max(avail, 1)), r.sDim})))
		r.blank(rest)
	case *ast.FencedCodeBlock:
		lang := string(n.Language(r.src))
		r.code(n.Lines(), lang, avail, first, rest)
	case *ast.CodeBlock:
		r.code(n.Lines(), "", avail, first, rest)
	case *ast.HTMLBlock:
		lines := n.Lines()
		pre := first
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			s := cleanText(strings.TrimRight(string(seg.Value(r.src)), "\n"))
			r.charWrap([]run{{s, r.sDim}}, avail, pre, rest)
			pre = rest
		}
		if n.HasClosure() {
			s := cleanText(strings.TrimRight(string(n.ClosureLine.Value(r.src)), "\n"))
			r.charWrap([]run{{s, r.sDim}}, avail, pre, rest)
		}
		r.blank(rest)
	case *ast.Blockquote:
		bar := run{"▎ ", r.sQuote}
		r.blocks(n, w, append(append([]run{}, first...), bar), append(append([]run{}, rest...), bar))
		r.blank(rest)
	case *ast.List:
		idx := n.Start
		depth := 0
		for p := n.Parent(); p != nil; p = p.Parent() {
			if _, ok := p.(*ast.List); ok {
				depth++
			}
		}
		bullets := []string{"•", "◦", "▪"}
		pre := first
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			var marker string
			if n.IsOrdered() {
				marker = fmt.Sprintf("%d. ", idx)
				idx++
			} else {
				marker = bullets[depth%len(bullets)] + " "
			}
			pad := strings.Repeat(" ", strWidth(marker))
			r.blocks(c, w, append(append([]run{}, pre...), run{marker, r.sBullet}), append(append([]run{}, rest...), run{pad, r.theme.Default}))
			pre = rest
		}
		r.blank(rest)
	case *east.Table:
		r.table(n, avail-max(width(first)-width(rest), 0), first, rest)
		r.blank(rest)
	default:
		r.blocks(n, w, first, rest)
	}
}

func (r *renderer) code(lines *text.Segments, lang string, avail int, first, rest []run) {
	var buf bytes.Buffer
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		buf.Write(seg.Value(r.src))
	}
	// expand tabs before highlighting so span columns match the text
	body := cleanText(strings.TrimRight(buf.String(), "\n"))
	codeLines := strings.Split(body, "\n")
	ft := strings.ToLower(lang)
	spans := syntax.HighlightLines(syntax.Lexer(ft), body, len(codeLines), r.theme)
	label := "╭─"
	if lang != "" {
		if strWidth(lang) > avail-4 {
			lang = string(splitRunesAt([]rune(lang), max(avail-5, 1))[0]) + "…"
		}
		label += " " + lang + " "
	}
	r.emit(r.line(append(append([]run{}, first...), run{label, r.sDim})))
	start := r.curSrc
	frame := append(append([]run{}, rest...), run{"│ ", r.sDim})
	more := append(append([]run{}, rest...), run{"│", r.sDim}, run{"↪", r.sDim})
	textW := max(avail-2, 1) // after "│ " (the continuation "│↪" is as wide)
	for i, cl := range codeLines {
		r.curSrc = start + i
		rs := []rune(cl)
		// cut the line at the width; the pieces keep their colours
		pieceStart := 0
		for k, pc := range splitRunesAt(rs, textW) {
			pre := frame
			if k > 0 {
				pre = more
			}
			l := r.line(pre)
			off := len(l.Text)
			l.Text = append(l.Text, pc...)
			for _, sp := range spans[i] {
				a, b := max(sp.Start, pieceStart), min(sp.End, pieceStart+len(pc))
				if a < b {
					l.Spans = append(l.Spans, syntax.Span{Start: a - pieceStart + off, End: b - pieceStart + off, Style: sp.Style})
				}
			}
			r.emit(l)
			pieceStart += len(pc)
		}
	}
	r.curSrc = start
	r.emit(r.line(append(append([]run{}, rest...), run{"╰─", r.sDim})))
	r.blank(rest)
}

// splitRunesAt cuts a line into pieces at most w display columns wide.
func splitRunesAt(rs []rune, w int) [][]rune {
	var out [][]rune
	start, cw := 0, 0
	for i, c := range rs {
		rw := runeCells(c)
		if cw+rw > w && i > start {
			out = append(out, rs[start:i])
			start, cw = i, 0
		}
		cw += rw
	}
	return append(out, rs[start:])
}

// charWrap emits runs cut at the width (no word boundaries: raw HTML).
func (r *renderer) charWrap(rs []run, avail int, first, rest []run) {
	for k, pc := range splitRunsAt(rs, max(avail, 1), max(avail, 1)) {
		pre := rest
		if k == 0 {
			pre = first
		}
		r.emit(r.line(append(append([]run{}, pre...), pc...)))
	}
}

// tableMinCol is the narrowest a column is squeezed to before a table
// is shown stacked (one block per row) instead.
const tableMinCol = 4

func (r *renderer) table(t *east.Table, avail int, first, rest []run) {
	// the first emitted row gets the first-line prefix (e.g. a bullet)
	pre := first
	px := func() []run {
		p := pre
		pre = rest
		return p
	}
	var rows [][][]run
	header := 0
	for row := t.FirstChild(); row != nil; row = row.NextSibling() {
		var cells [][]run
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			st := r.theme.Default
			if _, ok := row.(*east.TableHeader); ok {
				st = st.Bold(true)
			}
			cells = append(cells, flatten(r.inlines(c, st)))
		}
		if _, ok := row.(*east.TableHeader); ok {
			header = len(rows) + 1
		}
		rows = append(rows, cells)
	}
	ncol := 0
	for _, row := range rows {
		ncol = max(ncol, len(row))
	}
	if ncol == 0 {
		return
	}
	natural := make([]int, ncol)
	for _, row := range rows {
		for i, c := range row {
			natural[i] = max(natural[i], width(c), 1)
		}
	}
	// "│ cell │ cell │": one border plus 3 columns per cell
	budget := avail - 1 - 3*ncol
	widths := fitColumns(natural, budget)
	if widths == nil {
		r.stackedTable(rows, header, avail, px, rest)
		return
	}
	border := func(l, m, rr string) {
		var sb strings.Builder
		sb.WriteString(l)
		for i, wd := range widths {
			sb.WriteString(strings.Repeat("─", wd+2))
			if i < ncol-1 {
				sb.WriteString(m)
			}
		}
		sb.WriteString(rr)
		r.emit(r.line(append(append([]run{}, px()...), run{sb.String(), r.sDim})))
	}
	border("┌", "┬", "┐")
	for ri, row := range rows {
		// each cell is wrapped inside its column; the row is as tall as
		// its tallest cell
		cellLines := make([][][]run, ncol)
		height := 1
		for i := 0; i < ncol; i++ {
			var cell []run
			if i < len(row) {
				cell = row[i]
			}
			cellLines[i] = layoutRuns(cell, widths[i], widths[i], r.theme.Default)
			height = max(height, len(cellLines[i]))
		}
		for li := 0; li < height; li++ {
			rs := append([]run{}, px()...)
			rs = append(rs, run{"│", r.sDim})
			for i := 0; i < ncol; i++ {
				var cell []run
				if li < len(cellLines[i]) {
					cell = cellLines[i][li]
				}
				pad := max(widths[i]-width(cell), 0)
				left := 0
				if i < len(t.Alignments) {
					switch t.Alignments[i] {
					case east.AlignRight:
						left = pad
					case east.AlignCenter:
						left = pad / 2
					}
				}
				rs = append(rs, run{" " + strings.Repeat(" ", left), r.theme.Default})
				rs = append(rs, cell...)
				rs = append(rs, run{strings.Repeat(" ", pad-left) + " ", r.theme.Default}, run{"│", r.sDim})
			}
			r.emit(r.line(rs))
		}
		if ri+1 == header && ri < len(rows)-1 {
			border("├", "┼", "┤")
		}
	}
	border("└", "┴", "┘")
}

// fitColumns gives each column its natural width when the table fits in
// budget columns; otherwise narrow columns keep their width and the rest
// of the budget is shared equally by the wide ones. It returns nil when
// the columns cannot all get tableMinCol (or their natural width).
func fitColumns(natural []int, budget int) []int {
	total := 0
	for _, n := range natural {
		total += n
	}
	widths := append([]int(nil), natural...)
	if total <= budget {
		return widths
	}
	need := 0
	for _, n := range natural {
		need += min(n, tableMinCol)
	}
	if need > budget {
		return nil
	}
	settled := make([]bool, len(natural))
	left, open := budget, len(natural)
	for changed := true; changed && open > 0; {
		changed = false
		share := left / open
		for i, n := range natural {
			if !settled[i] && n <= share {
				settled[i], changed = true, true
				widths[i] = n
				left -= n
				open--
			}
		}
	}
	if open == 0 {
		return widths
	}
	share, extra := left/open, left%open
	for i := range natural {
		if !settled[i] {
			widths[i] = max(share, tableMinCol)
			if extra > 0 {
				widths[i]++
				extra--
			}
		}
	}
	return widths
}

// stackedTable shows a table too wide for its columns as one block per
// row: "header: value" lines, wrapped, between thin rules.
func (r *renderer) stackedTable(rows [][][]run, header, avail int, px func() []run, rest []run) {
	var names [][]run
	data := rows
	if header > 0 {
		names, data = rows[header-1], append(append([][][]run{}, rows[:header-1]...), rows[header:]...)
	}
	rule := func() {
		r.emit(r.line(append(append([]run{}, px()...), run{strings.Repeat("─", max(avail, 1)), r.sDim})))
	}
	rule()
	for _, row := range data {
		for i, cell := range row {
			name := []run{{fmt.Sprintf("%d", i+1), r.theme.Default.Bold(true)}}
			if i < len(names) && width(names[i]) > 0 {
				name = names[i]
			}
			label := append(append([]run{}, name...), run{": ", r.sDim})
			indent := append(append([]run{}, rest...), run{"  ", r.theme.Default})
			first := append(append([]run{}, px()...), label...)
			if width(label) > avail/2 {
				// a long header gets its own (wrapped) line; the value follows
				for k, l := range layoutRuns(label, avail, avail, r.theme.Default) {
					pre := rest
					if k == 0 {
						pre = first[:len(first)-len(label)]
					}
					r.emit(r.line(append(append([]run{}, pre...), l...)))
				}
				first = indent
				label = nil
			}
			lines := layoutRuns(cell, max(avail-width(label), 1), max(avail-2, 1), r.theme.Default)
			for k, l := range lines {
				pre := indent
				if k == 0 {
					pre = first
				}
				r.emit(r.line(append(append([]run{}, pre...), l...)))
			}
		}
		rule()
	}
}

// flatten replaces newlines in a run list by spaces (for table cells).
func flatten(rs []run) []run {
	out := make([]run, len(rs))
	for i, x := range rs {
		out[i] = run{strings.ReplaceAll(x.text, "\n", " "), x.style}
	}
	return out
}

// inlines collects the styled text of the inline children of n.
func (r *renderer) inlines(n ast.Node, st tcell.Style) []run {
	var out []run
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, r.inline(c, st)...)
	}
	return out
}

func (r *renderer) inline(n ast.Node, st tcell.Style) []run {
	switch n := n.(type) {
	case *ast.Text:
		out := []run{{cleanText(string(n.Value(r.src))), st}}
		if n.HardLineBreak() {
			out = append(out, run{"\n", st})
		} else if n.SoftLineBreak() {
			out = append(out, run{" ", st})
		}
		return out
	case *ast.String:
		return []run{{cleanText(string(n.Value)), st}}
	case *ast.CodeSpan:
		var sb strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch t := c.(type) {
			case *ast.Text:
				sb.Write(t.Value(r.src))
			case *ast.String:
				sb.Write(t.Value)
			}
		}
		return []run{{cleanText(sb.String()), r.sCode}}
	case *ast.Emphasis:
		if n.Level >= 2 {
			return r.inlines(n, st.Bold(true))
		}
		return r.inlines(n, st.Italic(true))
	case *east.Strikethrough:
		return r.inlines(n, st.StrikeThrough(true))
	case *ast.Link:
		out := r.inlines(n, r.sLink)
		dest := string(n.Destination)
		if dest != "" && dest != runsText(out) {
			out = append(out, run{" (" + cleanText(dest) + ")", r.sDim})
		}
		return out
	case *ast.AutoLink:
		return []run{{cleanText(string(n.URL(r.src))), r.sLink}}
	case *ast.Image:
		alt := runsText(r.inlines(n, st))
		return []run{{"[image: " + alt + "]", r.sDim}}
	case *ast.RawHTML:
		var sb strings.Builder
		for i := 0; i < n.Segments.Len(); i++ {
			s := n.Segments.At(i)
			sb.Write(s.Value(r.src))
		}
		return []run{{sb.String(), r.sDim}}
	case *east.TaskCheckBox:
		if n.IsChecked {
			return []run{{"☑ ", r.sBullet}}
		}
		return []run{{"☐ ", r.sBullet}}
	default:
		return r.inlines(n, st)
	}
}

// word is a run of non-blank text kept together by the word wrapper.
type word struct {
	parts []run
	width int
	brk   bool // forced line break before this word
}

// splitWords cuts runs at blanks; "\n" forces a line break.
func splitWords(rs []run) []word {
	var words []word
	var cur word
	flush := func() {
		if len(cur.parts) > 0 || cur.brk {
			words = append(words, cur)
		}
		cur = word{}
	}
	for _, x := range rs {
		s := x.text
		for len(s) > 0 {
			i := strings.IndexAny(s, " \n")
			if i < 0 {
				cur.parts = append(cur.parts, run{s, x.style})
				cur.width += strWidth(s)
				break
			}
			if i > 0 {
				cur.parts = append(cur.parts, run{s[:i], x.style})
				cur.width += strWidth(s[:i])
			}
			nl := s[i] == '\n'
			flush()
			if nl {
				cur.brk = true
			}
			s = s[i+1:]
		}
	}
	flush()
	return words
}

// splitRunsAt cuts runs into pieces: the first at most first display
// columns wide, the others at most w (each piece has at least one rune).
func splitRunsAt(rs []run, first, w int) [][]run {
	var out [][]run
	var cur []run
	cw := 0
	limit := first
	for _, x := range rs {
		var sb strings.Builder
		for _, c := range x.text {
			rw := runeCells(c)
			if cw+rw > limit && cw > 0 {
				if sb.Len() > 0 {
					cur = append(cur, run{sb.String(), x.style})
					sb.Reset()
				}
				out = append(out, cur)
				cur, cw, limit = nil, 0, w
			}
			sb.WriteRune(c)
			cw += rw
		}
		if sb.Len() > 0 {
			cur = append(cur, run{sb.String(), x.style})
		}
	}
	if len(cur) > 0 || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

// layoutRuns word-wraps runs into lines: the first line has firstW
// columns, the others w. Words wider than a line are split; blank is
// the style of the spaces put between words.
func layoutRuns(rs []run, firstW, w int, blank tcell.Style) [][]run {
	w = max(w, 1)
	firstW = max(firstW, 1)
	var lines [][]run
	var line []run
	lw, empty := 0, true
	limit := func() int {
		if len(lines) == 0 {
			return firstW
		}
		return w
	}
	newLine := func() {
		lines = append(lines, line)
		line, lw, empty = nil, 0, true
	}
	for _, wd := range splitWords(rs) {
		if wd.brk && !empty {
			newLine()
		}
		if len(wd.parts) == 0 {
			continue
		}
		if !empty && lw+1+wd.width > limit() {
			newLine()
		}
		if wd.width > limit() {
			// wider than a line (a URL, a path): cut it into pieces
			for k, pc := range splitRunsAt(wd.parts, limit(), w) {
				if k > 0 {
					newLine()
				}
				line = append(line, pc...)
				lw = width(pc)
				empty = false
			}
			continue
		}
		if !empty {
			line = append(line, run{" ", blank})
			lw++
		}
		line = append(line, wd.parts...)
		lw += wd.width
		empty = false
	}
	if !empty || len(lines) == 0 {
		lines = append(lines, line)
	}
	return lines
}

// wrap word-wraps runs to w columns after the line prefixes and emits
// the lines (the first prefix may be wider than the others).
func (r *renderer) wrap(rs []run, w int, first, rest []run) {
	if w < 10 {
		w = 10
	}
	extra := max(width(first)-width(rest), 0)
	for i, l := range layoutRuns(rs, w-extra, w, r.theme.Default) {
		pre := rest
		if i == 0 {
			pre = first
		}
		r.emit(r.line(append(append([]run{}, pre...), l...)))
	}
}
