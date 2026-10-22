package editor

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"scihe/internal/buffer"
	"scihe/internal/config"
	"scihe/internal/keymap"
	"scihe/internal/syntax"
	"scihe/internal/table"
)

type exArgs struct {
	name     string
	bang     bool
	arg      string
	hasRange bool
	from, to int  // 0-based inclusive line range
	zero     bool // the range was line 0 (:0put, :0r insert above line 1)
	raw      string
}

type exCmd struct {
	name   string
	min    int // shortest accepted abbreviation
	fn     func(e *Editor, a *exArgs) error
	desc   string
	ranged bool
}

var exCommands []exCmd

func init() {
	exCommands = []exCmd{
		{"write", 1, exWrite, "write buffer to file", true},
		{"wq", 2, exWriteQuit, "write and close window", false},
		{"wall", 2, exWriteAll, "write all modified buffers", false},
		{"wqall", 3, exWriteQuitAll, "write all and quit", false},
		{"xall", 2, exWriteQuitAll, "write all and quit", false},
		{"xit", 1, exXit, "write if modified and close", false},
		{"update", 2, exUpdate, "write if modified", false},
		{"saveas", 3, exSaveAs, "write to a new name", false},
		{"quit", 1, exQuit, "close window / quit", false},
		{"qall", 2, exQuitAll, "quit all", false},
		{"quitall", 5, exQuitAll, "quit all", false},
		{"close", 3, exClose, "close window", false},
		{"only", 2, exOnly, "close other windows", false},
		{"layout", 3, exLayout, "window layout: next | prev | even-horizontal | even-vertical | main-horizontal | main-vertical | tiled", false},
		{"edit", 1, exEdit, "edit a file", false},
		{"enew", 3, exEnew, "edit a new unnamed buffer", false},
		{"split", 2, exSplit, "split window", false},
		{"vsplit", 2, exVsplit, "split window vertically", false},
		{"new", 3, exNew, "new window with empty buffer", false},
		{"vnew", 3, exVnew, "new vertical window with empty buffer", false},
		{"buffer", 1, exBuffer, "go to buffer", false},
		{"bnext", 2, exBnext, "next buffer", false},
		{"bprevious", 2, exBprev, "previous buffer", false},
		{"bNext", 2, exBprev, "previous buffer", false},
		{"bdelete", 2, exBdelete, "delete buffer", false},
		{"bwipeout", 2, exBdelete, "delete buffer", false},
		{"bdall", 3, exBdeleteAll, "delete all buffers (! = discard changes)", false},
		{"buffers", 7, exLs, "buffer list (mark, close, rename, copy, save)", false},
		{"ls", 2, exLs, "buffer list (mark, close, rename, copy, save)", false},
		{"files", 5, exLs, "buffer list (mark, close, rename, copy, save)", false},
		{"substitute", 1, exSubstitute, "search and replace", true},
		{"&&", 2, exRepeatSub, "repeat last :s with flags", true},
		{"&", 1, exRepeatSub, "repeat last :s", true},
		{"global", 1, exGlobal, "run command on matching lines", true},
		{"vglobal", 1, exVglobal, "run command on non-matching lines", true},
		{"delete", 1, exDelete, "delete lines", true},
		{"yank", 1, exYank, "yank lines", true},
		{"put", 2, exPut, "put register below line", true},
		{"move", 1, exMove, "move lines", true},
		{"copy", 2, exCopy, "copy lines", true},
		{"t", 1, exCopy, "copy lines", true},
		{"join", 1, exJoin, "join lines", true},
		{"normal", 4, exNormal, "run normal-mode keys on lines", true},
		{">", 1, exShiftRight, "shift lines right", true},
		{"<", 1, exShiftLeft, "shift lines left", true},
		{"sort", 3, exSort, "sort lines", true},
		{"nohlsearch", 3, exNohl, "clear search highlight", false},
		{"set", 2, exSet, "set option", false},
		{"setlocal", 4, exSet, "set option", false},
		{"setfiletype", 4, exSetFiletype, "set filetype / language", false},
		{"registers", 3, exRegisters, "show registers", false},
		{"display", 2, exRegisters, "show registers", false},
		{"mark", 4, exMark, "table: mark cells of a column matching [column] OP VALUE", false},
		{"marks", 4, exMarks, "show marks", false},
		{"messages", 3, exMessages, "show message history", false},
		{"pwd", 2, exPwd, "print working directory", false},
		{"cd", 2, exCd, "change directory", false},
		{"read", 1, exRead, "insert file or command output", true},
		{"!", 1, exBang, "run shell command / filter lines", true},
		{"undo", 1, exUndo, "undo", false},
		{"redo", 3, exRedo, "redo", false},
		{"colorscheme", 4, exTheme, "set colour theme", false},
		{"theme", 2, exTheme, "set colour theme", false},
		{"git", 3, exGit, "git: status | diff [args] | log [%|file] | blame | commit | add [file] | <any git args>", false},
		{"config", 4, exConfig, "config file: edit | save | path", false},
		{"help", 1, exHelp, "show key bindings and commands", false},
		{"languages", 4, exLanguages, "list highlighted languages", false},
		{"explore", 2, exExplore, "browse directory", false},
		{"Explore", 1, exExplore, "browse directory", false},
		{"find", 3, exFind, "fuzzy find file", false},
		{"grep", 2, exGrep, "search files in project", false},
		{"run", 3, exRun, "run current file", false},
		{"repl", 4, exRepl, "REPL: start [NAME] | restart | connect [addr] | jack-in | quit | log | interrupt", false},
		{"eval", 3, exEval, "evaluate lines in REPL / kernel", true},
		{"nbrun", 5, exNbRun, "notebook: run current cell", false},
		{"nbrunall", 6, exNbRunAll, "notebook: run all cells", false},
		{"nbclear", 4, exNbClear, "notebook: clear outputs (! = all)", false},
		{"nbinterrupt", 4, exNbInterrupt, "kernel: interrupt", false},
		{"nbrestart", 4, exNbRestart, "kernel: restart", false},
		{"nbimage", 4, exNbImage, "notebook: open image outputs", false},
		{"nboutput", 4, exNbOutput, "notebook: show full output of cell", false},
		{"nbtype", 4, exNbType, "notebook: set cell type code|python|markdown|raw (no arg: toggle)", false},
		{"nbexport", 4, exNbExport, "notebook: export as percent-format .py", false},
		{"preview", 4, exPreview, "toggle markdown preview", false},
		{"table", 5, exTable, "toggle CSV/TSV table view [delim] [noheader]", false},
		{"where", 5, exWhere, "table: filter rows [column] OP VALUE (no arg clears)", false},
		{"stats", 5, exStats, "table: summary of selection or [columns|*]", true},
		{"hist", 4, exHist, "table: histogram / value counts [bins]", true},
		{"recode", 3, exRecode, "table: make cells numeric [auto|number|bool|codes|map V=C …] (! = in place)", true},
		{"column", 3, exColumn, "table: column rename NAME | delete | add [NAME]", false},
		{"corr", 4, exCorr, "table: correlation matrix [pearson|spearman|kendall] [columns|*]", true},
		{"cov", 3, exCov, "table: covariance matrix [columns|*]", true},
		{"quantile", 5, exQuantile, "table: quantile P (0.9 or 90) [columns]", true},
		{"groupby", 5, exGroupBy, "table: KEY[,KEY…] [FN COLUMN …] → new table", false},
		{"freq", 4, exFreq, "table: frequency table of the column", true},
		{"compute", 4, exCompute, "table: NAME = EXPRESSION → new column; mean(x), std(x), … are column aggregates (! replaces)", false},
		{"transform", 5, exTransform, "table: KIND [columns] or EXPRESSION with _ for the column (! in place); KIND: z minmax pct log log10 log2 sqrt square exp abs rank cumsum diff", true},
		{"MarkdownPreview", 2, exPreview, "toggle markdown preview", false},
	}
}

