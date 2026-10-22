package editor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"scihe/internal/buffer"
	"scihe/internal/keymap"
)

// setupKeymaps builds the default key bindings for every mode.
func (e *Editor) setupKeymaps() {
	e.normalMap = keymap.NewMap[*Cmd]()
	e.visualMap = keymap.NewMap[*Cmd]()
	e.opMap = keymap.NewMap[*Cmd]()
	e.insertMap = keymap.NewMap[*Cmd]()
	L := e.leader

	// bind helpers
	nm := func(keys, label string, c *Cmd) { e.normalMap.Bind(keymap.Parse(keys, L), label, c) }
	vm := func(keys, label string, c *Cmd) { e.visualMap.Bind(keymap.Parse(keys, L), label, c) }
	om := func(keys, label string, c *Cmd) { e.opMap.Bind(keymap.Parse(keys, L), label, c) }
	// motions are shared by normal, visual and operator-pending modes
	mo := func(keys, label string, c *Cmd) { nm(keys, label, c); vm(keys, label, c); om(keys, label, c) }

	// ---- motions
	mo("h", "left", motion(mLeft(false)))
	mo("<Left>", "left", motion(mLeft(false)))
	mo("<BS>", "left (wrap)", motion(mLeft(true)))
	mo("l", "right", motion(mRight(false)))
	mo("<Right>", "right", motion(mRight(false)))
	if L == "<Space>" {
		om("<Space>", "right (wrap)", motion(mRight(true)))
	} else {
		mo("<Space>", "right (wrap)", motion(mRight(true)))
	}
	mo("j", "down", vmotion(mDown))
	mo("<Down>", "down", vmotion(mDown))
	mo("<C-n>", "down", vmotion(mDown))
	mo("gj", "down (screen row)", motion(mDisplayDown))
	mo("k", "up", vmotion(mUp))
	mo("<Up>", "up", vmotion(mUp))
	mo("<C-p>", "up", vmotion(mUp))
	mo("gk", "up (screen row)", motion(mDisplayUp))
	mo("w", "next word", motion(mWord(false)))
	mo("W", "next WORD", motion(mWord(true)))
	mo("b", "previous word", motion(mWordBack(false)))
	mo("B", "previous WORD", motion(mWordBack(true)))
	mo("e", "end of word", motion(mWordEnd(false)))
	mo("E", "end of WORD", motion(mWordEnd(true)))
	mo("ge", "end of previous word", motion(mWordEndBack(false)))
	mo("gE", "end of previous WORD", motion(mWordEndBack(true)))
	mo("0", "line start", motion(mLineStart))
	mo("<Home>", "line start", motion(mLineStart))
	mo("^", "first non-blank", motion(mFirstNonBlank))
	eol := motion(mLineEnd)
	eol.EOL = true
	mo("$", "line end", eol)
	mo("<End>", "line end", eol)
	mo("g_", "last non-blank", motion(mLastNonBlank))
	mo("|", "column", motion(mColumn))
	mo("+", "next line start", motion(mLineDownFirst))
	mo("<CR>", "next line start", motion(mLineDownFirst))
	mo("-", "previous line start", motion(mLineUpFirst))
	mo("_", "current line start", motion(mCurLineFirst))
	mo("gg", "first line", jumpMotion(mGotoFirst))
	mo("G", "last line", jumpMotion(mGotoLast))
	mo("H", "screen top", jumpMotion(mScreenTop))
	mo("M", "screen middle", jumpMotion(mScreenMiddle))
	mo("L", "screen bottom", jumpMotion(mScreenBottom))
	mo("f", "find char", charMotion(mFind(true, false)))
	mo("F", "find char backward", charMotion(mFind(false, false)))
	mo("t", "till char", charMotion(mFind(true, true)))
	mo("T", "till char backward", charMotion(mFind(false, true)))
	mo(";", "repeat f/t", motion(mRepeatFind(false)))
	mo(",", "repeat f/t reversed", motion(mRepeatFind(true)))
	mo("%", "matching bracket", jumpMotion(mPercent))
	mo("}", "next paragraph", jumpMotion(mParagraph(true)))
	mo("{", "previous paragraph", jumpMotion(mParagraph(false)))
	mo("]]", "next section / top form", jumpMotion(mSection(true)))
	mo("[[", "previous section / top form", jumpMotion(mSection(false)))
	mo("]c", "next cell", motion(mCell(true)))
	mo("[c", "previous cell", motion(mCell(false)))
	mo("n", "next match", jumpMotion(mSearchNext(false)))
	mo("N", "previous match", jumpMotion(mSearchNext(true)))
	mo("*", "search word forward", jumpMotion(mStar(true, true)))
	mo("#", "search word backward", jumpMotion(mStar(false, true)))
	mo("g*", "search word forward (partial)", jumpMotion(mStar(true, false)))
	mo("g#", "search word backward (partial)", jumpMotion(mStar(false, false)))
	mark := charMotion(mMark(false))
	mark.Jump = true
	markExact := charMotion(mMark(true))
	markExact.Jump = true
	mo("'", "go to mark line", mark)
	mo("`", "go to mark", markExact)
	searchF := &Cmd{Kind: KindMotion, Action: func(c *ctx) { c.e.startCmdline('/', "") }}
	searchB := &Cmd{Kind: KindMotion, Action: func(c *ctx) { c.e.startCmdline('?', "") }}
	mo("/", "search forward", searchF)
	mo("?", "search backward", searchB)

	// ---- text objects (operator-pending and visual)
	to := func(keys, label string, f TextObjFunc) {
		om(keys, label, textObj(f))
		vm(keys, label, textObj(f))
	}
	e.opMap.SetGroup(keymap.Parse("i", L), "inner")
	e.opMap.SetGroup(keymap.Parse("a", L), "around")
	e.visualMap.SetGroup(keymap.Parse("i", L), "inner")
	e.visualMap.SetGroup(keymap.Parse("a", L), "around")
	to("iw", "word", toWord(false, false))
	to("aw", "word", toWord(false, true))
	to("iW", "WORD", toWord(true, false))
	to("aW", "WORD", toWord(true, true))
	for _, p := range []struct {
		keys        []string
		open, close rune
		label       string
	}{
		{[]string{"(", ")", "b"}, '(', ')', "( ) block"},
		{[]string{"[", "]"}, '[', ']', "[ ] block"},
		{[]string{"{", "}", "B"}, '{', '}', "{ } block"},
		{[]string{"<lt>", ">"}, '<', '>', "< > block"},
	} {
		for _, k := range p.keys {
			to("i"+k, p.label, toPair(p.open, p.close, false))
			to("a"+k, p.label, toPair(p.open, p.close, true))
		}
	}
	for _, q := range []rune{'"', '\'', '`'} {
		to("i"+string(q), string(q)+" string", toQuote(q, false))
		to("a"+string(q), string(q)+" string", toQuote(q, true))
	}
	to("ip", "paragraph", toParagraph(false))
	to("ap", "paragraph", toParagraph(true))
	to("ie", "entire buffer", toEntire)
	to("ae", "entire buffer", toEntire)
	to("ic", "notebook cell", toCell(false))
	to("ac", "notebook cell (with marker)", toCell(true))
	to("if", "lisp form contents", toForm(false))
	to("af", "lisp form", toForm(true))

	// ---- operators
	ops := []struct {
		keys  string
		label string
		op    Operator
	}{
		{"d", "delete", OpDelete}, {"c", "change", OpChange}, {"y", "yank", OpYank},
		{">", "indent", OpIndent}, {"<lt>", "dedent", OpDedent},
		{"gu", "lowercase", OpLower}, {"gU", "uppercase", OpUpper}, {"g~", "toggle case", OpToggleCase},
		{"gc", "toggle comment", OpComment}, {"gq", "format text", OpFormat},
	}
	for _, o := range ops {
		nm(o.keys, o.label, operator(o.op))
		vm(o.keys, o.label, operator(o.op))
	}
	vm("x", "delete", operator(OpDelete))
	vm("<Del>", "delete", operator(OpDelete))
	vm("s", "change", operator(OpChange))
	vm("u", "lowercase", operator(OpLower))
	vm("U", "uppercase", operator(OpUpper))
	vm("~", "toggle case", operator(OpToggleCase))
	e.normalMap.SetGroup(keymap.Parse("g", L), "goto / misc")
	e.visualMap.SetGroup(keymap.Parse("g", L), "goto / misc")
	e.normalMap.SetGroup(keymap.Parse("z", L), "scroll / view")
	e.normalMap.SetGroup(keymap.Parse("[", L), "previous")
	e.normalMap.SetGroup(keymap.Parse("]", L), "next")
	e.normalMap.SetGroup(keymap.Parse("<C-w>", L), "window")
	e.normalMap.SetGroup(keymap.Parse("Z", L), "quit")

	// ---- normal-mode actions
	nm("i", "insert", change(func(c *ctx) { c.e.enterInsertKind('i', c.count) }))
	nm("a", "append", change(func(c *ctx) {
		if c.b.LineLen(c.w.cur.Line) > 0 {
			c.w.cur.Col++
		}
		c.e.enterInsertKind('a', c.count)
	}))
	nm("I", "insert at line start", change(func(c *ctx) {
		c.w.cur.Col = firstNonBlank(c.b, c.w.cur.Line)
		if c.b.LineLen(c.w.cur.Line) == 0 {
			c.w.cur.Col = 0
		}
		c.e.enterInsertKind('I', c.count)
	}))
	nm("gI", "insert at column 0", change(func(c *ctx) { c.w.cur.Col = 0; c.e.enterInsertKind('I', c.count) }))
	nm("A", "append at line end", change(func(c *ctx) {
		c.w.cur.Col = c.b.LineLen(c.w.cur.Line)
		c.e.enterInsertKind('A', c.count)
	}))
	nm("gi", "insert at last insert position", change(func(c *ctx) {
		if p, ok := c.b.Marks['^']; ok {
			c.w.cur = c.b.Clamp(p)
		}
		c.e.enterInsert()
	}))
	nm("o", "open line below", change(func(c *ctx) {
		if c.b.ReadOnly {
			c.e.errorf("E21: Cannot make changes, buffer is read-only")
			return
		}
		c.e.openLine(c.w, true)
		c.e.enterInsertKind('o', c.count)
	}))
	nm("O", "open line above", change(func(c *ctx) {
		if c.b.ReadOnly {
			c.e.errorf("E21: Cannot make changes, buffer is read-only")
			return
		}
		c.e.openLine(c.w, false)
		c.e.enterInsertKind('O', c.count)
	}))
	nm("x", "delete char", change(func(c *ctx) { c.e.deleteChars(c, true) }))
	nm("<Del>", "delete char", change(func(c *ctx) { c.e.deleteChars(c, true) }))
	nm("X", "delete char before", change(func(c *ctx) { c.e.deleteChars(c, false) }))
	nm("s", "substitute char", change(func(c *ctx) {
		c.e.deleteChars(c, true)
		c.e.enterInsert()
	}))
	nm("S", "substitute line", change(func(c *ctx) { c.e.feedOperatorLine(c, OpChange) }))
	nm("C", "change to end of line", change(func(c *ctx) { c.e.toEOL(c, OpChange) }))
	nm("D", "delete to end of line", change(func(c *ctx) { c.e.toEOL(c, OpDelete) }))
	nm("Y", "yank line", action(func(c *ctx) { c.e.feedOperatorLine(c, OpYank) }))
	nm("r", "replace char", charChange(func(c *ctx) { c.e.replaceChars(c) }))
	nm("R", "replace mode", change(func(c *ctx) { c.e.enterReplace(c.count) }))
	nm("J", "join lines", change(func(c *ctx) { c.e.joinLines(c.w, max(c.count, 2), true) }))
	nm("gJ", "join lines (no spaces)", change(func(c *ctx) { c.e.joinLines(c.w, max(c.count, 2), false) }))
	nm("~", "toggle case", change(func(c *ctx) { c.e.tildeChars(c) }))
	nm("p", "put after", change(func(c *ctx) { c.e.putReg(c, true, false) }))
	nm("P", "put before", change(func(c *ctx) { c.e.putReg(c, false, false) }))
	nm("gp", "put after, cursor after", change(func(c *ctx) { c.e.putReg(c, true, true) }))
	nm("gP", "put before, cursor after", change(func(c *ctx) { c.e.putReg(c, false, true) }))
	nm("u", "undo", noUndo(func(c *ctx) { c.e.undo(c, false) }))
	nm("<C-r>", "redo", noUndo(func(c *ctx) { c.e.undo(c, true) }))
	nm(".", "repeat last change", noUndo(func(c *ctx) { c.e.repeatDot(c) }))
	nm("<C-a>", "increment number", change(func(c *ctx) { c.e.incrementNumber(c, c.count) }))
	nm("<C-x>", "decrement number", change(func(c *ctx) { c.e.incrementNumber(c, -c.count) }))
	nm("v", "visual", action(func(c *ctx) { c.e.enterVisual(ModeVisual) }))
	nm("V", "visual line", action(func(c *ctx) { c.e.enterVisual(ModeVisualLine) }))
	nm("<C-v>", "visual block", action(func(c *ctx) { c.e.enterVisual(ModeVisualBlock) }))
	nm("gv", "reselect", action(func(c *ctx) { c.e.reselect() }))
	nm(":", "command line", action(func(c *ctx) {
		init := ""
		if c.hasCount {
			init = ".,.+" + strconv.Itoa(c.count-1)
			if c.count == 1 {
				init = "."
			}
		}
		c.e.startCmdline(':', init)
	}))
	// "q" while recording stops the recording (handled in normalKey)
	nm("q", "record macro", &Cmd{Kind: KindAction, NeedsChar: true, NoUndo: true, Action: func(c *ctx) { c.e.toggleMacro(c) }})
	nm("@", "play macro", &Cmd{Kind: KindAction, NeedsChar: true, NoUndo: true, Action: func(c *ctx) { c.e.playMacro(c) }})
	nm("m", "set mark", charAction(func(c *ctx) {
		if c.char >= 'a' && c.char <= 'z' || c.char == '\'' || c.char == '`' {
			r := c.char
			if r == '`' {
				r = '\''
			}
			c.b.Marks[r] = c.w.cur
		}
	}))
	nm("&", "repeat :s", change(func(c *ctx) { c.e.execEx("s") }))
	nm("g&", "repeat :s on all lines", change(func(c *ctx) { c.e.execEx("%&&") }))
	nm("<C-o>", "jump back", action(func(c *ctx) { c.e.jumpBack(c.w, c.count) }))
	nm("<C-i>", "jump forward", action(func(c *ctx) { c.e.jumpBack(c.w, -c.count) }))
	nm("<Tab>", "jump forward", action(func(c *ctx) { c.e.jumpBack(c.w, -c.count) }))
	nm("<C-^>", "alternate buffer", action(func(c *ctx) {
		if c.w.alt != nil && c.e.bufferAlive(c.w.alt) {
			c.e.setBuffer(c.w, c.w.alt)
		} else {
			c.e.errorf("E23: No alternate file")
		}
	}))
	nm("<C-g>", "file info", action(func(c *ctx) { c.e.fileInfo() }))
	nm("g<C-g>", "word count", action(func(c *ctx) { c.e.wordCount() }))
	nm("<C-l>", "redraw", action(func(c *ctx) { c.e.screen.Sync(); c.e.search.noHL = true }))
	nm("<C-z>", "suspend", action(func(c *ctx) { c.e.suspend() }))
	nm("<Esc>", "cancel", action(func(c *ctx) {}))
	nm("ZZ", "write and quit", action(func(c *ctx) { c.e.execEx("x") }))
	nm("ZQ", "quit without writing", action(func(c *ctx) { c.e.execEx("q!") }))
	nm("gf", "go to file under cursor", action(func(c *ctx) { c.e.gotoFile(c) }))
	nm("gx", "open URL/file with system app", action(func(c *ctx) { c.e.openExternal(c) }))
	nm("K", "documentation for symbol", action(func(c *ctx) { c.e.docLookup() }))
	nm("ga", "print char code", action(func(c *ctx) {
		r := c.b.RuneAt(c.w.cur)
		c.e.infof("<%c> %d, Hex %04x", r, r, r)
	}))

	// scrolling
	nm("<C-e>", "scroll down", action(func(c *ctx) { c.e.scrollLines(c.w, c.count) }))
	nm("<C-y>", "scroll up", action(func(c *ctx) { c.e.scrollLines(c.w, -c.count) }))
	nm("<C-d>", "half page down", action(func(c *ctx) { c.e.scrollHalf(c.w, true) }))
	nm("<C-u>", "half page up", action(func(c *ctx) { c.e.scrollHalf(c.w, false) }))
	nm("<C-f>", "page down", action(func(c *ctx) { c.e.scrollPage(c.w, c.count) }))
	nm("<PageDown>", "page down", action(func(c *ctx) { c.e.scrollPage(c.w, c.count) }))
	nm("<C-b>", "page up", action(func(c *ctx) { c.e.scrollPage(c.w, -c.count) }))
	nm("<PageUp>", "page up", action(func(c *ctx) { c.e.scrollPage(c.w, -c.count) }))
	nm("zz", "center cursor line", action(func(c *ctx) { c.e.scrollTo(c.w, "middle") }))
	nm("z.", "center cursor line", action(func(c *ctx) { c.e.scrollTo(c.w, "middle") }))
	nm("zt", "cursor line to top", action(func(c *ctx) { c.e.scrollTo(c.w, "top") }))
	nm("z<CR>", "cursor line to top", action(func(c *ctx) { c.e.scrollTo(c.w, "top") }))
	nm("zb", "cursor line to bottom", action(func(c *ctx) { c.e.scrollTo(c.w, "bottom") }))
	nm("z-", "cursor line to bottom", action(func(c *ctx) { c.e.scrollTo(c.w, "bottom") }))
	nm("zh", "scroll left", action(func(c *ctx) {
		if !c.e.wraps(c.w) {
			c.w.left = max(0, c.w.left-c.count)
		}
	}))
	nm("zl", "scroll right", action(func(c *ctx) {
		if !c.e.wraps(c.w) {
			c.w.left += c.count
		}
	}))
	vm("<C-d>", "half page down", action(func(c *ctx) { c.e.scrollHalf(c.w, true) }))
	vm("<C-u>", "half page up", action(func(c *ctx) { c.e.scrollHalf(c.w, false) }))
	vm("<C-f>", "page down", action(func(c *ctx) { c.e.scrollPage(c.w, c.count) }))
	vm("<C-b>", "page up", action(func(c *ctx) { c.e.scrollPage(c.w, -c.count) }))

	// windows
	win := func(k, label string, f func(c *ctx)) {
		nm("<C-w>"+k, label, action(f))
	}
	for _, k := range []string{"s", "S", "<C-s>"} {
		win(k, "split", func(c *ctx) { c.e.split(c.b, false) })
	}
	for _, k := range []string{"v", "<C-v>"} {
		win(k, "vertical split", func(c *ctx) { c.e.split(c.b, true) })
	}
	win("n", "new window", func(c *ctx) { c.e.execEx("new") })
	for _, k := range []string{"c", "q", "<C-q>"} {
		win(k, "close window", func(c *ctx) { c.e.execEx("close") })
	}
	win("o", "only this window", func(c *ctx) { c.e.onlyWindow() })
	for _, d := range []struct {
		keys   []string
		dx, dy int
		label  string
	}{
		{[]string{"h", "<Left>"}, -1, 0, "window left"},
		{[]string{"j", "<Down>"}, 0, 1, "window below"},
		{[]string{"k", "<Up>"}, 0, -1, "window above"},
		{[]string{"l", "<Right>"}, 1, 0, "window right"},
	} {
		d := d
		for _, k := range d.keys {
			win(k, d.label, func(c *ctx) { c.e.focus(c.e.neighbor(c.w, d.dx, d.dy)) })
		}
	}
	for _, k := range []string{"w", "<C-w>"} {
		win(k, "next window", func(c *ctx) { c.e.cycleWindow(1) })
	}
	win("W", "previous window", func(c *ctx) { c.e.cycleWindow(-1) })
	win("p", "last window", func(c *ctx) {
		if c.e.prevWin != nil && c.e.findNode(c.e.prevWin) != nil {
			c.e.focus(c.e.prevWin)
		}
	})
	win("=", "equalize windows", func(c *ctx) { c.e.equalizeAll() })
	// tmux-like resizing: keep Ctrl held (or use Alt for 5 cells) and the
	// direction moves the border; then h/j/k/l / arrows repeat
	for _, k := range []string{"<C-h>", "<BS>", "<C-Left>", "<M-Left>", "<M-h>",
		"<C-l>", "<C-Right>", "<M-Right>", "<M-l>",
		"<C-j>", "<C-Down>", "<M-Down>", "<M-j>",
		"<C-k>", "<C-Up>", "<M-Up>", "<M-k>"} {
		vertical, dir, step, _ := resizeKey(Key(k))
		label := resizeLabel(vertical, dir, step)
		win(k, label, func(c *ctx) { c.e.resizeStep(vertical, dir*step*c.count) })
	}
	win("<Space>", "next layout (tmux)", func(c *ctx) { c.e.execEx("layout next") })

	// ---- visual-mode actions
	vm("<Esc>", "exit visual", action(func(c *ctx) { c.e.exitVisual() }))
	vm("<C-c>", "exit visual", action(func(c *ctx) { c.e.exitVisual() }))
	vm("v", "charwise / exit", action(func(c *ctx) { c.e.enterVisual(ModeVisual) }))
	vm("V", "linewise / exit", action(func(c *ctx) { c.e.enterVisual(ModeVisualLine) }))
	vm("<C-v>", "blockwise / exit", action(func(c *ctx) { c.e.enterVisual(ModeVisualBlock) }))
	vm("o", "other end", action(vSwapEnds))
	vm("O", "other corner", action(vSwapCorner))
	vm("I", "insert before selection", action(vInsertBlock(false)))
	vm("A", "append after selection", action(vInsertBlock(true)))
	vm("c", "change", operator(OpChange))
	vm("C", "change lines", action(func(c *ctx) {
		c.e.mode = ModeVisualLine
		r := c.e.visualRange()
		c.e.exitVisual()
		c.op = OpChange
		c.e.applyOperator(c, OpChange, r)
	}))
	vm("D", "delete lines", action(func(c *ctx) { c.e.visualLinesOp(c, OpDelete) }))
	vm("X", "delete lines", action(func(c *ctx) { c.e.visualLinesOp(c, OpDelete) }))
	vm("Y", "yank lines", action(func(c *ctx) { c.e.visualLinesOp(c, OpYank) }))
	vm("S", "change lines", action(func(c *ctx) { c.e.visualLinesOp(c, OpChange) }))
	vm("R", "change lines", action(func(c *ctx) { c.e.visualLinesOp(c, OpChange) }))
	vm("r", "replace chars", charAction(vReplaceChar))
	vm("J", "join lines", action(vJoin(true)))
	vm("gJ", "join lines (no spaces)", action(vJoin(false)))
	vm("p", "put over selection", action(vPut(false)))
	vm("P", "put (keep register)", action(vPut(true)))
	vm(":", "command line on selection", action(func(c *ctx) { c.e.startCmdline(':', "'<,'>") }))
	vm("<C-a>", "increment numbers", action(func(c *ctx) { c.e.visualIncrement(c, c.count) }))
	vm("<C-x>", "decrement numbers", action(func(c *ctx) { c.e.visualIncrement(c, -c.count) }))

	e.setupLeader()
}

