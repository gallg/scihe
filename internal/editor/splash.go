package editor

import (
	"fmt"
	"math/rand/v2"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"

	"scihe/internal/buffer"
)

// appVersion is the release shown by the splash, set by main through
// SetVersion ("" in tests).
var appVersion string

// SetVersion records the release for the splash.
func SetVersion(v string) { appVersion = v }

// splashState is the start-up screen, drawn over the empty buffer scihe
// starts with until that buffer is edited, replaced or split.
type splashState struct {
	buf   *buffer.Buffer
	ver   int   // buf.Version when started: an edit ends the splash
	order []int // the logos in a random order: the first that fits is drawn
}

func newSplash(b *buffer.Buffer) *splashState {
	return &splashState{buf: b, ver: b.Version, order: rand.Perm(len(splashLogos))}
}

// splashKeys are the keys the splash lists: only ones useful in the
// empty buffer scihe starts with.
var splashKeys = [][2]string{
	{"Space Space", "Find file"},
	{"Space f e", "File explorer"},
	{"Space f n", "New file"},
	{"Space t t", "Theme"},
	{":config", "Edit the configuration"},
	{":help", "Every key and command"},
	{":q", "Quit"},
}

// splashShown reports whether the splash is still up, ending it for
// good once its buffer changed or is no longer alone on the screen.
func (e *Editor) splashShown() bool {
	sp := e.splash
	if sp == nil {
		return false
	}
	if sp.buf.Version != sp.ver || len(e.windows()) != 1 || e.win.buf != sp.buf {
		e.splash = nil
		return false
	}
	return true
}

// splashSection is a block of lines centered as a whole.
type splashSection struct {
	lines []string
	st    tcell.Style
	keys  bool // the lines are splashKeys
}

func (s splashSection) width() int {
	w := 0
	for _, l := range s.lines {
		w = max(w, runewidth.StringWidth(l))
	}
	return w
}

// drawSplash draws the splash centered in the text area of w below the
// cursor line, fitted to its current size: the first logo of the random
// order that fits, the tagline and the keys. A narrower window gets the
// name and version for the tagline, one too small for any logo no logo,
// then the name alone, then nothing.
func (e *Editor) drawSplash(w *Window) {
	th := e.theme
	x0, y0 := w.x+e.gutterWidth(w), w.y+1
	ww, wh := e.textWidth(w), w.textHeight()-1
	title, tagline := "scihe", "a modal editor for data and code"
	if appVersion != "" {
		title += " " + appVersion
		tagline += " · v" + appVersion
	}
	keyW := 0
	for _, k := range splashKeys {
		keyW = max(keyW, runewidth.StringWidth(k[0]))
	}
	keys := make([]string, len(splashKeys))
	for i, k := range splashKeys {
		keys[i] = fmt.Sprintf("%-*s   %s", keyW, k[0], k[1])
	}
	gap := splashSection{lines: []string{""}}
	tags := []splashSection{{lines: []string{tagline}, st: th.Dim}, {lines: []string{title}, st: th.Dim}}
	ks := splashSection{lines: keys, keys: true}
	fits := func(secs []splashSection) bool {
		h := 0
		for _, s := range secs {
			if s.width() > ww {
				return false
			}
			h += len(s.lines)
		}
		return h <= wh
	}
	var cands [][]splashSection
	for _, i := range e.splash.order {
		for _, tag := range tags {
			cands = append(cands, []splashSection{{lines: splashLogos[i], st: th.CurLineNr}, gap, tag, gap, ks})
		}
	}
	for _, tag := range tags {
		cands = append(cands, []splashSection{tag, gap, ks})
	}
	cands = append(cands, []splashSection{{lines: []string{title}, st: th.CurLineNr}})
	var layout []splashSection
	for _, l := range cands {
		if fits(l) {
			layout = l
			break
		}
	}
	h := 0
	for _, s := range layout {
		h += len(s.lines)
	}
	y := y0 + (wh-h)/2
	for _, s := range layout {
		x := x0 + (ww-s.width())/2
		for i, l := range s.lines {
			if s.keys {
				drawText(e.screen, x, y, keyW, splashKeys[i][0], th.Info)
				drawText(e.screen, x+keyW+3, y, ww-keyW-3, splashKeys[i][1], th.Default)
			} else {
				drawText(e.screen, x, y, ww, l, s.st)
			}
			y++
		}
	}
}

