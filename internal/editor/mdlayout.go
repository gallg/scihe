package editor

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// The rendered markdown view laid out as a page: markers are gone,
// emphasis, code and links take their styles, headings stand out,
// paragraphs flow to the width of the window with their formulas
// (mdformula.go) set in the text, scripts are raised and lowered, lists
// hang, quotes and alerts have their bar and tables fit the window,
// their cells wrapped. Without 'wrap' the lines of the source are kept
// and nothing is narrowed (the view scrolls sideways). Images are shown
// by name and opened with <leader>mI.

// mrow is one line of the view: its cells, the source line it shows
// and the images on it.
type mrow struct {
	cells []mcell
	src   int
	imgs  []mdImg
	above bool // above the baseline of its line (raised scripts)
}

// mdImg is an image on a row: cells [start, end) and what it shows.
type mdImg struct {
	start, end int
	target     string
}

type mdLayout struct {
	wrap bool
	list int // depth of the list being laid out (its bullet)
}

// mdNoWrap is the width given to text when lines are not wrapped.
const mdNoWrap = 1 << 30

// layoutMarkdown lays out a markdown text for a window width columns
// wide.
func layoutMarkdown(src string, width int, wrap bool) []mrow {
	lines := strings.Split(src, "\n")
	srcs := make([]int, len(lines))
	for i := range srcs {
		srcs[i] = i
	}
	L := &mdLayout{wrap: wrap}
	var rows []mrow
	start := frontMatterEnd(lines)
	for i := 0; i < start; i++ {
		rows = append(rows, mrow{cells: cellsOf(lines[i], msDim), src: i})
	}
	rows = append(rows, L.blocks(lines[start:], srcs[start:], max(width, 8))...)
	if len(rows) == 0 {
		rows = append(rows, mrow{})
	}
	return rows
}

// flowWidth is the width text flows to.
func (L *mdLayout) flowWidth(width int) int {
	if !L.wrap {
		return mdNoWrap
	}
	return max(width, 1)
}

