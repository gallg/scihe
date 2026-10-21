// Package keymap provides vim-style key notation ("<C-w>", "<CR>", "a")
// and a prefix trie of bindings that also feeds the which-key popup.
package keymap

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
)

// Key is a single key in vim notation: "a", "<C-a>", "<Esc>", "<Space>".
type Key string

var specialNames = map[tcell.Key]Key{
	tcell.KeyEnter:      "<CR>",
	tcell.KeyTab:        "<Tab>",
	tcell.KeyBacktab:    "<S-Tab>",
	tcell.KeyEsc:        "<Esc>",
	tcell.KeyBackspace:  "<BS>",
	tcell.KeyBackspace2: "<BS>",
	tcell.KeyDelete:     "<Del>",
	tcell.KeyInsert:     "<Insert>",
	tcell.KeyUp:         "<Up>",
	tcell.KeyDown:       "<Down>",
	tcell.KeyLeft:       "<Left>",
	tcell.KeyRight:      "<Right>",
	tcell.KeyHome:       "<Home>",
	tcell.KeyEnd:        "<End>",
	tcell.KeyPgUp:       "<PageUp>",
	tcell.KeyPgDn:       "<PageDown>",
	tcell.KeyF1:         "<F1>",
	tcell.KeyF2:         "<F2>",
	tcell.KeyF3:         "<F3>",
	tcell.KeyF4:         "<F4>",
	tcell.KeyF5:         "<F5>",
	tcell.KeyF6:         "<F6>",
	tcell.KeyF7:         "<F7>",
	tcell.KeyF8:         "<F8>",
	tcell.KeyF9:         "<F9>",
	tcell.KeyF10:        "<F10>",
	tcell.KeyF11:        "<F11>",
	tcell.KeyF12:        "<F12>",
}

// FromEvent converts a tcell key event into vim notation.
func FromEvent(ev *tcell.EventKey) Key {
	mod := ev.Modifiers()
	k := ev.Key()
	if name, ok := specialNames[k]; ok {
		s := string(name)
		prefix := ""
		if mod&tcell.ModCtrl != 0 && k != tcell.KeyBackspace {
			prefix += "C-"
		}
		if mod&tcell.ModShift != 0 && k != tcell.KeyBacktab {
			prefix += "S-"
		}
		if mod&tcell.ModAlt != 0 {
			prefix += "M-"
		}
		if prefix != "" {
			s = "<" + prefix + s[1:]
		}
		return Key(s)
	}
	if k >= tcell.KeyCtrlA && k <= tcell.KeyCtrlZ {
		r := rune('a' + (k - tcell.KeyCtrlA))
		if mod&tcell.ModAlt != 0 {
			return Key("<C-M-" + string(r) + ">")
		}
		return Key("<C-" + string(r) + ">")
	}
	switch k {
	case tcell.KeyCtrlSpace:
		return "<C-Space>"
	case tcell.KeyCtrlBackslash:
		return "<C-\\>"
	case tcell.KeyCtrlRightSq:
		return "<C-]>"
	case tcell.KeyCtrlCarat:
		return "<C-^>"
	case tcell.KeyCtrlUnderscore:
		return "<C-_>"
	}
	if k == tcell.KeyRune {
		r := ev.Rune()
		base := RuneKey(r)
		// ESC followed quickly by a control key arrives as Alt+Ctrl+letter
		if mod&tcell.ModAlt != 0 && mod&tcell.ModCtrl != 0 {
			switch r {
			case 'm', 'j':
				return "<M-CR>"
			case 'i':
				return "<M-Tab>"
			case 'h':
				return "<M-BS>"
			case '[':
				return "<M-Esc>"
			}
			return Key("<M-C-" + strings.Trim(string(base), "<>") + ">")
		}
		if mod&tcell.ModAlt != 0 {
			return Key("<M-" + strings.Trim(string(base), "<>") + ">")
		}
		if mod&tcell.ModCtrl != 0 {
			return Key("<C-" + strings.Trim(string(base), "<>") + ">")
		}
		return base
	}
	return Key("<" + ev.Name() + ">")
}

// RuneKey returns the key notation for a plain rune.
func RuneKey(r rune) Key {
	switch r {
	case ' ':
		return "<Space>"
	case '<':
		return "<lt>"
	}
	return Key(string(r))
}

// Rune returns the character a key inserts, if it is a printable key.
func (k Key) Rune() (rune, bool) {
	switch k {
	case "<Space>":
		return ' ', true
	case "<lt>":
		return '<', true
	case "<Tab>":
		return '\t', true
	}
	if strings.HasPrefix(string(k), "<") && len(k) > 1 {
		return 0, false
	}
	r, n := utf8.DecodeRuneInString(string(k))
	if n != len(k) {
		return 0, false
	}
	return r, true
}