// visualLinesOp applies op to the selected lines.
func (e *Editor) visualLinesOp(c *ctx, op Operator) {
	e.mode = ModeVisualLine
	r := e.visualRange()
	e.exitVisual()
	c.op = op
	e.applyOperator(c, op, r)
}

// feedOperatorLine runs op on count lines (S, Y).
func (e *Editor) feedOperatorLine(c *ctx, op Operator) {
	last := min(c.w.cur.Line+c.count-1, c.b.LineCount()-1)
	r := opRange{start: Pos{c.w.cur.Line, 0}, end: Pos{last, c.b.LineLen(last)}, linewise: true}
	c.op = op
	e.applyOperator(c, op, r)
}

func (e *Editor) toEOL(c *ctx, op Operator) {
	l := min(c.w.cur.Line+c.count-1, c.b.LineCount()-1)
	r := opRange{start: c.w.cur, end: Pos{l, c.b.LineLen(l)}}
	c.op = op
	e.applyOperator(c, op, r)
	if op == OpDelete {
		e.clampCursor(c.w)
	}
}

func (e *Editor) deleteChars(c *ctx, forward bool) {
	b, w := c.b, c.w
	if b.ReadOnly {
		e.errorf("E21: Cannot make changes, buffer is read-only")
		return
	}
	n := b.LineLen(w.cur.Line)
	if forward {
		if n == 0 {
			return
		}
		end := min(w.cur.Col+c.count, n)
		e.regs.Delete(c.reg, Register{Text: b.Slice(w.cur, Pos{w.cur.Line, end})})
		b.Delete(w.cur, Pos{w.cur.Line, end})
	} else {
		start := max(w.cur.Col-c.count, 0)
		if start == w.cur.Col {
			return
		}
		e.regs.Delete(c.reg, Register{Text: b.Slice(Pos{w.cur.Line, start}, w.cur)})
		b.Delete(Pos{w.cur.Line, start}, w.cur)
		w.cur.Col = start
	}
}

