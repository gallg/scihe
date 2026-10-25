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

// SPC f Y reloads from disk even with unsaved changes; u brings them back.
func TestForceReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(path, []byte("disk\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>ccmine<Esc>")
	typeKeys(e, "<Space>fy")
	if curText(e) != "mine" || !strings.Contains(e.msg, "E37") {
		t.Fatalf("SPC f y must refuse: %q / %q", curText(e), e.msg)
	}
	typeKeys(e, "<Space>fY")
	if curText(e) != "disk" || e.modified(e.win.buf) {
		t.Fatalf("SPC f Y: %q modified=%v", curText(e), e.modified(e.win.buf))
	}
	typeKeys(e, "u")
	if curText(e) != "mine" {
		t.Fatalf("undo after reload: %q", curText(e))
	}
}

func TestDeleteAllBuffers(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		p := filepath.Join(dir, n)
		os.WriteFile(p, []byte(n+"\n"), 0o644)
		paths = append(paths, p)
	}
	e := newTestEditor(t, "")
	for _, p := range paths {
		typeKeys(e, ":e "+p+"<CR>")
	}
	typeKeys(e, "ccchanged<Esc>:split<CR>:messages<CR>")
	// :bda keeps the modified buffer and says so
	typeKeys(e, ":bda<CR>")
	if !strings.Contains(e.msg, "kept with unsaved changes: c.txt") {
		t.Fatalf(":bda message %q", e.msg)
	}
	var names []string
	for _, b := range e.buffers {
		names = append(names, b.Name)
	}
	if len(e.buffers) != 1 || e.buffers[0].Name != "c.txt" {
		t.Fatalf("after :bda: %v", names)
	}
	for _, w := range e.windows() {
		if !e.bufferAlive(w.buf) {
			t.Fatalf("window shows deleted buffer %q", w.buf.Name)
		}
	}
	// SPC b A discards it too and leaves one empty buffer
	typeKeys(e, "<Space>bA")
	if len(e.buffers) != 1 || e.buffers[0].Name != "[No Name]" || e.modified(e.buffers[0]) {
		t.Fatalf("after SPC b A: %d buffers, %q", len(e.buffers), e.buffers[0].Name)
	}
	if !strings.Contains(e.msg, "buffer(s) deleted") {
		t.Fatalf("SPC b A message %q", e.msg)
	}
}

// staged table edits count as unsaved work for :bda
func TestDeleteAllKeepsStagedTable(t *testing.T) {
	e, tv, _ := openEditTable(t, editCSV, "s.csv")
	typeKeys(e, "e2Gdd")
	if !tv.pending() {
		t.Fatal("no staged edit")
	}
	typeKeys(e, ":bda<CR>")
	if !e.bufferAlive(tv.buf) || !e.bufferAlive(tv.src) || !tv.pending() {
		t.Fatalf("staged table deleted: %q", e.msg)
	}
	typeKeys(e, ":bda!<CR>")
	if e.bufferAlive(tv.buf) || e.bufferAlive(tv.src) || len(e.buffers) != 1 {
		t.Fatalf(":bda! left %d buffers", len(e.buffers))
	}
}

func TestQClosesReadOnlyBuffers(t *testing.T) {
	e := newTestEditor(t, "text")
	src := e.win.buf

	// output buffers are read-only, q closes their window
	typeKeys(e, ":messages<CR>")
	out := e.win.buf
	if out.Name != "*messages*" || !out.ReadOnly {
		t.Fatalf("output buffer %q read-only=%v", out.Name, out.ReadOnly)
	}
	before := out.Text()
	typeKeys(e, "x")
	if out.Text() != before {
		t.Fatal("output buffer edited")
	}
	n := len(e.windows())
	typeKeys(e, "q")
	if len(e.windows()) != n-1 || e.win.buf != src || e.macro.recording {
		t.Fatalf("q in *messages*: windows %d→%d, buf %q, recording %v", n, len(e.windows()), e.win.buf.Name, e.macro.recording)
	}

	// the buffer list (only window): q goes back to the previous buffer
	typeKeys(e, ":ls<CR>q")
	if e.win.buf != src {
		t.Fatalf("q in *buffers*: %q", e.win.buf.Name)
	}

	// q in a file buffer still records a macro
	typeKeys(e, "qa")
	if !e.macro.recording {
		t.Fatal("q no longer records macros in files")
	}
	typeKeys(e, "q")
}

func TestQClosesPreviewAndLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "n.md")
	os.WriteFile(path, []byte("# hi\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>:preview<CR>")
	md := e.win.buf
	if len(e.previews) != 1 {
		t.Fatal("no preview")
	}
	typeKeys(e, "<C-w>w")
	if e.win.buf.Kind != buffer.KindPreview {
		t.Fatalf("not in the preview: %q", e.win.buf.Name)
	}
	typeKeys(e, "q")
	if len(e.previews) != 0 || len(e.windows()) != 1 || e.win.buf != md {
		t.Fatalf("q in preview: previews %d windows %d", len(e.previews), len(e.windows()))
	}

	// a REPL log in the only window: q switches away but keeps the log
	log := e.pythonLog()
	e.setBuffer(e.win, log)
	e.win.alt = nil
	typeKeys(e, "q")
	if e.win.buf == log || !e.bufferAlive(log) {
		t.Fatalf("q in a log: showing %q, alive %v", e.win.buf.Name, e.bufferAlive(log))
	}
}

// a table view keeps its own q (back to the text)
func TestQInTableView(t *testing.T) {
	e, tv, _ := openTable(t)
	typeKeys(e, "q")
	if e.win.buf != tv.src {
		t.Fatalf("q in table: %q", e.win.buf.Name)
	}
}

func TestRunArgsQuoted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "args.py")
	os.WriteFile(path, []byte("import sys\nprint(sys.argv[1:])\n"), 0o644)
	e := newTestEditor(t, "")
	e.cfg.Python = testPython(t)
	typeKeys(e, ":e "+path+`<CR>:run -c "a b" 'c d' e<CR>`)
	out := e.scratch("*run*", buffer.KindRepl)
	waitFor(t, e, "run output", func() bool { return strings.Contains(out.Text(), "[done]") })
	if !strings.Contains(out.Text(), "['-c', 'a b', 'c d', 'e']") {
		t.Fatalf("run output:\n%s", out.Text())
	}
	if !out.ReadOnly {
		t.Fatal("*run* is editable")
	}
}

func TestNbExportCommentsShellLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.ipynb")
	os.WriteFile(path, []byte(`{"cells":[{"cell_type":"code","id":"c1","metadata":{},"execution_count":null,"outputs":[],"source":["!ls\n","for i in x:\n","    !echo hi\n","y = (a\n","     != b)"]}],"metadata":{},"nbformat":4,"nbformat_minor":5}`), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>:nbexport<CR>")
	got, err := os.ReadFile(filepath.Join(dir, "s.py"))
	if err != nil {
		t.Fatalf("%v (msg %q)", err, e.msg)
	}
	want := "# %%\n# !ls\nfor i in x:\n    # !echo hi\ny = (a\n     != b)"
	if !strings.HasPrefix(string(got), want) {
		t.Fatalf("export:\n%s", got)
	}
}

// an output buffer written to a file becomes an ordinary, editable file
func TestWriteOutputBufferEditable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	e := newTestEditor(t, "")
	typeKeys(e, ":messages<CR>:w "+path+"<CR>")
	b := e.win.buf
	if b.Kind != buffer.KindFile || b.ReadOnly {
		t.Fatalf("written output: kind %v read-only %v (msg %q)", b.Kind, b.ReadOnly, e.msg)
	}
	typeKeys(e, "ihello<Esc>")
	if !strings.HasPrefix(b.LineString(0), "hello") {
		t.Fatalf("not editable: %q", b.LineString(0))
	}
}

