// Package table parses delimited text data (CSV, TSV and friends) and
// computes simple column statistics for the data viewer. Only open,
// plain-text formats are supported.
package table

import (
	"encoding/csv"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// Table is parsed delimited data.
type Table struct {
	Header []string
	Rows   [][]string // every row has len(Header) cells
	Lines  []int      // 0-based source line where each row starts
	Ends   []int      // line after the last line of each row
	Delim  rune
	// HeaderLine / HeaderEnd are the lines of the header (-1 without one).
	HeaderLine, HeaderEnd int
	BOM                   bool // the text started with a byte order mark
	// Err is set when parsing stopped early; Rows holds what was read.
	Err error
}

// Extensions maps file extensions to their delimiter (0 = sniff).
var Extensions = map[string]rune{".csv": 0, ".tsv": '\t', ".tab": '\t', ".psv": '|', ".ssv": ';'}

// IsDataFile reports whether name has a supported data extension.
func IsDataFile(name string) bool {
	_, ok := Extensions[strings.ToLower(filepath.Ext(name))]
	return ok
}

// Sniff guesses the delimiter of text: the extension decides when it is
// unambiguous, else the candidate that splits the first lines into the
// same number (> 1) of fields most consistently.
func Sniff(name, text string) rune {
	if d := Extensions[strings.ToLower(filepath.Ext(name))]; d != 0 {
		return d
	}
	var lines []string
	for _, l := range strings.SplitN(text, "\n", 51) {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > 50 {
		lines = lines[:50]
	}
	best, bestScore := ',', -1.0
	for _, d := range []rune{',', '\t', ';', '|'} {
		counts := map[int]int{}
		for _, l := range lines {
			counts[countOutsideQuotes(l, d)]++
		}
		mode, modeN := 0, 0
		for c, n := range counts {
			if n > modeN || (n == modeN && c > mode) {
				mode, modeN = c, n
			}
		}
		if mode == 0 || len(lines) == 0 {
			continue
		}
		// consistency first, then more columns
		score := float64(modeN)/float64(len(lines))*1000 + float64(mode)
		if score > bestScore {
			best, bestScore = d, score
		}
	}
	return best
}

func countOutsideQuotes(line string, d rune) int {
	n, quoted := 0, false
	for _, r := range line {
		switch {
		case r == '"':
			quoted = !quoted
		case r == d && !quoted:
			n++
		}
	}
	return n
}

// Parse reads delimited text. With header, the first row names the
// columns, else they are named 1, 2, … Ragged rows are padded with
// empty cells. Tab-separated data is split literally (TSV has no
// quoting); other delimiters follow RFC 4180 quoting, leniently.
func Parse(text string, delim rune, header bool) *Table {
	t := &Table{Delim: delim, HeaderLine: -1, HeaderEnd: -1}
	if strings.HasPrefix(text, "\ufeff") { // byte order mark of some exports
		text, t.BOM = text[len("\ufeff"):], true
	}
	var recs [][]string
	if delim == '\t' {
		for i, l := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
			l = strings.TrimSuffix(l, "\r")
			if l == "" {
				continue
			}
			recs = append(recs, strings.Split(l, "\t"))
			t.Lines = append(t.Lines, i)
			t.Ends = append(t.Ends, i+1)
		}
	} else {
		r := csv.NewReader(strings.NewReader(text))
		r.Comma = delim
		r.LazyQuotes = true
		r.FieldsPerRecord = -1
		counted, nl := 0, 0 // newlines in text[:counted]
		for {
			rec, err := r.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Err = err
				break
			}
			line, _ := r.FieldPos(0)
			off := int(r.InputOffset())
			nl += strings.Count(text[counted:off], "\n")
			counted = off
			end := nl
			if off > 0 && text[off-1] != '\n' {
				end++ // last record without a final newline
			}
			recs = append(recs, rec)
			t.Lines = append(t.Lines, line-1)
			t.Ends = append(t.Ends, end)
		}
	}
	ncol := 0
	for _, r := range recs {
		ncol = max(ncol, len(r))
	}
	if header && len(recs) > 0 {
		t.Header = pad(append([]string(nil), recs[0]...), ncol)
		t.HeaderLine, t.HeaderEnd = t.Lines[0], t.Ends[0]
		recs = recs[1:]
		t.Lines = t.Lines[1:]
		t.Ends = t.Ends[1:]
	} else {
		t.Header = make([]string, ncol)
		for i := range t.Header {
			t.Header[i] = strconv.Itoa(i + 1)
		}
	}
	for i, h := range t.Header {
		if strings.TrimSpace(h) == "" {
			t.Header[i] = strconv.Itoa(i + 1)
		}
	}
	t.Rows = make([][]string, len(recs))
	for i, r := range recs {
		t.Rows[i] = pad(r, ncol)
	}
	return t
}

func pad(r []string, n int) []string {
	if len(r) >= n {
		return r
	}
	out := make([]string, n)
	copy(out, r)
	return out
}

// Column returns the index of a column given by name (case-insensitive)
// or 1-based number, or -1.
func (t *Table) Column(s string) int {
	s = strings.TrimSpace(s)
	for i, h := range t.Header {
		if h == s {
			return i
		}
	}
	for i, h := range t.Header {
		if strings.EqualFold(h, s) {
			return i
		}
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
		if n > len(t.Header) {
			return -1
		}
	}
	if n >= 1 && n <= len(t.Header) && s != "" {
		return n - 1
	}
	return -1
}

// Encode writes one record in the table's format: RFC 4180 quoting for
// delimited text (cells with the delimiter, quotes or line breaks are
// quoted), plain tab-joined cells for TSV.
func Encode(cells []string, delim rune) (string, error) {
	if delim == '\t' {
		if len(cells) == 1 && cells[0] == "" {
			return "", errors.New("an empty row of a one-column TSV file cannot be written (it would read as a blank line)")
		}
		for _, c := range cells {
			if strings.ContainsAny(c, "\t\n\r") {
				return "", errors.New("tab-separated cells cannot contain tabs or line breaks")
			}
		}
		return strings.Join(cells, "\t"), nil
	}
	if len(cells) == 1 && cells[0] == "" {
		return `""`, nil // an empty line would be skipped as a blank line
	}
	var sb strings.Builder
	w := csv.NewWriter(&sb)
	w.Comma = delim
	if err := w.Write(cells); err != nil {
		return "", err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", err
	}
	return strings.TrimSuffix(sb.String(), "\n"), nil
}
