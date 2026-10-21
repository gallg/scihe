package repl

import (
	"bufio"
	"bytes"
	"net"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFormAt(t *testing.T) {
	src := []rune("(define (sq x) (* x x))\n\n'(1 2) #\\( \"a (str\" ; (comment\n(display (sq 3))")
	cases := []struct {
		off  int
		want string
	}{
		{1, "(define (sq x) (* x x))"},  // on "define"
		{9, "(sq x)"},                   // inside (sq x)
		{18, "(* x x)"},                 // on "x" inside (* x x)
		{23, "(define (sq x) (* x x))"}, // just after the closing paren
		{26, "'(1 2)"},                  // quoted list
		{strings.Index(string(src), "(display") + 10, "(sq 3)"},
	}
	for _, c := range cases {
		f, ok := FormAt(src, c.off, Scheme)
		if !ok || string(src[f.Start:f.End]) != c.want {
			t.Errorf("off %d: got %q want %q", c.off, string(src[f.Start:min(f.End, len(src))]), c.want)
		}
	}
	top, ok := TopLevelAt(src, strings.Index(string(src), "sq 3"), Scheme)
	if !ok || string(src[top.Start:top.End]) != "(display (sq 3))" {
		t.Errorf("top-level: %q", string(src[top.Start:top.End]))
	}
	// the "(" inside the string, char literal and comment must not unbalance
	forms := Parse(src, Scheme)
	tops := 0
	for _, f := range forms {
		if f.Depth == 0 && f.List {
			tops++
		}
	}
	if tops != 3 {
		t.Errorf("expected 3 top-level lists, got %d", tops)
	}
}

func TestClojureForms(t *testing.T) {
	src := []rune("(ns my.app (:require [clojure.string :as str]))\n(defn f [x] #{x \\(} #\"re(\" @(atom 1))")
	if ns := ClojureNS(string(src)); ns != "my.app" {
		t.Fatalf("ns %q", ns)
	}
	top, ok := TopLevelAt(src, len(src)-3, Clojure)
	if !ok || !strings.HasPrefix(string(src[top.Start:top.End]), "(defn") || top.End != len(src) {
		t.Fatalf("top %+v %q", top, string(src[top.Start:top.End]))
	}
	f, _ := FormAt(src, strings.Index(string(src), "atom")+1, Clojure)
	if string(src[f.Start:f.End]) != "@(atom 1)" {
		t.Fatalf("deref form %q", string(src[f.Start:f.End]))
	}
}

func TestBencode(t *testing.T) {
	var buf bytes.Buffer
	in := map[string]any{"op": "eval", "code": "(+ 1 2)", "n": 3, "l": []any{"a", 1}}
	Encode(&buf, in)
	if buf.String() != "d4:code7:(+ 1 2)1:ll1:ai1ee1:ni3e2:op4:evale" {
		t.Fatalf("encoded %q", buf.String())
	}
	v, err := Decode(bufio.NewReader(&buf))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"op": "eval", "code": "(+ 1 2)", "n": int64(3), "l": []any{"a", int64(1)}}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("decoded %#v", v)
	}
}

func TestSchemeStringRoundTrip(t *testing.T) {
	for _, s := range []string{"plain", "a\nb\t\"q\" \\ λ", "\x01ctl", ""} {
		got, err := ParseSchemeString(SchemeString(s))
		if err != nil || got != s {
			t.Errorf("%q -> %q (%v)", s, got, err)
		}
	}
	// format produced by Chez `write`
	if got, _ := ParseSchemeString(`"x\x1;y"`); got != "x\x01y" {
		t.Errorf("hex escape: %q", got)
	}
}

// fakeNREPL implements just enough of the nREPL protocol for the client.
func fakeNREPL(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen")
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		for {
			v, err := Decode(r)
			if err != nil {
				return
			}
			m := v.(map[string]any)
			id := m["id"].(string)
			switch m["op"] {
			case "clone":
				Encode(conn, map[string]any{"id": id, "new-session": "s1", "status": []any{"done"}})
			case "eval":
				if m["session"] != "s1" {
					Encode(conn, map[string]any{"id": id, "err": "no session", "status": []any{"done"}})
					continue
				}
				Encode(conn, map[string]any{"id": id, "out": "hello\n"})
				Encode(conn, map[string]any{"id": id, "value": "3", "ns": m["ns"]})
				Encode(conn, map[string]any{"id": id, "status": []any{"done"}})
			}
		}
	}()
	return ln.Addr().String()
}

