package editor

import (
	"image"
	"math"
	"strings"
	"unicode"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/vector"
)

// Formulas typeset as TeX does, with the math font (mathfont.go): the
// fnode tree of the parser (mdformula.go) is laid out in ems with the
// rules of TeX's appendix G, in the OpenType form of the MATH table:
// four styles (display, text, script, scriptscript) at their sizes,
// spacing between atoms by class, fractions, scripts, limits, radicals
// and delimiters that grow. The result is drawn into a bitmap shown by
// the terminal (termgfx.go).

// mclass is TeX's class of an atom, which decides the spacing.
type mclass byte

const (
	mOrd mclass = iota
	mOp
	mBin
	mRel
	mOpen
	mClose
	mPunct
	mInner
)

// mSpacing is TeX's table of spacing between atoms, in mu (1/18 em):
// negative only in display and text styles.
var mSpacing = [8][8]int8{
	mOrd:   {0, 3, -4, -5, 0, 0, 0, -3},
	mOp:    {3, 3, 0, -5, 0, 0, 0, -3},
	mBin:   {-4, -4, 0, 0, -4, 0, 0, -4},
	mRel:   {-5, -5, 0, 0, -5, 0, 0, -5},
	mOpen:  {0, 0, 0, 0, 0, 0, 0, 0},
	mClose: {0, 3, -4, -5, 0, 0, 0, -3},
	mPunct: {-3, -3, 0, -3, -3, -3, -3, -3},
	mInner: {-3, 3, -4, -5, -3, 0, -3, -3},
}

// mop draws a glyph (its origin at x, y) or a rule (its top left).
type mop struct {
	g    sfnt.GlyphIndex
	rule bool
	x, y float64 // ems, y down from the baseline
	s    float64 // glyph size in ems
	w, h float64 // rule size
}

// mbox is a typeset piece: width, height above and depth below the
// baseline, in ems of the formula.
type mbox struct {
	w, h, d float64
	ital    float64 // italic correction (scripts go after it)
	acc     float64 // where an accent is centred; < 0: the middle
	single  bool    // one character: scripts at fixed shifts
	l, r    mclass  // class at its left and right side
	space   bool    // explicit space: no spacing around it
	prime   bool    // primes: after the italic correction before them
	ops     []mop
}

// put draws c at x, y (its baseline y down) in b.
func (b *mbox) put(c mbox, x, y float64) {
	for _, o := range c.ops {
		o.x += x
		o.y += y
		b.ops = append(b.ops, o)
	}
	b.h = max(b.h, c.h-y)
	b.d = max(b.d, c.d+y)
	b.w = max(b.w, x+c.w)
}

// rule draws a rectangle from x, y (top left).
func (b *mbox) rule(x, y, w, h float64) {
	b.ops = append(b.ops, mop{rule: true, x: x, y: y, w: w, h: h})
	b.h = max(b.h, -y)
	b.d = max(b.d, y+h)
	b.w = max(b.w, x+w)
}

// mstyle is a TeX style: 0 display, 1 text, 2 script, 3 scriptscript.
type mstyle struct {
	lvl     int
	cramped bool
}

func (s mstyle) sup() mstyle { return mstyle{max(2, min(s.lvl+1, 3)), s.cramped} }
func (s mstyle) sub() mstyle { return mstyle{s.sup().lvl, true} }
func (s mstyle) num() mstyle { return mstyle{min(s.lvl+1, 3), s.cramped} }
func (s mstyle) den() mstyle { return mstyle{min(s.lvl+1, 3), true} }
func (s mstyle) cramp() mstyle {
	s.cramped = true
	return s
}

// mtyper typesets one formula.
type mtyper struct {
	f       *mathFont
	sizes   [4]float64 // the size of each style, ems of the formula
	missing bool       // a character the font lacks: not typeset
}

func newTyper(minSize float64) *mtyper {
	f := mfont
	t := &mtyper{f: f, sizes: [4]float64{1, 1, f.c(mcScriptPercent) / 100, f.c(mcScriptScriptPercent) / 100}}
	for i := range t.sizes {
		t.sizes[i] = max(t.sizes[i], min(minSize, 1))
	}
	return t
}