func init() {
	for _, fn := range table.Funcs {
		exCommands = append(exCommands, exCmd{fn, len(fn), exAggregate(fn), "table: " + fn + " of selection or [columns] (! = per row)", true})
	}
}

func exCommandNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range exCommands {
		if !seen[c.name] && unicode.IsLetter(rune(c.name[0])) {
			seen[c.name] = true
			out = append(out, c.name)
		}
	}
	sort.Strings(out)
	return out
}

func lookupEx(name string) *exCmd {
	for i := range exCommands {
		c := &exCommands[i]
		if name == c.name || (len(name) >= c.min && strings.HasPrefix(c.name, name)) {
			return c
		}
	}
	return nil
}

func exCanonical(name string) string {
	if c := lookupEx(name); c != nil {
		return c.name
	}
	return name
}

// execEx parses and runs one command line.
func (e *Editor) execEx(line string) {
	if err := e.runEx(line); err != nil {
		e.errorf("%v", err)
	}
}

func (e *Editor) runEx(line string) error {
	line = strings.TrimLeft(line, " \t:")
	if line == "" {
		return nil
	}
	w := e.win
	b := w.buf
	a := &exArgs{raw: line}
	e.rangeZero = false
	from, to, has, rest, err := e.parseRange(line)
	if err != nil {
		return err
	}
	a.from, a.to, a.hasRange, a.zero = from, to, has, e.rangeZero
	rest = strings.TrimLeft(rest, " ")
	if rest == "" {
		if has {
			// :N goes to line N
			e.pushJump(w)
			e.setCursor(w, Pos{to, firstNonBlank(b, to)})
		}
		return nil
	}
	// command name: letters, or a single symbol (! & < > =)
	var name string
	switch {
	case strings.HasPrefix(rest, "&&"):
		name, rest = "&&", rest[2:]
	case strings.ContainsRune("!&<>=", rune(rest[0])):
		name, rest = rest[:1], rest[1:]
		for (name == "<" || name == ">") && len(rest) > 0 && rest[0] == name[0] {
			rest = rest[1:]
			a.arg = "x" + a.arg // shift count
		}
	default:
		i := 0
		for i < len(rest) && unicode.IsLetter(rune(rest[i])) {
			i++
		}
		name, rest = rest[:i], rest[i:]
		// :s#a#b#, :g!/x/ — a delimiter right after the name
		if name == "" {
			return fmt.Errorf("E492: Not an editor command: %s", line)
		}
	}
	// ":r!cmd" means "read command output", not ":read!"
	if strings.HasPrefix(rest, "!") && name != "!" && exCanonical(name) != "read" {
		a.bang = true
		rest = rest[1:]
	}
	cmd := lookupEx(name)
	if cmd == nil {
		// :s with a letter delimiter is not supported; :sno etc. neither
		return fmt.Errorf("E492: Not an editor command: %s", line)
	}
	a.name = cmd.name
	if cmd.name == ">" || cmd.name == "<" {
		a.arg = a.arg + strings.TrimSpace(rest)
	} else if cmd.name == "substitute" || cmd.name == "global" || cmd.name == "vglobal" || cmd.name == "&" || cmd.name == "&&" || cmd.name == "!" || cmd.name == "normal" {
		a.arg = strings.TrimPrefix(rest, " ")
		if cmd.name == "normal" {
			a.arg = strings.TrimPrefix(rest, " ")
		}
	} else {
		a.arg = strings.TrimSpace(rest)
	}
	if !has {
		a.from, a.to = w.cur.Line, w.cur.Line
	}
	if a.from > a.to {
		a.from, a.to = a.to, a.from
	}
	// one undo step per command line, even when it runs nested commands
	b.BeginUndo(w.cur)
	b.HoldUndo()
	err = cmd.fn(e, a)
	b.ReleaseUndo(e.win.cur)
	if e.win.buf == b || e.bufferAlive(b) {
		b.EndUndo(e.win.cur)
	}
	if e.win.buf != b {
		e.win.buf.EndUndo(e.win.cur)
	}
	if e.win != nil {
		e.clampCursor(e.win)
	}
	return err
}

// ---------------------------------------------------------------- ranges

func (e *Editor) parseRange(s string) (int, int, bool, string, error) {
	w := e.win
	b := w.buf
	last := b.LineCount() - 1
	if strings.HasPrefix(s, "%") {
		return 0, last, true, s[1:], nil
	}
	var addrs []int
	cur := w.cur.Line
	has := false
	for {
		s = strings.TrimLeft(s, " ")
		addr, ok, rest, err := e.parseAddress(s, cur)
		if err != nil {
			return 0, 0, false, s, err
		}
		if ok {
			has = true
			s = rest
		} else {
			addr = cur
		}
		if len(s) > 0 && (s[0] == ',' || s[0] == ';') {
			if !ok {
				addr = cur
			}
			addrs = append(addrs, addr)
			has = true
			if s[0] == ';' {
				cur = addr
			}
			s = s[1:]
			continue
		}
		if ok {
			addrs = append(addrs, addr)
		}
		break
	}
	if !has {
		return 0, 0, false, s, nil
	}
	if len(addrs) == 0 {
		addrs = []int{cur}
	}
	from, to := addrs[0], addrs[len(addrs)-1]
	if len(addrs) >= 2 {
		from, to = addrs[len(addrs)-2], addrs[len(addrs)-1]
	}
	if from < -1 || to < -1 {
		return 0, 0, false, s, errors.New("E16: Invalid range")
	}
	e.rangeZero = to == -1
	// like vim, a line past the end means the last line, 0 means line 1
	from, to = min(max(from, 0), last), min(max(to, 0), last)
	return from, to, true, s, nil
}

func (e *Editor) parseAddress(s string, cur int) (int, bool, string, error) {
	b := e.win.buf
	addr := 0
	ok := false
	switch {
	case s == "":
		return 0, false, s, nil
	case s[0] == '.':
		addr, ok, s = cur, true, s[1:]
	case s[0] == '$':
		addr, ok, s = b.LineCount()-1, true, s[1:]
	case s[0] >= '0' && s[0] <= '9':
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		n, _ := strconv.Atoi(s[:i])
		addr, ok, s = n-1, true, s[i:] // line 0 is -1 ("before the first line")
	case s[0] == '\'' && len(s) > 1:
		p, found := e.markPos(b, rune(s[1]))
		if !found {
			return 0, false, s, errors.New("E20: Mark not set")
		}
		addr, ok, s = p.Line, true, s[2:]
	case s[0] == '/' || s[0] == '?':
		delim := s[0]
		end := 1
		for end < len(s) && s[end] != delim {
			if s[end] == '\\' {
				end++
			}
			end++
		}
		pat := s[1:min(end, len(s))]
		if pat == "" {
			pat = e.search.pattern
		}
		re, err := e.compilePattern(pat)
		if err != nil {
			return 0, false, s, err
		}
		start := Pos{cur, b.LineLen(cur)}
		if delim == '?' {
			start = Pos{cur, 0}
		}
		p, _, _, found := findMatch(b, re, start, delim == '/')
		if !found {
			return 0, false, s, fmt.Errorf("E486: Pattern not found: %s", pat)
		}
		addr, ok = p.Line, true
		if end < len(s) {
			s = s[end+1:]
		} else {
			s = ""
		}
	case s[0] == '+' || s[0] == '-':
		addr, ok = cur, true
	}
	// offsets
	for len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		sign := 1
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		n := 1
		if i > 0 {
			n, _ = strconv.Atoi(s[:i])
		}
		s = s[i:]
		addr += sign * n
	}
	return addr, ok, s, nil
}

