package repl

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

type collected struct {
	out, val, err string
	done          chan struct{}
}

func evalWait(t *testing.T, s Session, code string) collected {
	t.Helper()
	c := collected{done: make(chan struct{})}
	var once bool
	if err := s.Eval(code, "", func(r Result) {
		c.out += r.Out
		c.err += r.Err
		if r.Value != "" {
			c.val = r.Value
		}
		if r.Done && !once {
			once = true
			close(c.done)
		}
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.done:
	case <-time.After(10 * time.Second):
		t.Fatalf("no result for %q (out %q)", code, c.out)
	}
	return c
}

const schemeSentinel = `(begin (display (integer->char 29)) (display "SCIHE {id} done") (newline) (flush-output-port (current-output-port)))`

func TestSubprocessWithChez(t *testing.T) {
	bin, err := exec.LookPath("chez")
	if err != nil {
		t.Skip("no chez")
	}
	s, err := StartSubprocess("chez", []string{bin, "-q"}, t.TempDir(), schemeSentinel, regexp.MustCompile(`^(> )+`), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r := evalWait(t, s, "(+ 1 2)"); r.val != "3" {
		t.Fatalf("value %q (out %q)", r.val, r.out)
	}
	r := evalWait(t, s, `(begin (display "hello") (newline) 42)`)
	if !strings.Contains(r.out, "hello") || r.val != "42" {
		t.Fatalf("out %q value %q", r.out, r.val)
	}
	// state persists; several queued evaluations complete in order
	evalWait(t, s, "(define x 10)")
	a := collected{done: make(chan struct{})}
	b := collected{done: make(chan struct{})}
	s.Eval("(* x 2)", "", func(r Result) {
		if r.Value != "" {
			a.val = r.Value
		}
		if r.Done {
			close(a.done)
		}
	})
	s.Eval("(* x 3)", "", func(r Result) {
		if r.Value != "" {
			b.val = r.Value
		}
		if r.Done {
			close(b.done)
		}
	})
	<-a.done
	<-b.done
	if a.val != "20" || b.val != "30" {
		t.Fatalf("queued: %q %q", a.val, b.val)
	}
}

func TestSubprocessExitAndPrompt(t *testing.T) {
	dir := t.TempDir()
	// a fake REPL: echoes "> value" lines, prints the marker on SENTINEL
	script := filepath.Join(dir, "fakerepl")
	os.WriteFile(script, []byte(`#!/bin/sh
while read -r line; do
  case "$line" in
    SENTINEL*) printf '> \035SCIHE %s done\n' "${line#SENTINEL }" ;;
    quit) exit 3 ;;
    *) printf '> > %s\n' "$line" ;;
  esac
done
`), 0o755)
	closed := make(chan struct{})
	s, err := StartSubprocess("fake", []string{script}, dir, "SENTINEL {id}", regexp.MustCompile(`^(> )+`), func(error) { close(closed) })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if r := evalWait(t, s, "hello"); r.val != "hello" || strings.Contains(r.out, ">") {
		t.Fatalf("prompt not stripped: out %q value %q", r.out, r.val)
	}
	// the process dies: the open request completes with an error
	r := evalWait(t, s, "quit")
	if !strings.Contains(r.err, "exited") {
		t.Fatalf("err %q", r.err)
	}
	<-closed
	if s.Alive() {
		t.Fatal("still alive")
	}
	if err := s.Eval("x", "", func(Result) {}); err == nil {
		t.Fatal("eval on a dead process")
	}
}

func TestSubprocessReapsAndResistsFakeMarkers(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "echo")
	os.WriteFile(script, []byte(`#!/bin/sh
while read -r line; do
  case "$line" in
    SENTINEL*) printf '\035SCIHE %s done\n' "${line#SENTINEL }" ;;
    "") ;;
    *) printf '%s\n' "$line" ;;
  esac
done
`), 0o755)
	s, err := StartSubprocess("echo", []string{script}, dir, "SENTINEL {id}", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// code printing a marker of its own does not end other requests
	r := evalWait(t, s, "\x1dSCIHE 99 done")
	if r.err != "" {
		t.Fatalf("err %q", r.err)
	}
	if r2 := evalWait(t, s, "after"); r2.val != "after" {
		t.Fatalf("second request: %+v", r2)
	}
	pid := s.cmd.Process.Pid
	s.Close()
	for i := 0; i < 100; i++ {
		if data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")); err != nil || !strings.Contains(string(data), ") Z ") {
			return // reaped (or not Linux)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("process left as a zombie")
}

func TestKernelSessionExitBeforeReady(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "python")
	os.WriteFile(fake, []byte("#!/bin/sh\necho 'broken interpreter' >&2\nexit 1\n"), 0o755)
	closed := make(chan struct{})
	s, err := StartKernel(fake, t.TempDir(), os.Environ(), nil, func(error) { close(closed) })
	if err != nil {
		t.Fatal(err)
	}
	r := collected{done: make(chan struct{})}
	s.Eval("1 + 1", "", func(res Result) {
		r.err += res.Err
		if res.Done {
			close(r.done)
		}
	})
	select {
	case <-r.done:
	case <-time.After(10 * time.Second):
		t.Fatal("waiting evaluation never completed")
	}
	if r.err == "" {
		t.Fatal("no error for an evaluation on a kernel that exited")
	}
	<-closed
}