func (t *mtyper) size(s mstyle) float64 { return t.sizes[s.lvl] }

// c is a MATH constant at the size of s.
func (t *mtyper) c(off int, s mstyle) float64 { return t.f.c(off) * t.size(s) }

func mclassOf(c fclass) mclass {
	switch c {
	case cBin:
		return mBin
	case cRel:
		return mRel
	case cPunct:
		return mPunct
	case cOpen:
		return mOpen
	case cClose:
		return mClose
	case cFunc, cBig:
		return mOp
	}
	return mOrd
}

func (t *mtyper) lay(n *fnode, s mstyle) mbox {
	switch n.kind {
	case fSym:
		b := t.sym(n.text, n.cls, n.st, s)
		if n.st&msUnderline != 0 {
			z := t.size(s)
			b.rule(0, b.d+t.c(mcOverbarGap, s), b.w, max(t.c(mcOverbarRule, s), 0.04*z))
		}
		return b
	case fSpace:
		return t.space(n, s)
	case fRow:
		return t.row(n.kids, s, false)
	case fFrac:
		return t.frac(n, s)
	case fScript:
		base := t.lay(n.kids[0], s)
		if k := n.kids[0]; k.kind == fFence && k.auto {
			base.single = true // (x_i − μ)²: on the closing bracket, as TeX reads it
		}
		return t.scripts(base, n.sup, n.sub, s, false)
	case fBig:
		return t.big(n, s)
	case fSqrt:
		return t.sqrt(n, s)
	case fFence:
		return t.fence(n, s)
	case fMatrix:
		return t.matrix(n, s)
	case fAccent:
		return t.accent(n, s)
	}
	return mbox{}
}

// mathAlpha maps a character to the math alphabet of its style:
// variables italic, \mathbf bold.
func mathAlpha(r rune, italic, bold bool) rune {
	switch {
	case r >= 'a' && r <= 'z':
		switch {
		case bold && italic:
			return 0x1D482 + r - 'a'
		case bold:
			return 0x1D41A + r - 'a'
		case italic && r == 'h':
			return 'ℎ'
		case italic:
			return 0x1D44E + r - 'a'
		}
	case r >= 'A' && r <= 'Z':
		switch {
		case bold && italic:
			return 0x1D468 + r - 'A'
		case bold:
			return 0x1D400 + r - 'A'
		case italic:
			return 0x1D434 + r - 'A'
		}
	case r >= '0' && r <= '9' && bold:
		return 0x1D7CE + r - '0'
	case italic && r >= 'α' && r <= 'ω':
		return 0x1D6FC + r - 'α'
	case italic:
		if c, ok := map[rune]rune{'ϵ': 0x1D716, 'ϑ': 0x1D717, 'ϰ': 0x1D718, 'ϕ': 0x1D719, 'ϱ': 0x1D71A, 'ϖ': 0x1D71B}[r]; ok {
			return c
		}
	}
	return r
}

// glyphBox is one glyph at size z.
func (t *mtyper) glyphBox(g sfnt.GlyphIndex, z float64) mbox {
	m := t.f.metric(g)
	b := mbox{w: m.adv * z, h: m.h * z, d: m.d * z, ital: t.f.italic[g] * z, acc: -1, single: true}
	if a, ok := t.f.accent[g]; ok {
		b.acc = a * z
	}
	b.ops = []mop{{g: g, s: z}}
	return b
}

