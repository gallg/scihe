package editor

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"scihe/internal/notebook"
)

func TestTypeset(t *testing.T) {
	frac, ok := typeset(parseFormula(`\frac{a}{b}`), true, 0.6)
	if !ok || frac.h <= 0.5 || frac.d <= 0.5 {
		t.Fatalf("display fraction: ok %v, h %.2f d %.2f", ok, frac.h, frac.d)
	}
	rules := 0
	for _, o := range frac.ops {
		if o.rule {
			rules++
		}
	}
	if rules != 1 || len(frac.ops) != 3 {
		t.Fatalf("fraction ops %+v", frac.ops)
	}
	// text style is smaller than display style
	if inline, _ := typeset(parseFormula(`\frac{a}{b}`), false, 0.6); inline.h+inline.d >= frac.h+frac.d {
		t.Fatalf("text fraction %.2f not smaller than display %.2f", inline.h+inline.d, frac.h+frac.d)
	}
	// scripts: raised and lowered, smaller, but not below the minimum size
	sup, _ := typeset(parseFormula(`x^{2^{2}}`), false, 0.6)
	sizes := map[float64]bool{}
	for _, o := range sup.ops {
		sizes[o.s] = true
	}
	if !sizes[1] || !sizes[0.7] || !sizes[0.6] || len(sizes) != 3 {
		t.Fatalf("script sizes %v", sizes)
	}
	if sub, _ := typeset(parseFormula(`x_i`), false, 0.6); sub.d <= 0.1 {
		t.Fatalf("subscript not lowered: depth %.2f", sub.d)
	}
	// a big operator grows in display style and takes limits there
	sum, _ := typeset(parseFormula(`\sum_{k=1}^{n}`), true, 0.6)
	tsum, _ := typeset(parseFormula(`\sum_{k=1}^{n}`), false, 0.6)
	if sum.h+sum.d <= tsum.h+tsum.d || sum.w >= tsum.w+0.5 {
		t.Fatalf("display sum %+v, text sum %+v", sum, tsum)
	}
	// delimiters grow around what they enclose
	small, _ := typeset(parseFormula(`\left( x \right)`), true, 0.6)
	tall, _ := typeset(parseFormula(`\left( \frac{a}{b} \right)`), true, 0.6)
	if tall.h+tall.d <= small.h+small.d+0.5 {
		t.Fatalf("delimiters did not grow: %.2f vs %.2f", tall.h+tall.d, small.h+small.d)
	}
	// a character the font lacks: not typeset
	if _, ok := typeset(parseFormula(`\text{日本}`), false, 0.6); ok {
		t.Fatal("typeset without the glyphs")
	}
	// variables are italic, \mathrm and functions upright, \mathbb double-struck
	for in, want := range map[string]rune{`x`: 0x1D465, `\mathrm{x}`: 'x', `\mathbf{x}`: 0x1D431, `\alpha`: 0x1D6FC, `h`: 'ℎ'} {
		b, _ := typeset(parseFormula(in), false, 0.6)
		g, _ := mfont.glyph(want)
		if len(b.ops) != 1 || b.ops[0].g != g {
			t.Errorf("%s: glyph %v, want that of %U", in, b.ops, want)
		}
	}
	// the parser keeps enough for spacing: \, and \quad differ
	thin, _ := typeset(parseFormula(`a\,b`), false, 0.6)
	quad, _ := typeset(parseFormula(`a\quad b`), false, 0.6)
	if quad.w-thin.w < 0.7 {
		t.Fatalf("\\quad %.2f vs \\, %.2f", quad.w, thin.w)
	}
	// a + b has medium spaces, a, b only after the comma, -a none
	plus, _ := typeset(parseFormula(`a+b`), false, 0.6)
	ab, _ := typeset(parseFormula(`ab`), false, 0.6)
	pg, _ := mfont.glyph('+')
	if d := plus.w - ab.w - mfont.metric(pg).adv; d < 0.4 || d > 0.5 {
		t.Fatalf("spaces around +: %.3f em, want 8 mu", d)
	}
	neg, _ := typeset(parseFormula(`-a`), false, 0.6)
	mg, _ := mfont.glyph('−')
	ag, _ := mfont.glyph(0x1D44E)
	if d := neg.w - mfont.metric(mg).adv - mfont.metric(ag).adv; d > 0.001 {
		t.Fatalf("a sign is spaced: %.3f", d)
	}
}

