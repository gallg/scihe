package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"

	"scihe/internal/notebook"
)

func formulaText(s string) string { return boxString(formulaBox(parseFormula(s))) }

// boxString is the text of b, one line per row, trailing blanks cut.
func boxString(b box) string {
	lines := make([]string, len(b.rows))
	for i, r := range b.rows {
		lines[i] = strings.TrimRight(rowString(r), " ")
	}
	return strings.Join(lines, "\n")
}

func TestFormulaLayout(t *testing.T) {
	cases := []struct{ in, want string }{
		{"x^2 + y_i", " 2\nx  + y\n      i"},
		{`\frac{a}{b} + 1`, " a\n─── + 1\n b"},
		{`\frac{a+b}{c-d}`, " a + b\n───────\n c − d"},
		{`{a+b \over c-d}`, " a + b\n───────\n c − d"},
		{`\sqrt{x}`, "  _\n╲╱x"},
		{`\sqrt[3]{8}`, "3  _\n ╲╱8"},
		{`\sum_{k=1}^{n} k`, " n\n ∑  k\nk=1"},
		{`\lim_{x \to 0} f(x)`, "lim f(x)\nx→0"},
		{`\left( \frac{a}{b} \right)`, "⎛ a ⎞\n⎜───⎟\n⎝ b ⎠"},
		{`\begin{matrix} a & b \\ c & d \end{matrix}`, "a  b\nc  d"},
		{`|x| \le 1 \ne 2 \to 3`, "|x| ≤ 1 ≠ 2 → 3"},
		{`\begin{cases} 1 & x > 0 \\ 0 & x \leq 0 \end{cases}`, "⎧1  x > 0\n⎩0  x ≤ 0"},
		{`\alpha + \beta \in \mathbb{R}`, "α + β ∈ ℝ"},
		{`x \text{if} y`, "x if y"},
		{`\hat x`, "^\nx"},
		{"x²", " 2\nx"},
		{`\int_0^1 x dx`, " 1\n∫  x dx\n 0"}, // blanks between terms are kept
		{"ad - b c", "ad − b c"},
		{`\frac12`, " 1\n───\n 2"},
		{`\left( \frac{a}{b} \right)^2`, "⎛ a ⎞2\n⎜───⎟\n⎝ b ⎠"},
		{`\begin{pmatrix} 1 & 0 \\ 0 & 1 \end{pmatrix}`, "⎛1  0⎞\n⎝0  1⎠"},
		{`\begin{aligned} a &= b + c \\ d &= e \\ \end{aligned}`, "a = b + c\nd = e"},
		{`\sin\theta + \cos(x)`, "sin θ + cos(x)"},
		{`\mathbb{R}^n \to \mathcal{L}`, " n\nℝ  → ℒ"},
		{"f'(x) = -1", "f′(x) = −1"},
		{`2\sigma + m v`, "2σ + m v"},
		{`\{ a \}`, "{a}"},
		{`x \not\in A, \; x^12 % a comment`, "        1\nx ∉ A, x 2"}, // x^12 is x¹ then 2, as in TeX
		{`\operatorname{tr}{\left(A \right)} + \foo{x}`, `tr(A) + \foo x`},
		{`\begin{array}{lr} 1 & 22 \\ 333 & 4 \end{array}`, "1    22\n333   4"},
		{`\displaystyle \hat{\beta} \tag{2}`, "^\nβ    (2)"},
		// brackets around a script stay on the line; around a fraction they grow
		{`p(x_i \mid \theta)`, "p(x  ∣ θ)\n   i"},
		{`(\frac{1}{2})`, "⎛ 1 ⎞\n⎜───⎟\n⎝ 2 ⎠"},
		// an integral is as tall as its integrand
		{`\int e^{\frac{x}{2}} dx`, "⌠  x/2\n⌡ e    dx"},
		// scripts are set on one row, simple ones in a fraction on the line:
		// no formula taller than it needs
		{"e^{-x^2}", " −x²\ne"},
		{`e^{-\frac{(x - \mu)^2}{2\sigma^2}}`, " −(x − μ)²/(2σ²)\ne"},
		{`\frac{a^2 + b_i}{2\sigma^2}`, " a² + bᵢ\n─────────\n   2σ²"},
		{`x^{\frac{a}{b}}`, " a/b\nx"},
		{`(y_i - x_i^T \beta)^2`, "       T   2\n(y  − x  β)\n  i    i"},
		{`\Delta x ,\, 2 \log x \; f(x) dx`, "Δx, 2 log x f(x) dx"},
	}
	for _, c := range cases {
		if got := formulaText(c.in); got != c.want {
			t.Errorf("%s:\n%s\nwant\n%s", c.in, got, c.want)
		}
	}
	// the one-line fallback
	for in, want := range map[string]string{
		`\frac{a+b}{2}`: "(a + b)/2", "x^{n+1}_i": "xᵢⁿ⁺¹", `\sqrt{x}`: "√x", `e^{i \pi}`: "e^(iπ)",
	} {
		if got := parseFormula(in).linear(); got != want {
			t.Errorf("linear %s: %q, want %q", in, got, want)
		}
	}
}

