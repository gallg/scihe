package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"

	"scihe/internal/config"
)

// Regression tests for issues found in the code audit.

func TestAuditEditing(t *testing.T) {
	cases := []vimCase{
		// ex / search / text objects
		{":s non-ASCII replacement", "abc", ":s/b/é/<CR>", "aéc", nil},
		{":s escaped non-ASCII", "abc", `:s/b/\é/<CR>`, "aéc", nil},
		{":s tilde previous replacement", "abc", ":s/b/X/<CR>:s/c/~/<CR>", "aXX", nil},
		{":s empty pattern without search", "abc", ":s//x/<CR>", "abc", nil},
		{":g escaped delimiter", "a/b\nc", `:g/a\/b/d<CR>`, "c", nil},
		{":g sets search pattern", "foo bar\nbaz", ":g/foo/s//X/<CR>", "X bar\nbaz", nil},
		{":g with relative delete", "x\nx\ny\nz", ":g/x/+1d<CR>", "x\ny\nz", nil},
		{":g undo is one step", "x\nx\nx\ny", ":g/x/d<CR>u", "x\nx\nx\ny", nil},
		{":normal undo is one step", "a\nb", ":%norm Ax<CR>u", "a\nb", nil},
		{":m-2 moves up", "a\nb\nc", "j:m-2<CR>", "b\na\nc", nil},
		{":m .+1", "a\nb\nc", ":m .+1<CR>", "b\na\nc", nil},
		{":%m0 is a no-op", "a\nb", ":%m0<CR>", "a\nb", nil},
		{":r!cmd", "a", ":r!echo hi<CR>", "a\nhi", nil},
		{":0put", "a\nb", "jyy:0put<CR>", "b\na\nb", nil},
		{":0r!cmd", "a", ":0r !echo hi<CR>", "hi\na", nil},
		{"filter to empty deletes lines", "a\nb\nc", ":2!true<CR>", "a\nc", nil},
		{":N past end goes to last line", "a\nb", ":100<CR>dd", "a", nil},
		{"search trailing delimiter", "foo bar", "/bar/<CR>D", "foo ", nil},
		{"2i( directly nested", "((a))", "fad2i(", "()", nil},
		{"star on non-ASCII word", "café x café", "*D", "café x ", nil},
		{"C-x hex stays unsigned", "0x0", "<C-x>", "0x0", nil},
		// core editing
		{"C-o inside counted insert", "abc", "3ix<C-o>ly<Esc>", "xaybc", nil},
		{"C-r in dot repeat", "foo\n", "yiwjA<C-r>0<Esc>k.", "foofoo\nfoo", nil},
		{"C-r in counted insert", "ab", "yl3a<C-r>\"<Esc>ixyz<Esc>", "aaaxyzab", nil},
		{"macro stops at failing motion", "a\nb\nc", "qaA;<Esc>jq10@a", "a;\nb;\nc;", nil},
		{"recursive macro ends", "a\nb\nc", "qaqqaA;<Esc>j@aq@a", "a;\nb;\nc;", nil},
		{"visual d is dot-repeatable", "abcdefgh", "vld.", "efgh", nil},
		{"visual repeat replaces older dot", "abcdef", "xvld.", "f", nil},
		{"V> dot", "a\nb", "V>.", "        a\nb", nil},
		{"d/ search dot", "x foo y foo z foo", "d/foo<CR>.", "foo z foo", nil},
		{"dot count replaces operator count", "a b c d e f g h i j", "d3w2.", "f g h i j", nil},
		{"failed operator keeps dot", "abc abc", "xdfz.", "c abc", nil},
		{"block width with CJK", "日本語\nabcdef", "l<C-v>jd", "日語\nabef", nil},
		{"block on empty line", "abc\n\nxyz", "<C-v>jjd", "bc\n\nyz", nil},
		{"block with tab (tabstop 4)", "\tab\nxxxxxxxxab", "<C-v>jd", "ab\nxxxxab", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newTestEditor(t, c.text)
			typeKeys(e, c.keys)
			if got := curText(e); got != c.want {
				t.Errorf("keys %q on %q:\n got %q\nwant %q (msg %q)", c.keys, c.text, got, c.want, e.msg)
			}
			if e.mode != ModeNormal {
				t.Errorf("mode %v after test", e.mode)
			}
		})
	}
}

