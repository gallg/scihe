package editor

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"scihe/internal/buffer"
)

type searchState struct {
	pattern string
	forward bool
	re      *regexp.Regexp
	reSrc   string
	noHL    bool // :nohlsearch until next search
}

// compilePattern converts a vim-flavoured pattern to a Go regexp.
// Supported vim-isms: \< \> (word boundaries), \c / \C (case), smartcase.
func (e *Editor) compilePattern(pat string) (*regexp.Regexp, error) {
	ignore := e.cfg.Options.IgnoreCase
	if strings.Contains(pat, `\c`) {
		ignore = true
		pat = strings.ReplaceAll(pat, `\c`, "")
	}
	if strings.Contains(pat, `\C`) {
		ignore = false
		pat = strings.ReplaceAll(pat, `\C`, "")
	} else if ignore && e.cfg.Options.SmartCase && hasUpper(pat) {
		ignore = false
	}
	pat = strings.ReplaceAll(pat, `\<`, `\b`)
	pat = strings.ReplaceAll(pat, `\>`, `\b`)
	if ignore {
		pat = "(?i)" + pat
	}
	return regexp.Compile(pat)
}

func hasUpper(s string) bool {
	for i, r := range s {
		if unicode.IsUpper(r) {
			// skip escapes like \S \W \B
			if i > 0 && s[i-1] == '\\' {
				continue
			}
			return true
		}
	}
	return false
}

func (e *Editor) searchRegexp() *regexp.Regexp {
	if e.search.pattern == "" {
		return nil
	}
	if e.search.re == nil || e.search.reSrc != e.search.pattern {
		re, err := e.compilePattern(e.search.pattern)
		if err != nil {
			return nil
		}
		e.search.re, e.search.reSrc = re, e.search.pattern
	}
	return e.search.re
}

// runeIndex converts a byte offset in s to a rune offset.
func runeIndex(s string, byteOff int) int {
	return utf8.RuneCountInString(s[:byteOff])
}

// findMatch searches from p (exclusive) in the given direction, wrapping.
func findMatch(b *buffer.Buffer, re *regexp.Regexp, p Pos, forward bool) (Pos, Pos, bool, bool) {
	n := b.LineCount()
	wrapped := false
	if forward {
		for i := 0; i <= n; i++ {
			l := (p.Line + i) % n
			if p.Line+i >= n {
				wrapped = true
			}
			line := b.LineString(l)
			locs := re.FindAllStringIndex(line, -1)
			for _, loc := range locs {
				s := runeIndex(line, loc[0])
				if i == 0 && s <= p.Col {
					continue
				}
				en := runeIndex(line, loc[1])
				return Pos{l, s}, Pos{l, en}, wrapped, true
			}
		}
		return p, p, false, false
	}
	for i := 0; i <= n; i++ {
		l := p.Line - i
		if l < 0 {
			l += n
			wrapped = true
		}
		line := b.LineString(l)
		locs := re.FindAllStringIndex(line, -1)
		for j := len(locs) - 1; j >= 0; j-- {
			s := runeIndex(line, locs[j][0])
			if i == 0 && s >= p.Col {
				continue
			}
			en := runeIndex(line, locs[j][1])
			return Pos{l, s}, Pos{l, en}, wrapped, true
		}
	}
	return p, p, false, false
}

// searchMotion finds the count-th match of the current pattern.
func (e *Editor) searchMotion(c *ctx, forward bool) (Pos, MotionType, bool) {
	re := e.searchRegexp()
	if re == nil {
		e.errorf("E35: No previous regular expression")
		return c.w.cur, Exclusive, false
	}
	e.search.noHL = false
	p := c.w.cur
	wrappedAny := false
	for i := 0; i < c.count; i++ {
		s, _, wrapped, ok := findMatch(c.b, re, p, forward)
		if !ok {
			e.errorf("E486: Pattern not found: %s", e.search.pattern)
			return c.w.cur, Exclusive, false
		}
		wrappedAny = wrappedAny || wrapped
		p = s
	}
	if wrappedAny {
		if forward {
			e.warnf("search hit BOTTOM, continuing at TOP")
		} else {
			e.warnf("search hit TOP, continuing at BOTTOM")
		}
	} else {
		dir := "/"
		if !forward {
			dir = "?"
		}
		e.infof("%s%s", dir, e.search.pattern)
	}
	return p, Exclusive, true
}

func mSearchNext(reverse bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		fwd := c.e.search.forward
		if reverse {
			fwd = !fwd
		}
		return c.e.searchMotion(c, fwd)
	}
}

// mStar searches for the word under the cursor (* and #).
func mStar(forward, whole bool) MotionFunc {
	return func(c *ctx) (Pos, MotionType, bool) {
		line := c.b.Line(c.w.cur.Line)
		col := c.w.cur.Col
		for col < len(line) && charClass(line[col], false) != clsWord {
			col++
		}
		if col >= len(line) {
			c.e.errorf("E348: No string under cursor")
			return c.w.cur, Exclusive, false
		}
		s, en := col, col
		for s > 0 && charClass(line[s-1], false) == clsWord {
			s--
		}
		for en < len(line) && charClass(line[en], false) == clsWord {
			en++
		}
		word := regexp.QuoteMeta(string(line[s:en]))
		// Go's \b only knows ASCII word characters, so anchor only on
		// sides where the word starts/ends with an ASCII letter or digit
		if whole {
			if line[s] < 128 {
				word = `\<` + word
			}
			if line[en-1] < 128 {
				word += `\>`
			}
		}
		c.e.search.pattern = word
		c.e.search.forward = forward
		c.e.regs.SetSpecial('/', word)
		c.e.pushHistory('/', word)
		// start from the word start so "#" does not stop on the current word
		save := c.w.cur
		c.w.cur = Pos{c.w.cur.Line, s}
		p, mt, ok := c.e.searchMotion(c, forward)
		if !ok {
			c.w.cur = save
		}
		return p, mt, ok
	}
}

// matchesInLine returns rune ranges of search matches on a line for hlsearch.
func (e *Editor) matchesInLine(line string) [][2]int {
	if !e.cfg.Options.HLSearch || e.search.noHL {
		return nil
	}
	re := e.searchRegexp()
	if re == nil {
		return nil
	}
	var out [][2]int
	for _, loc := range re.FindAllStringIndex(line, 50) {
		if loc[0] == loc[1] {
			continue
		}
		out = append(out, [2]int{runeIndex(line, loc[0]), runeIndex(line, loc[1])})
	}
	return out
}
