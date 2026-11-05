package editor

import (
	"bytes"
	"cmp"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// Typeset formulas (mathtype.go) shown by the terminal with the kitty
// graphics protocol and its Unicode placeholders, which Ghostty and
// kitty implement: an image is sent once, and the cells showing it are
// ordinary text, U+10EEEE with the image's id as foreground colour. The
// view, the scrolling and tmux handle them as any text; only sending
// the image needs tmux's passthrough.
//
// Each row of a formula is an image of its own, so a cell needs no
// diacritics to say which row it shows, and its column follows from the
// cell before it. In the text of the view, the cells of image slot n
// are the rune gfxBase+n (a private character), turned into the
// placeholder when drawn. The terminal knows the image as idBase+n: an
// offset of each editor, as the editors in the panes of a tmux window
// share the terminal's images.
//
// Konsole implements the protocol without the placeholders: there the
// cells are blank and each image is placed at their position after the
// screen is drawn (moved, or deleted, when they move or go).

const (
	gfxBase        = 0x100000 // + id: a cell of image id in the text of the view
	gfxMaxID       = 0xEEED   // below the placeholder itself
	gfxPlaceholder = 0x10EEEE
	gfxChunk       = 4096 // base64 bytes per escape sequence
	gfxMaxCache    = 2000 // formulas kept at least (see rotate)
)

// gfxImage is a sent (or to be sent) image: one row of a formula.
type gfxImage struct {
	payload string // zlib-compressed RGBA, base64
	w, h    int    // pixels
	cols    int
	row     int    // of its formula
	above   int    // the image of the row above (row > 0)
	alt     string // the formula on one line, on its baseline row (yanked)
	sent    bool
}

// gfxFormula is a formula's cells and its images, ok false when it is
// shown as text.
type gfxFormula struct {
	b   box
	ids []int
	ok  bool
}

type mathGfx struct {
	out          io.Writer
	tmux         bool
	scale        float64 // the size of formulas, times the text
	cellW, cellH int     // pixels of a cell, as drawn into the images
	color        [3]uint8
	gen          int // changes when the images are drawn anew
	idBase       int // the terminal's id of slot n is idBase+n
	next         int
	free         []int // slots deleted, to be used again
	imgs         map[int]*gfxImage
	cache, old   map[string]gfxFormula // laid out since the last rotate, and before
	rotated      int                   // formulas in the cache at the last rotate
	dead         []int                 // slots to delete from the terminal
	// direct placement (Konsole): the image cells drawn in this frame,
	// and the placements in the terminal
	direct bool
	frame  []gfxCellAt
	placed map[gfxPlacement]bool
	check  func(x, y int, st tcell.Style) bool // the cell still shows the image's blank
	// tmux: the pane's own allow-passthrough before the editor set it
	// ("" none), restored on exit
	pane, panePass string
}

// gfxCellAt is a cell of image id drawn at x, y in style st.
type gfxCellAt struct {
	id, x, y int
	st       tcell.Style
}

// gfxPlacement is image id placed (as placement p) at x, y.
type gfxPlacement struct{ id, p, x, y int }

// mathImages draws formulas when the terminal can show them (nil: the
// text layout of mdformula.go).
var mathImages *mathGfx

// gfxTerminal reports a terminal with kitty's Unicode placeholders.
func gfxTerminal() bool {
	term, prog := os.Getenv("TERM"), os.Getenv("TERM_PROGRAM")
	return prog == "ghostty" || term == "xterm-ghostty" || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" ||
		term == "xterm-kitty" || os.Getenv("KITTY_WINDOW_ID") != ""
}

// gfxDirect reports a terminal with the kitty protocol but without its
// placeholders: images are placed where their cells are.
func gfxDirect() bool {
	return os.Getenv("KONSOLE_VERSION") != ""
}

// tmuxRGB reports a terminal tmux sends RGB colours to: the ids of the
// placeholders are colours.
func tmuxRGB() bool {
	out, err := exec.Command("tmux", "display", "-p", "-t", os.Getenv("TMUX_PANE"), "#{client_termfeatures}").Output()
	return err == nil && slices.Contains(strings.Split(strings.TrimSpace(string(out)), ","), "RGB")
}

// tmuxOption reads an option of pane (with -A, as inherited).
func tmuxOption(pane, name string, inherited bool) (string, error) {
	flags := "-pv"
	if inherited {
		flags = "-Apv"
	}
	out, err := exec.Command("tmux", "show", flags, "-t", pane, name).Output()
	return strings.TrimSpace(string(out)), err
}

// tmuxPassthrough lets escape sequences through tmux to the terminal:
// with allow-passthrough on, or set on for the editor's pane only (its
// own value is kept in g, restored by close). False when tmux refuses.
func (g *mathGfx) tmuxPassthrough() bool {
	g.pane = os.Getenv("TMUX_PANE")
	v, err := tmuxOption(g.pane, "allow-passthrough", true)
	if err != nil {
		return false
	}
	if v == "on" || v == "all" {
		g.pane = ""
		return true
	}
	if g.panePass, err = tmuxOption(g.pane, "allow-passthrough", false); err != nil {
		return false
	}
	if exec.Command("tmux", "set", "-p", "-t", g.pane, "allow-passthrough", "on").Run() != nil {
		g.pane = ""
		return false
	}
	return true
}

// initMathImages turns typeset formulas on when the terminal shows
// them; otherwise formulas keep their text layout.
func (e *Editor) initMathImages() {
	placeholders := gfxTerminal()
	if !e.cfg.MathImages || !placeholders && !gfxDirect() {
		return
	}
	tty, ok := e.screen.Tty()
	if !ok || placeholders && e.screen.Colors() < 1<<24 { // ids are colours
		return
	}
	tmux := os.Getenv("TMUX") != ""
	if tmux && !placeholders {
		return // images placed at a position: tmux moves panes and text under them
	}
	g := &mathGfx{out: tty, tmux: tmux, scale: e.cfg.MathScale, direct: !placeholders, idBase: 1 + rand.IntN(0xFFFFFF-gfxMaxID)}
	if tmux && !tmuxRGB() {
		e.warnf("formulas as text: tmux passes no RGB colours to the terminal (terminal-features RGB)")
		return
	}
	if tmux && !g.tmuxPassthrough() {
		e.warnf("formulas as text: tmux refused allow-passthrough (set -g allow-passthrough on in tmux.conf)")
		return
	}
	if g.scale <= 0 {
		g.scale = 1.2
	}
	g.measure(tty)
	mathImages = g
}

// measure reads the size of a cell in pixels.
func (g *mathGfx) measure(tty tcell.Tty) {
	w, h := 10, 20
	if ws, err := tty.WindowSize(); err == nil && ws.Width > 0 && ws.Height > 0 && ws.PixelWidth > 0 && ws.PixelHeight > 0 {
		w, h = ws.PixelWidth/ws.Width, ws.PixelHeight/ws.Height
	}
	if h < 24 { // drawn finer than the terminal reports (a Retina display)
		w, h = 2*w, 2*h
	}
	g.setup(w, h, g.color)
}

// setup sets the cell size and the colour; the images are drawn anew
// when they change.
func (g *mathGfx) setup(cellW, cellH int, color [3]uint8) {
	if cellW == g.cellW && cellH == g.cellH && color == g.color {
		return
	}
	g.cellW, g.cellH, g.color = cellW, cellH, color
	g.reset()
}

// reset drops every image (deleted from the terminal before any new one
// is sent, so their ids are free again); the layouts are made anew.
func (g *mathGfx) reset() {
	for id := range g.imgs {
		g.dead = append(g.dead, id)
	}
	g.imgs = map[int]*gfxImage{}
	g.cache, g.old, g.rotated = map[string]gfxFormula{}, nil, 0
	g.placed = nil
	g.next, g.free = 0, nil
	g.gen++
}

// rotate drops the formulas not laid out since the last rotate (edited
// away, closed); the layouts are made anew, which keeps the others. It
// waits for the formulas to double, so a page of many is not laid out
// again and again.
func (g *mathGfx) rotate() {
	for _, f := range g.old {
		for _, id := range f.ids {
			g.dead = append(g.dead, id)
			g.free = append(g.free, id)
			delete(g.imgs, id)
		}
	}
	g.rotated = len(g.cache)
	g.old, g.cache = g.cache, map[string]gfxFormula{}
	g.gen++
}

// syncMathImages follows the theme and the cell size before a redraw.
func (e *Editor) syncMathImages(resized bool) {
	g := mathImages
	if g == nil {
		return
	}
	if resized {
		if tty, ok := e.screen.Tty(); ok {
			g.measure(tty)
		}
	}
	color := [3]uint8{0xd0, 0xd0, 0xd0}
	if fg, _, _ := e.theme.Default.Decompose(); fg.Valid() {
		r, gr, b := fg.TrueColor().RGB()
		color = [3]uint8{uint8(r), uint8(gr), uint8(b)}
	}
	g.setup(g.cellW, g.cellH, color)
	if len(g.cache) > max(gfxMaxCache, 2*g.rotated) { // formulas edited in a split, a long session
		g.rotate()
	}
}

// formula returns the cells showing a typeset formula, false when it
// can't be typeset (a character the font lacks).
func (g *mathGfx) formula(content string, n *fnode, display bool) (box, bool) {
	key := content + "\x00" + map[bool]string{true: "d", false: "t"}[display]
	f, ok := g.cache[key]
	if !ok {
		if f, ok = g.old[key]; ok {
			delete(g.old, key)
		} else {
			f = g.draw(n, display)
		}
		g.cache[key] = f
	}
	return f.b, f.ok
}

// gfxBaseline is where the baseline of the text is in a cell, and
// gfxEm the size of its font, in cell heights.
const (
	gfxBaseline = 0.77
	gfxEm       = 1 / 1.3
)

func (g *mathGfx) draw(n *fnode, display bool) gfxFormula {
	mb, ok := typeset(n, display, 0.6)
	if !ok || mb.w <= 0 {
		return gfxFormula{}
	}
	W, H := float64(g.cellW), float64(g.cellH)
	px := g.scale * gfxEm * H // pixels to the em
	var rows int
	var base float64 // the baseline, pixels from the top
	if display {     // alone on its rows: centred in them
		hpx, dpx := max(mb.h, 0)*px, max(mb.d, 0)*px
		rows = max(int(math.Ceil((hpx+dpx)/H)), 1)
		base = (float64(rows)*H-(hpx+dpx))/2 + hpx
	} else {
		// in the line of text: as large as fits in it, down to the size
		// of the text; a taller formula makes its line taller
		least := min(g.scale, 1)
		for s := g.scale; ; s -= 0.05 {
			s = max(s, least)
			px = s * gfxEm * H
			if rows, base = inlineRows(mb, px, H); rows == 1 || s <= least {
				break
			}
		}
		if rows > 1 {
			px = g.scale * gfxEm * H
			rows, base = inlineRows(mb, px, H)
		}
	}
	wpx := mb.w * px
	cols := max(int(math.Ceil((wpx+2)/W)), 1)
	if rows > len(g.free)+gfxMaxID-g.next {
		return gfxFormula{} // out of ids: text
	}
	mask := raster(mb, px, cols*g.cellW, rows*g.cellH, (float64(cols)*W-wpx)/2, base)
	out := box{base: min(int(base/H), rows-1)}
	var ids []int
	for r := 0; r < rows; r++ {
		var id int
		if k := len(g.free); k > 0 {
			id, g.free = g.free[k-1], g.free[:k-1]
		} else {
			g.next++
			id = g.next
		}
		ids = append(ids, id)
		pix := make([]byte, 0, cols*g.cellW*g.cellH*4)
		for y := r * g.cellH; y < (r+1)*g.cellH; y++ {
			for _, a := range mask.Pix[y*mask.Stride : y*mask.Stride+cols*g.cellW] {
				pix = append(pix, g.color[0], g.color[1], g.color[2], a)
			}
		}
		var zb bytes.Buffer
		zw := zlib.NewWriter(&zb)
		zw.Write(pix)
		zw.Close()
		g.imgs[id] = &gfxImage{payload: base64.StdEncoding.EncodeToString(zb.Bytes()), w: cols * g.cellW, h: g.cellH, cols: cols, row: r}
		if r > 0 {
			g.imgs[id].above = ids[r-1]
		}
		if r == out.base {
			g.imgs[id].alt = n.linear()
		}
		row := make([]mcell, cols)
		for c := range row {
			row[c] = mcell{string(rune(gfxBase + id)), msMath}
		}
		out.rows = append(out.rows, row)
	}
	return gfxFormula{out, ids, true}
}

// inlineRows returns the rows a formula takes on the baseline of the
// text (a few pixels off at most), at px pixels to the em, and where
// its baseline is, in pixels from the top.
func inlineRows(mb mbox, px, H float64) (int, float64) {
	hpx, dpx := max(mb.h, 0)*px, max(mb.d, 0)*px
	B, tol := gfxBaseline*H, 0.15*H
	above := max(int(math.Ceil((hpx-B-tol)/H)), 0)
	below := max(int(math.Ceil((dpx-(H-B)-tol)/H)), 0)
	rows := above + 1 + below
	for hpx+dpx > float64(rows)*H {
		rows++
	}
	base := float64(above)*H + B
	base = max(base, hpx)
	return rows, min(base, float64(rows)*H-dpx)
}

// esc writes a kitty graphics command, through tmux when inside it.
func (g *mathGfx) esc(w *strings.Builder, ctl, payload string) {
	seq := "\x1b_G" + ctl
	if payload != "" {
		seq += ";" + payload
	}
	seq += "\x1b\\"
	if g.tmux {
		seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	w.WriteString(seq)
}

// flush sends the images not sent yet and deletes the dropped ones;
// called before the screen shows their cells.
func (g *mathGfx) flush() {
	var sb strings.Builder
	for _, id := range g.dead {
		g.esc(&sb, fmt.Sprintf("a=d,d=I,i=%d,q=2", g.idBase+id), "")
	}
	g.dead = g.dead[:0]
	for id, im := range g.imgs {
		if im.sent {
			continue
		}
		p := im.payload
		first := true
		for {
			chunk := p[:min(len(p), gfxChunk)]
			p = p[len(chunk):]
			more := 0
			if p != "" {
				more = 1
			}
			if first {
				g.esc(&sb, fmt.Sprintf("a=t,f=32,o=z,s=%d,v=%d,i=%d,q=2,m=%d", im.w, im.h, g.idBase+id, more), chunk)
				first = false
			} else {
				g.esc(&sb, fmt.Sprintf("q=2,m=%d", more), chunk)
			}
			if p == "" {
				break
			}
		}
		if !g.direct {
			g.esc(&sb, fmt.Sprintf("a=p,U=1,i=%d,c=%d,r=1,q=2", g.idBase+id, im.cols), "")
		}
		im.sent = true
	}
	if sb.Len() > 0 {
		io.WriteString(g.out, sb.String())
	}
}

// resend sends every image again (the terminal may have dropped them:
// a tmux client attached from another terminal, a cleared screen).
func (g *mathGfx) resend() {
	for _, im := range g.imgs {
		im.sent = false
	}
	g.placed = nil
}

// at notes a cell of image id drawn at x, y in style st (direct
// placement).
func (g *mathGfx) at(id, x, y int, st tcell.Style) {
	if g.direct {
		g.frame = append(g.frame, gfxCellAt{id, x, y, st})
	}
}

// unplace deletes the placements (direct placement), before the screen
// is cleared.
func (g *mathGfx) unplace() {
	var sb strings.Builder
	for pl := range g.placed {
		g.esc(&sb, fmt.Sprintf("a=d,d=i,i=%d,p=%d,q=2", g.idBase+pl.id, pl.p), "")
	}
	g.placed = nil
	io.WriteString(g.out, sb.String())
}

// place puts the images drawn in this frame where their cells are,
// after the screen is shown: one placement per run of cells showing an
// image whole (the same formula may be shown twice); the others go.
func (g *mathGfx) place() {
	if !g.direct {
		return
	}
	slices.SortFunc(g.frame, func(a, b gfxCellAt) int {
		return cmp.Or(cmp.Compare(a.id, b.id), cmp.Compare(a.y, b.y), cmp.Compare(a.x, b.x))
	})
	var sb strings.Builder
	shown := map[gfxPlacement]bool{}
	count := map[int]int{} // placements per image
	for i := 0; i < len(g.frame); {
		c, j := g.frame[i], i+1
		whole := g.check == nil || g.check(c.x, c.y, c.st) // not covered by a popup
		for j < len(g.frame) && g.frame[j].id == c.id && g.frame[j].y == c.y && g.frame[j].x == g.frame[j-1].x+1 {
			whole = whole && (g.check == nil || g.check(g.frame[j].x, g.frame[j].y, g.frame[j].st))
			j++
		}
		im := g.imgs[c.id]
		if im != nil && j-i == im.cols && whole {
			count[c.id]++
			pl := gfxPlacement{c.id, count[c.id], c.x, c.y}
			shown[pl] = true
			if !g.placed[pl] {
				// at the cell, the cursor kept where it was (C=1, and saved);
				// the same placement id moves an image placed before
				sb.WriteString(fmt.Sprintf("\x1b7\x1b[%d;%dH", c.y+1, c.x+1))
				g.esc(&sb, fmt.Sprintf("a=p,i=%d,p=%d,c=%d,r=1,C=1,q=2", g.idBase+c.id, pl.p, im.cols), "")
				sb.WriteString("\x1b8")
			}
		}
		i = j
	}
	for pl := range g.placed {
		if pl.p > count[pl.id] { // not moved by a placement of the same id: gone
			g.esc(&sb, fmt.Sprintf("a=d,d=i,i=%d,p=%d,q=2", g.idBase+pl.id, pl.p), "")
		}
	}
	g.placed, g.frame = shown, g.frame[:0]
	if sb.Len() > 0 {
		io.WriteString(g.out, sb.String())
	}
}

// close deletes the images from the terminal.
func (g *mathGfx) close() {
	for id := range g.imgs {
		g.dead = append(g.dead, id)
	}
	g.imgs = map[int]*gfxImage{}
	g.flush()
	if g.pane != "" { // the pane's passthrough as it was
		if g.panePass == "" {
			exec.Command("tmux", "set", "-pu", "-t", g.pane, "allow-passthrough").Run()
		} else {
			exec.Command("tmux", "set", "-p", "-t", g.pane, "allow-passthrough", g.panePass).Run()
		}
	}
}

// gfxCell turns a cell of an image into the placeholder and its style
// (a blank under the image with direct placement); 0 when r is not one.
func gfxCell(r rune, st tcell.Style) (rune, tcell.Style, int) {
	if r <= gfxBase || r > gfxBase+gfxMaxID || mathImages == nil {
		return r, st, 0
	}
	id := int(r - gfxBase)
	_, bg, _ := st.Decompose()
	if mathImages.direct {
		return ' ', tcell.StyleDefault.Background(bg), id
	}
	return gfxPlaceholder, tcell.StyleDefault.Background(bg).Foreground(tcell.NewHexColor(int32(mathImages.idBase + id))), id
}

// gfxText is text with the image cells of formulas turned into the
// formulas on one line (yanked from the view).
func gfxText(s string) string {
	g := mathImages
	if g == nil || !strings.ContainsFunc(s, func(r rune) bool { return r > gfxBase && r <= gfxBase+gfxMaxID }) {
		return s
	}
	var sb strings.Builder
	prev := rune(-1)
	for _, r := range s {
		if r <= gfxBase || r > gfxBase+gfxMaxID {
			sb.WriteRune(r)
		} else if r != prev {
			if im := g.imgs[int(r-gfxBase)]; im != nil {
				sb.WriteString(im.alt)
			}
		}
		prev = r
	}
	return sb.String()
}

// withoutMathImages runs f with formulas laid out as text (output that
// leaves the view: a buffer of text).
func withoutMathImages(f func()) {
	g := mathImages
	mathImages = nil
	defer func() { mathImages = g }()
	f()
}

// mathFormulaBox lays out a formula typeset when the terminal shows
// images, as text otherwise.
func mathFormulaBox(content string, n *fnode, display bool) box {
	if g := mathImages; g != nil {
		if b, ok := g.formula(content, n, display); ok {
			return b
		}
	}
	return formulaBox(n)
}

// rowImages returns the ids of the images on a row of cells.
func rowImages(row []mcell) map[int]bool {
	var ids map[int]bool
	for _, c := range row {
		if id := gfxCellID(c); id != 0 {
			if ids == nil {
				ids = map[int]bool{}
			}
			ids[id] = true
		}
	}
	return ids
}

func gfxCellID(c mcell) int {
	r := []rune(c.s)
	if len(r) == 1 && r[0] > gfxBase && r[0] <= gfxBase+gfxMaxID {
		return int(r[0] - gfxBase)
	}
	return 0
}

// spaceFormulas sets off rows showing different formulas, which would
// touch: a blank row between them (as TeX's \lineskip), through which
// the bars of tables and quotes go on. The rows of one formula stay
// together.
func (g *mathGfx) spaceFormulas(rows []mrow) []mrow {
	out := make([]mrow, 0, len(rows))
	var prev map[int]bool
	for i, r := range rows {
		ids := rowImages(r.cells)
		if prev != nil && ids != nil && !g.continued(prev, ids) {
			out = append(out, spacerRow(rows[i-1], r))
		}
		out = append(out, r)
		prev = ids
	}
	return out
}

// continued reports a formula of the row above going on in this one.
func (g *mathGfx) continued(above, ids map[int]bool) bool {
	for id := range ids {
		if im := g.imgs[id]; im != nil && im.row > 0 && above[im.above] {
			return true
		}
	}
	return false
}

// spacerRow is a blank row between a and b keeping the vertical bars
// they share.
func spacerRow(a, b mrow) mrow {
	cells := blankRow(max(len(a.cells), len(b.cells)))
	for j := range min(len(a.cells), len(b.cells)) {
		if c := a.cells[j]; c == b.cells[j] && strings.Contains("│┃║|", c.s) && c.s != "" {
			cells[j] = c
		}
	}
	for len(cells) > 0 && cells[len(cells)-1] == blankCell {
		cells = cells[:len(cells)-1]
	}
	return mrow{cells: cells, src: a.src}
}

// isTypeset reports a box of image cells.
func isTypeset(b box) bool {
	return len(b.rows) > 0 && len(b.rows[0]) > 0 && gfxCellID(b.rows[0][0]) != 0
}

// mathGen changes when typeset formulas are drawn anew: the layouts
// holding them are stale.
func mathGen() int {
	if mathImages == nil {
		return 0
	}
	return mathImages.gen
}