// ---------------------------------------------------------------- files

func exWrite(e *Editor, a *exArgs) error {
	b := e.win.buf
	if tv := e.tableFor(b); tv != nil && a.arg == "" {
		return e.tableApply(tv)
	}
	if strings.HasPrefix(a.arg, "!") {
		// :w !cmd — pipe buffer (or range) to a command
		if e.mappingDepth > 0 {
			return errMappingShell
		}
		from, to := 0, b.LineCount()-1
		if a.hasRange {
			from, to = a.from, a.to
		}
		input := e.linesText(b, from, to) + "\n"
		out, err := runShell(strings.TrimPrefix(a.arg, "!"), input, e.win.buf)
		e.showOutput("*shell*", out)
		return err
	}
	if a.hasRange && a.arg != "" {
		path := expandHome(a.arg)
		if _, err := os.Stat(path); err == nil && !a.bang {
			return errors.New("E13: File exists (add ! to override)")
		}
		text := e.linesText(b, a.from, a.to) + "\n"
		return os.WriteFile(path, []byte(text), 0o644)
	}
	return e.writeBuffer(b, a.arg, a.bang)
}

// writeBuffer saves b (to path if non-empty).
func (e *Editor) writeBuffer(b *buffer.Buffer, path string, force bool) error {
	if b.Kind == buffer.KindPreview || b.Kind == buffer.KindRepl || (b.Kind == buffer.KindScratch && path == "") {
		return errors.New("E382: Cannot write, special buffer")
	}
	if path == "" && b.Path == "" {
		return errors.New("E32: No file name")
	}
	if path != "" {
		path = expandHome(path)
		abs, _ := filepath.Abs(path)
		if other := e.findBuffer(abs); other != nil && other != b {
			return errors.New("E139: File is loaded in another buffer")
		}
		if _, err := os.Stat(abs); err == nil && abs != b.Path && !force {
			return errors.New("E13: File exists (add ! to override)")
		}
		path = abs
	}
	if path == "" || path == b.Path {
		if t, ok := e.modTimes[b.ID]; ok && !force && b.Path != "" {
			if st, err := os.Stat(b.Path); err == nil && st.ModTime().After(t) {
				return errors.New("WARNING: The file has been changed since reading it (add ! to override)")
			}
		}
	}
	var err error
	if k := e.kernels[b.ID]; k != nil && k.nb != nil {
		err = e.saveNotebook(b, path)
	} else {
		err = b.Save(path)
	}
	if err != nil {
		return err
	}
	if b.Kind == buffer.KindScratch && path != "" {
		// an output buffer written to a file becomes an ordinary file
		b.Kind = buffer.KindFile
		b.ReadOnly = false
	}
	if b.Filetype == "" {
		b.Filetype = syntax.Detect(b.Path, b.LineString(0))
	}
	e.recordModTime(b)
	e.infof("%q %dL written", b.Name, b.LineCount())
	return nil
}

func exWriteQuit(e *Editor, a *exArgs) error {
	if err := e.writeBuffer(e.win.buf, a.arg, a.bang); err != nil {
		return err
	}
	return exQuit(e, &exArgs{bang: a.bang})
}

func exXit(e *Editor, a *exArgs) error {
	if e.modified(e.win.buf) {
		if err := e.writeBuffer(e.win.buf, a.arg, a.bang); err != nil {
			return err
		}
	}
	return exQuit(e, &exArgs{bang: a.bang})
}

func exUpdate(e *Editor, a *exArgs) error {
	if e.modified(e.win.buf) {
		return e.writeBuffer(e.win.buf, a.arg, a.bang)
	}
	return nil
}

func exSaveAs(e *Editor, a *exArgs) error {
	if a.arg == "" {
		return errors.New("E471: Argument required")
	}
	b := e.win.buf
	abs, _ := filepath.Abs(expandHome(a.arg))
	if _, err := os.Stat(abs); err == nil && !a.bang {
		return errors.New("E13: File exists (add ! to override)")
	}
	old := b.Path
	b.Path = ""
	if err := e.writeBuffer(b, abs, true); err != nil {
		b.Path = old
		return err
	}
	b.Filetype = syntax.Detect(b.Path, b.LineString(0))
	return nil
}

func exWriteAll(e *Editor, a *exArgs) error {
	for _, b := range e.modifiedBuffers() {
		if b.Path == "" {
			continue
		}
		if err := e.writeBuffer(b, "", a.bang); err != nil {
			return err
		}
	}
	return nil
}

func exWriteQuitAll(e *Editor, a *exArgs) error {
	if err := exWriteAll(e, a); err != nil {
		return err
	}
	e.quitAll(a.bang)
	return nil
}

func exQuit(e *Editor, a *exArgs) error {
	w := e.win
	if len(e.windows()) > 1 {
		// buffers stay loaded (like 'hidden'), so closing a window is safe
		e.closeWindow(w)
		return nil
	}
	e.quitAll(a.bang)
	return nil
}

func exQuitAll(e *Editor, a *exArgs) error {
	e.quitAll(a.bang)
	return nil
}

func exClose(e *Editor, a *exArgs) error {
	if !e.closeWindow(e.win) {
		return errors.New("E444: Cannot close last window")
	}
	return nil
}

func exOnly(e *Editor, a *exArgs) error {
	e.onlyWindow()
	return nil
}

func exEdit(e *Editor, a *exArgs) error {
	w := e.win
	if a.arg == "" {
		b := w.buf
		if b.Path == "" {
			return nil
		}
		if e.modified(b) && !a.bang {
			return errors.New("E37: No write since last change (add ! to override)")
		}
		return e.reload(b)
	}
	b, err := e.loadBuffer(a.arg)
	if err != nil {
		return err
	}
	e.pushJump(w)
	e.setBuffer(w, b)
	return nil
}

// reload re-reads a buffer from disk.
func (e *Editor) reload(b *buffer.Buffer) error {
	if k := e.kernels[b.ID]; k != nil && k.nb != nil {
		return e.reloadNotebook(b)
	}
	nb, err := buffer.Load(b.Path)
	if err != nil {
		return err
	}
	// an undoable change: u brings back the text from before the reload
	if text := nb.Text(); text != b.Text() {
		ro := b.ReadOnly
		b.ReadOnly = false
		b.BeginUndo(Pos{})
		b.Replace(Pos{}, b.End(), text)
		b.EndUndo(Pos{})
		b.ReadOnly = ro
	}
	b.CRLF, b.NoEOL, b.BlankLine = nb.CRLF, nb.NoEOL, nb.BlankLine
	b.MarkSaved()
	e.recordModTime(b)
	e.forWindows(func(w *Window) {
		if w.buf == b {
			w.cur = b.Clamp(w.cur)
		}
	})
	e.infof("%q reloaded", b.Name)
	return nil
}