// splashLogos are "scihe" in figlet fonts, at most 45×8.
var splashLogos = [][]string{
	{ // standard
		"          _ _",
		" ___  ___(_) |__   ___",
		"/ __|/ __| | '_ \\ / _ \\",
		"\\__ \\ (__| | | | |  __/",
		"|___/\\___|_|_| |_|\\___|",
	},
	{ // slant
		"              _ __",
		"   __________(_) /_  ___",
		"  / ___/ ___/ / __ \\/ _ \\",
		" (__  ) /__/ / / / /  __/",
		"/____/\\___/_/_/ /_/\\___/",
	},
	{ // small
		"        _ _",
		" ___ __(_) |_  ___",
		"(_-</ _| | ' \\/ -_)",
		"/__/\\__|_|_||_\\___|",
	},
	{ // smslant
		"           _ __",
		"  ___ ____(_) /  ___",
		" (_-</ __/ / _ \\/ -_)",
		"/___/\\__/_/_//_/\\__/",
	},
	{ // shadow
		"          _) |",
		"  __|  __| | __ \\   _ \\",
		"\\__ \\ (    | | | |  __/",
		"____/\\___|_|_| |_|\\___|",
	},
	{ // larry3d
		"                  __",
		"               __/\\ \\",
		"  ____    ___ /\\_\\ \\ \\___      __",
		" /',__\\  /'___\\/\\ \\ \\  _ `\\  /'__`\\",
		"/\\__, `\\/\\ \\__/\\ \\ \\ \\ \\ \\ \\/\\  __/",
		"\\/\\____/\\ \\____\\\\ \\_\\ \\_\\ \\_\\ \\____\\",
		" \\/___/  \\/____/ \\/_/\\/_/\\/_/\\/____/",
	},
	{ // puffy
		"                 _",
		"              _ ( )",
		"  ___    ___ (_)| |__     __",
		"/',__) /'___)| ||  _ `\\ /'__`\\",
		"\\__, \\( (___ | || | | |(  ___/",
		"(____/`\\____)(_)(_) (_)`\\____)",
	},
	{ // rounded
		"            _ _",
		"           (_) |",
		"  ___  ____ _| |__  _____",
		" /___)/ ___) |  _ \\| ___ |",
		"|___ ( (___| | | | | ____|",
		"(___/ \\____)_|_| |_|_____)",
	},
	{ // speed
		"             ___________",
		"________________(_)__  /______",
		"__  ___/  ___/_  /__  __ \\  _ \\",
		"_(__  )/ /__ _  / _  / / /  __/",
		"/____/ \\___/ /_/  /_/ /_/\\___/",
	},
	{ // graffiti
		"              .__.__",
		"  ______ ____ |__|  |__   ____",
		" /  ___// ___\\|  |  |  \\_/ __ \\",
		" \\___ \\\\  \\___|  |   Y  \\  ___/",
		"/____  >\\___  >__|___|  /\\___  >",
		"     \\/     \\/        \\/     \\/",
	},
	{ // chunky
		"             __ __",
		".-----.----.|__|  |--.-----.",
		"|__ --|  __||  |     |  -__|",
		"|_____|____||__|__|__|_____|",
	},
	{ // bulbhead
		" ___   ___  ____  _   _  ____",
		"/ __) / __)(_  _)( )_( )( ___)",
		"\\__ \\( (__  _)(_  ) _ (  )__)",
		"(___/ \\___)(____)(_) (_)(____)",
	},
	{ // cyberlarge
		" _______ _______ _____ _     _ _______",
		" |______ |         |   |_____| |______",
		" ______| |_____  __|__ |     | |______",
	},
	{ // 3-d
		"                 ** **",
		"                // /**",
		"  ******  *****  **/**       *****",
		" **////  **///**/**/******  **///**",
		"//***** /**  // /**/**///**/*******",
		" /////**/**   **/**/**  /**/**////",
		" ****** //***** /**/**  /**//******",
		"//////   /////  // //   //  //////",
	},
	{ // epic
		" _______  _______ _________          _______",
		"(  ____ \\(  ____ \\\\__   __/|\\     /|(  ____ \\",
		"| (    \\/| (    \\/   ) (   | )   ( || (    \\/",
		"| (_____ | |         | |   | (___) || (__",
		"(_____  )| |         | |   |  ___  ||  __)",
		"      ) || |         | |   | (   ) || (",
		"/\\____) || (____/\\___) (___| )   ( || (____/\\",
		"\\_______)(_______/\\_______/|/     \\|(_______/",
	},
}