// :bdall keeps the log of a running job (its output would be lost)
func TestDeleteAllKeepsRunningLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "slow.py")
	os.WriteFile(path, []byte("import time\nprint('start', flush=True)\ntime.sleep(30)\n"), 0o644)
	e := newTestEditor(t, "")
	e.cfg.Python = testPython(t)
	typeKeys(e, ":e "+path+"<CR>:run<CR>")
	out := e.scratch("*run*", buffer.KindRepl)
	waitFor(t, e, "run start", func() bool { return strings.Contains(out.Text(), "start") })
	typeKeys(e, ":bda<CR>")
	if !e.bufferAlive(out) {
		t.Fatal(":bdall deleted the log of a running job")
	}
	typeKeys(e, ":run stop<CR>")
	waitFor(t, e, "run end", func() bool { return strings.Contains(out.Text(), "[") && !e.logInUse(out) })
	typeKeys(e, ":bda<CR>")
	if e.bufferAlive(out) {
		t.Fatal(":bdall kept a finished log")
	}
}

// a user mapping of q wins over closing read-only buffers
func TestUserQMappingKept(t *testing.T) {
	e := newTestEditor(t, "text")
	e.cfg.Keys = map[string]map[string]string{"normal": {"q": ":messages"}}
	e.setupKeymaps()
	e.applyUserKeys()
	typeKeys(e, ":messages<CR>")
	b := e.win.buf
	typeKeys(e, "q")
	if e.win.buf != b || len(e.windows()) != 2 {
		t.Fatalf("user q mapping overridden: %q, %d windows", e.win.buf.Name, len(e.windows()))
	}
}

// audit fixes: :bd and :qa refuse to throw away staged table edits
func TestStagedTableGuards(t *testing.T) {
	e, tv, _ := openEditTable(t, editCSV, "g.csv")
	typeKeys(e, "e2Gdd")
	if !tv.pending() {
		t.Fatal("no staged edit")
	}
	typeKeys(e, ":bd<CR>")
	if !e.bufferAlive(tv.buf) || !strings.Contains(e.msg, "staged") {
		t.Fatalf(":bd on the table: %q", e.msg)
	}
	e.setBuffer(e.win, tv.src)
	typeKeys(e, ":bd<CR>")
	if !e.bufferAlive(tv.src) || !tv.pending() {
		t.Fatalf(":bd on the source: %q", e.msg)
	}
	typeKeys(e, ":qa<CR>")
	if e.quit || !strings.Contains(e.msg, "staged") {
		t.Fatalf(":qa with staged edits: quit=%v %q", e.quit, e.msg)
	}
	typeKeys(e, ":qa!<CR>")
	if !e.quit {
		t.Fatal(":qa! did not quit")
	}
}

// audit fix: deleting a markdown buffer deletes its preview too
func TestDeleteMarkdownDeletesPreview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.md")
	os.WriteFile(path, []byte("# hi\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>:preview<CR>")
	md := e.win.buf
	var pb *buffer.Buffer
	for _, p := range e.previews {
		pb = p.buf
	}
	typeKeys(e, ":bd<CR>")
	if e.bufferAlive(md) || pb == nil || e.bufferAlive(pb) {
		t.Fatalf("source alive %v, preview alive %v", e.bufferAlive(md), pb != nil && e.bufferAlive(pb))
	}
	for _, w := range e.windows() {
		if !e.bufferAlive(w.buf) {
			t.Fatalf("a window shows the deleted %q", w.buf.Name)
		}
	}
}

// audit fixes: :g after joins, :put with a digit register
func TestGlobalJoinAndPutDigit(t *testing.T) {
	e := newTestEditor(t, "x1\nx2\nx3\nx4")
	typeKeys(e, ":g/x/j<CR>")
	if curText(e) != "x1 x2\nx3 x4" {
		t.Fatalf(":g/x/j: %q", curText(e))
	}
	// inserting at the start of a later marked line must not skip it
	e = newTestEditor(t, "a\na\nb")
	typeKeys(e, ":g/a/normal jI-<CR>")
	if curText(e) != "a\n-a\n-b" {
		t.Fatalf(":g/a/normal jI-: %q", curText(e))
	}
	e = newTestEditor(t, "one\ntwo")
	typeKeys(e, "yyjdd:put 0<CR>")
	if curText(e) != "one\none" {
		t.Fatalf(":put 0: %q", curText(e))
	}
}