func collect(t *testing.T, s Session, code string) Result {
	t.Helper()
	ch := make(chan Result, 100)
	if err := s.Eval(code, "user", func(r Result) { ch <- r }); err != nil {
		t.Fatal(err)
	}
	var all Result
	timeout := time.After(10 * time.Second)
	for {
		select {
		case r := <-ch:
			all.Out += r.Out
			all.Err += r.Err
			if r.Value != "" {
				all.Value += r.Value + ";"
			}
			if r.Done {
				return all
			}
		case <-timeout:
			t.Fatalf("timeout; partial %+v", all)
		}
	}
}

func TestNREPLClient(t *testing.T) {
	addr := fakeNREPL(t)
	c, err := DialNREPL(addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := collect(t, c, "(+ 1 2)")
	if r.Out != "hello\n" || r.Value != "3;" {
		t.Fatalf("%+v", r)
	}
}

func TestChez(t *testing.T) {
	if _, err := exec.LookPath("chez"); err != nil {
		t.Skip("chez not installed")
	}
	c, err := StartChez("chez", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := collect(t, c, "(define (sq x) (* x x)) (display \"hi\\n\") (sq 7) (values 1 \"two\")")
	if r.Out != "hi\n" || r.Value != "49;1;\"two\";" || r.Err != "" {
		t.Fatalf("%+v", r)
	}
	r = collect(t, c, "(car '())")
	if !strings.Contains(r.Err, "car") {
		t.Fatalf("error not reported: %+v", r)
	}
	// state persists across evaluations and errors
	if r = collect(t, c, "(sq 3)"); r.Value != "9;" {
		t.Fatalf("%+v", r)
	}
	// reader error (unbalanced) is reported, not fatal
	if r = collect(t, c, "(+ 1"); r.Err == "" {
		t.Fatalf("expected reader error: %+v", r)
	}
	// interrupt an infinite loop
	ch := make(chan Result, 10)
	c.Eval("(let loop () (loop))", "", func(r Result) { ch <- r })
	time.Sleep(300 * time.Millisecond)
	c.Interrupt()
	var got Result
	timeout := time.After(5 * time.Second)
	for !got.Done {
		select {
		case r := <-ch:
			got.Err += r.Err
			got.Done = r.Done
		case <-timeout:
			t.Fatal("interrupt timed out")
		}
	}
	if !strings.Contains(got.Err, "interrupted") {
		t.Fatalf("interrupt: %+v", got)
	}
	if r = collect(t, c, "(sq 4)"); r.Value != "16;" {
		t.Fatalf("after interrupt: %+v", r)
	}
	if !c.Alive() {
		t.Fatal("session died")
	}
}

func TestClojureNSMetadata(t *testing.T) {
	cases := map[string]string{
		"(ns ^:no-doc my.ns)":                   "my.ns",
		"(ns ^{:doc \"d\"} my.ns (:require x))": "my.ns",
		"(ns-unmap *ns* 'foo)\n(ns real.one)":   "real.one",
		"(in-ns 'user.two)":                     "user.two",
	}
	for src, want := range cases {
		if got := ClojureNS(src); got != want {
			t.Errorf("%q: got %q want %q", src, got, want)
		}
	}
}

func TestSchemeReaderPrefixes(t *testing.T) {
	src := []rune("#;(display 1) (display 2) #u8(1 2 3)")
	f, ok := FormAt(src, strings.Index(string(src), "(display 2)")+1, Scheme)
	if !ok || string(src[f.Start:f.End]) != "(display 2)" {
		t.Fatalf("after datum comment: %q", string(src[f.Start:f.End]))
	}
	f, ok = FormAt(src, len(src)-3, Scheme)
	if !ok || string(src[f.Start:f.End]) != "#u8(1 2 3)" {
		t.Fatalf("bytevector: %q", string(src[f.Start:f.End]))
	}
}

func TestChezConsoleOutputWithoutNewline(t *testing.T) {
	if _, err := exec.LookPath("chez"); err != nil {
		t.Skip("chez not installed")
	}
	c, err := StartChez("chez", t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := collect(t, c, `(display "x" (console-output-port)) 5`)
	if !strings.Contains(r.Out, "x") || r.Value != "5;" {
		t.Fatalf("%+v", r)
	}
}