func TestFormulaNestingBounded(t *testing.T) {
	start := time.Now()
	for _, s := range []string{
		strings.Repeat(`\sqrt{`, 20000), strings.Repeat("{", 100000) + "x", strings.Repeat(`\hat `, 50000) + "x",
		strings.Repeat("(", 20000), strings.Repeat(`a \over `, 20000) + "b", strings.Repeat("x^", 20000),
		strings.Repeat(`\begin{pmatrix}`, 20000), strings.Repeat(`\left(`, 20000), strings.Repeat(`\frac{`, 20000),
		strings.Repeat(`\sqrt[`, 20000), strings.Repeat(`\text{`, 20000), strings.Repeat(`\not`, 20000),
	} {
		formulaBox(parseFormula(s))
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("deep formulas took %v", d)
	}
}

// layoutText is the rendered text of src.
func layoutText(src string, width int, wrap bool) string {
	rows := layoutMarkdown(src, width, wrap)
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = strings.TrimRight(rowString(r.cells), " ")
	}
	return strings.Join(lines, "\n")
}

func TestMarkdownLayout(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"emphasis, code and links lose their markers",
			"**bold** *it* `co de` ~~gone~~ [link](http://x) _under_",
			"bold it co de gone link under"},
		{"dollars that are not formulas", "`$y$` costs $5 and $10 in total, \\$x", "$y$ costs $5 and $10 in total, $x"},
		{"paragraph lines flow", "one\ntwo\nthree", "one two three"},
		{"hard break", "one  \ntwo\\\nthree", "one\ntwo\nthree"},
		{"headings", "# Title\n## Sub\n### Small", "Title\n━━━━━\nSub\n───\nSmall"},
		{"setext", "Title\n===\n\nSub\n---", "Title\n━━━━━\n\nSub\n───"},
		{"rule", "a\n\n---", "a\n\n" + strings.Repeat("─", 20)},
		{"lists", "- a\n  - b\n    - c\n1. one\n- [ ] todo\n- [x] done",
			"• a\n  ◦ b\n    ▪ c\n1. one\n☐ todo\n☑ done"},
		{"list text hangs", "- one two three four five six", "• one two three four\n  five six"},
		{"quote and alert", "> quoted\n> > nested\n\n> [!WARNING]\n> careful",
			"┃ quoted\n┃ ┃ nested\n\n┃ ⚠ Warning\n┃ careful"},
		{"code is left alone", "```\n**a** $x$\n```", "```\n**a** $x$\n```"},
		{"indented code", "text\n\n    $x$ *y*", "text\n\n$x$ *y*"},
		{"scripts in the text are raised", "H₂O and x² and 1<sup>st</sup>", "         2      st\nH O and x  and 1\n 2"},
		{"display formula centred", "$$\n\\frac{x}{2}\n$$", "         x\n        ───\n         2"},
		{"environment centred", "\\begin{aligned}\na &= b\n\\end{aligned} after", "       a = b\nafter"},
		{"front matter is kept", "---\ntitle: x\n---\ntext", "---\ntitle: x\n---\ntext"},
		{"image", "see ![plot](p.png)", "see ▣ plot"},
		{"footnote", "a[^1]\n\n[^1]: note", " 1\na\n\n1\n  note"},
		{"html comment hidden", "a <!-- hidden --> b", "a b"},
	}
	for _, c := range cases {
		width := 60
		if strings.Contains(c.name, "hangs") || c.name == "rule" || strings.Contains(c.name, "centred") {
			width = 20
		}
		if got := layoutText(c.in, width, true); got != c.want {
			t.Errorf("%s:\n%s\nwant\n%s", c.name, got, c.want)
		}
	}
	// without wrap the lines of the source are kept
	if got := layoutText("one\ntwo", 20, false); got != "one\ntwo" {
		t.Errorf("nowrap: %q", got)
	}
	if got := layoutText("- one two three four five six", 20, false); got != "• one two three four five six" {
		t.Errorf("nowrap list: %q", got)
	}
}

