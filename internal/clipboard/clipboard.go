// Package clipboard talks to the system clipboard through the usual
// command-line helpers (wl-copy/wl-paste, xclip, xsel, pbcopy).
package clipboard

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

type tool struct {
	copy, paste []string
}

func pick() (tool, bool) {
	var cands []tool
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		cands = append(cands, tool{[]string{"wl-copy"}, []string{"wl-paste", "--no-newline"}})
	}
	if os.Getenv("DISPLAY") != "" {
		cands = append(cands,
			tool{[]string{"xclip", "-selection", "clipboard"}, []string{"xclip", "-selection", "clipboard", "-o"}},
			tool{[]string{"xsel", "--clipboard", "--input"}, []string{"xsel", "--clipboard", "--output"}})
	}
	cands = append(cands, tool{[]string{"pbcopy"}, []string{"pbpaste"}})
	for _, c := range cands {
		if _, err := exec.LookPath(c.copy[0]); err == nil {
			return c, true
		}
	}
	return tool{}, false
}

// ErrUnavailable means no clipboard helper was found.
var ErrUnavailable = errors.New("no clipboard tool found")

// Copy writes text to the system clipboard.
func Copy(text string) error {
	t, ok := pick()
	if !ok {
		return ErrUnavailable
	}
	cmd := exec.Command(t.copy[0], t.copy[1:]...)
	cmd.Stdin = bytes.NewBufferString(text)
	if err := cmd.Start(); err != nil {
		return err
	}
	// wl-copy and xclip fork into the background holding the selection.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		return nil
	}
}

// pasteTimeout bounds a read: a helper waiting on an unresponsive
// selection owner would otherwise freeze the editor (a var for tests).
var pasteTimeout = 2 * time.Second

// Paste reads the system clipboard.
func Paste() (string, error) {
	t, ok := pick()
	if !ok {
		return "", ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), pasteTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, t.paste[0], t.paste[1:]...)
	cmd.WaitDelay = 100 * time.Millisecond // a child holding stdout open
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", errors.New(t.paste[0] + " timed out")
	}
	// the helper's own message ("Nothing is copied") says more than
	// "exit status 1"
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
			return "", errors.New(t.paste[0] + ": " + msg)
		}
	}
	return string(out), err
}
