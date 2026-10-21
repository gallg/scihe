// Package config loads the declarative TOML configuration. There is no
// scripting: it only sets the editor's own options, per-filetype
// overrides and key mappings. Mappings cannot launch external programs.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// MaxTabStop bounds tabstop: a huge value makes every redraw slow.
const MaxTabStop = 64

// Options are editor settings that can be changed with :set.
type Options struct {
	TabStop        int  `toml:"tabstop"`
	ShiftWidth     int  `toml:"shiftwidth"`
	ExpandTab      bool `toml:"expandtab"`
	Number         bool `toml:"number"`
	RelativeNumber bool `toml:"relativenumber"`
	CursorLine     bool `toml:"cursorline"`
	ScrollOff      int  `toml:"scrolloff"`
	IgnoreCase     bool `toml:"ignorecase"`
	SmartCase      bool `toml:"smartcase"`
	HLSearch       bool `toml:"hlsearch"`
	IncSearch      bool `toml:"incsearch"`
	AutoIndent     bool `toml:"autoindent"`
	Wrap           bool `toml:"wrap"`      // soft-wrap long lines
	LineBreak      bool `toml:"linebreak"` // wrap after blanks, not mid-word
	// Autoreload re-reads a buffer whose file changed on disk (when it
	// has no unsaved changes); off by default (a markdown buffer with an
	// open preview reloads anyway)
	Autoreload bool `toml:"autoreload"`
}

// FiletypeOptions override Options for one filetype; nil means inherit.
type FiletypeOptions struct {
	TabStop    *int  `toml:"tabstop"`
	ShiftWidth *int  `toml:"shiftwidth"`
	ExpandTab  *bool `toml:"expandtab"`
	Wrap       *bool `toml:"wrap"`
	LineBreak  *bool `toml:"linebreak"`
	Autoreload *bool `toml:"autoreload"`
}

// Config is the whole configuration file.
type Config struct {
	Theme          string                     `toml:"theme"`
	Transparent    bool                       `toml:"transparent"`
	Leader         string                     `toml:"leader"`
	WhichKeyDelay  int                        `toml:"whichkey_delay_ms"`
	Mouse          bool                       `toml:"mouse"`
	Python         string                     `toml:"python"`
	Scheme         string                     `toml:"scheme"`
	ClojureJackIn  string                     `toml:"clojure_jack_in"`
	MaxOutputLines int                        `toml:"notebook_max_output_lines"`
	Options        Options                    `toml:"options"`
	Filetypes      map[string]FiletypeOptions `toml:"filetype"`
	// Keys maps mode ("normal", "visual", "insert") to key -> action.
	// An action starting with ":" runs an ex command, anything else is
	// replayed as keys.
	Keys map[string]map[string]string `toml:"keys"`
	// Repls configures evaluation per language ([repl.NAME] tables); the
	// built-in entries (see DefaultRepls) are always present.
	Repls map[string]Repl `toml:"repl"`

	// Warnings collects problems that did not stop loading (unknown keys,
	// rejected mappings); the editor shows them at startup.
	Warnings []string `toml:"-"`
}

// Repl is one language of the REPL table.
type Repl struct {
	Filetypes []string `toml:"filetypes"` // buffer filetypes it serves
	// Kind: "subprocess" (a REPL program on a pipe, the default), "chez"
	// (Chez Scheme with the built-in driver), "nrepl" (connect to an
	// nREPL server), "kernel" (the built-in Python kernel)
	Kind    string `toml:"kind"`
	Command string `toml:"command"` // program and arguments (no shell)
	// Reader selects how code is cut from the buffer: "scheme" or
	// "clojure" forms, "python" lines / blocks / "# %%" cells
	Reader string `toml:"reader"`
	// Sentinel is the code sent after each evaluation to print the end
	// marker (subprocess kind); {id} is replaced by the request id
	Sentinel    string `toml:"sentinel"`
	PromptRegex string `toml:"prompt_regex"` // prompt text stripped from output lines
}

// ReplKinds and ReplReaders are the accepted values.
var (
	ReplKinds   = []string{"subprocess", "chez", "nrepl", "kernel"}
	ReplReaders = []string{"scheme", "clojure", "python"}
)