func exEnew(e *Editor, a *exArgs) error {
	b := buffer.New("[No Name]")
	e.addBuffer(b)
	e.setBuffer(e.win, b)
	return nil
}

func (e *Editor) splitWith(arg string, vertical bool, empty bool) error {
	b := e.win.buf
	if empty {
		b = buffer.New("[No Name]")
		e.addBuffer(b)
	} else if arg != "" {
		var err error
		b, err = e.loadBuffer(arg)
		if err != nil {
			return err
		}
	}
	e.split(b, vertical)
	return nil
}

func exSplit(e *Editor, a *exArgs) error  { return e.splitWith(a.arg, false, false) }
func exVsplit(e *Editor, a *exArgs) error { return e.splitWith(a.arg, true, false) }
func exNew(e *Editor, a *exArgs) error    { return e.splitWith(a.arg, false, a.arg == "") }
func exVnew(e *Editor, a *exArgs) error   { return e.splitWith(a.arg, true, a.arg == "") }

func exBuffer(e *Editor, a *exArgs) error {
	if a.arg == "" {
		e.pickBuffer()
		return nil
	}
	if n, err := strconv.Atoi(a.arg); err == nil {
		for _, b := range e.buffers {
			if b.ID == n {
				e.setBuffer(e.win, b)
				return nil
			}
		}
		return fmt.Errorf("E86: Buffer %d does not exist", n)
	}
	var match []*buffer.Buffer
	for _, b := range e.buffers {
		if b.Name == a.arg || b.Path == a.arg {
			e.setBuffer(e.win, b)
			return nil
		}
		if strings.Contains(b.Name, a.arg) || strings.Contains(b.Path, a.arg) {
			match = append(match, b)
		}
	}
	if len(match) == 1 {
		e.setBuffer(e.win, match[0])
		return nil
	}
	if len(match) > 1 {
		return fmt.Errorf("E93: More than one match for %s", a.arg)
	}
	return fmt.Errorf("E94: No matching buffer for %s", a.arg)
}

func (e *Editor) cycleBuffer(delta int) {
	bs := e.listedBuffers()
	var files []*buffer.Buffer
	for _, b := range bs {
		if b.Kind != buffer.KindPreview {
			files = append(files, b)
		}
	}
	if len(files) < 2 {
		return
	}
	idx := 0
	for i, b := range files {
		if b == e.win.buf {
			idx = i
		}
	}
	idx = (idx + delta + len(files)) % len(files)
	e.setBuffer(e.win, files[idx])
}

func exBnext(e *Editor, a *exArgs) error { e.cycleBuffer(1); return nil }
func exBprev(e *Editor, a *exArgs) error { e.cycleBuffer(-1); return nil }

func exBdelete(e *Editor, a *exArgs) error {
	b := e.win.buf
	if a.arg != "" {
		n, err := strconv.Atoi(a.arg)
		if err != nil {
			return fmt.Errorf("E94: No matching buffer for %s", a.arg)
		}
		b = nil
		for _, x := range e.buffers {
			if x.ID == n {
				b = x
			}
		}
		if b == nil {
			return fmt.Errorf("E516: No buffers were deleted")
		}
	}
	return e.deleteBuffer(b, a.bang)
}

// exBdeleteAll deletes every buffer; without ! it keeps the modified
// ones (and tables with staged edits, with their source).
func exBdeleteAll(e *Editor, a *exArgs) error {
	var kept []string
	n := 0
	for _, b := range e.listedBuffers() {
		if !e.bufferAlive(b) {
			continue
		}
		if !a.bang && e.unsavedWork(b) {
			kept = append(kept, b.Name)
			continue
		}
		if e.logInUse(b) {
			continue // still written to: deleting it would lose the output
		}
		if err := e.deleteBuffer(b, true); err != nil {
			return err
		}
		n++
	}
	if len(kept) > 0 {
		return fmt.Errorf("E89: %d buffer(s) deleted; kept with unsaved changes: %s (add ! to discard)", n, strings.Join(kept, ", "))
	}
	e.infof("%d buffer(s) deleted", n)
	return nil
}

// unsavedWork reports whether deleting b would lose changes: a modified
// file or notebook, or a table view (or its source) with staged edits.
func (e *Editor) unsavedWork(b *buffer.Buffer) bool {
	if e.modified(b) && (b.Kind == buffer.KindFile || b.Kind == buffer.KindNotebook) {
		return true
	}
	if tv := e.tables[b.ID]; tv != nil && tv.pending() {
		return true
	}
	tv := e.tableOfSource(b)
	return tv != nil && tv.pending()
}

// logInUse reports whether b is the log of a running :run / :git job,
// of the jack-in server or of a live REPL session.
func (e *Editor) logInUse(b *buffer.Buffer) bool {
	if b.Kind != buffer.KindRepl {
		return false
	}
	if b.Name == "*nrepl-server*" && e.jackInProc != nil {
		return true
	}
	for name, s := range e.sessions {
		if s != nil && s.Alive() && b.Name == "*repl:"+name+"*" {
			return true
		}
	}
	e.jobsMu.Lock()
	defer e.jobsMu.Unlock()
	for _, name := range e.jobs {
		if name == b.Name {
			return true
		}
	}
	return false
}

func (e *Editor) deleteBuffer(b *buffer.Buffer, force bool) error {
	if !force && e.unsavedWork(b) {
		if e.modified(b) && (b.Kind == buffer.KindFile || b.Kind == buffer.KindNotebook) {
			return fmt.Errorf("E89: No write since last change for buffer %d (add ! to override)", b.ID)
		}
		return fmt.Errorf("E89: buffer %d has a table with staged edits: apply (:w) or discard (ZQ) them (add ! to override)", b.ID)
	}
	// pick a replacement for windows showing b
	var repl *buffer.Buffer
	for _, x := range e.listedBuffers() {
		if x != b && x.Kind != buffer.KindPreview {
			repl = x
		}
	}
	if repl == nil {
		repl = buffer.New("[No Name]")
		e.addBuffer(repl)
	}
	e.forWindows(func(w *Window) {
		if w.buf == b {
			if w.alt != nil && w.alt != b && e.bufferAlive(w.alt) {
				e.setBuffer(w, w.alt)
			} else {
				e.setBuffer(w, repl)
			}
		}
		if w.alt == b {
			w.alt = nil
		}
		delete(w.lastPos, b.ID)
	})
	e.removeBuffer(b)
	return nil
}

// ---------------------------------------------------------------- editing

type substitution struct {
	pattern string
	repl    string
	flags   string
}

func splitDelimited(s string) (parts []string, rest string, ok bool) {
	if s == "" {
		return nil, "", false
	}
	d := s[0]
	if unicode.IsLetter(rune(d)) || unicode.IsDigit(rune(d)) || d == '\\' || d == '"' || d == '|' || d == ' ' {
		return nil, s, false
	}
	s = s[1:]
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			if s[i+1] == d {
				cur.WriteByte(d)
			} else {
				cur.WriteByte(c)
				cur.WriteByte(s[i+1])
			}
			i++
			continue
		}
		if c == d {
			parts = append(parts, cur.String())
			cur.Reset()
			if len(parts) == 2 {
				return parts, s[i+1:], true
			}
			continue
		}
		cur.WriteByte(c)
	}
	parts = append(parts, cur.String())
	return parts, "", true
}

