package editor

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"scihe/internal/keymap"
	"scihe/internal/syntax"
)

type cmdline struct {
	kind     byte // ':', '/', '?', '@' (input prompt)
	prompt   string
	text     []rune
	cur      int
	histIdx  int
	histPref string
	prevMode Mode
	onDone   func(text string, ok bool)

	origCur Pos
	origTop int

	compl     []string
	complIdx  int
	complBase int // rune index where the completed word starts

	ctrlR bool
}

func (c *cmdline) insert(s string) {
	rs := []rune(s)
	c.text = append(c.text[:c.cur], append(rs, c.text[c.cur:]...)...)
	c.cur += len(rs)
}

func (e *Editor) startCmdline(kind byte, initial string) {
	prev := e.mode
	if kind == ':' && e.mode.visual() {
		e.exitVisual()
		prev = ModeNormal
	}
	e.cmdline = &cmdline{kind: kind, prompt: string(kind), prevMode: prev, origCur: e.win.cur, origTop: e.win.top}
	e.cmdline.insert(initial)
	e.cmdline.histIdx = len(e.cmdHist[histKey(kind)])
	e.mode = ModeCmdline
}

// histKey maps a command-line kind to its history list ("?" shares "/").
func histKey(kind byte) byte {
	if kind == '?' {
		return '/'
	}
	return kind
}

// stripSearchOffset drops an unescaped trailing delimiter (and any offset
// after it): "/foo/" and "/foo/e" search for "foo".
func stripSearchOffset(text string, delim byte) string {
	for i := 0; i < len(text); i++ {
		if text[i] == '\\' {
			i++
			continue
		}
		if text[i] == delim {
			return text[:i]
		}
	}
	return text
}

// prompt asks the user for a line of text.
// confirmed reads the answer of a (y/N) prompt.
func confirmed(answer string, ok bool) bool {
	return ok && strings.EqualFold(strings.TrimSpace(answer), "y")
}

func (e *Editor) prompt(label, initial string, done func(string, bool)) {
	prev := e.mode
	if prev == ModeCmdline {
		prev = ModeNormal
	}
	if e.mode.visual() {
		e.exitVisual()
		prev = ModeNormal
	}
	e.cmdline = &cmdline{kind: '@', prompt: label, prevMode: prev, onDone: done, origCur: e.win.cur, origTop: e.win.top}
	e.cmdline.insert(initial)
	e.cmdline.histIdx = len(e.cmdHist['@'])
	e.mode = ModeCmdline
}

func (e *Editor) pushHistory(kind byte, s string) {
	if s == "" {
		return
	}
	h := e.cmdHist[kind]
	for i, x := range h {
		if x == s {
			h = append(h[:i], h[i+1:]...)
			break
		}
	}
	h = append(h, s)
	if len(h) > 200 {
		h = h[1:]
	}
	e.cmdHist[kind] = h
}

func (e *Editor) cmdlineKey(k Key) {
	c := e.cmdline
	if c.ctrlR {
		c.ctrlR = false
		switch k {
		case "<C-w>":
			w := e.win
			c.insert(wordAt(w.buf.Line(w.cur.Line), w.cur.Col))
		default:
			if r, ok := keyChar(k); ok {
				if reg, ok := e.regs.Get(r); ok {
					c.insert(strings.ReplaceAll(strings.TrimSuffix(reg.Text, "\n"), "\n", "\r"))
				}
			}
		}
		e.cmdlineChanged()
		return
	}
	if k != "<Tab>" && k != "<S-Tab>" {
		c.compl = nil
	}
	switch k {
	case "<Esc>", "<C-c>":
		e.finishCmdline(false)
	case "<CR>", "<C-j>", "<C-m>":
		e.finishCmdline(true)
	case "<BS>", "<C-h>":
		if len(c.text) == 0 {
			e.finishCmdline(false)
			return
		}
		if c.cur > 0 {
			c.text = append(c.text[:c.cur-1], c.text[c.cur:]...)
			c.cur--
		}
		e.cmdlineChanged()
	case "<Del>":
		if c.cur < len(c.text) {
			c.text = append(c.text[:c.cur], c.text[c.cur+1:]...)
		}
		e.cmdlineChanged()
	case "<Left>":
		if c.cur > 0 {
			c.cur--
		}
	case "<Right>":
		if c.cur < len(c.text) {
			c.cur++
		}
	case "<Home>", "<C-b>":
		c.cur = 0
	case "<End>", "<C-e>":
		c.cur = len(c.text)
	case "<C-u>":
		c.text = c.text[c.cur:]
		c.cur = 0
		e.cmdlineChanged()
	case "<C-w>":
		s := c.cur
		for s > 0 && c.text[s-1] == ' ' {
			s--
		}
		if s > 0 {
			cls := charClass(c.text[s-1], false)
			for s > 0 && c.text[s-1] != ' ' && charClass(c.text[s-1], false) == cls {
				s--
			}
		}
		c.text = append(c.text[:s], c.text[c.cur:]...)
		c.cur = s
		e.cmdlineChanged()
	case "<C-r>":
		c.ctrlR = true
	case "<Up>", "<Down>", "<C-p>", "<C-n>":
		e.cmdHistory(k == "<Up>" || k == "<C-p>")
	case "<Tab>", "<S-Tab>":
		if c.kind == ':' {
			e.cmdComplete(k == "<Tab>")
		}
	default:
		if r, ok := k.Rune(); ok {
			c.insert(string(r))
			e.cmdlineChanged()
		}
	}
}

