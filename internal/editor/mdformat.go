package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"

	"scihe/internal/buffer"
)

// Markdown editing under <leader>m: emphasis, links, headings, lists,
// quotes, code blocks, table alignment, a table of contents, heading
// jumps and opening images.

// mdEditable reports (as an error) why b cannot be formatted.
func (e *Editor) mdEditable(b *buffer.Buffer) bool {
	switch {
	case !isMarkdown(b):
		e.errorf("not a markdown buffer")
	case b.ReadOnly:
		e.errorf("E21: Cannot make changes, buffer is read-only")
	default:
		return true
	}
	return false
}

// setLine replaces the text of line l.
func setLine(b *buffer.Buffer, l int, s string) {
	if s != b.LineString(l) {
		b.Replace(Pos{l, 0}, Pos{l, b.LineLen(l)}, s)
	}
}

// selectedLines is the line span of the visual selection (leaving
// visual mode), or the cursor line in normal mode.
func (e *Editor) selectedLines(c *ctx) (int, int) {
	if !e.mode.visual() {
		return c.w.cur.Line, c.w.cur.Line
	}
	r := e.visualRange()
	e.exitVisual()
	return r.start.Line, r.end.Line
}

// ---------------------------------------------------------------- inline

// mdEmphasis toggles a marker (**, *, `, ~~). Inside a span on the
// cursor line (markers included) it acts on the whole span: bold or
// italic is removed when the span has it, else the span takes that
// style (**a b** with italic gives *a b*); code and strikethrough are
// removed. Elsewhere it wraps the
// word under the cursor or the selection; without a word the pair is
// inserted and insert mode starts between them. A selection holding
// markers outside a single span is refused: it would cross spans.
func (e *Editor) mdEmphasis(c *ctx, m string) {
	b := c.b
	if !e.mdEditable(b) {
		return
	}
	chars := mdMarkerChars(m)
	if e.mode.visual() {
		r := e.visualRange()
		e.exitVisual()
		switch {
		case r.block:
			e.errorf("not supported in a block selection")
		case r.linewise:
			e.mdEmphasisLines(b, r.start.Line, r.end.Line, m)
		default:
			if r.end.Col == 0 && r.end.Line > r.start.Line {
				r.end = Pos{r.end.Line - 1, b.LineLen(r.end.Line - 1)} // up to the end of the line
			}
			c.w.cur = r.start
			if r.start.Line == r.end.Line {
				if s, _, ok := toggleEmphasis(b.Line(r.start.Line), r.start.Col, r.end.Col, m, false); ok {
					setLine(b, r.start.Line, s)
					return
				}
			}
			if strings.ContainsAny(b.Slice(r.start, r.end), chars) {
				e.errorf("the selection has %s markers outside a single span: select plain text, or toggle inside the span", chars)
				return
			}
			b.Insert(r.end, m)
			b.Insert(r.start, m)
		}
		return
	}
	l, col := c.w.cur.Line, c.w.cur.Col
	if s, shift, ok := toggleEmphasis(b.Line(l), col, col+1, m, false); ok {
		setLine(b, l, s)
		c.w.cur = b.Clamp(Pos{l, max(col+shift, 0)})
		return
	}
	s, en := wordBounds(b.Line(l), col)
	if s == en {
		p := b.Insert(c.w.cur, m+m)
		c.w.cur = Pos{p.Line, p.Col - len([]rune(m))}
		e.enterInsert()
		return
	}
	b.Insert(Pos{l, en}, m)
	b.Insert(Pos{l, s}, m)
	c.w.cur = Pos{l, s}
}

