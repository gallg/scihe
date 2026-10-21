package keymap

import (
	"reflect"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestParse(t *testing.T) {
	got := Parse("<leader>ff<C-W>v<cr>x<lt>", "<Space>")
	want := []Key{"<Space>", "f", "f", "<C-w>", "v", "<CR>", "x", "<lt>"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestFromEvent(t *testing.T) {
	cases := []struct {
		ev   *tcell.EventKey
		want Key
	}{
		{tcell.NewEventKey(tcell.KeyRune, 'a', 0), "a"},
		{tcell.NewEventKey(tcell.KeyRune, ' ', 0), "<Space>"},
		{tcell.NewEventKey(tcell.KeyCtrlW, 0, tcell.ModCtrl), "<C-w>"},
		{tcell.NewEventKey(tcell.KeyEnter, 0, 0), "<CR>"},
		{tcell.NewEventKey(tcell.KeyBackspace2, 0, 0), "<BS>"},
		{tcell.NewEventKey(tcell.KeyBackspace, 0, 0), "<BS>"},
		{tcell.NewEventKey(tcell.KeyEsc, 0, 0), "<Esc>"},
		{tcell.NewEventKey(tcell.KeyRune, 'x', tcell.ModAlt), "<M-x>"},
		{tcell.NewEventKey(tcell.KeyRune, '<', 0), "<lt>"},
	}
	for _, c := range cases {
		if got := FromEvent(c.ev); got != c.want {
			t.Errorf("%v: got %q want %q", c.ev.Name(), got, c.want)
		}
	}
}

func TestTrie(t *testing.T) {
	m := NewMap[int]()
	m.Bind(Parse("gg", ""), "top", 1)
	m.Bind(Parse("gU", ""), "upper", 2)
	m.SetGroup(Parse("g", ""), "goto")
	n := m.Lookup(Parse("g", ""))
	if n == nil || n.Binding != nil || len(n.Entries()) != 2 {
		t.Fatalf("bad prefix node %+v", n)
	}
	if m.Lookup(Parse("gx", "")) != nil {
		t.Fatal("unexpected node")
	}
	if m.Lookup(Parse("gg", "")).Binding.Value != 1 {
		t.Fatal("bad binding")
	}
}