func TestMarkdownTableFitsTheWidth(t *testing.T) {
	src := "| Model | Description | Score |\n|:--|---|--:|\n" +
		"| baseline | a simple linear model trained on the whole data set | 0.81 |\n" +
		"| ours | deep network with $\\frac{1}{2}$ dropout | **0.93** |\n| 東京 | wide | 1 |"
	for width := 16; width <= 90; width++ {
		rows := layoutMarkdown(src, width, true)
		for _, r := range rows {
			if w := runewidth.StringWidth(rowString(r.cells)); w > width {
				t.Fatalf("width %d: row %q is %d wide", width, rowString(r.cells), w)
			}
		}
		// every row of the table has its borders at the same columns
		first := rowString(rows[0].cells)
		for _, r := range rows {
			if s := rowString(r.cells); runewidth.StringWidth(s) != runewidth.StringWidth(first) {
				t.Fatalf("width %d: ragged table\n%s", width, layoutText(src, width, true))
			}
		}
	}
	// rows on several lines: a rule between all of them, a double one
	// under the header
	want := "┌──────────┬───────────────┬───────┐\n" +
		"│ Model    │ Description   │ Score │\n" +
		"╞══════════╪═══════════════╪═══════╡\n" +
		"│ baseline │ a simple      │  0.81 │\n" +
		"│          │ linear model  │       │\n" +
		"│          │ trained on    │       │\n" +
		"│          │ the whole     │       │\n" +
		"│          │ data set      │       │\n" +
		"├──────────┼───────────────┼───────┤\n" +
		"│ ours     │ deep network  │  0.93 │\n" +
		"│          │       1       │       │\n" +
		"│          │ with ───      │       │\n" +
		"│          │       2       │       │\n" +
		"│          │ dropout       │       │\n" +
		"├──────────┼───────────────┼───────┤\n" +
		"│ 東京     │ wide          │     1 │\n" +
		"└──────────┴───────────────┴───────┘"
	if got := layoutText(src, 36, true); got != want {
		t.Errorf("table at 36 columns:\n%s", got)
	}
	// rows map to their source lines
	rows := layoutMarkdown(src, 36, true)
	if rows[0].src != 0 || rows[2].src != 1 || rows[4].src != 2 || rows[len(rows)-2].src != 4 {
		t.Errorf("source lines: %v", rows)
	}
	// rows of one line each need no rules
	if got := layoutText("| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |", 36, true); got != "┌───┬───┐\n│ a │ b │\n├───┼───┤\n│ 1 │ 2 │\n│ 3 │ 4 │\n└───┴───┘" {
		t.Errorf("one-line rows:\n%s", got)
	}
	// unwrapped, a table keeps its natural width
	if got := strings.Split(layoutText(src, 36, false), "\n")[3]; !strings.Contains(got, "a simple linear model trained on the whole data set") {
		t.Errorf("nowrap row: %q", got)
	}
}

// the view shares the options of its source: wrap carries over both
// ways, and the layout follows the width of the window
func TestRenderWrapAndResize(t *testing.T) {
	e := newTestEditor(t, "")
	e.screen.(tcell.SimulationScreen).SetSize(40, 12)
	e.win.buf.Filetype = "markdown"
	e.win.buf.SetText("# T\n\n" + prose + "\n\n| a | b |\n|---|---|\n| " + prose + " | x |")
	md := e.win.buf
	typeKeys(e, ":set wrap<CR><Space>mr")
	e.draw()
	r := e.renders[e.win.buf.ID]
	if r == nil || !r.wrap || !e.wraps(e.win) {
		t.Fatal("the view does not start wrapped")
	}
	fits := func(width int) {
		t.Helper()
		for i := 0; i < r.buf.LineCount(); i++ {
			if w := runewidth.StringWidth(r.buf.LineString(i)); w > width {
				t.Fatalf("row %d is %d wide in %d columns:\n%s", i, w, width, r.buf.Text())
			}
		}
	}
	fits(e.textWidth(e.win))
	// resizing lays it out again, the cursor stays on its source line
	typeKeys(e, "G")
	src := r.srcOf(e.win.cur.Line)
	e.screen.(tcell.SimulationScreen).SetSize(30, 12)
	e.draw()
	if r.width != e.textWidth(e.win) || r.srcOf(e.win.cur.Line) != src {
		t.Fatalf("not laid out again: width %d, cursor on line %d", r.width, r.srcOf(e.win.cur.Line))
	}
	fits(e.textWidth(e.win))
	// toggled in the view, wrap stays off in the source
	typeKeys(e, ":set nowrap<CR>")
	e.draw()
	if r.wrap || !strings.Contains(r.buf.Text(), prose) {
		t.Fatal("nowrap: the paragraph should keep its line")
	}
	typeKeys(e, "<Space>mr")
	if e.win.buf != md || e.options(md).Wrap {
		t.Fatal("wrap not carried back to the source")
	}
	typeKeys(e, ":set wrap<CR><Space>mr<Space>mr")
	if !e.options(md).Wrap {
		t.Fatal("wrap lost through the view")
	}
}

