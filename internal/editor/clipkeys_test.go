package editor

import (
	"strings"
	"testing"
	"unicode/utf8"

	"scihe/internal/clipboard"
	"scihe/internal/keymap"
)

// fakeClipboard replaces the system clipboard for the test.
func fakeClipboard(t *testing.T, err error) *string {
	t.Helper()
	var text string
	oldCopy, oldPaste := clipCopy, clipPaste
	clipCopy = func(s string) error {
		if err == nil {
			text = s
		}
		return err
	}
	clipPaste = func() (string, error) { return text, err }
	t.Cleanup(func() { clipCopy, clipPaste = oldCopy, oldPaste })
	return &text
}

func TestClipboardYankKeys(t *testing.T) {
	clip := fakeClipboard(t, nil)
	e := newTestEditor(t, "alpha beta\nsecond\nthird\n")

	// y stays internal
	typeKeys(e, "yy")
	if *clip != "" || unnamedText(e) != "alpha beta\n" {
		t.Fatalf("yy: clipboard %q, unnamed %q", *clip, unnamedText(e))
	}
	// <leader>y{motion}, <leader>yy and <leader>Y go to the clipboard only
	typeKeys(e, "<Space>yiw")
	if *clip != "alpha" || unnamedText(e) != "alpha beta\n" {
		t.Fatalf("<leader>yiw: clipboard %q, unnamed %q", *clip, unnamedText(e))
	}
	if !strings.Contains(e.msg, "system clipboard") {
		t.Fatalf("no clipboard message: %q", e.msg)
	}
	typeKeys(e, "j<Space>yy")
	if *clip != "second\n" {
		t.Fatalf("<leader>yy: clipboard %q", *clip)
	}
	typeKeys(e, "gg2<Space>Y")
	if *clip != "alpha beta\nsecond\n" {
		t.Fatalf("2<leader>Y: clipboard %q", *clip)
	}
	// visual <leader>y and <leader>Y yank the selection
	typeKeys(e, "ggwve<Space>y")
	if *clip != "beta" || e.mode != ModeNormal {
		t.Fatalf("v<leader>y: clipboard %q, mode %v", *clip, e.mode)
	}
	typeKeys(e, "jV<Space>Y")
	if *clip != "second\n" {
		t.Fatalf("V<leader>Y: clipboard %q", *clip)
	}
	// visual <leader>Y yanks whole lines, as vY does
	typeKeys(e, "ggwv<Space>Y")
	if *clip != "alpha beta\n" {
		t.Fatalf("v<leader>Y: clipboard %q", *clip)
	}
	if unnamedText(e) != "alpha beta\n" {
		t.Fatalf("clipboard yanks changed the unnamed register: %q", unnamedText(e))
	}
}

func TestClipboardPasteKeys(t *testing.T) {
	clip := fakeClipboard(t, nil)
	e := newTestEditor(t, "one\ntwo\n")
	*clip = "X"
	typeKeys(e, "gg<Space>p")
	if got := e.win.buf.LineString(0); got != "oXne" {
		t.Fatalf("<leader>p: %q", got)
	}
	typeKeys(e, "u0<Space>P")
	if got := e.win.buf.LineString(0); got != "Xone" {
		t.Fatalf("<leader>P: %q", got)
	}
	// linewise clipboard text is put on its own line
	*clip = "new\n"
	typeKeys(e, "u<Space>p")
	if got := e.win.buf.Text(); !strings.HasPrefix(got, "one\nnew\ntwo") {
		t.Fatalf("linewise <leader>p: %q", got)
	}
	// visual <leader>R / <leader>p replace the selection
	*clip = "Z"
	typeKeys(e, "uggviw<Space>R")
	if got := e.win.buf.LineString(0); got != "Z" {
		t.Fatalf("v<leader>R: %q", got)
	}
	typeKeys(e, "ujviw<Space>p")
	if got := e.win.buf.LineString(1); got != "Z" {
		t.Fatalf("v<leader>p: %q", got)
	}
}

