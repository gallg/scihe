package editor

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// Block elements of the rendered markdown view, drawn as GitHub and
// most renderers show them: GFM tables, blockquotes and alerts
// ("> [!NOTE]", also Obsidian callouts), horizontal rules and task
// list items. As with formulas (mdmath.go) every source line gives
// exactly one rendered line, so nothing gets a top or bottom border.

// mdRuleWidth is the width of a rendered horizontal rule.
const mdRuleWidth = 60

// renderMarkdown renders the block elements and the formulas of a
// markdown text; code and YAML front matter are left alone.
func renderMarkdown(src string) string {
	lines := strings.Split(src, "\n")
	out := make([]string, len(lines))
	copy(out, lines)
	start := frontMatterEnd(lines)
	fence := ""    // the opening run of the open ``` or ~~~ fence
	closer := ""   // the delimiter closing a display formula left open
	mathQuote := 0 // the quote depth the open formula started at
	prevDepth := 0 // quote depth of the previous line
	prevBody := "" // its text after the quote markers ("" after a block)
	for i := start; i < len(lines); i++ {
		l := lines[i]
		if closer == "" && fenceStep(strings.TrimSpace(l), &fence) || fence != "" {
			prevDepth, prevBody = 0, ""
			continue
		}
		if closer != "" {
			depth, body := quoteDepth(l)
			if mathQuote == 0 || depth == 0 {
				depth, body = 0, l
			}
			out[i], closer = mathLine(body, closer)
			out[i] = strings.Repeat("┃ ", depth) + out[i]
			prevDepth, prevBody = depth, ""
			continue
		}
		if n := tableLen(lines, i); n > 0 {
			copy(out[i:], renderTable(lines[i:i+n]))
			i += n - 1
			prevDepth, prevBody = 0, ""
			continue
		}
		depth, body := quoteDepth(l)
		prefix := strings.Repeat("┃ ", depth)
		title := ""
		if depth > 0 && prevDepth == 0 {
			title = alertTitle(body)
		}
		switch {
		case title != "":
			out[i] = prefix + title
		case isRule(body) && !(depth == prevDepth && isSetextUnderline(body, prevBody)):
			out[i] = prefix + strings.Repeat("─", mdRuleWidth)
		default:
			out[i], closer = mathLine(taskItem(body), "")
			out[i] = prefix + out[i]
			mathQuote = depth
		}
		prevDepth, prevBody = depth, body
	}
	return strings.Join(out, "\n")
}

// frontMatterEnd returns the first line after a YAML front matter block
// ("---" on the first line up to "---" or "..."), 0 without one.
func frontMatterEnd(lines []string) int {
	if len(lines) == 0 || strings.TrimRight(lines[0], " ") != "---" {
		return 0
	}
	for i := 1; i < len(lines); i++ {
		if t := strings.TrimRight(lines[i], " "); t == "---" || t == "..." {
			return i + 1
		}
	}
	return 0
}

// ---------------------------------------------------------------- tables

// tableLen returns the number of lines of the GFM table starting at
// line i: a header row, a delimiter row with as many cells, then rows up
// to a blank line or another block. 0 when no table starts there.
func tableLen(lines []string, i int) int {
	if i+1 >= len(lines) || !strings.Contains(lines[i], "|") || !strings.Contains(lines[i+1], "|") ||
		indent(lines[i]) > 3 || startsBlock(lines[i]) {
		return 0
	}
	head, delim := splitTableRow(lines[i]), splitTableRow(lines[i+1])
	if len(head) != len(delim) || !strings.Contains(lines[i+1], "-") {
		return 0
	}
	for _, c := range delim {
		if !mdDelimCellRe.MatchString(c) {
			return 0
		}
	}
	n := 2
	for i+n < len(lines) {
		if strings.TrimSpace(lines[i+n]) == "" || startsBlock(lines[i+n]) {
			break
		}
		n++
	}
	return n
}

// startsBlock reports a line that opens another block (a quote,
// heading, fence, rule or list item), which no table row can be.
func startsBlock(l string) bool {
	t := strings.TrimSpace(l)
	return strings.HasPrefix(t, ">") || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "```") ||
		strings.HasPrefix(t, "~~~") || isRule(l) || mdBulletRe.MatchString(l) || mdNumberRe.MatchString(l)
}

// indent returns the width of the leading blanks, a tab counting to the
// next multiple of 4 as in CommonMark.
func indent(l string) int {
	n := 0
	for _, r := range l {
		switch r {
		case ' ':
			n++
		case '\t':
			n += 4 - n%4
		default:
			return n
		}
	}
	return n
}

