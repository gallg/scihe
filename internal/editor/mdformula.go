package editor

import (
	"strings"
	"unicode"
)

// Formulas laid out in two dimensions, as TeX does with fewer means:
// fractions stacked over a rule, scripts raised and lowered a row,
// limits above and below big operators, roots with their bar,
// delimiters as tall as what they enclose, matrices and aligned rows.
// Inside scripts and fractions the layout is tighter, as TeX sets them
// smaller: nested scripts become Unicode characters and fractions in a
// script are written a/b, so a formula is no taller than it needs.
//
// The syntax is LaTeX's math mode:
//
//	\frac{a}{b}  \sqrt{x}  \sqrt[3]{x}  x^2  x_{ij}  \alpha  \sum_{k=1}^{n} k
//	\int_0^\infty f(x)\,dx  \lim_{x \to 0}  \left( \frac{a}{b} \right)  \binom{n}{k}
//	\mathbb{R}  \mathbf{x}  \hat{\beta}  \text{if }  \operatorname{tr}  \tag{1}
//	\begin{pmatrix} a & b \\ c & d \end{pmatrix}  \begin{cases} … \end{cases}  \begin{aligned} … \end{aligned}
//
// Braces group; a blank between two variables is kept (x dx); ~, \,
// and \quad are blanks; unknown commands are shown as written.

type fkind byte

const (
	fSym    fkind = iota
	fRow          // kids side by side
	fFrac         // kids[0] over kids[1]
	fScript       // kids[0] with sup and sub
	fBig          // a big operator (or lim, max…) with limits sub and sup
	fSqrt         // kids[0] under the root, sub the index
	fFence        // kids[0] between left and right
	fMatrix       // rows of cells
	fAccent       // text drawn above kids[0]
	fSpace        // gap blank columns
)

// fclass is the TeX class of a symbol, which decides the spacing.
type fclass byte

const (
	cOrd fclass = iota
	cVar        // a variable: italic
	cNum
	cBin
	cRel
	cPunct
	cOpen
	cClose
	cFunc
	cBig
	cText // "text": a blank on each side
)

type fnode struct {
	kind        fkind
	cls         fclass
	text        string
	st          mdStyle
	kids        []*fnode
	sup, sub    *fnode
	rows        [][]*fnode
	left, right string
	align       []byte // fMatrix: 'l', 'c' or 'r' per column (the last repeats)
	gap         int    // fMatrix: blanks between columns; fSpace: its width
	tight       bool   // fRow: a script or limit, set without spaces (k=1)
	sp          bool   // written after a blank, kept between terms (x dx)
	auto        bool   // fFence: plain brackets, grown only around what needs it
}

func fsym(s string, c fclass) *fnode { return &fnode{kind: fSym, cls: c, text: s} }
func frow(kids ...*fnode) *fnode     { return &fnode{kind: fRow, kids: kids} }

// ---------------------------------------------------------------- tokens

type ftok struct {
	k        byte // 'w' letter, 'n' number, 's' symbol, 'c' command (\name), '{', '}', '^', '_', 'R' row break (\\), 'C' column break (&), '\'' prime
	s        string
	pos, end int  // runes of the source
	sp       bool // after a blank
}

// fScriptChars maps Unicode super- and subscript characters back to
// the character they raise or lower (a formula may hold "x²").
var fScriptChars = func() map[rune][2]rune {
	m := map[rune][2]rune{}
	for r, s := range superscripts {
		if r != s {
			m[s] = [2]rune{'^', r}
		}
	}
	for r, s := range subscripts {
		if r != s {
			m[s] = [2]rune{'_', r}
		}
	}
	return m
}()

func isASCIILetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

func ftokenize(rs []rune) []ftok {
	var out []ftok
	add := func(k byte, s string, pos, end int) {
		out = append(out, ftok{k, s, pos, end, pos > 0 && pos <= len(rs) && unicode.IsSpace(rs[pos-1])})
	}
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '%': // a comment, to the end of the line
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
		case r == '\\' && i+1 < len(rs) && isASCIILetter(rs[i+1]):
			j := i + 1
			for j < len(rs) && isASCIILetter(rs[j]) {
				j++
			}
			add('c', string(rs[i+1:j]), i, j)
			i = j
		case r == '\\' && i+1 < len(rs) && rs[i+1] == '\\':
			add('R', "", i, i+2)
			i += 2
		case r == '\\' && i+1 < len(rs):
			switch c := rs[i+1]; {
			case strings.ContainsRune(",:>; !", c): // \, \; …: spaces
				add('c', string(c), i, i+2)
			case c == '|':
				add('s', "‖", i, i+2)
			default: // \{ \} \$ \% \& \#: the character itself
				add('s', string(c), i, i+2)
			}
			i += 2
		case r == '&':
			add('C', "", i, i+1)
			i++
		case r == '~':
			add('c', "~", i, i+1)
			i++
		case isASCIILetter(r): // each letter is a variable: ab is a times b
			add('w', string(r), i, i+1)
			i++
		case r >= '0' && r <= '9':
			j := i + 1
			for j < len(rs) && (rs[j] >= '0' && rs[j] <= '9' || rs[j] == '.' && j+1 < len(rs) && rs[j+1] >= '0' && rs[j+1] <= '9') {
				j++
			}
			add('n', string(rs[i:j]), i, j)
			i = j
		case strings.ContainsRune("{}^_'", r):
			add(byte(r), "", i, i+1)
			i++
		case hasAt(rs, i, ":="):
			add('s', ":=", i, i+2)
			i += 2
		default:
			if m, ok := fScriptChars[r]; ok { // x²³ reads x^{23}
				add(byte(m[0]), "", i, i)
				add('{', "", i, i)
				for ; i < len(rs) && fScriptChars[rs[i]][0] == m[0]; i++ {
					c := fScriptChars[rs[i]][1]
					k := byte('s')
					if c >= '0' && c <= '9' {
						k = 'n'
					} else if isASCIILetter(c) {
						k = 'w'
					}
					add(k, string(c), i, i+1)
				}
				add('}', "", i, i)
				continue
			}
			add('s', string(r), i, i+1)
			i++
		}
	}
	return out
}