var (
	mdATXRe     = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$`)
	mdItemRe    = regexp.MustCompile(`^( *)([-*+]|\d{1,9}[.)])( +|$)`)
	mdTaskBoxRe = regexp.MustCompile(`^\[([ xX])\](?: +|$)`)
)

// displayMathAt reports a line opening a display formula.
func displayMathAt(l string) bool {
	return strings.HasPrefix(strings.TrimLeft(l, " "), "$$")
}

// blockStart reports a line that ends a paragraph.
func (L *mdLayout) blockStart(lines []string, i int) bool {
	l := lines[i]
	t := strings.TrimSpace(l)
	return t == "" || mdATXRe.MatchString(l) || strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") ||
		indent(l) <= 3 && strings.HasPrefix(t, ">") || isRule(l) || mdItemRe.MatchString(expandTabs(l)) ||
		tableLen(lines, i) > 0 || displayMathAt(l) || strings.HasPrefix(t, "<!--")
}

// blocks lays out lines (with their source lines) width columns wide.
func (L *mdLayout) blocks(lines []string, srcs []int, width int) []mrow {
	width = max(width, 1)                   // deep quotes and lists leave little room, never less than none
	lines = append([]string(nil), lines...) // a display formula may leave text to read
	var out []mrow
	prevBlank := false
	for i := 0; i < len(lines); {
		l := lines[i]
		t := strings.TrimSpace(l)
		blank := t == ""
		switch {
		case blank:
			if !prevBlank && len(out) > 0 {
				out = append(out, mrow{src: srcs[i]})
			}
			i++
		case strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~"):
			fence := ""
			fenceStep(t, &fence)
			out = append(out, mrow{cells: cellsOf(t, msDim), src: srcs[i]})
			i++
			for i < len(lines) {
				if fenceStep(strings.TrimSpace(lines[i]), &fence) {
					out = append(out, mrow{cells: cellsOf(strings.TrimSpace(lines[i]), msDim), src: srcs[i]})
					i++
					break
				}
				out = append(out, mrow{cells: cellsOf(expandTabs(lines[i]), msCode), src: srcs[i]})
				i++
			}
		case strings.HasPrefix(t, "<!--"):
			for ; i < len(lines); i++ {
				out = append(out, mrow{cells: cellsOf(lines[i], msDim), src: srcs[i]})
				if strings.Contains(lines[i], "-->") {
					i++
					break
				}
			}
		case indent(l) >= 4 && (len(out) == 0 || prevBlank):
			for ; i < len(lines) && (indent(lines[i]) >= 4 || strings.TrimSpace(lines[i]) == "" && i+1 < len(lines) && indent(lines[i+1]) >= 4); i++ {
				code := expandTabs(lines[i])
				out = append(out, mrow{cells: cellsOf(code[min(4, len(code)):], msCode), src: srcs[i]})
			}
		case displayMathAt(l):
			rows, next, ok := L.displayMath(lines, srcs, i, width)
			if !ok {
				rows, next = L.paragraph(lines, srcs, i, width)
			}
			out = append(out, rows...)
			i = next
		case mdATXRe.MatchString(l):
			m := mdATXRe.FindStringSubmatch(l)
			out = append(out, L.heading(m[2], len(m[1]), srcs[i], width)...)
			i++
		case isRule(l):
			out = append(out, mrow{cells: cellsOf(strings.Repeat("─", width), msBorder), src: srcs[i]})
			i++
		case indent(l) <= 3 && strings.HasPrefix(t, ">"):
			rows, next := L.quote(lines, srcs, i, width)
			out = append(out, rows...)
			i = next
		case mdItemRe.MatchString(expandTabs(l)):
			rows, next := L.listItem(lines, srcs, i, width)
			out = append(out, rows...)
			i = next
		case tableLen(lines, i) > 0:
			n := tableLen(lines, i)
			out = append(out, L.table(lines[i:i+n], srcs[i:i+n], width)...)
			i += n
		default:
			rows, next := L.paragraph(lines, srcs, i, width)
			out = append(out, rows...)
			i = next
		}
		prevBlank = blank
	}
	return out
}

// paragraph lays out the lines from i up to the next block (a setext
// underline makes it a heading).
func (L *mdLayout) paragraph(lines []string, srcs []int, i, width int) ([]mrow, int) {
	j := i + 1
	for j < len(lines) && !L.blockStart(lines, j) {
		if t := strings.TrimSpace(lines[j]); strings.Trim(t, "=") == "" {
			break // a setext underline
		}
		j++
	}
	if j < len(lines) && j > i { // a setext underline ends the paragraph as a heading
		t := strings.TrimSpace(lines[j])
		if t != "" && strings.Trim(t, "=") == "" && indent(lines[j]) <= 3 {
			return L.headingLines(lines[i:j], srcs[i:j], 1, width, srcs[j]), j + 1
		}
		if t != "" && isSetextUnderline(lines[j], lines[j-1]) && indent(lines[j]) <= 3 {
			return L.headingLines(lines[i:j], srcs[i:j], 2, width, srcs[j]), j + 1
		}
	}
	atoms := L.inline(lines[i:j], srcs[i:j], 0)
	return L.flow(atoms, width), j
}

func (L *mdLayout) heading(text string, level, src, width int) []mrow {
	return L.headingLines([]string{text}, []int{src}, level, width, src)
}

// headingLines lays out a heading; the first two levels are underlined
// on the row under (mapped to under, the setext underline).
func (L *mdLayout) headingLines(lines []string, srcs []int, level, width, under int) []mrow {
	rows := L.flow(L.inline(lines, srcs, msHeading|msBold), width)
	if level > 2 {
		return rows
	}
	w := 0
	for _, r := range rows {
		w = max(w, len(r.cells))
	}
	rule := "━"
	if level == 2 {
		rule = "─"
	}
	return append(rows, mrow{cells: cellsOf(strings.Repeat(rule, min(max(w, 1), width)), msHeading), src: under})
}

// displayMath lays out the display formula opened on line i, centred;
// the text after it on its last line is read on.
func (L *mdLayout) displayMath(lines []string, srcs []int, i, width int) ([]mrow, int, bool) {
	text := []rune(strings.Join(lines[i:], "\n"))
	at := 0
	for at < len(text) && text[at] == ' ' {
		at++
	}
	end, ok := findDelim(text, at+2, "$$")
	if !ok {
		return nil, 0, false
	}
	content := string(text[at+2 : end])
	last := i + strings.Count(string(text[:end]), "\n")
	rest := string(text[end+2:])
	if k := strings.IndexByte(rest, '\n'); k >= 0 {
		rest = rest[:k]
	}
	rows := L.formulaRows(content, srcs[i], width)
	if strings.TrimSpace(rest) != "" {
		lines[last] = rest
		return rows, last, true
	}
	return rows, last + 1, true
}

// formulaRows lays out a display formula centred, or on one line when
// it is wider than the window.
func (L *mdLayout) formulaRows(content string, src, width int) []mrow {
	n := parseFormula(content)
	b := formulaBox(n)
	if b.width() > width && L.wrap {
		atoms := []inl{{b: textBox(n.linear(), msMath), src: src}}
		return L.flow(atoms, width)
	}
	return boxRows(padBox(b, width, 'c'), src)
}

// boxRows are the rows of a box shown for source line src.
func boxRows(b box, src int) []mrow {
	rows := make([]mrow, len(b.rows))
	for k, r := range b.rows {
		rows[k] = mrow{cells: r, src: src, above: k < b.base}
	}
	return rows
}

// quote lays out the quoted lines from i, a bar before them; an alert
// ("> [!NOTE]") has a title and a coloured bar.
func (L *mdLayout) quote(lines []string, srcs []int, i, width int) ([]mrow, int) {
	var inner []string
	var innerSrc []int
	j := i
	for ; j < len(lines); j++ {
		t := strings.TrimLeftFunc(lines[j], unicode.IsSpace) // as blocks reads it
		if indent(lines[j]) > 3 || !strings.HasPrefix(t, ">") {
			break
		}
		inner = append(inner, strings.TrimPrefix(t[1:], " "))
		innerSrc = append(innerSrc, srcs[j])
	}
	if len(inner) == 0 { // not a quote after all (a tab indents it): read it as text
		return L.paragraph(lines, srcs, i, width)
	}
	bar := msDim
	var title []mrow
	if t := alertTitle(inner[0]); t != "" {
		bar = alertStyle(inner[0])
		title = []mrow{{cells: cellsOf(t, bar|msBold), src: innerSrc[0]}}
		inner, innerSrc = inner[1:], innerSrc[1:]
	}
	rows := append(title, L.blocks(inner, innerSrc, width-2)...)
	if len(rows) == 0 {
		rows = []mrow{{src: srcs[i]}}
	}
	return prefixRows(rows, cellsOf("┃ ", bar), cellsOf("┃ ", bar)), j
}

// alertStyle is the colour of an alert's bar and title.
func alertStyle(first string) mdStyle {
	m := mdAlertRe.FindStringSubmatch(strings.TrimSpace(first))
	if m == nil {
		return msInfo
	}
	switch strings.ToLower(m[1]) {
	case "warning":
		return msWarn
	case "caution", "danger", "error":
		return msError
	}
	return msInfo
}

// listItem lays out the list item on line i with what belongs to it:
// lines indented to its text (nested lists, code, paragraphs) and lazy
// continuation lines of its paragraph.
func (L *mdLayout) listItem(lines []string, srcs []int, i, width int) ([]mrow, int) {
	first := expandTabs(lines[i])
	m := mdItemRe.FindStringSubmatch(first)
	ind, marker := len(m[1]), m[2]
	content := len(m[0])
	if m[3] == "" {
		content++
	}
	body := []string{first[len(m[0]):]}
	bodySrc := []int{srcs[i]}
	j := i + 1
	for j < len(lines) {
		l := expandTabs(lines[j])
		if strings.TrimSpace(l) == "" {
			k := j
			for k < len(lines) && strings.TrimSpace(lines[k]) == "" {
				k++
			}
			if k == len(lines) || indent(lines[k]) < content {
				break
			}
			for ; j < k; j++ {
				body, bodySrc = append(body, ""), append(bodySrc, srcs[j])
			}
			continue
		}
		in := indent(l)
		nested := in > ind && mdItemRe.MatchString(l)
		lazy := strings.TrimSpace(body[len(body)-1]) != "" && !L.blockStart(lines, j)
		if in < content && !nested && !lazy {
			break
		}
		body = append(body, l[min(in, content):])
		bodySrc = append(bodySrc, srcs[j])
		j++
	}
	// the marker: a bullet by depth, the number, or the checkbox
	mark := []string{"•", "◦", "▪"}[L.list%3]
	st := mdStyle(0)
	if marker[0] >= '0' && marker[0] <= '9' {
		mark = marker
	}
	if tm := mdTaskBoxRe.FindStringSubmatch(body[0]); tm != nil {
		box := "☐"
		if tm[1] != " " {
			box = "☑"
		}
		body[0] = body[0][len(tm[0]):]
		if mark == marker {
			mark += " " + box
		} else {
			mark = box
		}
		st = msTask
	}
	L.list++
	rows := L.blocks(body, bodySrc, width-runewidth.StringWidth(mark)-1)
	L.list--
	if len(rows) == 0 {
		rows = []mrow{{src: srcs[i]}}
	}
	head := cellsOf(mark+" ", st)
	return prefixRows(rows, head, blankRow(len(head))), j
}

// prefixRows puts first before the baseline row of the first line,
// rest before the others.
func prefixRows(rows []mrow, first, rest []mcell) []mrow {
	at := 0
	for at < len(rows)-1 && rows[at].above {
		at++
	}
	out := make([]mrow, len(rows))
	for k, r := range rows {
		p := rest
		if k == at {
			p = first
		}
		cells := append(append([]mcell(nil), p...), r.cells...)
		var imgs []mdImg
		for _, im := range r.imgs {
			imgs = append(imgs, mdImg{im.start + len(p), im.end + len(p), im.target})
		}
		out[k] = mrow{cells: cells, src: r.src, imgs: imgs, above: r.above}
	}
	return out
}

func expandTabs(l string) string {
	if !strings.Contains(l, "\t") {
		return l
	}
	var sb strings.Builder
	col := 0
	for _, r := range l {
		if r == '\t' {
			n := 4 - col%4
			sb.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		sb.WriteRune(r)
		col++
	}
	return sb.String()
}

// ---------------------------------------------------------------- tables

// table lays out a GFM table with box-drawing borders. Columns keep
// their natural width while the table fits; otherwise the widest give
// way and their cells wrap. When a row takes several lines (a formula,
// wrapped text) a rule separates every row, so each cell reads as one,
// and a double rule marks the header.
func (L *mdLayout) table(lines []string, srcs []int, width int) []mrow {
	delim := splitTableRow(lines[1])
	ncol := len(delim)
	align := make([]byte, ncol)
	for i, c := range delim {
		switch {
		case strings.HasPrefix(c, ":") && strings.HasSuffix(c, ":"):
			align[i] = 'c'
		case strings.HasSuffix(c, ":"):
			align[i] = 'r'
		default:
			align[i] = 'l'
		}
	}
	type rowCells struct {
		atoms [][]inl
		src   int
	}
	var rows []rowCells
	nat, least := make([]int, ncol), make([]int, ncol)
	for ri, line := range lines {
		if ri == 1 {
			continue
		}
		st := mdStyle(0)
		if ri == 0 {
			st = msBold
		}
		src := splitTableRow(line)
		rc := rowCells{atoms: make([][]inl, ncol), src: srcs[ri]}
		for c := 0; c < ncol && c < len(src); c++ {
			cell := strings.ReplaceAll(strings.ReplaceAll(src[c], `\|`, "|"), "\t", "    ")
			rc.atoms[c] = L.inline([]string{cell}, []int{srcs[ri]}, st)
			w, g := atomsWidth(rc.atoms[c])
			nat[c], least[c] = max(nat[c], w), max(least[c], g)
		}
		rows = append(rows, rc)
	}
	colW := make([]int, ncol)
	for c := range colW {
		colW[c] = max(nat[c], 1)
	}
	if L.wrap { // narrow the widest columns until the table fits
		avail := width - 3*ncol - 1
		total := 0
		for _, w := range colW {
			total += w
		}
		for total > avail {
			best := -1
			for c, w := range colW {
				if w > least[c] && (best < 0 || w > colW[best]) {
					best = c
				}
			}
			if best < 0 { // words wider than the room: cut them
				for c, w := range colW {
					if w > 1 && (best < 0 || w > colW[best]) {
						best = c
					}
				}
				if best < 0 {
					break
				}
			}
			colW[best]--
			total--
		}
	}
	border := func(l, m, r string, line ...string) []mcell {
		h := "─"
		if len(line) > 0 {
			h = line[0]
		}
		parts := make([]string, ncol)
		for c, w := range colW {
			parts[c] = strings.Repeat(h, w+2)
		}
		return cellsOf(l+strings.Join(parts, m)+r, msBorder)
	}
	bar := cellsOf("│", msBorder)
	// the cells laid out in their columns, and the height of each row
	laid := make([][][]mrow, len(rows))
	heights := make([]int, len(rows))
	multi := false // a row on several lines: rules between all rows
	for ri, rc := range rows {
		laid[ri] = make([][]mrow, ncol)
		heights[ri] = 1
		for c := range laid[ri] {
			laid[ri][c] = L.flow(rc.atoms[c], colW[c])
			heights[ri] = max(heights[ri], len(laid[ri][c]))
		}
		multi = multi || heights[ri] > 1
	}
	out := []mrow{{cells: border("┌", "┬", "┐"), src: srcs[0]}}
	for ri, rc := range rows {
		switch {
		case ri == 1 && multi:
			out = append(out, mrow{cells: border("╞", "╪", "╡", "═"), src: srcs[1]})
		case ri == 1:
			out = append(out, mrow{cells: border("├", "┼", "┤"), src: srcs[1]})
		case ri > 1 && multi:
			out = append(out, mrow{cells: border("├", "┼", "┤"), src: rc.src})
		}
		cells := laid[ri]
		for k := 0; k < heights[ri]; k++ {
			row := mrow{src: rc.src}
			row.cells = append(row.cells, bar...)
			for c := range cells {
				cell := mrow{}
				if k < len(cells[c]) {
					cell = cells[c][k]
				}
				if len(cell.cells) > colW[c] { // never into the next cell
					cell.cells = cell.cells[:colW[c]]
				}
				pad := colW[c] - len(cell.cells)
				left := 0
				switch align[c] {
				case 'r':
					left = pad
				case 'c':
					left = pad / 2
				}
				x := len(row.cells) + 1 + left
				for _, im := range cell.imgs {
					row.imgs = append(row.imgs, mdImg{im.start + x, min(im.end, colW[c]) + x, im.target})
				}
				row.cells = append(row.cells, blankRow(1+left)...)
				row.cells = append(row.cells, cell.cells...)
				row.cells = append(row.cells, blankRow(pad-left+1)...)
				row.cells = append(row.cells, bar...)
			}
			out = append(out, row)
		}
	}
	if len(rows) == 1 { // a header alone
		out = append(out, mrow{cells: border("├", "┼", "┤"), src: srcs[1]})
	}
	return append(out, mrow{cells: border("└", "┴", "┘"), src: srcs[len(srcs)-1]})
}

// atomsWidth is the width of atoms on one line and of their widest
// unbreakable group.
func atomsWidth(atoms []inl) (int, int) {
	w, g, gw := 0, 0, 0
	for i, a := range atoms {
		if i > 0 && (a.space || a.brk || a.alone) {
			w++
			gw = 0
		}
		w += a.b.width()
		gw += a.b.width()
		g = max(g, gw)
	}
	return w, g
}

// ---------------------------------------------------------------- inline

// inl is a piece of inline text: a word, a formula, a raised script.
type inl struct {
	b     box
	space bool // after a blank: a line may break before it
	brk   bool // a line break before it
	alone bool // a display formula: a row of its own, centred
	src   int
	alt   string // one-line form when b is too wide (formulas)
	img   string // what an image shows
}

// flow breaks atoms into rows width columns wide (the lines of the
// source without 'wrap').
func (L *mdLayout) flow(atoms []inl, width int) []mrow {
	fw := L.flowWidth(width)
	var out []mrow
	var line []inl
	lineW := 0
	emit := func() {
		if len(line) == 0 {
			return
		}
		out = append(out, lineRows(line)...)
		line, lineW = nil, 0
	}
	// groups of atoms with no blank between them
	var groups [][]inl
	for i, a := range atoms {
		if i == 0 || a.space || a.brk || a.alone || atoms[i-1].alone {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], a)
	}
	for gi, g := range groups {
		if g[0].alone {
			emit()
			out = append(out, L.formulaRowsBox(g[0], width)...)
			continue
		}
		forced := g[0].brk || gi > 0 && groups[gi-1][0].alone || !L.wrap && len(line) > 0 && g[0].src != line[0].src
		if forced {
			emit()
		}
		gw := 0
		for _, a := range g {
			gw += a.b.width()
		}
		need := gw
		if len(line) > 0 {
			need++
		}
		if len(line) > 0 && lineW+need > fw {
			emit()
			need = gw
		}
		if gw > fw {
			emit()
			for _, piece := range cutGroup(g, fw) {
				out = append(out, lineRows(piece)...)
			}
			continue
		}
		if len(line) > 0 {
			g[0].space = true
		} else {
			g[0].space = false
		}
		line = append(line, g...)
		lineW += need
	}
	emit()
	return out
}

// formulaRowsBox lays out a display formula met in a paragraph.
func (L *mdLayout) formulaRowsBox(a inl, width int) []mrow {
	if a.b.width() > width && L.wrap {
		return L.flow([]inl{{b: textBox(a.alt, msMath), src: a.src}}, width)
	}
	return boxRows(padBox(a.b, width, 'c'), a.src)
}

// cutGroup splits a group wider than width: formulas take their one-line
// form, then the text is cut every width columns.
func cutGroup(g []inl, width int) [][]inl {
	var cells []mcell
	src := g[0].src
	for _, a := range g {
		b := a.b
		if b.width() > width && a.alt != "" {
			b = textBox(a.alt, msMath)
		}
		if b.height() > 1 {
			if a.alt != "" {
				b = textBox(a.alt, msMath)
			} else {
				b = textBox(boxLine(b), msMath)
			}
		}
		cells = append(cells, b.rows[0]...)
	}
	var out [][]inl
	for len(cells) > 0 {
		n := min(width, len(cells))
		if n < len(cells) && cells[n].s == "" && n > 1 {
			n-- // not between the halves of a wide character
		}
		out = append(out, []inl{{b: box{rows: [][]mcell{cells[:n]}}, src: src}})
		cells = cells[n:]
	}
	return out
}

// boxLine is the base row of b with the other rows' text appended
// (a last resort: what is too narrow to show stays readable).
func boxLine(b box) string {
	return strings.TrimSpace(rowString(b.rows[b.base]))
}

// lineRows draws a line of atoms aligned on their baselines.
func lineRows(line []inl) []mrow {
	var bs []box
	type imgAt struct {
		x      int
		w      int
		target string
	}
	var imgs []imgAt
	x := 0
	for i, a := range line {
		if i > 0 && a.space {
			bs = append(bs, blankBox(1, 1))
			x++
		}
		if a.img != "" {
			imgs = append(imgs, imgAt{x, a.b.width(), a.img})
		}
		bs = append(bs, a.b)
		x += a.b.width()
	}
	b := hcat(bs...)
	rows := boxRows(b, line[0].src)
	for k := range rows { // on every row of the line: <leader>mI works on a raised one too
		for _, im := range imgs {
			rows[k].imgs = append(rows[k].imgs, mdImg{im.x, im.x + im.w, im.target})
		}
	}
	return rows
}

// inlParser reads the inline syntax of a paragraph.
type inlParser struct {
	rs    []rune
	src   []int // source line of each rune
	out   []inl
	word  []mcell
	at    int // where the word started
	space bool
	brk   bool
	// searches for a closer that failed: one that found nothing in
	// [from, end) finds nothing from a later start either, so a long
	// paragraph of unclosed markers stays linear
	fails map[string]failSpan
	// bracket pairs, found once per paragraph (-1: unmatched)
	brackets, parens []int
	nextRB           []int // the next "]" from each rune
}

type failSpan struct{ from, end int }

func (p *inlParser) knownFail(key string, from, end int) bool {
	f, ok := p.fails[key]
	return ok && from >= f.from && end <= f.end
}

func (p *inlParser) noteFail(key string, from, end int) {
	if p.fails == nil {
		p.fails = map[string]failSpan{}
	}
	if f, ok := p.fails[key]; !ok || from <= f.from && end >= f.end {
		p.fails[key] = failSpan{from, end}
	}
}

// ticks returns the end of the code span whose run of n backticks ends
// at from (closingTicks, remembered).
func (p *inlParser) ticks(from, n, end int) (int, bool) {
	key := "`" + strconv.Itoa(n)
	if p.knownFail(key, from, end) {
		return 0, false
	}
	close, ok := closingTicks(p.rs[:end], from, n)
	if !ok {
		p.noteFail(key, from, end)
	}
	return close, ok
}