func (e *Editor) replaceChars(c *ctx) {
	b, w := c.b, c.w
	if b.ReadOnly {
		e.errorf("E21: Cannot make changes, buffer is read-only")
		return
	}
	n := b.LineLen(w.cur.Line)
	if w.cur.Col+c.count > n {
		return
	}
	end := Pos{w.cur.Line, w.cur.Col + c.count}
	if c.char == '\n' {
		b.Replace(w.cur, end, "\n")
		w.cur = Pos{w.cur.Line + 1, 0}
		return
	}
	b.Replace(w.cur, end, strings.Repeat(string(c.char), c.count))
	w.cur.Col = end.Col - 1
}

func (e *Editor) tildeChars(c *ctx) {
	b, w := c.b, c.w
	n := b.LineLen(w.cur.Line)
	if n == 0 || b.ReadOnly {
		return
	}
	end := min(w.cur.Col+c.count, n)
	old := b.Slice(w.cur, Pos{w.cur.Line, end})
	b.Replace(w.cur, Pos{w.cur.Line, end}, toggleCase(old))
	w.cur.Col = min(end, n-1)
}

// joinLines joins count lines starting at the cursor line.
func (e *Editor) joinLines(w *Window, count int, spaces bool) {
	b := w.buf
	if b.ReadOnly {
		e.errorf("E21: Cannot make changes, buffer is read-only")
		return
	}
	l := w.cur.Line
	for i := 1; i < count && l+1 < b.LineCount(); i++ {
		cur := b.LineString(l)
		next := b.LineString(l + 1)
		curLen := utf8.RuneCountInString(cur)
		if !spaces {
			b.Delete(Pos{l, curLen}, Pos{l + 1, 0})
			w.cur = Pos{l, curLen}
			continue
		}
		trimmed := strings.TrimLeft(next, " \t")
		lead := utf8.RuneCountInString(next) - utf8.RuneCountInString(trimmed)
		sep := " "
		if strings.HasSuffix(cur, " ") || strings.HasSuffix(cur, "\t") || trimmed == "" || strings.HasPrefix(trimmed, ")") || cur == "" {
			sep = ""
		}
		b.Replace(Pos{l, curLen}, Pos{l + 1, lead}, sep)
		w.cur = Pos{l, curLen}
	}
}