func wordAt(line []rune, col int) string {
	if col >= len(line) {
		return ""
	}
	s, en := col, col
	for s > 0 && charClass(line[s-1], false) == clsWord {
		s--
	}
	for en < len(line) && charClass(line[en], false) == clsWord {
		en++
	}
	return string(line[s:en])
}

func (e *Editor) cmdHistory(up bool) {
	c := e.cmdline
	h := e.cmdHist[histKey(c.kind)]
	if c.histIdx == len(h) {
		c.histPref = string(c.text)
	}
	i := c.histIdx
	for {
		if up {
			i--
		} else {
			i++
		}
		if i < 0 {
			return
		}
		if i >= len(h) {
			c.histIdx = len(h)
			c.text = []rune(c.histPref)
			c.cur = len(c.text)
			return
		}
		if strings.HasPrefix(h[i], c.histPref) {
			break
		}
	}
	c.histIdx = i
	c.text = []rune(h[i])
	c.cur = len(c.text)
}

// cmdlineChanged updates incremental search.
func (e *Editor) cmdlineChanged() {
	c := e.cmdline
	if c == nil || (c.kind != '/' && c.kind != '?') || !e.cfg.Options.IncSearch {
		return
	}
	w := e.win
	w.cur, w.top = c.origCur, c.origTop
	pat := string(c.text)
	if pat == "" {
		e.search.re = nil
		return
	}
	re, err := e.compilePattern(pat)
	if err != nil {
		return
	}
	if s, _, _, ok := findMatch(w.buf, re, c.origCur, c.kind == '/'); ok {
		w.cur = s
	}
}

func (e *Editor) finishCmdline(ok bool) {
	c := e.cmdline
	e.cmdline = nil
	e.mode = c.prevMode
	text := string(c.text)
	w := e.win
	switch c.kind {
	case '@':
		if ok {
			e.pushHistory('@', text)
		}
		if c.onDone != nil {
			c.onDone(text, ok)
		}
	case ':':
		if !ok {
			return
		}
		e.pushHistory(':', text)
		e.regs.SetSpecial(':', text)
		if e.mappingDepth == 0 { // a mapping cannot plant a command for @:
			e.lastExCmd = text
		}
		e.execEx(text)
	case '/', '?':
		w.cur, w.top = c.origCur, c.origTop
		pop := e.pendingOp
		e.pendingOp = nil
		if !ok {
			return
		}
		text = stripSearchOffset(text, c.kind)
		if text == "" {
			text = e.search.pattern
		}
		e.pushHistory('/', text)
		e.search.pattern = text
		e.search.forward = c.kind == '/'
		e.regs.SetSpecial('/', text)
		cx := &ctx{e: e, w: w, b: w.buf, count: 1}
		if pop != nil {
			cx.count, cx.hasCount, cx.op, cx.reg = pop.count, pop.hasCount, pop.op, pop.reg
		}
		target, mt, found := e.searchMotion(cx, c.kind == '/')
		if !found {
			return
		}
		if pop != nil {
			cx.op = pop.op
			w.buf.BeginUndo(w.cur)
			r := e.rangeFromMotion(cx, target, mt)
			e.applyOperator(cx, pop.op, r)
			if e.mode == ModeInsert {
				e.insert.undoBuf = w.buf
			} else {
				w.buf.EndUndo(w.cur)
			}
			if pop.op != OpYank && pop.op != OpEval && e.dot.replay == 0 && pop.dotKeys != nil {
				keys := append([]Key(nil), pop.dotKeys...)
				keys = append(keys, Key(string(c.kind)))
				for _, r := range text {
					keys = append(keys, keymap.RuneKey(r))
				}
				keys = append(keys, "<CR>")
				e.setDot(keys, pop.dotCount)
			}
			return
		}
		e.pushJump(w)
		e.setCursor(w, target)
	}
}