// ---------------------------------------------------------------- parser

type fparser struct {
	toks   []ftok
	rs     []rune
	i, lim int // next token, end of the tokens in reach
	depth  int
}

// fMaxDepth bounds the nesting the parser follows (the text may be
// anything); deeper text is kept as written.
const fMaxDepth = 64

// fMaxScan bounds the search for a closing bracket.
const fMaxScan = 4096

// fMaxRest is how much of the text past fMaxDepth is shown.
const fMaxRest = 200

// parseFormula reads a formula.
func parseFormula(src string) *fnode {
	rs := []rune(src)
	p := &fparser{toks: ftokenize(rs), rs: rs}
	p.lim = len(p.toks)
	return p.body()
}

func (p *fparser) cmd(t ftok, name string) bool { return t.k == 'c' && t.s == name }

func (p *fparser) next(name string) bool { return p.i < p.lim && p.cmd(p.toks[p.i], name) }

// stops reports a token ending a row.
func (p *fparser) stops(t ftok) bool {
	switch t.k {
	case '}', 'C', 'R':
		return true
	case 'c':
		return t.s == "right" || t.s == "end"
	}
	return false
}

// body reads up to the end of the reach: rows and cells, with stray
// closers kept as symbols.
func (p *fparser) body() *fnode {
	n := p.gridNode(p.grid())
	if p.i >= p.lim {
		return n
	}
	out := frow(n)
	for p.i < p.lim {
		t := p.toks[p.i]
		p.i++
		switch {
		case t.k == '}':
			out.kids = append(out.kids, fsym("}", cClose))
		case p.cmd(t, "right"):
			p.delim()
		case p.cmd(t, "end"):
			p.rawArg()
		}
		out.kids = append(out.kids, p.gridNode(p.grid()))
	}
	return out
}

// grid reads cells separated by & in rows separated by \\.
func (p *fparser) grid() [][]*fnode {
	rows := [][]*fnode{nil}
	for {
		last := len(rows) - 1
		rows[last] = append(rows[last], p.row())
		if p.i >= p.lim {
			return rows
		}
		switch p.toks[p.i].k {
		case 'C':
			p.i++
		case 'R':
			p.i++
			rows = append(rows, nil)
		default:
			return rows
		}
	}
}

// gridNode is the single cell of a grid, or a matrix of its cells.
func (p *fparser) gridNode(rows [][]*fnode) *fnode {
	if len(rows) == 1 && len(rows[0]) == 1 {
		return rows[0][0]
	}
	return &fnode{kind: fMatrix, rows: rows, align: []byte{'c'}, gap: 2}
}

// row reads symbols up to a token that ends the row.
func (p *fparser) row() *fnode {
	n := frow()
	if p.depth >= fMaxDepth {
		n.kids = append(n.kids, p.rest())
		return n
	}
	p.depth++
	defer func() { p.depth-- }()
	for p.i < p.lim {
		t := p.toks[p.i]
		if p.stops(t) {
			break
		}
		if p.cmd(t, "over") { // TeX's {a \over b}: all before over all after
			p.i++
			num := frow(n.kids...)
			n.kids = []*fnode{{kind: fFrac, kids: []*fnode{num, p.row()}}}
			continue
		}
		n.kids = append(n.kids, p.term())
	}
	return n
}

// rest keeps the tokens left in reach as written.
func (p *fparser) rest() *fnode {
	if p.i >= p.lim {
		return frow()
	}
	s := p.rs[p.toks[p.i].pos:p.toks[p.lim-1].end]
	p.i = p.lim
	if len(s) > fMaxRest {
		s = append(s[:fMaxRest:fMaxRest], '…')
	}
	return fsym(string(s), cOrd)
}

// term reads a symbol with its scripts (and an operator's limits).
func (p *fparser) term() *fnode {
	base := p.primary()
	var sup, sub *fnode
	nests := 0 // x^a^b… nests: bounded like groups
loop:
	for p.i < p.lim {
		t := p.toks[p.i]
		if nests+p.depth >= fMaxDepth {
			break
		}
		switch {
		case t.k == '^':
			p.i++
			if sup != nil {
				base, sup, sub = p.scripted(base, sup, sub), nil, nil
				nests++
			}
			sup = p.arg()
		case t.k == '_':
			p.i++
			if sub != nil {
				base, sup, sub = p.scripted(base, sup, sub), nil, nil
				nests++
			}
			sub = p.arg()
		case t.k == '\'':
			p.i++
			base = frow(p.scripted(base, sup, sub), fsym("′", cOrd))
			sup, sub = nil, nil
			nests++
		case p.cmd(t, "limits") || p.cmd(t, "nolimits"):
			p.i++
		default:
			break loop
		}
	}
	return p.scripted(base, sup, sub)
}