func TestWrappedLineNavigation(t *testing.T) {
	e := newTestEditor(t, prose+"\nshort\n"+prose)
	e.screen.(tcell.SimulationScreen).SetSize(40, 12)
	typeKeys(e, ":set wrap<CR>")
	e.draw()
	row := func() int { return segIndex(e.segments(e.win, e.win.cur.Line), e.win.cur.Col) }
	// the arrows go by screen row too
	typeKeys(e, "gg<Down>")
	if e.win.cur.Line != 0 || row() != 1 {
		t.Fatalf("<Down> -> %v row %d", e.win.cur, row())
	}
	typeKeys(e, "<Up>")
	if e.win.cur.Line != 0 || row() != 0 {
		t.Fatalf("<Up> -> %v", e.win.cur)
	}
	// the column is kept across a short row
	typeKeys(e, "gg10l")
	typeKeys(e, "jjjjj")
	if e.win.cur.Line != 2 || e.win.cur.Col != 10 {
		t.Fatalf("column lost: %v", e.win.cur)
	}
	// operators still take whole lines
	typeKeys(e, "ggdj")
	if e.win.buf.LineCount() != 1 {
		t.Fatalf("dj: %d lines left", e.win.buf.LineCount())
	}
	// insert mode
	e.win.buf.SetText(prose)
	typeKeys(e, "ggi<Down>")
	if row() != 1 {
		t.Fatalf("insert <Down> -> %v", e.win.cur)
	}
	typeKeys(e, "<Up><Esc>")
	if row() != 0 {
		t.Fatalf("insert <Up> -> %v", e.win.cur)
	}
}

func TestMarkdownScientificInput(t *testing.T) {
	e := newTestEditor(t, "")
	e.win.buf.Filetype = "markdown"
	b := e.win.buf
	set := func(s string) { b.SetText(s); typeKeys(e, "gg0") }
	// superscripts and subscripts
	set("E = mc2")
	typeKeys(e, "$<Space>m^")
	if b.LineString(0) != "E = mc²" {
		t.Fatalf("sup: %q", b.LineString(0))
	}
	typeKeys(e, "$<Space>m^")
	if b.LineString(0) != "E = mc2" {
		t.Fatalf("sup back: %q", b.LineString(0))
	}
	set("H2O and x-12")
	typeKeys(e, "l<Space>m_$<Space>m^")
	if b.LineString(0) != "H₂O and x⁻¹²" {
		t.Fatalf("sub and signed sup: %q", b.LineString(0))
	}
	set("1st")
	typeKeys(e, "lvl<Space>m^")
	if b.LineString(0) != "1ˢᵗ" {
		t.Fatalf("selection: %q", b.LineString(0))
	}
	set("aq")
	typeKeys(e, "l<Space>m^")
	if b.LineString(0) != "a<sup>q</sup>" {
		t.Fatalf("no Unicode form: %q", b.LineString(0))
	}
	typeKeys(e, "<Space>m^")
	if b.LineString(0) != "aq" {
		t.Fatalf("unwrap: %q", b.LineString(0))
	}
	// formulas
	set("area pi")
	typeKeys(e, "w<Space>mf")
	if b.LineString(0) != "area $pi$" {
		t.Fatalf("inline formula: %q", b.LineString(0))
	}
	set("x")
	typeKeys(e, "<Space>mF\\frac{a}{b}<Esc>")
	if b.Text() != "x\n$$\n\\frac{a}{b}\n$$" {
		t.Fatalf("display formula: %q", b.Text())
	}
	// symbols by name, also in insert mode
	set("")
	typeKeys(e, "i<C-k>alpha<CR>+<C-k>superscript 2<CR><Esc>")
	if b.LineString(0) != "α+²" {
		t.Fatalf("symbols: %q", b.LineString(0))
	}
}

