package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scihe/internal/config"
)

// touch rewrites a file with a later modification time.
func touchWrite(t *testing.T, path, text string) {
	t.Helper()
	os.WriteFile(path, []byte(text), 0o644)
	later := time.Now().Add(2 * time.Second)
	os.Chtimes(path, later, later)
}

// checkNow runs the periodic disk check immediately.
func checkNow(e *Editor) {
	e.lastDiskCheck = time.Time{}
	e.checkDisk()
	e.draw()
}

func TestAutoreloadMarkdown(t *testing.T) {
	dir := t.TempDir()
	md := filepath.Join(dir, "n.md")
	os.WriteFile(md, []byte("# One\n\nfirst\nsecond\nthird\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+md+"<CR>4G")
	b := e.win.buf
	// a markdown buffer alone behaves like code: manual reload
	if e.options(b).Autoreload {
		t.Fatal("markdown buffers must not auto-reload by default")
	}
	touchWrite(t, md, "# Zero\n\na\nb\nc\n")
	checkNow(e)
	if b.LineString(0) != "# One" || !strings.Contains(e.msg, "SPC f y reloads") {
		t.Fatalf("manual: %q / %q", b.LineString(0), e.msg)
	}
	typeKeys(e, "<Space>fy")
	// with 'autoreload' it reloads by itself, and a rendered view follows
	touchWrite(t, md, "# Two\n\nfirst\nsecond\nthird\nfourth\n")
	typeKeys(e, ":set autoreload<CR>:mdrender<CR>")
	rb := e.win.buf
	checkNow(e)
	if b.LineString(0) != "# Two" || e.modified(b) {
		t.Fatalf("not reloaded: %q", b.Text())
	}
	if !strings.Contains(e.msg, "reloaded (changed on disk)") {
		t.Fatalf("msg %q", e.msg)
	}
	if e.win.cur.Line != 3 {
		t.Fatalf("cursor moved to %d", e.win.cur.Line)
	}
	if !strings.Contains(rb.Text(), "Two") || strings.Contains(rb.Text(), "One") {
		t.Fatalf("rendered view not updated:\n%s", rb.Text())
	}
	typeKeys(e, ":mdrender<CR>")
	// unsaved changes are never overwritten; reported once per version
	typeKeys(e, "Gx")
	touchWrite(t, md, "# Three\n")
	checkNow(e)
	if b.LineString(0) != "# Two" || !strings.Contains(e.msg, "unsaved changes are kept") {
		t.Fatalf("modified buffer reloaded or no warning: %q / %q", b.LineString(0), e.msg)
	}
	e.msg = ""
	checkNow(e)
	if e.msg != "" {
		t.Fatalf("reported twice: %q", e.msg)
	}
	// 'autoreload' off: manual again
	typeKeys(e, ":e!<CR>:set noautoreload<CR>")
	touchWrite(t, md, "# Four\n")
	checkNow(e)
	if b.LineString(0) != "# Three" || !strings.Contains(e.msg, "SPC f y reloads") {
		t.Fatalf("after 'noautoreload': %q / %q", b.LineString(0), e.msg)
	}
	// a deleted file is reported once and the buffer kept
	os.Remove(md)
	checkNow(e)
	if !strings.Contains(e.msg, "deleted on disk") || b.LineString(0) != "# Three" {
		t.Fatalf("deleted: %q / %q", e.msg, b.LineString(0))
	}
}

func TestAutoreloadOffByDefaultForCode(t *testing.T) {
	dir := t.TempDir()
	py := filepath.Join(dir, "a.py")
	os.WriteFile(py, []byte("x = 1\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+py+"<CR>")
	b := e.win.buf
	touchWrite(t, py, "x = 2\n")
	checkNow(e)
	if b.LineString(0) != "x = 1" || !strings.Contains(e.msg, "changed on disk (SPC f y reloads)") {
		t.Fatalf("code buffer: %q / %q", b.LineString(0), e.msg)
	}
	typeKeys(e, "<Space>fy")
	if b.LineString(0) != "x = 2" {
		t.Fatal("SPC f y did not reload")
	}
	// turned on for this buffer only
	typeKeys(e, "<Space>ta")
	if !e.options(b).Autoreload {
		t.Fatal("SPC t a")
	}
	touchWrite(t, py, "x = 3\n")
	checkNow(e)
	if b.LineString(0) != "x = 3" {
		t.Fatalf("autoreload on: %q", b.Text())
	}
	other := filepath.Join(dir, "b.py")
	os.WriteFile(other, []byte("y\n"), 0o644)
	typeKeys(e, ":e "+other+"<CR>")
	if e.options(e.win.buf).Autoreload {
		t.Fatal("setting leaked to another buffer")
	}
}

func TestAutoreloadConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[options]\nautoreload = true\n[filetype.markdown]\nautoreload = false\n"), 0o644)
	cfg, err := config.Load(p)
	if err != nil || len(cfg.Warnings) != 0 {
		t.Fatalf("%v %v", err, cfg.Warnings)
	}
	if !cfg.ForFiletype("python").Autoreload || cfg.ForFiletype("markdown").Autoreload {
		t.Fatalf("python %v markdown %v", cfg.ForFiletype("python").Autoreload, cfg.ForFiletype("markdown").Autoreload)
	}
	if config.Default().ForFiletype("markdown").Autoreload || config.Default().ForFiletype("go").Autoreload {
		t.Fatal("auto-reload must be off by default")
	}
	e := newTestEditor(t, "a")
	typeKeys(e, ":set ar<CR>")
	if !e.options(e.win.buf).Autoreload {
		t.Fatal(":set ar")
	}
}
