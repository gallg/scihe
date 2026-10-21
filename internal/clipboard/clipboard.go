// Package clipboard talks to the system clipboard through the usual
// command-line helpers (wl-copy/wl-paste, xclip, xsel, pbcopy).
package clipboard

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
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

// Paste reads the system clipboard.
func Paste() (string, error) {
	t, ok := pick()
	if !ok {
		return "", ErrUnavailable
	}
	out, err := exec.Command(t.paste[0], t.paste[1:]...).Output()
	return string(out), err
}
