package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"scihe/internal/config"
	"scihe/internal/syntax"
)

// cellStyle is the drawn style of buffer position (l, col) in the current
// window (no wrapping, no tabs before col).
func cellStyle(t *testing.T, e *Editor, l, col int) tcell.Style {
	t.Helper()
	e.draw()
	w := e.win
	_, st, _ := e.screen.Get(w.x+e.gutterWidth(w)+col-w.left, w.y+l-w.top)
	return st
}

func bgOfStyle(st tcell.Style) tcell.Color {
	_, bg, _ := st.Decompose()
	return bg
}

func fgOfStyle(st tcell.Style) tcell.Color {
	fg, _, _ := st.Decompose()
	return fg
}

func isMatchHL(e *Editor, st tcell.Style) bool {
	return bgOfStyle(st) == bgOfStyle(e.theme.MatchParen)
}

func TestMatchParenModes(t *testing.T) {
	e := newTestEditor(t, "f(a, [b])")
	// normal mode, cursor on "(": the ")" is highlighted
	typeKeys(e, "0f(")
	if !isMatchHL(e, cellStyle(t, e, 0, 8)) {
		t.Fatal("normal mode: pair not highlighted")
	}
	// visual mode: no highlight
	typeKeys(e, "v")
	if isMatchHL(e, cellStyle(t, e, 0, 8)) {
		t.Fatal("visual mode: pair highlighted")
	}
	typeKeys(e, "<Esc>")
	// insert mode, cursor right after "]": both "]" and "[" are highlighted
	typeKeys(e, "0f]a")
	if e.mode != ModeInsert || e.win.cur.Col != 8 {
		t.Fatalf("setup: mode %v col %d", e.mode, e.win.cur.Col)
	}
	if !isMatchHL(e, cellStyle(t, e, 0, 5)) || !isMatchHL(e, cellStyle(t, e, 0, 7)) {
		t.Fatal("insert mode: bracket before the cursor and its pair not highlighted")
	}
	// insert mode, cursor on "(" with no bracket before it
	typeKeys(e, "<Esc>0fai")
	if e.win.cur.Col != 2 {
		t.Fatalf("setup: col %d", e.win.cur.Col)
	}
	typeKeys(e, "<Left>")
	if !isMatchHL(e, cellStyle(t, e, 0, 1)) || !isMatchHL(e, cellStyle(t, e, 0, 8)) {
		t.Fatal("insert mode: bracket under the cursor and its pair not highlighted")
	}
	// typing a closing bracket highlights its opening one
	e = newTestEditor(t, "")
	typeKeys(e, "ix = (1 + 2)")
	if !isMatchHL(e, cellStyle(t, e, 0, 4)) {
		t.Fatal("typed ) does not highlight its (")
	}
}

func TestMatchParenLimit(t *testing.T) {
	text := "(" + strings.Repeat("\n", matchParenLines+10) + ")"
	e := newTestEditor(t, text)
	if _, ok := matchBracketWithin(e.win.buf, Pos{}, matchParenLines); ok {
		t.Fatal("limited search found a pair beyond the limit")
	}
	if q, ok := matchBracket(e.win.buf, Pos{}); !ok || q.Line != matchParenLines+10 {
		t.Fatal("% search must stay unlimited")
	}
}

func TestColorBrackets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.py")
	os.WriteFile(path, []byte("f(a[b{c}])\ns = \")\"  # ( in comment\n)\n)\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>")
	th := e.theme
	if len(th.Brackets) < 3 {
		t.Fatalf("theme has %d bracket colours", len(th.Brackets))
	}
	plain := fgOfStyle(cellStyle(t, e, 0, 1))
	// off by default
	if fgOfStyle(cellStyle(t, e, 0, 3)) == th.Brackets[1] {
		t.Fatal("colored without the option")
	}
	typeKeys(e, "<Space>tp")
	if !e.options(e.win.buf).ColorBrackets {
		t.Fatal("Space t p did not turn it on")
	}
	typeKeys(e, "G") // keep the cursor away from the brackets
	want := map[int]tcell.Color{1: th.Brackets[0], 3: th.Brackets[1], 5: th.Brackets[2],
		7: th.Brackets[2], 8: th.Brackets[1], 9: th.Brackets[0]}
	for col, c := range want {
		if got := fgOfStyle(cellStyle(t, e, 0, col)); got != c {
			t.Errorf("col %d: colour %v, want %v", col, got, c)
		}
	}
	// brackets in strings and comments are left alone and don't nest
	if got := fgOfStyle(cellStyle(t, e, 1, 5)); got == th.BracketError || got == th.Brackets[0] {
		t.Errorf("bracket in a string coloured: %v", got)
	}
	// unmatched closing brackets on later lines get the error colour
	if got := fgOfStyle(cellStyle(t, e, 2, 0)); got != th.BracketError {
		t.Errorf("unmatched ) on line 3: %v, want error colour", got)
	}
	// editing updates the depths: open a bracket on line 1, line 3 now closes it
	typeKeys(e, "ggI(<Esc>G")
	if got := fgOfStyle(cellStyle(t, e, 2, 0)); got != th.Brackets[0] {
		t.Errorf("after the edit, ) on line 3: %v, want depth 0 colour", got)
	}
	// toggled off again
	typeKeys(e, "<Space>tp")
	if got := fgOfStyle(cellStyle(t, e, 0, 1)); got == th.Brackets[0] && plain != th.Brackets[0] {
		t.Error("still colored after turning it off")
	}
}