func (p *fparser) scripted(base, sup, sub *fnode) *fnode {
	for _, s := range []*fnode{sup, sub} {
		if s != nil && s.kind == fRow {
			s.tight = true
		}
	}
	switch {
	case sup == nil && sub == nil:
		return base
	case base.kind == fBig && base.sup == nil && base.sub == nil:
		base.sup, base.sub = sup, sub
		return base
	}
	return &fnode{kind: fScript, kids: []*fnode{base}, sup: sup, sub: sub, sp: base.sp}
}

// arg reads the argument of a command or a script: a group, a command
// or one symbol (one digit: \frac12 and x^12 take the 1 alone).
func (p *fparser) arg() *fnode {
	if p.i >= p.lim {
		return frow()
	}
	t := p.toks[p.i]
	if p.stops(t) || t.k == '^' || t.k == '_' {
		return frow()
	}
	if t.k == 'n' && len(t.s) > 1 {
		p.toks[p.i].s, p.toks[p.i].pos, p.toks[p.i].sp = t.s[1:], t.pos+1, false
		return fsym(t.s[:1], cNum)
	}
	return p.primary()
}

func fMinus(s string) string {
	if s == "-" {
		return "−"
	}
	return s
}

func (p *fparser) primary() *fnode {
	if p.i >= p.lim {
		return frow()
	}
	if p.depth >= fMaxDepth {
		return p.rest()
	}
	t := p.toks[p.i]
	if p.stops(t) || t.k == '^' || t.k == '_' {
		return frow() // a script without base: the caller reads it
	}
	p.depth++
	n := p.primaryOf(t)
	p.depth--
	n.sp = t.sp
	return n
}

func (p *fparser) primaryOf(t ftok) *fnode {
	p.i++
	switch t.k {
	case '{':
		n := p.gridNode(p.grid())
		if p.i < p.lim && p.toks[p.i].k == '}' {
			p.i++
		}
		if n.kind == fRow && len(n.kids) == 1 {
			return n.kids[0] // {\left(x\right)}: the fence itself
		}
		return n
	case 'n':
		return fsym(t.s, cNum)
	case 'w':
		return fsym(t.s, cVar)
	case 's':
		return p.symbol(t.s)
	case 'c':
		return p.command(t.s)
	case '\'':
		return fsym("′", cOrd)
	}
	return fsym(t.s, cOrd)
}

// symbol reads a character; a bracket with its closing one in the same
// group encloses what is between them, as tall as it is.
func (p *fparser) symbol(s string) *fnode {
	if s == "(" || s == "[" {
		cl := map[string]string{"(": ")", "[": "]"}[s]
		if j := p.closing(s, cl); j >= 0 {
			body := p.within(j)
			p.i = j + 1
			return &fnode{kind: fFence, left: s, right: cl, kids: []*fnode{body}, auto: true}
		}
	}
	if op, ok := fBigSyms[s]; ok {
		return &fnode{kind: fBig, cls: cBig, text: op}
	}
	s = fMinus(s)
	return fsym(s, symClass(s))
}

// within reads the tokens up to j (excluded).
func (p *fparser) within(j int) *fnode {
	lim := p.lim
	p.lim = j
	n := p.body()
	p.lim = lim
	return n
}

// closing returns the token closing the bracket open just before p.i,
// -1 when the group ends first.
func (p *fparser) closing(open, cl string) int {
	depth, braces := 0, 0
	for j := p.i; j < p.lim && j < p.i+fMaxScan; j++ {
		t := p.toks[j]
		switch {
		case t.k == '{':
			braces++
		case t.k == '}':
			if braces--; braces < 0 {
				return -1
			}
		case braces > 0:
		case t.k == 's' && t.s == open:
			depth++
		case t.k == 's' && t.s == cl:
			if depth == 0 {
				return j
			}
			depth--
		case p.stops(t) || p.cmd(t, "left"):
			return -1
		}
	}
	return -1
}

// delim reads the delimiter after \left, \right, \middle or \big: a
// character or a command (\langle, \{); "." is none.
func (p *fparser) delim() string {
	if p.i >= p.lim {
		return ""
	}
	t := p.toks[p.i]
	p.i++
	switch t.k {
	case 's':
		if t.s == "." {
			return ""
		}
		return t.s
	case 'c':
		if s, ok := fAliases[t.s]; ok {
			return s
		}
		if s, ok := mathSymbols[t.s]; ok && s != t.s {
			return s
		}
	}
	p.i--
	return ""
}

// rawArg returns the source text of the next group ({…}) or token, as
// written: an environment's name, the text of \text{…}.
func (p *fparser) rawArg() string {
	if p.i >= p.lim {
		return ""
	}
	t := p.toks[p.i]
	if t.k != '{' {
		p.i++
		return string(p.rs[t.pos:t.end])
	}
	depth := 0
	for j := p.i; j < p.lim && j < p.i+fMaxScan; j++ {
		switch p.toks[j].k {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				p.i = j + 1
				return string(p.rs[t.end:p.toks[j].pos])
			}
		}
	}
	p.i++ // never closed: the brace alone
	return ""
}

// skipStar skips the * of a starred command (\operatorname*).
func (p *fparser) skipStar() {
	if p.i < p.lim && p.toks[p.i].k == 's' && p.toks[p.i].s == "*" {
		p.i++
	}
}

var fBigOps = map[string]string{
	"sum": "∑", "prod": "∏", "coprod": "∐", "int": "∫", "iint": "∬", "iiint": "∭", "oint": "∮",
	"bigcup": "⋃", "bigcap": "⋂", "bigoplus": "⨁", "bigotimes": "⨂", "bigodot": "⨀",
	"biguplus": "⨄", "bigsqcup": "⨆", "bigvee": "⋁", "bigwedge": "⋀",
}