// DefaultRepls are the built-in languages. Racket and SBCL entries are
// ready to use when those programs are installed.
func DefaultRepls() map[string]Repl {
	return map[string]Repl{
		"clojure": {Filetypes: []string{"clojure"}, Kind: "nrepl", Reader: "clojure"},
		"scheme":  {Filetypes: []string{"scheme"}, Kind: "chez", Command: "chez", Reader: "scheme"},
		"racket": {Filetypes: []string{"racket"}, Kind: "subprocess", Command: "racket -i", Reader: "scheme",
			Sentinel:    `(begin (display (integer->char 29)) (display "SCIHE {id} done") (newline) (flush-output))`,
			PromptRegex: `^(> )+`},
		"common lisp": {Filetypes: []string{"common lisp"}, Kind: "subprocess", Command: "sbcl --noinform", Reader: "scheme",
			Sentinel:    `(progn (write-char (code-char 29)) (format t "SCIHE {id} done~%") (finish-output) (values))`,
			PromptRegex: `^(\* )+`},
		"python": {Filetypes: []string{"python"}, Kind: "kernel", Reader: "python"},
	}
}

// DefaultSentinel is the end-marker code for a subprocess REPL whose
// entry sets none, by reader.
func DefaultSentinel(reader string) string {
	switch reader {
	case "clojure":
		// (symbol "") prints as an empty line, not "nil", after the marker
		return `(do (print (char 29)) (println "SCIHE {id} done") (flush) (symbol ""))`
	case "python":
		return `print("\x1dSCIHE {id} done", flush=True)`
	}
	return `(begin (display (integer->char 29)) (display "SCIHE {id} done") (newline) (flush-output-port (current-output-port)))`
}