// delete_to_register = false: deletes keep what p pastes, everywhere
func TestDeleteToRegisterOff(t *testing.T) {
	if !config.Default().DeleteToRegister {
		t.Fatal("delete_to_register must default to true")
	}
	newEd := func(on bool, text string) *Editor {
		e := newTestEditor(t, text)
		e.cfg.DeleteToRegister = on
		e.regs.noDeleteReg = !on
		return e
	}
	unnamed := func(e *Editor) string {
		r, _ := e.regs.Get('"')
		return r.Text
	}
	// yank the first line, delete on the second: p still has the yank
	for _, c := range []struct{ name, keys, after string }{
		{"dd", "yyjdd", "keep\nother"},
		{"D", "yyjD", "keep\n\nother"},
		{"dw", "yyjdw", "keep\n\nother"},
		{"x", "yyjx", "keep\nropme\nother"},
		{"X", "yyj$X", "keep\ndrope\nother"},
		{"cw", "yyjcwnew<Esc>", "keep\nnew\nother"},
		{"s", "yyjsZ<Esc>", "keep\nZropme\nother"},
		{"visual d", "yyjVd", "keep\nother"},
		{"visual p", "yyjVp", "keep\nkeep\nother"},
		{":d", "yyj:d<CR>", "keep\nother"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEd(false, "keep\ndropme\nother")
			typeKeys(e, c.keys)
			if curText(e) != c.after {
				t.Fatalf("text %q, want %q", curText(e), c.after)
			}
			if got := unnamed(e); got != "keep\n" {
				t.Fatalf("unnamed register %q, want the yank", got)
			}
		})
	}
	// a named register still gets the deleted text
	e := newEd(false, "keep\ndropme")
	typeKeys(e, `yyj"add`)
	if r, _ := e.regs.Get('a'); r.Text != "dropme\n" || unnamed(e) != "dropme\n" {
		t.Fatalf(`"add: a=%q unnamed=%q`, r.Text, unnamed(e))
	}
	// the default is vim: the deleted line replaces the yank
	e = newEd(true, "keep\ndropme")
	typeKeys(e, "yyjdd")
	if unnamed(e) != "dropme\n" {
		t.Fatalf("default: unnamed %q", unnamed(e))
	}
}

// Space n d follows delete_to_register too
func TestDeleteToRegisterNotebookCell(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.ipynb")
	os.WriteFile(path, []byte(`{"cells":[{"cell_type":"code","id":"a","metadata":{},"execution_count":null,"outputs":[],"source":["x = 1"]},{"cell_type":"code","id":"b","metadata":{},"execution_count":null,"outputs":[],"source":["y = 2"]}],"metadata":{},"nbformat":4,"nbformat_minor":5}`), 0o644)
	for _, on := range []bool{true, false} {
		e := newTestEditor(t, "")
		e.regs.noDeleteReg = !on
		e.regs.Yank(0, Register{Text: "yanked\n", Type: RegLine})
		typeKeys(e, ":e "+path+"<CR>G<Space>nd")
		r, _ := e.regs.Get('"')
		if gotYank := r.Text == "yanked\n"; gotYank == on {
			t.Fatalf("delete_to_register=%v: unnamed after Space n d = %q (msg %q)", on, r.Text, e.msg)
		}
	}
}

// the option is read from config.toml only (no :set, no toggle)
func TestDeleteToRegisterFromConfigOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(path, []byte("delete_to_register = false\n"), 0o644)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DeleteToRegister || len(cfg.Warnings) > 0 {
		t.Fatalf("loaded %v, warnings %v", cfg.DeleteToRegister, cfg.Warnings)
	}
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Fini)
	e := New(s, cfg)
	t.Cleanup(e.shutdown)
	if !e.regs.noDeleteReg {
		t.Fatal("config not applied to the registers")
	}
	e2 := newTestEditor(t, "x")
	typeKeys(e2, ":set delete_to_register<CR>")
	if !strings.HasPrefix(e2.msg, "E518") && !strings.Contains(e2.msg, "nknown") {
		t.Fatalf(":set accepted the option: %q", e2.msg)
	}
}

