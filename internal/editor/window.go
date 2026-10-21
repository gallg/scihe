package editor

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/mattn/go-runewidth"

	"scihe/internal/buffer"
)

// Window is a view on a buffer.
type Window struct {
	id   int
	buf  *buffer.Buffer
	cur  buffer.Pos
	want int // desired display column for vertical moves; -1 = end of line
	top  int // first visible line
	skip int // wrapped rows of the top line scrolled past (very long lines)
	left int // first visible display column
	// pinTop keeps an explicitly chosen top line (zt/zz/zb) for one redraw
	pinTop bool
	// screen position of the cursor from the last draw (-1 if hidden)
	curX, curY int

	x, y, w, h int // screen rectangle including the status line

	jumps   []jump
	jumpIdx int
	alt     *buffer.Buffer
	// positions remembered per buffer when switching buffers in this window
	lastPos map[int]buffer.Pos
}

type jump struct {
	buf *buffer.Buffer
	pos buffer.Pos
}

var winIDs = 0

func (e *Editor) newWindow(b *buffer.Buffer) *Window {
	winIDs++
	return &Window{id: winIDs, buf: b, lastPos: map[int]buffer.Pos{}, curX: -1, curY: -1}
}

// textHeight is the number of rows for text (excluding status line).
func (w *Window) textHeight() int {
	if w.h <= 1 {
		return 1
	}
	return w.h - 1
}

// setBuffer switches the buffer shown in w.
func (e *Editor) setBuffer(w *Window, b *buffer.Buffer) {
	if w.buf == b {
		return
	}
	w.lastPos[w.buf.ID] = w.cur
	w.alt = w.buf
	w.buf = b
	w.cur = b.Clamp(w.lastPos[b.ID])
	w.top, w.left, w.want = 0, 0, 0
	e.afterEnter(w)
}

// afterEnter runs when a buffer becomes visible in a window.
func (e *Editor) afterEnter(w *Window) {
	if d := e.direds[w.buf.ID]; d != nil && w.cur.Line == 0 {
		// a fresh listing starts on the first entry after "../"
		w.cur = Pos{Line: min(2, w.buf.LineCount()-1), Col: diredNameCol}
	}
	e.clampCursor(w)
}

// ---------------------------------------------------------------- layout

// node is a layout tree node: either a window leaf or a split.
type node struct {
	win      *Window
	vertical bool // children arranged left-to-right
	kids     []*node
	weights  []float64
	parent   *node
}

