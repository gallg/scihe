package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mdEditor(t *testing.T, text string) *Editor {
	t.Helper()
	e := newTestEditor(t, text)
	e.win.buf.Filetype = "markdown"
	return e
}

func TestMdEmphasis(t *testing.T) {
	cases := []struct{ text, keys, want string }{
		// outside a span: the word under the cursor
		{"say hello now", "w<Space>mb", "say **hello** now"},
		{"say hello now", "w<Space>mi", "say *hello* now"},
		{"say hello now", "w<Space>mc", "say `hello` now"},
		{"say hello now", "w<Space>ms", "say ~~hello~~ now"},
		// inside a span, markers included: the whole span
		{"say **hello** now", "7|<Space>mb", "say hello now"},
		{"say **hello** now", "5|<Space>mb", "say hello now"},
		{"say **hello** now", "13|<Space>mb", "say hello now"},
		{"say *hello* now", "6|<Space>mi", "say hello now"},
		{"**hello world** and **x**", "fx<Space>mb", "**hello world** and x"},
		{"`code here` x", "fh<Space>mc", "code here x"},
		{"``a`b`` x", "fa<Space>mc", "a`b x"},
		{"~~gone text~~", "ft<Space>ms", "gone text"},
		// on the whole span: bold becomes italic and back, the same key
		// removes its style
		{"**hello world**", "fw<Space>mi", "*hello world*"},
		{"**hello world**", "0<Space>mi", "*hello world*"},
		{"*hello world*", "fw<Space>mb", "**hello world**"},
		{"**hello world**", "fw<Space>mb", "hello world"},
		{"*hello world*", "fw<Space>mi", "hello world"},
		{"say **hello** now", "7|<Space>mi<Space>mb", "say **hello** now"},
		{"*a* and **b**", "fa<Space>mi", "a and **b**"},
		{"*a* and **b**", "fb<Space>mi", "*a* and *b*"},
		// bold and italic together (typed by hand): the key's style goes
		{"***hello world***", "fw<Space>mi", "**hello world**"},
		{"***hello world***", "fw<Space>mb", "*hello world*"},
		// the innermost span
		{"**a *b* c**", "fb<Space>mi", "**a b c**"},
		// selections: plain text is wrapped, a selection in a span acts on it
		{"say hello now", "wve<Space>mb", "say **hello** now"},
		{"say two words", "wv$<Space>mi", "say *two words*"},
		{"say **hello world** now", "fov4l<Space>mb", "say hello world now"},
		{"say **hello world** now", "fov4l<Space>mi", "say *hello world* now"},
		{"one two\nthree", "Vj<Space>mi", "*one two*\n*three*"},
		// line markers stay outside
		{"- one\n> ## two  \n3. [x] three", "VG<Space>mb", "- **one**\n> ## **two**  \n3. [x] **three**"},
		// empty items are left alone
		{"- \n# \nx", "VG<Space>mb", "- \n# \n**x**"},
		// an unmatched backtick is text, not the start of code
		{"don`t **bold** here", "fb<Space>mb", "don`t bold here"},
		{"don`t **bold** here", "fb<Space>mi", "don`t *bold* here"},
		{"- **a b**", "V<Space>mb", "- a b"},
		{"- *a b*", "V<Space>mb", "- **a b**"},
		// markers outside a single span: refused, nothing changes
		{"**a** and **b**", "V<Space>mb", "**a** and **b**"},
		{"**a** and **b**", "v$<Space>mb", "**a** and **b**"},
		{"plain\n*a* b", "Vj<Space>mi", "plain\n*a* b"},
		// underscores count as markers, but not inside words
		{"__bold__ x", "fo<Space>mb", "bold x"},
		{"_it_ x", "fi<Space>mi", "it x"},
		{"__x__", "V<Space>mb", "x"},
		{"__x__", "fx<Space>mi", "_x_"},
		{"my snake_case_name", "fc<Space>mi", "my *snake_case_name*"},
		// markers in code are not emphasis
		{"`**x**` y", "fy<Space>mb", "`**x**` **y**"},
		// no word: an empty pair, typing between the markers
		{"a  b", "l<Space>mbx<Esc>", "a**x**  b"},
	}
	for _, c := range cases {
		e := mdEditor(t, c.text)
		typeKeys(e, c.keys)
		if got := curText(e); got != c.want {
			t.Errorf("%q + %s: %q, want %q", c.text, c.keys, got, c.want)
		}
	}
	// the cursor stays on its letter, so the keys can be repeated
	e := mdEditor(t, "**hello world**")
	typeKeys(e, "fw<Space>mi")
	if e.win.cur.Col != 7 || e.win.buf.RuneAt(e.win.cur) != 'w' {
		t.Fatalf("cursor at %d", e.win.cur.Col)
	}
	typeKeys(e, "<Space>mb")
	if curText(e) != "**hello world**" || e.win.buf.RuneAt(e.win.cur) != 'w' {
		t.Fatalf("round trip: %q, cursor %d", curText(e), e.win.cur.Col)
	}
	// a refusal says why
	e = mdEditor(t, "**a** and **b**")
	typeKeys(e, "V<Space>mb")
	if !strings.Contains(e.msg, "outside a single span") {
		t.Fatalf("msg %q", e.msg)
	}
	// one undo step
	e = mdEditor(t, "say hello")
	typeKeys(e, "w<Space>mbu")
	if curText(e) != "say hello" {
		t.Fatalf("undo: %q", curText(e))
	}
}