func (e *Editor) putReg(c *ctx, after, cursorAfter bool) {
	reg, ok := e.regs.Get(c.reg)
	if !ok {
		if c.reg != 0 {
			e.errorf("E353: Nothing in register %c", c.reg)
		}
		return
	}
	e.put(c, reg, after, cursorAfter)
}

func (e *Editor) undo(c *ctx, redo bool) {
	b := c.b
	for i := 0; i < c.count; i++ {
		var p Pos
		var ok bool
		if redo {
			p, ok = b.Redo()
		} else {
			p, ok = b.Undo()
		}
		if !ok {
			if redo {
				e.infof("Already at newest change")
			} else {
				e.infof("Already at oldest change")
			}
			break
		}
		c.w.cur = p
	}
	e.clampCursor(c.w)
	c.w.want = displayCol(b.Line(c.w.cur.Line), c.w.cur.Col, e.options(b).TabStop)
}

var numberRe = regexp.MustCompile(`0[xX][0-9a-fA-F]+|-?\d+`)

func (e *Editor) incrementNumber(c *ctx, delta int) {
	b, w := c.b, c.w
	line := b.LineString(w.cur.Line)
	runes := []rune(line)
	for _, m := range numberRe.FindAllStringIndex(line, -1) {
		s, en := runeIndex(line, m[0]), runeIndex(line, m[1])
		if en <= w.cur.Col {
			continue
		}
		num := line[m[0]:m[1]]
		var repl string
		if strings.HasPrefix(strings.ToLower(num), "0x") {
			v, _ := strconv.ParseUint(num[2:], 16, 64)
			nv := int64(v) + int64(delta)
			if nv < 0 {
				nv = 0 // hex numbers are unsigned
			}
			repl = num[:2] + strconv.FormatInt(nv, 16)
		} else {
			// a '-' directly after a word char is a separator, not a sign
			if strings.HasPrefix(num, "-") && s > 0 && charClass(runes[s-1], false) == clsWord {
				num = num[1:]
				s++
			}
			v, err := strconv.ParseInt(num, 10, 64)
			nv := v + int64(delta)
			if err != nil || (delta > 0) != (nv > v) {
				e.errorf("number out of range")
				return
			}
			repl = strconv.FormatInt(nv, 10)
		}
		b.Replace(Pos{w.cur.Line, s}, Pos{w.cur.Line, en}, repl)
		w.cur.Col = s + utf8.RuneCountInString(repl) - 1
		return
	}
}