// mdEmphasisLines toggles m on the text of whole lines (after list,
// heading and quote markers): a line that is one span is toggled like
// the cursor in it, a plain line is wrapped. Lines with markers outside
// a single span make the whole command refuse.
func (e *Editor) mdEmphasisLines(b *buffer.Buffer, from, to int, m string) {
	chars := mdMarkerChars(m)
	out := make([]string, to-from+1)
	for l := from; l <= to; l++ {
		line := b.LineString(l)
		prefix := mdPrefixRe.FindString(line)
		rest := line[len(prefix):]
		body := []rune(strings.TrimRight(rest, " \t"))
		trail := rest[len(string(body)):] // two blanks are a hard break
		switch s, _, ok := toggleEmphasis(body, 0, len(body), m, true); {
		case len(body) == 0:
			out[l-from] = line
		case ok:
			out[l-from] = prefix + s + trail
		case strings.ContainsAny(string(body), chars):
			e.errorf("line %d has %s markers outside a single span: toggle inside the span", l+1, chars)
			return
		default:
			out[l-from] = prefix + m + string(body) + m + trail
		}
	}
	for l := from; l <= to; l++ {
		setLine(b, l, out[l-from])
	}
}

// mdSpan is an emphasis span on a line: the opening run of markers
// starts at s, the closing one ends at e (exclusive).
type mdSpan struct {
	s, e          int
	openN, closeN int
}

// mdSpans finds the spans delimited by runs of ch (*, _, ~ or `).
// Code spans pair runs of the same length; other runs open when
// followed by a non-blank and close when preceded by one, the closest
// opener first ("_" not inside a word: snake_case is no emphasis).
// Code spans hide the markers inside them.
func mdSpans(line []rune, ch rune) []mdSpan {
	var spans, stack []mdSpan
	alnum := func(i int) bool {
		return i >= 0 && i < len(line) && (unicode.IsLetter(line[i]) || unicode.IsDigit(line[i]))
	}
	for i := 0; i < len(line); {
		if line[i] == '\\' {
			i += 2
			continue
		}
		if line[i] == '`' && ch != '`' {
			n := runLen(line, i, '`')
			if end, ok := closingTicks(line, i+n, n); ok {
				i = end // markers in code are text
			} else {
				i += n
			}
			continue
		}
		if line[i] != ch {
			i++
			continue
		}
		n := runLen(line, i, ch)
		if ch == '`' {
			if end, ok := closingTicks(line, i+n, n); ok {
				spans = append(spans, mdSpan{i, end, n, n})
				i = end
			} else {
				i += n
			}
			continue
		}
		closes := i > 0 && !unicode.IsSpace(line[i-1])
		opens := i+n < len(line) && !unicode.IsSpace(line[i+n])
		if ch == '_' {
			closes = closes && !alnum(i+n)
			opens = opens && !alnum(i-1)
		}
		if closes && len(stack) > 0 {
			o := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			spans = append(spans, mdSpan{o.s, i + n, o.openN, n})
		} else if opens {
			stack = append(stack, mdSpan{s: i, openN: n})
		}
		i += n
	}
	return spans
}

// mdMarkerChars are the characters of m's spans: bold and italic are
// written with * or _.
func mdMarkerChars(m string) string {
	if m[0] == '*' {
		return "*_"
	}
	return m[:1]
}

// toggleEmphasis toggles m on the innermost span of line holding
// columns [from, to) (with exact: the span that is [from, to)). It
// returns the new line and how far text after the opening run moved;
// false when there is no such span.
func toggleEmphasis(line []rune, from, to int, m string, exact bool) (string, int, bool) {
	var sp mdSpan
	found := false
	for _, ch := range mdMarkerChars(m) {
		for _, x := range mdSpans(line, ch) {
			inside := x.s <= from && to <= x.e
			if exact {
				inside = x.s == from && x.e == to
			}
			if inside && (m != "~~" || min(x.openN, x.closeN) >= 2) && (!found || x.e-x.s < sp.e-sp.s) {
				sp, found = x, true
			}
		}
	}
	if !found {
		return "", 0, false
	}
	run := ""
	if m[0] == '*' {
		// the run length encodes bold (2) and italic (1); the key removes
		// its style when the span has it, else makes the span that style
		// (bold becomes italic, not bold and italic)
		k := min(sp.openN, sp.closeN)
		bold, italic := k >= 2, k%2 == 1
		switch {
		case m == "**" && bold:
			bold = false
		case m == "**":
			bold, italic = true, false
		case italic:
			italic = false
		default:
			bold, italic = false, true
		}
		n := 0
		if bold {
			n += 2
		}
		if italic {
			n++
		}
		run = strings.Repeat(string(line[sp.s]), n)
	}
	s := string(line[:sp.s]) + run + string(line[sp.s+sp.openN:sp.e-sp.closeN]) + run + string(line[sp.e:])
	return s, len([]rune(run)) - sp.openN, true
}

