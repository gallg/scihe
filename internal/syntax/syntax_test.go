package syntax

import "testing"

func TestDetect(t *testing.T) {
	cases := map[string]string{
		"a.py": "python", "lib.sls": "scheme", "x.ss": "scheme", "core.clj": "clojure",
		"README.md": "markdown", "main.go": "go", "nb.ipynb": "python",
	}
	for name, want := range cases {
		if got := Detect(name, ""); got != want {
			t.Errorf("%s: got %q want %q", name, got, want)
		}
	}
	if got := Detect("script", "#!/usr/bin/env python3\nprint(1)"); got != "python" {
		t.Errorf("shebang: %q", got)
	}
}

func TestHighlightLines(t *testing.T) {
	th := LoadTheme("monokai", false)
	spans := HighlightLines(Lexer("python"), "def f():\n    return \"é\"", 2, th)
	if len(spans) != 2 || len(spans[0]) == 0 {
		t.Fatalf("no spans: %+v", spans)
	}
	// "def" keyword must start at col 0 with width 3
	if spans[0][0].Start != 0 || spans[0][0].End != 3 {
		t.Fatalf("keyword span %+v", spans[0][0])
	}
	// the string literal "é" occupies runes 11..14 on line 2
	found := false
	for _, s := range spans[1] {
		if s.Start == 11 && s.End == 14 {
			found = true
		}
	}
	if !found {
		t.Fatalf("string span missing: %+v", spans[1])
	}
}

type lines []string

func (l lines) LineCount() int          { return len(l) }
func (l lines) LineString(i int) string { return l[i] }

func TestRegions(t *testing.T) {
	th := LoadTheme("monokai", false)
	h := NewHighlighter("python", th, func() []Region {
		return []Region{{0, 1, "markdown"}, {1, 2, "python"}}
	})
	src := lines{"# Title", "import os"}
	if s := h.Spans(src, 1, 1, nil); len(s) == 0 {
		t.Fatal("python region not highlighted")
	}
	h.Shift(0, 1, 2)
	if s := h.Spans(src, 1, 2, nil); len(s) == 0 {
		t.Fatal("shifted spans lost")
	}
}
