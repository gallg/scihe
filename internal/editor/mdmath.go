package editor

import (
	"strings"
	"unicode"
)

// Formulas in markdown ($…$, $$…$$, \(…\), \[…\] and math environments
// such as \begin{equation}…\end{equation}) shown as Unicode text:
// symbols, Greek letters, super- and subscripts, fractions on one line.
// The text keeps its lines, so a rendered line is the source line.

// renderMath replaces the formulas of a markdown text; code is left alone.
func renderMath(src string) string {
	lines := strings.Split(src, "\n")
	fence := ""  // the opening run of the open ``` or ~~~ fence
	closer := "" // the delimiter closing a display block left open
	for i, l := range lines {
		if closer == "" && fenceStep(strings.TrimSpace(l), &fence) {
			continue
		}
		if fence == "" {
			lines[i], closer = mathLine(l, closer)
		}
	}
	return strings.Join(lines, "\n")
}

// mathLine renders the formulas of one line; closer is the delimiter of
// a display block still open from an earlier line (returned updated).
func mathLine(line, closer string) (string, string) {
	var sb strings.Builder
	rs := []rune(line)
	i := 0
	if closer != "" {
		end, ok := findDelim(rs, 0, closer)
		sb.WriteString(texToUnicode(string(rs[:end])))
		if !ok {
			return sb.String(), closer
		}
		i, closer = end+len(closer), ""
	}
	for i < len(rs) {
		r := rs[i]
		switch {
		case r == '`':
			n := runLen(rs, i, '`')
			end, ok := closingTicks(rs, i+n, n)
			if !ok {
				end = i + n
			}
			sb.WriteString(string(rs[i:end]))
			i = end
		case r == '\\' && i+1 < len(rs) && rs[i+1] == '$':
			sb.WriteRune('$')
			i += 2
		case hasAt(rs, i, "$$") || hasAt(rs, i, `\[`) || mathEnvAt(rs, i) != "":
			open, cl := "$$", "$$"
			if env := mathEnvAt(rs, i); env != "" {
				open, cl = `\begin{`+env+"}", `\end{`+env+"}"
			} else if r == '\\' {
				open, cl = `\[`, `\]`
			}
			end, ok := findDelim(rs, i+len(open), cl)
			sb.WriteString(texToUnicode(string(rs[i+len(open) : end])))
			if !ok {
				return sb.String(), cl
			}
			i = end + len(cl)
		case hasAt(rs, i, `\(`):
			end, ok := findDelim(rs, i+2, `\)`)
			if !ok {
				sb.WriteString(string(rs[i:]))
				return sb.String(), ""
			}
			sb.WriteString(texToUnicode(string(rs[i+2 : end])))
			i = end + 2
		case r == '$':
			// a blank after the opening dollar needs one before the
			// closing dollar ("$ x $" or "$x$") and one before the
			// opening dollar, and no digit may follow the closing one:
			// "$5 and $10" and "$5, then $x$" are not formulas
			end := -1
			spaced := i+1 < len(rs) && unicode.IsSpace(rs[i+1])
			if i+1 < len(rs) && (!spaced || i == 0 || unicode.IsSpace(rs[i-1])) {
				for j := i + 1; j < len(rs); j++ {
					if rs[j] == '\\' {
						j++
						continue
					}
					if rs[j] == '$' {
						if unicode.IsSpace(rs[j-1]) == spaced && strings.TrimSpace(string(rs[i+1:j])) != "" &&
							(j+1 >= len(rs) || !unicode.IsDigit(rs[j+1])) {
							end = j
						}
						break
					}
				}
			}
			if end < 0 {
				sb.WriteRune(r)
				i++
				continue
			}
			sb.WriteString(texToUnicode(strings.TrimSpace(string(rs[i+1 : end]))))
			i = end + 1
		default:
			sb.WriteRune(r)
			i++
		}
	}
	return sb.String(), ""
}