func TestMdOnlyInMarkdown(t *testing.T) {
	e := newTestEditor(t, "hello")
	typeKeys(e, "<Space>mb")
	if curText(e) != "hello" || !strings.Contains(e.msg, "not a markdown buffer") {
		t.Fatalf("%q / %q", curText(e), e.msg)
	}
}

func TestMdLink(t *testing.T) {
	e := mdEditor(t, "see docs here")
	typeKeys(e, "w<Space>mlhttps://x.org<CR>")
	if curText(e) != "see [docs](https://x.org) here" {
		t.Fatalf("link: %q", curText(e))
	}
	typeKeys(e, "u")
	if curText(e) != "see docs here" {
		t.Fatalf("undo: %q", curText(e))
	}
	typeKeys(e, "0v2e<Space>mlu<CR>")
	if curText(e) != "[see docs](u) here" {
		t.Fatalf("selection link: %q", curText(e))
	}
	e = mdEditor(t, "see docs")
	typeKeys(e, "w<Space>mlmy file (1).png<CR>")
	if curText(e) != "see [docs](<my file (1).png>)" {
		t.Fatalf("url with blanks: %q", curText(e))
	}
	// in a link: unlinked (images are left alone)
	e = mdEditor(t, "see [the docs](<a b>) and ![img](x.png)")
	typeKeys(e, "fh<Space>ml")
	if curText(e) != "see the docs and ![img](x.png)" {
		t.Fatalf("unlink: %q", curText(e))
	}
	typeKeys(e, "fi<Space>ml<Esc>")
	if curText(e) != "see the docs and ![img](x.png)" {
		t.Fatalf("image unlinked: %q", curText(e))
	}
	e = mdEditor(t, "a  b")
	typeKeys(e, "l<Space>mlu<CR>text<Esc>")
	if curText(e) != "a[text](u)  b" {
		t.Fatalf("link without a word: %q", curText(e))
	}
	typeKeys(e, "0<Space>ml<Esc>")
	if curText(e) != "a[text](u)  b" {
		t.Fatalf("cancelled link changed the text: %q", curText(e))
	}
}

func TestMdHeading(t *testing.T) {
	e := mdEditor(t, "Title")
	for _, want := range []string{"# Title", "## Title", "### Title", "#### Title", "##### Title", "###### Title", "Title"} {
		typeKeys(e, "<Space>mh")
		if curText(e) != want {
			t.Fatalf("cycle: %q, want %q", curText(e), want)
		}
	}
	typeKeys(e, "<Space>m3")
	if curText(e) != "### Title" {
		t.Fatalf("m3: %q", curText(e))
	}
	typeKeys(e, "<Space>m3")
	if curText(e) != "Title" {
		t.Fatalf("m3 again: %q", curText(e))
	}
	for _, c := range []struct{ in, want string }{
		{"# C#", "## C#"},
		{"# Title ##", "## Title"},
		{"#", "## "},
		{"#hashtag", "# #hashtag"},
	} {
		e := mdEditor(t, c.in)
		typeKeys(e, "<Space>mh")
		if curText(e) != c.want {
			t.Errorf("%q: %q, want %q", c.in, curText(e), c.want)
		}
	}
}