// find returns where s (matched without case) starts in [from, end),
// -1 when it does not.
func (p *inlParser) find(s string, from, end int) int {
	if p.knownFail(s, from, end) {
		return -1
	}
	want := []rune(s)
	for k := from; k+len(want) <= end; k++ {
		if p.rs[k] == want[0] && strings.EqualFold(string(p.rs[k:k+len(want)]), s) {
			return k
		}
	}
	p.noteFail(s, from, end)
	return -1
}

// pairs finds the brackets and parentheses of the paragraph that match
// (escaped brackets do not count, as in link).
func (p *inlParser) pairs() {
	if p.brackets != nil {
		return
	}
	n := len(p.rs)
	p.brackets, p.parens, p.nextRB = make([]int, n), make([]int, n), make([]int, n+1)
	var bs, ps []int
	for i := 0; i < n; i++ {
		p.brackets[i], p.parens[i] = -1, -1
	}
	for i := 0; i < n; i++ {
		switch p.rs[i] {
		case '\\':
			i++
		case '[':
			bs = append(bs, i)
		case ']':
			if len(bs) > 0 {
				p.brackets[bs[len(bs)-1]] = i
				bs = bs[:len(bs)-1]
			}
		}
	}
	for i, r := range p.rs {
		switch r {
		case '(':
			ps = append(ps, i)
		case ')':
			if len(ps) > 0 {
				p.parens[ps[len(ps)-1]] = i
				ps = ps[:len(ps)-1]
			}
		}
	}
	p.nextRB[n] = n
	for i := n - 1; i >= 0; i-- {
		p.nextRB[i] = p.nextRB[i+1]
		if p.rs[i] == ']' {
			p.nextRB[i] = i
		}
	}
}

