package editor

import (
	"strconv"
	"strings"
	"time"

	"scihe/internal/buffer"
	"scihe/internal/keymap"
)

type Key = keymap.Key

type parseStatus int

const (
	parseIncomplete parseStatus = iota
	parseInvalid
	parseComplete
	parseAmbiguous // complete binding that is also a prefix
)

// parsed is a fully decoded command.
type parsed struct {
	reg        rune
	count1     int
	count2     int
	cmd        *Cmd
	char       rune
	motion     *Cmd // operator target (motion or text object)
	motionChar rune
	doubled    bool // operator repeated: linewise on count lines
	countEnd   int  // index in keys where the count1 prefix ends
	regEnd     int
	end        int // number of keys consumed
	// [count2Start, count2End) holds the count typed after an operator
	count2Start, count2End int
	node                   *keymap.Node[*Cmd] // deepest trie node reached (for which-key)
}

func (p *parsed) count() (int, bool) {
	c1, c2 := p.count1, p.count2
	if c1 == 0 && c2 == 0 {
		return 1, false
	}
	return max(c1, 1) * max(c2, 1), true
}

func isDigitKey(k Key, allowZero bool) (int, bool) {
	if len(k) != 1 || k[0] < '0' || k[0] > '9' {
		return 0, false
	}
	if k[0] == '0' && !allowZero {
		return 0, false
	}
	return int(k[0] - '0'), true
}

func keyChar(k Key) (rune, bool) {
	if k == "<Esc>" || k == "<C-c>" {
		return 0, false
	}
	if k == "<CR>" {
		return '\n', true
	}
	r, ok := k.Rune()
	return r, ok
}

// walk descends the trie from keys[i]; returns binding, next index and status.
func walk(m *keymap.Map[*Cmd], keys []Key, i int) (*keymap.Node[*Cmd], int, parseStatus) {
	n := m.Root
	for i < len(keys) {
		c, ok := n.Children[keys[i]]
		if !ok {
			return n, i, parseInvalid
		}
		n = c
		i++
		if n.Binding != nil {
			if len(n.Children) > 0 && i == len(keys) {
				return n, i, parseAmbiguous
			}
			if len(n.Children) == 0 || i == len(keys) {
				return n, i, parseComplete
			}
			// binding with children and more keys: continue if they match
			if _, ok := n.Children[keys[i]]; !ok {
				return n, i, parseComplete
			}
		}
	}
	return n, i, parseIncomplete
}

// parseKeys decodes keys using m (and opMap for operator targets).
func (e *Editor) parseKeys(keys []Key, m *keymap.Map[*Cmd], withOps bool) (parsed, parseStatus) {
	var p parsed
	i := 0
	if i < len(keys) && keys[i] == "\"" {
		if len(keys) < 2 {
			return p, parseIncomplete
		}
		r, ok := keys[1].Rune()
		if !ok || !validRegister(r) {
			return p, parseInvalid
		}
		p.reg = r
		i = 2
	}
	p.regEnd = i
	for i < len(keys) {
		d, ok := isDigitKey(keys[i], p.count1 > 0)
		if !ok {
			break
		}
		p.count1 = p.count1*10 + d
		if p.count1 > 999999 {
			p.count1 = 999999
		}
		i++
	}
	p.countEnd = i
	if i == len(keys) {
		p.node = m.Root
		return p, parseIncomplete
	}
	node, j, st := walk(m, keys, i)
	p.node = node
	if st == parseInvalid || st == parseIncomplete {
		return p, st
	}
	ambiguous := st == parseAmbiguous
	p.cmd = node.Binding.Value
	cmdKeys := keys[i:j]
	i = j
	if p.cmd.NeedsChar {
		if i == len(keys) {
			return p, parseIncomplete
		}
		r, ok := keyChar(keys[i])
		if !ok {
			return p, parseInvalid
		}
		p.char = r
		i++
	}
	if p.cmd.Kind == KindOperator && withOps {
		p.count2Start = i
		for i < len(keys) {
			d, ok := isDigitKey(keys[i], p.count2 > 0)
			if !ok {
				break
			}
			p.count2 = p.count2*10 + d
			i++
		}
		p.count2End = i
		if i == len(keys) {
			p.node = e.opMap.Root
			return p, parseIncomplete
		}
		if keys[i] == "<Esc>" {
			return p, parseInvalid
		}
		// doubled operator: dd, yy, gUU, gUgU, gcc
		last := cmdKeys[len(cmdKeys)-1]
		if keys[i] == last {
			p.doubled = true
			p.end = i + 1
			return p, parseComplete
		}
		rest := keys[i:]
		if len(cmdKeys) > 1 && len(rest) <= len(cmdKeys) {
			match := true
			for k := range rest {
				if rest[k] != cmdKeys[k] {
					match = false
					break
				}
			}
			if match {
				if len(rest) == len(cmdKeys) {
					p.doubled = true
					p.end = len(keys)
					return p, parseComplete
				}
				// could still be a motion (e.g. gUgg): keep waiting
				if n2, _, st2 := walk(e.opMap, keys, i); st2 == parseInvalid {
					return p, parseIncomplete
				} else {
					p.node = n2
					if st2 == parseIncomplete {
						return p, parseIncomplete
					}
				}
			}
		}
		n2, j2, st2 := walk(e.opMap, keys, i)
		p.node = n2
		if st2 == parseInvalid || st2 == parseIncomplete {
			return p, st2
		}
		p.motion = n2.Binding.Value
		i = j2
		if p.motion.NeedsChar {
			if i == len(keys) {
				return p, parseIncomplete
			}
			r, ok := keyChar(keys[i])
			if !ok {
				return p, parseInvalid
			}
			p.motionChar = r
			i++
		}
		p.end = i
		return p, parseComplete
	}
	p.end = i
	if ambiguous {
		return p, parseAmbiguous
	}
	return p, parseComplete
}

