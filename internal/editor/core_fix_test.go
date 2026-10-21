package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadOnlyCannotChange(t *testing.T) {
	for _, keys := range []string{":t0<CR>", ":><CR>", "<C-a>", "u", "Vrx", "vp", "Vp", "V<C-a>", ":>>3<CR>", "x", "dd", "ix", ":s/1/2/<CR>", ":normal Ax<CR>"} {
		e := newTestEditor(t, "1\nb2\nc3")
		e.regs.Yank(0, Register{Text: "zz"})
		typeKeys(e, "ix<Esc>") // an undo step to undo
		b := e.win.buf
		before := b.Text()
		b.ReadOnly = true
		typeKeys(e, "gg0"+keys)
		if !strings.Contains(e.msg, "read-only") {
			t.Errorf("%q: no read-only message (%q)", keys, e.msg)
		}
		typeKeys(e, "<Esc>")
		if b.Text() != before {
			t.Errorf("%q changed a read-only buffer: %q -> %q", keys, before, b.Text())
		}
	}
}

func TestIncrementOverflow(t *testing.T) {
	e := newTestEditor(t, "x 9223372036854775807 y")
	typeKeys(e, "w<C-a>")
	if e.win.buf.LineString(0) != "x 9223372036854775807 y" || !strings.Contains(e.msg, "out of range") {
		t.Fatalf("%q (msg %q)", e.win.buf.LineString(0), e.msg)
	}
	typeKeys(e, "<C-x>")
	if e.win.buf.LineString(0) != "x 9223372036854775806 y" {
		t.Fatalf("decrement %q", e.win.buf.LineString(0))
	}
}

func TestRangeWriteNeedsBang(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keep.txt")
	os.WriteFile(path, []byte("keep\n"), 0o644)
	e := newTestEditor(t, "a\nb\nc")
	typeKeys(e, ":1,2w "+path+"<CR>")
	if data, _ := os.ReadFile(path); string(data) != "keep\n" || !strings.Contains(e.msg, "E13") {
		t.Fatalf("overwritten: %q (msg %q)", data, e.msg)
	}
	typeKeys(e, ":1,2w! "+path+"<CR>")
	if data, _ := os.ReadFile(path); string(data) != "a\nb\n" {
		t.Fatalf(":w! %q", data)
	}
}

func TestMappingCannotPlantAtColon(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	e := newTestEditor(t, "a")
	e.cfg.Keys = map[string]map[string]string{"normal": {"Q": ":!touch " + marker + "<CR>"}}
	e.applyUserKeys()
	typeKeys(e, "Q@:")
	time.Sleep(50 * time.Millisecond)
	if exists(marker) {
		t.Fatal("@: ran the command a mapping typed")
	}
}

func TestJackInRunsWithoutShell(t *testing.T) {
	bin := t.TempDir()
	marker := filepath.Join(t.TempDir(), "ran")
	os.WriteFile(filepath.Join(bin, "clojure"), []byte("#!/bin/sh\nsleep 0.2\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "deps.edn"), []byte("{}\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.clj"), []byte("(+ 1 2)\n"), 0o644)
	e := newTestEditor(t, "")
	e.cfg.ClojureJackIn = "clojure ; touch " + marker
	typeKeys(e, ":e "+filepath.Join(dir, "a.clj")+"<CR>:repl jack-in<CR>")
	time.Sleep(400 * time.Millisecond)
	if exists(marker) {
		t.Fatal("clojure_jack_in went through a shell")
	}
}

func TestTabstopIsBounded(t *testing.T) {
	e := newTestEditor(t, "\ta")
	typeKeys(e, ":set ts=2000000000<CR>")
	if ts := e.options(e.win.buf).TabStop; ts != 64 {
		t.Fatalf("tabstop %d", ts)
	}
}