// mdTagScan bounds how far an HTML tag or an autolink is looked for.
const mdTagScan = 2048

// inline reads the inline text of lines (a paragraph, a heading or a
// table cell) with the base style st.
func (L *mdLayout) inline(lines []string, srcs []int, st mdStyle) []inl {
	p := &inlParser{}
	for k, l := range lines {
		if k > 0 {
			p.rs = append(p.rs, '\n')
			p.src = append(p.src, srcs[k])
		}
		l = strings.TrimLeft(expandTabs(l), " ")
		for _, r := range l {
			p.rs = append(p.rs, r)
			p.src = append(p.src, srcs[k])
		}
	}
	if len(p.src) == 0 {
		return nil
	}
	start := 0
	if m := mdFootDefRe.FindStringSubmatch(string(p.rs)); m != nil { // [^1]: a note
		p.add(raised(m[1], st, true), 0)
		start = len([]rune(m[0]))
		p.space = true
	}
	p.run(start, len(p.rs), st)
	p.flush()
	return p.out
}

var mdFootDefRe = regexp.MustCompile(`^\[\^([^\]\s]+)\]:\s*`)

func (p *inlParser) srcAt(i int) int {
	return p.src[min(max(i, 0), len(p.src)-1)]
}

// blank ends the word being read.
func (p *inlParser) blank() {
	p.flush()
	p.space = true
}