func exSubstitute(e *Editor, a *exArgs) error {
	parts, rest, ok := splitDelimited(a.arg)
	var sub substitution
	if !ok {
		// ":s" alone or ":s g" repeats with new flags
		if e.lastSub == nil {
			return errors.New("E35: No previous regular expression")
		}
		sub = *e.lastSub
		sub.flags = strings.TrimSpace(a.arg)
	} else {
		sub.pattern = parts[0]
		if len(parts) > 1 {
			prev := ""
			if e.lastSub != nil {
				prev = e.lastSub.repl
			}
			sub.repl = expandTilde(parts[1], prev)
		}
		sub.flags = strings.TrimSpace(rest)
		if sub.pattern == "" {
			sub.pattern = e.search.pattern
		}
		if sub.pattern == "" {
			return errors.New("E35: No previous regular expression")
		}
	}
	e.lastSub = &sub
	e.search.pattern = sub.pattern
	e.search.noHL = false
	return e.substitute(sub, a.from, a.to)
}

func exRepeatSub(e *Editor, a *exArgs) error {
	if e.lastSub == nil {
		return errors.New("E35: No previous regular expression")
	}
	sub := *e.lastSub
	if a.name != "&&" {
		sub.flags = strings.TrimSpace(a.arg)
	}
	return e.substitute(sub, a.from, a.to)
}

// expandTilde replaces an unescaped ~ in a replacement with the previous
// replacement string (vim's magic ~).
func expandTilde(repl, prev string) string {
	var sb strings.Builder
	for i := 0; i < len(repl); i++ {
		switch {
		case repl[i] == '\\' && i+1 < len(repl):
			sb.WriteByte(repl[i])
			sb.WriteByte(repl[i+1])
			i++
		case repl[i] == '~':
			sb.WriteString(prev)
		default:
			sb.WriteByte(repl[i])
		}
	}
	return sb.String()
}

// expandReplacement implements vim replacement syntax on one match.
func expandReplacement(repl string, src string, m []int) string {
	var sb strings.Builder
	upperNext, lowerNext := false, false
	upperAll, lowerAll := false, false
	write := func(s string) {
		for _, r := range s {
			switch {
			case upperNext:
				r = unicode.ToUpper(r)
				upperNext = false
			case lowerNext:
				r = unicode.ToLower(r)
				lowerNext = false
			case upperAll:
				r = unicode.ToUpper(r)
			case lowerAll:
				r = unicode.ToLower(r)
			}
			sb.WriteRune(r)
		}
	}
	group := func(n int) string {
		if 2*n+1 < len(m) && m[2*n] >= 0 {
			return src[m[2*n]:m[2*n+1]]
		}
		return ""
	}
	rs := []rune(repl)
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if c == '&' {
			write(group(0))
			continue
		}
		if c != '\\' || i+1 >= len(rs) {
			write(string(c))
			continue
		}
		i++
		n := rs[i]
		switch {
		case n >= '0' && n <= '9':
			write(group(int(n - '0')))
		case n == 'n' || n == 'r':
			sb.WriteByte('\n')
		case n == 't':
			sb.WriteByte('\t')
		case n == 'u':
			upperNext = true
		case n == 'l':
			lowerNext = true
		case n == 'U':
			upperAll, lowerAll = true, false
		case n == 'L':
			lowerAll, upperAll = true, false
		case n == 'e' || n == 'E':
			upperAll, lowerAll = false, false
		default:
			write(string(n))
		}
	}
	return sb.String()
}

func (e *Editor) substitute(sub substitution, from, to int) error {
	b := e.win.buf
	if b.ReadOnly {
		return errors.New("E21: Cannot make changes, buffer is read-only")
	}
	if strings.Contains(sub.flags, "c") {
		return errors.New("the [c]onfirm flag of :s is not supported")
	}
	pat := sub.pattern
	if strings.Contains(sub.flags, "i") {
		pat = `\c` + pat
	} else if strings.Contains(sub.flags, "I") {
		pat = `\C` + pat
	}
	re, err := e.compilePattern(pat)
	if err != nil {
		return err
	}
	global := strings.Contains(sub.flags, "g")
	countOnly := strings.Contains(sub.flags, "n")
	total, lines := 0, 0
	lastLine := -1
	// process bottom-up so inserted newlines do not shift unprocessed lines
	for l := to; l >= from; l-- {
		line := b.LineString(l)
		var out strings.Builder
		last := 0
		n := 0
		for _, m := range re.FindAllStringSubmatchIndex(line, -1) {
			if !global && n > 0 {
				break
			}
			out.WriteString(line[last:m[0]])
			out.WriteString(expandReplacement(sub.repl, line, m))
			last = m[1]
			n++
		}
		if n == 0 {
			continue
		}
		total += n
		lines++
		if lastLine < 0 {
			lastLine = l
		}
		if countOnly {
			continue
		}
		out.WriteString(line[last:])
		b.Replace(Pos{l, 0}, Pos{l, b.LineLen(l)}, out.String())
	}
	if total == 0 {
		if strings.Contains(sub.flags, "e") {
			return nil
		}
		return fmt.Errorf("E486: Pattern not found: %s", sub.pattern)
	}
	if !countOnly {
		e.win.cur = Pos{lastLine, firstNonBlank(b, lastLine)}
	}
	if total > 1 || countOnly {
		verb := "substitutions"
		if countOnly {
			verb = "matches"
		}
		e.infof("%d %s on %d lines", total, verb, lines)
	}
	return nil
}

func (e *Editor) globalCmd(a *exArgs, invert bool) error {
	if a.bang {
		invert = !invert
	}
	pat, rest, ok := splitPattern(a.arg)
	if !ok {
		return errors.New("E476: Invalid command")
	}
	if pat == "" {
		pat = e.search.pattern
	}
	if pat == "" {
		return errors.New("E35: No previous regular expression")
	}
	cmd := "p"
	if strings.TrimSpace(rest) != "" {
		cmd = rest
	}
	re, err := e.compilePattern(pat)
	if err != nil {
		return err
	}
	e.search.pattern = pat // so :g/pat/s//x/ works like vim
	e.search.noHL = false
	from, to := a.from, a.to
	if !a.hasRange {
		from, to = 0, e.win.buf.LineCount()-1
	}
	b := e.win.buf
	// mark matching lines first (like vim), using marks that follow edits
	var marks []int
	for l := from; l <= to; l++ {
		if re.MatchString(b.LineString(l)) != invert {
			marks = append(marks, l)
		}
	}
	if strings.TrimSpace(cmd) == "p" || strings.TrimSpace(cmd) == "print" {
		var lines []string
		for _, l := range marks {
			lines = append(lines, fmt.Sprintf("%6d %s", l+1, b.LineString(l)))
		}
		e.showLines(lines)
		return nil
	}
	// a temporary mark per line follows edits; marks on deleted lines vanish
	const markBase = 0x10000
	for i, l := range marks {
		b.Marks[rune(markBase+i)] = Pos{l, 0}
	}
	defer func() {
		for i := range marks {
			delete(b.Marks, rune(markBase+i))
		}
	}()
	last := -1 // line the command last ran on
	for i := range marks {
		p, ok := b.Marks[rune(markBase+i)]
		if !ok || p.Line >= b.LineCount() {
			continue
		}
		// a mark moved off column 0 onto a line already done: its line
		// was joined into that one (:g/x/j), so it is gone, as in vim
		if p.Col != 0 && p.Line <= last {
			continue
		}
		last = p.Line
		e.win.cur = Pos{p.Line, 0}
		if err := e.runEx(cmd); err != nil {
			return err
		}
	}
	return nil
}

