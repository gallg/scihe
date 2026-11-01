package editor

import (
	"strings"
	"testing"
)

func TestRenderMarkdownBlocks(t *testing.T) {
	rule := strings.Repeat("─", mdRuleWidth)
	cases := []struct{ name, in, want string }{
		{"table",
			"| Name | Value |\n|:-----|------:|\n| a | $\\pi$ |\n| long name | 10 |",
			"Name      │ Value\n──────────┼──────\na         │     π\nlong name │    10"},
		{"table centered, no outer pipes",
			"a | b\n:-:|---\nxyz | 1",
			" a  │ b\n────┼──\nxyz │ 1"},
		{"escaped pipe and formula in a cell",
			"| x |\n|---|\n| $\\|v\\|$ |",
			"x\n───\n|v|"},
		{"missing and extra cells",
			"| a | b |\n|---|---|\n| 1 |\n| 1 | 2 | 3 |",
			"a │ b\n──┼──\n1 │\n1 │ 2"},
		{"table ends at a blank line",
			"| a |\n|---|\n| 1 |\n\n| not a row",
			"a\n─\n1\n\n| not a row"},
		{"no delimiter row: not a table",
			"| a | b |\n| c | d |",
			"| a | b |\n| c | d |"},
		{"cell counts differ: not a table",
			"| a | b |\n|---|",
			"| a | b |\n|---|"},
		{"quote", "> quoted $x^2$\n> > nested", "┃ quoted x²\n┃ ┃ nested"},
		{"alert", "> [!WARNING]\n> Do not run as root", "┃ ⚠ Warning\n┃ Do not run as root"},
		{"alert lowercase with title", "> [!tip]- Read this\n> body", "┃ ★ Read this\n┃ body"},
		{"unknown callout", "> [!example]", "┃ ▸ Example"},
		{"alert marker inside a quote is text", "> text\n> [!NOTE]", "┃ text\n┃ [!NOTE]"},
		{"rules", "---\n\n* * *\n\n___", rule + "\n\n" + rule + "\n\n" + rule},
		{"setext heading underline is kept", "Title\n---", "Title\n---"},
		{"rule after a list item", "- a\n---", "- a\n" + rule},
		{"front matter is kept", "---\ntitle: x\n---\n---", "---\ntitle: x\n---\n" + rule},
		{"tasks", "- [ ] todo\n  * [x] done\n1. [X] first\n- [ ]", "☐ todo\n  ☑ done\n1. ☑ first\n☐"},
		{"task in a quote", "> - [x] $a$", "┃ ☑ a"},
		{"plain list unchanged", "- item\n1. one", "- item\n1. one"},
		{"code is left alone",
			"```\n| a |\n|---|\n> q\n---\n- [ ] t\n```",
			"```\n| a |\n|---|\n> q\n---\n- [ ] t\n```"},
		{"indented code is not a table", "    | a |\n    |---|", "    | a |\n    |---|"},
		{"open display formula is not a quote", "$$\n> x\n$$", "\n> x\n"},
		{"display formula in a quote keeps the bars", "> $$\n> x^2\n> $$\nafter", "┃ \n┃ x²\n┃ \nafter"},
		{"tab in a cell", "| a\tb | c |\n|---|---|\n| x | y |", "a    b │ c\n───────┼──\nx      │ y"},
		{"tab-indented dashes are code, not a rule", "\t---", "\t---"},
		{"a rule ends a table", "| a |\n|---|\n| 1 |\n***", "a\n─\n1\n" + rule},
		{"a list item ends a table", "| a |\n|---|\n- item", "a\n─\n- item"},
		{"delimiter row needs a pipe", "Title |\n---", "Title |\n---"},
		{"heading with a pipe is not a table", "# a | b\n|---|---|", "# a | b\n|---|---|"},
		{"setext underline in a quote", "> Title\n> ---", "┃ Title\n┃ ---"},
	}
	for _, c := range cases {
		got := renderMarkdown(c.in)
		if got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
		if n, m := strings.Count(got, "\n"), strings.Count(c.in, "\n"); n != m {
			t.Errorf("%s: %d lines, source has %d", c.name, n+1, m+1)
		}
	}
}

// renderMarkdown renders formulas exactly as renderMath outside blocks.
func TestRenderMarkdownKeepsMath(t *testing.T) {
	for _, in := range []string{
		"costs $5 and $10", "$x^{n+1}_i$", "$$\n\\alpha\n$$\ntext $y$",
		"```\n$x$\n```\n$x$", "\\begin{equation}\nE = mc^2\n\\end{equation}",
	} {
		if got, want := renderMarkdown(in), renderMath(in); got != want {
			t.Errorf("%q: %q, renderMath %q", in, got, want)
		}
	}
}