func (e *Editor) visualIncrement(c *ctx, delta int) {
	r := e.visualRange()
	e.exitVisual()
	for l := r.start.Line; l <= r.end.Line; l++ {
		c.w.cur = Pos{l, 0}
		e.incrementNumber(c, delta)
	}
	c.w.cur = r.start
}

func (e *Editor) fileInfo() {
	b := e.win.buf
	name := b.Name
	if b.Path != "" {
		name = b.Path
	}
	mod := ""
	if e.modified(b) {
		mod = " [Modified]"
	}
	pct := (e.win.cur.Line + 1) * 100 / b.LineCount()
	e.infof("%q%s %d lines --%d%%--", name, mod, b.LineCount(), pct)
}

func (e *Editor) wordCount() {
	b := e.win.buf
	words, chars := 0, 0
	for l := 0; l < b.LineCount(); l++ {
		s := b.LineString(l)
		words += len(strings.Fields(s))
		chars += utf8.RuneCountInString(s) + 1
	}
	e.infof("Line %d of %d; Word count %d; Chars %d", e.win.cur.Line+1, b.LineCount(), words, chars)
}

func (e *Editor) cycleWindow(delta int) {
	ws := e.windows()
	for i, w := range ws {
		if w == e.win {
			e.focus(ws[(i+delta+len(ws))%len(ws)])
			return
		}
	}
}