func (p *inlParser) flush() {
	if len(p.word) == 0 {
		return
	}
	p.out = append(p.out, inl{b: box{rows: [][]mcell{p.word}}, space: p.space, brk: p.brk, src: p.srcAt(p.at)})
	p.word, p.space, p.brk = nil, false, false
}

func (p *inlParser) char(i int, r rune, st mdStyle) {
	if len(p.word) == 0 {
		p.at = i
	}
	p.word = append(p.word, cellsOf(string(r), st)...)
}

// add puts a box after the text read so far (no blank between them
// unless one was read).
func (p *inlParser) add(a inl, i int) {
	p.flush()
	a.space, a.brk, a.src = a.space || p.space, a.brk || p.brk, p.srcAt(i)
	p.out = append(p.out, a)
	p.space, p.brk = false, false
}

// raised is s raised as a superscript (lowered with sup false) by one
// row, in the size of the text.
func raised(s string, st mdStyle, sup bool) inl {
	t := textBox(s, st)
	if sup {
		return inl{b: box{rows: [][]mcell{t.rows[0], blankRow(t.width())}, base: 1}, alt: script(s, true)}
	}
	return inl{b: box{rows: [][]mcell{blankRow(t.width()), t.rows[0]}, base: 0}, alt: script(s, false)}
}

