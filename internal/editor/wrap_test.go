package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"scihe/internal/buffer"
	"scihe/internal/config"
)

func segTexts(line string, width int, lbr bool) []string {
	r := []rune(line)
	var out []string
	for _, s := range segmentsOf(r, width, 4, lbr) {
		out = append(out, string(r[s.start:s.end]))
	}
	return out
}

func TestSegmentsOf(t *testing.T) {
	cases := []struct {
		line  string
		width int
		lbr   bool
		want  string
	}{
		{"", 10, true, ""},
		{"short", 10, true, "short"},
		{"exactly10!", 10, true, "exactly10!"},
		{"the quick brown fox jumps", 10, true, "the quick |brown fox |jumps"},
		{"the quick brown fox jumps", 10, false, "the quick |brown fox |jumps"},
		{"aaaa bbbbbbbbbbbbbbbb cc", 10, true, "aaaa |bbbbbbbbbb|bbbbbb cc"},
		{"abcdefghijklmnop", 5, true, "abcde|fghij|klmno|p"},
		{"aa bb cc dd", 5, false, "aa bb| cc d|d"},
		{"日本語のテキスト", 5, true, "日本|語の|テキ|スト"},
		{"\tx", 3, true, "\t|x"},
	}
	for _, c := range cases {
		if got := strings.Join(segTexts(c.line, c.width, c.lbr), "|"); got != c.want {
			t.Errorf("%q w=%d lbr=%v: got %q want %q", c.line, c.width, c.lbr, got, c.want)
		}
	}
}

func screenLines(e *Editor) []string {
	s := e.screen.(tcell.SimulationScreen)
	cells, w, h := s.GetContents()
	out := make([]string, h)
	for y := 0; y < h; y++ {
		var sb strings.Builder
		for x := 0; x < w; x++ {
			if r := cells[y*w+x].Runes; len(r) > 0 {
				sb.WriteRune(r[0])
			} else {
				sb.WriteRune(' ')
			}
		}
		out[y] = strings.TrimRight(sb.String(), " ")
	}
	return out
}

const prose = "Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua."

func TestWrapToggleAndDraw(t *testing.T) {
	e := newTestEditor(t, prose+"\nsecond line")
	e.screen.(tcell.SimulationScreen).SetSize(40, 12)
	e.draw()
	if lines := screenLines(e); !strings.HasPrefix(lines[1], "  2 second") {
		t.Fatalf("nowrap layout:\n%s", strings.Join(lines, "\n"))
	}
	typeKeys(e, "<Space>tw")
	if !e.options(e.win.buf).Wrap || e.cfg.Options.Wrap {
		t.Fatal("wrap should be buffer-local")
	}
	e.draw()
	lines := screenLines(e)
	// 36 text columns: rows break after blanks, numbers only on the first row
	want := []string{
		"  1 Lorem ipsum dolor sit amet,",
		"    consectetur adipiscing elit, sed do",
		"    eiusmod tempor incididunt ut labore",
		"    et dolore magna aliqua.",
		"  2 second line",
	}
	for i, w := range want {
		if lines[i] != w {
			t.Fatalf("row %d = %q, want %q\n%s", i, lines[i], w, strings.Join(lines, "\n"))
		}
	}
	// cursor on a word of the third row
	typeKeys(e, "0/tempor<CR>")
	e.draw()
	if x, y := e.cursorScreen(e.win); x != 12 || y != 2 {
		t.Fatalf("cursor at %d,%d", x, y)
	}
	// gj / gk move by row, j by line
	typeKeys(e, "gj")
	if e.win.cur.Line != 0 || !strings.HasPrefix(string(e.win.buf.Line(0)[e.win.cur.Col:]), "e magna") { // same column within the row
		t.Fatalf("gj -> %v", e.win.cur)
	}
	typeKeys(e, "gj")
	if e.win.cur.Line != 1 {
		t.Fatalf("gj to next line -> %v", e.win.cur)
	}
	typeKeys(e, "gkgkgkgk")
	if e.win.cur.Line != 0 || segIndex(e.segments(e.win, 0), e.win.cur.Col) != 0 {
		t.Fatalf("gk to the first row -> %v", e.win.cur)
	}
	typeKeys(e, "j")
	if e.win.cur.Line != 1 {
		t.Fatal("j should move by line")
	}
	// another buffer keeps the global default (off)
	typeKeys(e, ":enew<CR>")
	if e.options(e.win.buf).Wrap {
		t.Fatal("wrap leaked to another buffer")
	}
	// zl does nothing while wrapping
	typeKeys(e, ":b1<CR>zl")
	if e.win.left != 0 {
		t.Fatal("horizontal scroll while wrapping")
	}
	typeKeys(e, ":set nowrap<CR>")
	if e.options(e.win.buf).Wrap {
		t.Fatal("nowrap")
	}
}

func TestWrapScrollAndMouse(t *testing.T) {
	var text []string
	for i := 0; i < 20; i++ {
		text = append(text, prose)
	}
	e := newTestEditor(t, strings.Join(text, "\n"))
	e.screen.(tcell.SimulationScreen).SetSize(40, 12)
	typeKeys(e, ":set wrap<CR>")
	// each line takes 4 rows; 11 text rows show 2 whole lines + part
	typeKeys(e, "5G")
	e.draw()
	_, y := e.cursorScreen(e.win)
	if y < 0 || y >= 11 {
		t.Fatalf("cursor row %d (top %d)", y, e.win.top)
	}
	if last := e.lastVisibleLine(e.win); last-e.win.top > 3 {
		t.Fatalf("lastVisibleLine %d with top %d", last, e.win.top)
	}
	// the end of the last line is visible after G$
	typeKeys(e, "G$")
	e.draw()
	if _, y := e.cursorScreen(e.win); y < 0 || y >= 11 {
		t.Fatalf("G$ cursor row %d", y)
	}
	// click on the second row of the top line
	typeKeys(e, "gg")
	e.draw()
	e.handleMouse(tcell.NewEventMouse(4+2, 1, tcell.Button1, 0))
	if e.win.cur.Line != 0 || !strings.HasPrefix(string(e.win.buf.Line(0)[e.win.cur.Col:]), "nsectetur") {
		t.Fatalf("click -> %v", e.win.cur)
	}
}