// ---------------------------------------------------------------- scrolling

// lastVisibleLine returns the last buffer line fully shown when the
// window starts at w.top (virtual rows such as notebook outputs count).
func (e *Editor) lastVisibleLine(w *Window) int {
	rows, h := 0, w.textHeight()
	last := w.top
	for l := w.top; l < w.buf.LineCount(); l++ {
		rows += e.lineRows(w, l)
		if rows > h && l > w.top {
			break
		}
		last = l
	}
	return last
}

// fitCursorToView moves the cursor into the visible lines (respecting
// scrolloff) after the view was scrolled, and pins the scroll position.
func (e *Editor) fitCursorToView(w *Window) {
	so := min(max(e.options(w.buf).ScrollOff, 0), (w.textHeight()-1)/2)
	lo, hi := w.top, e.lastVisibleLine(w)
	if w.top > 0 {
		lo += so
	}
	if hi < w.buf.LineCount()-1 {
		hi -= so
	}
	if hi < lo {
		hi = lo
	}
	if l := w.cur.Line; l < lo || l > hi {
		w.cur = e.wantPos(w, min(max(l, lo), min(hi, w.buf.LineCount()-1)))
	}
	e.clampCursor(w)
	w.pinTop = true
}

func (e *Editor) scrollLines(w *Window, n int) {
	w.top = max(0, min(w.top+n, w.buf.LineCount()-1))
	e.fitCursorToView(w)
}