// sym sets the characters of a symbol side by side.
func (t *mtyper) sym(text string, cls fclass, st mdStyle, s mstyle) mbox {
	z := t.size(s)
	if cls != cText && text != "" && strings.Trim(text, "′") == "" { // primes: raised as superscripts
		b := t.sym(text, cText, st, s.sup())
		out := mbox{l: mOrd, r: mOrd, prime: true}
		out.put(b, 0, -t.c(mcSupShiftUp, s))
		out.w += t.c(mcSpaceAfterScript, s)
		return out
	}
	bold := st&msBold != 0
	italic := cls == cVar && !bold || st&msItalic != 0 // \mathbf{x} is upright
	out := mbox{acc: -1, l: mclassOf(cls), r: mclassOf(cls)}
	n := 0
	for _, r := range text {
		if cls != cText {
			r = mathAlpha(r, italic, bold)
			if r == '*' && cls == cBin {
				r = '∗'
			}
		} else {
			r = mathAlpha(r, st&msItalic != 0, bold)
		}
		g, ok := t.f.glyph(r)
		if !ok {
			if unicode.IsSpace(r) {
				out.w += 0.333 * z
				continue
			}
			t.missing = true
			continue
		}
		gb := t.glyphBox(g, z)
		x := out.w
		out.put(gb, x, 0)
		out.ital, out.acc = gb.ital, gb.acc
		n++
	}
	out.single = n == 1 && len([]rune(text)) == 1
	if !out.single {
		out.acc = -1
	}
	return out
}

// axisCentre moves b so that its middle is on the math axis.
func (t *mtyper) axisCentre(b mbox, s mstyle) mbox {
	shift := (b.h-b.d)/2 - t.c(mcAxisHeight, s)
	out := b
	out.ops = nil
	out.h, out.d = 0, 0
	out.put(b, 0, shift)
	out.w = b.w
	return out
}

// spaceWidths are the spacing commands in mu (18 to the em).
var spaceWidths = map[string]float64{
	",": 3, "thinspace": 3, ":": 4, ">": 4, ";": 5, " ": 6, "~": 6, "enspace": 9, "quad": 18, "qquad": 36, "": 36,
}

func (t *mtyper) space(n *fnode, s mstyle) mbox {
	mu, ok := spaceWidths[n.text]
	if !ok {
		mu = 6 * float64(n.gap)
	}
	return mbox{w: mu / 18 * t.size(s), space: true}
}

// row sets atoms side by side with TeX's spacing between them; lead
// is an empty atom before them (amsmath's {} after an alignment &).
func (t *mtyper) row(kids []*fnode, s mstyle, lead bool) mbox {
	type item struct {
		b mbox
		n *fnode
	}
	var items []item
	for _, k := range kids {
		if k.kind == fRow && len(k.kids) == 0 {
			continue
		}
		items = append(items, item{t.lay(k, s), k})
	}
	// a binary operator with nothing to operate on is ordinary
	prev := -1
	prevCls := mOrd
	if !lead {
		prevCls = mOpen
	}
	for i := range items {
		b := &items[i].b
		if b.space {
			continue
		}
		if b.l == mBin && (prevCls == mBin || prevCls == mOp || prevCls == mRel || prevCls == mOpen || prevCls == mPunct) {
			b.l, b.r = mOrd, mOrd
		}
		if prev >= 0 && (b.l == mRel || b.l == mClose || b.l == mPunct) && items[prev].b.r == mBin {
			items[prev].b.l, items[prev].b.r = mOrd, mOrd
		}
		prev, prevCls = i, b.r
	}
	if prev >= 0 && items[prev].b.r == mBin {
		items[prev].b.l, items[prev].b.r = mOrd, mOrd
	}
	z := t.size(s)
	out := mbox{acc: -1, l: mOrd, r: mOrd}
	x := 0.0
	prev = -1
	if lead {
		prevCls = mOrd
	}
	first := true
	for i, it := range items {
		if !it.b.space {
			var mu float64
			if !first || lead {
				a := prevCls
				sp := mSpacing[a][it.b.l]
				if sp > 0 || sp < 0 && s.lvl < 2 {
					mu = math.Abs(float64(sp))
				}
				if prev >= 0 {
					pn := items[prev].n
					switch {
					case it.n.cls == cText && it.n.kind == fSym || pn.cls == cText && pn.kind == fSym:
						mu = max(mu, 6) // a word space around text
					case it.n.sp && it.n.class() == cVar && (pn.class() == cVar || pn.kind == fFence):
						mu = max(mu, 3) // a blank written between variables: x dx
					}
				}
			}
			x += mu / 18 * z
			if it.b.prime && prev >= 0 {
				x += items[prev].b.ital
			}
			if first {
				out.l = it.b.l
				first = false
			}
			prev, prevCls = i, it.b.r
			out.r = it.b.r
		}
		out.put(it.b, x, 0)
		x += it.b.w
		out.ital = it.b.ital
	}
	out.w = x
	if len(items) == 1 {
		out.single, out.acc = items[0].b.single, items[0].b.acc
	}
	return out
}