// mathEnvs are the LaTeX environments rendered without dollars, as
// Jupyter (MathJax) does.
var mathEnvs = map[string]bool{
	"equation": true, "equation*": true, "align": true, "align*": true, "gather": true,
	"gather*": true, "multline": true, "multline*": true, "eqnarray": true, "eqnarray*": true,
	"flalign": true, "flalign*": true, "displaymath": true, "math": true,
}

// mathEnvAt returns the name of the math environment opened at i.
func mathEnvAt(rs []rune, i int) string {
	if !hasAt(rs, i, `\begin{`) {
		return ""
	}
	end, ok := findDelim(rs, i+7, "}")
	if name := string(rs[i+7 : end]); ok && mathEnvs[name] {
		return name
	}
	return ""
}

func hasAt(rs []rune, i int, s string) bool {
	return strings.HasPrefix(string(rs[i:min(i+len(s), len(rs))]), s)
}

// findDelim returns the index of delim from i on (len(rs), false when
// missing); escaped characters are skipped.
func findDelim(rs []rune, i int, delim string) (int, bool) {
	for ; i < len(rs); i++ {
		if hasAt(rs, i, delim) {
			return i, true
		}
		if rs[i] == '\\' {
			i++
		}
	}
	return len(rs), false
}

func runLen(rs []rune, i int, r rune) int {
	n := 0
	for i+n < len(rs) && rs[i+n] == r {
		n++
	}
	return n
}

// closingTicks returns the end of the code span whose opening run of n
// backticks ends at i; false when no run of n closes it (the backticks
// are then plain text).
func closingTicks(rs []rune, i, n int) (int, bool) {
	for i < len(rs) {
		if rs[i] != '`' {
			i++
			continue
		}
		m := runLen(rs, i, '`')
		if m == n {
			return i + m, true
		}
		i += m
	}
	return i, false
}

// ---------------------------------------------------------------- TeX

// texToUnicode converts a TeX formula; unknown commands stay as written.
func texToUnicode(s string) string {
	p := &texParser{rs: []rune(s)}
	return p.seq(false)
}

type texParser struct {
	rs    []rune
	i     int
	depth int // nested groups: bounded, the text may be anything
}

// texMaxDepth bounds the recursion of seq; deeper text is kept as is.
const texMaxDepth = 50

// seq converts up to the end, or up to the closing brace of a group.
func (p *texParser) seq(group bool) string {
	if p.depth >= texMaxDepth {
		s := string(p.rs[p.i:])
		p.i = len(p.rs)
		return s
	}
	p.depth++
	defer func() { p.depth-- }()
	var sb strings.Builder
	for p.i < len(p.rs) {
		r := p.rs[p.i]
		switch r {
		case '}':
			p.i++
			if group {
				return sb.String()
			}
		case '^', '_':
			p.i++
			sb.WriteString(script(p.arg(), r == '^'))
		case '\'':
			p.i++
			sb.WriteRune('′')
		case '&', '~':
			p.i++
			sb.WriteRune(' ')
		default:
			sb.WriteString(p.atom())
		}
	}
	return sb.String()
}

// atom converts one group, command or character.
func (p *texParser) atom() string {
	r := p.rs[p.i]
	p.i++
	switch r {
	case '{':
		return p.seq(true)
	case '\\':
		return p.command()
	}
	return string(r)
}

// arg reads a command argument: a group, a command or one character.
func (p *texParser) arg() string {
	for p.i < len(p.rs) && p.rs[p.i] == ' ' {
		p.i++
	}
	if p.i >= len(p.rs) || p.rs[p.i] == '}' {
		return ""
	}
	return p.atom()
}