var mdHTMLTagRe = regexp.MustCompile(`^<(/?)([A-Za-z]+)[^<>]*?(/?)>`)

var mdHTMLStyles = map[string]mdStyle{
	"b": msBold, "strong": msBold, "i": msItalic, "em": msItalic, "u": msUnderline, "ins": msUnderline,
	"s": msStrike, "del": msStrike, "strike": msStrike, "code": msCode, "kbd": msCode, "tt": msCode,
	"mark": msBold,
}

func (p *inlParser) run(i, end int, st mdStyle) {
	rs := p.rs[:end]
	for i < end {
		r := rs[i]
		switch {
		case r == '\n':
			hard := i >= 2 && rs[i-1] == ' ' && rs[i-2] == ' '
			p.blank()
			p.brk = p.brk || hard
			i++
		case r == ' ' || r == '\t':
			p.blank()
			i++
		case r == '`':
			n := runLen(rs, i, '`')
			close, ok := p.ticks(i+n, n, end)
			if !ok {
				for k := 0; k < n; k++ {
					p.char(i+k, '`', st)
				}
				i += n
				continue
			}
			code := []rune(strings.ReplaceAll(string(rs[i+n:close-n]), "\n", " "))
			if len(code) > 1 && code[0] == ' ' && code[len(code)-1] == ' ' {
				code = code[1 : len(code)-1]
			}
			for k, c := range code {
				p.char(i+n+k, c, st|msCode)
			}
			i = close
		case r == '\\' && i+1 < end && rs[i+1] == '\n':
			p.blank()
			p.brk = true
			i += 2
		case r == '\\' && i+1 < end && rs[i+1] == '$':
			p.char(i, '$', st)
			i += 2
		case hasAt(rs, i, "$$"):
			close, ok := findDelim(rs, i+2, "$$")
			if !ok {
				p.char(i, '$', st)
				p.char(i+1, '$', st)
				i += 2
				continue
			}
			p.formula(string(rs[i+2:close]), i, true)
			i = close + 2
		case r == '\\' && i+1 < end && isMDPunct(rs[i+1]):
			p.char(i, rs[i+1], st)
			i += 2
		case r == '$':
			close := inlineDollar(rs, i)
			if close < 0 {
				p.char(i, r, st)
				i++
				continue
			}
			p.formula(strings.TrimSpace(string(rs[i+1:close])), i, false)
			i = close + 1
		case r == '*' || r == '_' || r == '~':
			if next, ok := p.emphasis(i, end, st); ok {
				i = next
				continue
			}
			n := runLen(rs, i, r)
			for k := 0; k < n; k++ {
				p.char(i+k, r, st)
			}
			i += n
		case r == '!' && i+1 < end && rs[i+1] == '[':
			if next, ok := p.link(i+1, end, st, true); ok {
				i = next
				continue
			}
			p.char(i, r, st)
			i++
		case r == '[':
			if next, ok := p.link(i, end, st, false); ok {
				i = next
				continue
			}
			p.char(i, r, st)
			i++
		case r == '<':
			if next, ok := p.html(i, end, st); ok {
				i = next
				continue
			}
			p.char(i, r, st)
			i++
		case fScriptChars[r][0] != 0: // x² and H₂O: raised and lowered
			kind := fScriptChars[r][0]
			var sb strings.Builder
			j := i
			for ; j < end && fScriptChars[rs[j]][0] == kind; j++ {
				sb.WriteRune(fScriptChars[rs[j]][1])
			}
			p.add(raised(sb.String(), st, kind == '^'), i)
			i = j
		default:
			p.char(i, r, st)
			i++
		}
	}
}