// renderTable lays out a table with box-drawing lines: "│" between
// cells and the delimiter row as the rule under the header. Cells are
// split before their formulas are rendered, as GFM splits them before
// reading inline syntax ($|x|$ needs \| in a table).
func renderTable(rows []string) []string {
	delim := splitTableRow(rows[1])
	ncol := len(delim)
	align := make([]byte, ncol) // 'r', 'c' or 0 (left)
	for i, c := range delim {
		switch {
		case strings.HasPrefix(c, ":") && strings.HasSuffix(c, ":"):
			align[i] = 'c'
		case strings.HasSuffix(c, ":"):
			align[i] = 'r'
		}
	}
	cells := make([][]string, len(rows))
	width := make([]int, ncol)
	for i := range width {
		width[i] = 1
	}
	for ri, row := range rows {
		if ri == 1 {
			continue
		}
		src := splitTableRow(row)
		cells[ri] = make([]string, ncol) // extra cells are dropped, missing ones empty
		for c := 0; c < ncol && c < len(src); c++ {
			cell := strings.ReplaceAll(strings.ReplaceAll(src[c], `\|`, "|"), "\t", "    ")
			cells[ri][c], _ = mathLine(cell, "")
			width[c] = max(width[c], runewidth.StringWidth(cells[ri][c]))
		}
	}
	out := make([]string, len(rows))
	for ri := range rows {
		parts := make([]string, ncol)
		if ri == 1 {
			for c := range parts {
				parts[c] = strings.Repeat("─", width[c])
			}
			out[ri] = strings.Join(parts, "─┼─")
			continue
		}
		for c, cell := range cells[ri] {
			pad := width[c] - runewidth.StringWidth(cell)
			left := 0
			switch align[c] {
			case 'r':
				left = pad
			case 'c':
				left = pad / 2
			}
			parts[c] = strings.Repeat(" ", left) + cell + strings.Repeat(" ", pad-left)
		}
		out[ri] = strings.TrimRight(strings.Join(parts, " │ "), " ")
	}
	return out
}

// ---------------------------------------------------------------- quotes

// quoteDepth returns how deeply l is quoted ("> > x" is 2) and the text
// after the markers.
func quoteDepth(l string) (int, string) {
	depth, rest := 0, l
	for {
		t := strings.TrimLeft(rest, " ")
		if len(rest)-len(t) > 3 || !strings.HasPrefix(t, ">") {
			break
		}
		depth++
		rest = strings.TrimPrefix(t[1:], " ")
	}
	if depth == 0 {
		return 0, l
	}
	return depth, rest
}

// mdAlertRe matches the first line of an alert, "[!NOTE]", or of an
// Obsidian callout, "[!tip]- Title" (the fold marker is ignored).
var mdAlertRe = regexp.MustCompile(`^\[!([A-Za-z]+)\][-+]?\s*(.*)$`)

var mdAlertIcons = map[string]string{
	"note": "ℹ", "info": "ℹ", "tip": "★", "hint": "★", "important": "‼",
	"warning": "⚠", "caution": "✖", "danger": "✖", "error": "✖",
}

// alertTitle returns the rendered title line of an alert ("ℹ Note"),
// "" when body does not open one.
func alertTitle(body string) string {
	t := strings.TrimSpace(body)
	if !strings.HasPrefix(t, "[!") {
		return "" // skip the regexp on ordinary lines
	}
	m := mdAlertRe.FindStringSubmatch(t)
	if m == nil {
		return ""
	}
	kind := strings.ToLower(m[1])
	title := strings.TrimSpace(m[2])
	if title == "" {
		title = string(unicode.ToUpper(rune(kind[0]))) + kind[1:]
	}
	icon, ok := mdAlertIcons[kind]
	if !ok {
		icon = "▸"
	}
	return icon + " " + title
}

// ---------------------------------------------------------------- rules

// isRule reports a thematic break: three or more -, * or _ (the same
// one), spaces allowed between them, indented at most three spaces.
func isRule(l string) bool {
	if indent(l) > 3 {
		return false
	}
	t := strings.TrimSpace(l)
	if t == "" || !strings.ContainsRune("-*_", rune(t[0])) {
		return false
	}
	n := 0
	for _, r := range t {
		switch {
		case r == rune(t[0]):
			n++
		case r != ' ' && r != '\t':
			return false
		}
	}
	return n >= 3
}

// isSetextUnderline reports a "---" that underlines prev as a heading
// instead of being a rule: dashes only, after a paragraph line.
func isSetextUnderline(l, prev string) bool {
	t := strings.TrimSpace(l)
	if strings.Trim(t, "-") != "" {
		return false
	}
	p := strings.TrimSpace(prev)
	if p == "" || isRule(prev) || strings.HasPrefix(p, "#") || strings.HasPrefix(p, ">") ||
		strings.HasPrefix(p, "```") || strings.HasPrefix(p, "~~~") {
		return false
	}
	return !mdBulletRe.MatchString(prev) && !mdNumberRe.MatchString(prev)
}

// ---------------------------------------------------------------- tasks

// taskItem draws the checkbox of a task list item (mdTaskRe): "- [ ] a"
// reads "☐ a", "1. [x] b" reads "1. ☑ b".
func taskItem(l string) string {
	if !strings.Contains(l, "[") {
		return l // skip the regexp on ordinary lines
	}
	m := mdTaskRe.FindStringSubmatch(l)
	if m == nil {
		return l
	}
	box := "☐"
	if m[2] != " " {
		box = "☑"
	}
	lead := m[1] // indent and marker
	if mdBulletRe.MatchString(lead) {
		lead = lead[:len(lead)-len(strings.TrimLeft(lead, " \t"))]
	}
	if rest := strings.TrimLeft(l[len(m[0]):], " "); rest != "" {
		return lead + box + " " + rest
	}
	return lead + box
}
