package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"scihe/internal/config"
)

// A [repl.*] entry of kind subprocess: Chez run as a plain REPL serves
// racket buffers (.rkt), exercising the generic session end to end.
func TestConfiguredSubprocessRepl(t *testing.T) {
	if _, err := exec.LookPath("chez"); err != nil {
		t.Skip("no chez")
	}
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte(`[repl.racket]
command = "chez -q"
sentinel = '(begin (display (integer->char 29)) (display "SCIHE {id} done") (newline) (flush-output-port (current-output-port)))'
prompt_regex = '^(> )+'
`), 0o644)
	cfg, err := config.Load(p)
	if err != nil || len(cfg.Warnings) != 0 {
		t.Fatalf("%v %v", err, cfg.Warnings)
	}
	if r := cfg.Repls["racket"]; r.Kind != "subprocess" || r.Reader != "scheme" || len(r.Filetypes) != 1 {
		t.Fatalf("defaults not merged: %+v", r)
	}
	src := filepath.Join(t.TempDir(), "a.rkt")
	os.WriteFile(src, []byte("(define (sq x) (* x x))\n(sq 7)\n(display \"hi\")\n"), 0o644)
	e := newTestEditor(t, "")
	e.cfg = cfg
	typeKeys(e, ":e "+src+"<CR>")
	b := e.win.buf
	if b.Filetype != "racket" {
		t.Skipf("filetype %q", b.Filetype)
	}
	typeKeys(e, "<Space>rt")
	waitFor(t, e, "define", func() bool { a, ok := e.annots[b.ID][0]; return ok && a.text == "=> ✓" })
	typeKeys(e, "2G<Space>rt")
	waitFor(t, e, "value", func() bool { a, ok := e.annots[b.ID][1]; return ok && a.text == "=> 49" })
	typeKeys(e, "3G<Space>rt")
	waitFor(t, e, "output", func() bool { a, ok := e.annots[b.ID][2]; return ok && a.text == "=> hi" })
	if log := e.replLog("racket").Text(); !strings.Contains(log, "racket> (sq 7)") || !strings.Contains(log, "49") {
		t.Fatalf("log:\n%s", log)
	}
	typeKeys(e, ":repl quit<CR>")
	if e.sessions["racket"] != nil {
		t.Fatal("quit")
	}
	// :repl start NAME forces an entry; unknown names list the table
	typeKeys(e, ":repl start nosuch<CR>")
	if !strings.Contains(e.msg, "no [repl.nosuch] entry") || !strings.Contains(e.msg, "racket") {
		t.Fatalf("msg %q", e.msg)
	}
}

func TestReplConfigValidation(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte(`scheme = "petite"
[repl.bad]
filetypes = ["x"]
kind = "telnet"
command = "x"
[repl.empty]
filetypes = ["y"]
[repl.regex]
filetypes = ["z"]
command = "z"
prompt_regex = "("
[repl.guile]
filetypes = ["guile"]
command = "guile -q"
`), 0o644)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 3 {
		t.Fatalf("warnings %q", cfg.Warnings)
	}
	if _, ok := cfg.Repls["bad"]; ok {
		t.Fatal("invalid entry kept")
	}
	if g := cfg.Repls["guile"]; g.Kind != "subprocess" || g.Reader != "scheme" || !strings.Contains(g.Sentinel, "SCIHE {id} done") {
		t.Fatalf("guile defaults: %+v", g)
	}
	if cfg.Repls["scheme"].Command != "petite" {
		t.Fatalf("scheme alias: %q", cfg.Repls["scheme"].Command)
	}
	// a buffer with no entry says what is configured
	e := newTestEditor(t, "x")
	e.cfg = cfg
	e.win.buf.Filetype = "cobol"
	typeKeys(e, "<Space>rr")
	if !strings.Contains(e.msg, "no REPL for filetype") || !strings.Contains(e.msg, "[repl.NAME]") {
		t.Fatalf("msg %q", e.msg)
	}
}
