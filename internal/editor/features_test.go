package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scihe/internal/notebook"
)

// testPython returns the repo's uv-created venv python (with jupyter and
// matplotlib) if present, else python3 from PATH.
func testPython(t *testing.T) string {
	p, _ := filepath.Abs("../../.venv/bin/python")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("no python")
	}
	return "python3"
}

// waitFor runs the event loop work until cond holds.
func waitFor(t *testing.T, e *Editor, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s (msg %q)", what, e.msg)
		}
		select {
		case <-e.wake:
		case <-time.After(20 * time.Millisecond):
		}
		e.drainAsync()
		e.draw()
	}
}

const nbJSON = `{
 "cells": [
  {"cell_type": "markdown", "id": "md1", "metadata": {}, "source": ["# Analysis"]},
  {"cell_type": "code", "execution_count": null, "id": "c1", "metadata": {}, "outputs": [], "source": ["x = 6 * 7\n", "print('x is', x)\n", "x"]},
  {"cell_type": "code", "execution_count": null, "id": "c2", "metadata": {}, "outputs": [], "source": ["x + 1"]}
 ],
 "metadata": {"kernelspec": {"display_name": "Python 3", "language": "python", "name": "python3"}, "language_info": {"name": "python"}},
 "nbformat": 4,
 "nbformat_minor": 5
}`