// ---------------------------------------------------------------- dispatch

// handleKey dispatches one key, then reports an edit refused because
// the buffer is read-only.
func (e *Editor) handleKey(k Key) {
	top := e.replaying == 0 // keys replayed by a macro / mapping report at the end
	var before *buffer.Buffer
	if top && e.win != nil {
		before = e.win.buf
		before.Refused() // forget a refusal that happened outside a key
	}
	e.dispatchKey(k)
	if !top || e.win == nil {
		return
	}
	refused := e.win.buf.Refused()
	if before != nil && before != e.win.buf && before.Refused() {
		refused = true
	}
	if refused {
		e.errorf("E21: Cannot make changes, buffer is read-only")
	}
}

func (e *Editor) dispatchKey(k Key) {
	if e.replaying > 200 {
		e.errorf("E223: recursive mapping")
		return
	}
	// Terminals encode Alt+x as ESC x, so a fast <Esc> followed by a key
	// arrives as <M-x>. Without an explicit binding, treat it like vim:
	// <Esc> then the key.
	// Alt+key after <C-w> is a window command (<C-w><M-Left>), not <Esc>
	afterCW := len(e.pending) > 0 && e.pending[len(e.pending)-1] == "<C-w>"
	if strings.HasPrefix(string(k), "<M-") && !e.hasBinding(k) && !afterCW && e.resizeUntil.IsZero() {
		inner := strings.TrimPrefix(string(k[1:len(k)-1]), "M-")
		base := Key(inner)
		if len([]rune(inner)) > 1 {
			base = Key("<" + inner + ">")
		}
		e.handleKey("<Esc>")
		e.handleKey(base)
		return
	}
	if e.replaying == 0 {
		e.abortReplay = false
	}
	if e.macro.recording && e.replaying == 0 {
		e.macro.keys = append(e.macro.keys, k)
	}
	if e.resizeModeKey(k) {
		return
	}
	if e.replaying == 0 && e.mode != ModeCmdline && len(e.pending) == 0 {
		e.msg = ""
	}
	if e.picker != nil {
		e.pickerKey(k)
		return
	}
	switch {
	case e.mode == ModeCmdline:
		e.cmdlineKey(k)
	case e.mode == ModeInsert || e.mode == ModeReplace:
		e.insertKey(k)
	case e.mode.visual():
		e.visualKey(k)
	default:
		e.normalKey(k)
	}
}

// hasBinding reports whether k is bound at the top level of the current mode.
func (e *Editor) hasBinding(k Key) bool {
	var m *keymap.Map[*Cmd]
	switch {
	case e.mode == ModeInsert || e.mode == ModeReplace:
		m = e.insertMap
	case e.mode.visual():
		m = e.visualMap
	case e.mode == ModeNormal:
		m = e.normalMap
	default:
		return false
	}
	_, ok := m.Root.Children[k]
	return ok
}

// feedKeys replays keys as if typed.
func (e *Editor) feedKeys(keys []Key) {
	if e.replaying == 0 {
		e.abortReplay = false
	}
	e.replaying++
	defer func() { e.replaying-- }()
	for _, k := range keys {
		if e.quit || e.abortReplay {
			break
		}
		e.handleKey(k)
	}
	// a replay must not leave a half-typed command behind
	if e.replaying == 1 && len(e.pending) > 0 {
		e.resetPending()
	}
}