// ---------------------------------------------------------------- completion

func (e *Editor) cmdComplete(forward bool) {
	c := e.cmdline
	if c.compl == nil {
		text := string(c.text[:c.cur])
		cands, base := e.completeCandidates(text)
		if len(cands) == 0 {
			return
		}
		c.compl = cands
		c.complBase = base
		c.complIdx = -1
	}
	n := len(c.compl)
	if forward {
		c.complIdx = (c.complIdx + 1) % n
	} else {
		c.complIdx = (c.complIdx - 1 + n) % n
	}
	word := []rune(c.compl[c.complIdx])
	c.text = append(append(append([]rune{}, c.text[:c.complBase]...), word...), c.text[c.cur:]...)
	c.cur = c.complBase + len(word)
}

// completeCandidates returns candidates for the word ending at the cursor.
func (e *Editor) completeCandidates(text string) ([]string, int) {
	// strip a range prefix
	rng := 0
	for rng < len(text) && strings.ContainsRune("0123456789,.$%'<>+-; ", rune(text[rng])) {
		rng++
	}
	rest := text[rng:]
	sp := strings.IndexByte(rest, ' ')
	if sp < 0 {
		var out []string
		for _, name := range exCommandNames() {
			if strings.HasPrefix(name, rest) {
				out = append(out, name)
			}
		}
		return out, len([]rune(text[:rng]))
	}
	cmd := rest[:sp]
	cmd = strings.TrimSuffix(cmd, "!")
	arg := rest[sp+1:]
	// complete only the last space-separated word
	lastSp := strings.LastIndexByte(arg, ' ')
	word := arg[lastSp+1:]
	base := len([]rune(text)) - len([]rune(word))
	var cands []string
	switch exCanonical(cmd) {
	case "edit", "write", "split", "vsplit", "read", "cd", "saveas", "new", "vnew", "wq", "source":
		cands = completeFiles(word, cmd == "cd")
	case "buffer", "bdelete":
		for _, b := range e.buffers {
			if strings.Contains(b.Name, word) {
				cands = append(cands, b.Name)
			}
		}
	case "set":
		for _, o := range optionNames() {
			if strings.HasPrefix(o, strings.TrimPrefix(word, "no")) {
				if strings.HasPrefix(word, "no") {
					o = "no" + o
				}
				cands = append(cands, o)
			}
		}
	case "theme", "colorscheme":
		for _, t := range syntax.ThemeNames() {
			if strings.HasPrefix(t, word) {
				cands = append(cands, t)
			}
		}
	case "layout":
		for _, s := range append([]string{"next", "prev"}, layoutNames...) {
			if strings.HasPrefix(s, word) {
				cands = append(cands, s)
			}
		}
	case "nbtype":
		for _, s := range []string{"code", "python", "markdown", "raw"} {
			if strings.HasPrefix(s, word) {
				cands = append(cands, s)
			}
		}
	case "repl":
		for _, s := range []string{"start", "restart", "connect", "jack-in", "quit", "log", "interrupt"} {
			if strings.HasPrefix(s, word) {
				cands = append(cands, s)
			}
		}
	case "setfiletype":
		for _, n := range syntax.LexerNames() {
			n = strings.ToLower(n)
			if strings.HasPrefix(n, word) {
				cands = append(cands, n)
			}
		}
	}
	sort.Strings(cands)
	return cands, base
}

func completeFiles(word string, dirsOnly bool) []string {
	if word == "~" {
		word = "~/"
	}
	w := expandHome(word)
	dir, prefix := filepath.Split(w)
	d := dir
	if d == "" {
		d = "."
	}
	ents, err := os.ReadDir(d)
	if err != nil {
		return nil
	}
	var out []string
	for _, en := range ents {
		name := en.Name()
		if !strings.HasPrefix(name, prefix) || (strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".")) {
			continue
		}
		isDir := en.IsDir()
		if !isDir && en.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(d, name)); err == nil && st.IsDir() {
				isDir = true
			}
		}
		if dirsOnly && !isDir {
			continue
		}
		full := filepath.Join(dir, name)
		if dir == "" {
			full = name
		}
		if word != w && len(word) >= len(prefix) { // keep the user's ~ prefix
			full = word[:len(word)-len(prefix)] + name
		}
		if isDir {
			full += "/"
		}
		out = append(out, full)
	}
	return out
}
