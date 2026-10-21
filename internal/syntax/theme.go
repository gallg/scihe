// Package syntax provides syntax highlighting (backed by chroma lexers)
// and the colour theme used by the whole UI.
package syntax

import (
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/gdamore/tcell/v2"
	"github.com/lucasb-eyer/go-colorful"
)

// Theme maps token types and UI elements to terminal styles.
type Theme struct {
	Name string
	// Transparent leaves the text background to the terminal (so its
	// transparency or wallpaper shows through); bars and popups stay opaque.
	Transparent bool

	Default        tcell.Style
	LineNr         tcell.Style
	CurLineNr      tcell.Style
	CursorLine     tcell.Style
	Selection      tcell.Style
	Search         tcell.Style
	Status         tcell.Style
	StatusInactive tcell.Style
	StatusMode     tcell.Style
	Popup          tcell.Style
	PopupKey       tcell.Style
	PopupGroup     tcell.Style
	PopupSel       tcell.Style
	Border         tcell.Style
	Error          tcell.Style
	Warning        tcell.Style
	Info           tcell.Style
	Dim            tcell.Style
	Virtual        tcell.Style
	CellHeader     tcell.Style
	MatchParen     tcell.Style
	NonText        tcell.Style

	bg, fg tcell.Color
	style  *chroma.Style
	mu     sync.Mutex
	cache  map[chroma.TokenType]tcell.Style
}

// ThemeNames lists available theme names.
func ThemeNames() []string { return styles.Names() }

func toColor(c chroma.Colour) tcell.Color {
	if !c.IsSet() {
		return tcell.ColorDefault
	}
	return tcell.NewRGBColor(int32(c.Red()), int32(c.Green()), int32(c.Blue()))
}

func toColorful(c tcell.Color) colorful.Color {
	r, g, b := c.RGB()
	if r < 0 {
		return colorful.Color{}
	}
	return colorful.Color{R: float64(r) / 255, G: float64(g) / 255, B: float64(b) / 255}
}

func fromColorful(c colorful.Color) tcell.Color {
	r, g, b := c.Clamped().RGB255()
	return tcell.NewRGBColor(int32(r), int32(g), int32(b))
}

// blend mixes a toward b by t (0..1).
func blend(a, b tcell.Color, t float64) tcell.Color {
	return fromColorful(toColorful(a).BlendRgb(toColorful(b), t))
}

func isDark(c tcell.Color) bool {
	l, _, _ := toColorful(c).Lab()
	return l < 0.5
}