// inputs that crashed the view, found by fuzzing
func TestMarkdownLayoutNeverPanics(t *testing.T) {
	var deepList strings.Builder
	for i := 0; i < 12; i++ {
		deepList.WriteString(strings.Repeat("  ", i) + "- item\n")
	}
	deepList.WriteString(strings.Repeat("  ", 12) + "# heading\n" + strings.Repeat("  ", 12) + "---")
	for _, in := range []string{
		"$%i$", "$%$", "$%I x$", "$%ixyz$",
		" > quoted", "\v> q", "\f>",
		strings.Repeat("> ", 25) + "---", strings.Repeat("> ", 25) + "# h", strings.Repeat("> ", 30) + "***",
		deepList.String(), "0\n0) * 0) * # 00000000000",
		"$matrix$" + strings.Repeat("x", 60), "$$matrix$$", "| $matrix$ |\n|---|\n| $matrix$ |",
		`$\frac$`, `$\sqrt[$`, `$\begin{$`, `$\end{x}$`, `$\left$`, `$\right)$`, `$\text{$`, `$x^$`, `$_$`,
		`$&&\\\\$`, `$\begin{array}{$`, "\\begin{cases}\nx", `\begin{x}\end{x}`, `$\not$`, `$\tag$`, `$\\$`,
	} {
		for _, wrap := range []bool{true, false} {
			for _, w := range []int{1, 8, 20, 40} {
				layoutMarkdown(in, w, wrap)
			}
		}
	}
	if got := layoutText(" > quoted", 40, true); got != "┃ quoted" {
		t.Errorf("quote after a non-breaking space: %q", got)
	}
}

// paragraphs full of unclosed markers are laid out in linear time
func TestMarkdownLayoutLinear(t *testing.T) {
	for _, unit := range []string{"a < ", "*a ", "_a ", "[a ", "[a](", "`a ", "~~a ", "<!-- ", "<sup>a ", "[a][b ", "$$a ", "\\begin{a}\n"} {
		small := strings.Repeat(unit, 5000/len(unit))
		big := strings.Repeat(unit, 80000/len(unit))
		st := time.Now()
		layoutMarkdown(small, 80, true)
		ds := time.Since(st)
		st = time.Now()
		layoutMarkdown(big, 80, true)
		db := time.Since(st)
		// 16 times the text: linear is ~16x, quadratic ~256x
		if db > 60*ds+50*time.Millisecond {
			t.Errorf("%q: %v for 5 KB, %v for 80 KB", unit, ds, db)
		}
	}
	// the memo does not change what is found
	if got := layoutText("*a *b* c* [x [y](u) <!-- c --> <sup>2</sup>", 60, true); got != "             2\na *b c* [x y" {
		t.Errorf("nested markers: %q", got)
	}
}

func TestRenderedViewFixes(t *testing.T) {
	// C1 controls never reach the terminal
	if got := layoutText("a\u009b31mb", 20, true); got != "a 31mb" {
		t.Errorf("C1 in the view: %q", got)
	}
	e := newTestEditor(t, "a\u009bb")
	e.draw()
	if !strings.HasSuffix(screenLines(e)[0], " a?b") {
		t.Errorf("C1 in a buffer: %q", screenLines(e)[0])
	}
	// :setf keeps the view on its source's options
	e = newTestEditor(t, prose)
	e.win.buf.Filetype = "markdown"
	md := e.win.buf
	typeKeys(e, "<Space>mr:setf markdown<CR>")
	rb := e.win.buf
	if e.opts[rb.ID] != e.options(md) {
		t.Fatal(":setf detached the view's options")
	}
	typeKeys(e, "<Space>mr:setf markdown<CR><Space>mr")
	if e.opts[e.win.buf.ID] != e.options(md) {
		t.Fatal(":setf on the source detached the view")
	}
	// an image on a raised row opens too
	r := e.renders[e.win.buf.ID]
	md.SetText("x² ![a](a.png) ![b](b.png)")
	e.draw()
	if len(r.imgs[0]) != 2 || len(r.imgs[1]) != 2 {
		t.Fatalf("images per row: %v", r.imgs)
	}
}