// splitPattern parses "/pat/rest" (any delimiter, \-escapes honoured).
func splitPattern(s string) (pat, rest string, ok bool) {
	if s == "" {
		return "", "", false
	}
	d := s[0]
	if unicode.IsLetter(rune(d)) || unicode.IsDigit(rune(d)) || d == '\\' || d == '"' || d == '|' || d == ' ' {
		return "", "", false
	}
	var sb strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			if s[i+1] != d {
				sb.WriteByte(c)
			}
			sb.WriteByte(s[i+1])
			i++
			continue
		}
		if c == d {
			return sb.String(), s[i+1:], true
		}
		sb.WriteByte(c)
	}
	return sb.String(), "", true
}

func exGlobal(e *Editor, a *exArgs) error  { return e.globalCmd(a, false) }
func exVglobal(e *Editor, a *exArgs) error { return e.globalCmd(a, true) }

func (e *Editor) countArg(a *exArgs) {
	if n, err := strconv.Atoi(strings.TrimSpace(a.arg)); err == nil && n > 0 {
		a.from = a.to
		a.to = min(a.from+n-1, e.win.buf.LineCount()-1)
		a.arg = ""
	}
}

func regArg(a *exArgs) rune {
	s := strings.TrimSpace(a.arg)
	if s != "" && !unicode.IsDigit(rune(s[0])) {
		r := rune(s[0])
		a.arg = strings.TrimSpace(s[1:])
		return r
	}
	return 0
}

func exDelete(e *Editor, a *exArgs) error {
	reg := regArg(a)
	e.countArg(a)
	b := e.win.buf
	if b.ReadOnly {
		return errors.New("E21: Cannot make changes, buffer is read-only")
	}
	r := opRange{start: Pos{a.from, 0}, end: Pos{a.to, b.LineLen(a.to)}, linewise: true}
	e.regs.Delete(reg, e.rangeText(b, r))
	e.win.cur = e.deleteRange(b, r)
	return nil
}

func exYank(e *Editor, a *exArgs) error {
	reg := regArg(a)
	e.countArg(a)
	b := e.win.buf
	r := opRange{start: Pos{a.from, 0}, end: Pos{a.to, b.LineLen(a.to)}, linewise: true}
	e.regs.Yank(reg, e.rangeText(b, r))
	return nil
}

func exPut(e *Editor, a *exArgs) error {
	// the argument is only a register name: :put 0 is register 0, not a count
	var reg rune
	if s := strings.TrimSpace(a.arg); s != "" {
		reg = rune(s[0])
	}
	r, ok := e.regs.Get(reg)
	if !ok {
		return errors.New("E353: Nothing in register")
	}
	r.Type = RegLine
	if !strings.HasSuffix(r.Text, "\n") {
		r.Text += "\n"
	}
	e.win.cur = Pos{a.to, 0}
	c := &ctx{e: e, w: e.win, b: e.win.buf, count: 1}
	e.put(c, r, !a.bang && !a.zero, false)
	return nil
}

func (e *Editor) linesText(b *buffer.Buffer, from, to int) string {
	var sb strings.Builder
	for l := from; l <= to; l++ {
		if l > from {
			sb.WriteByte('\n')
		}
		sb.WriteString(b.LineString(l))
	}
	return sb.String()
}

// targetLine parses the destination of :m / :t (-1 = before the first line).
func (e *Editor) targetLine(arg string) (int, error) {
	addr, ok, rest, err := e.parseAddress(strings.TrimSpace(arg), e.win.cur.Line)
	if err != nil {
		return 0, err
	}
	if !ok || strings.TrimSpace(rest) != "" || addr < -1 || addr >= e.win.buf.LineCount() {
		return 0, errors.New("E14: Invalid address")
	}
	return addr, nil
}

func exMove(e *Editor, a *exArgs) error {
	b := e.win.buf
	dest, err := e.targetLine(a.arg)
	if err != nil {
		return err
	}
	if dest >= a.from && dest < a.to {
		return errors.New("E134: Cannot move a range of lines into itself")
	}
	if (dest >= a.from && dest <= a.to) || dest == a.from-1 {
		return nil // already there
	}
	text := e.linesText(b, a.from, a.to)
	n := a.to - a.from + 1
	e.deleteRange(b, opRange{start: Pos{a.from, 0}, end: Pos{a.to, 0}, linewise: true})
	if dest > a.to {
		dest -= n
	}
	if dest < 0 {
		b.Insert(Pos{0, 0}, text+"\n")
		e.win.cur = Pos{n - 1, 0}
	} else {
		b.Insert(Pos{dest, b.LineLen(dest)}, "\n"+text)
		e.win.cur = Pos{dest + n, 0}
	}
	return nil
}

func exCopy(e *Editor, a *exArgs) error {
	b := e.win.buf
	dest, err := e.targetLine(a.arg)
	if err != nil {
		return err
	}
	text := e.linesText(b, a.from, a.to)
	n := a.to - a.from + 1
	if dest < 0 {
		b.Insert(Pos{0, 0}, text+"\n")
		e.win.cur = Pos{n - 1, 0}
	} else {
		b.Insert(Pos{dest, b.LineLen(dest)}, "\n"+text)
		e.win.cur = Pos{dest + n, 0}
	}
	return nil
}

func exJoin(e *Editor, a *exArgs) error {
	n := a.to - a.from + 1
	if !a.hasRange || a.from == a.to {
		n = 2
	}
	e.win.cur = Pos{a.from, 0}
	e.joinLines(e.win, n, !a.bang)
	return nil
}

func exNormal(e *Editor, a *exArgs) error {
	keys := keymap.Parse(a.arg, e.leader)
	if len(keys) == 0 {
		return nil
	}
	b := e.win.buf
	if !a.hasRange {
		e.feedKeys(keys)
		e.forceNormal()
		return nil
	}
	const markBase = 0x20000
	for l := a.from; l <= a.to; l++ {
		b.Marks[rune(markBase+l-a.from)] = Pos{l, 0}
	}
	for l := a.from; l <= a.to; l++ {
		p, ok := b.Marks[rune(markBase+l-a.from)]
		delete(b.Marks, rune(markBase+l-a.from))
		if !ok || p.Line >= b.LineCount() {
			continue
		}
		e.win.cur = Pos{p.Line, 0}
		e.feedKeys(keys)
		e.forceNormal()
	}
	return nil
}

// forceNormal ends insert/visual/cmdline modes left open by :normal.
func (e *Editor) forceNormal() {
	switch {
	case e.mode == ModeInsert || e.mode == ModeReplace:
		e.exitInsert()
	case e.mode.visual():
		e.exitVisual()
	case e.mode == ModeCmdline:
		e.cmdline = nil
		e.mode = ModeNormal
	}
	e.resetPending()
}

func exShift(e *Editor, a *exArgs, right bool) error {
	n := strings.Count(a.arg, "x") + 1
	a.arg = strings.ReplaceAll(a.arg, "x", "")
	e.countArg(a)
	b := e.win.buf
	for l := a.from; l <= a.to; l++ {
		for i := 0; i < n; i++ {
			e.shiftLine(b, l, right)
		}
	}
	e.win.cur = Pos{a.to, firstNonBlank(b, a.to)}
	return nil
}

func exShiftRight(e *Editor, a *exArgs) error { return exShift(e, a, true) }
func exShiftLeft(e *Editor, a *exArgs) error  { return exShift(e, a, false) }

