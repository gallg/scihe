package editor

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"scihe/internal/buffer"
)

type insertState struct {
	count    int
	kind     byte // 'i', 'a', 'o', 'O', 'A', 'I', 'c' ...
	keys     []Key
	start    Pos
	ctrlR    bool
	oneShot  bool // <C-o>: run one normal command then come back
	undoBuf  *buffer.Buffer
	replaced []rune // replace mode: original chars for <BS>
	block    *blockInsert
	compl    *completion
}

type blockInsert struct {
	r      opRange
	append bool
	line   int
	col    int // rune column where typing started on the first line
}

type completion struct {
	start Pos
	words []string
	idx   int // -1 = original prefix
	orig  string
}

// enterInsert switches to insert mode (the caller positions the cursor).
func (e *Editor) enterInsert() {
	e.enterInsertKind('i', 1)
}

func (e *Editor) enterInsertKind(kind byte, count int) {
	if e.win.buf.ReadOnly {
		e.errorf("E21: Cannot make changes, buffer is read-only")
		return
	}
	if e.mode.visual() {
		e.setVisualMarks()
	}
	e.mode = ModeInsert
	e.insert.kind = kind
	e.insert.count = max(count, 1)
	e.insert.keys = nil
	e.insert.start = e.win.cur
	e.insert.replaced = nil
	e.insert.compl = nil
	e.insert.ctrlR = false
	if e.insert.undoBuf == nil {
		e.insert.undoBuf = e.win.buf
	}
	// one undo group for the whole insert session (no-op if already open)
	e.win.buf.BeginUndo(e.win.cur)
}

func (e *Editor) enterReplace(count int) {
	e.enterInsertKind('R', count)
	e.mode = ModeReplace
}

func (e *Editor) startBlockInsert(r opRange, appendMode bool) {
	e.enterInsertKind('b', 1)
	e.insert.block = &blockInsert{r: r, append: appendMode, line: e.win.cur.Line, col: e.win.cur.Col}
}

// exitInsert returns to normal mode, completing repeats and dot recording.
func (e *Editor) exitInsert() {
	w := e.win
	b := w.buf
	keys := e.insert.keys
	// the key that ended insert mode is not part of the repeated text
	for len(keys) > 0 {
		last := keys[len(keys)-1]
		if last != "<Esc>" && last != "<C-c>" && last != "<C-[>" {
			break
		}
		keys = keys[:len(keys)-1]
	}
	if e.insert.count > 1 && e.replaying < 50 {
		n := e.insert.count - 1
		e.insert.count = 1
		for i := 0; i < n; i++ {
			switch e.insert.kind {
			case 'o':
				e.openLine(w, true)
			case 'O':
				e.openLine(w, false)
			}
			e.replayInsertKeys(keys)
		}
	}
	blockDone := false
	if bi := e.insert.block; bi != nil {
		e.insert.block = nil
		e.finishBlockInsert(bi)
		blockDone = true
	}
	b.Marks['^'] = w.cur
	b.Marks['.'] = w.cur
	e.mode = ModeNormal
	e.insert.oneShot = false
	e.insert.ctrlR = false
	if w.cur.Col > 0 && !blockDone {
		w.cur.Col--
	}
	ub := e.insert.undoBuf
	e.insert.undoBuf = nil
	if ub != nil {
		ub.EndUndo(w.cur)
	}
	if b != ub {
		b.EndUndo(w.cur)
	}
	e.insert.compl = nil
	e.clampCursor(w)
	w.want = displayCol(b.Line(w.cur.Line), w.cur.Col, e.options(b).TabStop)
	e.regs.SetSpecial('.', insertedText(keys))
	e.commitDot()
}