func (e *Editor) scrollHalf(w *Window, down bool) {
	n := max(w.textHeight()/2, 1)
	if !down {
		n = -n
	}
	// the cursor moves as far as the view; scrollToCursor then applies
	// scrolloff by adjusting the view (no pinning needed)
	line := max(0, min(w.cur.Line+n, w.buf.LineCount()-1))
	w.top = max(0, min(w.top+n, w.buf.LineCount()-1))
	w.cur = e.wantPos(w, line)
	e.clampCursor(w)
}

func (e *Editor) scrollPage(w *Window, pages int) {
	n := max(w.textHeight()-2, 1) * pages
	w.top = max(0, min(w.top+n, w.buf.LineCount()-1))
	if pages > 0 {
		w.cur = e.wantPos(w, w.top)
	} else {
		w.cur = e.wantPos(w, e.lastVisibleLine(w))
	}
	e.fitCursorToView(w)
}

func (e *Editor) scrollTo(w *Window, where string) {
	h := w.textHeight()
	switch where {
	case "top":
		w.top = w.cur.Line
	case "middle":
		w.top = max(0, w.cur.Line-h/2)
	case "bottom":
		w.top = max(0, w.cur.Line-h+1)
	}
	w.pinTop = true
}

// ---------------------------------------------------------------- files

var fileRefRe = regexp.MustCompile(`[\w./~\-+@]+(?::\d+)?`)