func exSort(e *Editor, a *exArgs) error {
	b := e.win.buf
	if tv := e.tableFor(b); tv != nil {
		return exTableSort(e, tv, a)
	}
	from, to := a.from, a.to
	if !a.hasRange {
		from, to = 0, b.LineCount()-1
	}
	lines := strings.Split(e.linesText(b, from, to), "\n")
	flags := a.arg
	numeric := strings.Contains(flags, "n")
	icase := strings.Contains(flags, "i")
	uniq := strings.Contains(flags, "u")
	var numRe = regexp.MustCompile(`-?\d+(\.\d+)?`)
	key := func(s string) string {
		if icase {
			return strings.ToLower(s)
		}
		return s
	}
	num := func(s string) float64 {
		m := numRe.FindString(s)
		if m == "" {
			return -1e308
		}
		f, _ := strconv.ParseFloat(m, 64)
		return f
	}
	sort.SliceStable(lines, func(i, j int) bool {
		if numeric {
			return num(lines[i]) < num(lines[j])
		}
		return key(lines[i]) < key(lines[j])
	})
	if a.bang {
		for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
			lines[i], lines[j] = lines[j], lines[i]
		}
	}
	if uniq {
		out := lines[:0]
		for i, l := range lines {
			if i == 0 || key(l) != key(out[len(out)-1]) {
				out = append(out, l)
			}
		}
		lines = out
	}
	b.Replace(Pos{from, 0}, Pos{to, b.LineLen(to)}, strings.Join(lines, "\n"))
	return nil
}

func exNohl(e *Editor, a *exArgs) error {
	e.search.noHL = true
	return nil
}

func exUndo(e *Editor, a *exArgs) error {
	b := e.win.buf
	b.EndUndo(e.win.cur)
	if p, ok := b.Undo(); ok {
		e.win.cur = p
	}
	return nil
}

func exRedo(e *Editor, a *exArgs) error {
	b := e.win.buf
	b.EndUndo(e.win.cur)
	if p, ok := b.Redo(); ok {
		e.win.cur = p
	}
	return nil
}

// ---------------------------------------------------------------- options

func optionNames() []string {
	return []string{"number", "relativenumber", "cursorline", "tabstop", "shiftwidth", "expandtab",
		"scrolloff", "ignorecase", "smartcase", "hlsearch", "incsearch", "autoindent", "filetype", "transparent",
		"wrap", "linebreak", "autoreload"}
}

var optAbbrev = map[string]string{
	"nu": "number", "rnu": "relativenumber", "cul": "cursorline", "ts": "tabstop", "sw": "shiftwidth",
	"et": "expandtab", "so": "scrolloff", "ic": "ignorecase", "scs": "smartcase", "hls": "hlsearch",
	"is": "incsearch", "ai": "autoindent", "ft": "filetype", "lbr": "linebreak", "ar": "autoreload", "autoread": "autoreload",
}

func exSet(e *Editor, a *exArgs) error {
	if a.arg == "" {
		o := e.options(e.win.buf)
		e.showLines([]string{fmt.Sprintf("tabstop=%d shiftwidth=%d expandtab=%v number=%v relativenumber=%v wrap=%v linebreak=%v filetype=%s",
			o.TabStop, o.ShiftWidth, o.ExpandTab, o.Number, o.RelativeNumber, o.Wrap, o.LineBreak, e.win.buf.Filetype)})
		return nil
	}
	for _, item := range strings.Fields(a.arg) {
		if err := e.setOption(item); err != nil {
			return err
		}
	}
	return nil
}

func (e *Editor) setOption(item string) error {
	b := e.win.buf
	o := e.options(b)
	g := &e.cfg.Options
	name, val, hasVal := strings.Cut(item, "=")
	toggle := strings.HasSuffix(name, "!")
	query := strings.HasSuffix(name, "?")
	name = strings.TrimRight(name, "!?")
	neg := false
	if !hasVal && strings.HasPrefix(name, "no") && name != "number" {
		neg = true
		name = name[2:]
	}
	if full, ok := optAbbrev[name]; ok {
		name = full
	}
	if hasVal && name != "filetype" {
		switch val {
		case "true", "on", "yes", "1":
		case "false", "off", "no", "0":
			neg = true
		default:
			if _, err := strconv.Atoi(val); err != nil {
				return fmt.Errorf("E474: Invalid argument: %s", item)
			}
		}
	}
	setBool := func(local, global *bool) {
		v := !neg
		if toggle {
			v = !*local
		}
		if query {
			e.infof("%s%s", map[bool]string{true: "", false: "no"}[*local], name)
			return
		}
		*local = v
		if global != nil {
			*global = v
		}
	}
	setInt := func(local, global *int) error {
		if query || !hasVal {
			e.infof("%s=%d", name, *local)
			return nil
		}
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return fmt.Errorf("E521: Number required after =: %s", item)
		}
		*local = n
		if global != nil {
			*global = n
		}
		return nil
	}
	switch name {
	case "number":
		setBool(&o.Number, &g.Number)
	case "relativenumber":
		setBool(&o.RelativeNumber, &g.RelativeNumber)
	case "cursorline":
		setBool(&o.CursorLine, &g.CursorLine)
	case "expandtab":
		setBool(&o.ExpandTab, nil)
	case "autoindent":
		setBool(&o.AutoIndent, nil)
	case "wrap": // buffer-local; the config sets the default
		setBool(&o.Wrap, nil)
		e.win.left = 0
	case "linebreak":
		setBool(&o.LineBreak, nil)
	case "autoreload": // buffer-local; the config sets the default
		if tv := e.tableFor(b); tv != nil {
			o = e.options(tv.src) // a table view reloads through its file buffer
		}
		setBool(&o.Autoreload, nil)
	case "ignorecase":
		setBool(&g.IgnoreCase, nil)
	case "smartcase":
		setBool(&g.SmartCase, nil)
	case "hlsearch":
		setBool(&g.HLSearch, nil)
		e.search.noHL = false
	case "incsearch":
		setBool(&g.IncSearch, nil)
	case "tabstop":
		if err := setInt(&o.TabStop, nil); err != nil {
			return err
		}
		if o.TabStop == 0 {
			o.TabStop = 8
		}
		o.TabStop = min(o.TabStop, config.MaxTabStop)
	case "shiftwidth":
		if err := setInt(&o.ShiftWidth, nil); err != nil {
			return err
		}
		if o.ShiftWidth == 0 {
			o.ShiftWidth = o.TabStop
		}
	case "scrolloff":
		return setInt(&o.ScrollOff, &g.ScrollOff)
	case "transparent":
		v := e.cfg.Transparent
		setBool(&v, nil)
		if v != e.cfg.Transparent {
			e.applyTheme(e.cfg.Theme, v)
			e.saveSettings()
		}
	case "filetype":
		if !hasVal {
			e.infof("filetype=%s", b.Filetype)
			return nil
		}
		return exSetFiletype(e, &exArgs{arg: val})
	default:
		return fmt.Errorf("E518: Unknown option: %s", name)
	}
	return nil
}

func exSetFiletype(e *Editor, a *exArgs) error {
	ft := strings.ToLower(strings.TrimSpace(a.arg))
	if ft != "" && syntax.Lexer(ft) == nil {
		return fmt.Errorf("unknown language: %s", ft)
	}
	if l := syntax.Lexer(ft); l != nil {
		ft = strings.ToLower(l.Config().Name)
	}
	b := e.win.buf
	b.Filetype = ft
	delete(e.opts, b.ID)
	return nil
}

// ---------------------------------------------------------------- info