// wordBounds returns the word around col (s == en when there is none).
func wordBounds(line []rune, col int) (int, int) {
	if col >= len(line) || charClass(line[col], false) != clsWord {
		return col, col
	}
	s, en := col, col
	for s > 0 && charClass(line[s-1], false) == clsWord {
		s--
	}
	for en < len(line) && charClass(line[en], false) == clsWord {
		en++
	}
	return s, en
}

var mdLinkRe = regexp.MustCompile(`\[([^\]]*)\]\((?:<[^>]*>|[^)]*)\)`)

// unlink replaces the link (not an image) holding columns [from, to) of
// line l by its text; false when there is none.
func unlink(b *buffer.Buffer, l, from, to int) bool {
	line := b.Line(l)
	str := string(line)
	for _, m := range mdLinkRe.FindAllStringSubmatchIndex(str, -1) {
		s, en := len([]rune(str[:m[0]])), len([]rune(str[:m[1]]))
		if s > 0 && line[s-1] == '!' {
			continue // an image
		}
		if s <= from && to <= en {
			b.Replace(Pos{l, s}, Pos{l, en}, str[m[2]:m[3]])
			return true
		}
	}
	return false
}

// mdLink turns the word under the cursor or the selection into a link
// to the URL asked for; in a link it leaves only the link text.
func (e *Editor) mdLink(c *ctx) {
	b, w := c.b, c.w
	if !e.mdEditable(b) {
		return
	}
	var s, en Pos
	if e.mode.visual() {
		r := e.visualRange()
		e.exitVisual()
		s, en = r.start, r.end
		if r.linewise || r.end.Line != r.start.Line {
			en = Pos{s.Line, b.LineLen(s.Line)}
		}
		if unlink(b, s.Line, s.Col, en.Col) {
			w.cur = b.Clamp(s)
			return
		}
	} else {
		if unlink(b, w.cur.Line, w.cur.Col, w.cur.Col+1) {
			w.cur = b.Clamp(w.cur)
			return
		}
		ws, we := wordBounds(b.Line(w.cur.Line), w.cur.Col)
		s, en = Pos{w.cur.Line, ws}, Pos{w.cur.Line, we}
	}
	e.prompt("Link URL: ", "", func(url string, ok bool) {
		if !ok || url == "" || !e.bufferAlive(b) || s.Line >= b.LineCount() || en.Col > b.LineLen(en.Line) {
			return
		}
		if strings.ContainsAny(url, " \t()") {
			url = "<" + url + ">"
		}
		b.BeginUndo(w.cur)
		text := b.Slice(s, en)
		b.Replace(s, en, "["+text+"]("+url+")")
		b.EndUndo(w.cur)
		if w.buf == b {
			w.cur = Pos{s.Line, s.Col + 1}
			if text == "" {
				e.enterInsert() // type the link text
			}
		}
	})
}

// ---------------------------------------------------------------- lines

var (
	mdHeadingRe = regexp.MustCompile(`^(#{1,6})(?:[ \t]+(.*))?$`)
	// the markers a line starts with: quote, heading, list item, checkbox
	mdPrefixRe = regexp.MustCompile(`^[ \t]*(?:>[ \t]?)*(?:#{1,6}[ \t]+|(?:[-*+]|\d+[.)])[ \t]+(?:\[[ xX]\][ \t]+)?)?`)
	mdBulletRe = regexp.MustCompile(`^(\s*)[-*+]\s+`)
	mdNumberRe = regexp.MustCompile(`^(\s*)(\d+)[.)]\s+`)
	mdTaskRe   = regexp.MustCompile(`^(\s*(?:[-*+]|\d+[.)])\s+)\[([ xX])\]\s?`)
)

