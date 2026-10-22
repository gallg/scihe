package repl

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// a server that answers clone and then stops reading must not block Eval
// (it runs on the UI goroutine) nor Close
func TestNREPLStalledServer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen")
	}
	t.Cleanup(func() { ln.Close() })
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		v, err := Decode(bufio.NewReader(conn))
		if err != nil {
			return
		}
		id := v.(map[string]any)["id"].(string)
		Encode(conn, map[string]any{"id": id, "new-session": "s1", "status": []any{"done"}})
		<-release // never reads again
	}()
	c, err := DialNREPL(ln.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	done := make(chan Result, 10)
	if err := c.Eval(strings.Repeat("x", 64<<20), "", func(r Result) {
		if r.Done {
			done <- r
		}
	}); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Eval blocked for %v", d)
	}
	closed := make(chan struct{})
	go func() { c.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked")
	}
	// the pending request is ended, not left hanging
	select {
	case r := <-done:
		if r.Err == "" {
			t.Fatalf("pending eval ended without an error: %+v", r)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("pending eval never ended")
	}
	if c.Alive() {
		t.Fatal("still alive after Close")
	}
}