// testGfx turns typeset formulas on for a test, its commands in out.
func testGfx(t *testing.T) *bytes.Buffer {
	t.Helper()
	var out bytes.Buffer
	g := &mathGfx{out: &out, scale: 1.2}
	g.setup(16, 30, [3]uint8{200, 200, 200})
	mathImages = g
	t.Cleanup(func() { mathImages = nil })
	return &out
}

func gfxIDs(s string) []int {
	var ids []int
	for _, r := range s {
		if r > gfxBase && r <= gfxBase+gfxMaxID {
			ids = append(ids, int(r-gfxBase))
		}
	}
	return ids
}

func TestMathImagesLayout(t *testing.T) {
	out := testGfx(t)
	// a simple inline formula stays on the line; a fraction takes rows
	rows := layoutMarkdown("Energy $E = mc^2$ and $x_i$.", 80, true)
	if len(rows) != 1 || len(gfxIDs(rowString(rows[0].cells))) == 0 {
		t.Fatalf("inline formulas: %d rows %q", len(rows), rowString(rows[0].cells))
	}
	rows = layoutMarkdown("so $\\begin{pmatrix} a \\\\ b \\end{pmatrix}$ here", 80, true)
	if len(rows) < 2 {
		t.Fatalf("inline matrix on %d rows", len(rows))
	}
	text := 0
	for _, r := range rows {
		if strings.Contains(rowString(r.cells), "here") {
			text++
		}
	}
	if text != 1 {
		t.Fatalf("text not on one row: %d rows", len(rows))
	}
	// each row of a formula is an image of its own, sent once with its
	// virtual placement before it is shown
	rows = layoutMarkdown("$$\\sum_{k=1}^{n} \\frac{1}{k^2}$$", 80, true)
	seen := map[int]bool{}
	for _, r := range rows {
		ids := gfxIDs(rowString(r.cells))
		for i, id := range ids {
			if i > 0 && id != ids[0] {
				t.Fatalf("two images on one row: %v", ids)
			}
			seen[id] = true
		}
	}
	if len(seen) < 3 {
		t.Fatalf("display formula on %d image rows", len(seen))
	}
	mathImages.flush()
	cmds := out.String()
	for id := range seen {
		if !strings.Contains(cmds, "i="+strconv.Itoa(id)+",q=2,m=") || !strings.Contains(cmds, "a=p,U=1,i="+strconv.Itoa(id)+",") {
			t.Fatalf("image %d not sent:\n%.300q", id, cmds)
		}
	}
	// laid out again: the same images, not sent again
	out.Reset()
	layoutMarkdown("$$\\sum_{k=1}^{n} \\frac{1}{k^2}$$", 80, true)
	mathImages.flush()
	if out.Len() != 0 {
		t.Fatalf("sent again: %.100q", out.String())
	}
	// Ctrl-L sends them again; a new theme draws them anew
	mathImages.resend()
	mathImages.flush()
	if !strings.Contains(out.String(), "a=t,") {
		t.Fatal("not sent again after resend")
	}
	gen := mathGen()
	mathImages.setup(16, 32, [3]uint8{1, 2, 3})
	out.Reset()
	mathImages.flush()
	if mathGen() == gen || !strings.Contains(out.String(), "a=d,d=I,") {
		t.Fatalf("old images not deleted: %.100q", out.String())
	}
	// a formula wider than the window is shown as text
	wide := layoutMarkdown("$$"+strings.Repeat("x + ", 40)+"x$$", 30, true)
	for _, r := range wide {
		if len(gfxIDs(rowString(r.cells))) > 0 {
			t.Fatalf("too wide but typeset: %q", rowString(r.cells))
		}
	}
	// a formula the font can't draw is shown as text
	if rows := layoutMarkdown("$\\text{日本}$", 80, true); len(gfxIDs(rowString(rows[0].cells))) > 0 {
		t.Fatal("typeset without glyphs")
	}
}

func TestMathImagesTmux(t *testing.T) {
	var sb strings.Builder
	g := &mathGfx{tmux: true}
	g.esc(&sb, "a=d,d=I,i=1,q=2", "")
	if want := "\x1bPtmux;\x1b\x1b_Ga=d,d=I,i=1,q=2\x1b\x1b\\\x1b\\"; sb.String() != want {
		t.Fatalf("%q, want %q", sb.String(), want)
	}
}