func TestNotebookWorkflow(t *testing.T) {
	py := testPython(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "a.ipynb")
	os.WriteFile(path, []byte(nbJSON), 0o644)
	e := newTestEditor(t, "")
	e.cfg.Python = py
	typeKeys(e, ":e "+path+"<CR>")
	b := e.win.buf
	if !strings.HasPrefix(b.LineString(0), "# %% [markdown] id=md1") {
		t.Fatalf("notebook text: %q", b.Text())
	}
	ks := e.kernels[b.ID]
	// header lines are decorated
	if text, _, ok := ks.Header(0); !ok || text != "Markdown" {
		t.Fatalf("header: %q", text)
	}
	// run all cells
	typeKeys(e, ":nbrunall<CR>")
	waitFor(t, e, "cells to run", func() bool {
		c := ks.nb.Cell("c2")
		return c != nil && c.ExecutionCount != nil && ks.running == nil
	})
	c1 := ks.nb.Cell("c1")
	lines := notebook.DisplayLines(c1.Outputs)
	if len(lines) != 2 || lines[0].Text != "x is 42" || lines[1].Text != "42" {
		t.Fatalf("c1 outputs: %+v", lines)
	}
	if got := notebook.DisplayLines(ks.nb.Cell("c2").Outputs); len(got) != 1 || got[0].Text != "43" {
		t.Fatalf("c2 outputs: %+v", got)
	}
	// after the cell's last line: box bottom, the Out box, a blank row
	r, _ := ks.rangeAt(3)
	if rows := ks.Virtual(r.End - 1); len(rows) != 6 || rows[0].kind != vBoxBottom || rows[1].text != "Out [1]" ||
		rows[2].text != "x is 42" || rows[3].text != "42" || rows[4].kind != vBoxBottom {
		t.Fatalf("virtual rows: %+v", rows)
	}
	if !e.modified(b) {
		t.Fatal("new outputs should make the notebook modified")
	}
	// add a cell below the last one, type code, run it with <leader>nr
	typeKeys(e, "G<Space>no")
	typeKeys(e, "y = x * 2<Esc>")
	typeKeys(e, "<Space>nr")
	waitFor(t, e, "new cell", func() bool { return ks.running == nil && len(ks.queue) == 0 && countCells(ks) == 4 && lastCellRan(ks) })
	// an error stops run-all and is reported
	typeKeys(e, "o1/0<Esc><Space>nr")
	waitFor(t, e, "error cell", func() bool { return ks.running == nil && len(ks.queue) == 0 && lastCellRan(ks) })
	last := ks.nb.Cells[len(ks.nb.Cells)-1]
	if dl := notebook.DisplayLines(last.Outputs); len(dl) == 0 || !strings.Contains(dl[len(dl)-1].Text, "ZeroDivisionError") {
		t.Fatalf("error output: %+v", dl)
	}
	// save and validate with nbformat
	typeKeys(e, ":w<CR>")
	if e.modified(b) {
		t.Fatalf("modified after save: %q", e.msg)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(py, ".venv") { // nbformat is only in the test venv
		out, err := exec.Command(py, "-c", "import nbformat,sys; nb=nbformat.read(sys.argv[1], as_version=4); nbformat.validate(nb); print(len(nb.cells))", path).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "4" {
			t.Fatalf("nbformat: %v %s\n%s", err, out, data)
		}
	}
	// reopen: outputs survive
	nb2, err := notebook.Load(path)
	if err != nil || len(nb2.Cell("c1").Outputs) != 2 {
		t.Fatalf("reload: %v %+v", err, nb2)
	}
	// cell commands: toggle type, delete
	typeKeys(e, "gg]c<Space>nm")
	if !strings.Contains(b.LineString(2), "[markdown] id=c1") {
		t.Fatalf("toggle type: %q", b.LineString(2))
	}
	typeKeys(e, "<Space>nm<Space>nd")
	if strings.Contains(b.Text(), "id=c1") {
		t.Fatalf("delete cell failed:\n%s", b.Text())
	}
	typeKeys(e, "u")
	if !strings.Contains(b.Text(), "id=c1") {
		t.Fatal("undo of cell delete failed")
	}
	// export to percent script
	typeKeys(e, ":nbexport<CR>")
	script, err := os.ReadFile(filepath.Join(dir, "a.py"))
	if err != nil || !strings.Contains(string(script), "# %%\nx = 6 * 7") || !strings.Contains(string(script), "# %% [markdown]\n# # Analysis") {
		t.Fatalf("export: %v\n%s", err, script)
	}
	// interrupt a long cell
	typeKeys(e, "Go<Esc>")
	typeKeys(e, "<Space>noimport time<CR>time.sleep(30)<Esc><Space>nr")
	waitFor(t, e, "kernel busy", func() bool { return ks.running != nil && ks.k != nil && ks.k.Busy() })
	time.Sleep(200 * time.Millisecond)
	typeKeys(e, "<Space>ni")
	waitFor(t, e, "interrupt", func() bool { return ks.running == nil })
	last = ks.nb.Cells[len(ks.nb.Cells)-1]
	if dl := notebook.DisplayLines(last.Outputs); len(dl) == 0 || !strings.Contains(dl[len(dl)-1].Text, "KeyboardInterrupt") {
		t.Fatalf("interrupt output: %+v", dl)
	}
}

func countCells(ks *kernelState) int { return len(ks.cellRanges()) }

func lastCellRan(ks *kernelState) bool {
	if len(ks.nb.Cells) == 0 {
		return false
	}
	c := ks.nb.Cells[len(ks.nb.Cells)-1]
	return c.ExecutionCount != nil
}

func TestNewNotebook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.ipynb")
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>")
	typeKeys(e, "jiprint(1)<Esc>:w<CR>")
	nb, err := notebook.Load(path)
	if err != nil || len(nb.Cells) != 1 || nb.Cells[0].Source != "print(1)" {
		t.Fatalf("new notebook: %v %+v (msg %q)", err, nb, e.msg)
	}
	// a bare "# %%" typed by the user becomes a proper cell on save
	typeKeys(e, "Go# %%<CR>y = 2<Esc>:w<CR>")
	nb, _ = notebook.Load(path)
	if len(nb.Cells) != 2 || nb.Cells[1].Source != "y = 2" || nb.Cells[1].ID == "" {
		t.Fatalf("bare marker: %+v", nb.Cells)
	}
	if !strings.HasPrefix(e.win.buf.LineString(2), "# %% [code] id=") {
		t.Fatalf("marker not normalized: %q", e.win.buf.LineString(2))
	}
}

