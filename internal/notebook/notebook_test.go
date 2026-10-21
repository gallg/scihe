package notebook

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleNB = `{
 "cells": [
  {"cell_type": "markdown", "metadata": {}, "source": ["# Title\n", "text"]},
  {"cell_type": "code", "execution_count": 1, "id": "abc", "metadata": {"tags": ["x"]},
   "outputs": [{"name": "stdout", "output_type": "stream", "text": ["hi\n"]}],
   "source": "print('hi')"}
 ],
 "metadata": {"kernelspec": {"name": "python3", "display_name": "Python 3", "language": "python"}},
 "nbformat": 4,
 "nbformat_minor": 4
}`

func lines(s string) (int, func(int) string) {
	ls := strings.Split(s, "\n")
	return len(ls), func(i int) string { return ls[i] }
}

func TestRoundTrip(t *testing.T) {
	nb, err := Parse([]byte(sampleNB))
	if err != nil {
		t.Fatal(err)
	}
	if len(nb.Cells) != 2 || nb.Cells[0].Source != "# Title\ntext" || nb.Cells[1].ID != "abc" {
		t.Fatalf("parse: %+v", nb.Cells)
	}
	text := nb.ToText()
	n, f := lines(text)
	ranges := nb.Sync(n, f)
	if len(ranges) != 2 || ranges[1].ID != "abc" || ranges[0].Type != "markdown" {
		t.Fatalf("ranges: %+v", ranges)
	}
	if c := nb.Cell("abc"); c == nil || len(c.Outputs) != 1 || c.Source != "print('hi')" {
		t.Fatalf("outputs lost: %+v", c)
	}
	// edit: add a new cell with a bare marker
	text += "\n# %%\nx = 1"
	n, f = lines(text)
	ranges = nb.Sync(n, f)
	if len(ranges) != 3 || ranges[2].ID == "" || nb.Cells[2].Source != "x = 1" {
		t.Fatalf("new cell: %+v", ranges)
	}
	data, err := nb.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	nb2, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if nb2.ToText() != nb.ToText() {
		t.Fatalf("text changed after save/load:\n%s\n---\n%s", nb.ToText(), nb2.ToText())
	}
	if !strings.Contains(string(data), `"tags"`) || !strings.Contains(string(data), `"nbformat_minor": 5`) {
		t.Fatalf("metadata not preserved:\n%s", data)
	}
	validate(t, data)
}

func TestRangesLeadingText(t *testing.T) {
	n, f := lines("import os\n# %% [markdown] id=m1\n# hi")
	r := Ranges(n, f)
	if len(r) != 2 || r[0].Header != -1 || r[0].End != 1 || r[1].ID != "m1" {
		t.Fatalf("%+v", r)
	}
}

func TestDisplayLines(t *testing.T) {
	outs := []map[string]any{
		{"output_type": "stream", "name": "stdout", "text": []any{"a\n", "b\n"}},
		{"output_type": "execute_result", "data": map[string]any{"text/plain": "42"}},
		{"output_type": "error", "traceback": []any{"\x1b[31mTraceback\x1b[0m", "ValueError: x"}},
		{"output_type": "display_data", "data": map[string]any{"image/png": "AAAA", "text/plain": "<Figure>"}},
	}
	got := DisplayLines(outs)
	want := []string{"a", "b", "42", "Traceback", "ValueError: x"}
	for i, w := range want {
		if got[i].Text != w {
			t.Fatalf("line %d: %q want %q", i, got[i].Text, w)
		}
	}
	if got[5].Kind != "image" {
		t.Fatalf("image placeholder missing: %+v", got)
	}
}

// testPython returns an interpreter with nbformat (the repo .venv made
// with uv) or "" if unavailable.
func testPython() string {
	if p := os.Getenv("SCIHE_TEST_PYTHON"); p != "" {
		return p
	}
	p, _ := filepath.Abs("../../.venv/bin/python")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func validate(t *testing.T, data []byte) {
	py := testPython()
	if py == "" {
		t.Log("nbformat validation skipped: no test venv")
		return
	}
	f := filepath.Join(t.TempDir(), "nb.ipynb")
	os.WriteFile(f, data, 0o644)
	out, err := exec.Command(py, "-c", "import nbformat,sys; nbformat.validate(nbformat.read(sys.argv[1], as_version=4)); print('valid')", f).CombinedOutput()
	if err != nil || !strings.Contains(string(out), "valid") {
		t.Fatalf("nbformat validation failed: %v\n%s", err, out)
	}
}

func python(t *testing.T) string {
	if p := testPython(); p != "" {
		return p
	}
	p, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}
	return p
}

type collector struct {
	ch chan Event
}

func (c *collector) until(t *testing.T, typ string) []Event {
	t.Helper()
	var evs []Event
	timeout := time.After(20 * time.Second)
	for {
		select {
		case ev := <-c.ch:
			evs = append(evs, ev)
			if ev.Type == typ {
				return evs
			}
		case <-timeout:
			t.Fatalf("timeout waiting for %s; got %+v", typ, evs)
		}
	}
}