func exRegisters(e *Editor, a *exArgs) error {
	lines := []string{"Type Name Content"}
	for _, r := range e.regs.Names() {
		reg := e.regs.m[r]
		t := "c"
		if reg.Type == RegLine {
			t = "l"
		} else if reg.Type == RegBlock {
			t = "b"
		}
		txt := strings.ReplaceAll(reg.Text, "\n", "^J")
		if len(txt) > 70 {
			txt = txt[:70]
		}
		lines = append(lines, fmt.Sprintf("  %s  \"%c   %s", t, r, txt))
	}
	e.showLines(lines)
	return nil
}

func exMarks(e *Editor, a *exArgs) error {
	b := e.win.buf
	lines := []string{"mark line  col text"}
	var keys []rune
	for k := range b.Marks {
		if k < 0x10000 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		p := b.Clamp(b.Marks[k])
		lines = append(lines, fmt.Sprintf(" %c %6d %4d %s", k, p.Line+1, p.Col, strings.TrimSpace(b.LineString(p.Line))))
	}
	e.showLines(lines)
	return nil
}

func exMessages(e *Editor, a *exArgs) error {
	e.showOutput("*messages*", strings.Join(e.messages, "\n"))
	return nil
}

func exPwd(e *Editor, a *exArgs) error {
	e.infof("%s", e.cwd)
	return nil
}

func exCd(e *Editor, a *exArgs) error {
	dir := a.arg
	if dir == "" {
		dir = "~"
	}
	dir = expandHome(dir)
	if err := os.Chdir(dir); err != nil {
		return err
	}
	e.cwd, _ = os.Getwd()
	e.infof("%s", e.cwd)
	return nil
}

func exRead(e *Editor, a *exArgs) error {
	b := e.win.buf
	var text string
	if strings.HasPrefix(a.arg, "!") {
		if e.mappingDepth > 0 {
			return errMappingShell
		}
		out, err := runShell(strings.TrimPrefix(a.arg, "!"), "", b)
		if err != nil && out == "" {
			return err
		}
		text = out
	} else {
		if a.arg == "" {
			return errors.New("E32: No file name")
		}
		data, err := os.ReadFile(expandHome(a.arg))
		if err != nil {
			return err
		}
		text = string(data)
	}
	text = strings.TrimSuffix(text, "\n")
	if a.zero {
		b.Insert(Pos{0, 0}, text+"\n")
		e.win.cur = Pos{0, 0}
		return nil
	}
	line := a.to
	b.Insert(Pos{line, b.LineLen(line)}, "\n"+text)
	e.win.cur = Pos{line + 1, 0}
	return nil
}

// runShell runs cmd with sh -c, feeding input; % expands to the file name.
func runShell(cmd, input string, b *buffer.Buffer) (string, error) {
	if b != nil && b.Path != "" {
		cmd = strings.ReplaceAll(cmd, "%", shellQuote(b.Path))
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	c := exec.Command(sh, "-c", cmd)
	c.Stdin = strings.NewReader(input)
	var out bytes.Buffer
	c.Stdout = &out
	c.Stderr = &out
	err := c.Run()
	return out.String(), err
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// errMappingShell / errMappingProgram refuse running other programs from
// a config mapping: the config only drives the editor itself.
var (
	errMappingShell   = errors.New("shell commands cannot be run from a config mapping")
	errMappingProgram = errors.New("external programs cannot be started from a config mapping")
)

func exBang(e *Editor, a *exArgs) error {
	if e.mappingDepth > 0 {
		return errMappingShell
	}
	b := e.win.buf
	cmd := strings.TrimSpace(a.arg)
	if cmd == "" {
		return errors.New("E471: Argument required")
	}
	if !a.hasRange {
		out, err := runShell(cmd, "", b)
		e.showOutput("*shell*", strings.TrimSuffix(out, "\n"))
		if err != nil {
			e.warnf("shell returned: %v", err)
		}
		return nil
	}
	// filter lines through the command
	if b.ReadOnly {
		return errors.New("E21: Cannot make changes, buffer is read-only")
	}
	input := e.linesText(b, a.from, a.to) + "\n"
	out, err := runShell(cmd, input, b)
	if err != nil {
		return fmt.Errorf("filter failed: %v: %s", err, strings.TrimSpace(out))
	}
	if out == "" {
		e.win.cur = e.deleteRange(b, opRange{start: Pos{a.from, 0}, end: Pos{a.to, b.LineLen(a.to)}, linewise: true})
	} else {
		b.Replace(Pos{a.from, 0}, Pos{a.to, b.LineLen(a.to)}, strings.TrimSuffix(out, "\n"))
		e.win.cur = Pos{a.from, 0}
	}
	e.infof("%d lines filtered", a.to-a.from+1)
	return nil
}

func exTheme(e *Editor, a *exArgs) error {
	if a.arg == "" {
		e.pickTheme()
		return nil
	}
	if !slices.Contains(syntax.ThemeNames(), a.arg) {
		return fmt.Errorf("unknown theme: %s (see :theme)", a.arg)
	}
	e.setTheme(a.arg)
	return nil
}

func (e *Editor) setTheme(name string) {
	e.applyTheme(name, e.cfg.Transparent)
	e.saveSettings()
}

// applyTheme switches the colour theme without touching the config file.
func (e *Editor) applyTheme(name string, transparent bool) {
	e.theme = syntax.LoadTheme(name, transparent)
	e.cfg.Theme = e.theme.Name
	e.cfg.Transparent = transparent
	for _, h := range e.highlighters {
		h.SetTheme(e.theme)
	}
	for _, p := range e.previews {
		p.version = -1
	}
	// force a full repaint so no cell keeps the old colours
	if e.screen != nil {
		e.screen.Sync()
	}
}

func exLanguages(e *Editor, a *exArgs) error {
	names := syntax.LexerNames()
	sort.Strings(names)
	e.showOutput("*languages*", fmt.Sprintf("%d languages:\n%s", len(names), strings.Join(names, "\n")))
	return nil
}

func exFind(e *Editor, a *exArgs) error {
	if a.arg != "" {
		return exEdit(e, a)
	}
	e.pickFile()
	return nil
}

func exGrep(e *Editor, a *exArgs) error {
	e.pickGrep(a.arg)
	return nil
}

// ---------------------------------------------------------------- output

// showLines displays short output in the message area or a scratch buffer.
func (e *Editor) showLines(lines []string) {
	if len(lines) <= 1 {
		e.infof("%s", strings.Join(lines, ""))
		return
	}
	e.showOutput("*output*", strings.Join(lines, "\n"))
}

// showOutput opens text in a read-only scratch split.
func (e *Editor) showOutput(name, text string) {
	b := e.scratch(name, buffer.KindScratch)
	delete(e.static, b.ID) // colours of a previous output
	b.SetText(text)
	b.ReadOnly = true
	for _, w := range e.windows() {
		if w.buf == b {
			e.focus(w)
			w.cur = Pos{}
			w.top = 0
			return
		}
	}
	nw := e.split(b, false)
	nw.cur = Pos{}
	// keep output windows small
	lines := min(b.LineCount()+1, 15)
	e.layoutNow()
	if nw.h > lines+1 {
		e.resizeWindow(false, lines+1-nw.h)
	}
}

// layoutNow recomputes window rectangles immediately.
func (e *Editor) layoutNow() {
	if e.screen == nil {
		return
	}
	w, h := e.screen.Size()
	e.layout(0, 0, w, max(h-1, 1))
}