func insertedText(keys []Key) string {
	var sb strings.Builder
	for _, k := range keys {
		if strings.HasPrefix(string(k), "\x00paste:") {
			sb.WriteString(string(k)[7:])
			continue
		}
		if k == "<CR>" {
			sb.WriteByte('\n')
			continue
		}
		if r, ok := k.Rune(); ok {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func (e *Editor) replayInsertKeys(keys []Key) {
	saved := e.insert.keys
	for _, k := range keys {
		e.insertKeyRaw(k)
	}
	e.insert.keys = saved
}

func (e *Editor) finishBlockInsert(bi *blockInsert) {
	w := e.win
	b := w.buf
	if w.cur.Line != bi.line || w.cur.Col < bi.col {
		return
	}
	text := string(b.Line(bi.line)[bi.col:w.cur.Col])
	if text == "" {
		return
	}
	ts := e.options(b).TabStop
	for l := bi.r.start.Line + 1; l <= bi.r.end.Line; l++ {
		line := b.Line(l)
		var col int
		if bi.append {
			if bi.r.toEOL {
				col = len(line)
			} else {
				cur := displayCol(line, len(line), ts)
				if cur < bi.r.vcolEnd {
					b.Insert(Pos{l, len(line)}, strings.Repeat(" ", bi.r.vcolEnd-cur))
				}
				col = colForDisplay(b.Line(l), bi.r.vcolEnd, ts)
			}
		} else {
			if displayCol(line, len(line), ts) < bi.r.vcolStart {
				continue // short line: vim skips it for I
			}
			col = colForDisplay(line, bi.r.vcolStart, ts)
		}
		b.Insert(Pos{l, col}, text)
	}
	w.cur = Pos{bi.line, bi.col}
}

// openLine opens a new line below/above the cursor with auto-indent.
func (e *Editor) openLine(w *Window, below bool) {
	b := w.buf
	l := w.cur.Line
	indent := ""
	if e.options(b).AutoIndent {
		indent = leadingWS(b.LineString(l))
		if below && e.indentAfter(b, b.LineString(l)) {
			indent += e.indentUnit(b)
		}
	}
	if below {
		b.Insert(Pos{l, b.LineLen(l)}, "\n"+indent)
		w.cur = Pos{l + 1, utf8.RuneCountInString(indent)}
	} else {
		b.Insert(Pos{l, 0}, indent+"\n")
		w.cur = Pos{l, utf8.RuneCountInString(indent)}
	}
}

// indentAfter reports whether a line opens a block (extra indent).
func (e *Editor) indentAfter(b *buffer.Buffer, line string) bool {
	t := strings.TrimRight(line, " \t")
	if t == "" {
		return false
	}
	switch b.Filetype {
	case "python", "nim", "yaml", "coffeescript":
		if strings.HasSuffix(t, ":") && !strings.HasPrefix(strings.TrimSpace(t), "#") {
			return true
		}
	case "clojure", "scheme", "racket", "common lisp", "emacslisp":
		return false
	}
	last := t[len(t)-1]
	return last == '{' || last == '[' || last == '('
}

func (e *Editor) insertKey(k Key) {
	if e.insert.ctrlR {
		e.insert.ctrlR = false
		// replace the recorded <C-r> by the inserted text itself
		if n := len(e.insert.keys); n > 0 && e.insert.keys[n-1] == "<C-r>" {
			e.insert.keys = e.insert.keys[:n-1]
		}
		if n := len(e.dot.insertKeys); e.dot.recording && n > 0 && e.dot.insertKeys[n-1] == "<C-r>" {
			e.dot.insertKeys = e.dot.insertKeys[:n-1]
		}
		if r, ok := keyChar(k); ok {
			if reg, ok := e.regs.Get(r); ok {
				text := reg.Text
				e.win.cur = e.win.buf.Insert(e.win.cur, text)
				e.recordInsertKey(Key("\x00paste:" + text))
			}
		}
		return
	}
	// insert-mode mappings (e.g. "jk" -> <Esc>)
	if len(e.insertMap.Root.Children) > 0 {
		seq := append(append([]Key(nil), e.insertPend...), k)
		node, j, st := walk(e.insertMap, seq, 0)
		switch {
		case st == parseIncomplete && j == len(seq):
			e.insertPend = seq
			return
		case st == parseComplete && j == len(seq):
			e.insertPend = nil
			m := node.Binding.Value.Mapping
			if m == "<Esc>" || m == "<esc>" {
				e.recordInsertKey("<Esc>")
				e.exitInsert()
				return
			}
			e.runMapping(m, &ctx{e: e, w: e.win, b: e.win.buf, count: 1})
			return
		}
		if len(e.insertPend) > 0 {
			e.flushInsertPending()
			// the key that broke the sequence may start a new mapping
			e.insertKey(k)
			return
		}
	}
	e.recordInsertKey(k)
	e.insertKeyRaw(k)
}

func (e *Editor) recordInsertKey(k Key) {
	e.insert.keys = append(e.insert.keys, k)
	if e.dot.recording && e.dot.replay == 0 {
		e.dot.insertKeys = append(e.dot.insertKeys, k)
	}
}

// flushInsertPending types keys held back by a partial insert mapping.
func (e *Editor) flushInsertPending() {
	pend := e.insertPend
	e.insertPend = nil
	for _, pk := range pend {
		e.recordInsertKey(pk)
		e.insertKeyRaw(pk)
	}
}

// insertKeyRaw performs the effect of one key in insert/replace mode.
func (e *Editor) insertKeyRaw(k Key) {
	w := e.win
	b := w.buf
	o := e.options(b)
	if e.insert.compl != nil && k != "<C-n>" && k != "<C-p>" && k != "<Down>" && k != "<Up>" {
		e.insert.compl = nil
		if k == "<CR>" {
			return // accept completion
		}
	}
	if strings.HasPrefix(string(k), "\x00paste:") {
		w.cur = b.Insert(w.cur, string(k)[7:])
		return
	}
	switch k {
	case "<Esc>", "<C-c>", "<C-[>":
		e.exitInsert()
	case "<CR>", "<C-j>", "<C-m>":
		line := b.LineString(w.cur.Line)
		before := string([]rune(line)[:w.cur.Col])
		indent := ""
		if o.AutoIndent {
			indent = leadingWS(line)
			if len([]rune(indent)) > w.cur.Col {
				indent = string([]rune(indent)[:w.cur.Col])
			}
			if e.indentAfter(b, before) {
				indent += e.indentUnit(b)
			}
		}
		// drop trailing whitespace left on the line we break
		trimmed := strings.TrimRight(before, " \t")
		if trimmed != before && strings.TrimSpace(trimmed) == "" {
			b.Delete(Pos{w.cur.Line, 0}, w.cur)
			w.cur.Col = 0
		}
		w.cur = b.Insert(w.cur, "\n"+indent)
		// skip whitespace that followed the cursor
		rest := b.Line(w.cur.Line)
		n := 0
		for n+w.cur.Col < len(rest) && (rest[w.cur.Col+n] == ' ' || rest[w.cur.Col+n] == '\t') {
			n++
		}
		if n > 0 {
			b.Delete(w.cur, Pos{w.cur.Line, w.cur.Col + n})
		}
	case "<BS>", "<C-h>":
		if e.mode == ModeReplace {
			if n := len(e.insert.replaced); n > 0 && w.cur.Col > 0 {
				orig := e.insert.replaced[n-1]
				e.insert.replaced = e.insert.replaced[:n-1]
				w.cur.Col--
				b.Delete(w.cur, Pos{w.cur.Line, w.cur.Col + 1})
				if orig != 0 {
					b.Insert(w.cur, string(orig))
				}
			} else if w.cur.Col > 0 {
				w.cur.Col--
			}
			return
		}
		if w.cur.Col == 0 {
			if w.cur.Line > 0 {
				prev := b.LineLen(w.cur.Line - 1)
				b.Delete(Pos{w.cur.Line - 1, prev}, w.cur)
				w.cur = Pos{w.cur.Line - 1, prev}
			}
			return
		}
		line := b.Line(w.cur.Line)
		n := 1
		// soft tab stop: remove a whole indent level of spaces
		if o.ExpandTab && allSpaces(line[:w.cur.Col]) {
			vc := w.cur.Col
			n = (vc-1)%o.ShiftWidth + 1
		}
		// delete an empty bracket pair together
		b.Delete(Pos{w.cur.Line, w.cur.Col - n}, w.cur)
		w.cur.Col -= n
	case "<Del>":
		if w.cur.Col < b.LineLen(w.cur.Line) {
			b.Delete(w.cur, Pos{w.cur.Line, w.cur.Col + 1})
		} else if w.cur.Line < b.LineCount()-1 {
			b.Delete(w.cur, Pos{w.cur.Line + 1, 0})
		}
	case "<Tab>":
		if o.ExpandTab {
			vc := displayCol(b.Line(w.cur.Line), w.cur.Col, o.TabStop)
			n := o.ShiftWidth - vc%o.ShiftWidth
			w.cur = b.Insert(w.cur, strings.Repeat(" ", n))
		} else {
			w.cur = b.Insert(w.cur, "\t")
		}
	case "<C-w>":
		if w.cur.Col == 0 {
			e.insertKeyRaw("<BS>")
			return
		}
		start := w.cur.Col
		line := b.Line(w.cur.Line)
		for start > 0 && (line[start-1] == ' ' || line[start-1] == '\t') {
			start--
		}
		if start > 0 {
			cls := charClass(line[start-1], false)
			for start > 0 && charClass(line[start-1], false) == cls {
				start--
			}
		}
		b.Delete(Pos{w.cur.Line, start}, w.cur)
		w.cur.Col = start
	case "<C-u>":
		line := b.Line(w.cur.Line)
		fnb := 0
		for fnb < len(line) && (line[fnb] == ' ' || line[fnb] == '\t') {
			fnb++
		}
		start := fnb
		if w.cur.Col <= fnb {
			start = 0
		}
		b.Delete(Pos{w.cur.Line, start}, w.cur)
		w.cur.Col = start
	case "<C-t>", "<C-d>":
		old := b.LineLen(w.cur.Line)
		e.shiftLine(b, w.cur.Line, k == "<C-t>")
		w.cur.Col = max(0, w.cur.Col+b.LineLen(w.cur.Line)-old)
	case "<C-r>":
		e.insert.ctrlR = true
	case "<C-o>":
		e.insert.oneShot = true
		e.mode = ModeNormal
		// like vim, repeats/"." only cover text typed after <C-o>
		e.insert.keys = nil
		e.insert.count = 1
		if e.dot.recording {
			e.dot.recording = false
			e.dot.insertKeys = nil
		}
	case "<C-n>", "<C-p>":
		e.complete(k == "<C-n>")
	case "<Left>":
		if w.cur.Col > 0 {
			w.cur.Col--
		}
	case "<Right>":
		if w.cur.Col < b.LineLen(w.cur.Line) {
			w.cur.Col++
		}
	case "<Up>", "<Down>":
		if e.insert.compl != nil {
			e.complete(k == "<Down>")
			return
		}
		l := w.cur.Line - 1
		if k == "<Down>" {
			l = w.cur.Line + 1
		}
		if l >= 0 && l < b.LineCount() {
			w.cur = e.wantPos(w, l)
			w.cur = b.Clamp(w.cur)
		}
	case "<Home>":
		w.cur.Col = 0
	case "<End>":
		w.cur.Col = b.LineLen(w.cur.Line)
	default:
		r, ok := k.Rune()
		if !ok {
			return
		}
		if e.mode == ModeReplace && w.cur.Col < b.LineLen(w.cur.Line) {
			e.insert.replaced = append(e.insert.replaced, b.RuneAt(w.cur))
			b.Replace(w.cur, Pos{w.cur.Line, w.cur.Col + 1}, string(r))
			w.cur.Col++
			return
		}
		if e.mode == ModeReplace {
			e.insert.replaced = append(e.insert.replaced, 0)
		}
		w.cur = b.Insert(w.cur, string(r))
		// dedent a closing bracket typed on an otherwise blank line
		if (r == '}' || r == ']' || r == ')') && o.AutoIndent && !isLisp(b.Filetype) && w.cur.Line > 0 {
			line := b.Line(w.cur.Line)
			ind := w.cur.Col - 1
			prevInd := utf8.RuneCountInString(leadingWS(b.LineString(w.cur.Line - 1)))
			if ind > 0 && allSpaces(line[:ind]) && ind >= prevInd {
				old := b.LineLen(w.cur.Line)
				e.shiftLine(b, w.cur.Line, false)
				w.cur.Col += b.LineLen(w.cur.Line) - old
			}
		}
	}
}

func isLisp(ft string) bool {
	switch ft {
	case "clojure", "scheme", "racket", "common lisp", "emacslisp", "fennel":
		return true
	}
	return false
}

func allSpaces(rs []rune) bool {
	for _, r := range rs {
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- completion

func isKeywordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// complete cycles keyword completion from words in open buffers.
func (e *Editor) complete(next bool) {
	w := e.win
	b := w.buf
	c := e.insert.compl
	if c == nil {
		line := b.Line(w.cur.Line)
		s := w.cur.Col
		for s > 0 && isKeywordRune(line[s-1]) {
			s--
		}
		prefix := string(line[s:w.cur.Col])
		words := e.collectWords(prefix, w)
		if len(words) == 0 {
			e.infof("-- Keyword completion: pattern not found")
			return
		}
		c = &completion{start: Pos{w.cur.Line, s}, words: words, idx: -1, orig: prefix}
		e.insert.compl = c
	}
	n := len(c.words)
	if next {
		c.idx++
		if c.idx >= n {
			c.idx = -1
		}
	} else {
		c.idx--
		if c.idx < -1 {
			c.idx = n - 1
		}
	}
	word := c.orig
	if c.idx >= 0 {
		word = c.words[c.idx]
	}
	b.Replace(c.start, w.cur, word)
	w.cur = Pos{c.start.Line, c.start.Col + utf8.RuneCountInString(word)}
}

func (e *Editor) collectWords(prefix string, w *Window) []string {
	seen := map[string]bool{prefix: true}
	type cand struct {
		word string
		dist int
	}
	var cands []cand
	scan := func(b *buffer.Buffer, near int) {
		for l := 0; l < b.LineCount(); l++ {
			line := b.Line(l)
			for i := 0; i < len(line); {
				if !isKeywordRune(line[i]) {
					i++
					continue
				}
				j := i
				for j < len(line) && isKeywordRune(line[j]) {
					j++
				}
				word := string(line[i:j])
				if len(word) > len(prefix) && strings.HasPrefix(word, prefix) && !seen[word] {
					seen[word] = true
					d := 1 << 20
					if near >= 0 {
						d = abs(l - near)
					}
					cands = append(cands, cand{word, d})
				}
				i = j
			}
		}
	}
	scan(w.buf, w.cur.Line)
	for _, b := range e.buffers {
		if b != w.buf && b.Kind != buffer.KindPreview {
			scan(b, -1)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].dist < cands[j].dist })
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.word)
		if len(out) >= 50 {
			break
		}
	}
	return out
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