func (e *Editor) resetPending() {
	e.pending = e.pending[:0]
	e.whichKey = false
	e.pendAmb = false
}

func (e *Editor) mapsFor(base *keymap.Map[*Cmd]) []*keymap.Map[*Cmd] {
	if lm := e.localMaps[e.win.buf.ID]; lm != nil && base == e.normalMap {
		return []*keymap.Map[*Cmd]{lm, base}
	}
	if lm := e.localVisual[e.win.buf.ID]; lm != nil && base == e.visualMap {
		return []*keymap.Map[*Cmd]{lm, base}
	}
	return []*keymap.Map[*Cmd]{base}
}

// whichKeyNode returns the trie node for the which-key popup.
func (e *Editor) whichKeyNode() *keymap.Node[*Cmd] {
	if len(e.pending) == 0 {
		return nil
	}
	base := e.normalMap
	if e.mode.visual() {
		base = e.visualMap
	}
	for _, m := range e.mapsFor(base) {
		p, st := e.parseKeys(e.pending, m, true)
		if st == parseIncomplete && p.node != nil && len(p.node.Children) > 0 {
			return p.node
		}
	}
	return nil
}

func (e *Editor) normalKey(k Key) {
	if len(e.pending) == 0 {
		e.pendingSince = time.Now()
		if k == "q" && e.macro.recording {
			e.toggleMacro(&ctx{e: e, w: e.win, b: e.win.buf, count: 1})
			return
		}
	}
	e.pending = append(e.pending, k)
	keys := e.pending
	var p parsed
	st := parseInvalid
	for _, m := range e.mapsFor(e.normalMap) {
		p, st = e.parseKeys(keys, m, true)
		if st != parseInvalid {
			break
		}
	}
	switch st {
	case parseIncomplete, parseAmbiguous:
		if len(keys) > p.countEnd && keys[p.countEnd] == e.leader {
			e.whichKey = true
		}
		// an ambiguous binding runs after a timeout (see tick)
		e.pendAmb = st == parseAmbiguous
		return
	case parseInvalid:
		e.resetPending()
		return
	}
	keys = append([]Key(nil), keys...)
	e.resetPending()
	e.execNormal(p, keys[:p.end])
	for _, k := range keys[p.end:] {
		e.handleKey(k)
	}
}

// flushAmbiguous executes a pending ambiguous binding after the timeout.
func (e *Editor) flushAmbiguous() {
	keys := append([]Key(nil), e.pending...)
	base := e.normalMap
	if e.mode.visual() {
		base = e.visualMap
	}
	e.resetPending()
	for _, m := range e.mapsFor(base) {
		p, st := e.parseKeys(keys, m, true)
		if st == parseAmbiguous || st == parseComplete {
			if e.mode.visual() {
				e.execVisual(p, keys)
			} else {
				e.execNormal(p, keys)
			}
			return
		}
	}
}

// execNormal runs a parsed normal-mode command.
func (e *Editor) execNormal(p parsed, keys []Key) {
	w := e.win
	b := w.buf
	count, has := p.count()
	c := &ctx{e: e, w: w, b: b, count: count, hasCount: has, reg: p.reg, char: p.char, keys: keys}
	cmd := p.cmd
	if cmd.Mapping != "" {
		e.runMapping(cmd.Mapping, c)
		return
	}
	undo := !cmd.NoUndo && cmd.Kind != KindMotion
	if undo {
		b.BeginUndo(w.cur)
	}
	switch cmd.Kind {
	case KindMotion:
		e.doMotion(c, cmd)
	case KindAction:
		cmd.Action(c)
	case KindOperator:
		c.op = cmd.Op
		ok, deferred := e.doOperator(c, &p)
		if deferred {
			// the search prompt finishes the command and records "."
			e.pendingOp.dotKeys = dotRecord(keys, &p)
			e.pendingOp.dotCount = combinedCount(&p)
		}
		if !ok {
			if undo {
				b.EndUndo(w.cur)
			}
			return // failed or deferred operators do not replace "."
		}
	}
	// the command may have switched buffers/windows
	if undo {
		if e.mode == ModeInsert || e.mode == ModeReplace {
			e.insert.undoBuf = b
		} else {
			b.EndUndo(w.cur)
		}
	}
	if e.win == w {
		e.clampCursor(w)
	}
	if e.insert.oneShot && e.mode == ModeNormal {
		// back to insert mode after a <C-o> command
		e.insert.oneShot = false
		e.mode = ModeInsert
		e.win.buf.BeginUndo(e.win.cur)
		e.insert.undoBuf = e.win.buf
		return
	}
	// dot repeat
	isChange := cmd.Change || (cmd.Kind == KindOperator && cmd.Op != OpYank && cmd.Op != OpEval)
	if isChange && e.dot.replay == 0 {
		e.setDot(dotRecord(keys, &p), combinedCount(&p))
	}
}