// frac sets a fraction (TeX's rule 15).
func (t *mtyper) frac(n *fnode, s mstyle) mbox {
	if n.disp && s.lvl > 0 { // \dfrac
		s.lvl = 0
	}
	num, den := t.lay(n.kids[0], s.num()), t.lay(n.kids[1], s.den())
	a, th := t.c(mcAxisHeight, s), t.c(mcFracRule, s)
	var u, v, gu, gv float64
	if s.lvl == 0 {
		u, v = t.c(mcFracNumDispShiftUp, s), t.c(mcFracDenDispShiftDown, s)
		gu, gv = t.c(mcFracNumDispGapMin, s), t.c(mcFracDenDispGapMin, s)
	} else {
		u, v = t.c(mcFracNumShiftUp, s), t.c(mcFracDenShiftDown, s)
		gu, gv = t.c(mcFracNumGapMin, s), t.c(mcFracDenGapMin, s)
	}
	u = max(u, gu+a+th/2+num.d)
	v = max(v, gv+den.h-a+th/2)
	null := 0.12 * t.size(s)
	w := max(num.w, den.w)
	out := mbox{acc: -1, l: mInner, r: mInner}
	out.put(num, null+(w-num.w)/2, -u)
	out.put(den, null+(w-den.w)/2, v)
	out.rule(null, -(a + th/2), w, th)
	out.w = w + 2*null
	return out
}

// stack sets a over b without a rule (\binom).
func (t *mtyper) stack(top, bot *fnode, s mstyle) mbox {
	num, den := t.lay(top, s.num()), t.lay(bot, s.den())
	var u, v, gap float64
	if s.lvl == 0 {
		u, v, gap = t.c(mcStackTopDispShiftUp, s), t.c(mcStackBotDispShift, s), t.c(mcStackDispGapMin, s)
	} else {
		u, v, gap = t.c(mcStackTopShiftUp, s), t.c(mcStackBotShiftDown, s), t.c(mcStackGapMin, s)
	}
	if clear := (u - num.d) - (den.h - v); clear < gap {
		u += (gap - clear) / 2
		v += (gap - clear) / 2
	}
	w := max(num.w, den.w)
	out := mbox{acc: -1}
	out.put(num, (w-num.w)/2, -u)
	out.put(den, (w-den.w)/2, v)
	out.w = w
	return out
}

// scripts attaches sup and sub to base (TeX's rule 18); op: the base
// is a big operator, whose subscript tucks under its slant.
func (t *mtyper) scripts(base mbox, sup, sub *fnode, s mstyle, op bool) mbox {
	if sup == nil && sub == nil {
		return base
	}
	var sb, bb mbox
	u, v := 0.0, 0.0
	if sup != nil {
		sb = t.lay(sup, s.sup())
		if !base.single {
			u = base.h - t.c(mcSupBaselineDropMax, s.sup())
		}
	}
	if sub != nil {
		bb = t.lay(sub, s.sub())
		if !base.single {
			v = base.d + t.c(mcSubBaselineDropMin, s.sub())
		}
	}
	shiftUp := t.c(mcSupShiftUp, s)
	if s.cramped {
		shiftUp = t.c(mcSupShiftUpCramped, s)
	}
	switch {
	case sub == nil:
		u = max(u, shiftUp, sb.d+t.c(mcSupBottomMin, s))
	case sup == nil:
		v = max(v, t.c(mcSubShiftDown, s), bb.h-t.c(mcSubTopMax, s))
	default:
		u = max(u, shiftUp, sb.d+t.c(mcSupBottomMin, s))
		v = max(v, t.c(mcSubShiftDown, s))
		if gap := (u - sb.d) - (bb.h - v); gap < t.c(mcSubSupGapMin, s) {
			v += t.c(mcSubSupGapMin, s) - gap
			if psi := t.c(mcSupBottomMaxWithSub, s) - (u - sb.d); psi > 0 {
				u += psi
				v -= psi
			}
		}
	}
	supX, subX := base.w+base.ital, base.w
	if op {
		supX, subX = base.w, base.w-base.ital
	}
	out := base
	out.ops = append([]mop(nil), base.ops...)
	out.single, out.ital, out.acc = false, 0, -1
	w := base.w
	if sup != nil {
		out.put(sb, supX, -u)
		w = max(w, supX+sb.w)
	}
	if sub != nil {
		out.put(bb, subX, v)
		w = max(w, subX+bb.w)
	}
	out.w = w + t.c(mcSpaceAfterScript, s)
	return out
}