func TestAuditCursorPositions(t *testing.T) {
	cases := []struct {
		name, text, keys string
		want             Pos
	}{
		{"block I cursor", "abcd\nabcd", "l<C-v>jIX<Esc>", Pos{0, 1}},
		{"block r cursor", "abcd\nabcd", "ll<C-v>jrX", Pos{0, 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newTestEditor(t, c.text)
			typeKeys(e, c.keys)
			if e.win.cur != c.want {
				t.Errorf("cursor %v want %v", e.win.cur, c.want)
			}
		})
	}
}

func TestAuditInsertMappingRetry(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	s.Init()
	s.SetSize(80, 24)
	defer s.Fini()
	cfg := config.Default()
	cfg.Keys = map[string]map[string]string{"insert": {"jk": "<Esc>"}}
	e := New(s, cfg)
	e.Open(nil)
	typeKeys(e, "ijjk")
	if curText(e) != "j" || e.mode != ModeNormal {
		t.Fatalf("got %q mode %v", curText(e), e.mode)
	}
}

func TestAuditCompletionTilde(t *testing.T) {
	e := newTestEditor(t, "")
	typeKeys(e, ":e ~<Tab>")
	if e.cmdline == nil || !strings.HasPrefix(string(e.cmdline.text), "e ~/") {
		t.Fatalf("completion: %+v", e.cmdline)
	}
	typeKeys(e, "<Esc>")
}

func TestAuditSearchHistory(t *testing.T) {
	e := newTestEditor(t, "foo bar")
	typeKeys(e, "/bar<CR>?<Up>")
	if e.cmdline == nil || string(e.cmdline.text) != "bar" {
		t.Fatalf("? history: %+v", e.cmdline)
	}
	typeKeys(e, "<Esc>")
}

func TestAuditRepeatLastEx(t *testing.T) {
	e := newTestEditor(t, "a\nb\nc")
	typeKeys(e, ":d<CR>ZZ") // ZZ fails? buffer has no name; must not replace @:
	e.quit = false
	typeKeys(e, "@:")
	if curText(e) != "c" {
		t.Fatalf("@: should repeat :d, got %q", curText(e))
	}
}

func TestAuditScrolling(t *testing.T) {
	e := newTestEditor(t, strings.Repeat("line\n", 100))
	typeKeys(e, "gg<C-e><C-e><C-e>")
	if e.win.top != 3 {
		t.Fatalf("<C-e> top=%d", e.win.top)
	}
	if e.win.cur.Line < e.win.top {
		t.Fatalf("cursor above view: %v top %d", e.win.cur, e.win.top)
	}
	typeKeys(e, "50G<C-y>")
	top := e.win.top
	typeKeys(e, "<C-y><C-y>")
	if e.win.top != top-2 {
		t.Fatalf("<C-y>: top %d -> %d", top, e.win.top)
	}
	// mouse wheel
	e.handleMouse(tcell.NewEventMouse(5, 5, tcell.WheelDown, 0))
	e.draw()
	if e.win.top != top+1 {
		t.Fatalf("wheel: top %d (from %d)", e.win.top, top-2)
	}
}

func TestAuditConfigZeroTabstop(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[filetype.python]\ntabstop = 0\nshiftwidth = 0\n"), 0o644)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if o := cfg.ForFiletype("python"); o.TabStop <= 0 || o.ShiftWidth <= 0 {
		t.Fatalf("invalid widths kept: %+v", o)
	}
}

func TestAuditWideCharAtEdge(t *testing.T) {
	e := newTestEditor(t, strings.Repeat("a", 95)+"中")
	typeKeys(e, "$")
	sim := e.screen.(tcell.SimulationScreen)
	cells, w, _ := sim.GetContents()
	x, y := e.win.curX, e.win.curY
	if r := cells[y*w+x].Runes; len(r) == 0 || r[0] != '中' {
		t.Fatalf("cursor cell %d,%d does not show the wide char: %q", x, y, string(r))
	}
}

