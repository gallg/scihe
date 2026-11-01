package clipboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeTools puts wl-copy and a wl-paste running script first in PATH.
func fakeTools(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{"wl-copy": "cat >/dev/null", "wl-paste": script} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WAYLAND_DISPLAY", "test")
	t.Setenv("DISPLAY", "")
}

func TestPaste(t *testing.T) {
	fakeTools(t, "printf 'hello'")
	if s, err := Paste(); err != nil || s != "hello" {
		t.Fatalf("got %q, %v", s, err)
	}
}

func TestPasteErrorKeepsMessage(t *testing.T) {
	fakeTools(t, "echo 'Nothing is copied' >&2; exit 1")
	_, err := Paste()
	if err == nil || err.Error() != "wl-paste: Nothing is copied" {
		t.Fatalf("got %v", err)
	}
}

func TestPasteTimesOut(t *testing.T) {
	fakeTools(t, "exec sleep 5")
	old := pasteTimeout
	pasteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { pasteTimeout = old })
	start := time.Now()
	if _, err := Paste(); err == nil {
		t.Fatal("no error from a hung paste")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("paste took %v", d)
	}
}