// big sets a big operator (∑, ∫, lim): larger in display style, its
// limits above and below it there, else as scripts.
func (t *mtyper) big(n *fnode, s mstyle) mbox {
	z := t.size(s)
	var op mbox
	if n.cls == cFunc {
		op = t.sym(n.text, cText, 0, s)
		op.single = false
	} else {
		r := []rune(n.text)[0]
		g, ok := t.f.glyph(r)
		if !ok {
			t.missing = true
			return mbox{}
		}
		if mc := t.f.vert[g]; mc != nil && s.lvl == 0 {
			for i, v := range mc.variants {
				if mc.sizes[i] >= t.f.c(mcDisplayOperatorMin) || i == len(mc.variants)-1 {
					g = v
					break
				}
			}
		}
		op = t.axisCentre(t.glyphBox(g, z), s)
		op.ital = t.f.italic[g] * z
		op.single = false // a boxed nucleus: its scripts drop from its top and bottom
	}
	op.l, op.r = mOp, mOp
	if s.lvl > 0 || isIntegral(n) {
		return t.scripts(op, n.sup, n.sub, s, true)
	}
	out := mbox{acc: -1, l: mOp, r: mOp}
	var sb, bb mbox
	w := op.w
	if n.sup != nil {
		sb = t.lay(n.sup, s.sup())
		w = max(w, sb.w)
	}
	if n.sub != nil {
		bb = t.lay(n.sub, s.sub())
		w = max(w, bb.w)
	}
	out.put(op, (w-op.w)/2, 0)
	if n.sup != nil {
		y := op.h + max(t.c(mcUpperLimitGapMin, s)+sb.d, t.c(mcUpperLimitRiseMin, s))
		out.put(sb, (w-sb.w)/2+op.ital/2, -y)
	}
	if n.sub != nil {
		y := op.d + max(t.c(mcLowerLimitGapMin, s)+bb.h, t.c(mcLowerLimitDropMin, s))
		out.put(bb, (w-bb.w)/2-op.ital/2, y)
	}
	out.w = w
	return out
}

// grow returns the glyph of r at least size ems tall (wide when horiz),
// a larger variant or an assembly of its pieces.
func (t *mtyper) grow(r rune, size, z float64, horiz bool) mbox {
	g, ok := t.f.glyph(r)
	if !ok {
		t.missing = true
		return mbox{}
	}
	cons := t.f.vert
	if horiz {
		cons = t.f.horiz
	}
	mc := cons[g]
	if mc == nil {
		return t.glyphBox(g, z)
	}
	extent := func(b mbox) float64 {
		if horiz {
			return b.w
		}
		return b.h + b.d
	}
	best := t.glyphBox(g, z)
	if extent(best) >= size {
		return best
	}
	for _, v := range mc.variants {
		best = t.glyphBox(v, z)
		if extent(best) >= size {
			return best
		}
	}
	if len(mc.parts) == 0 {
		return best
	}
	return t.assemble(mc.parts, size, z, horiz)
}