// the buffer list keeps its columns once buffer IDs pass 999
func TestBufListWideIDs(t *testing.T) {
	e, _ := bufListSetup(t)
	for buffer.New("x").ID < 1500 {
	}
	path := filepath.Join(t.TempDir(), "wide.txt")
	os.WriteFile(path, []byte("w\n"), 0o644)
	typeKeys(e, ":e "+path+"<CR>:ls<CR>")
	header := e.win.buf.LineString(0)
	if !strings.HasPrefix(header[bufListNameCol:], "name") {
		t.Fatalf("header %q", header)
	}
	for l := 1; l < e.win.buf.LineCount(); l++ {
		line := e.win.buf.Line(l)
		if len(line) <= bufListNameCol || line[bufListNameCol-1] != ' ' || line[bufListNameCol] == ' ' {
			t.Fatalf("row %d misaligned: %q", l, string(line))
		}
	}
	if l := e.win.buf.Line(e.win.cur.Line); !strings.HasPrefix(string(l[e.win.cur.Col:]), "wide.txt") {
		t.Fatalf("cursor not on the name: %q col %d", string(l), e.win.cur.Col)
	}
}

// :w FILE on a listing writes a copy; the listing stays a listing
func TestWriteListingWritesCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "list.txt")
	e := newTestEditor(t, "")
	typeKeys(e, ":ls<CR>")
	b := e.win.buf
	typeKeys(e, ":w "+path+"<CR>")
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "name") {
		t.Fatalf("copy not written: %v %q (msg %q)", err, data, e.msg)
	}
	if b.Kind != buffer.KindScratch || b.Path != "" || !b.ReadOnly || e.buflists[b.ID] == nil {
		t.Fatalf("listing changed: kind %v path %q ro %v", b.Kind, b.Path, b.ReadOnly)
	}
}

// visual p in a read-only buffer changes nothing, the register included
func TestVisualPutReadOnly(t *testing.T) {
	e := newTestEditor(t, "keep")
	typeKeys(e, "yy")
	e.showOutput("*output*", "alpha\nbeta")
	typeKeys(e, "Vp")
	if r, _ := e.regs.Get('"'); r.Text != "keep\n" || e.win.buf.Text() != "alpha\nbeta" {
		t.Fatalf("register %q, text %q", r.Text, e.win.buf.Text())
	}
	if !strings.Contains(e.msg, "E21") || e.mode != ModeNormal {
		t.Fatalf("msg %q mode %v", e.msg, e.mode)
	}
}

// :nbexport leaves ! lines inside triple-quoted strings alone
func TestCommentShellLines(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"!ls", "# !ls"},
		{"x = (a\n  != b)", "x = (a\n  != b)"},
		{"msg = \"\"\"\n!important\n\"\"\"\n!ls", "msg = \"\"\"\n!important\n\"\"\"\n# !ls"},
		{"s = '''a\n!keep'''\n  !echo", "s = '''a\n!keep'''\n  # !echo"},
		{"q = '\"\"\"'  # not a string start\n!ls", "q = '\"\"\"'  # not a string start\n# !ls"},
		{"# \"\"\" in a comment\n!ls", "# \"\"\" in a comment\n# !ls"},
		{"d = \"\"\"one\"\"\"\n!ls", "d = \"\"\"one\"\"\"\n# !ls"},
	} {
		if got := commentShellLines(c.in); got != c.want {
			t.Errorf("commentShellLines(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// an unclosed quote in :run arguments is an apostrophe
func TestSplitArgsApostrophe(t *testing.T) {
	if got := splitArgs("it's a test"); strings.Join(got, "|") != "it's|a|test" {
		t.Fatalf("got %q", got)
	}
	if got := splitArgs(`-c "a b" 'c d'`); strings.Join(got, "|") != "-c|a b|c d" {
		t.Fatalf("got %q", got)
	}
}

// :bdall counts a preview deleted with its markdown source
func TestDeleteAllCountsPreview(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.md")
	os.WriteFile(path, []byte("# c\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>:preview<CR>:bdall<CR>")
	if !strings.Contains(e.msg, "3 buffer(s) deleted") {
		t.Fatalf("msg %q", e.msg)
	}
}