// LoadTheme builds a theme from a chroma style name; transparent uses
// the terminal's default background for text. Unknown names fall back to
// catppuccin-mocha.
func LoadTheme(name string, transparent bool) *Theme {
	st := styles.Get(name)
	if st == nil || st == styles.Fallback && name != "swapoff" {
		if s2 := styles.Get("catppuccin-mocha"); s2 != nil {
			st = s2
			name = "catppuccin-mocha"
		}
	}
	t := &Theme{Name: name, Transparent: transparent, style: st, cache: map[chroma.TokenType]tcell.Style{}}
	bgEntry := st.Get(chroma.Background)
	t.bg = toColor(bgEntry.Background)
	t.fg = toColor(bgEntry.Colour)
	if t.bg == tcell.ColorDefault {
		t.bg = tcell.NewRGBColor(30, 30, 46)
	}
	if t.fg == tcell.ColorDefault {
		if isDark(t.bg) {
			t.fg = tcell.NewRGBColor(205, 214, 244)
		} else {
			t.fg = tcell.NewRGBColor(40, 40, 40)
		}
	}
	dark := isDark(t.bg)
	contrast := tcell.ColorWhite
	if !dark {
		contrast = tcell.ColorBlack
	}
	accent := t.colorOf(chroma.Keyword, tcell.ColorBlue)
	str := t.colorOf(chroma.LiteralString, tcell.ColorGreen)
	errc := t.colorOf(chroma.Error, tcell.ColorRed)
	if errc == t.fg || errc == tcell.ColorDefault {
		errc = tcell.NewRGBColor(243, 139, 168)
	}
	comment := t.colorOf(chroma.Comment, blend(t.fg, t.bg, 0.5))
	base := tcell.StyleDefault.Background(t.bg).Foreground(t.fg)
	if transparent {
		// derived colours below still blend against t.bg
		base = base.Background(tcell.ColorDefault)
	}
	t.Default = base
	t.LineNr = base.Foreground(blend(t.fg, t.bg, 0.6))
	t.CurLineNr = base.Foreground(accent).Bold(true)
	t.CursorLine = base.Background(blend(t.bg, contrast, 0.06))
	t.Selection = base.Background(blend(t.bg, accent, 0.35))
	t.Search = base.Background(blend(t.bg, str, 0.45))
	statusBg := blend(t.bg, contrast, 0.12)
	t.Status = base.Background(statusBg)
	t.StatusInactive = base.Background(blend(t.bg, contrast, 0.05)).Foreground(blend(t.fg, t.bg, 0.4))
	t.StatusMode = base.Background(accent).Foreground(t.bg).Bold(true)
	popBg := blend(t.bg, contrast, 0.08)
	t.Popup = base.Background(popBg)
	t.PopupKey = base.Background(popBg).Foreground(str).Bold(true)
	t.PopupGroup = base.Background(popBg).Foreground(accent)
	t.PopupSel = base.Background(blend(popBg, accent, 0.35))
	t.Border = base.Background(popBg).Foreground(blend(t.fg, t.bg, 0.5))
	t.Error = base.Foreground(errc)
	t.Warning = base.Foreground(t.colorOf(chroma.NameFunction, tcell.ColorYellow))
	t.Info = base.Foreground(str)
	t.Dim = base.Foreground(comment)
	t.Virtual = base.Foreground(comment).Italic(true)
	t.CellHeader = base.Background(blend(t.bg, accent, 0.18)).Foreground(accent).Bold(true)
	t.MatchParen = base.Background(blend(t.bg, contrast, 0.25)).Bold(true)
	t.NonText = base.Foreground(blend(t.fg, t.bg, 0.7))
	return t
}

// Background returns the text background colour (tcell.ColorDefault when
// the theme is transparent).
func (t *Theme) Background() tcell.Color { return bgOf(t.Default) }

func bgOf(st tcell.Style) tcell.Color {
	_, bg, _ := st.Decompose()
	return bg
}

// Foreground returns the theme foreground colour.
func (t *Theme) Foreground() tcell.Color { return t.fg }

func (t *Theme) colorOf(tt chroma.TokenType, def tcell.Color) tcell.Color {
	e := t.style.Get(tt)
	if c := toColor(e.Colour); c != tcell.ColorDefault {
		return c
	}
	return def
}

// TokenStyle returns the style for a chroma token type.
func (t *Theme) TokenStyle(tt chroma.TokenType) tcell.Style {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s, ok := t.cache[tt]; ok {
		return s
	}
	e := t.style.Get(tt)
	s := t.Default
	if c := toColor(e.Colour); c != tcell.ColorDefault {
		s = s.Foreground(c)
	}
	if c := toColor(e.Background); c != tcell.ColorDefault && c != t.bg {
		s = s.Background(c)
	}
	if e.Bold == chroma.Yes {
		s = s.Bold(true)
	}
	if e.Italic == chroma.Yes {
		s = s.Italic(true)
	}
	if e.Underline == chroma.Yes {
		s = s.Underline(true)
	}
	t.cache[tt] = s
	return s
}

// Accent returns an accent foreground style derived from a token type.
func (t *Theme) Accent(tt chroma.TokenType) tcell.Style {
	return t.Default.Foreground(t.colorOf(tt, t.fg))
}
