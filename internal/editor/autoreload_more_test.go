package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// msgFits: the message must be readable on the message line
func msgFits(t *testing.T, msg string) {
	t.Helper()
	if len(msg) > 50 {
		t.Errorf("message too long (%d): %q", len(msg), msg)
	}
}

// Space t a in a buffer that can't reload says so and changes nothing
func TestAutoreloadUnsupportedMessages(t *testing.T) {
	repo := gitRepo(t)
	f := filepath.Join(repo, "a.txt")
	os.WriteFile(f, []byte("one\n"), 0o644)
	git(repo, "add", "a.txt")
	git(repo, "commit", "-q", "-m", "first")
	os.WriteFile(f, []byte("two\n"), 0o644)
	md := filepath.Join(repo, "n.md")
	os.WriteFile(md, []byte("# n\n"), 0o644)

	e := newTestEditor(t, "")
	typeKeys(e, ":e "+f+"<CR>")
	cases := []struct {
		name, open, want string
	}{
		{"git status", ":git<CR>", "not available in git buffers"},
		{"git diff", ":e " + f + "<CR>:git diff<CR>", "not available in git buffers"},
		{"git log", ":git log<CR>", "not available in git buffers"},
		{"explorer", ":Explore " + repo + "<CR>", "not available in the file explorer"},
		{"buffer list", ":ls<CR>", "not available in the buffer list"},
		{"help", ":help<CR>", "not available in output buffers"},
		{"messages", ":messages<CR>", "not available in output buffers"},
		{"log", "", "not available in logs"},
	}
	for _, c := range cases {
		if c.name == "log" {
			e.setBuffer(e.win, e.pythonLog())
		} else {
			typeKeys(e, c.open)
		}
		b := e.win.buf
		before := e.options(b).Autoreload
		typeKeys(e, "<Space>ta")
		if !strings.Contains(e.msg, c.want) {
			t.Errorf("%s (%q): message %q, want %q", c.name, b.Name, e.msg, c.want)
		}
		msgFits(t, e.msg)
		if e.options(b).Autoreload != before {
			t.Errorf("%s: the option changed", c.name)
		}
		// asking for the value still works
		typeKeys(e, ":set autoreload?<CR>")
		if !strings.Contains(e.msg, "autoreload") || strings.Contains(e.msg, "not available") {
			t.Errorf("%s: :set autoreload? said %q", c.name, e.msg)
		}
	}
}

// a buffer without a file accepts it, and it works once saved
func TestAutoreloadNoFileYet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "later.txt")
	e := newTestEditor(t, "")
	typeKeys(e, ":enew<CR>ihello<Esc><Space>ta")
	if !e.options(e.win.buf).Autoreload || !strings.Contains(e.msg, "once saved") {
		t.Fatalf("option %v, msg %q", e.options(e.win.buf).Autoreload, e.msg)
	}
	msgFits(t, e.msg)
	typeKeys(e, ":w "+path+"<CR>")
	touchWrite(t, path, "from disk\n")
	checkNow(e)
	if curText(e) != "from disk" {
		t.Fatalf("not reloaded after saving: %q (msg %q)", curText(e), e.msg)
	}
}

// a table view of a file still sets the option on its file; one of
// :groupby (no file) says it can't
func TestAutoreloadTables(t *testing.T) {
	e, tv, _ := openTable(t)
	typeKeys(e, "<Space>ta")
	if !e.options(tv.src).Autoreload {
		t.Fatalf("table of a file: source option not set (msg %q)", e.msg)
	}
	typeKeys(e, ":groupby region<CR>")
	if e.tableFor(e.win.buf) == nil {
		t.Skipf("no groupby table (msg %q)", e.msg)
	}
	typeKeys(e, "<Space>ta")
	if !strings.Contains(e.msg, "not available in this table") {
		t.Fatalf("groupby table: %q", e.msg)
	}
}

// notebooks: a change on disk is reported, and with autoreload on the
// notebook reloads (not while modified)
func TestAutoreloadNotebook(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "r.ipynb")
	nb := func(src string) string {
		return `{"cells":[{"cell_type":"code","id":"a","metadata":{},"execution_count":null,"outputs":[],"source":["` + src + `"]}],"metadata":{},"nbformat":4,"nbformat_minor":5}`
	}
	os.WriteFile(path, []byte(nb("x = 1")), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>G")
	b := e.win.buf
	touchWrite(t, path, nb("x = 2"))
	checkNow(e)
	if !strings.Contains(e.msg, "changed on disk") || strings.Contains(b.Text(), "x = 2") {
		t.Fatalf("change not reported: %q / %q", e.msg, b.Text())
	}
	typeKeys(e, "<Space>ta")
	if !e.options(b).Autoreload {
		t.Fatalf("option not set on a notebook: %q", e.msg)
	}
	touchWrite(t, path, nb("x = 3"))
	checkNow(e)
	if !strings.Contains(b.Text(), "x = 3") {
		t.Fatalf("notebook not reloaded: %q (msg %q)", b.Text(), e.msg)
	}
	if e.win.cur.Line >= b.LineCount() {
		t.Fatal("cursor beyond the reloaded notebook")
	}
	// unsaved changes are kept
	typeKeys(e, "ggOy = 0<Esc>")
	touchWrite(t, path, nb("x = 4"))
	checkNow(e)
	if strings.Contains(b.Text(), "x = 4") || !strings.Contains(e.msg, "unsaved changes are kept") {
		t.Fatalf("modified notebook reloaded: %q (msg %q)", b.Text(), e.msg)
	}
}

// turning auto-reload off is never an error
func TestAutoreloadOffAnywhere(t *testing.T) {
	e := newTestEditor(t, "")
	typeKeys(e, ":messages<CR>:set noautoreload<CR>")
	if strings.Contains(e.msg, "not available") {
		t.Fatalf(":set noautoreload: %q", e.msg)
	}
}

// a reloaded notebook shows the outputs saved in the file
func TestAutoreloadNotebookOutputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "o.ipynb")
	nb := func(out string) string {
		return `{"cells":[{"cell_type":"code","id":"a","metadata":{},"execution_count":1,"outputs":[{"output_type":"stream","name":"stdout","text":["` + out + `\n"]}],"source":["print(1)"]}],"metadata":{},"nbformat":4,"nbformat_minor":5}`
	}
	os.WriteFile(path, []byte(nb("OLDOUT")), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>:set ar<CR>")
	if !strings.Contains(strings.Join(screenLines(e), "\n"), "OLDOUT") {
		t.Fatal("setup: old output not shown")
	}
	touchWrite(t, path, nb("NEWOUT"))
	checkNow(e)
	screen := strings.Join(screenLines(e), "\n")
	if !strings.Contains(screen, "NEWOUT") || strings.Contains(screen, "OLDOUT") {
		t.Fatalf("after reload the screen shows:\n%s", screen)
	}
}