func isMDPunct(r rune) bool {
	return r < 128 && unicode.IsPunct(r) || r < 128 && unicode.IsSymbol(r)
}

// inlineDollar returns the dollar closing the formula opened at i, -1
// when there is none. A blank after the opening dollar needs one
// before the closing dollar ("$ x $" or "$x$") and one before the
// opening dollar, and no digit may follow the closing one: "$5 and
// $10" and "$5, then $x$" are not formulas.
func inlineDollar(rs []rune, i int) int {
	spaced := i+1 < len(rs) && unicode.IsSpace(rs[i+1])
	if i+1 >= len(rs) || spaced && i > 0 && !unicode.IsSpace(rs[i-1]) {
		return -1
	}
	for j := i + 1; j < len(rs); j++ {
		if rs[j] == '\\' {
			j++
			continue
		}
		if rs[j] == '$' {
			if unicode.IsSpace(rs[j-1]) == spaced && strings.TrimSpace(string(rs[i+1:j])) != "" &&
				(j+1 >= len(rs) || !unicode.IsDigit(rs[j+1])) {
				return j
			}
			return -1
		}
	}
	return -1
}

// formula lays out a formula met at i.
func (p *inlParser) formula(content string, i int, display bool) {
	n := parseFormula(content)
	p.add(inl{b: formulaBox(n), alt: n.linear(), alone: display}, i)
	if display {
		p.space = true
	}
}