func (e *Editor) forWindows(fn func(*Window)) {
	var walk func(*node)
	walk = func(n *node) {
		if n == nil {
			return
		}
		if n.win != nil {
			fn(n.win)
			return
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(e.root)
}

func (e *Editor) windows() []*Window {
	var out []*Window
	e.forWindows(func(w *Window) { out = append(out, w) })
	return out
}

func (e *Editor) findNode(w *Window) *node {
	var found *node
	var walk func(*node)
	walk = func(n *node) {
		if found != nil || n == nil {
			return
		}
		if n.win == w {
			found = n
			return
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(e.root)
	return found
}

// split opens b in a new window above / left of the current one (vim).
func (e *Editor) split(b *buffer.Buffer, vertical bool) *Window {
	return e.splitAt(b, vertical, false)
}

// splitAt opens b in a new window before or after the current one and
// focuses it.
func (e *Editor) splitAt(b *buffer.Buffer, vertical, after bool) *Window {
	cur := e.findNode(e.win)
	nw := e.newWindow(b)
	nw.cur = e.win.cur
	if b != e.win.buf {
		nw.cur = buffer.Pos{}
	}
	nw.top = e.win.top
	leaf := &node{win: nw}
	p := cur.parent
	if p != nil && p.vertical == vertical {
		idx := indexOf(p.kids, cur)
		if after {
			idx++
		}
		p.kids = insertNode(p.kids, idx, leaf)
		p.weights = insertWeight(p.weights, idx, 1)
		leaf.parent = p
		equalize(p)
	} else {
		// replace cur by a split containing [new, cur]
		old := &node{win: cur.win, parent: cur}
		leaf.parent = cur
		cur.win = nil
		cur.vertical = vertical
		cur.kids = []*node{leaf, old}
		if after {
			cur.kids = []*node{old, leaf}
		}
		cur.weights = []float64{1, 1}
	}
	e.prevWin = e.win
	e.win = nw
	e.afterEnter(nw)
	return nw
}

func indexOf(ns []*node, n *node) int {
	for i, x := range ns {
		if x == n {
			return i
		}
	}
	return -1
}

func insertNode(ns []*node, i int, n *node) []*node {
	ns = append(ns, nil)
	copy(ns[i+1:], ns[i:])
	ns[i] = n
	return ns
}

func insertWeight(ws []float64, i int, w float64) []float64 {
	ws = append(ws, 0)
	copy(ws[i+1:], ws[i:])
	ws[i] = w
	return ws
}

func equalize(n *node) {
	for i := range n.weights {
		n.weights[i] = 1
	}
}

func (e *Editor) equalizeAll() {
	var walk func(*node)
	walk = func(n *node) {
		if n.win != nil {
			return
		}
		equalize(n)
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(e.root)
}

// closeWindow removes w from the layout. It refuses to close the last window.
func (e *Editor) closeWindow(w *Window) bool {
	n := e.findNode(w)
	if n == nil || n.parent == nil {
		return false
	}
	p := n.parent
	i := indexOf(p.kids, n)
	p.kids = append(p.kids[:i], p.kids[i+1:]...)
	p.weights = append(p.weights[:i], p.weights[i+1:]...)
	if len(p.kids) == 1 {
		only := p.kids[0]
		p.win, p.vertical, p.kids, p.weights = only.win, only.vertical, only.kids, only.weights
		for _, k := range p.kids {
			k.parent = p
		}
	}
	if e.win == w {
		next := e.prevWin
		if next == nil || e.findNode(next) == nil {
			next = e.windows()[0]
		}
		e.win = next
	}
	if e.prevWin == w {
		e.prevWin = nil
	}
	return true
}

// onlyWindow closes all windows but the current one.
func (e *Editor) onlyWindow() {
	e.root = &node{win: e.win}
	e.prevWin = nil
}

// layout assigns screen rectangles to all windows.
func (e *Editor) layout(x, y, w, h int) {
	var place func(n *node, x, y, w, h int)
	place = func(n *node, x, y, w, h int) {
		if n.win != nil {
			n.win.x, n.win.y, n.win.w, n.win.h = x, y, w, h
			return
		}
		total := 0.0
		for _, wt := range n.weights {
			total += wt
		}
		size := h
		if n.vertical {
			size = w - (len(n.kids) - 1) // separators
		}
		pos := 0
		for i, k := range n.kids {
			sz := int(float64(size) * n.weights[i] / total)
			if i == len(n.kids)-1 {
				sz = size - pos
			}
			if sz < 1 {
				sz = 1
			}
			if n.vertical {
				place(k, x+pos+i, y, sz, h)
			} else {
				place(k, x, y+pos, w, sz)
			}
			pos += sz
		}
	}
	place(e.root, x, y, w, h)
}

// resizeWindow grows (delta>0) or shrinks the current window.
func (e *Editor) resizeWindow(vertical bool, delta int) {
	n := e.findNode(e.win)
	for n.parent != nil && n.parent.vertical != vertical {
		n = n.parent
	}
	p := n.parent
	if p == nil {
		return
	}
	i := indexOf(p.kids, n)
	if len(p.kids) < 2 {
		return
	}
	// work in cells: weights become the children's current sizes, the
	// change is taken from (or given to) the neighbouring child
	sizes := make([]float64, len(p.kids))
	for k, kid := range p.kids {
		sizes[k] = float64(e.nodeSize(kid, vertical))
	}
	j := i + 1
	if j >= len(p.kids) {
		j = i - 1
	}
	d := float64(delta)
	if sizes[i]+d < 1 {
		d = 1 - sizes[i]
	}
	if sizes[j]-d < 1 {
		d = sizes[j] - 1
	}
	sizes[i] += d
	sizes[j] -= d
	p.weights = sizes
}

// nodeSize returns the width (vertical) or height of a layout node.
func (e *Editor) nodeSize(n *node, vertical bool) int {
	lo, hi := 1<<30, -1
	var walk func(*node)
	walk = func(n *node) {
		if n.win != nil {
			a, b := n.win.y, n.win.y+n.win.h
			if vertical {
				a, b = n.win.x, n.win.x+n.win.w
			}
			lo, hi = min(lo, a), max(hi, b)
			return
		}
		for _, k := range n.kids {
			walk(k)
		}
	}
	walk(n)
	if hi < lo {
		return 1
	}
	return hi - lo
}

// neighbor returns the window in direction (dx, dy) from w.
func (e *Editor) neighbor(w *Window, dx, dy int) *Window {
	cx, cy := w.x+w.w/2, w.y+w.h/2
	sx, sy := e.cursorScreen(w)
	if sx >= 0 {
		if dx != 0 {
			cy = sy
		} else {
			cx = sx
		}
	}
	var best *Window
	bestDist := 1 << 30
	for _, o := range e.windows() {
		if o == w {
			continue
		}
		var ok bool
		var dist int
		switch {
		case dx < 0:
			ok = o.x+o.w <= w.x && cy >= o.y && cy < o.y+o.h
			dist = w.x - (o.x + o.w)
		case dx > 0:
			ok = o.x >= w.x+w.w && cy >= o.y && cy < o.y+o.h
			dist = o.x - (w.x + w.w)
		case dy < 0:
			ok = o.y+o.h <= w.y && cx >= o.x && cx <= o.x+o.w
			dist = w.y - (o.y + o.h)
		case dy > 0:
			ok = o.y >= w.y+w.h && cx >= o.x && cx <= o.x+o.w
			dist = o.y - (w.y + w.h)
		}
		if ok && dist < bestDist {
			best, bestDist = o, dist
		}
	}
	return best
}

func (e *Editor) focus(w *Window) {
	if w == nil || w == e.win {
		return
	}
	if e.mode.visual() {
		e.exitVisual()
	}
	e.prevWin = e.win
	e.win = w
	e.clampCursor(w)
}

// ---------------------------------------------------------------- columns

// displayCol returns the display column of rune index col in line.
func displayCol(line []rune, col, ts int) int {
	vc := 0
	for i := 0; i < col && i < len(line); i++ {
		vc += runeWidth(line[i], vc, ts)
	}
	if col > len(line) {
		vc += col - len(line)
	}
	return vc
}

// colForDisplay returns the rune index at display column vc.
func colForDisplay(line []rune, vc, ts int) int {
	c := 0
	for i, r := range line {
		w := runeWidth(r, c, ts)
		if c+w > vc {
			return i
		}
		c += w
	}
	return len(line)
}

// cellWidth is the display width of the character at p (1 at end of line).
func cellWidth(b *buffer.Buffer, p buffer.Pos, vc, ts int) int {
	if p.Col >= b.LineLen(p.Line) {
		return 1
	}
	return runeWidth(b.RuneAt(p), vc, ts)
}

func runeWidth(r rune, vc, ts int) int {
	if r == '\t' {
		if ts <= 0 {
			ts = 8
		}
		return ts - vc%ts
	}
	if r < 32 || r == 0x7f {
		return 2 // shown as ^X / ^?
	}
	w := runewidth.RuneWidth(r)
	if w == 0 {
		return 1
	}
	return w
}

// clampCursor keeps the cursor inside the buffer for the current mode.
func (e *Editor) clampCursor(w *Window) {
	w.cur = w.buf.Clamp(w.cur)
	allowEnd := w == e.win && (e.mode == ModeInsert || e.mode == ModeReplace || e.mode.visual())
	if !allowEnd {
		if n := w.buf.LineLen(w.cur.Line); w.cur.Col >= n && n > 0 {
			w.cur.Col = n - 1
		}
	}
}

// setCursor moves the cursor and updates the desired column.
func (e *Editor) setCursor(w *Window, p buffer.Pos) {
	w.cur = p
	e.clampCursor(w)
	w.want = displayCol(w.buf.Line(w.cur.Line), w.cur.Col, e.options(w.buf).TabStop)
}

// pushJump records the current position in the jump list.
func (e *Editor) pushJump(w *Window) {
	j := jump{w.buf, w.cur}
	if w.jumpIdx < len(w.jumps) {
		w.jumps = w.jumps[:w.jumpIdx]
	}
	if n := len(w.jumps); n > 0 && w.jumps[n-1] == j {
		return
	}
	w.jumps = append(w.jumps, j)
	if len(w.jumps) > 100 {
		w.jumps = w.jumps[1:]
	}
	w.jumpIdx = len(w.jumps)
	w.buf.Marks['\''] = w.cur
}

func (e *Editor) jumpBack(w *Window, n int) {
	if w.jumpIdx == len(w.jumps) {
		e.pushJump(w)
		w.jumpIdx--
	}
	idx := w.jumpIdx - n
	if idx < 0 || idx >= len(w.jumps) {
		return
	}
	w.jumpIdx = idx
	j := w.jumps[idx]
	if !e.bufferAlive(j.buf) {
		return
	}
	if j.buf != w.buf {
		e.setBuffer(w, j.buf)
	}
	e.setCursor(w, j.buf.Clamp(j.pos))
}

func (e *Editor) bufferAlive(b *buffer.Buffer) bool {
	for _, x := range e.buffers {
		if x == b {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- tmux-like resize

// moveBorder moves a border of the current window by delta cells: to the
// right / down for delta > 0. Like tmux, the right (bottom) border moves
// when there is one, else the left (top) border. vertical selects the
// left-right direction. It reports whether anything moved.
func (e *Editor) moveBorder(vertical bool, delta int) bool {
	type spot struct {
		p *node
		i int
	}
	var after, before []spot // splits where the window has a neighbour after / before
	for n := e.findNode(e.win); n != nil && n.parent != nil; n = n.parent {
		p := n.parent
		if p.vertical != vertical {
			continue
		}
		i := indexOf(p.kids, n)
		if i < len(p.kids)-1 {
			after = append(after, spot{p, i})
		}
		if i > 0 {
			before = append(before, spot{p, i - 1})
		}
	}
	var s spot
	switch {
	case len(after) > 0:
		s = after[0]
	case len(before) > 0:
		s = before[0]
	default:
		return false
	}
	// the border between kids a and a+1 moves by delta
	sizes := make([]float64, len(s.p.kids))
	for k, kid := range s.p.kids {
		sizes[k] = float64(e.nodeSize(kid, vertical))
	}
	a, b := s.i, s.i+1
	d := float64(delta)
	if sizes[a]+d < 1 {
		d = 1 - sizes[a]
	}
	if sizes[b]-d < 1 {
		d = sizes[b] - 1
	}
	if d == 0 {
		return false
	}
	sizes[a] += d
	sizes[b] -= d
	s.p.weights = sizes
	return true
}

// resizeKey maps keys accepted in resize mode to a direction and step:
// h j k l, arrows and their Ctrl forms move 1 cell, Alt forms 5.
func resizeKey(k Key) (vertical bool, dir, step int, ok bool) {
	key := string(k)
	step = 1
	if strings.HasPrefix(key, "<M-") {
		step = 5
		key = "<" + key[3:]
		if len(key) == 3 { // <M-h> -> h
			key = key[1:2]
		}
	} else if strings.HasPrefix(key, "<C-") {
		key = "<" + key[3:]
		if len(key) == 3 { // <C-h> -> h
			key = key[1:2]
		}
	}
	switch key {
	case "h", "<Left>", "<BS>":
		return true, -1, step, true
	case "l", "<Right>":
		return true, 1, step, true
	case "j", "<Down>":
		return false, 1, step, true
	case "k", "<Up>":
		return false, -1, step, true
	}
	return false, 0, 0, false
}

const resizeRepeat = time.Second

func resizeLabel(vertical bool, dir, step int) string {
	s := "resize: border "
	switch {
	case vertical && dir < 0:
		s += "left"
	case vertical:
		s += "right"
	case dir > 0:
		s += "down"
	default:
		s += "up"
	}
	if step > 1 {
		s += fmt.Sprintf(" ×%d", step)
	}
	return s
}

// resizeStep moves a border by delta cells (0: just enter resize mode)
// and (re)starts resize mode.
func (e *Editor) resizeStep(vertical bool, delta int) {
	if delta != 0 && !e.moveBorder(vertical, delta) {
		e.infof("-- RESIZE -- (no border to move that way)")
	} else {
		e.infof("-- RESIZE --  h/j/k/l or arrows move the border, Alt ×5; any other key ends")
	}
	e.resizeUntil = time.Now().Add(resizeRepeat)
}

// resizeModeKey handles a key while resize mode is on; it reports
// whether the key was used.
func (e *Editor) resizeModeKey(k Key) bool {
	if e.resizeUntil.IsZero() {
		return false
	}
	if e.mode != ModeNormal || time.Now().After(e.resizeUntil) || len(e.pending) > 0 {
		e.endResize()
		return false
	}
	vertical, dir, step, ok := resizeKey(k)
	if !ok {
		e.endResize()
		return false
	}
	e.resizeStep(vertical, dir*step)
	return true
}

func (e *Editor) endResize() {
	if !e.resizeUntil.IsZero() {
		e.resizeUntil = time.Time{}
		if strings.HasPrefix(e.msg, "-- RESIZE --") {
			e.msg = ""
		}
	}
}

// ---------------------------------------------------------------- layouts

// layoutNames are tmux's preset layouts, in its cycling order.
var layoutNames = []string{"even-horizontal", "even-vertical", "main-horizontal", "main-vertical", "tiled"}

// applyLayout rebuilds the window tree as a preset; the current window
// is the main one of the main-* layouts and keeps focus.
func (e *Editor) applyLayout(name string) error {
	wins := e.windows()
	if len(wins) < 2 {
		return errors.New("only one window: nothing to arrange")
	}
	main := e.win
	others := make([]*Window, 0, len(wins)-1)
	for _, w := range wins {
		if w != main {
			others = append(others, w)
		}
	}
	leaves := func(ws []*Window) []*node {
		out := make([]*node, len(ws))
		for i, w := range ws {
			out[i] = &node{win: w}
		}
		return out
	}
	split := func(vertical bool, kids []*node, weights ...float64) *node {
		if len(kids) == 1 {
			return kids[0]
		}
		n := &node{vertical: vertical, kids: kids}
		for i, k := range kids {
			k.parent = n
			wt := 1.0
			if i < len(weights) {
				wt = weights[i]
			}
			n.weights = append(n.weights, wt)
		}
		return n
	}
	var root *node
	switch name {
	case "even-horizontal":
		root = split(true, leaves(wins))
	case "even-vertical":
		root = split(false, leaves(wins))
	case "main-vertical": // main on the left, the others stacked on the right
		root = split(true, []*node{{win: main}, split(false, leaves(others))}, 3, 2)
	case "main-horizontal": // main on top, the others side by side below
		root = split(false, []*node{{win: main}, split(true, leaves(others))}, 3, 2)
	case "tiled":
		cols := int(math.Ceil(math.Sqrt(float64(len(wins)))))
		var rows []*node
		for i := 0; i < len(wins); i += cols {
			rows = append(rows, split(true, leaves(wins[i:min(i+cols, len(wins))])))
		}
		root = split(false, rows)
	default:
		return fmt.Errorf("unknown layout %q (%s)", name, strings.Join(layoutNames, ", "))
	}
	root.parent = nil
	e.root = root
	e.layoutName = name
	e.layoutNow()
	return nil
}

// exLayout: ":layout [next | prev | NAME]".
func exLayout(e *Editor, a *exArgs) error {
	arg := strings.TrimSpace(a.arg)
	idx := -1
	for i, n := range layoutNames {
		if n == e.layoutName {
			idx = i
		}
	}
	switch arg {
	case "", "next":
		arg = layoutNames[(idx+1)%len(layoutNames)]
	case "prev", "previous":
		if idx < 0 {
			idx = 0
		}
		arg = layoutNames[(idx-1+len(layoutNames))%len(layoutNames)]
	}
	if err := e.applyLayout(arg); err != nil {
		return err
	}
	e.infof("layout: %s", arg)
	return nil
}