func TestWrapLineTallerThanWindow(t *testing.T) {
	long := strings.Repeat("word ", 200) // ~28 rows at 36 columns
	e := newTestEditor(t, long+"\nnext")
	e.screen.(tcell.SimulationScreen).SetSize(40, 8)
	typeKeys(e, ":set wrap<CR>$")
	e.draw()
	if _, y := e.cursorScreen(e.win); y < 0 || y >= 7 {
		t.Fatalf("cursor row %d, skip %d", y, e.win.skip)
	}
	if e.win.skip == 0 {
		t.Fatal("expected rows of the top line to be skipped")
	}
	typeKeys(e, "0")
	e.draw()
	if _, y := e.cursorScreen(e.win); y != 0 || e.win.skip != 0 {
		t.Fatalf("back at start: row %d skip %d", y, e.win.skip)
	}
}

func TestWrapFromConfigPerFiletype(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[options]\nlinebreak = false\n[filetype.markdown]\nwrap = true\nlinebreak = true\n"), 0o644)
	cfg, err := config.Load(p)
	if err != nil || len(cfg.Warnings) != 0 {
		t.Fatalf("%v %v", err, cfg.Warnings)
	}
	if o := cfg.ForFiletype("markdown"); !o.Wrap || !o.LineBreak {
		t.Fatalf("markdown %+v", o)
	}
	if o := cfg.ForFiletype("python"); o.Wrap || o.LineBreak {
		t.Fatalf("python %+v", o)
	}
	dir := t.TempDir()
	md := filepath.Join(dir, "a.md")
	py := filepath.Join(dir, "a.py")
	os.WriteFile(md, []byte(prose+"\n"), 0o644)
	os.WriteFile(py, []byte(prose+"\n"), 0o644)
	e := newTestEditor(t, "")
	e.cfg = cfg
	typeKeys(e, ":e "+md+"<CR>")
	if !e.wraps(e.win) {
		t.Fatal("markdown should wrap")
	}
	typeKeys(e, ":e "+py+"<CR>")
	if e.wraps(e.win) {
		t.Fatal("python should not wrap")
	}
	// global default
	g := config.Default()
	g.Options.Wrap = true
	if !g.ForFiletype("go").Wrap || !g.ForFiletype("go").LineBreak {
		t.Fatal("global wrap / default linebreak")
	}
}

func TestWrapNeverInTablesAndListings(t *testing.T) {
	e, tv, _ := openTable(t)
	typeKeys(e, "q:set wrap<CR>:table<CR>")
	if e.tableFor(e.win.buf) != tv || e.wraps(e.win) {
		t.Fatal("table view wraps")
	}
	typeKeys(e, ":e "+t.TempDir()+"<CR>")
	e.options(e.win.buf).Wrap = true
	if e.wraps(e.win) {
		t.Fatal("listing wraps")
	}
}

func TestWrapNotebookFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.ipynb")
	e := newTestEditor(t, "")
	e.screen.(tcell.SimulationScreen).SetSize(40, 12)
	typeKeys(e, ":e "+path+"<CR>ji"+prose+"<Esc>:set wrap<CR>")
	e.draw()
	lines := screenLines(e)
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, "    │ ") && strings.HasSuffix(l, "│") {
			n++ // continuation rows keep both borders
		}
	}
	if n < 3 {
		t.Fatalf("framed continuation rows: %d\n%s", n, strings.Join(lines, "\n"))
	}
}

func TestPreviewIgnoresSourceWrap(t *testing.T) {
	md := filepath.Join(t.TempDir(), "p.md")
	os.WriteFile(md, []byte("# T\n\n| a | long description column |\n|---|---|\n| 1 | "+prose+" |\n\n```\n"+prose+"\n```\n"), 0o644)
	e := newTestEditor(t, "")
	e.screen.(tcell.SimulationScreen).SetSize(80, 40)
	typeKeys(e, ":e "+md+"<CR>:preview<CR>")
	e.draw()
	var pw *Window
	for _, w := range e.windows() {
		if w.buf.Kind == buffer.KindPreview {
			pw = w
		}
	}
	if pw == nil {
		t.Fatal("no preview window")
	}
	region := func() []string {
		var out []string
		for _, l := range screenLines(e) {
			if r := []rune(l); len(r) > pw.x {
				out = append(out, string(r[pw.x:]))
			}
		}
		return out
	}
	before := region()
	typeKeys(e, ":set wrap<CR>")
	e.draw()
	if e.wraps(pw) || !e.options(e.win.buf).Wrap {
		t.Fatal("preview window wraps / source not wrapped")
	}
	if after := region(); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("preview changed when the source wrapped:\n%s\n---\n%s", strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
	// everything fits the preview window: nothing cut at the edge
	for _, l := range strings.Split(pw.buf.Text(), "\n") {
		if w := runeWidthOf(l); w > pw.w {
			t.Fatalf("preview line wider (%d) than its window (%d): %q", w, pw.w, l)
		}
	}
}

func runeWidthOf(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r, w, 8)
	}
	return w
}
