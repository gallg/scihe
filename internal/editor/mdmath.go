package editor

import (
	"strings"
	"unicode"
)

// Helpers for the formula delimiters of markdown ($…$ and $$…$$) and
// the symbol tables of the formula parser (mdformula.go): the names
// are those of LibreOffice Math and LaTeX, written without backslash.

// hasAt reports whether s starts at rs[i] (called per character: no
// allocations).
func hasAt(rs []rune, i int, s string) bool {
	for _, c := range s {
		if i >= len(rs) || rs[i] != c {
			return false
		}
		i++
	}
	return true
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

// wrapTerm parenthesises a fraction or root operand of several symbols.
func wrapTerm(s string) string {
	core := strings.TrimRightFunc(s, func(r rune) bool { return fScriptChars[r][0] != 0 })
	if len([]rune(core)) <= 1 || strings.IndexFunc(core, func(r rune) bool { return !unicode.IsLetter(r) }) < 0 ||
		strings.IndexFunc(core, func(r rune) bool { return !unicode.IsDigit(r) && r != '.' }) < 0 {
		return s // a word or a number, its scripts after it (σ², 10³)
	}
	// already one bracket, scripts after it: (x − μ)²
	if rs := []rune(s); rs[0] == '(' {
		depth := 0
		for i, r := range rs {
			switch r {
			case '(':
				depth++
			case ')':
				depth--
			}
			if depth == 0 {
				if strings.IndexFunc(string(rs[i+1:]), func(r rune) bool { return fScriptChars[r][0] == 0 }) < 0 {
					return s
				}
				break
			}
		}
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

var mathSymbols = map[string]string{
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
	"vert": "|", "Vert": "‖", "lbrace": "{", "rbrace": "}", "colon": ":", "backslash": `\`,
	// function names
	"sin": "sin", "cos": "cos", "tan": "tan", "cot": "cot", "sec": "sec", "csc": "csc",
	"arcsin": "arcsin", "arccos": "arccos", "arctan": "arctan", "sinh": "sinh", "cosh": "cosh",
	"tanh": "tanh", "log": "log", "ln": "ln", "exp": "exp", "lim": "lim", "max": "max",
	"min": "min", "sup": "sup", "inf": "inf", "det": "det", "arg": "arg", "deg": "deg",
	"gcd": "gcd", "Pr": "Pr", "dim": "dim", "ker": "ker",
}