func TestAuditAltCtrlDecoding(t *testing.T) {
	e := newTestEditor(t, "abc")
	typeKeys(e, "ix<M-C-u>")
	// <Esc> then <C-u> (half page up) — not "u" (undo)
	if curText(e) != "xabc" || e.mode != ModeNormal {
		t.Fatalf("got %q mode %v", curText(e), e.mode)
	}
}

func TestAuditKernelRestart(t *testing.T) {
	py := testPython(t)
	path := filepath.Join(t.TempDir(), "r.py")
	os.WriteFile(path, []byte("x = 1\nx\n"), 0o644)
	e := newTestEditor(t, "")
	e.cfg.Python = py
	typeKeys(e, ":e "+path+"<CR><Space>rr")
	waitFor(t, e, "first eval", func() bool { _, ok := e.annots[e.win.buf.ID][0]; return ok })
	old := e.sessions["python"]
	typeKeys(e, ":repl restart<CR>")
	if e.sessions["python"] == old || e.sessions["python"] == nil {
		t.Fatal("no new session")
	}
	time.Sleep(300 * time.Millisecond) // let the old kernel's exit event arrive
	e.drainAsync()
	if s := e.sessions["python"]; s == nil || !s.Alive() {
		t.Fatal("new kernel lost after restart")
	}
	typeKeys(e, "j<Space>rr")
	waitFor(t, e, "eval after restart", func() bool {
		a, ok := e.annots[e.win.buf.ID][1]
		return ok && a.err && strings.Contains(a.text, "NameError")
	})
}

func TestAuditNotebookLargeOutput(t *testing.T) {
	py := testPython(t)
	path := filepath.Join(t.TempDir(), "big.ipynb")
	e := newTestEditor(t, "")
	e.cfg.Python = py
	typeKeys(e, ":e "+path+"<CR>jifor i in range(20000): print(i)<Esc><Space>nr")
	ks := e.kernels[e.win.buf.ID]
	start := time.Now()
	waitFor(t, e, "big output", func() bool { return ks.running == nil && lastCellRan(ks) })
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("large output too slow: %v", d)
	}
	lines := ks.displayLines(ks.nb.Cells[0])
	if len(lines) != 20000 || lines[19999].Text != "19999" {
		t.Fatalf("output lines %d, last %+v", len(lines), lines[len(lines)-1])
	}
}

func TestWindowArrowKeys(t *testing.T) {
	for _, prefix := range []string{"<C-w>", "<Space>w"} {
		for _, pair := range [][2]string{{"<Left>", "<Right>"}, {"h", "l"}} {
			e := newTestEditor(t, "one")
			typeKeys(e, ":vsplit<CR>:enew<CR>")
			left := e.win
			typeKeys(e, prefix+pair[1])
			if e.win == left {
				t.Fatalf("%s%s did not move right", prefix, pair[1])
			}
			typeKeys(e, prefix+pair[0])
			if e.win != left {
				t.Fatalf("%s%s did not move left", prefix, pair[0])
			}
		}
		e := newTestEditor(t, "one")
		typeKeys(e, ":split<CR>")
		top := e.win
		typeKeys(e, prefix+"<Down>")
		if e.win == top {
			t.Fatalf("%s<Down> did not move down", prefix)
		}
		typeKeys(e, prefix+"<Up>")
		if e.win != top {
			t.Fatalf("%s<Up> did not move up", prefix)
		}
	}
}

func TestThemeChangeRepaintsAllCells(t *testing.T) {
	e := newTestEditor(t, "def f(x):\n    return x")
	e.win.buf.Filetype = "python"
	e.draw()
	typeKeys(e, "<Space>tt")
	typeKeys(e, "solarized-light<CR>")
	// every cell must carry an explicit style: cells left at StyleDefault
	// are not resent to the terminal when the theme changes
	sim := e.screen.(tcell.SimulationScreen)
	cells, w, h := sim.GetContents()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if cells[y*w+x].Style == tcell.StyleDefault {
				t.Fatalf("cell %d,%d left with the implicit default style", x, y)
			}
		}
	}
}