// mdHeading sets the heading level of the line (0 cycles 1…6, none).
func (e *Editor) mdHeading(c *ctx, level int) {
	b, l := c.b, c.w.cur.Line
	if !e.mdEditable(b) {
		return
	}
	line := b.LineString(l)
	cur, text, ok := parseHeading(line)
	if !ok {
		text = strings.TrimSpace(line)
	}
	if level == 0 {
		level = (cur + 1) % 7
	} else if level == cur {
		level = 0
	}
	if level > 0 {
		text = strings.Repeat("#", level) + " " + text
	}
	setLine(b, l, text)
	c.w.cur = b.Clamp(Pos{l, 0})
}

// parseHeading returns the level and text of an ATX heading; a closing
// run of # counts only after a blank ("# C#" is about C#).
func parseHeading(line string) (int, string, bool) {
	m := mdHeadingRe.FindStringSubmatch(line)
	if m == nil {
		return 0, "", false
	}
	text := strings.TrimSpace(m[2])
	if t := strings.TrimRight(text, "#"); t == "" || strings.HasSuffix(t, " ") || strings.HasSuffix(t, "\t") {
		text = strings.TrimSpace(t)
	}
	return len(m[1]), text, true
}

// mdTask toggles the checkbox of list items, adding one (and a bullet)
// where there is none.
func (e *Editor) mdTask(c *ctx) {
	b := c.b
	if !e.mdEditable(b) {
		return
	}
	from, to := e.selectedLines(c)
	for l := from; l <= to; l++ {
		line := b.LineString(l)
		switch m := mdTaskRe.FindStringSubmatch(line); {
		case strings.TrimSpace(line) == "":
		case m != nil:
			mark := "x"
			if m[2] != " " {
				mark = " "
			}
			setLine(b, l, m[1]+"["+mark+"] "+line[len(m[0]):])
		case mdBulletRe.MatchString(line) || mdNumberRe.MatchString(line):
			ind, body := splitListItem(line)
			setLine(b, l, ind+"[ ] "+body)
		default:
			ind, body := splitListItem(line)
			setLine(b, l, ind+"- [ ] "+body)
		}
	}
}

// splitListItem returns the indentation and marker of a list item, and
// its text; a plain line has only indentation.
func splitListItem(line string) (string, string) {
	if m := mdBulletRe.FindString(line); m != "" {
		return m, line[len(m):]
	}
	if m := mdNumberRe.FindString(line); m != "" {
		return m, line[len(m):]
	}
	body := strings.TrimLeft(line, " \t")
	return line[:len(line)-len(body)], body
}

// mdList toggles bullets (numbered false) or numbers on the lines: when
// every line already has that kind of marker it is removed, otherwise
// the lines get it (numbers restart at 1, or continue the list above).
func (e *Editor) mdList(c *ctx, numbered bool) {
	b := c.b
	if !e.mdEditable(b) {
		return
	}
	from, to := e.selectedLines(c)
	re := mdBulletRe
	if numbered {
		re = mdNumberRe
	}
	all := true
	for l := from; l <= to; l++ {
		if line := b.LineString(l); strings.TrimSpace(line) != "" && !re.MatchString(line) {
			all = false
		}
	}
	n := 1
	if numbered && from > 0 {
		if m := mdNumberRe.FindStringSubmatch(b.LineString(from - 1)); m != nil {
			if k, err := strconv.Atoi(m[2]); err == nil && k < 1e9 {
				n = k + 1
			}
		}
	}
	for l := from; l <= to; l++ {
		line := b.LineString(l)
		if strings.TrimSpace(line) == "" {
			continue
		}
		ind, body := splitListItem(line)
		ind = ind[:len(ind)-len(strings.TrimLeft(ind, " \t"))]
		switch {
		case all:
			setLine(b, l, ind+body)
		case numbered:
			setLine(b, l, ind+strconv.Itoa(n)+". "+body)
			n++
		default:
			setLine(b, l, ind+"- "+body)
		}
	}
}