// emphasis reads *a*, **b**, ***c***, _d_, __e__ or ~~f~~ at i.
func (p *inlParser) emphasis(i, end int, st mdStyle) (int, bool) {
	rs := p.rs[:end]
	c := rs[i]
	n := runLen(rs, i, c)
	if c == '~' && n != 2 {
		return 0, false
	}
	alnum := func(k int) bool {
		return k >= 0 && k < len(rs) && (unicode.IsLetter(rs[k]) || unicode.IsDigit(rs[k]))
	}
	if i+n >= end || unicode.IsSpace(rs[i+n]) || c == '_' && alnum(i-1) {
		return 0, false
	}
	key := string(c) + strconv.Itoa(n)
	if p.knownFail(key, i+n, end) {
		return 0, false
	}
	for j := i + n; j < end; j++ {
		switch rs[j] {
		case '`':
			m := runLen(rs, j, '`')
			if cl, ok := p.ticks(j+m, m, end); ok {
				j = cl - 1
			} else {
				j += m - 1
			}
			continue
		case '\\':
			j++
			continue
		case c:
		default:
			continue
		}
		m := runLen(rs, j, c)
		if m == n && !unicode.IsSpace(rs[j-1]) && !(c == '_' && alnum(j+m)) {
			add := map[int]mdStyle{1: msItalic, 2: msBold}[min(n, 3)]
			if n >= 3 {
				add = msBold | msItalic
			}
			if c == '~' {
				add = msStrike
			}
			p.run(i+n, j, st|add)
			return j + m, true
		}
		j += m - 1
	}
	p.noteFail(key, i+n, end)
	return 0, false
}

// link reads [text](url), [text][ref], [^note] or an image ![alt](src)
// with its "[" at i.
func (p *inlParser) link(i, end int, st mdStyle, image bool) (int, bool) {
	rs := p.rs[:end]
	p.pairs()
	close := p.brackets[i]
	if close < 0 || close >= end {
		return 0, false
	}
	text := rs[i+1 : close]
	if !image && len(text) > 1 && text[0] == '^' { // a footnote
		p.add(raised(string(text[1:]), st, true), i)
		return close + 1, true
	}
	next := close + 1
	target := ""
	switch {
	case next < end && rs[next] == '(':
		k := p.parens[next]
		if k < 0 || k >= end {
			return 0, false
		}
		target = strings.TrimSpace(string(rs[next+1 : k]))
		if f := strings.Fields(target); len(f) > 0 {
			target = strings.Trim(f[0], "<>")
		}
		next = k + 1
	case next < end && rs[next] == '[':
		k := p.nextRB[next]
		if k >= end {
			return 0, false
		}
		next = k + 1
	default:
		return 0, false // [text] alone stays as written
	}
	if image {
		start := i - 1
		label := "▣ " + strings.TrimSpace(string(text))
		a := inl{b: textBox(strings.TrimSpace(label), st|msLink), img: target}
		p.add(a, start)
		return next, true
	}
	p.run(i+1, close, st|msLink)
	return next, true
}

// html reads the tags rendered (sup, sub, br, emphasis), comments
// (hidden) and autolinks at i.
func (p *inlParser) html(i, end int, st mdStyle) (int, bool) {
	rs := p.rs[:end]
	if hasAt(rs, i, "<!--") {
		if k := p.find("-->", i+4, end); k >= 0 {
			return k + 3, true
		}
		return 0, false
	}
	rest := string(rs[i:min(end, i+mdTagScan)]) // a tag is short
	if m := mdAutolinkRe.FindStringSubmatch(rest); m != nil {
		p.flush()
		for k, c := range m[1] {
			p.char(i+1+k, c, st|msLink)
		}
		return i + len([]rune(m[0])), true
	}
	m := mdHTMLTagRe.FindStringSubmatch(rest)
	if m == nil || m[1] == "/" {
		return 0, false
	}
	name := strings.ToLower(m[2])
	after := i + len([]rune(m[0]))
	if name == "br" {
		p.blank()
		p.brk = true
		return after, true
	}
	if name == "img" {
		if im := mdImageRe.FindStringSubmatch(m[0]); im != nil && im[2] != "" {
			p.add(inl{b: textBox("▣ "+im[2], st|msLink), img: im[2]}, i)
			return after, true
		}
		return 0, false
	}
	closeTag := []rune("</" + name + ">")
	inner := p.find(string(closeTag), after, end)
	if inner < 0 {
		return 0, false
	}
	switch name {
	case "sup", "sub":
		p.add(raised(strings.TrimSpace(string(rs[after:inner])), st, name == "sup"), i)
	default:
		add, ok := mdHTMLStyles[name]
		if !ok {
			return 0, false
		}
		p.run(after, inner, st|add)
	}
	return inner + len(closeTag), true
}

var mdAutolinkRe = regexp.MustCompile(`^<((?:https?|ftp|mailto):[^\s<>]+)>`)
