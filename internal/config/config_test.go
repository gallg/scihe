package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMerge(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte(`
theme = "nord"
[options]
tabstop = 8
[filetype.python]
shiftwidth = 2
[filetype.go]
tabstop = 2
[keys.normal]
"<leader>w" = ":w"
`), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Theme != "nord" || c.Options.TabStop != 8 {
		t.Fatalf("bad top-level: %+v", c)
	}
	if !c.Options.Number {
		t.Fatal("unset option lost its default")
	}
	if o := c.ForFiletype("go"); o.TabStop != 2 || o.ExpandTab {
		t.Fatalf("go override merge: %+v", o)
	}
	if o := c.ForFiletype("python"); o.ShiftWidth != 2 || o.TabStop != 8 {
		t.Fatalf("python: %+v", o)
	}
	if c.Keys["normal"]["<leader>w"] != ":w" {
		t.Fatal("keys not loaded")
	}
}

func TestMissingFile(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || c.Theme == "" {
		t.Fatal("missing file should give defaults")
	}
}

func TestRunsExternal(t *testing.T) {
	for act, want := range map[string]bool{
		":w":                      false,
		":w!":                     false,
		":q!":                     false,
		":bd!":                    false,
		":set number!":            false,
		"@q":                      false,
		"dd":                      false,
		":!lazygit":               true,
		": !ls":                   true,
		":%!sort":                 true,
		":'<,'>!sort":             true,
		":.,+3!fmt":               true,
		":r !date":                true,
		":read !date":             true,
		":r file.txt":             false,
		":w !sh":                  true,
		":wq!":                    false,
		":g/x/!rm -rf":            true,
		"ggdG:!lazygit<CR>":       true,
		":w<CR>:r !date<CR>":      true,
		":w<CR>:set nu!<CR>":      false,
		"<leader>w:normal dd<CR>": false,
	} {
		if got := RunsExternal(act); got != want {
			t.Errorf("RunsExternal(%q) = %v, want %v", act, got, want)
		}
	}
}

func TestLoadRejectsShellMappingsAndUnknownKeys(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte(`
transparent = true
editor = "lazygit"
[keys.normal]
"<leader>g" = ":!lazygit"
"<leader>w" = ":w"
`), 0o644)
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Transparent {
		t.Fatal("transparent not loaded")
	}
	if _, ok := c.Keys["normal"]["<leader>g"]; ok {
		t.Fatal("shell mapping accepted")
	}
	if c.Keys["normal"]["<leader>w"] != ":w" {
		t.Fatal("plain mapping dropped")
	}
	if len(c.Warnings) != 2 {
		t.Fatalf("warnings: %q", c.Warnings)
	}
}

func TestSaveSettings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "config.toml")
	// created from scratch
	if err := SaveSettings(p, map[string]any{"theme": "nord", "transparent": true}); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil || c.Theme != "nord" || !c.Transparent || len(c.Warnings) != 0 {
		t.Fatalf("new file: %v %+v", err, c)
	}
	// updated in place: comments, other keys and tables survive
	os.WriteFile(p, []byte("# my config\ntheme = \"dracula\" # dark\nmouse = false\n\n[options]\ntabstop = 8\n"), 0o644)
	if err := SaveSettings(p, map[string]any{"theme": "github", "transparent": false}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	want := "# my config\ntheme = \"github\"\nmouse = false\ntransparent = false\n\n[options]\ntabstop = 8\n"
	if string(data) != want {
		t.Fatalf("rewritten file:\n%s\nwant:\n%s", data, want)
	}
	c, _ = Load(p)
	if c.Theme != "github" || c.Mouse || c.Options.TabStop != 8 {
		t.Fatalf("reloaded: %+v", c)
	}
}

func TestSaveSettingsMultilineAndSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.toml")
	link := filepath.Join(dir, "config.toml")
	os.WriteFile(real, []byte("clojure_jack_in = \"\"\"\ntheme = \"keep-me\"\n[not a table]\n\"\"\"\n\"theme\" = \"old\"\nlist = [\n  \"[x]\",\n]\n[options]\n"), 0o644)
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	if err := SaveSettings(link, map[string]any{"theme": "nord"}); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced")
	}
	data, _ := os.ReadFile(real)
	want := "clojure_jack_in = \"\"\"\ntheme = \"keep-me\"\n[not a table]\n\"\"\"\ntheme = \"nord\"\nlist = [\n  \"[x]\",\n]\n[options]\n"
	if string(data) != want {
		t.Fatalf("got:\n%s", data)
	}
}

func TestJackInValidated(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte(`clojure_jack_in = "lazygit"`), 0o644)
	c, _ := Load(p)
	if c.ClojureJackIn != Default().ClojureJackIn || len(c.Warnings) != 1 {
		t.Fatalf("%q %q", c.ClojureJackIn, c.Warnings)
	}
}

func TestUserReplEntriesWinFiletypes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("scheme = \"petite\"\n[repl.guile]\nfiletypes = [\"scheme\"]\ncommand = \"guile -q\"\n[repl.scheme]\nkind = \"bogus\"\n"), 0o644)
	c, _ := Load(p)
	if fts := c.Repls["scheme"].Filetypes; len(fts) != 0 {
		t.Fatalf("built-in scheme keeps %v", fts)
	}
	if c.Repls["scheme"].Command != "petite" {
		t.Fatalf("alias lost after an invalid entry: %q", c.Repls["scheme"].Command)
	}
	if fts := c.Repls["guile"].Filetypes; len(fts) != 1 || fts[0] != "scheme" {
		t.Fatalf("guile %v", fts)
	}
}