// colorbrackets can be set in the config, also per filetype
func TestColorBracketsConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("[filetype.python]\ncolorbrackets = true\n"), 0o644)
	cfg, err := config.Load(path)
	if err != nil || len(cfg.Warnings) > 0 {
		t.Fatalf("%v %v", err, cfg.Warnings)
	}
	if !cfg.ForFiletype("python").ColorBrackets || cfg.ForFiletype("go").ColorBrackets {
		t.Fatal("per-filetype colorbrackets not applied")
	}
}

// review fixes ---------------------------------------------------------

// typing a : command keeps the pair highlighted, as before
func TestMatchParenCmdline(t *testing.T) {
	e := newTestEditor(t, "f(a)")
	typeKeys(e, "0f(:")
	if e.mode != ModeCmdline || !isMatchHL(e, cellStyle(t, e, 0, 3)) {
		t.Fatalf("cmdline mode %v: pair not highlighted", e.mode)
	}
}

// a string or comment the theme leaves uncoloured keeps the cursorline
func TestLiteralKeepsCursorLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.py")
	os.WriteFile(path, []byte("x = 1  # comment\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":theme average<CR>:e "+path+"<CR>:set cursorline<CR>")
	want := bgOfStyle(e.theme.CursorLine)
	if got := bgOfStyle(cellStyle(t, e, 0, 10)); got != want {
		t.Fatalf("comment background %v, want the cursorline %v", got, want)
	}
}

// numbers are not strings, preprocessor lines are not comments
func TestColorBracketsLiteralKinds(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct{ name, text string }{
		{"c.yaml", "b: {c: (d)}\nx: [1]\n"},
		{"c.c", "#define SQ(a) ((a)*(a))\nint x[1];\n"},
	} {
		path := filepath.Join(dir, c.name)
		os.WriteFile(path, []byte(c.text), 0o644)
		e := newTestEditor(t, "")
		typeKeys(e, ":e "+path+"<CR>:set cb<CR>G")
		col := strings.IndexAny(c.text[strings.Index(c.text, "\n")+1:], "[")
		if got := fgOfStyle(cellStyle(t, e, 1, col)); got != e.theme.Brackets[0] {
			t.Errorf("%s: line 2 bracket colour %v, want depth 0", c.name, got)
		}
		if c.name == "c.c" {
			if got := fgOfStyle(cellStyle(t, e, 0, 10)); got != e.theme.Brackets[0] {
				t.Errorf("#define: ( not coloured: %v", got)
			}
		}
	}
}

// a new filetype (new highlighter) rebuilds the depths
func TestColorBracketsFiletypeChange(t *testing.T) {
	e := newTestEditor(t, "# (\nfoo)")
	typeKeys(e, ":set cb<CR>G")
	cellStyle(t, e, 1, 3)
	typeKeys(e, ":set ft=python<CR>:set cb<CR>") // options follow the new filetype
	if got := fgOfStyle(cellStyle(t, e, 1, 3)); got != e.theme.BracketError {
		t.Fatalf("after :set ft=python the ) is %v, want the error colour", got)
	}
}

// edits invalidate only from the edited line; the result matches a
// fresh computation
func TestColorBracketsIncremental(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		sb.WriteString("f(a[i], {b: (c)})\n")
	}
	e := newTestEditor(t, sb.String())
	typeKeys(e, ":set cb<CR>G")
	cellStyle(t, e, e.win.cur.Line, 0)
	bd := e.brackets[e.win.buf.ID]
	if bd == nil || bd.valid < 1990 {
		t.Fatal("depths not computed")
	}
	typeKeys(e, "1500GI(<Esc>")
	if bd := e.brackets[e.win.buf.ID]; bd.valid < 1500 {
		t.Fatalf("an edit on line 1500 invalidated from line %d", bd.valid)
	}
	typeKeys(e, "G")
	cellStyle(t, e, e.win.cur.Line, 0)
	got := append([]int(nil), e.brackets[e.win.buf.ID].depth...)
	delete(e.brackets, e.win.buf.ID)
	cellStyle(t, e, e.win.cur.Line, 0)
	want := e.brackets[e.win.buf.ID].depth
	for i := range want[:len(want)-1] {
		if got[i] != want[i] {
			t.Fatalf("line %d: incremental depth %d, fresh %d", i, got[i], want[i])
		}
	}
	if got[1600] != 1 {
		t.Fatalf("the unclosed ( on line 1500 should raise later depths, got %d", got[1600])
	}
}

// in a notebook each cell starts at depth 0
func TestColorBracketsNotebookCells(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.ipynb")
	os.WriteFile(path, []byte(`{"cells":[{"cell_type":"markdown","id":"m","metadata":{},"source":["Note (see below"]},{"cell_type":"code","id":"c","metadata":{},"execution_count":null,"outputs":[],"source":["f(x)"]}],"metadata":{},"nbformat":4,"nbformat_minor":5}`), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>:set cb<CR>")
	b := e.win.buf
	line := -1
	for l := 0; l < b.LineCount(); l++ {
		if b.LineString(l) == "f(x)" {
			line = l
		}
	}
	if line < 0 {
		t.Fatalf("no code line in\n%s", b.Text())
	}
	e.win.cur = Pos{Line: 0}
	if got := e.depthAt(b, e.highlighter(b), line, e.cellHeaders(b), nil); got != 0 {
		t.Fatalf("code cell starts at depth %d, want 0", got)
	}
}

// in every theme the nesting colours differ from the error colour
func TestBracketColorsAllThemes(t *testing.T) {
	for _, name := range syntax.ThemeNames() {
		th := syntax.LoadTheme(name, false)
		if len(th.Brackets) < 3 {
			t.Errorf("%s: %d bracket colours", name, len(th.Brackets))
		}
		for i, c := range th.Brackets {
			if c == th.BracketError {
				t.Errorf("%s: level %d has the error colour", name, i)
			}
		}
	}
}
