package editor

import (
	"regexp"
	"strings"
	"unicode"
)

// Recognising the block elements of markdown for the rendered view
// (mdlayout.go): front matter, GFM tables, blockquotes and alerts
// ("> [!NOTE]", also Obsidian callouts) and horizontal rules.

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