// assemble builds a glyph size ems long from parts (bottom to top, or
// left to right), repeating the extenders.
func (t *mtyper) assemble(parts []mpart, size, z float64, horiz bool) mbox {
	minOv := t.f.overlap * z
	var fixedLen, extLen float64
	nFixed, nExt := 0, 0
	for _, p := range parts {
		if p.ext {
			extLen += p.full * z
			nExt++
		} else {
			fixedLen += p.full * z
			nFixed++
		}
	}
	reps := 0
	for nExt > 0 && reps < 100 {
		n := nFixed + reps*nExt
		if fixedLen+float64(reps)*extLen-float64(n-1)*minOv >= size {
			break
		}
		reps++
	}
	var seq []mpart
	for _, p := range parts {
		k := 1
		if p.ext {
			k = reps
		}
		for i := 0; i < k; i++ {
			seq = append(seq, p)
		}
	}
	total := 0.0
	for _, p := range seq {
		total += p.full * z
	}
	ov := minOv
	if len(seq) > 1 {
		ov = max(minOv, (total-size)/float64(len(seq)-1))
		// no more than the connectors allow
		lim := math.Inf(1)
		for i := 1; i < len(seq); i++ {
			lim = min(lim, seq[i-1].end*z, seq[i].start*z)
		}
		if lim >= minOv {
			ov = min(ov, lim)
		}
	}
	out := mbox{acc: -1}
	pos := 0.0
	for _, p := range seq {
		gb := t.glyphBox(p.g, z)
		if horiz {
			out.put(gb, pos, 0)
		} else { // y up from the bottom: the part's bottom at pos
			out.put(gb, 0, -(pos + gb.d))
		}
		pos += p.full*z - ov
	}
	return out
}

// delim returns delimiter d at least size ems tall, centred on the axis.
func (t *mtyper) delim(d string, size float64, s mstyle) mbox {
	if d == "" {
		return mbox{w: 0.12 * t.size(s)}
	}
	r := []rune(d)[0]
	b := t.grow(r, size, t.size(s), false)
	return t.axisCentre(b, s)
}

// fence sets \left d … \right d: delimiters as tall as the body; plain
// brackets keep their size, as in TeX.
func (t *mtyper) fence(n *fnode, s mstyle) mbox {
	inner := n.kids[0]
	var body mbox
	if m := inner; !n.auto && n.left == "(" && n.right == ")" && m.kind == fMatrix && m.gap == 0 &&
		len(m.rows) == 2 && len(m.rows[0]) == 1 && len(m.rows[1]) == 1 { // \binom
		body = t.stack(m.rows[0][0], m.rows[1][0], s)
		body.h, body.d = max(body.h, 0), max(body.d, 0)
	} else {
		body = t.lay(inner, s)
	}
	z := t.size(s)
	var lb, rb mbox
	if n.auto {
		lb, rb = t.sym(n.left, cOpen, 0, s), t.sym(n.right, cClose, 0, s)
	} else {
		a := t.c(mcAxisHeight, s)
		delta := max(body.h-a, body.d+a)
		size := max(2*delta*0.901, 2*delta-0.5*z)
		lb, rb = t.delim(n.left, size, s), t.delim(n.right, size, s)
	}
	out := mbox{acc: -1, l: mInner, r: mInner}
	if n.auto {
		out.l, out.r = mOpen, mClose
	}
	out.put(lb, 0, 0)
	out.put(body, lb.w, 0)
	out.put(rb, lb.w+body.w, 0)
	out.w = lb.w + body.w + rb.w
	return out
}