// dotRecord returns the keys of a command without its counts (they are
// kept separately so a count given to "." can replace them).
func dotRecord(keys []Key, p *parsed) []Key {
	rec := append([]Key(nil), keys[:p.regEnd]...)
	if p.count2End > p.count2Start {
		rec = append(rec, keys[p.countEnd:p.count2Start]...)
		return append(rec, keys[p.count2End:]...)
	}
	return append(rec, keys[p.countEnd:]...)
}

// combinedCount is the effective count of a command (0 = none given).
func combinedCount(p *parsed) int {
	if p.count1 == 0 && p.count2 == 0 {
		return 0
	}
	n, _ := p.count()
	return n
}

// setDot records a change for "."; if insert mode was entered the typed
// text is appended when insert mode ends.
func (e *Editor) setDot(keys []Key, count int) {
	e.dot.pendingKeys = keys
	e.dot.pendingCount = count
	if e.mode == ModeInsert || e.mode == ModeReplace {
		e.dot.recording = true
		e.dot.insertKeys = nil
	} else {
		e.dot.keys, e.dot.count = keys, count
	}
}

func (e *Editor) doMotion(c *ctx, cmd *Cmd) {
	if cmd.Action != nil {
		cmd.Action(c)
		return
	}
	target, _, ok := cmd.Motion(c)
	if !ok {
		if e.replaying > 0 {
			e.abortReplay = true // a failing motion stops macros like vim
		}
		return
	}
	if cmd.Jump {
		e.pushJump(c.w)
	}
	want := c.w.want
	c.w.cur = c.b.Clamp(target)
	e.clampCursor(c.w)
	switch {
	case cmd.KeepWant:
		c.w.want = want
	case cmd.EOL:
		c.w.want = -1
	default:
		c.w.want = displayCol(c.b.Line(c.w.cur.Line), c.w.cur.Col, e.options(c.b).TabStop)
	}
}

// doOperator runs an operator; ok reports success, deferred that it
// waits for a search prompt.
func (e *Editor) doOperator(c *ctx, p *parsed) (ok, deferred bool) {
	var r opRange
	if p.doubled {
		last := min(c.w.cur.Line+c.count-1, c.b.LineCount()-1)
		r = opRange{start: Pos{c.w.cur.Line, 0}, end: Pos{last, c.b.LineLen(last)}, linewise: true}
	} else {
		m := p.motion
		c.char = p.motionChar
		switch m.Kind {
		case KindTextObject:
			s, en, mt, found := m.TextObj(c)
			if !found {
				return false, false
			}
			r = opRange{start: s, end: en, linewise: mt == Linewise}
		case KindMotion:
			if m.Action != nil {
				// prompt-based motion (search): defer the operator
				e.pendingOp = &pendingOperator{op: c.op, reg: c.reg, count: c.count, hasCount: c.hasCount}
				m.Action(c)
				return false, true
			}
			target, mt, found := m.Motion(c)
			if !found {
				if e.replaying > 0 {
					e.abortReplay = true
				}
				return false, false
			}
			if m.Jump {
				c.b.Marks['\''] = c.w.cur
			}
			r = e.rangeFromMotion(c, target, mt)
		default:
			return false, false
		}
	}
	e.applyOperator(c, c.op, r)
	return true, false
}

// pendingOperator waits for a search prompt to provide its motion.
type pendingOperator struct {
	op       Operator
	reg      rune
	count    int
	hasCount bool
	dotKeys  []Key // keys up to the prompt, for "."
	dotCount int
}

// runMapping executes a config-defined mapping.
func (e *Editor) runMapping(m string, c *ctx) {
	e.mappingDepth++
	defer func() { e.mappingDepth-- }()
	if strings.HasPrefix(m, ":") {
		e.execEx(strings.TrimPrefix(m, ":"))
		return
	}
	keys := keymap.Parse(m, e.leader)
	if c.hasCount {
		keys = append(keymap.Parse(strconv.Itoa(c.count), e.leader), keys...)
	}
	e.feedKeys(keys)
}

// ---------------------------------------------------------------- dot / macro

type dotState struct {
	keys         []Key
	count        int
	pendingKeys  []Key
	pendingCount int
	recording    bool
	insertKeys   []Key
	replay       int
}

