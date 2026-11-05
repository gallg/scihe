package editor

import (
	_ "embed"
	"encoding/binary"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// The math font of typeset formulas (mathtype.go): Scihe Math, Latin
// Modern Math (the OpenType version of TeX's Computer Modern) renamed,
// under the GUST Font License (fonts/README-Scihe-Math.md). Its MATH
// table holds the constants TeX's layout rules use (script shifts,
// fraction gaps, rule thicknesses), and the larger variants and the
// pieces of the delimiters, radicals and operators that grow.

//go:embed fonts/scihe-math.otf
var mathFontData []byte

// mglyph is a glyph's metrics in ems.
type mglyph struct {
	adv, h, d float64 // advance, ink height above and depth below the baseline
	x0, x1    float64 // ink left and right
}

// mpart is a piece of a glyph assembly (OpenType GlyphPartRecord).
type mpart struct {
	g          sfnt.GlyphIndex
	start, end float64 // connector lengths
	full       float64 // advance
	ext        bool    // an extender: repeated as needed
}

// mconstruction is how a glyph grows: larger variants, then an
// assembly of pieces.
type mconstruction struct {
	variants []sfnt.GlyphIndex
	sizes    []float64
	parts    []mpart
}

type mathFont struct {
	mu       sync.Mutex
	f        *sfnt.Font
	buf      sfnt.Buffer
	upem     float64
	consts   []byte // MathConstants
	italic   map[sfnt.GlyphIndex]float64
	accent   map[sfnt.GlyphIndex]float64 // top accent attachment
	vert     map[sfnt.GlyphIndex]*mconstruction
	horiz    map[sfnt.GlyphIndex]*mconstruction
	overlap  float64 // minimum connector overlap
	metrics  map[sfnt.GlyphIndex]mglyph
	glyphOf  map[rune]sfnt.GlyphIndex
	loadOnce sync.Once
	err      error
}

var mfont = &mathFont{}

// load parses the font once; false when it can't be read.
func (m *mathFont) load() bool {
	m.loadOnce.Do(func() {
		m.f, m.err = sfnt.Parse(mathFontData)
		if m.err != nil {
			return
		}
		m.upem = float64(m.f.UnitsPerEm())
		m.metrics = map[sfnt.GlyphIndex]mglyph{}
		m.glyphOf = map[rune]sfnt.GlyphIndex{}
		m.italic = map[sfnt.GlyphIndex]float64{}
		m.accent = map[sfnt.GlyphIndex]float64{}
		m.vert = map[sfnt.GlyphIndex]*mconstruction{}
		m.horiz = map[sfnt.GlyphIndex]*mconstruction{}
		if t := otfTable(mathFontData, "MATH"); t != nil {
			m.parseMath(t)
		}
		if m.consts == nil {
			m.err = errNoMath
		}
	})
	return m.err == nil
}

type mathFontError string

func (e mathFontError) Error() string { return string(e) }

const errNoMath = mathFontError("math font: no MATH table")

// otfTable returns a table of an OpenType file, nil when missing.
func otfTable(data []byte, tag string) []byte {
	if len(data) < 12 {
		return nil
	}
	n := int(binary.BigEndian.Uint16(data[4:]))
	for i := 0; i < n; i++ {
		rec := 12 + 16*i
		if rec+16 > len(data) {
			return nil
		}
		if string(data[rec:rec+4]) == tag {
			off, ln := int(binary.BigEndian.Uint32(data[rec+8:])), int(binary.BigEndian.Uint32(data[rec+12:]))
			if off+ln > len(data) {
				return nil
			}
			return data[off : off+ln]
		}
	}
	return nil
}

// u16 reads a big-endian uint16 at i of t (0 out of bounds).
func u16(t []byte, i int) int {
	if i < 0 || i+2 > len(t) {
		return 0
	}
	return int(binary.BigEndian.Uint16(t[i:]))
}

func i16(t []byte, i int) int { return int(int16(u16(t, i))) }

// coverage reads an OpenType coverage table: glyph -> coverage index.
func coverage(t []byte, at int) map[sfnt.GlyphIndex]int {
	out := map[sfnt.GlyphIndex]int{}
	switch u16(t, at) {
	case 1:
		n := u16(t, at+2)
		for i := 0; i < n; i++ {
			out[sfnt.GlyphIndex(u16(t, at+4+2*i))] = i
		}
	case 2:
		n := u16(t, at+2)
		for i := 0; i < n; i++ {
			r := at + 4 + 6*i
			start, end, idx := u16(t, r), u16(t, r+2), u16(t, r+4)
			for g := start; g <= end; g++ {
				out[sfnt.GlyphIndex(g)] = idx + g - start
			}
		}
	}
	return out
}

func (m *mathFont) parseMath(t []byte) {
	if c := u16(t, 4); c > 0 && c+214 <= len(t) {
		m.consts = t[c : c+214]
	}
	// MathGlyphInfo: italic corrections and top accent attachments
	if gi := u16(t, 6); gi > 0 {
		values := func(at int, into map[sfnt.GlyphIndex]float64) {
			if at == gi {
				return
			}
			for g, i := range coverage(t, at+u16(t, at)) {
				into[g] = float64(i16(t, at+4+4*i)) / m.upem
			}
		}
		values(gi+u16(t, gi), m.italic)
		values(gi+u16(t, gi+2), m.accent)
	}
	// MathVariants
	mv := u16(t, 8)
	if mv == 0 {
		return
	}
	m.overlap = float64(u16(t, mv)) / m.upem
	vcov, hcov := coverage(t, mv+u16(t, mv+2)), coverage(t, mv+u16(t, mv+4))
	nv := u16(t, mv+6)
	read := func(cov map[sfnt.GlyphIndex]int, first int, into map[sfnt.GlyphIndex]*mconstruction) {
		for g, i := range cov {
			c := mv + u16(t, first+2*i)
			mc := &mconstruction{}
			nvar := u16(t, c+2)
			for k := 0; k < nvar; k++ {
				mc.variants = append(mc.variants, sfnt.GlyphIndex(u16(t, c+4+4*k)))
				mc.sizes = append(mc.sizes, float64(u16(t, c+6+4*k))/m.upem)
			}
			if a := u16(t, c); a > 0 {
				a += c
				np := u16(t, a+4)
				for k := 0; k < np; k++ {
					p := a + 6 + 10*k
					mc.parts = append(mc.parts, mpart{
						g:     sfnt.GlyphIndex(u16(t, p)),
						start: float64(u16(t, p+2)) / m.upem,
						end:   float64(u16(t, p+4)) / m.upem,
						full:  float64(u16(t, p+6)) / m.upem,
						ext:   u16(t, p+8)&1 != 0,
					})
				}
			}
			into[g] = mc
		}
	}
	read(vcov, mv+10, m.vert)
	read(hcov, mv+10+2*nv, m.horiz)
}

// The MathConstants used, by their offset in the table.
const (
	mcScriptPercent        = 0 // int16 percentages
	mcScriptScriptPercent  = 2
	mcDisplayOperatorMin   = 6  // uint16
	mcAxisHeight           = 12 // MathValueRecords
	mcAccentBaseHeight     = 16
	mcSubShiftDown         = 24
	mcSubTopMax            = 28
	mcSubBaselineDropMin   = 32
	mcSupShiftUp           = 36
	mcSupShiftUpCramped    = 40
	mcSupBottomMin         = 44
	mcSupBaselineDropMax   = 48
	mcSubSupGapMin         = 52
	mcSupBottomMaxWithSub  = 56
	mcSpaceAfterScript     = 60
	mcUpperLimitGapMin     = 64
	mcUpperLimitRiseMin    = 68
	mcLowerLimitGapMin     = 72
	mcLowerLimitDropMin    = 76
	mcStackTopShiftUp      = 80
	mcStackTopDispShiftUp  = 84
	mcStackBotShiftDown    = 88
	mcStackBotDispShift    = 92
	mcStackGapMin          = 96
	mcStackDispGapMin      = 100
	mcFracNumShiftUp       = 120
	mcFracNumDispShiftUp   = 124
	mcFracDenShiftDown     = 128
	mcFracDenDispShiftDown = 132
	mcFracNumGapMin        = 136
	mcFracNumDispGapMin    = 140
	mcFracRule             = 144
	mcFracDenGapMin        = 148
	mcFracDenDispGapMin    = 152
	mcOverbarGap           = 164
	mcOverbarRule          = 168
	mcOverbarExtra         = 172
	mcRadicalGap           = 188
	mcRadicalDispGap       = 192
	mcRadicalRule          = 196
	mcRadicalExtra         = 200
	mcRadicalKernBefore    = 204
	mcRadicalKernAfter     = 208
	mcRadicalDegreeRaise   = 212 // int16 percentage
)

// c is a constant in ems (a percentage for the int16 ones).
func (m *mathFont) c(off int) float64 {
	switch off {
	case mcScriptPercent, mcScriptScriptPercent, mcRadicalDegreeRaise:
		return float64(i16(m.consts, off))
	case mcDisplayOperatorMin:
		return float64(u16(m.consts, off)) / m.upem
	}
	return float64(i16(m.consts, off)) / m.upem
}

// glyph returns the glyph of r, false when the font has none.
func (m *mathFont) glyph(r rune) (sfnt.GlyphIndex, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if g, ok := m.glyphOf[r]; ok {
		return g, g != 0
	}
	g, err := m.f.GlyphIndex(&m.buf, r)
	if err != nil {
		g = 0
	}
	m.glyphOf[r] = g
	return g, g != 0
}

// metric returns the metrics of g in ems.
func (m *mathFont) metric(g sfnt.GlyphIndex) mglyph {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mg, ok := m.metrics[g]; ok {
		return mg
	}
	ppem := fixed.Int26_6(m.upem * 64)
	var mg mglyph
	if b, adv, err := m.f.GlyphBounds(&m.buf, g, ppem, font.HintingNone); err == nil {
		u := func(v fixed.Int26_6) float64 { return float64(v) / 64 / m.upem }
		mg = mglyph{adv: u(adv), h: -u(b.Min.Y), d: u(b.Max.Y), x0: u(b.Min.X), x1: u(b.Max.X)}
		if b.Min.Y == b.Max.Y { // no ink (a space)
			mg.h, mg.d = 0, 0
		}
	}
	m.metrics[g] = mg
	return mg
}

// outline returns the segments of g at ppem pixels per em (y down).
func (m *mathFont) outline(g sfnt.GlyphIndex, ppem float64) sfnt.Segments {
	m.mu.Lock()
	defer m.mu.Unlock()
	segs, err := m.f.LoadGlyph(&m.buf, g, fixed.Int26_6(ppem*64), nil)
	if err != nil {
		return nil
	}
	return append(sfnt.Segments(nil), segs...) // the buffer is reused
}