// sqrt sets a radical (TeX's rule 11) with its index.
func (t *mtyper) sqrt(n *fnode, s mstyle) mbox {
	body := t.lay(n.kids[0], s.cramp())
	gap := t.c(mcRadicalGap, s)
	if s.lvl == 0 {
		gap = t.c(mcRadicalDispGap, s)
	}
	th := t.c(mcRadicalRule, s)
	need := body.h + body.d + gap + th
	rad := t.grow('√', need, t.size(s), false)
	if total := rad.h + rad.d; total > need {
		gap += (total - need) / 2
	}
	top := body.h + gap + th // the radical's top, above the baseline
	out := mbox{acc: -1}
	x := 0.0
	if n.sub != nil {
		deg := t.lay(n.sub, mstyle{3, false})
		total := rad.h + rad.d
		bottom := -top + total
		kb, ka := t.c(mcRadicalKernBefore, s), t.c(mcRadicalKernAfter, s)
		out.put(deg, kb, bottom-t.f.c(mcRadicalDegreeRaise)/100*total)
		x = max(kb+deg.w+ka, 0)
	}
	out.put(rad, x, -top+rad.h)
	rx := x + rad.w
	out.rule(rx, -top, body.w, th)
	out.put(body, rx, 0)
	out.h = max(out.h, top+t.c(mcRadicalExtra, s))
	out.w = rx + body.w
	return out
}

// accentMarks are the combining marks of the accents.
var accentMarks = map[string]rune{
	"^": 0x302, "~": 0x303, "_": 0x304, "˙": 0x307, "¨": 0x308, "´": 0x301, "`": 0x300,
	"ˇ": 0x30C, "˘": 0x306, "˚": 0x30A, "→": 0x20D7,
}

// accent sets a mark above its base: a wider one over a wide base.
func (t *mtyper) accent(n *fnode, s mstyle) mbox {
	base := t.lay(n.kids[0], s.cramp())
	out := mbox{acc: -1, l: mOrd, r: mOrd}
	if n.text == "_" && !base.single { // \overline
		gap, th := t.c(mcOverbarGap, s), t.c(mcOverbarRule, s)
		out.put(base, 0, 0)
		out.rule(0, -(base.h + gap + th), base.w, th)
		out.h += t.c(mcOverbarExtra, s)
		out.w = base.w
		return out
	}
	z := t.size(s)
	if n.text == "→" && !base.single { // \overrightarrow: a long arrow
		arrow := t.grow('→', base.w, z, true)
		out.put(base, 0, 0)
		out.put(arrow, (base.w-arrow.w)/2, -(base.h + t.c(mcOverbarGap, s) + arrow.d))
		out.w = max(base.w, arrow.w)
		return out
	}
	g, ok := t.f.glyph(accentMarks[n.text])
	if !ok {
		t.missing = true
		return base
	}
	mark := t.glyphBox(g, z)
	if mc := t.f.horiz[g]; mc != nil && !base.single {
		for _, v := range mc.variants { // the widest not wider than the base
			vb := t.glyphBox(v, z)
			if vb.x1(t) > base.w*1.05 {
				break
			}
			mark = vb
		}
	}
	at := base.w / 2
	if base.acc >= 0 {
		at = base.acc
	}
	markAt := (t.f.metric(mark.ops[0].g).x0 + t.f.metric(mark.ops[0].g).x1) / 2 * z
	if a, ok := t.f.accent[mark.ops[0].g]; ok {
		markAt = a * z
	}
	shift := base.h - min(base.h, t.c(mcAccentBaseHeight, s))
	out.put(base, 0, 0)
	out.put(mark, at-markAt, -shift)
	out.w, out.ital, out.single = base.w, base.ital, false
	out.l, out.r = base.l, base.r
	return out
}

// x1 is the width of the ink of a one-glyph box, in ems.
func (b mbox) x1(t *mtyper) float64 {
	if len(b.ops) != 1 {
		return b.w
	}
	o := b.ops[0]
	m := t.f.metric(o.g)
	return (m.x1 - m.x0) * o.s
}

