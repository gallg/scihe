package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scihe/internal/buffer"
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
