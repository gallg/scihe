package editor

import (
	"slices"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// screenRows returns the text of each screen row.
func screenRows(e *Editor) []string {
	cells, w, h := e.screen.(tcell.SimulationScreen).GetContents()
	rows := make([]string, h)
	for y := range h {
		var sb strings.Builder
		for x := range w {
			if r := cells[y*w+x].Runes; len(r) > 0 {
				sb.WriteString(string(r))
			} else {
				sb.WriteByte(' ')
			}
		}
		rows[y] = sb.String()
	}
	return rows
}

// shows reports whether every line of lines is on the screen, on
// consecutive rows at the same column.
func shows(rows, lines []string) bool {
	for y := range rows {
		x := strings.Index(rows[y], lines[0])
		if x < 0 || y+len(lines) > len(rows) {
			continue
		}
		ok := true
		for i, l := range lines {
			if !strings.HasPrefix(rows[y+i][x:], l) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// The splash fits the terminal at any size and follows its resizing: the
// logo of the random order that fits, else the keys, else the name.
func TestSplashFitsAndResizes(t *testing.T) {
	e := newTestEditor(t, "")
	e.Open(nil)
	if e.splash == nil {
		t.Fatal("no splash at start without files")
	}
	const epic, standard = 14, 0
	e.splash.order = []int{epic, standard, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}
	sim := e.screen.(tcell.SimulationScreen)
	keys := []string{"Find file"}
	// the splash has the text area below the cursor line: the screen
	// less the gutter, and less 3 rows (cursor line, status, command line)
	gw := e.gutterWidth(e.win)
	cases := []struct {
		w, h       int
		logo       int // -1: none
		keys, name bool
	}{
		{100, 30, epic, true, false},
		{gw + 44, 30, standard, true, false}, // epic is 45 wide
		{100, 15, -1, true, false},           // 12 rows: a logo needs 13 or more
		{gw + 36, 16, -1, true, false},       // the widest key line is 36
		{gw + 35, 16, -1, false, true},
		{20, 8, -1, false, true},
		{3, 3, -1, false, false},
		{100, 30, epic, true, false}, // back to the first choice
	}
	for _, c := range cases {
		sim.SetSize(c.w, c.h)
		e.draw()
		rows := screenRows(e)
		for i, l := range splashLogos {
			if got := shows(rows, l); got != (i == c.logo) {
				t.Fatalf("%dx%d: logo %d shown %v\n%s", c.w, c.h, i, got, strings.Join(rows, "\n"))
			}
		}
		if shows(rows, keys) != c.keys {
			t.Fatalf("%dx%d: keys shown %v\n%s", c.w, c.h, !c.keys, strings.Join(rows, "\n"))
		}
		name := !c.keys && slices.ContainsFunc(rows, func(r string) bool { return slices.Contains(strings.Fields(r), "scihe") })
		if name != c.name {
			t.Fatalf("%dx%d: name shown %v\n%s", c.w, c.h, name, strings.Join(rows, "\n"))
		}
	}
}

// The first edit, a split or another buffer ends the splash for good;
// it is not shown with a file or with splash = false.
func TestSplashEnds(t *testing.T) {
	for _, keys := range []string{"ihello<Esc>u", ":vsplit<CR>:only<CR>", ":enew<CR>:b1<CR>"} {
		e := newTestEditor(t, "")
		e.Open(nil)
		typeKeys(e, keys)
		if e.splash != nil || shows(screenRows(e), []string{"Find file"}) {
			t.Fatalf("%q: splash still shown", keys)
		}
	}
	e := newTestEditor(t, "")
	e.Open([]string{"splash_test.go"})
	if e.splash != nil {
		t.Fatal("splash with a file")
	}
	e = newTestEditor(t, "")
	e.cfg.Splash = false
	e.Open(nil)
	if e.splash != nil {
		t.Fatal("splash = false")
	}
}

// A window narrower than the tagline with the version shows the name and
// version in its place, keeping the logo and the keys.
func TestSplashShortTagline(t *testing.T) {
	SetVersion("0.2.6")
	defer SetVersion("")
	e := newTestEditor(t, "")
	e.Open(nil)
	const larry3d = 5
	e.splash.order = []int{larry3d}
	e.screen.(tcell.SimulationScreen).SetSize(40, 22)
	e.draw()
	rows := screenRows(e)
	if !shows(rows, splashLogos[larry3d]) || !shows(rows, []string{"scihe 0.2.6"}) || !shows(rows, []string{"Find file"}) || shows(rows, []string{"a modal editor"}) {
		t.Fatalf("\n%s", strings.Join(rows, "\n"))
	}
}