// matrix sets rows of cells: matrices, cases and arrays in text style,
// aligned rows in display style, with LaTeX's struts and column gaps.
func (t *mtyper) matrix(n *fnode, s mstyle) mbox {
	pairs := string(n.align) == "rl"
	cs := mstyle{1, s.cramped}
	if pairs || n.disp && s.lvl == 0 {
		cs = mstyle{0, s.cramped}
	}
	if s.lvl >= 2 {
		cs = s
	}
	z := t.size(s)
	ncol := 0
	for _, r := range n.rows {
		ncol = max(ncol, len(r))
	}
	cells := make([][]mbox, len(n.rows))
	colW := make([]float64, ncol)
	for i, r := range n.rows {
		cells[i] = make([]mbox, ncol)
		for j := range cells[i] {
			if j < len(r) {
				if pairs && j%2 == 1 && r[j].kind == fRow {
					cells[i][j] = t.row(r[j].kids, cs, true)
				} else {
					cells[i][j] = t.lay(r[j], cs)
				}
			}
			colW[j] = max(colW[j], cells[i][j].w)
		}
	}
	gap := 1.0 * z
	strutH, strutD := 0.84*z, 0.36*z
	jot := 0.0
	if pairs {
		jot = 0.3 * z
	}
	out := mbox{acc: -1}
	y := 0.0
	for i := range cells {
		h, d := strutH, strutD
		for _, c := range cells[i] {
			h, d = max(h, c.h), max(d, c.d)
		}
		if i > 0 {
			y += jot
		}
		y += h
		x := 0.0
		for j, c := range cells[i] {
			if j > 0 {
				switch {
				case pairs && j%2 == 1:
				case pairs:
					x += 2 * z
				default:
					x += gap
				}
			}
			a := n.align[min(j, len(n.align)-1)]
			if pairs {
				a = n.align[j%2]
			}
			off := 0.0
			switch a {
			case 'c':
				off = (colW[j] - c.w) / 2
			case 'r':
				off = colW[j] - c.w
			}
			out.put(c, x+off, y)
			x += colW[j]
		}
		out.w = max(out.w, x)
		y += d
	}
	// centred on the axis
	shift := y/2 - t.c(mcAxisHeight, s)
	res := mbox{acc: -1}
	res.put(out, 0, -shift)
	res.h, res.d = max(res.h, shift), max(res.d, y-shift)
	res.w = out.w
	return res
}

// ---------------------------------------------------------------- bitmap

// typeset lays out a formula, false when the font lacks a character.
func typeset(n *fnode, display bool, minSize float64) (mbox, bool) {
	if !mfont.load() {
		return mbox{}, false
	}
	t := newTyper(minSize)
	s := mstyle{1, false}
	if display {
		s.lvl = 0
	}
	b := t.lay(n, s)
	return b, !t.missing
}

// raster draws b with px pixels to the em, its baseline at (ox, oy),
// into a w×h mask.
func raster(b mbox, px float64, w, h int, ox, oy float64) *image.Alpha {
	z := vector.NewRasterizer(w, h)
	for _, o := range b.ops {
		x, y := ox+o.x*px, oy+o.y*px
		if o.rule {
			// on whole pixels, at least one thick: a crisp line
			x0, y0 := float32(x), float32(math.Round(y))
			x1, y1 := float32(x+o.w*px), y0+float32(max(math.Round(o.h*px), 1))
			z.MoveTo(x0, y0)
			z.LineTo(x1, y0)
			z.LineTo(x1, y1)
			z.LineTo(x0, y1)
			z.ClosePath()
			continue
		}
		segs := mfont.outline(o.g, o.s*px)
		started := false
		for _, sg := range segs {
			p := func(i int) (float32, float32) {
				return float32(x) + float32(sg.Args[i].X)/64, float32(y) + float32(sg.Args[i].Y)/64
			}
			switch sg.Op {
			case sfnt.SegmentOpMoveTo:
				if started {
					z.ClosePath()
				}
				z.MoveTo(p(0))
				started = true
			case sfnt.SegmentOpLineTo:
				z.LineTo(p(0))
			case sfnt.SegmentOpQuadTo:
				ax, ay := p(0)
				bx, by := p(1)
				z.QuadTo(ax, ay, bx, by)
			case sfnt.SegmentOpCubeTo:
				ax, ay := p(0)
				bx, by := p(1)
				cx, cy := p(2)
				z.CubeTo(ax, ay, bx, by, cx, cy)
			}
		}
		if started {
			z.ClosePath()
		}
	}
	dst := image.NewAlpha(image.Rect(0, 0, w, h))
	z.Draw(dst, dst.Bounds(), image.Opaque, image.Point{})
	return dst
}
