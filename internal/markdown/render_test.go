package markdown

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"

	"scihe/internal/syntax"
)

const sample = `# Title

Some *emphasis* and **bold** text with ` + "`code`" + ` and a [link](http://x.y).

- one
- two
  - nested
- [x] done

> quoted
> text

| a | bb |
|---|---:|
| 1 | 22 |

` + "```python\nimport os\nprint(os.getcwd())\n```" + `

1. first
2. second
`

func TestRender(t *testing.T) {
	doc := Render([]byte(sample), 60, syntax.LoadTheme("monokai", false))
	out := doc.String()
	for _, want := range []string{"# Title", "emphasis", "(http://x.y)", "• one", "◦ nested", "☑ done", "▎ quoted text", "│ 1 │ 22 │", "╭─ python", "│ import os", "1. first", "2. second"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if len(doc.Lines) != len(doc.Source) {
		t.Fatal("source map length mismatch")
	}
	// the code block line should map back to its source line
	for i, l := range doc.Lines {
		if strings.Contains(string(l.Text), "import os") {
			src := strings.Split(sample, "\n")[doc.Source[i]]
			if src != "import os" {
				t.Errorf("source map points to %q", src)
			}
		}
	}
}

func TestWrap(t *testing.T) {
	long := strings.Repeat("word ", 40)
	doc := Render([]byte(long), 30, syntax.LoadTheme("monokai", false))
	for _, l := range doc.Lines {
		if len(l.Text) > 30 {
			t.Fatalf("line too long: %q", string(l.Text))
		}
	}
	if len(doc.Lines) < 6 {
		t.Fatalf("expected wrapping, got %d lines", len(doc.Lines))
	}
}

func TestRenderEdgeCases(t *testing.T) {
	th := syntax.LoadTheme("monokai", false)
	out := Render([]byte("- item\n\n  | a | b |\n  |---|---|\n  | 1 | 2 |\n"), 60, th).String()
	if strings.Count(out, "•") != 1 {
		t.Errorf("table in list repeats bullet:\n%s", out)
	}
	out = Render([]byte("<!--\ncomment\n-->\n"), 60, th).String()
	if !strings.Contains(out, "-->") {
		t.Errorf("html closure line dropped:\n%s", out)
	}
	doc := Render([]byte("```go\n\tx := \"s\"\n```\n"), 60, th)
	for _, l := range doc.Lines {
		txt := string(l.Text)
		if i := strings.Index(txt, `"s"`); i >= 0 {
			ok := false
			for _, sp := range l.Spans {
				if sp.Start == len([]rune(txt[:i])) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("string span misaligned after tab: %q %+v", txt, l.Spans)
			}
		}
	}
}

func TestRenderedLineFor(t *testing.T) {
	doc := Render([]byte("# T\n\npara\n\n- a\n"), 40, syntax.LoadTheme("monokai", false))
	if got := doc.RenderedLineFor(0); got != 0 {
		t.Fatalf("heading maps to %d", got)
	}
	if got := doc.RenderedLineFor(2); string(doc.Lines[got].Text) != "para" {
		t.Fatalf("para maps to %q", string(doc.Lines[got].Text))
	}
	if got := doc.RenderedLineFor(1); string(doc.Lines[got].Text) != "para" {
		t.Fatalf("blank source line should map to next block, got %q", string(doc.Lines[got].Text))
	}
}

const wideDoc = "# A heading that is long enough to need wrapping at narrow widths\n\n" +
	"Text with a long URL https://example.com/a/very/long/path/that/cannot/be/broken/at/spaces/index.html end.\n\n" +
	"- a list item with quite a lot of words so that it wraps over several lines\n" +
	"  - nested item with more words than fit on one narrow line of the preview\n\n" +
	"> a quote with enough words to wrap over more than one line of output\n\n" +
	"| Name | Description | Value |\n|:-----|:-----------:|------:|\n" +
	"| alpha | the first entry, with a long description that will not fit | 1.5 |\n" +
	"| beta | short | 22 |\n\n" +
	"```python\nresult = some_function(argument_one, argument_two, argument_three, argument_four)\n```\n\n" +
	"<div class=\"note\">some raw html block that is rather long and must be cut somewhere</div>\n"

func TestRenderFitsWidth(t *testing.T) {
	th := syntax.LoadTheme("monokai", false)
	for _, w := range []int{20, 30, 40, 60, 100} {
		doc := Render([]byte(wideDoc), w, th)
		for i, l := range doc.Lines {
			if lw := runewidth.StringWidth(string(l.Text)); lw > w {
				t.Errorf("width %d: line %d is %d wide: %q", w, i, lw, string(l.Text))
			}
			for _, sp := range l.Spans {
				if sp.Start < 0 || sp.End > len(l.Text) || sp.Start > sp.End {
					t.Errorf("width %d: bad span %+v on %q", w, sp, string(l.Text))
				}
			}
		}
	}
}

func TestRenderTableWrapsCells(t *testing.T) {
	doc := Render([]byte(wideDoc), 50, syntax.LoadTheme("monokai", false))
	var table []string
	in := false
	for _, l := range doc.Lines {
		s := string(l.Text)
		if strings.HasPrefix(s, "┌") {
			in = true
		}
		if in {
			table = append(table, s)
		}
		if strings.HasPrefix(s, "└") {
			in = false
		}
	}
	if len(table) < 6 {
		t.Fatalf("table lines:\n%s", strings.Join(table, "\n"))
	}
	// every table line has the same width and the borders line up
	w0 := runewidth.StringWidth(table[0])
	cols := func(s string) []int {
		var out []int
		x := 0
		for _, r := range s {
			if strings.ContainsRune("│┬┼┴┌┐├┤└┘", r) {
				out = append(out, x)
			}
			x += runewidth.RuneWidth(r)
		}
		return out
	}
	want := cols(table[0])
	for _, l := range table {
		if runewidth.StringWidth(l) != w0 || fmt.Sprint(cols(l)) != fmt.Sprint(want) {
			t.Fatalf("misaligned table:\n%s", strings.Join(table, "\n"))
		}
	}
	// the long description continues on following lines of the same row
	joined := strings.Join(table, "\n")
	if !strings.Contains(joined, "first entry,") || !strings.Contains(joined, "will not fit") {
		t.Fatalf("cell text lost:\n%s", joined)
	}
	// right alignment is kept on the wrapped rows
	for _, l := range table {
		if strings.Contains(l, "1.5") && !strings.Contains(l, "1.5 │") {
			t.Fatalf("value not right-aligned: %q", l)
		}
	}
}

func TestRenderNarrowTableStacks(t *testing.T) {
	src := "| a | b | c | d | e | f |\n|---|---|---|---|---|---|\n| 1 | 2 | 3 | 4 | 5 | 6 |\n"
	doc := Render([]byte(src), 20, syntax.LoadTheme("monokai", false)).String()
	if strings.Contains(doc, "┌") || !strings.Contains(doc, "a: 1") || !strings.Contains(doc, "f: 6") {
		t.Fatalf("stacked table:\n%s", doc)
	}
	// wide enough: a normal table
	if doc := Render([]byte(src), 60, syntax.LoadTheme("monokai", false)).String(); !strings.Contains(doc, "┌") {
		t.Fatalf("normal table:\n%s", doc)
	}
}

func TestRenderCodeContinuation(t *testing.T) {
	doc := Render([]byte(wideDoc), 40, syntax.LoadTheme("monokai", false))
	var code []Line
	in := false
	for _, l := range doc.Lines {
		s := string(l.Text)
		switch {
		case strings.HasPrefix(s, "╭─"):
			in = true
		case strings.HasPrefix(s, "╰─"):
			in = false
		case in:
			code = append(code, l)
		}
	}
	if len(code) < 2 || !strings.HasPrefix(string(code[1].Text), "│↪") {
		t.Fatalf("code lines: %q", doc.String())
	}
	// the pieces put back together give the original line
	var sb strings.Builder
	for _, l := range code {
		sb.WriteString(string(l.Text[2:]))
	}
	if sb.String() != "result = some_function(argument_one, argument_two, argument_three, argument_four)" {
		t.Fatalf("code text %q", sb.String())
	}
	// colours survive the break: the continuation has spans too
	if len(code[1].Spans) == 0 {
		t.Fatal("continuation lost its highlighting")
	}
	// all pieces map to the code line in the source
	if doc.RenderedLineFor(13) < 0 {
		t.Fatal("source mapping")
	}
}

func TestRenderLongWordSplit(t *testing.T) {
	doc := Render([]byte(wideDoc), 30, syntax.LoadTheme("monokai", false)).String()
	if !strings.Contains(strings.ReplaceAll(doc, "\n", ""), "https://example.com/a/very/long/path/that/cannot/be/broken/at/spaces/index.html") {
		t.Fatalf("URL pieces lost:\n%s", doc)
	}
}

func TestRenderWidthEdgeCases(t *testing.T) {
	th := syntax.LoadTheme("monokai", false)
	src := "a\tb\tc\td\te\tf\tg\th\ti\tj\tk\n\n" + strings.Repeat("👩‍💻 ", 12) + "\n\n" + strings.Repeat("café ", 8) + "\n\n" +
		"| h\tx | y |\n|---|---|\n| a\tb | c |\n\n```" + strings.Repeat("verylonglanguage", 4) + "\nx\n```\n\n" +
		"| " + strings.Repeat("H", 40) + " | b | c | d | e | f | g |\n|---|---|---|---|---|---|---|\n| 1 | 2 | 3 | 4 | 5 | 6 | 7 |\n\nctl\x01\x7f\u0085x\n"
	for _, w := range []int{20, 30, 50} {
		for _, l := range Render([]byte(src), w, th).Lines {
			cells := 0
			for _, r := range l.Text {
				if r < 32 || r == 0x7f || r >= 0x80 && r < 0xa0 {
					t.Fatalf("control character in %q", string(l.Text))
				}
				cells += runeCells(r)
			}
			if cells > w {
				t.Errorf("width %d: %d cells: %q", w, cells, string(l.Text))
			}
		}
	}
}