func TestMdLists(t *testing.T) {
	cases := []struct{ text, keys, want string }{
		{"a\n\nb", "Vjj<Space>m-", "- a\n\n- b"},
		{"- a\n  * b", "Vj<Space>m-", "a\n  b"},
		{"a\n- b\nc", "VG<Space>m-", "- a\n- b\n- c"},
		{"a\n- b\nc", "VG<Space>mo", "1. a\n2. b\n3. c"},
		{"1. a\n2. b", "Vj<Space>mo", "a\nb"},
		{"1. a\nb", "j<Space>mo", "1. a\n2. b"},
		{"99999999999999999999. a\nb", "j<Space>mo", "99999999999999999999. a\n1. b"},
		{"a", "<Space>mx", "- [ ] a"},
		{"- [ ] a", "<Space>mx", "- [x] a"},
		{"  1. [X] a", "<Space>mx", "  1. [ ] a"},
		{"* a\nb", "Vj<Space>mx", "* [ ] a\n- [ ] b"},
		{"a\n\nb", "VG<Space>mq", "> a\n>\n> b"},
		{"> a\n>\n> b", "VG<Space>mq", "a\n\nb"},
	}
	for _, c := range cases {
		e := mdEditor(t, c.text)
		typeKeys(e, c.keys)
		if got := curText(e); got != c.want {
			t.Errorf("%q + %s: %q, want %q", c.text, c.keys, got, c.want)
		}
	}
}

func TestMdCodeBlock(t *testing.T) {
	e := mdEditor(t, "intro\nx = 1\ny = 2\nend")
	typeKeys(e, "jVj<Space>mCpython<CR>")
	if want := "intro\n```python\nx = 1\ny = 2\n```\nend"; curText(e) != want {
		t.Fatalf("fence: %q", curText(e))
	}
	typeKeys(e, "u")
	if curText(e) != "intro\nx = 1\ny = 2\nend" {
		t.Fatalf("undo: %q", curText(e))
	}
	// in a fenced block (fence lines included) the fences go
	for _, keys := range []string{"3G<Space>mC", "2G<Space>mC", "5G<Space>mC", "3GVj<Space>mC"} {
		e := mdEditor(t, "intro\n```python\nx = 1\ny = 2\n```\nend")
		typeKeys(e, keys)
		if curText(e) != "intro\nx = 1\ny = 2\nend" {
			t.Errorf("%s: %q", keys, curText(e))
		}
	}
	// an empty block leaves an empty line
	e = mdEditor(t, "a\n```\n```\nb")
	typeKeys(e, "j<Space>mC")
	if curText(e) != "a\n\nb" {
		t.Fatalf("empty block: %q", curText(e))
	}
	// a shorter fence does not close a longer one
	e = mdEditor(t, "````\ny\n```\nz\n````")
	typeKeys(e, "jj<Space>mC")
	if curText(e) != "y\n```\nz" {
		t.Fatalf("nested fence: %q", curText(e))
	}
	// "```python" does not close a block
	e = mdEditor(t, "```\na\n```python\nb\n```")
	typeKeys(e, "4G<Space>mC")
	if curText(e) != "a\n```python\nb" {
		t.Fatalf("info string closing: %q", curText(e))
	}
}

