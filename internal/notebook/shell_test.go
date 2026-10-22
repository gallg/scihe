package notebook

import (
	"os"
	"strings"
	"testing"
	"time"
)

func streamText(evs []Event) (string, int) {
	var sb strings.Builder
	n := 0
	for _, e := range evs {
		if e.Type == "stream" {
			sb.WriteString(e.Text)
			n++
		}
	}
	return sb.String(), n
}

func TestKernelShellLines(t *testing.T) {
	c := &collector{ch: make(chan Event, 100000)}
	k, err := Start(python(t), t.TempDir(), os.Environ(), func(e Event) { c.ch <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	c.until(t, "ready")

	// ! lines run in the shell, in order with Python output, stderr too
	k.Execute("print('a')\n!echo b; echo c >&2\nfor i in range(2):\n    !echo loop\nprint('d')")
	evs := c.until(t, "done")
	if out, _ := streamText(evs); out != "a\nb\nc\nloop\nloop\nd\n" {
		t.Fatalf("shell lines: %q", out)
	}
	if evs[len(evs)-1].Status != "ok" {
		t.Fatalf("status: %+v", evs[len(evs)-1])
	}

	// python with arguments, through the shell
	k.Execute(`!python3 -c "import sys; print(sys.argv[1:])" x "y z"`)
	if out, _ := streamText(c.until(t, "done")); out != "['x', 'y z']\n" {
		t.Fatalf("python -c: %q", out)
	}

	// a cell that parses is never rewritten: a string line starting with !
	k.Execute("s = '''\n!not a command\n'''\nprint(s.strip())")
	if out, _ := streamText(c.until(t, "done")); out != "!not a command\n" {
		t.Fatalf("string rewritten: %q", out)
	}

	// a real syntax error is still reported, on its own line, as written
	k.Execute("!echo x\ndef (")
	evs = c.until(t, "done")
	var synErr *Event
	for i := range evs {
		if evs[i].Type == "error" {
			synErr = &evs[i]
		}
	}
	if synErr == nil || synErr.Ename != "SyntaxError" || !strings.Contains(synErr.Evalue, "line 2") {
		t.Fatalf("syntax error not reported on line 2: %+v", evs)
	}

	// only lines starting a statement are commands: not != continuations,
	// not lines inside strings; a backslash continues a command
	k.Execute("a, b = 1, 2\n!echo one \\\n  two\nok = (a\n      != b)\ns = '''\n!keep\n'''\nprint(ok, s.strip())")
	if out, _ := streamText(c.until(t, "done")); out != "one two\nTrue !keep\n" {
		t.Fatalf("statement detection: %q", out)
	}

	// a background job does not hold the cell
	start0 := time.Now()
	k.Execute("!sleep 3 &")
	c.until(t, "done")
	if d := time.Since(start0); d > 2*time.Second {
		t.Fatalf("cmd & held the cell for %v", d)
	}

	// the command reads no input: it does not hang
	k.Execute("!cat")
	if evs := c.until(t, "done"); evs[len(evs)-1].Status != "ok" {
		t.Fatalf("cat: %+v", evs)
	}

	// interrupt stops the command and the kernel lives on
	k.Execute("!sleep 30; echo never")
	time.Sleep(500 * time.Millisecond)
	start := time.Now()
	k.Interrupt()
	evs = c.until(t, "done")
	if time.Since(start) > 5*time.Second {
		t.Fatal("interrupt did not stop the shell command")
	}
	if out, _ := streamText(evs); strings.Contains(out, "never") {
		t.Fatalf("command kept running: %q", out)
	}
	k.Execute("print('alive')")
	if out, _ := streamText(c.until(t, "done")); out != "alive\n" {
		t.Fatalf("after interrupt: %q", out)
	}

	// interrupting a command leaves other processes of the kernel alone
	k.Execute("import subprocess, sys\nbg = subprocess.Popen([sys.executable, '-c', 'import time; time.sleep(30)'])")
	c.until(t, "done")
	k.Execute("!sleep 30")
	time.Sleep(500 * time.Millisecond)
	k.Interrupt()
	c.until(t, "done")
	k.Execute("print(bg.poll())\nbg.kill()")
	if out, _ := streamText(c.until(t, "done")); out != "None\n" {
		t.Fatalf("background process hit by the interrupt: %q", out)
	}
}

func TestPartialMarker(t *testing.T) {
	for _, c := range []struct {
		data string
		keep int
	}{
		{"abc", 0},
		{"abc\x1d", 1},
		{"abc\x1dSCIHE-SY", 9},
		{"abc\x1dSCIHE-SYNC 12", 14},
		{"abc\x1dSCIHE-SYNC " + strings.Repeat("x", 40), 0},
	} {
		if got := partialMarker([]byte(c.data)); got != c.keep {
			t.Errorf("partialMarker(%q) = %d, want %d", c.data, got, c.keep)
		}
	}
}

func TestKernelDirectOutputBatched(t *testing.T) {
	c := &collector{ch: make(chan Event, 100000)}
	k, err := Start(python(t), t.TempDir(), os.Environ(), func(e Event) { c.ch <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	c.until(t, "ready")

	// an unbuffered child writing straight to fd 1: every line arrives,
	// before "done", in far fewer events than lines
	k.Execute("import subprocess, sys\nsubprocess.run([sys.executable, '-u', '-c', 'for i in range(20000): print(i)'])\nprint('end')")
	out, n := streamText(c.until(t, "done"))
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 20001 || lines[19999] != "19999" || lines[20000] != "end" {
		t.Fatalf("got %d lines, tail %q", len(lines), lines[max(0, len(lines)-2):])
	}
	if n > 2000 {
		t.Fatalf("%d stream events for 20000 lines: not batched", n)
	}

	// os.system output is delivered before the cell's done event
	for i := 0; i < 50; i++ {
		k.Execute("import os\nos.system('echo direct')")
		if out, _ := streamText(c.until(t, "done")); out != "direct\n" {
			t.Fatalf("run %d: %q", i, out)
		}
	}
}