func TestMathImagesDrawn(t *testing.T) {
	testGfx(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "m.md")
	os.WriteFile(path, []byte("Energy $E = mc^2$.\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR><Space>mr")
	r := e.renders[e.win.buf.ID]
	if r == nil {
		t.Fatal("not rendered")
	}
	ids := gfxIDs(r.buf.Text())
	if len(ids) == 0 {
		t.Fatalf("no image cells: %q", r.buf.Text())
	}
	e.draw()
	s := e.screen.(tcell.SimulationScreen)
	found := false
	w, h := s.Size()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c, _, st, _ := s.GetContent(x, y)
			if c >= gfxBase && c != gfxPlaceholder && c <= gfxBase+gfxMaxID {
				t.Fatalf("private rune drawn at %d,%d", x, y)
			}
			if c == gfxPlaceholder {
				fg, _, _ := st.Decompose()
				if fg.Hex() != int32(ids[0]) {
					t.Fatalf("placeholder colour %x, want id %d", fg.Hex(), ids[0])
				}
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no placeholder drawn")
	}
}

func TestMathImagesNotebook(t *testing.T) {
	testGfx(t)
	lines := renderOutput("text/latex", `\frac{x^{2}}{2}`)
	n := 0
	for _, l := range lines {
		n += len(gfxIDs(l))
	}
	if len(lines) < 2 || n == 0 {
		t.Fatalf("notebook formula: %q", lines)
	}
}

func TestMathImagesDirect(t *testing.T) {
	out := testGfx(t)
	g := mathImages
	g.direct = true
	b := mathFormulaBox("x", parseFormula("x"), false)
	id := gfxIDs(rowString(b.rows[0]))[0]
	cols := b.width()
	g.flush()
	if strings.Contains(out.String(), "U=1") {
		t.Fatal("a virtual placement with direct placement")
	}
	frame := func(xs ...int) string { // the image drawn at x on row 3 for each x
		out.Reset()
		for _, x := range xs {
			for c := 0; c < cols; c++ {
				g.at(id, x+c, 3, tcell.StyleDefault)
			}
		}
		g.place()
		return out.String()
	}
	if s := frame(5); !strings.Contains(s, "\x1b7\x1b[4;6H") || !strings.Contains(s, "a=p,i="+strconv.Itoa(id)+",p=1,") {
		t.Fatalf("not placed: %q", s)
	}
	if s := frame(5); s != "" {
		t.Fatalf("placed again: %q", s)
	}
	if s := frame(7, 20); !strings.Contains(s, "[4;8H") || !strings.Contains(s, ",p=2,") || strings.Contains(s, "a=d") {
		t.Fatalf("moved and shown twice: %q", s)
	}
	if s := frame(7); !strings.Contains(s, "a=d,d=i,i="+strconv.Itoa(id)+",p=2,") {
		t.Fatalf("second placement not deleted: %q", s)
	}
	// cut by the edge of a window: not placed
	out.Reset()
	g.at(id, 0, 9, tcell.StyleDefault)
	g.place()
	if strings.Contains(out.String(), "[10;1H") {
		t.Fatalf("a partly drawn image placed: %q", out.String())
	}
}

func TestMathImagesInlineRows(t *testing.T) {
	testGfx(t)
	// inline formulas fit the line of text, shrinking to its size if need be
	for _, f := range []string{`a_{ij}`, `\hat{L}`, `\frac{1}{2}`, `f'(x)`, `(X^T X)^{-1}`, `\sum_i (y_i - x_i^T \beta)^2`, `\mathcal{N}(\mu, \sigma^2)`} {
		if b, ok := mathImages.formula(f, parseFormula(f), false); !ok || b.height() != 1 {
			t.Errorf("%s: %d rows", f, b.height())
		}
	}
	// a display fraction makes its line taller, as in LaTeX
	if b, _ := mathImages.formula(`\dfrac{a}{b}`, parseFormula(`\dfrac{a}{b}`), false); b.height() < 2 {
		t.Errorf("\\dfrac on %d rows", b.height())
	}
	// scripts in the text are typeset on the line too
	if rows := layoutMarkdown("H₂O, mc², the 1<sup>st</sup>", 80, true); len(rows) != 1 {
		t.Errorf("text scripts on %d rows", len(rows))
	}
}

func TestMathImagesSpacing(t *testing.T) {
	testGfx(t)
	// formulas whose ink would meet over the boundary of their rows —
	// a fraction filling its line, against script raised to the top of
	// the next one — are set off by a blank row
	rows := layoutMarkdown("- $\\frac{a}{b}$\n- $x^2$\n- plain", 80, true)
	var got []string
	for _, r := range rows {
		got = append(got, strings.TrimRight(rowString(r.cells), " "))
	}
	if len(got) != 4 || got[1] != "" || got[3] != "• plain" {
		t.Fatalf("list rows %q", got)
	}
	if rows[1].src != 0 {
		t.Fatalf("blank row shown for line %d, want the line above it", rows[1].src)
	}
	// small formulas leave as much room around them as text does: no
	// blank row, between different formulas as between the same one
	for _, src := range []string{
		"- $x^2$ twice:\n- $x^2$ again",
		"a $x^2$ line after\na $y_i$ line before",
	} {
		if rows := layoutMarkdown(src, 80, false); len(rows) != 2 {
			var got []string
			for _, r := range rows {
				got = append(got, strings.TrimRight(rowString(r.cells), " "))
			}
			t.Fatalf("%q spaced: %q", src, got)
		}
	}
	// the rows of one formula stay together
	rows = layoutMarkdown("$$\\sum_{k=1}^{n} \\frac{1}{k^2}$$", 80, true)
	for _, r := range rows {
		if strings.TrimSpace(rowString(r.cells)) == "" {
			t.Fatal("a blank row inside a formula")
		}
	}
	// in a table the bars go on through the blank row
	rows = layoutMarkdown("| a | b |\n|---|---|\n| $\\frac{a}{b}$ | 1 |\n| $x^2$ | 2 |", 80, true)
	blank := -1
	for i, r := range rows {
		if s := rowString(r.cells); len(gfxIDs(s)) == 0 && strings.Trim(s, "│ ") == "" {
			blank = i
		}
	}
	if blank < 0 || strings.Count(rowString(rows[blank].cells), "│") != strings.Count(rowString(rows[blank-1].cells), "│") {
		t.Fatalf("no spacer with the bars in the table")
	}
}

func TestNotebookFormulasSpaced(t *testing.T) {
	testGfx(t)
	outs := []map[string]any{
		{"output_type": "display_data", "data": map[string]any{"text/latex": `$x^2$`}},
		{"output_type": "display_data", "data": map[string]any{"text/latex": `$y_i$`}},
	}
	lines := notebook.DisplayLines(outs, renderOutput)
	if len(lines) != 3 || lines[1].Text != "" {
		t.Fatalf("formula outputs not set off: %q", lines)
	}
}

func TestMathImagesReset(t *testing.T) {
	out := testGfx(t)
	g := mathImages
	b, _ := g.formula("x", parseFormula("x"), false)
	first := gfxIDs(rowString(b.rows[0]))[0]
	g.flush()
	// dropped (too many formulas): deleted before the new ones are
	// sent, and their ids used again
	gen := mathGen()
	g.reset()
	b, _ = g.formula("y", parseFormula("y"), false)
	if mathGen() == gen || gfxIDs(rowString(b.rows[0]))[0] != first {
		t.Fatalf("ids not reused after reset: gen %d→%d", gen, mathGen())
	}
	out.Reset()
	g.flush()
	s := out.String()
	if del, send := strings.Index(s, "a=d,d=I,i="+strconv.Itoa(first)), strings.Index(s, "a=t,"); del < 0 || send < del {
		t.Fatalf("not deleted before sent again: %.120q", s)
	}
}

func TestMathImagesSmallScale(t *testing.T) {
	testGfx(t)
	mathImages.scale = 0.8
	big, _ := typeset(parseFormula("x"), false, 0.6)
	b, _ := mathImages.formula("x", parseFormula("x"), false)
	// at 0.8 of the text, as asked, not at the size of the text
	if want := int(math.Ceil((big.w*0.8*gfxEm*30 + 2) / 16)); b.width() != want {
		t.Fatalf("x %d cells wide, want %d", b.width(), want)
	}
}

func TestMathImagesRotate(t *testing.T) {
	out := testGfx(t)
	g := mathImages
	keep, _ := g.formula("a", parseFormula("a"), false)
	gone, _ := g.formula("b", parseFormula("b"), false)
	kept, dropped := gfxIDs(rowString(keep.rows[0]))[0], gfxIDs(rowString(gone.rows[0]))[0]
	g.rotate() // laid out again: only "a" is still shown
	gen := mathGen()
	if b, _ := g.formula("a", parseFormula("a"), false); gfxIDs(rowString(b.rows[0]))[0] != kept {
		t.Fatal("a formula still shown drawn anew")
	}
	g.rotate()
	if mathGen() == gen || g.imgs[dropped] != nil || g.imgs[kept] == nil {
		t.Fatalf("rotate: dropped %v, kept %v", g.imgs[dropped] == nil, g.imgs[kept] != nil)
	}
	out.Reset()
	g.flush()
	if !strings.Contains(out.String(), "a=d,d=I,i="+strconv.Itoa(dropped)+",") {
		t.Fatalf("not deleted: %q", out.String())
	}
	// its id is used again, and a formula of several rows still holds
	// together on reused ids
	tall, _ := g.formula(`\begin{pmatrix} a \\ b \\ c \end{pmatrix}`, parseFormula(`\begin{pmatrix} a \\ b \\ c \end{pmatrix}`), true)
	if gfxIDs(rowString(tall.rows[0]))[0] != dropped {
		t.Fatal("freed id not used again")
	}
	rows := boxRows(tall, 0)
	if spaced := g.spaceFormulas(rows); len(spaced) != len(rows) {
		t.Fatal("rows of one formula set apart")
	}
	// many formulas, all shown: not rotated again and again
	e := newTestEditor(t, "")
	e.syncMathImages(false) // the theme's colour
	for i := 0; i <= gfxMaxCache; i++ {
		g.formula(strconv.Itoa(i), parseFormula(strconv.Itoa(i)), false)
	}
	e.syncMathImages(false) // rotated once
	gen = mathGen()
	for i := 0; i <= gfxMaxCache; i++ {
		g.formula(strconv.Itoa(i), parseFormula(strconv.Itoa(i)), false)
	}
	e.syncMathImages(false)
	if mathGen() != gen {
		t.Fatal("rotated again with the same formulas")
	}
}

func TestMathImagesYank(t *testing.T) {
	testGfx(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "y.md")
	os.WriteFile(path, []byte("Energy $E = mc^2$.\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR><Space>mryy")
	if r, _ := e.regs.Get('"'); r.Text != "Energy E = mc².\n" {
		t.Fatalf("yanked %q", r.Text)
	}
	// :nboutput-like text has no image cells
	var lines []string
	withoutMathImages(func() { lines = renderOutput("text/latex", `x^2`) })
	if len(gfxIDs(strings.Join(lines, ""))) > 0 || mathImages == nil {
		t.Fatalf("images in text output: %q", lines)
	}
}

func TestMathImagesQuoteSpacer(t *testing.T) {
	testGfx(t)
	// a quote keeps its bar through the space formulas are set off by
	rows := layoutMarkdown("> - $\\frac{a}{b}$\n> - $x^2$", 80, true)
	if len(rows) != 3 || strings.TrimRight(rowString(rows[1].cells), " ") != "┃" {
		var got []string
		for _, r := range rows {
			got = append(got, rowString(r.cells))
		}
		t.Fatalf("quote rows %q", got)
	}
	// small formulas in a quote are not set off
	if rows = layoutMarkdown("> - $x^2$\n> - $y_i$", 80, true); len(rows) != 2 {
		var got []string
		for _, r := range rows {
			got = append(got, rowString(r.cells))
		}
		t.Fatalf("quote rows %q", got)
	}
}

func TestMathImagesPopup(t *testing.T) {
	out := testGfx(t)
	g := mathImages
	g.direct = true
	b, _ := g.formula("x", parseFormula("x"), false)
	id := gfxIDs(rowString(b.rows[0]))[0]
	for c := 0; c < b.width(); c++ {
		g.at(id, 2+c, 1, tcell.StyleDefault)
	}
	g.check = func(x, y int, st tcell.Style) bool { return x != 3 } // a popup over one cell
	out.Reset()
	g.place()
	if strings.Contains(out.String(), "a=p,") {
		t.Fatalf("placed over a popup: %q", out.String())
	}
}