func TestMdAlignTable(t *testing.T) {
	e := mdEditor(t, "text\n| a | long header |\n|:-|-:|\n|wide cell|1|\n| é | `x\\|y` |\nafter")
	typeKeys(e, "jj<Space>mt")
	want := "text\n" +
		"| a         | long header |\n" +
		"| :-------- | ----------: |\n" +
		"| wide cell |           1 |\n" +
		"| é         |      `x\\|y` |\n" +
		"after"
	if curText(e) != want {
		t.Fatalf("aligned:\n%s", curText(e))
	}
	// GFM: a pipe in code splits the cell too, a backtick hides nothing
	if got := splitTableRow("| it`s | `a|b` |"); strings.Join(got, ",") != "it`s,`a,b`" {
		t.Fatalf("cells %q", got)
	}
	if formatTable([][]string{{"x", "y"}, {":-:", "-"}})[1] != "| :-: | --- |" {
		t.Fatalf("narrow delimiters: %q", formatTable([][]string{{"x", "y"}, {":-:", "-"}}))
	}
	typeKeys(e, "gg<Space>mt")
	if !strings.Contains(e.msg, "no table") {
		t.Fatalf("msg %q", e.msg)
	}
}

func TestMdHeadingsAndTOC(t *testing.T) {
	text := "# Intro\n\n```\n# not a heading\n```python\n# nor this\n```\n## Set-up & Use\n## Set-up & Use\n### Deep"
	e := mdEditor(t, text)
	typeKeys(e, "<Space>mn")
	if e.win.cur.Line != 7 {
		t.Fatalf("next heading: line %d", e.win.cur.Line)
	}
	typeKeys(e, "<Space>mn<Space>mN<Space>mN")
	if e.win.cur.Line != 0 {
		t.Fatalf("previous heading: line %d", e.win.cur.Line)
	}
	typeKeys(e, "<Space>mT")
	toc := "<!-- toc -->\n- [Intro](#intro)\n  - [Set-up & Use](#set-up--use)\n  - [Set-up & Use](#set-up--use-1)\n    - [Deep](#deep)\n<!-- tocstop -->\n"
	if curText(e) != toc+text {
		t.Fatalf("toc:\n%s", curText(e))
	}
	// only the first block is refreshed
	e2 := mdEditor(t, "<!-- toc -->\n<!-- tocstop -->\n<!-- toc -->\n# A")
	typeKeys(e2, "<Space>mT")
	if curText(e2) != "<!-- toc -->\n- [A](#a)\n<!-- tocstop -->\n<!-- toc -->\n# A" {
		t.Fatalf("two blocks:\n%s", curText(e2))
	}
	// a link in a heading gives its text, not a link in a link
	e3 := mdEditor(t, "# See [docs](u)")
	typeKeys(e3, "<Space>mT")
	if !strings.Contains(curText(e3), "- [See docs](#see-docs)") {
		t.Fatalf("link in heading:\n%s", curText(e3))
	}
	// refreshed in place
	typeKeys(e, "G<Space>mh<Space>mT")
	if n := strings.Count(curText(e), "<!-- toc -->"); n != 1 || !strings.Contains(curText(e), "      - [Deep](#deep)") {
		t.Fatalf("refresh:\n%s", curText(e))
	}
}

func TestMdImage(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "img"), 0o755)
	os.WriteFile(filepath.Join(dir, "img", "a.png"), []byte("png"), 0o644)
	md := filepath.Join(dir, "n.md")
	os.WriteFile(md, []byte("![one](img/a.png \"t\") and ![two](img/missing.png)\nno image\n<img src=\"img/a.png\" width=\"10\">\n"), 0o644)
	t.Setenv("PATH", "") // never launch a viewer: the opener is "not found"
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+md+"<CR><Space>mI")
	if !strings.Contains(e.msg, "no opener found") {
		t.Fatalf("first image: %q", e.msg)
	}
	typeKeys(e, "$<Space>mI")
	if !strings.Contains(e.msg, "image not found") || !strings.Contains(e.msg, filepath.Join(dir, "img", "missing.png")) {
		t.Fatalf("second image: %q", e.msg)
	}
	typeKeys(e, "j<Space>mI")
	if !strings.Contains(e.msg, "no image under the cursor") {
		t.Fatalf("no image: %q", e.msg)
	}
	typeKeys(e, "j<Space>mI")
	if !strings.Contains(e.msg, "no opener found") {
		t.Fatalf("<img>: %q", e.msg)
	}
	// only image files are handed to the opener (it runs programs too)
	os.WriteFile(filepath.Join(dir, "run.command"), []byte("#!/bin/sh\n"), 0o755)
	os.Symlink(filepath.Join(dir, "run.command"), filepath.Join(dir, "fake.png"))
	os.Mkdir(filepath.Join(dir, "dir.png"), 0o755)
	for _, c := range []struct{ link, want string }{
		{"run.command", "not an image file"},
		{"fake.png", "not an image file"},
		{"dir.png", "not an image file"},
		{"file:///etc/passwd.png", "only http(s) image links"},
		{"smb://host/x.png", "only http(s) image links"},
		{"https://example.org/x.png", "no opener found"},
	} {
		os.WriteFile(md, []byte("![x]("+c.link+")\n"), 0o644)
		typeKeys(e, ":e!<CR>gg<Space>mI")
		if !strings.Contains(e.msg, c.want) {
			t.Errorf("%s: %q, want %q", c.link, e.msg, c.want)
		}
	}
	os.WriteFile(md, []byte("![one](img/a.png)\n"), 0o644)
	typeKeys(e, ":e!<CR>")
	// also from the rendered view
	typeKeys(e, "gg<Space>mr<Space>mI")
	if !strings.Contains(e.msg, "no opener found") {
		t.Fatalf("rendered view: %q", e.msg)
	}
}

