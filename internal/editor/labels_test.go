package editor

import (
	"testing"

	"github.com/mattn/go-runewidth"

	"scihe/internal/keymap"
)

// every which-key label must fit the popup's label column, so none is
// ever shown cut off
func TestKeyLabelsFitPopup(t *testing.T) {
	e := newTestEditor(t, "")
	tableN, tableV := e.tableKeys()
	maps := map[string]*keymap.Map[*Cmd]{
		"normal":      e.normalMap,
		"visual":      e.visualMap,
		"read-only":   e.readOnlyMap,
		"buffer list": e.bufListKeys(),
		"git status":  e.gitStatusKeys(),
		"explorer":    e.diredKeys(),
		"table":       tableN,
		"table/vis":   tableV,
	}
	for name, m := range maps {
		var walk func(n *keymap.Node[*Cmd], prefix []keymap.Key)
		walk = func(n *keymap.Node[*Cmd], prefix []keymap.Key) {
			for _, en := range n.Entries() {
				keys := append(append([]keymap.Key(nil), prefix...), en.Key)
				if w := runewidth.StringWidth(en.Label); w > maxKeyLabel {
					t.Errorf("%s %s: %d > %d columns: %q", name, keymap.Format(keys), w, maxKeyLabel, en.Label)
				}
				walk(n.Children[en.Key], keys)
			}
		}
		walk(m.Root, nil)
	}
}
