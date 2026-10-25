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
	// far more than the socket buffers: a blocking write could never finish
	if err := c.Eval(strings.Repeat("x", 16<<20), "", func(r Result) {
		if r.Done {
			done <- r
		}
	}); err != nil {
		t.Fatal(err)
	}
	// encoding takes a moment (slower under -race); blocking on the
	// socket would take forever
	if d := time.Since(start); d > 3*time.Second {
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

// a server that closes before answering clone fails the dial at once
func TestNREPLCloneEOF(t *testing.T) {
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
		Decode(bufio.NewReader(conn))
		conn.Close()
	}()
	closed := false
	start := time.Now()
	_, err = DialNREPL(ln.Addr().String(), func(error) { closed = true })
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("dial: err %v after %v", err, time.Since(start))
	}
	time.Sleep(100 * time.Millisecond)
	if closed {
		t.Fatal("onClose called for a session never returned")
	}
}

// a server that goes away ends the writer goroutine; Close sends the
// close op even right after a large request
func TestNREPLDisconnectAndCloseOp(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("cannot listen")
	}
	t.Cleanup(func() { ln.Close() })
	gotClose := make(chan bool, 1)
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
				gotClose <- false
				return
			}
			m := v.(map[string]any)
			switch m["op"] {
			case "clone":
				Encode(conn, map[string]any{"id": m["id"], "new-session": "s1", "status": []any{"done"}})
			case "close":
				gotClose <- true
				return
			}
		}
	}()
	c, err := DialNREPL(ln.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Eval(strings.Repeat("x", 1<<20), "", func(Result) {})
	c.Close()
	select {
	case ok := <-gotClose:
		if !ok {
			t.Fatal("close op not delivered")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server never got the close op")
	}
	select {
	case <-c.wdone:
	default:
		t.Fatal("writer not stopped")
	}

	// the server closes the connection: the writer stops by itself
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		v, _ := Decode(bufio.NewReader(conn))
		Encode(conn, map[string]any{"id": v.(map[string]any)["id"], "new-session": "s2", "status": []any{"done"}})
		time.Sleep(100 * time.Millisecond)
		conn.Close()
	}()
	ended := make(chan struct{})
	c2, err := DialNREPL(ln.Addr().String(), func(error) { close(ended) })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("onClose not called")
	}
	select {
	case <-c2.wdone:
	case <-time.After(time.Second):
		t.Fatal("writer still running after the server left")
	}
}