func TestInputFixes(t *testing.T) {
	e := newTestEditor(t, "")
	b := e.win.buf
	// . repeats an insert with a symbol picked by <C-k>
	typeKeys(e, "ia<C-k>alpha<CR>b<Esc>0.")
	if b.Text() != "aαbaαb" {
		t.Fatalf("dot after <C-k>: %q", b.Text())
	}
	// ^ on a subscript raises it, _ on a superscript lowers it
	b.Filetype = "markdown"
	b.SetText("H₂ x²")
	typeKeys(e, "gg0l<Space>m^$<Space>m_")
	if b.Text() != "H² x₂" {
		t.Fatalf("script kinds: %q", b.Text())
	}
	// the row column is not reused after an edit
	e2 := newTestEditor(t, prose)
	e2.screen.(tcell.SimulationScreen).SetSize(40, 12)
	typeKeys(e2, ":set wrap<CR>")
	e2.draw()
	typeKeys(e2, "gg30lj")
	at := e2.win.cur
	e2.win.buf.Insert(Pos{0, 0}, "x")
	e2.win.cur = at
	e2.win.rowWant = 2 // stale
	typeKeys(e2, "k")
	if e2.win.cur.Col == 2 {
		t.Fatal("stale row column reused after an edit")
	}
}

// LaTeX and markdown outputs of notebook cells are drawn as formulas,
// at the left edge; text/plain stays for the rest
func TestNotebookFormulaOutputs(t *testing.T) {
	cases := []struct{ mime, in, want string }{
		{"text/latex", `$\displaystyle \frac{x^{2}}{2}$`, " x²\n────\n 2"},
		{"text/latex", `\left[\begin{matrix}1 & 2\\3 & 4\end{matrix}\right]`, "⎡1  2⎤\n⎣3  4⎦"},
		{"text/latex", `\begin{align} a &= b \\ c &= d \end{align}`, "a = b\nc = d"},
		{"text/markdown", "**Total**: $\\sum_i x_i$", "Total: ∑ x\n       i  i"},
	}
	for _, c := range cases {
		if got := strings.Join(renderOutput(c.mime, c.in), "\n"); got != c.want {
			t.Errorf("%s:\n%s\nwant\n%s", c.in, got, c.want)
		}
	}
	if renderOutput("text/latex", "  ") != nil || renderOutput("text/latex", "$"+strings.Repeat("x", nbRichMax)+"$") != nil {
		t.Error("an empty or huge output should fall back to text/plain")
	}
	outs := []map[string]any{{"output_type": "execute_result", "data": map[string]any{"text/plain": "x**2/2", "text/latex": `$\frac{x^{2}}{2}$`}}}
	if got := notebook.DisplayLines(outs, nil); len(got) != 1 || got[0].Text != "x**2/2" {
		t.Errorf("plain lines: %+v", got)
	}
	if got := notebook.DisplayLines(outs, renderOutput); len(got) != 3 || got[1].Text != "────" || got[1].Kind != "result" {
		t.Errorf("rendered lines: %+v", got)
	}
}

func TestNotebookFormulaOutputKernel(t *testing.T) {
	py := testPython(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "f.ipynb")
	os.WriteFile(path, []byte(`{"cells": [{"cell_type": "code", "execution_count": null, "id": "c1", "metadata": {}, "outputs": [],
 "source": ["class F:\n", "    def _repr_latex_(self): return r'$\\frac{a}{b}$'\n", "F()"]}],
 "metadata": {}, "nbformat": 4, "nbformat_minor": 5}`), 0o644)
	e := newTestEditor(t, "")
	e.cfg.Python = py
	typeKeys(e, ":e "+path+"<CR>:nbrunall<CR>")
	ks := e.kernels[e.win.buf.ID]
	waitFor(t, e, "cell to run", func() bool {
		c := ks.nb.Cell("c1")
		return c != nil && c.ExecutionCount != nil && ks.running == nil
	})
	var texts []string
	for _, r := range ks.outputRows("c1") {
		texts = append(texts, r.text)
	}
	if got := strings.Join(texts, "|"); got != "Out [1]| a|───| b|" {
		t.Fatalf("output box: %q", got)
	}
}