// rawArg reads a {…} argument as plain text (environment names, \text).
func (p *texParser) rawArg() string {
	for p.i < len(p.rs) && p.rs[p.i] == ' ' {
		p.i++
	}
	if p.i >= len(p.rs) || p.rs[p.i] != '{' {
		return ""
	}
	start := p.i + 1
	depth := 0
	for ; p.i < len(p.rs); p.i++ {
		switch p.rs[p.i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				p.i++
				return string(p.rs[start : p.i-1])
			}
		}
	}
	return string(p.rs[start:])
}

func (p *texParser) command() string {
	if p.i >= len(p.rs) {
		return `\`
	}
	start := p.i
	for p.i < len(p.rs) && unicode.IsLetter(p.rs[p.i]) {
		p.i++
	}
	if p.i == start {
		p.i++ // a one-character command: \, \{ \\ …
	}
	name := string(p.rs[start:p.i])
	if s, ok := texSymbols[name]; ok {
		return s
	}
	switch name {
	case ",", ":", ";", " ", "\\":
		return " "
	case "!", "left", "right", "big", "Big", "bigg", "Bigg", "displaystyle", "textstyle", "limits", "nolimits":
		if name == "left" || name == "right" {
			if p.i < len(p.rs) && p.rs[p.i] == '.' {
				p.i++ // \left. is an invisible delimiter
			}
		}
		return ""
	case "quad":
		return "  "
	case "qquad":
		return "    "
	case "frac", "dfrac", "tfrac":
		a, b := p.arg(), p.arg()
		return wrapTerm(a) + "/" + wrapTerm(b)
	case "sqrt":
		root := "√"
		if p.i < len(p.rs) && p.rs[p.i] == '[' {
			end, _ := findDelim(p.rs, p.i, "]")
			sub := &texParser{rs: p.rs[p.i+1 : end], depth: p.depth} // one depth budget
			n := sub.seq(false)
			p.i = min(end+1, len(p.rs))
			switch n {
			case "3":
				root = "∛"
			case "4":
				root = "∜"
			default:
				root = script(n, true) + "√"
			}
		}
		return root + wrapTerm(p.arg())
	case "text", "textrm", "textit", "textbf", "mbox", "operatorname":
		return p.rawArg()
	case "mathrm", "mathit", "mathbf", "boldsymbol", "mathsf", "mathtt", "bm":
		return p.arg()
	case "mathbb":
		return alphabet(p.arg(), mathbb)
	case "mathcal", "mathscr":
		return alphabet(p.arg(), mathcal)
	case "begin", "end":
		p.rawArg()
		return ""
	}
	if mark, ok := texAccents[name]; ok {
		var sb strings.Builder
		for _, r := range p.arg() {
			sb.WriteRune(r)
			sb.WriteRune(mark)
		}
		return sb.String()
	}
	if p.i < len(p.rs) && p.rs[p.i] == '{' {
		p.i++
		return `\` + name + "{" + p.seq(true) + "}"
	}
	return `\` + name
}

// wrapTerm parenthesises a fraction or root operand of several symbols.
func wrapTerm(s string) string {
	if len([]rune(s)) <= 1 || strings.IndexFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) < 0 {
		return s
	}
	return "(" + s + ")"
}

// script writes s raised or lowered with Unicode characters, or as ^(…)
// / _(…) when some character has no such form.
func script(s string, sup bool) string {
	s = strings.ReplaceAll(s, " ", "")
	m, mark := subscripts, "_"
	if sup {
		m, mark = superscripts, "^"
	}
	var sb strings.Builder
	for _, r := range s {
		c, ok := m[r]
		if !ok {
			if len([]rune(s)) == 1 {
				return mark + s
			}
			return mark + "(" + s + ")"
		}
		sb.WriteRune(c)
	}
	return sb.String()
}

func alphabet(s string, f func(r rune) (rune, bool)) string {
	var sb strings.Builder
	for _, r := range s {
		if c, ok := f(r); ok {
			r = c
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

func mathbb(r rune) (rune, bool) {
	if c, ok := map[rune]rune{'C': 'ℂ', 'H': 'ℍ', 'N': 'ℕ', 'P': 'ℙ', 'Q': 'ℚ', 'R': 'ℝ', 'Z': 'ℤ'}[r]; ok {
		return c, true
	}
	switch {
	case r >= 'A' && r <= 'Z':
		return 0x1D538 + r - 'A', true
	case r >= 'a' && r <= 'z':
		return 0x1D552 + r - 'a', true
	case r >= '0' && r <= '9':
		return 0x1D7D8 + r - '0', true
	}
	return r, false
}

func mathcal(r rune) (rune, bool) {
	if c, ok := map[rune]rune{'B': 'ℬ', 'E': 'ℰ', 'F': 'ℱ', 'H': 'ℋ', 'I': 'ℐ', 'L': 'ℒ', 'M': 'ℳ', 'R': 'ℛ'}[r]; ok {
		return c, true
	}
	if r >= 'A' && r <= 'Z' {
		return 0x1D49C + r - 'A', true
	}
	return r, false
}

var texAccents = map[string]rune{
	"hat": '̂', "widehat": '̂', "bar": '̄', "overline": '̅',
	"vec": '⃗', "dot": '̇', "ddot": '̈', "tilde": '̃', "widetilde": '̃',
}

var superscripts = map[rune]rune{
	'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
	'+': '⁺', '-': '⁻', '−': '⁻', '=': '⁼', '(': '⁽', ')': '⁾', '′': '′', '*': '*', ',': ',',
	'a': 'ᵃ', 'b': 'ᵇ', 'c': 'ᶜ', 'd': 'ᵈ', 'e': 'ᵉ', 'f': 'ᶠ', 'g': 'ᵍ', 'h': 'ʰ', 'i': 'ⁱ', 'j': 'ʲ',
	'k': 'ᵏ', 'l': 'ˡ', 'm': 'ᵐ', 'n': 'ⁿ', 'o': 'ᵒ', 'p': 'ᵖ', 'r': 'ʳ', 's': 'ˢ', 't': 'ᵗ', 'u': 'ᵘ',
	'v': 'ᵛ', 'w': 'ʷ', 'x': 'ˣ', 'y': 'ʸ', 'z': 'ᶻ',
	'A': 'ᴬ', 'B': 'ᴮ', 'D': 'ᴰ', 'E': 'ᴱ', 'G': 'ᴳ', 'H': 'ᴴ', 'I': 'ᴵ', 'J': 'ᴶ', 'K': 'ᴷ', 'L': 'ᴸ',
	'M': 'ᴹ', 'N': 'ᴺ', 'O': 'ᴼ', 'P': 'ᴾ', 'R': 'ᴿ', 'T': 'ᵀ', 'U': 'ᵁ', 'V': 'ⱽ', 'W': 'ᵂ',
	'β': 'ᵝ', 'γ': 'ᵞ', 'δ': 'ᵟ', 'θ': 'ᶿ', 'φ': 'ᵠ', 'χ': 'ᵡ',
}

var subscripts = map[rune]rune{
	'0': '₀', '1': '₁', '2': '₂', '3': '₃', '4': '₄', '5': '₅', '6': '₆', '7': '₇', '8': '₈', '9': '₉',
	'+': '₊', '-': '₋', '−': '₋', '=': '₌', '(': '₍', ')': '₎', ',': ',',
	'a': 'ₐ', 'e': 'ₑ', 'h': 'ₕ', 'i': 'ᵢ', 'j': 'ⱼ', 'k': 'ₖ', 'l': 'ₗ', 'm': 'ₘ', 'n': 'ₙ', 'o': 'ₒ',
	'p': 'ₚ', 'r': 'ᵣ', 's': 'ₛ', 't': 'ₜ', 'u': 'ᵤ', 'v': 'ᵥ', 'x': 'ₓ',
	'β': 'ᵦ', 'γ': 'ᵧ', 'ρ': 'ᵨ', 'φ': 'ᵩ', 'χ': 'ᵪ',
}

var texSymbols = map[string]string{
	// Greek
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ϵ", "varepsilon": "ε",
	"zeta": "ζ", "eta": "η", "theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ",
	"lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ", "pi": "π", "varpi": "ϖ", "rho": "ρ",
	"varrho": "ϱ", "sigma": "σ", "varsigma": "ς", "tau": "τ", "upsilon": "υ", "phi": "ϕ",
	"varphi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω",
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ", "Pi": "Π",
	"Sigma": "Σ", "Upsilon": "Υ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
	// big operators
	"sum": "∑", "prod": "∏", "coprod": "∐", "int": "∫", "iint": "∬", "iiint": "∭", "oint": "∮",
	"bigcup": "⋃", "bigcap": "⋂",
	// binary operators and relations
	"pm": "±", "mp": "∓", "times": "×", "div": "÷", "cdot": "⋅", "ast": "∗", "star": "⋆",
	"circ": "∘", "bullet": "•", "oplus": "⊕", "otimes": "⊗", "cup": "∪", "cap": "∩",
	"setminus": "∖", "wedge": "∧", "land": "∧", "vee": "∨", "lor": "∨",
	"le": "≤", "leq": "≤", "ge": "≥", "geq": "≥", "ne": "≠", "neq": "≠", "approx": "≈",
	"sim": "∼", "simeq": "≃", "cong": "≅", "equiv": "≡", "propto": "∝", "ll": "≪", "gg": "≫",
	"in": "∈", "notin": "∉", "ni": "∋", "subset": "⊂", "supset": "⊃", "subseteq": "⊆",
	"supseteq": "⊇", "perp": "⊥", "parallel": "∥", "mid": "∣", "vdash": "⊢", "models": "⊨",
	// arrows
	"to": "→", "rightarrow": "→", "leftarrow": "←", "gets": "←", "leftrightarrow": "↔",
	"Rightarrow": "⇒", "Leftarrow": "⇐", "Leftrightarrow": "⇔", "implies": "⟹", "iff": "⟺",
	"mapsto": "↦", "uparrow": "↑", "downarrow": "↓", "longrightarrow": "⟶",
	// logic, sets and other symbols
	"forall": "∀", "exists": "∃", "nexists": "∄", "neg": "¬", "lnot": "¬", "emptyset": "∅",
	"varnothing": "∅", "infty": "∞", "partial": "∂", "nabla": "∇", "angle": "∠", "prime": "′",
	"hbar": "ℏ", "ell": "ℓ", "Re": "ℜ", "Im": "ℑ", "aleph": "ℵ", "therefore": "∴",
	"because": "∵", "top": "⊤", "bot": "⊥", "dagger": "†", "degree": "°",
	"cdots": "⋯", "ldots": "…", "dots": "…", "vdots": "⋮", "ddots": "⋱",
	// delimiters and escapes
	"langle": "⟨", "rangle": "⟩", "lceil": "⌈", "rceil": "⌉", "lfloor": "⌊", "rfloor": "⌋",
	"vert": "|", "|": "‖", "Vert": "‖", "{": "{", "}": "}", "lbrace": "{", "rbrace": "}",
	"%": "%", "&": "&", "#": "#", "_": "_", "$": "$", "colon": ":", "backslash": `\`,
	// function names
	"sin": "sin", "cos": "cos", "tan": "tan", "cot": "cot", "sec": "sec", "csc": "csc",
	"arcsin": "arcsin", "arccos": "arccos", "arctan": "arctan", "sinh": "sinh", "cosh": "cosh",
	"tanh": "tanh", "log": "log", "ln": "ln", "exp": "exp", "lim": "lim", "max": "max",
	"min": "min", "sup": "sup", "inf": "inf", "det": "det", "arg": "arg", "deg": "deg",
	"gcd": "gcd", "Pr": "Pr", "dim": "dim", "ker": "ker",
}