func (e *Editor) gotoFile(c *ctx) {
	line := c.b.LineString(c.w.cur.Line)
	col := len(string(c.b.Line(c.w.cur.Line)[:min(c.w.cur.Col, c.b.LineLen(c.w.cur.Line))]))
	for _, m := range fileRefRe.FindAllStringIndex(line, -1) {
		if col < m[0] || col > m[1] {
			continue
		}
		ref := line[m[0]:m[1]]
		lineNo := 0
		if i := strings.LastIndexByte(ref, ':'); i > 0 {
			lineNo, _ = strconv.Atoi(ref[i+1:])
			ref = ref[:i]
		}
		path := expandHome(ref)
		if !filepath.IsAbs(path) && c.b.Path != "" {
			if _, err := os.Stat(path); err != nil {
				path = filepath.Join(filepath.Dir(c.b.Path), path)
			}
		}
		if _, err := os.Stat(path); err != nil {
			e.errorf("E447: Can't find file %q", ref)
			return
		}
		b, err := e.loadBuffer(path)
		if err != nil {
			e.errorf("%v", err)
			return
		}
		e.pushJump(c.w)
		e.setBuffer(c.w, b)
		if lineNo > 0 {
			e.setCursor(c.w, Pos{min(lineNo-1, b.LineCount()-1), 0})
		}
		return
	}
	e.errorf("E446: No file name under cursor")
}

var urlRe = regexp.MustCompile(`https?://[^\s<>"'()\]]+`)

func (e *Editor) openExternal(c *ctx) {
	line := c.b.LineString(c.w.cur.Line)
	target := ""
	for _, m := range urlRe.FindAllStringIndex(line, -1) {
		target = line[m[0]:m[1]]
		col := len(string(c.b.Line(c.w.cur.Line)[:min(c.w.cur.Col, c.b.LineLen(c.w.cur.Line))]))
		if col >= m[0] && col <= m[1] {
			break
		}
	}
	if target == "" {
		target = wordAt(c.b.Line(c.w.cur.Line), c.w.cur.Col)
		if _, err := os.Stat(target); err != nil {
			e.errorf("no URL under cursor")
			return
		}
	}
	if e.mappingDepth > 0 {
		e.errorf("%v", errMappingProgram)
		return
	}
	if err := openWithSystem(target); err != nil {
		e.errorf("%v", err)
		return
	}
	e.infof("opened %s", target)
}

func openWithSystem(target string) error {
	for _, opener := range []string{"xdg-open", "open", "wslview"} {
		if _, err := exec.LookPath(opener); err == nil {
			cmd := exec.Command(opener, target)
			cmd.Stdout, cmd.Stderr = nil, nil
			if err := cmd.Start(); err != nil {
				return err
			}
			go cmd.Wait()
			return nil
		}
	}
	return fmt.Errorf("no opener found (xdg-open)")
}

// applyUserKeys installs mappings from the config file.
func (e *Editor) applyUserKeys() {
	for mode, m := range e.cfg.Keys {
		var km *keymap.Map[*Cmd]
		switch mode {
		case "normal", "n":
			km = e.normalMap
		case "visual", "v", "x":
			km = e.visualMap
		case "insert", "i":
			km = e.insertMap
		default:
			e.warnf("config: unknown key mode %q", mode)
			continue
		}
		for keys, act := range m {
			km.Bind(keymap.Parse(keys, e.leader), act, &Cmd{Kind: KindAction, Mapping: act})
		}
	}
}

// docLookup shows documentation for the symbol under the cursor.
func (e *Editor) docLookup() {
	w := e.win
	b := w.buf
	sym := wordAtLisp(b.Line(w.cur.Line), w.cur.Col)
	if sym == "" {
		return
	}
	switch {
	case b.Filetype == "clojure":
		e.evalText(b, fmt.Sprintf("(clojure.repl/doc %s)", sym), -1, Pos{}, Pos{})
	case b.Filetype == "scheme":
		e.evalText(b, fmt.Sprintf("(let ([v (guard (c [#t #f]) (eval '%s))]) (if (procedure? v) (format \"~a: procedure\" '%s) v))", sym, sym), -1, Pos{}, Pos{})
	case b.Filetype == "python" || b.Kind == buffer.KindNotebook:
		e.evalText(b, fmt.Sprintf("help(%s)", wordAt(b.Line(w.cur.Line), w.cur.Col)), -1, Pos{}, Pos{})
	default:
		if _, err := exec.LookPath("man"); err == nil {
			out, _ := runShell("MANWIDTH=80 man "+shellQuote(sym)+" 2>&1 | col -b", "", nil)
			e.showOutput("*doc*", out)
		}
	}
}

func wordAtLisp(line []rune, col int) string {
	isSym := func(r rune) bool {
		return !(r == ' ' || r == '\t' || r == '(' || r == ')' || r == '[' || r == ']' || r == '{' || r == '}' || r == '"' || r == '\'' || r == ',' || r == ';' || r == '`')
	}
	if col >= len(line) || !isSym(line[col]) {
		return ""
	}
	s, en := col, col
	for s > 0 && isSym(line[s-1]) {
		s--
	}
	for en < len(line) && isSym(line[en]) {
		en++
	}
	return string(line[s:en])
}
