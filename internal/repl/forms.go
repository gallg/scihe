// Package repl implements interactive Lisp development: form extraction
// from source text, an nREPL client (Clojure) and a Chez Scheme session.
package repl

import (
	"regexp"
)

// Dialect selects reader rules.
type Dialect int

const (
	Clojure Dialect = iota
	Scheme
)

// DialectFor maps a filetype to a dialect.
func DialectFor(ft string) (Dialect, bool) {
	switch ft {
	case "clojure":
		return Clojure, true
	case "scheme", "racket", "common lisp", "emacslisp", "newlisp":
		return Scheme, true
	}
	return 0, false
}

// Form is a [Start, End) rune range of a datum.
type Form struct {
	Start, End int
	Depth      int  // 0 for top-level
	List       bool // delimited by () [] {}
}

func isSpace(r rune, d Dialect) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || (d == Clojure && r == ',')
}

func isOpen(r rune) bool  { return r == '(' || r == '[' || r == '{' }
func isClose(r rune) bool { return r == ')' || r == ']' || r == '}' }

func prefixChar(r rune, d Dialect) bool {
	switch r {
	case '\'', '`', '#', '@', '~', '^', '?':
		return true
	case ',':
		return d == Scheme
	}
	return false
}

// Parse returns every datum in text (lists and atoms at all depths),
// in order of their start. Unbalanced closers are ignored.
func Parse(text []rune, d Dialect) []Form {
	var forms []Form
	type open struct {
		start, idx int
	}
	var stack []open
	n := len(text)
	atomStart := -1
	endAtom := func(i int) {
		if atomStart >= 0 {
			forms = append(forms, Form{Start: atomStart, End: i, Depth: len(stack)})
			atomStart = -1
		}
	}
	for i := 0; i < n; i++ {
		r := text[i]
		switch {
		case r == ';' && d == Scheme && i > 0 && text[i-1] == '#' && atomStart == i-1:
			// #; datum comment: treat as a prefix of the next datum
		case r == ';':
			endAtom(i)
			for i < n && text[i] != '\n' {
				i++
			}
		case d == Scheme && r == '#' && i+1 < n && text[i+1] == '|':
			endAtom(i)
			depth := 1
			i += 2
			for i < n && depth > 0 {
				if text[i] == '|' && i+1 < n && text[i+1] == '#' {
					depth--
					i++
				} else if text[i] == '#' && i+1 < n && text[i+1] == '|' {
					depth++
					i++
				}
				i++
			}
			i--
		case d == Scheme && r == '#' && i+1 < n && text[i+1] == '\\':
			// character literal #\x, #\space
			if atomStart < 0 {
				atomStart = i
			}
			i += 2
		case d == Clojure && r == '\\':
			if atomStart < 0 {
				atomStart = i
			}
			i++
		case r == '"':
			start := i
			if atomStart >= 0 && atomStart == i-1 && text[i-1] == '#' {
				start = atomStart // regex literal #"..."
			} else {
				endAtom(i)
			}
			atomStart = -1
			i++
			for i < n && text[i] != '"' {
				if text[i] == '\\' {
					i++
				}
				i++
			}
			end := i + 1
			if end > n {
				end = n
			}
			forms = append(forms, Form{Start: start, End: end, Depth: len(stack)})
		case isOpen(r):
			start := i
			if atomStart >= 0 && allPrefix(text[atomStart:i], d) {
				start = atomStart
			} else {
				endAtom(i)
			}
			atomStart = -1
			forms = append(forms, Form{Start: start, Depth: len(stack), List: true})
			stack = append(stack, open{start, len(forms) - 1})
		case isClose(r):
			endAtom(i)
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				forms[top.idx].End = i + 1
			}
		case isSpace(r, d):
			endAtom(i)
		default:
			if atomStart < 0 {
				atomStart = i
			}
		}
	}
	endAtom(n)
	// unterminated lists extend to end of text
	for _, o := range stack {
		forms[o.idx].End = n
	}
	// a quote prefix followed by whitespace-free atom: merge 'foo into one atom
	return forms
}

func allPrefix(rs []rune, d Dialect) bool {
	if s := string(rs); d == Scheme && (s == "#u8" || s == "#vu8" || s == "#;" || s == "'#") {
		return true
	}
	for _, r := range rs {
		if !prefixChar(r, d) {
			return false
		}
	}
	return true
}

// FormAt returns the innermost list containing off, or the atom at off.
// When off is just past a form's end, that form is used (cursor after a
// closing paren).
func FormAt(text []rune, off int, d Dialect) (Form, bool) {
	forms := Parse(text, d)
	best := -1
	for i, f := range forms {
		if off >= f.Start && off < f.End {
			if best < 0 || f.Depth >= forms[best].Depth {
				best = i
			}
		}
	}
	if best >= 0 {
		f := forms[best]
		if !f.List {
			// prefer the enclosing list for atoms inside lists (eval-form semantics)
			for i := best - 1; i >= 0; i-- {
				g := forms[i]
				if g.List && g.Start <= f.Start && g.End >= f.End {
					return g, true
				}
			}
		}
		return f, true
	}
	for i := len(forms) - 1; i >= 0; i-- {
		if forms[i].End == off {
			return forms[i], true
		}
	}
	return Form{}, false
}

// TopLevelAt returns the top-level form containing off (or ending at off).
func TopLevelAt(text []rune, off int, d Dialect) (Form, bool) {
	for _, f := range Parse(text, d) {
		if f.Depth == 0 && off >= f.Start && off <= f.End {
			if off == f.End && off < len(text) && !isSpace(text[off], d) && !isClose(text[off]) {
				continue
			}
			return f, true
		}
	}
	return Form{}, false
}

var nsRe = regexp.MustCompile(`(?m)^\s*\(\s*(?:ns\s+|in-ns\s+')`)

// ClojureNS returns the namespace declared in a Clojure buffer, skipping
// metadata such as ^:no-doc or ^{:doc "..."} before the name.
func ClojureNS(text string) string {
	loc := nsRe.FindStringIndex(text)
	if loc == nil {
		return ""
	}
	rest := []rune(text[loc[1]:])
	i := 0
	skipSpace := func() {
		for i < len(rest) && (rest[i] == ' ' || rest[i] == '\t' || rest[i] == '\n' || rest[i] == '\r' || rest[i] == ',') {
			i++
		}
	}
	for {
		skipSpace()
		if i >= len(rest) || rest[i] != '^' {
			break
		}
		i++
		if i < len(rest) && rest[i] == '{' {
			// skip a balanced map literal
			f, ok := FormAt(rest, i, Clojure)
			if !ok {
				return ""
			}
			i = f.End
			continue
		}
		for i < len(rest) && !isSpace(rest[i], Clojure) && !isOpen(rest[i]) && !isClose(rest[i]) {
			i++
		}
	}
	start := i
	for i < len(rest) && !isSpace(rest[i], Clojure) && !isOpen(rest[i]) && !isClose(rest[i]) && rest[i] != '"' {
		i++
	}
	return string(rest[start:i])
}