func TestSchemeREPL(t *testing.T) {
	if _, err := exec.LookPath("chez"); err != nil {
		t.Skip("chez not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "m.ss")
	os.WriteFile(path, []byte("(define (sq x)\n  (* x x))\n\n(sq 12)\n(display \"hi\")\n(car '())\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>")
	b := e.win.buf
	if b.Filetype != "scheme" {
		t.Fatalf("filetype %q", b.Filetype)
	}
	// eval the top-level define from inside it
	typeKeys(e, "j$<Space>rt")
	waitFor(t, e, "define", func() bool { return e.sessions["scheme"] != nil })
	log := e.replLog("scheme")
	waitFor(t, e, "log shows define", func() bool { return strings.Contains(log.Text(), "(define (sq x)") })
	// eval (sq 12) with cursor inside the form: innermost form
	typeKeys(e, "4G<Space>rr")
	waitFor(t, e, "result 144", func() bool { a, ok := e.annots[b.ID][3]; return ok && a.text == "=> 144" })
	// output goes to the log
	typeKeys(e, "5G<Space>rr")
	waitFor(t, e, "display output", func() bool { return strings.Contains(log.Text(), "hi") })
	// errors are shown inline
	typeKeys(e, "6G$<Space>rr")
	waitFor(t, e, "error", func() bool { a, ok := e.annots[b.ID][5]; return ok && a.err && strings.Contains(a.text, "car") })
	// operator form: <leader>re + text object
	typeKeys(e, "4G<Space>reaf")
	// visual selection eval
	typeKeys(e, "ggVj<Space>rr")
	// evaluating the buffer works too
	typeKeys(e, "<Space>rb")
	// editing a line drops its annotation
	waitFor(t, e, "buffer eval", func() bool { _, ok := e.annots[b.ID][5]; return ok })
	typeKeys(e, "4GA <Esc>")
	if _, ok := e.annots[b.ID][3]; ok {
		t.Fatal("annotation kept after edit")
	}
	// interrupt an infinite loop, session survives
	typeKeys(e, "Go(let loop () (loop))<Esc><Space>rr")
	time.Sleep(300 * time.Millisecond)
	typeKeys(e, "<Space>ri")
	waitFor(t, e, "interrupted", func() bool { return strings.Contains(log.Text(), "interrupted") })
	typeKeys(e, "o(+ 1 2)<Esc><Space>rr")
	l := b.LineCount() - 1
	waitFor(t, e, "after interrupt", func() bool { a, ok := e.annots[b.ID][l]; return ok && a.text == "=> 3" })
	// K documentation lookup does not crash
	typeKeys(e, "ggwK")
}

func TestPythonScriptEval(t *testing.T) {
	py := testPython(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "s.py")
	os.WriteFile(path, []byte("import math\n\ndef f(x):\n    return x * 2\n\nf(21)\nprint('out')\nmath.sqrt(-1)\n"), 0o644)
	e := newTestEditor(t, "")
	e.cfg.Python = py
	typeKeys(e, ":e "+path+"<CR>")
	b := e.win.buf
	typeKeys(e, "<Space>rr")   // import math (line)
	typeKeys(e, "3G<Space>rr") // def block
	typeKeys(e, "6G<Space>rr") // f(21)
	waitFor(t, e, "f(21)", func() bool { a, ok := e.annots[b.ID][5]; return ok && a.text == "=> 42" })
	typeKeys(e, "7G<Space>rr")
	waitFor(t, e, "print", func() bool { a, ok := e.annots[b.ID][6]; return ok && a.text == "=> out" })
	typeKeys(e, "8G<Space>rr")
	waitFor(t, e, "error", func() bool {
		a, ok := e.annots[b.ID][7]
		return ok && a.err && strings.Contains(a.text, "ValueError")
	})
	if log := e.replLog("python").Text(); !strings.Contains(log, ">>> f(21)") {
		t.Fatalf("log: %s", log)
	}
	if e.kernels[b.ID] != nil || e.sessions["python"] == nil {
		t.Fatal(".py buffers use the shared python session, not a per-buffer kernel")
	}
	// percent cells: <leader>rt evaluates the current cell
	os.WriteFile(path, []byte("# %%\na = 1\nb = 2\n# %%\na + b\n"), 0o644)
	typeKeys(e, ":e!<CR>gg<Space>rt")
	waitFor(t, e, "cell eval", func() bool { a, ok := e.annots[b.ID][2]; return ok && a.text == "=> ✓" })
	typeKeys(e, "G<Space>rt")
	waitFor(t, e, "cell eval 2", func() bool { a, ok := e.annots[b.ID][4]; return ok && a.text == "=> 3" })
}

func TestPythonBlocks(t *testing.T) {
	e := newTestEditor(t, "@dec\ndef f(x):\n    if x:\n        return 1\n    else:\n        return 2\n\ny = f(\n    1)\nz = 3")
	b := e.win.buf
	if s, en := pythonTopBlock(b, 3); s != 0 || en != 5 {
		t.Fatalf("top block: %d-%d", s, en)
	}
	if s, en := pythonLineBlock(b, 2); s != 2 || en != 5 {
		t.Fatalf("if block: %d-%d", s, en)
	}
	if s, en := pythonLineBlock(b, 7); s != 7 || en != 8 {
		t.Fatalf("bracket continuation: %d-%d", s, en)
	}
	if s, en := pythonTopBlock(b, 9); s != 9 || en != 9 {
		t.Fatalf("simple stmt: %d-%d", s, en)
	}
	if got := dedent("    a\n      b\n"); got != "a\n  b\n" {
		t.Fatalf("dedent %q", got)
	}
}

func TestMarkdownRender(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.md")
	os.WriteFile(path, []byte("# Title\n\nEnergy $E = mc^2$ and $\\alpha_i \\le \\frac{1}{2}$.\n\n- a\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>3G<Space>mr")
	md := e.findBuffer(path)
	r := e.renders[e.win.buf.ID]
	if r == nil || r.src != md || len(e.windows()) != 1 {
		t.Fatalf("not rendered in place: %q", e.win.buf.Name)
	}
	if got := r.buf.LineString(2); got != "Energy E = mc² and αᵢ ≤ 1/2." {
		t.Fatalf("rendered %q", got)
	}
	if e.win.cur.Line != 2 {
		t.Fatalf("cursor line %d", e.win.cur.Line)
	}
	// read-only while rendered
	typeKeys(e, "x")
	if !strings.Contains(e.msg, "read-only") || r.buf.LineString(2) != "Energy E = mc² and αᵢ ≤ 1/2." {
		t.Fatalf("rendered view editable? msg %q", e.msg)
	}
	typeKeys(e, "<Space>mb")
	if !strings.Contains(e.msg, "read-only") || md.Modified() {
		t.Fatalf("formatting in the rendered view: msg %q", e.msg)
	}
	// the view follows its source
	md.Insert(Pos{4, 0}, "$x^n$ ")
	e.draw()
	if got := r.buf.LineString(4); got != "xⁿ - a" {
		t.Fatalf("not refreshed: %q", got)
	}
	// the cursor stays in the text when the source shrinks
	typeKeys(e, "G")
	md.SetText("# Title")
	e.draw()
	if e.win.cur.Line != 0 {
		t.Fatalf("cursor past the end: line %d", e.win.cur.Line)
	}
	md.SetText("# Title\n\nEnergy $E = mc^2$ and $\\alpha_i \\le \\frac{1}{2}$.\n\n$x^n$ - a")
	typeKeys(e, "3G")
	// SPC f y and 'autoreload' in the view act on its file
	os.WriteFile(path, []byte("# Disk $y^2$\n"), 0o644)
	typeKeys(e, "<Space>fy")
	if md.LineString(0) != "# Disk $y^2$" || r.buf.LineString(0) != "# Disk y²" {
		t.Fatalf("reload from the view: %q / %q (%q)", md.LineString(0), r.buf.LineString(0), e.msg)
	}
	typeKeys(e, ":set autoreload<CR>")
	if !e.options(md).Autoreload || strings.Contains(e.msg, "not available") {
		t.Fatalf("autoreload from the view: %v %q", e.options(md).Autoreload, e.msg)
	}
	md.SetText("# Title\n\nEnergy $E = mc^2$ and $\\alpha_i \\le \\frac{1}{2}$.\n\n$x^n$ - a")
	typeKeys(e, "3G")
	// off again: the source is back, writable, cursor kept
	typeKeys(e, "<Space>mr")
	if e.win.buf != md || len(e.renders) != 0 || e.bufferAlive(r.buf) || e.win.cur.Line != 2 {
		t.Fatalf("not back to the source: %q line %d", e.win.buf.Name, e.win.cur.Line)
	}
	typeKeys(e, "x")
	if !md.Modified() {
		t.Fatal("source not writable after rendering")
	}
	// only markdown
	typeKeys(e, ":e "+filepath.Join(dir, "a.py")+"<CR><Space>mr")
	if !strings.Contains(e.msg, "not a markdown buffer") {
		t.Fatalf("msg %q", e.msg)
	}
}

func TestExplorer(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "f.txt"), []byte("hello\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+dir+"<CR>")
	b := e.win.buf
	if !strings.HasSuffix(b.LineString(1), "  ../") || !strings.HasSuffix(b.LineString(2), "  sub/") {
		t.Fatalf("listing: %q", b.Text())
	}
	if e.win.cur.Line != 2 || e.win.cur.Col != diredNameCol {
		t.Fatalf("cursor %v", e.win.cur)
	}
	typeKeys(e, "<CR>")
	typeKeys(e, "<CR>")
	if e.win.buf.Text() != "hello" {
		t.Fatalf("open file via explorer: %q", e.win.buf.Text())
	}
	typeKeys(e, "<Space>fe-")
	if !strings.HasSuffix(e.win.buf.Name, filepath.Base(dir)+"/") {
		t.Fatalf("parent dir: %q", e.win.buf.Name)
	}
	// back on the directory we came from
	if l := e.win.buf.LineString(e.win.cur.Line); !strings.HasSuffix(l, "sub/") {
		t.Fatalf("cursor on %q", l)
	}
}

func TestRunPython(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.py")
	os.WriteFile(path, []byte("print('ran ok')\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>:run<CR>")
	out := e.scratch("*run*", 4)
	waitFor(t, e, "run output", func() bool { return strings.Contains(out.Text(), "[done]") })
	if !strings.Contains(out.Text(), "ran ok") {
		t.Fatalf("run output: %q", out.Text())
	}
}

func TestNotebookCellTypeAndFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.ipynb")
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>jiprint(1)<Esc>")
	b := e.win.buf
	ks := e.kernels[b.ID]
	marker := func() string { r, _ := ks.rangeAt(e.win.cur.Line); return b.LineString(r.Header) }
	if !strings.Contains(marker(), "[code]") {
		t.Fatalf("new cell: %q", marker())
	}
	typeKeys(e, ":nbtype markdown<CR>")
	if !strings.Contains(marker(), "[markdown]") {
		t.Fatalf("markdown: %q", marker())
	}
	if text, _, _ := ks.Header(0); text != "Markdown" {
		t.Fatalf("label %q", text)
	}
	typeKeys(e, ":nbtype python<CR>")
	if !strings.Contains(marker(), "[code]") {
		t.Fatalf("python: %q", marker())
	}
	if text, _, _ := ks.Header(0); text != "In [ ]  python" {
		t.Fatalf("label %q", text)
	}
	typeKeys(e, ":nbtype raw<CR>")
	if !strings.Contains(marker(), "[raw]") {
		t.Fatalf("raw: %q", marker())
	}
	typeKeys(e, "<Space>nm")
	if !strings.Contains(marker(), "[markdown]") {
		t.Fatalf("toggle from raw: %q", marker())
	}
	typeKeys(e, ":nbtype sql<CR>")
	if !strings.Contains(e.msg, "unknown cell type") {
		t.Fatalf("msg %q", e.msg)
	}
	// the source is untouched and the text column is shifted by the border
	if b.LineString(1) != "print(1)" {
		t.Fatalf("source %q", b.LineString(1))
	}
	if _, ok := ks.Frame(1); !ok {
		t.Fatal("cell line not framed")
	}
	if e.gutterWidth(e.win) != e.numberWidth(e.win)+2 {
		t.Fatal("no room for the left border")
	}
	// plain buffers have no frame
	typeKeys(e, ":enew<CR>")
	if e.frameWidth(e.win) != 0 {
		t.Fatal("frame on a plain buffer")
	}
}