var fBigSyms = map[string]string{"∑": "∑", "∏": "∏", "∐": "∐", "∫": "∫", "∬": "∬", "∭": "∭", "∮": "∮", "⋃": "⋃", "⋂": "⋂"}

// fLimFuncs are function names written with limits below (lim, max…).
var fLimFuncs = map[string]string{
	"lim": "lim", "liminf": "lim inf", "limsup": "lim sup", "max": "max", "min": "min",
	"sup": "sup", "inf": "inf", "det": "det", "gcd": "gcd", "Pr": "Pr",
}

// fAliases are the LaTeX names of symbols not in mathSymbols.
var fAliases = map[string]string{
	"to": "→", "odot": "⊙", "ominus": "⊖", "oslash": "⊘", "wp": "℘", "lvert": "|", "rvert": "|",
	"lVert": "‖", "rVert": "‖", "lbrack": "[", "rbrack": "]", "leqslant": "⩽", "geqslant": "⩾",
	"nleq": "≰", "ngeq": "≱", "nless": "≮", "ngtr": "≯", "nsim": "≁", "ncong": "≇", "nequiv": "≢",
	"nmid": "∤", "nparallel": "∦", "nsubseteq": "⊈", "nsupseteq": "⊉", "subsetneq": "⊊",
	"supsetneq": "⊋", "sqsubseteq": "⊑", "sqsupseteq": "⊒", "sqcup": "⊔", "sqcap": "⊓",
	"uplus": "⊎", "amalg": "⨿", "lesssim": "≲", "gtrsim": "≳", "prec": "≺", "succ": "≻",
	"preceq": "⪯", "succeq": "⪰", "asymp": "≍", "doteq": "≐", "triangleq": "≜", "coloneqq": "≔",
	"approxeq": "≊", "dotsc": "…", "dotsb": "⋯", "dotsm": "⋯", "dotsi": "⋯", "cdotp": "·",
	"longleftarrow": "⟵", "longleftrightarrow": "⟷", "Longrightarrow": "⟹", "Longleftarrow": "⟸",
	"Longleftrightarrow": "⟺", "longmapsto": "⟼", "hookrightarrow": "↪", "hookleftarrow": "↩",
	"rightleftharpoons": "⇌", "leadsto": "⇝", "Uparrow": "⇑", "Downarrow": "⇓", "updownarrow": "↕",
	"nearrow": "↗", "searrow": "↘", "swarrow": "↙", "nwarrow": "↖", "ddagger": "‡",
	"complement": "∁", "mho": "℧", "hslash": "ℏ", "beth": "ℶ", "gimel": "ℷ", "imath": "ı",
	"jmath": "ȷ", "varkappa": "ϰ", "triangle": "△", "square": "□", "Box": "□", "diamond": "⋄",
	"lozenge": "◊", "bigstar": "★", "blacksquare": "■", "checkmark": "✓",
}

// fNegated are the symbols \not turns into their negation.
var fNegated = map[string]string{
	"=": "≠", "∈": "∉", "⊂": "⊄", "⊃": "⊅", "⊆": "⊈", "⊇": "⊉", "≡": "≢", "∼": "≁", "≅": "≇",
	"<": "≮", ">": "≯", "≤": "≰", "≥": "≱", "∣": "∤", "∥": "∦", "∃": "∄",
}

var fAccents = map[string]string{
	"hat": "^", "widehat": "^", "bar": "_", "overline": "_", "vec": "→", "overrightarrow": "→",
	"dot": "˙", "ddot": "¨", "tilde": "~", "widetilde": "~", "acute": "´", "grave": "`",
	"check": "ˇ", "breve": "˘", "mathring": "˚",
}

// fLinearAccents are the combining marks of the one-line fallback.
var fLinearAccents = map[string]rune{
	"^": '̂', "_": '̄', "→": '⃗', "˙": '̇', "¨": '̈', "~": '̃', "´": '́', "`": '̀', "ˇ": '̌', "˘": '̆', "˚": '̊',
}

// fSpaces are the widths of the spacing commands.
var fSpaces = map[string]int{",": 1, ":": 1, ">": 1, ";": 1, " ": 1, "~": 1, "enspace": 1, "thinspace": 1, "quad": 2, "qquad": 4}

// fIgnored are commands that change nothing here (sizes, numbering).
var fIgnored = map[string]bool{
	"displaystyle": true, "textstyle": true, "scriptstyle": true, "scriptscriptstyle": true,
	"nonumber": true, "notag": true, "limits": true, "nolimits": true, "!": true,
	"strut": true, "nobreak": true, "allowbreak": true,
}