func TestClipboardRegisterNotNameable(t *testing.T) {
	clip := fakeClipboard(t, nil)
	e := newTestEditor(t, "keep\n")
	typeKeys(e, `"+yy`)
	if *clip != "" {
		t.Fatalf(`"+yy reached the clipboard: %q`, *clip)
	}
	typeKeys(e, ":yank +<CR>")
	if *clip != "" || !strings.Contains(e.msg, "E354") {
		t.Fatalf(":yank +: clipboard %q, msg %q", *clip, e.msg)
	}
	*clip = "nope"
	typeKeys(e, ":put +<CR>")
	if e.win.buf.LineCount() != 2 || !strings.Contains(e.msg, "E354") {
		t.Fatalf(":put +: %q, msg %q", e.win.buf.Text(), e.msg)
	}
}

// the clipboard is read by the clipboard keys only
func TestClipboardNotReadElsewhere(t *testing.T) {
	clip := fakeClipboard(t, nil)
	*clip = "ddi"
	e := newTestEditor(t, "a\nb\n")
	typeKeys(e, "ix<C-r>+<Esc>")
	typeKeys(e, "j@+")
	if got := e.win.buf.Text(); got != "xa\nb\n" {
		t.Fatalf("<C-r>+ / @+ read the clipboard: %q", got)
	}
	typeKeys(e, `"a<Space>p`)
	if got := e.win.buf.Text(); got != "xa\nb\n" || !strings.Contains(e.msg, "no register") {
		t.Fatalf(`"a<leader>p: %q, msg %q`, got, e.msg)
	}
}

func TestClipboardUnavailableIsReported(t *testing.T) {
	fakeClipboard(t, clipboard.ErrUnavailable)
	e := newTestEditor(t, "text\n")
	typeKeys(e, "<Space>yy")
	if !strings.Contains(e.msg, "no system clipboard") {
		t.Fatalf("yank: msg %q", e.msg)
	}
	typeKeys(e, "<Space>p")
	if !strings.Contains(e.msg, "no system clipboard") || e.win.buf.Text() != "text\n" {
		t.Fatalf("paste: msg %q, text %q", e.msg, e.win.buf.Text())
	}
}

func TestPythonKeysInReplGroup(t *testing.T) {
	e := newTestEditor(t, "")
	for _, k := range []string{"<Space>rf", "<Space>rF", "<Space>rk"} {
		if _, st := e.parseKeys(keymap.Parse(k, e.leader), e.normalMap, true); st != parseComplete {
			t.Errorf("%s is not bound", k)
		}
	}
	// <leader>p is the clipboard paste now, with no python keys under it
	keys := keymap.Parse("<Space>pr", e.leader)
	if p, _ := e.parseKeys(keys, e.normalMap, true); p.end == len(keys) {
		t.Error("<leader>pr still bound")
	}
}

func unnamedText(e *Editor) string {
	r, _ := e.regs.Get('"')
	return r.Text
}

// which-key lists only keys that do something in the current mode, and
// a group with nothing bound under it is not a key at all.
func TestWhichKeyHidesEmptyGroups(t *testing.T) {
	e := newTestEditor(t, "text\n")
	var check func(name string, n *keymap.Node[*Cmd], path string)
	check = func(name string, n *keymap.Node[*Cmd], path string) {
		for _, en := range n.Entries() {
			if en.Label == "" || en.Label == "+" {
				t.Errorf("%s: %s%s has no label", name, path, en.Key)
			}
			check(name, n.Children[en.Key], path+string(en.Key))
		}
	}
	check("normal", e.normalMap.Root, "")
	check("visual", e.visualMap.Root, "")

	typeKeys(e, "v<Space>")
	for _, en := range e.whichKeyNode().Entries() {
		if en.Key == "b" || en.Key == "w" {
			t.Errorf("visual popup shows the empty group %s", en.Key)
		}
	}
	typeKeys(e, "b")
	if len(e.pending) != 0 || e.mode != ModeVisual {
		t.Fatalf("v<Space>b: pending %v, mode %v", e.pending, e.mode)
	}
}

// :registers shortens long contents by characters, never mid-character.
func TestRegistersListKeepsUTF8(t *testing.T) {
	e := newTestEditor(t, strings.Repeat("é", 80)+"\n")
	typeKeys(e, "yy")
	e.execEx("registers")
	if txt := e.win.buf.Text(); !utf8.ValidString(txt) || !strings.Contains(txt, strings.Repeat("é", 70)) {
		t.Fatalf("listing %q", txt)
	}
}