// Parse splits a key notation string into keys. "<leader>" is expanded
// to leader. Unknown <...> groups are kept literally as single keys.
func Parse(s string, leader Key) []Key {
	var keys []Key
	for len(s) > 0 {
		if s[0] == '<' {
			if end := strings.IndexByte(s, '>'); end > 1 {
				name := s[:end+1]
				if strings.EqualFold(name, "<leader>") {
					keys = append(keys, leader)
				} else {
					keys = append(keys, Key(normalize(name)))
				}
				s = s[end+1:]
				continue
			}
		}
		r, n := utf8.DecodeRuneInString(s)
		keys = append(keys, RuneKey(r))
		s = s[n:]
	}
	return keys
}

var aliases = map[string]string{
	"<enter>": "<CR>", "<cr>": "<CR>", "<return>": "<CR>", "<esc>": "<Esc>",
	"<space>": "<Space>", "<tab>": "<Tab>", "<bs>": "<BS>", "<del>": "<Del>",
	"<up>": "<Up>", "<down>": "<Down>", "<left>": "<Left>", "<right>": "<Right>",
	"<lt>": "<lt>", "<home>": "<Home>", "<end>": "<End>",
	"<pageup>": "<PageUp>", "<pagedown>": "<PageDown>",
}

func normalize(name string) string {
	if a, ok := aliases[strings.ToLower(name)]; ok {
		return a
	}
	inner := name[1 : len(name)-1]
	parts := strings.Split(inner, "-")
	if len(parts) > 1 {
		last := parts[len(parts)-1]
		mods := parts[:len(parts)-1]
		for i, m := range mods {
			mods[i] = strings.ToUpper(m)
		}
		if len(last) == 1 {
			last = strings.ToLower(last)
		} else if a, ok := aliases["<"+strings.ToLower(last)+">"]; ok {
			last = strings.Trim(a, "<>")
		}
		return "<" + strings.Join(mods, "-") + "-" + last + ">"
	}
	return name
}

// Format renders keys back to a display string.
func Format(keys []Key) string {
	var sb strings.Builder
	for _, k := range keys {
		if k == "<Space>" {
			sb.WriteString("SPC ")
			continue
		}
		sb.WriteString(string(k))
	}
	return strings.TrimSpace(sb.String())
}

// Binding is the value stored at a trie node.
type Binding[T any] struct {
	Label string
	Value T
}

// Node is one trie node.
type Node[T any] struct {
	Children map[Key]*Node[T]
	Binding  *Binding[T]
	Group    string // which-key group name for prefixes
}

// Map is a prefix trie of key sequences.
type Map[T any] struct {
	Root *Node[T]
}

// NewMap returns an empty map.
func NewMap[T any]() *Map[T] {
	return &Map[T]{Root: &Node[T]{Children: map[Key]*Node[T]{}}}
}

func (m *Map[T]) walk(keys []Key, create bool) *Node[T] {
	n := m.Root
	for _, k := range keys {
		c, ok := n.Children[k]
		if !ok {
			if !create {
				return nil
			}
			c = &Node[T]{Children: map[Key]*Node[T]{}}
			n.Children[k] = c
		}
		n = c
	}
	return n
}

// Bind sets the binding for keys.
func (m *Map[T]) Bind(keys []Key, label string, v T) {
	m.walk(keys, true).Binding = &Binding[T]{Label: label, Value: v}
}

// SetGroup names the prefix keys for which-key.
func (m *Map[T]) SetGroup(keys []Key, name string) {
	m.walk(keys, true).Group = name
}

// Lookup returns the node for keys, or nil.
func (m *Map[T]) Lookup(keys []Key) *Node[T] {
	return m.walk(keys, false)
}

// Entry describes a child for the which-key popup.
type Entry struct {
	Key   Key
	Label string
	Group bool
}

// Entries returns the children of n sorted for display.
func (n *Node[T]) Entries() []Entry {
	var out []Entry
	for k, c := range n.Children {
		e := Entry{Key: k}
		if len(c.Children) > 0 && c.Binding == nil {
			e.Group = true
			e.Label = "+" + c.Group
			if c.Group == "" {
				e.Label = "+prefix"
			}
		} else if c.Binding != nil {
			e.Label = c.Binding.Label
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := sortKey(out[i].Key), sortKey(out[j].Key)
		return a < b
	})
	return out
}

func sortKey(k Key) string {
	s := string(k)
	if strings.HasPrefix(s, "<") && len(s) > 1 {
		return "~" + s
	}
	// lowercase before uppercase of same letter
	r, _ := utf8.DecodeRuneInString(s)
	if r >= 'A' && r <= 'Z' {
		return strings.ToLower(s) + "1"
	}
	return s + "0"
}