// mdQuote toggles "> " on the lines.
func (e *Editor) mdQuote(c *ctx) {
	b := c.b
	if !e.mdEditable(b) {
		return
	}
	from, to := e.selectedLines(c)
	all := true
	for l := from; l <= to; l++ {
		if line := b.LineString(l); strings.TrimSpace(line) != "" && !strings.HasPrefix(line, ">") {
			all = false
		}
	}
	for l := from; l <= to; l++ {
		line := b.LineString(l)
		switch {
		case all:
			line = strings.TrimPrefix(line, ">")
			setLine(b, l, strings.TrimPrefix(line, " "))
		case strings.TrimSpace(line) == "":
			setLine(b, l, ">")
		default:
			setLine(b, l, "> "+line)
		}
	}
}

// fenceStep follows fenced code blocks line by line: t (trimmed) opens
// one with ``` or ~~~ (fence keeps the opening run) and closes it with a
// line of only that character, at least as long ("```python" or ```
// after ```` do not close). It reports a fence line.
func fenceStep(t string, fence *string) bool {
	switch {
	case *fence == "" && (strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")):
		*fence = t[:runLen([]rune(t), 0, rune(t[0]))]
	case *fence != "" && len(t) >= len(*fence) && strings.Trim(t, (*fence)[:1]) == "":
		*fence = ""
	default:
		return false
	}
	return true
}

// fencedBlock returns the fence lines of the fenced code block holding
// lines [from, to].
func fencedBlock(b *buffer.Buffer, from, to int) (int, int, bool) {
	open, fence := -1, ""
	for l := 0; l < b.LineCount() && (fence != "" || l <= to); l++ {
		opened := fence == ""
		if !fenceStep(strings.TrimSpace(b.LineString(l)), &fence) {
			continue
		}
		if opened {
			open = l
		} else if open <= from && to <= l {
			return open, l, true
		}
	}
	return 0, 0, false
}

// mdCodeBlock fences the lines as a code block in the language asked
// for; in a fenced block it removes the fences.
func (e *Editor) mdCodeBlock(c *ctx) {
	b, w := c.b, c.w
	if !e.mdEditable(b) {
		return
	}
	from, to := e.selectedLines(c)
	if open, cl, ok := fencedBlock(b, from, to); ok {
		b.Delete(Pos{cl - 1, b.LineLen(cl - 1)}, Pos{cl, b.LineLen(cl)})
		if cl == open+1 {
			setLine(b, open, "") // an empty block: one line left
		} else {
			b.Delete(Pos{open, 0}, Pos{open + 1, 0})
		}
		w.cur = b.Clamp(Pos{max(from-1, open), 0})
		return
	}
	e.prompt("Code block language: ", "", func(lang string, ok bool) {
		if !ok || !e.bufferAlive(b) || to >= b.LineCount() {
			return
		}
		b.BeginUndo(w.cur)
		b.Insert(Pos{to, b.LineLen(to)}, "\n```")
		b.Insert(Pos{from, 0}, "```"+strings.TrimSpace(lang)+"\n")
		b.EndUndo(w.cur)
		if w.buf == b {
			w.cur = Pos{from + 1, 0}
		}
	})
}

// ---------------------------------------------------------------- tables

var mdDelimCellRe = regexp.MustCompile(`^:?-+:?$`)

// mdAlignTable pads the cells of the table under the cursor so that its
// pipes line up.
func (e *Editor) mdAlignTable(c *ctx) {
	b, l := c.b, c.w.cur.Line
	if !e.mdEditable(b) {
		return
	}
	isRow := func(l int) bool { return strings.HasPrefix(strings.TrimSpace(b.LineString(l)), "|") }
	if !isRow(l) {
		e.errorf("no table under the cursor (rows start with |)")
		return
	}
	from, to := l, l
	for from > 0 && isRow(from-1) {
		from--
	}
	for to < b.LineCount()-1 && isRow(to+1) {
		to++
	}
	var rows [][]string
	for i := from; i <= to; i++ {
		rows = append(rows, splitTableRow(b.LineString(i)))
	}
	text := strings.Join(formatTable(rows), "\n")
	if text != b.Slice(Pos{from, 0}, Pos{to, b.LineLen(to)}) {
		b.Replace(Pos{from, 0}, Pos{to, b.LineLen(to)}, text)
	}
	c.w.cur = b.Clamp(Pos{l, c.w.cur.Col})
}

// splitTableRow returns the trimmed cells of "| a | b |"; as in GFM only
// an escaped \| stays in its cell, also in code.
func splitTableRow(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	if strings.HasSuffix(line, "|") && !strings.HasSuffix(line, `\|`) {
		line = line[:len(line)-1]
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case ch == '\\' && i+1 < len(line):
			cur.WriteByte(ch)
			i++
			cur.WriteByte(line[i])
			continue
		case ch == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(ch)
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// formatTable lays out rows with padded columns; the delimiter row (if
// any) sets each column's alignment and is redrawn to the column width.
func formatTable(rows [][]string) []string {
	ncol := 0
	for _, r := range rows {
		ncol = max(ncol, len(r))
	}
	delim := -1
	if len(rows) > 1 {
		delim = 1
		for _, c := range rows[1] {
			if !mdDelimCellRe.MatchString(c) {
				delim = -1
			}
		}
	}
	align := make([]byte, ncol) // 'l', 'r', 'c' or 0 (default, left)
	width := make([]int, ncol)
	for i := range width {
		width[i] = 3
	}
	for ri, r := range rows {
		for i, c := range r {
			if ri == delim {
				switch {
				case strings.HasPrefix(c, ":") && strings.HasSuffix(c, ":"):
					align[i] = 'c'
				case strings.HasSuffix(c, ":"):
					align[i] = 'r'
				case strings.HasPrefix(c, ":"):
					align[i] = 'l'
				}
				continue
			}
			width[i] = max(width[i], runewidth.StringWidth(c))
		}
	}
	out := make([]string, len(rows))
	for ri, r := range rows {
		var sb strings.Builder
		sb.WriteString("|")
		for i := 0; i < ncol; i++ {
			cell := ""
			if i < len(r) {
				cell = r[i]
			}
			if ri == delim {
				d := strings.Repeat("-", width[i])
				switch align[i] {
				case 'c':
					d = ":" + d[2:] + ":"
				case 'r':
					d = d[1:] + ":"
				case 'l':
					d = ":" + d[1:]
				}
				sb.WriteString(" " + d + " |")
				continue
			}
			pad := width[i] - runewidth.StringWidth(cell)
			left := 0
			switch align[i] {
			case 'r':
				left = pad
			case 'c':
				left = pad / 2
			}
			sb.WriteString(" " + strings.Repeat(" ", left) + cell + strings.Repeat(" ", pad-left) + " |")
		}
		out[ri] = sb.String()
	}
	return out
}

// ---------------------------------------------------------------- headings

// mdHeadings returns the heading lines outside fenced code blocks.
func mdHeadings(b *buffer.Buffer) []int {
	var out []int
	fence := ""
	for l := 0; l < b.LineCount(); l++ {
		if fenceStep(strings.TrimSpace(b.LineString(l)), &fence) {
			continue
		}
		if _, _, ok := parseHeading(b.LineString(l)); fence == "" && ok {
			out = append(out, l)
		}
	}
	return out
}

// mdJumpHeading moves to the next (or previous) heading.
func (e *Editor) mdJumpHeading(c *ctx, forward bool) {
	if !isMarkdown(c.b) {
		e.errorf("not a markdown buffer")
		return
	}
	hs := mdHeadings(c.b)
	l := c.w.cur.Line
	target := -1
	for _, h := range hs {
		if forward && h > l && target < 0 {
			target = h
		}
		if !forward && h < l {
			target = h
		}
	}
	if target < 0 {
		e.warnf("no heading %s", map[bool]string{true: "below", false: "above"}[forward])
		return
	}
	e.setCursor(c.w, Pos{target, 0})
}

const (
	tocStart = "<!-- toc -->"
	tocEnd   = "<!-- tocstop -->"
)

// mdTOC inserts a table of contents at the cursor, or refreshes the one
// between <!-- toc --> and <!-- tocstop -->.
func (e *Editor) mdTOC(c *ctx) {
	b := c.b
	if !e.mdEditable(b) {
		return
	}
	type heading struct {
		level int
		text  string
	}
	var hs []heading
	top := 6
	for _, l := range mdHeadings(b) {
		level, text, _ := parseHeading(b.LineString(l))
		text = mdLinkRe.ReplaceAllString(text, "$1") // a link in a link is not one
		hs = append(hs, heading{level, text})
		top = min(top, level)
	}
	if len(hs) == 0 {
		e.errorf("no headings")
		return
	}
	lines := []string{tocStart}
	seen := map[string]int{}
	for _, h := range hs {
		a := mdAnchor(h.text)
		if n := seen[a]; n > 0 {
			seen[a]++
			a = fmt.Sprintf("%s-%d", a, n)
		} else {
			seen[a] = 1
		}
		lines = append(lines, fmt.Sprintf("%s- [%s](#%s)", strings.Repeat("  ", h.level-top), h.text, a))
	}
	lines = append(lines, tocEnd)
	text := strings.Join(lines, "\n")
	from, to := -1, -1
	for l := 0; l < b.LineCount(); l++ {
		switch strings.TrimSpace(b.LineString(l)) {
		case tocStart:
			if to < 0 {
				from = l
			}
		case tocEnd:
			if from >= 0 && to < 0 {
				to = l
			}
		}
	}
	if from >= 0 && to > from {
		b.Replace(Pos{from, 0}, Pos{to, b.LineLen(to)}, text)
		e.infof("table of contents updated")
		return
	}
	b.Insert(Pos{c.w.cur.Line, 0}, text+"\n")
}

// mdAnchor is the GitHub anchor of a heading: lower case, punctuation
// dropped, blanks as dashes.
func mdAnchor(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == ' ':
			sb.WriteRune('-')
		case r == '-' || r == '_' || charClass(r, false) == clsWord:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// ---------------------------------------------------------------- images

// mdImageExts are the files :mdimage opens: the system opener runs
// whatever it is given (a .desktop file, a macOS .command or .app).
var mdImageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".svg": true, ".webp": true,
	".bmp": true, ".tif": true, ".tiff": true, ".ico": true, ".avif": true, ".heic": true,
}

var mdImageRe = regexp.MustCompile(`!\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)|<img\s[^>]*src="([^"]+)"`)

// exMdImage opens the image under the cursor (or the only one on the
// line) with the system viewer, like :nbimage for notebook outputs.
func exMdImage(e *Editor, a *exArgs) error {
	if e.mappingDepth > 0 {
		return errMappingProgram
	}
	w := e.win
	path := w.buf.Path
	if r := e.renders[w.buf.ID]; r != nil {
		path = r.src.Path // the rendered view keeps the image syntax
	}
	line := w.buf.LineString(w.cur.Line)
	col := len(string(w.buf.Line(w.cur.Line)[:min(w.cur.Col, w.buf.LineLen(w.cur.Line))]))
	ms := mdImageRe.FindAllStringSubmatchIndex(line, -1)
	target := ""
	for _, m := range ms {
		if len(ms) == 1 || col >= m[0] && col < m[1] {
			if m[2] >= 0 {
				target = line[m[2]:m[3]]
			} else {
				target = line[m[4]:m[5]]
			}
		}
	}
	if target == "" {
		return errors.New("no image under the cursor")
	}
	if strings.Contains(target, "://") {
		if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
			return fmt.Errorf("only http(s) image links are opened: %s", target)
		}
	} else {
		if !filepath.IsAbs(target) {
			if path == "" {
				return errors.New("the buffer has no file: relative image paths need one")
			}
			target = filepath.Join(filepath.Dir(path), target)
		}
		// check what the opener will get: the file behind any symlink
		real, err := filepath.EvalSymlinks(target)
		if err != nil {
			return fmt.Errorf("image not found: %s", target)
		}
		if st, err := os.Stat(real); err != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("not an image file: %s", target)
		}
		if !mdImageExts[strings.ToLower(filepath.Ext(real))] {
			return fmt.Errorf("not an image file: %s", target)
		}
		target = real
	}
	if err := openWithSystem(target); err != nil {
		return err
	}
	e.infof("opened %s", target)
	return nil
}