// finishRepls merges the user's [repl.*] tables with the built-in ones,
// applies the old "scheme" key and validates the entries.
func (c *Config) finishRepls(schemeSet bool) {
	defs := DefaultRepls()
	if c.Repls == nil {
		c.Repls = map[string]Repl{}
	}
	userDefined := map[string]bool{}
	for name := range c.Repls {
		userDefined[name] = true
	}
	for name, d := range defs {
		r, ok := c.Repls[name]
		if !ok {
			r = d
		}
		if r.Kind == "" {
			r.Kind = d.Kind
		}
		if r.Reader == "" {
			r.Reader = d.Reader
		}
		if len(r.Filetypes) == 0 {
			r.Filetypes = d.Filetypes
		}
		if r.Command == "" {
			r.Command = d.Command
		}
		if r.Sentinel == "" && r.Kind == d.Kind {
			r.Sentinel = d.Sentinel
		}
		if r.PromptRegex == "" && r.Kind == d.Kind {
			r.PromptRegex = d.PromptRegex
		}
		c.Repls[name] = r
	}
	// the top-level "scheme" key is the command of the scheme entry
	if s := c.Repls["scheme"]; schemeSet && s.Command == defs["scheme"].Command {
		s.Command = c.Scheme
		c.Repls["scheme"] = s
	}
	for name, r := range c.Repls {
		if r.Kind == "" {
			r.Kind = "subprocess"
		}
		if r.Reader == "" {
			r.Reader = "scheme"
		}
		bad := ""
		switch {
		case !contains(ReplKinds, r.Kind):
			bad = fmt.Sprintf("kind %q (use %s)", r.Kind, strings.Join(ReplKinds, ", "))
		case !contains(ReplReaders, r.Reader):
			bad = fmt.Sprintf("reader %q (use %s)", r.Reader, strings.Join(ReplReaders, ", "))
		case len(r.Filetypes) == 0:
			bad = "no filetypes"
		case (r.Kind == "subprocess" || r.Kind == "chez") && len(strings.Fields(r.Command)) == 0:
			bad = "no command"
		}
		if r.PromptRegex != "" {
			if _, err := regexp.Compile(r.PromptRegex); err != nil {
				bad = fmt.Sprintf("prompt_regex: %v", err)
			}
		}
		if bad != "" {
			c.Warnings = append(c.Warnings, fmt.Sprintf("repl.%s: %s; entry ignored", name, bad))
			if d, ok := defs[name]; ok {
				if name == "scheme" && schemeSet {
					d.Command = c.Scheme
				}
				c.Repls[name] = d
			} else {
				delete(c.Repls, name)
			}
			delete(userDefined, name) // now a built-in entry (or none)
			continue
		}
		if r.Kind == "subprocess" && r.Sentinel == "" {
			r.Sentinel = DefaultSentinel(r.Reader)
		}
		c.Repls[name] = r
	}
	// a filetype served by one of the user's entries leaves the built-in
	// ones (e.g. [repl.guile] with filetypes = ["scheme"])
	claimed := map[string]bool{}
	for name := range userDefined {
		for _, ft := range c.Repls[name].Filetypes {
			claimed[ft] = true
		}
	}
	for name, r := range c.Repls {
		if userDefined[name] {
			continue
		}
		var keep []string
		for _, ft := range r.Filetypes {
			if !claimed[ft] {
				keep = append(keep, ft)
			}
		}
		r.Filetypes = keep // may become empty: still startable by name
		c.Repls[name] = r
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Default returns the built-in configuration.
func Default() *Config {
	four, two := 4, 2
	f := false
	return &Config{
		Theme:          "catppuccin-mocha",
		Leader:         "<Space>",
		WhichKeyDelay:  300,
		Mouse:          true,
		Python:         "python3",
		Scheme:         "chez",
		ClojureJackIn:  "clojure -Sdeps '{:deps {nrepl/nrepl {:mvn/version \"1.3.0\"}}}' -M -m nrepl.cmdline",
		MaxOutputLines: 30,
		Options: Options{
			TabStop: 4, ShiftWidth: 4, ExpandTab: true, Number: true,
			CursorLine: true, ScrollOff: 4, IgnoreCase: true, SmartCase: true,
			HLSearch: true, IncSearch: true, AutoIndent: true, LineBreak: true,
		},
		Filetypes: map[string]FiletypeOptions{
			"go":       {TabStop: &four, ShiftWidth: &four, ExpandTab: &f},
			"makefile": {ExpandTab: &f},
			"clojure":  {TabStop: &two, ShiftWidth: &two},
			"scheme":   {TabStop: &two, ShiftWidth: &two},
			"yaml":     {TabStop: &two, ShiftWidth: &two},
			"markdown": {TabStop: &two, ShiftWidth: &two},
		},
		Keys:  map[string]map[string]string{},
		Repls: DefaultRepls(),
	}
}

// Path returns the config file location.
func Path() string {
	if p := os.Getenv("SCIHE_CONFIG"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "scihe", "config.toml")
}

// Load reads the config file over the defaults. A missing file is not an error.
func Load(path string) (*Config, error) {
	c := Default()
	if path == "" {
		return c, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	// decode into a copy of defaults so unspecified keys keep defaults;
	// filetype tables are merged entry by entry.
	defFT := c.Filetypes
	c.Filetypes = nil
	c.Repls = nil // the user's tables, merged with the built-in ones below
	md, err := toml.Decode(string(data), c)
	if err != nil {
		c.Filetypes = defFT
		c.Repls = DefaultRepls()
		return c, err
	}
	c.finishRepls(md.IsDefined("scheme"))
	for _, k := range md.Undecoded() {
		c.Warnings = append(c.Warnings, "unknown key "+k.String())
	}
	// the jack-in command may only start a Clojure nREPL server
	if f := strings.Fields(c.ClojureJackIn); len(f) == 0 || !jackInTools[filepath.Base(f[0])] {
		c.Warnings = append(c.Warnings, fmt.Sprintf("clojure_jack_in must start with one of clojure, clj, lein, bb, boot: %q ignored", c.ClojureJackIn))
		c.ClojureJackIn = Default().ClojureJackIn
	}
	for mode, m := range c.Keys {
		for keys, act := range m {
			if RunsExternal(act) {
				delete(m, keys)
				c.Warnings = append(c.Warnings, fmt.Sprintf("keys.%s %q: mappings cannot run shell commands or external programs", mode, keys))
			}
		}
	}
	for k, v := range c.Filetypes {
		d := defFT[k]
		// zero/negative widths would crash indentation code: ignore them
		if v.TabStop != nil && (*v.TabStop <= 0 || *v.TabStop > MaxTabStop) {
			v.TabStop = nil
		}
		if v.ShiftWidth != nil && *v.ShiftWidth <= 0 {
			v.ShiftWidth = nil
		}
		if v.TabStop != nil {
			d.TabStop = v.TabStop
		}
		if v.ShiftWidth != nil {
			d.ShiftWidth = v.ShiftWidth
		}
		if v.ExpandTab != nil {
			d.ExpandTab = v.ExpandTab
		}
		if v.Wrap != nil {
			d.Wrap = v.Wrap
		}
		if v.LineBreak != nil {
			d.LineBreak = v.LineBreak
		}
		if v.Autoreload != nil {
			d.Autoreload = v.Autoreload
		}
		defFT[k] = d
	}
	c.Filetypes = defFT
	if c.Leader == "" {
		c.Leader = "<Space>"
	}
	if c.Options.TabStop <= 0 {
		c.Options.TabStop = 4
	}
	c.Options.TabStop = min(c.Options.TabStop, MaxTabStop)
	if c.Options.ShiftWidth <= 0 {
		c.Options.ShiftWidth = c.Options.TabStop
	}
	if c.Options.ScrollOff < 0 {
		c.Options.ScrollOff = 0
	}
	if c.WhichKeyDelay < 0 {
		c.WhichKeyDelay = 0
	}
	if c.MaxOutputLines <= 0 {
		c.MaxOutputLines = 30
	}
	return c, nil
}

// ForFiletype returns options with filetype overrides applied.
func (c *Config) ForFiletype(ft string) Options {
	o := c.Options
	if f, ok := c.Filetypes[ft]; ok {
		if f.TabStop != nil {
			o.TabStop = *f.TabStop
		}
		if f.ShiftWidth != nil {
			o.ShiftWidth = *f.ShiftWidth
		}
		if f.ExpandTab != nil {
			o.ExpandTab = *f.ExpandTab
		}
		if f.Wrap != nil {
			o.Wrap = *f.Wrap
		}
		if f.LineBreak != nil {
			o.LineBreak = *f.LineBreak
		}
		if f.Autoreload != nil {
			o.Autoreload = *f.Autoreload
		}
	}
	return o
}

var jackInTools = map[string]bool{"clojure": true, "clj": true, "lein": true, "bb": true, "boot": true}

// RunsExternal reports whether a mapping action would start an external
// program through the shell. Bang forms of editor commands (":w!",
// ":q!", ":bd!", ":set nu!") are not shell commands and are allowed.
func RunsExternal(action string) bool {
	for _, part := range exParts(action) {
		p := strings.TrimSpace(part)
		// strip a range: digits, . $ % ' marks, /pat/, ?pat?, + - ,;
		p = strings.TrimLeft(p, "0123456789.$%,;+- \t")
		for strings.HasPrefix(p, "'") && len(p) >= 2 {
			p = strings.TrimLeft(p[2:], "0123456789.$%,;+- \t")
		}
		if strings.HasPrefix(p, "!") {
			return true
		}
		name := p
		if i := strings.IndexFunc(p, func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') }); i >= 0 {
			name = p[:i]
		}
		rest := strings.TrimSpace(p[len(name):])
		if name != "" && strings.HasPrefix("read", name) && strings.HasPrefix(rest, "!") {
			return true
		}
		// ":w !cmd" pipes to a command (":w!" is a forced write)
		if name != "" && strings.HasPrefix("write", name) && len(p) > len(name) && (p[len(name)] == ' ' || p[len(name)] == '\t') && strings.HasPrefix(rest, "!") {
			return true
		}
		// :g/pat/!cmd and :normal can nest another command
		if name != "" && (strings.HasPrefix("global", name) || strings.HasPrefix("vglobal", name) || name == "v") && len(rest) > 1 {
			sep := rest[0]
			if i := strings.IndexByte(rest[1:], sep); i >= 0 && RunsExternal(":"+rest[i+2:]) {
				return true
			}
		}
	}
	return false
}

// exParts returns the ex command lines a mapping could run: every
// ":" ... "<CR>" segment (an ex action is one segment without <CR>).
func exParts(action string) []string {
	var out []string
	low := strings.ToLower(action)
	for i := 0; i < len(action); i++ {
		if action[i] != ':' {
			continue
		}
		end := strings.Index(low[i:], "<cr>")
		if end < 0 {
			out = append(out, action[i+1:])
			break
		}
		out = append(out, action[i+1:i+end])
		i += end
	}
	return out
}

// SaveSettings rewrites top-level keys of the config file at path in
// place (keeping comments, other keys and tables), adding missing ones
// before the first table. Values are strings or bools. The file and its
// directory are created when missing.
func SaveSettings(path string, values map[string]any) error {
	if path == "" {
		return errors.New("no config file path")
	}
	// write through a symlink (dotfile managers) instead of replacing it
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var lines []string
	if len(data) > 0 {
		lines = strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	} else {
		lines = []string{"# scihe configuration (see :help); written by the editor when", "# the theme or transparency is changed interactively.", ""}
	}
	format := func(v any) string {
		switch v := v.(type) {
		case bool:
			return strconv.FormatBool(v)
		case int:
			return strconv.Itoa(v)
		default:
			return strconv.Quote(fmt.Sprint(v))
		}
	}
	// lines that start a top-level key: not inside a multi-line string or
	// array value, and before the first table
	starts := map[int]bool{}
	firstTable := len(lines)
	inStr, depth := "", 0
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if inStr == "" && depth == 0 {
			if strings.HasPrefix(t, "[") {
				firstTable = i
				break
			}
			starts[i] = true
		}
		inStr, depth = scanTOMLLine(l, inStr, depth)
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		q := regexp.QuoteMeta(k)
		re := regexp.MustCompile(`^\s*(` + q + `|"` + q + `"|'` + q + `')\s*=`)
		line := k + " = " + format(values[k])
		found := false
		for i := 0; i < firstTable; i++ {
			if starts[i] && re.MatchString(lines[i]) {
				lines[i] = line
				found = true
				break
			}
		}
		if !found {
			at := firstTable
			// keep a blank line between the keys and the first table
			for at > 0 && strings.TrimSpace(lines[at-1]) == "" && at < len(lines) {
				at--
			}
			lines = append(lines[:at], append([]string{line}, lines[at:]...)...)
			firstTable++
		}
	}
	out := strings.Join(lines, "\n") + "\n"
	// refuse to write something that no longer parses
	var probe map[string]any
	if _, err := toml.Decode(out, &probe); err != nil {
		return fmt.Errorf("config would become invalid: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.toml")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(out); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if st, err := os.Stat(path); err == nil {
		os.Chmod(tmp.Name(), st.Mode().Perm())
	}
	return os.Rename(tmp.Name(), path)
}

// scanTOMLLine tracks whether a line ends inside a multi-line string
// (inStr is its delimiter) or an array (depth of open brackets), so that
// SaveSettings never edits text inside a value.
func scanTOMLLine(l, inStr string, depth int) (string, int) {
	for i := 0; i < len(l); i++ {
		if inStr != "" {
			if strings.HasPrefix(l[i:], inStr) {
				i += len(inStr) - 1
				inStr = ""
			} else if inStr == `"""` && l[i] == '\\' {
				i++
			}
			continue
		}
		switch c := l[i]; {
		case c == '#':
			return inStr, depth
		case strings.HasPrefix(l[i:], `"""`), strings.HasPrefix(l[i:], "'''"):
			inStr = l[i : i+3]
			i += 2
		case c == '"' || c == '\'':
			// single-line string: skip to its end
			j := i + 1
			for j < len(l) && l[j] != c {
				if c == '"' && l[j] == '\\' {
					j++
				}
				j++
			}
			i = j
		case c == '[' && (depth > 0 || strings.Contains(l[:i], "=")):
			depth++
		case c == ']' && depth > 0:
			depth--
		}
	}
	return inStr, depth
}
