package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"scihe/internal/config"
)

func TestTransparentThemeIsSavedToConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	SetConfigPath(p)
	t.Cleanup(func() { SetConfigPath("") })
	e := newTestEditor(t, "abc")
	if e.theme.Background() == tcell.ColorDefault {
		t.Fatal("default theme should be opaque")
	}
	typeKeys(e, ":set transparent<CR>")
	if !e.theme.Transparent || e.theme.Background() != tcell.ColorDefault {
		t.Fatal("transparent not applied")
	}
	// bars keep an opaque background
	if bgOf(e.theme.Status) == tcell.ColorDefault {
		t.Fatal("status line became transparent")
	}
	typeKeys(e, ":theme nord<CR>")
	c, err := config.Load(p)
	if err != nil || c.Theme != "nord" || !c.Transparent {
		t.Fatalf("saved config: %v %+v", err, c)
	}
	typeKeys(e, "<Space>tT")
	if e.theme.Transparent {
		t.Fatal("toggle did not switch back")
	}
	if c, _ := config.Load(p); c.Transparent {
		t.Fatal("toggle not saved")
	}
}

func TestMappingCannotRunShell(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ran")
	e := newTestEditor(t, "abc")
	e.cfg.Keys = map[string]map[string]string{"normal": {
		// bypasses config.Load's check, like a key replay of ":!" would
		"Q": ":!touch " + out,
		"X": ":normal :!touch " + out + "\r",
	}}
	e.applyUserKeys()
	typeKeys(e, "Q")
	if !strings.Contains(e.msg, "config mapping") {
		t.Fatalf("msg %q", e.msg)
	}
	typeKeys(e, "X")
	if _, err := os.Stat(out); err == nil {
		t.Fatal("mapping ran a shell command")
	}
	// typed by the user it still works
	typeKeys(e, ":!touch "+out+"<CR>")
	if _, err := os.Stat(out); err != nil {
		t.Fatal("interactive :! broken")
	}
}

func TestThemeTypoAndBoolValues(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	SetConfigPath(p)
	t.Cleanup(func() { SetConfigPath("") })
	e := newTestEditor(t, "abc")
	typeKeys(e, ":theme nrod<CR>")
	if !strings.Contains(e.msg, "unknown theme") || e.theme.Name != "catppuccin-mocha" {
		t.Fatalf("msg %q theme %s", e.msg, e.theme.Name)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("typo saved to config")
	}
	typeKeys(e, ":set transparent=true<CR>")
	if !e.theme.Transparent {
		t.Fatal("=true")
	}
	typeKeys(e, ":set transparent=false<CR>")
	if e.theme.Transparent {
		t.Fatal("=false turned it on")
	}
	typeKeys(e, ":set number=maybe<CR>")
	if !strings.Contains(e.msg, "Invalid argument") {
		t.Fatalf("msg %q", e.msg)
	}
}
