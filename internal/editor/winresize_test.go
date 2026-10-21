package editor

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestResizeModeHorizontal(t *testing.T) {
	e := newTestEditor(t, "a\nb\nc\nd")
	typeKeys(e, ":split<CR>") // new window on top, focused
	e.draw()
	top := e.win
	h := top.h
	typeKeys(e, "<C-w><C-j>") // border down: the top window grows
	e.draw()
	if top.h != h+1 {
		t.Fatalf("<C-w><C-j>: %d -> %d", h, top.h)
	}
	if !strings.HasPrefix(e.msg, "-- RESIZE --") {
		t.Fatalf("msg %q", e.msg)
	}
	// repeat without <C-w>, Ctrl held or not
	typeKeys(e, "<C-j>jj<Down>")
	e.draw()
	if top.h != h+5 {
		t.Fatalf("repeat: %d", top.h)
	}
	typeKeys(e, "k")
	e.draw()
	if top.h != h+4 {
		t.Fatalf("k: %d", top.h)
	}
	// any other key ends resize mode and does its normal job
	typeKeys(e, "x")
	if e.win.buf.LineString(0) != "" || !e.resizeUntil.IsZero() {
		t.Fatalf("x after resize: %q", e.win.buf.LineString(0))
	}
	typeKeys(e, "j")
	e.draw()
	if top.h != h+4 || e.win.cur.Line != 1 {
		t.Fatal("j moved a border after resize mode ended")
	}
	// Alt resizes by 5, a count multiplies
	typeKeys(e, "<C-w><M-Up>")
	e.draw()
	if top.h != h-1 {
		t.Fatalf("<M-Up>: %d", top.h)
	}
	e.endResize()
	typeKeys(e, "3<C-w><C-j>")
	e.draw()
	if top.h != h+2 {
		t.Fatalf("count: %d", top.h)
	}
	// the mode ends after a pause
	e.resizeUntil = time.Now().Add(-time.Millisecond)
	e.tick()
	if !e.resizeUntil.IsZero() || e.msg != "" {
		t.Fatalf("timeout: %v %q", e.resizeUntil, e.msg)
	}
	// windows never shrink below one row
	typeKeys(e, "<C-w><C-k>")
	for i := 0; i < 50; i++ {
		typeKeys(e, "k")
	}
	e.draw()
	if top.h < 1 || top.h > 2 {
		t.Fatalf("minimum: %d", top.h)
	}
}

func TestResizeModeVertical(t *testing.T) {
	e := newTestEditor(t, "a")
	typeKeys(e, ":vsplit<CR>") // new window on the left, focused
	e.draw()
	left := e.win
	w := left.w
	typeKeys(e, "<C-w><C-l>")
	e.draw()
	if left.w != w+1 {
		t.Fatalf("left window: %d -> %d", w, left.w)
	}
	// in the right window (no neighbour on its right) → moves its left border
	e.endResize()
	typeKeys(e, "<C-w>l")
	right := e.win
	rw := right.w
	typeKeys(e, "<C-w><C-Right>")
	e.draw()
	if right.w != rw-1 || left.w != w+2 {
		t.Fatalf("right window: %d -> %d (left %d)", rw, right.w, left.w)
	}
	// Ctrl-h may arrive as <BS>
	typeKeys(e, "<Esc><C-w><BS>")
	e.draw()
	if right.w != rw {
		t.Fatalf("<BS>: %d", right.w)
	}
	// no split in that direction
	typeKeys(e, "<Esc><C-w><C-j>")
	if !strings.Contains(e.msg, "no border") {
		t.Fatalf("msg %q", e.msg)
	}
	// plain <C-w>h still moves focus
	e.endResize()
	typeKeys(e, "<C-w>h")
	if e.win != left {
		t.Fatal("<C-w>h focus")
	}
}

func TestLayouts(t *testing.T) {
	e := newTestEditor(t, "a")
	typeKeys(e, ":layout<CR>")
	if !strings.Contains(e.msg, "only one window") {
		t.Fatalf("msg %q", e.msg)
	}
	typeKeys(e, ":split<CR>:split<CR>:vsplit<CR>")
	cur := e.win
	cur.cur = Pos{0, 0}
	bufs := map[*Window]bool{}
	for _, w := range e.windows() {
		bufs[w] = true
	}
	check := func(name string, f func(ws []*Window) bool) {
		t.Helper()
		e.draw()
		ws := e.windows()
		if len(ws) != 4 || e.win != cur {
			t.Fatalf("%s: %d windows, focus kept %v", name, len(ws), e.win == cur)
		}
		for _, w := range ws {
			if !bufs[w] || w.w < 1 || w.h < 1 {
				t.Fatalf("%s: window lost or empty", name)
			}
		}
		if !f(ws) {
			var sb strings.Builder
			for _, w := range ws {
				fmt.Fprintf(&sb, "[%d,%d %dx%d] ", w.x, w.y, w.w, w.h)
			}
			t.Fatalf("%s geometry: %s", name, sb.String())
		}
	}
	typeKeys(e, "<C-w><Space>")
	check("even-horizontal", func(ws []*Window) bool {
		for _, w := range ws {
			if w.y != 0 || w.h != ws[0].h {
				return false
			}
		}
		return true
	})
	typeKeys(e, "<C-w><Space>")
	check("even-vertical", func(ws []*Window) bool {
		for _, w := range ws {
			if w.x != 0 || w.w != 100 {
				return false
			}
		}
		return true
	})
	typeKeys(e, ":layout next<CR>") // main-horizontal
	check("main-horizontal", func(ws []*Window) bool {
		return cur.y == 0 && cur.w == 100 && cur.h > 14 && ws[1].y > 0 && ws[1].y == ws[3].y
	})
	typeKeys(e, "<Space>w<Space>") // main-vertical
	check("main-vertical", func(ws []*Window) bool {
		return cur.x == 0 && cur.h == 29 && cur.w > 50 && ws[1].x > 0 && ws[1].x == ws[3].x
	})
	typeKeys(e, ":layout tiled<CR>")
	check("tiled", func(ws []*Window) bool {
		return ws[0].y == ws[1].y && ws[2].y == ws[3].y && ws[0].y != ws[2].y && ws[0].x == ws[2].x
	})
	typeKeys(e, ":layout prev<CR>")
	if e.layoutName != "main-vertical" {
		t.Fatalf("prev -> %s", e.layoutName)
	}
	typeKeys(e, ":layout bogus<CR>")
	if !strings.Contains(e.msg, "unknown layout") {
		t.Fatalf("msg %q", e.msg)
	}
	// closing a window still works on a rebuilt tree
	typeKeys(e, ":close<CR>")
	if len(e.windows()) != 3 {
		t.Fatal("close after layout")
	}
	typeKeys(e, "<C-w><C-l>")
	e.draw()
}