func (e *Editor) commitDot() {
	if !e.dot.recording {
		return
	}
	e.dot.recording = false
	e.dot.keys = append(append([]Key(nil), e.dot.pendingKeys...), e.dot.insertKeys...)
	e.dot.keys = append(e.dot.keys, "<Esc>")
	e.dot.count = e.dot.pendingCount
	e.dot.insertKeys = nil
}

func (e *Editor) repeatDot(c *ctx) {
	if len(e.dot.keys) == 0 {
		return
	}
	keys := e.dot.keys
	count := e.dot.count
	if c.hasCount {
		count = c.count
	}
	var full []Key
	// keep a register prefix in front of the count
	i := 0
	if len(keys) >= 2 && keys[0] == "\"" {
		full = append(full, keys[0], keys[1])
		i = 2
	}
	if count > 0 {
		full = append(full, keymap.Parse(strconv.Itoa(count), e.leader)...)
	}
	full = append(full, keys[i:]...)
	saved := e.dot
	e.dot.replay++
	e.feedKeys(full)
	e.dot = saved
	if c.hasCount {
		e.dot.count = c.count
	}
}

type macroState struct {
	recording bool
	reg       rune
	keys      []Key
	last      rune
}

func (e *Editor) toggleMacro(c *ctx) {
	if e.macro.recording {
		keys := e.macro.keys
		// drop the q that ended the recording
		if n := len(keys); n > 0 && keys[n-1] == "q" {
			keys = keys[:n-1]
		}
		var sb strings.Builder
		for _, k := range keys {
			sb.WriteString(string(k))
		}
		reg := Register{Text: sb.String()}
		if e.macro.reg >= 'A' && e.macro.reg <= 'Z' {
			e.regs.store(e.macro.reg, reg)
		} else {
			e.regs.m[lower(e.macro.reg)] = reg
		}
		e.macro = macroState{last: e.macro.last}
		return
	}
	r := c.char
	if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
		return
	}
	e.macro = macroState{recording: true, reg: r, last: e.macro.last}
}

func (e *Editor) playMacro(c *ctx) {
	r := c.char
	if r == '@' {
		r = e.macro.last
	}
	if r == ':' {
		for i := 0; i < c.count; i++ {
			if e.lastExCmd != "" {
				e.execEx(e.lastExCmd)
			}
		}
		return
	}
	reg, ok := e.regs.Get(r)
	if !ok || reg.Text == "" {
		return
	}
	e.macro.last = r
	keys := keymap.Parse(reg.Text, e.leader)
	// the undo group of "@" itself must not swallow the macro's changes
	c.b.EndUndo(c.w.cur)
	e.abortReplay = false
	for i := 0; i < c.count && !e.abortReplay; i++ {
		e.feedKeys(keys)
	}
}

// ---------------------------------------------------------------- paste

// keyText converts a key received during a bracketed paste to text.
func keyText(k Key) string {
	switch k {
	case "<CR>", "<C-j>", "<C-m>":
		return "\n"
	case "<Tab>":
		return "\t"
	case "<Space>":
		return " "
	case "<lt>":
		return "<"
	}
	if r, ok := k.Rune(); ok {
		return string(r)
	}
	return ""
}

// pasteText inserts bracketed-paste content according to the mode.
func (e *Editor) pasteText(text string) {
	if text == "" {
		return
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if e.picker != nil {
		e.picker.input = append(e.picker.input, []rune(strings.ReplaceAll(text, "\n", " "))...)
		e.picker.refresh(e)
		return
	}
	switch {
	case e.mode == ModeCmdline:
		e.cmdline.insert(strings.ReplaceAll(text, "\n", " "))
		e.cmdlineChanged()
	case e.mode == ModeInsert || e.mode == ModeReplace:
		w := e.win
		if w.buf.ReadOnly {
			return
		}
		w.cur = w.buf.Insert(w.cur, text)
		e.insert.keys = append(e.insert.keys, Key("\x00paste:"+text))
		if e.dot.recording {
			e.dot.insertKeys = append(e.dot.insertKeys, Key("\x00paste:"+text))
		}
	default:
		if e.mode.visual() {
			e.exitVisual()
		}
		w := e.win
		if w.buf.ReadOnly {
			e.errorf("E21: Cannot make changes, buffer is read-only")
			return
		}
		w.buf.BeginUndo(w.cur)
		c := &ctx{e: e, w: w, b: w.buf, count: 1}
		t := RegChar
		if strings.HasSuffix(text, "\n") {
			t = RegLine
		}
		e.put(c, Register{Text: text, Type: t}, true, false)
		w.buf.EndUndo(w.cur)
	}
}