func TestRenderMath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"costs $5 and $10", "costs $5 and $10"},
		{`price \$x$`, "price $x$"},
		{"$x^{n+1}_i$", "xⁿ⁺¹ᵢ"},
		{"$e^{i\\pi} = -1$", "e^(iπ) = -1"},
		{`$\sqrt{x^2 + y^2}$, $\sqrt[3]{8}$`, "√(x² + y²), ∛8"},
		{`$\frac{a+b}{2}$`, "(a+b)/2"},
		{`$\mathbb{R}^n \to \mathcal{L}$`, "ℝⁿ → ℒ"},
		{`$\left( \sum_{k=1}^{\infty} \hat{x}_k \right)$`, "( ∑ₖ₌₁^∞ x̂ₖ )"},
		{`$\text{if } x \in A$`, "if  x ∈ A"},
		{`$\unknown{x}$`, `\unknown{x}`},
		{"`$x$` and $x$", "`$x$` and x"},
		{`\(a \cdot b\) and \[c \neq d\]`, "a ⋅ b and c ≠ d"},
		{"$$\n\\alpha\n$$\ntext $y$", "\nα\n\ntext y"},
		{"```\n$x$\n```\n$x$", "```\n$x$\n```\nx"},
		{"```\n```python\n$x$\n```\n$x$", "```\n```python\n$x$\n```\nx"},
		{"````\n```\n$x$\n```\n````\n$x$", "````\n```\n$x$\n```\n````\nx"},
		{"it`s $x^2$ ok", "it`s x² ok"},
		{"$$a\nb", "a\nb"},
		{"$f'(x)$", "f′(x)"},
		{"spaced $ a + b $.", "spaced a + b."},
		{"$5, then $x$", "$5, then x"},
		{"$ x$ and $y $", "$ x$ and $y $"},
		{"\\begin{equation}\nE = mc^2\n\\end{equation}", "\nE = mc²\n"},
		{`\begin{align*} a &= b \\ c &= d \end{align*}`, " a  = b   c  = d "},
		{`\begin{itemize}`, `\begin{itemize}`},
		{`$\alpha_{i,j}$`, "αᵢ,ⱼ"},
	}
	for _, c := range cases {
		if got := renderMath(c.in); got != c.want {
			t.Errorf("%q: %q, want %q", c.in, got, c.want)
		}
	}
	// nested \sqrt[ shares the depth budget: linear, not quadratic
	start := time.Now()
	renderMath("$" + strings.Repeat(`\sqrt[`, 20000) + "$")
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("nested roots took %v", d)
	}
	// deep nesting is cut off, not recursed into
	got := renderMath("$" + strings.Repeat("{", 100000) + "x$")
	if want := strings.Repeat("{", 100000-texMaxDepth) + "x"; got != want {
		t.Errorf("deep nesting: %d runes, want %d", len(got), len(want))
	}
}