// command reads \name: a construct, the name of a symbol, or an
// unknown command (shown as written).
func (p *fparser) command(name string) *fnode {
	if fIgnored[name] {
		return frow()
	}
	if w, ok := fSpaces[name]; ok {
		return &fnode{kind: fSpace, gap: w}
	}
	switch name {
	case "frac", "dfrac", "tfrac", "cfrac":
		a := p.arg()
		return &fnode{kind: fFrac, kids: []*fnode{a, p.arg()}}
	case "binom", "dbinom", "tbinom":
		a := p.arg()
		m := &fnode{kind: fMatrix, rows: [][]*fnode{{a}, {p.arg()}}, align: []byte{'c'}}
		return &fnode{kind: fFence, left: "(", right: ")", kids: []*fnode{m}}
	case "sqrt":
		var idx *fnode
		if p.i < p.lim && p.toks[p.i].k == 's' && p.toks[p.i].s == "[" { // \sqrt[3]{x}
			p.i++
			if j := p.closing("[", "]"); j >= 0 {
				idx = p.within(j)
				p.i = j + 1
			}
		}
		return &fnode{kind: fSqrt, kids: []*fnode{p.arg()}, sub: idx}
	case "left":
		l := p.delim()
		body := p.gridNode(p.grid())
		r := ""
		if p.next("right") {
			p.i++
			r = p.delim()
		}
		return &fnode{kind: fFence, left: l, right: r, kids: []*fnode{body}}
	case "right":
		p.delim()
		return frow()
	case "middle":
		return fsym(p.delim(), cRel)
	case "big", "Big", "bigg", "Bigg", "bigl", "Bigl", "biggl", "Biggl", "bigr", "Bigr", "biggr", "Biggr", "bigm", "Bigm":
		d := p.delim()
		return fsym(d, symClass(d))
	case "begin":
		return p.env()
	case "end":
		p.rawArg()
		return frow()
	case "over":
		return frow() // only between terms
	case "text", "textrm", "textnormal", "textup", "mbox", "textit", "textbf", "textsf", "texttt":
		n := fsym(strings.TrimSpace(p.rawArg()), cText)
		switch name {
		case "textit":
			n.st = msItalic
		case "textbf":
			n.st = msBold
		}
		return n
	case "operatorname":
		p.skipStar()
		return fsym(p.arg().linear(), cFunc)
	case "mathrm", "mathup", "mathsf", "mathtt", "mathfrak":
		n := p.arg()
		eachSym(n, func(s *fnode) {
			if s.cls == cVar {
				s.cls = cOrd
			}
		})
		return n
	case "mathit":
		n := p.arg()
		eachSym(n, func(s *fnode) { s.st |= msItalic })
		return n
	case "mathbf", "boldsymbol", "bm", "pmb":
		n := p.arg()
		eachSym(n, func(s *fnode) { s.st |= msBold })
		return n
	case "mathbb":
		n := p.arg()
		eachSym(n, func(s *fnode) { s.text, s.cls = alphabet(s.text, mathbb), cOrd })
		return n
	case "mathcal", "mathscr":
		n := p.arg()
		eachSym(n, func(s *fnode) { s.text, s.cls = alphabet(s.text, mathcal), cOrd })
		return n
	case "underline":
		n := p.arg()
		eachSym(n, func(s *fnode) { s.st |= msUnderline })
		return n
	case "boxed", "substack", "smash", "mathstrut":
		return p.arg()
	case "color", "label", "hspace", "vspace":
		p.rawArg()
		return frow()
	case "textcolor":
		p.rawArg()
		return p.arg()
	case "tag":
		p.skipStar()
		return frow(&fnode{kind: fSpace, gap: 4}, fsym("("+strings.TrimSpace(p.rawArg())+")", cOrd))
	case "not":
		n := p.primary()
		if s, ok := fNegated[n.text]; ok && n.kind == fSym {
			return fsym(s, cRel)
		}
		return frow(fsym("¬", cOrd), n)
	}
	if mark, ok := fAccents[name]; ok {
		return &fnode{kind: fAccent, text: mark, kids: []*fnode{p.arg()}}
	}
	if op, ok := fBigOps[name]; ok {
		return &fnode{kind: fBig, cls: cBig, text: op}
	}
	if f, ok := fLimFuncs[name]; ok {
		return &fnode{kind: fBig, cls: cFunc, text: f}
	}
	if s, ok := fAliases[name]; ok {
		return fsym(s, symClass(s))
	}
	if s, ok := mathSymbols[name]; ok {
		switch {
		case s == name:
			return fsym(s, cFunc) // \sin, \log…
		case unicode.IsLower([]rune(s)[0]) && unicode.Is(unicode.Greek, []rune(s)[0]):
			return fsym(s, cVar)
		}
		return fsym(s, symClass(s))
	}
	return fsym(`\`+name, cFunc) // unknown: as written
}

// fEnvFences are the brackets of the matrix environments.
var fEnvFences = map[string][2]string{
	"pmatrix": {"(", ")"}, "bmatrix": {"[", "]"}, "Bmatrix": {"{", "}"}, "vmatrix": {"|", "|"},
	"Vmatrix": {"‖", "‖"}, "cases": {"{", ""}, "dcases": {"{", ""}, "rcases": {"", "}"},
}

// env reads \begin{name} … \end{name}: matrices, cases, aligned rows,
// arrays and equations.
func (p *fparser) env() *fnode {
	name := strings.TrimSuffix(strings.TrimSpace(p.rawArg()), "*")
	align := []byte{'c'}
	switch name {
	case "array", "subarray":
		align = align[:0]
		for _, c := range p.rawArg() {
			if c == 'l' || c == 'c' || c == 'r' {
				align = append(align, byte(c))
			}
		}
		if len(align) == 0 {
			align = []byte{'c'}
		}
	case "alignat", "alignedat":
		p.rawArg() // the number of columns
		fallthrough
	case "align", "aligned", "split", "eqnarray", "flalign":
		align = []byte("rl")
	case "cases", "dcases", "rcases":
		align = []byte{'l'}
	}
	rows := p.grid()
	if p.next("end") {
		p.i++
		p.rawArg()
	}
	if last := rows[len(rows)-1]; len(rows) > 1 && len(last) == 1 && last[0].kind == fRow && len(last[0].kids) == 0 {
		rows = rows[:len(rows)-1] // a \\ ending the last row
	}
	switch name {
	case "equation", "displaymath", "math", "gather", "gathered", "multline":
		return p.gridNode(rows)
	}
	gap := 2
	if string(align) == "rl" {
		gap = 1 // a &= b: the relation follows the left side
	}
	m := &fnode{kind: fMatrix, rows: rows, align: align, gap: gap}
	if f, ok := fEnvFences[name]; ok {
		return &fnode{kind: fFence, left: f[0], right: f[1], kids: []*fnode{m}}
	}
	return m
}

func eachSym(n *fnode, f func(*fnode)) {
	if n == nil {
		return
	}
	if n.kind == fSym {
		f(n)
	}
	for _, k := range n.kids {
		eachSym(k, f)
	}
	eachSym(n.sup, f)
	eachSym(n.sub, f)
	for _, r := range n.rows {
		for _, c := range r {
			eachSym(c, f)
		}
	}
}

func symClass(s string) fclass {
	switch {
	case s == "":
		return cOrd
	case strings.Contains("=<>≤≥≠≈≡∼≃≅∝∈∉∋⊂⊃⊆⊇→←↔⇒⇐⇔⟹⟺↦≪≫∣⊥∥⊢⊨⩽⩾⊄⊈⊅⊉↑↓⟶∤≰≱≮≯≁≇≢∦⊊⊋⊑⊒≲≳≺≻⪯⪰≍≐≜≔≊⟵⟷⟸⟼↪↩⇌⇝⇑⇓↕↗↘↙↖", s) || s == ":" || s == ":=":
		return cRel
	case strings.Contains("+−-±∓×÷⋅∗⋆∘∪∩∖∧∨⊕⊗⊙⊖⊘⊔⊓⊎⨿⋄·*", s):
		return cBin
	case s == "," || s == ";":
		return cPunct
	case strings.Contains("([{⟨⌈⌊", s):
		return cOpen
	case strings.Contains(")]}⟩⌉⌋", s):
		return cClose
	}
	r := []rune(s)
	if len(r) == 1 && isASCIILetter(r[0]) {
		return cVar
	}
	return cOrd
}

// ---------------------------------------------------------------- layout

// Layout levels, as TeX's styles: a formula; the parts of a fraction,
// where simple scripts are Unicode characters on the line (2σ²); and
// scripts, written on one row (e^(−x²/2) is two rows, not five).
const (
	lvFormula = iota
	lvFraction
	lvScript
)

// formulaBox lays out a formula.
func formulaBox(n *fnode) box {
	return restyle(n.lay(lvFormula), msMath)
}

// scriptPart lays out a script or a limit: on one row.
func scriptPart(n *fnode) *box {
	if n == nil {
		return nil
	}
	b := n.lay(lvScript)
	if b.height() > 1 {
		b = textBox(n.linear(), 0)
	}
	return &b
}

// unicodeScripts writes the scripts of n as Unicode characters, false
// when one has no such form.
func unicodeScripts(n *fnode) (string, bool) {
	out := ""
	for _, part := range []struct {
		n   *fnode
		sup bool
	}{{n.sub, false}, {n.sup, true}} {
		if part.n == nil {
			continue
		}
		m := subscripts
		if part.sup {
			m = superscripts
		}
		for _, r := range strings.ReplaceAll(part.n.linear(), " ", "") {
			c, ok := m[r]
			if !ok {
				return "", false
			}
			out += string(c)
		}
	}
	return out, true
}

// class is the class of n for spacing.
func (n *fnode) class() fclass {
	switch n.kind {
	case fSym:
		return n.cls
	case fScript:
		return n.kids[0].class()
	case fBig:
		return n.cls
	case fAccent:
		return n.kids[0].class()
	}
	return cOrd
}

// tall reports a part that makes plain brackets grow: a fraction, a
// matrix, limits, grown brackets (not scripts: (x_i − μ)² stays on the
// line, as in TeX).
func (n *fnode) tall() bool {
	switch {
	case n == nil:
		return false
	case n.kind == fFrac || n.kind == fMatrix:
		return true
	case n.kind == fBig && (n.sup != nil || n.sub != nil):
		return true
	case n.kind == fFence && !n.auto:
		return true
	case n.kind == fFence:
		return n.kids[0].tall()
	}
	for _, k := range n.kids {
		if k.tall() {
			return true
		}
	}
	return false
}

// rowGaps returns the blanks before each kid of a row: around
// relations and binary operators, after commas, operators and
// function names (sin x, not sin(x)).
func rowGaps(kids []*fnode) []int {
	cls := make([]fclass, len(kids))
	for i, k := range kids {
		cls[i] = k.class()
		if k.kind == fRow && len(k.kids) == 0 {
			cls[i] = cOpen // empty: no spacing around it
		}
	}
	for i, c := range cls { // a sign, not an operation
		if c == cBin && (i == 0 || cls[i-1] == cBin || cls[i-1] == cRel || cls[i-1] == cOpen || cls[i-1] == cPunct || cls[i-1] == cBig) {
			cls[i] = cOrd
		}
	}
	gaps := make([]int, len(kids))
	for i := 1; i < len(kids); i++ {
		a, b := cls[i-1], cls[i]
		switch {
		case kids[i].kind == fSpace || kids[i-1].kind == fSpace:
		case b == cClose || b == cPunct || a == cOpen:
		case a == cRel || b == cRel || a == cBin || b == cBin || a == cPunct:
			gaps[i] = 1
		case a == cBig || a == cText || b == cText:
			gaps[i] = 1
		case a == cFunc && kids[i].kind != fFence && b != cOpen:
			gaps[i] = 1
		case b == cFunc && (a == cVar || a == cNum || a == cOrd || a == cClose):
			gaps[i] = 1 // 2 log x
		case kids[i].sp && (a == cVar || kids[i-1].kind == fFence) && b == cVar:
			gaps[i] = 1 // a blank written between variables: x dx, m v², f(x) dx
		}
	}
	return gaps
}

func isIntegral(n *fnode) bool {
	return n.kind == fBig && strings.ContainsAny(n.text, "∫∬∭∮")
}

func (n *fnode) lay(lv int) box {
	switch n.kind {
	case fSym:
		st := n.st
		if n.cls == cVar {
			st |= msItalic
		}
		return textBox(n.text, st)
	case fSpace:
		return blankBox(n.gap, 1)
	case fRow:
		return n.rowBox(lv)
	case fFrac, fSqrt, fMatrix, fAccent:
		if lv >= lvScript {
			return textBox(n.linear(), 0)
		}
	case fBig:
		if lv >= lvScript && (n.sup != nil || n.sub != nil) {
			return textBox(n.linear(), 0)
		}
	}
	switch n.kind {
	case fFrac:
		num, den := n.kids[0].lay(max(lv, lvFraction)), n.kids[1].lay(max(lv, lvFraction))
		w := max(num.width(), den.width()) + 2
		return vstack([]box{num, textBox(strings.Repeat("─", w), 0), den}, 1, 'c')
	case fScript:
		base := n.kids[0].lay(lv)
		if lv >= lvFraction && base.height() == 1 {
			if s, ok := unicodeScripts(n); ok {
				return hcat(base, textBox(s, 0))
			}
			if lv >= lvScript {
				return textBox(n.linear(), 0)
			}
		}
		return scriptBox(base, scriptPart(n.sup), scriptPart(n.sub), false)
	case fBig:
		return n.bigBox(1, 0)
	case fSqrt:
		return sqrtBox(n.kids[0].lay(lv), n.sub)
	case fFence:
		b := n.kids[0].lay(lv)
		h, base := b.height(), b.base
		if n.auto && !n.kids[0].tall() {
			h, base = 1, 0 // (x_i) and (σ²) keep their brackets on the line
		}
		l, r := delimBox(n.left, h, base), delimBox(n.right, h, base)
		return hcat(l, b, r)
	case fMatrix:
		return n.matrixBox(lv)
	case fAccent:
		b := n.kids[0].lay(lv)
		mark := n.text
		if mark == "_" {
			mark = strings.Repeat("_", max(b.width(), 1))
		} else if mark == "→" && b.width() > 1 {
			mark = strings.Repeat("─", b.width()-1) + "→"
		}
		return vstack([]box{textBox(mark, 0), b}, 1, 'c')
	}
	return blankBox(0, 1)
}

func (n *fnode) rowBox(lv int) box {
	gaps := rowGaps(n.kids)
	if n.tight {
		gaps = make([]int, len(n.kids))
	}
	bs := make([]box, len(n.kids))
	for i, k := range n.kids {
		bs[i] = k.lay(lv)
	}
	// an integral is as tall as what follows it, up to a relation
	for i, k := range n.kids {
		if !isIntegral(k) {
			continue
		}
		asc, desc := 0, 0
		for j := i + 1; j < len(n.kids) && n.kids[j].class() != cRel; j++ {
			asc, desc = max(asc, bs[j].base), max(desc, bs[j].height()-1-bs[j].base)
		}
		if asc+desc > 0 {
			bs[i] = k.bigBox(asc+desc+1, asc)
		}
	}
	var parts []box
	for i, b := range bs {
		if gaps[i] > 0 {
			parts = append(parts, blankBox(gaps[i], 1))
		}
		parts = append(parts, b)
	}
	return hcat(parts...)
}

// scriptBox raises sup and lowers sub beside base: a row above and
// below the baseline, level with the top and bottom of a taller base
// (hug: always level, as the limits of a tall integral).
func scriptBox(base box, sup, sub *box, hug bool) box {
	items := []placed{{base, 0, 0}}
	top, bottom := -base.base, base.height()-1-base.base
	if top == 0 && !(hug && base.height() > 1) { // nothing above the baseline: a row higher
		top--
	}
	if bottom == 0 && !(hug && base.height() > 1) {
		bottom++
	}
	if sup != nil {
		items = append(items, placed{*sup, base.width(), top - sup.height() + 1 + sup.base})
	}
	if sub != nil {
		items = append(items, placed{*sub, base.width(), bottom + sub.base})
	}
	return compose(items)
}

// bigBox draws a big operator h rows tall (an integral grows with its
// integrand), limits above and below, an integral's beside it.
func (n *fnode) bigBox(h, base int) box {
	op := textBox(n.text, 0)
	if h > 1 && isIntegral(n) {
		reps := len([]rune(n.text))
		rows := make([]box, h)
		for i := range rows {
			piece := "⎮"
			switch i {
			case 0:
				piece = "⌠"
			case h - 1:
				piece = "⌡"
			}
			rows[i] = textBox(strings.Repeat(piece, reps), 0)
		}
		op = vstack(rows, base, 'l')
	}
	if isIntegral(n) {
		return scriptBox(op, scriptPart(n.sup), scriptPart(n.sub), true)
	}
	bs, at := []box{}, 0
	if n.sup != nil {
		bs, at = append(bs, *scriptPart(n.sup)), 1
	}
	bs = append(bs, op)
	if n.sub != nil {
		bs = append(bs, *scriptPart(n.sub))
	}
	return vstack(bs, at, 'c')
}

// sqrtBox draws a root: a bar over the body and a slanted stroke as
// tall as it, the index raised before.
func sqrtBox(b box, index *fnode) box {
	h, w := b.height(), b.width()
	out := blankBox(h+1+w, h+1)
	for i := 0; i < w; i++ {
		out.rows[0][h+1+i] = mcell{"_", 0}
	}
	for j := 0; j < h; j++ {
		out.rows[1+j][h-j] = mcell{"╱", 0}
		copy(out.rows[1+j][h+1:], b.rows[j])
	}
	out.rows[h][0] = mcell{"╲", 0}
	out.base = 1 + b.base
	if index == nil {
		return out
	}
	idx := *scriptPart(index)
	// the index sits on the row above the foot of the stroke
	return compose([]placed{{idx, 0, h - 1 - out.base - (idx.height() - 1 - idx.base)}, {out, idx.width(), 0}})
}

// delimBox draws a delimiter h rows tall with its middle on row base.
func delimBox(d string, h, base int) box {
	if d == "" {
		return box{rows: make([][]mcell, h), base: base}
	}
	if h <= 1 {
		return textBox(d, 0)
	}
	type pieces struct{ top, mid, bot, centre string }
	ps := map[string]pieces{
		"(": {"⎛", "⎜", "⎝", ""}, ")": {"⎞", "⎟", "⎠", ""},
		"[": {"⎡", "⎢", "⎣", ""}, "]": {"⎤", "⎥", "⎦", ""},
		"{": {"⎧", "⎪", "⎩", "⎨"}, "}": {"⎫", "⎪", "⎭", "⎬"},
		"⌈": {"⎡", "⎢", "⎢", ""}, "⌉": {"⎤", "⎥", "⎥", ""},
		"⌊": {"⎢", "⎢", "⎣", ""}, "⌋": {"⎥", "⎥", "⎦", ""},
		"|": {"│", "│", "│", ""}, "‖": {"‖", "‖", "‖", ""},
		"⟨": {"╱", "╱", "╲", ""}, "⟩": {"╲", "╲", "╱", ""},
	}
	p, ok := ps[d]
	if !ok {
		p = pieces{d, d, d, ""}
	}
	rows := make([][]mcell, h)
	for i := range rows {
		s := p.mid
		switch {
		case i == 0:
			s = p.top
		case i == h-1:
			s = p.bot
		case p.centre != "" && i == h/2:
			s = p.centre
		}
		if d == "⟨" || d == "⟩" { // the two halves of an angle
			s = map[bool]string{true: p.top, false: p.bot}[i < (h+1)/2]
		}
		rows[i] = cellsOf(s, 0)
	}
	return box{rows: rows, base: base}
}

func (n *fnode) matrixBox(lv int) box {
	ncol := 0
	for _, r := range n.rows {
		ncol = max(ncol, len(r))
	}
	cells := make([][]box, len(n.rows))
	colW := make([]int, ncol)
	for i, r := range n.rows {
		cells[i] = make([]box, ncol)
		for j := range cells[i] {
			if j < len(r) {
				cells[i][j] = r[j].lay(lv)
			} else {
				cells[i][j] = blankBox(0, 1)
			}
			colW[j] = max(colW[j], cells[i][j].width())
		}
	}
	pairs := string(n.align) == "rl" // aligned rows: a &= b & c &= d
	rows := make([]box, len(n.rows))
	for i := range n.rows {
		var parts []box
		for j, c := range cells[i] {
			if j > 0 {
				gap := n.gap
				if pairs && j%2 == 0 {
					gap += 2 // between the pairs of aligned columns
				}
				parts = append(parts, blankBox(gap, 1))
			}
			a := n.align[min(j, len(n.align)-1)]
			if pairs {
				a = n.align[j%2]
			}
			parts = append(parts, padBox(c, colW[j], a))
		}
		rows[i] = hcat(parts...)
	}
	out := vstack(rows, 0, 'l')
	if len(rows) > 1 {
		out.base = (out.height() - 1) / 2
	}
	return out
}

// ---------------------------------------------------------------- linear

// linear writes n on one line (the fallback where a formula is wider
// than the window): scripts as Unicode characters, a/b, √x.
func (n *fnode) linear() string {
	switch n.kind {
	case fSym:
		return n.text
	case fSpace:
		return strings.Repeat(" ", n.gap)
	case fRow:
		var sb strings.Builder
		for i, g := range rowGaps(n.kids) {
			if !n.tight {
				sb.WriteString(strings.Repeat(" ", g))
			}
			sb.WriteString(n.kids[i].linear())
		}
		return sb.String()
	case fFrac:
		return wrapTerm(n.kids[0].linear()) + "/" + wrapTerm(n.kids[1].linear())
	case fScript, fBig:
		s := n.text
		if n.kind == fScript {
			s = n.kids[0].linear()
		}
		if n.sub != nil {
			s += script(n.sub.linear(), false)
		}
		if n.sup != nil {
			s += script(n.sup.linear(), true)
		}
		return s
	case fSqrt:
		root := "√"
		if n.sub != nil {
			root = script(n.sub.linear(), true) + root
		}
		return root + wrapTerm(n.kids[0].linear())
	case fFence:
		return n.left + n.kids[0].linear() + n.right
	case fMatrix:
		rows := make([]string, len(n.rows))
		for i, r := range n.rows {
			cells := make([]string, len(r))
			for j, c := range r {
				cells[j] = c.linear()
			}
			rows[i] = strings.Join(cells, ", ")
		}
		return strings.Join(rows, "; ")
	case fAccent:
		s := n.kids[0].linear()
		if m, ok := fLinearAccents[n.text]; ok && len([]rune(s)) == 1 {
			return s + string(m)
		}
		return s
	}
	return ""
}