func TestKernel(t *testing.T) {
	c := &collector{ch: make(chan Event, 1000)}
	k, err := Start(python(t), t.TempDir(), os.Environ(), func(e Event) { c.ch <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	c.until(t, "ready")

	k.Execute("x = 21\nprint('side')\nx * 2")
	evs := c.until(t, "done")
	var out, result string
	for _, e := range evs {
		if e.Type == "stream" {
			out += e.Text
		}
		if e.Type == "execute_result" {
			result = e.Data["text/plain"].(string)
		}
	}
	if out != "side\n" || result != "42" {
		t.Fatalf("out=%q result=%q", out, result)
	}

	// state persists, errors are reported with a clean traceback
	k.Execute("y = x + 1\n1/0")
	evs = c.until(t, "done")
	var errEv *Event
	for i := range evs {
		if evs[i].Type == "error" {
			errEv = &evs[i]
		}
	}
	if errEv == nil || errEv.Ename != "ZeroDivisionError" {
		t.Fatalf("expected ZeroDivisionError, got %+v", evs)
	}
	if strings.Contains(strings.Join(errEv.Traceback, "\n"), "_run") {
		t.Fatalf("driver frames leaked into traceback: %v", errEv.Traceback)
	}
	if !strings.Contains(strings.Join(errEv.Traceback, "\n"), "1/0") {
		t.Fatalf("cell source missing from traceback: %v", errEv.Traceback)
	}

	// subprocess output written to fd 1 is captured too
	k.Execute("import os\nos.system('echo from-shell')")
	evs = c.until(t, "done")
	time.Sleep(100 * time.Millisecond)
	all := ""
	for _, e := range evs {
		all += e.Text
	}
drain:
	for {
		select {
		case e := <-c.ch:
			all += e.Text
		default:
			break drain
		}
	}
	if !strings.Contains(all, "from-shell") {
		t.Fatalf("subprocess output missing: %q", all)
	}

	// interrupt a long-running cell
	k.Execute("import time\nwhile True: time.sleep(0.05)")
	time.Sleep(300 * time.Millisecond)
	k.Interrupt()
	evs = c.until(t, "done")
	found := false
	for _, e := range evs {
		if e.Type == "error" && e.Ename == "KeyboardInterrupt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("interrupt not reported: %+v", evs)
	}
	k.Execute("y")
	evs = c.until(t, "done")
	if evs[len(evs)-2].Data["text/plain"] != "22" {
		t.Fatalf("kernel state lost after interrupt: %+v", evs)
	}
}

func TestKernelMatplotlib(t *testing.T) {
	py := testPython()
	if py == "" {
		t.Skip("no test venv with matplotlib")
	}
	c := &collector{ch: make(chan Event, 1000)}
	k, err := Start(py, t.TempDir(), os.Environ(), func(e Event) { c.ch <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	c.until(t, "ready")
	k.Execute("import matplotlib.pyplot as plt\nplt.plot([1,2,3])\nplt.show()")
	evs := c.until(t, "done")
	var img map[string]any
	for _, e := range evs {
		if e.Type == "display_data" {
			img = e.Output()
		}
		if e.Type == "error" {
			t.Fatalf("error: %v", e.Traceback)
		}
	}
	if img == nil {
		t.Fatalf("no figure: %+v", evs)
	}
	nb := New()
	nb.Cells[0].Source = "plot"
	nb.Cells[0].Outputs = []map[string]any{img}
	data, _ := nb.Marshal()
	validate(t, data)
}

func TestKernelRobustness(t *testing.T) {
	c := &collector{ch: make(chan Event, 1000)}
	k, err := Start(python(t), t.TempDir(), os.Environ(), func(e Event) { c.ch <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	c.until(t, "ready")
	// a child process reading stdin must not eat the next request
	k.Execute("import subprocess, sys\nsubprocess.run([sys.executable, '-c', 'import sys; print(repr(sys.stdin.read()))'])")
	c.until(t, "done")
	k.Execute("40 + 2")
	evs := c.until(t, "done")
	found := false
	for _, e := range evs {
		if e.Type == "execute_result" && e.Data["text/plain"] == "42" {
			found = true
		}
	}
	if !found {
		t.Fatalf("request after stdin reader lost: %+v", evs)
	}
	// libraries poking at sys.stdout.encoding / .buffer work
	k.Execute("import sys\nsys.stdout.buffer.write(sys.stdout.encoding.encode())\nprint()")
	evs = c.until(t, "done")
	out := ""
	for _, e := range evs {
		out += e.Text
		if e.Type == "error" {
			t.Fatalf("stdout attrs: %v", e.Traceback)
		}
	}
	if !strings.Contains(out, "utf-8") {
		t.Fatalf("buffer write: %q", out)
	}
	// interrupts between requests are ignored (kernel stays alive)
	for i := 0; i < 20; i++ {
		k.Execute("pass")
		k.cmd.Process.Signal(os.Interrupt)
		c.until(t, "done")
	}
	k.Execute("'alive'")
	evs = c.until(t, "done")
	if evs[len(evs)-2].Data["text/plain"] != "'alive'" {
		t.Fatalf("kernel state after interrupts: %+v", evs)
	}
}

func TestBrokenFigureDoesNotPoison(t *testing.T) {
	py := testPython()
	if py == "" {
		t.Skip("no test venv with matplotlib")
	}
	c := &collector{ch: make(chan Event, 1000)}
	k, err := Start(py, t.TempDir(), os.Environ(), func(e Event) { c.ch <- e })
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	c.until(t, "ready")
	k.Execute("import matplotlib.pyplot as plt\nplt.plot([1])\nplt.title(r'$\\badcmd$')")
	c.until(t, "done")
	k.Execute("print('hello')")
	evs := c.until(t, "done")
	if evs[len(evs)-1].Status != "ok" {
		t.Fatalf("later cell failed: %+v", evs)
	}
}
