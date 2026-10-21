package editor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"scihe/internal/buffer"
	"scihe/internal/config"
	"scihe/internal/notebook"
	"scihe/internal/repl"
)

// replEntry returns the REPL table entry serving b's filetype.
// Notebooks are not served by the table: they keep their own kernel.
func (e *Editor) replEntry(b *buffer.Buffer) (string, config.Repl, bool) {
	if b.Kind == buffer.KindNotebook {
		return "", config.Repl{}, false
	}
	for _, name := range sortedKeys(e.cfg.Repls) {
		r := e.cfg.Repls[name]
		for _, ft := range r.Filetypes {
			if ft == b.Filetype {
				return name, r, true
			}
		}
	}
	return "", config.Repl{}, false
}

func sortedKeys(m map[string]config.Repl) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// readerFor tells how code is cut from b: "python", "scheme",
// "clojure", or "" when no REPL serves it.
func (e *Editor) readerFor(b *buffer.Buffer) string {
	if b.Kind == buffer.KindNotebook {
		return "python"
	}
	if _, r, ok := e.replEntry(b); ok {
		return r.Reader
	}
	if d, ok := repl.DialectFor(b.Filetype); ok {
		return dialectReader(d)
	}
	return ""
}

func dialectReader(d repl.Dialect) string {
	if d == repl.Clojure {
		return "clojure"
	}
	return "scheme"
}

func readerDialect(reader string) repl.Dialect {
	if reader == "clojure" {
		return repl.Clojure
	}
	return repl.Scheme
}

// noReplError lists what is configured.
func (e *Editor) noReplError(b *buffer.Buffer) error {
	var fts []string
	for _, name := range sortedKeys(e.cfg.Repls) {
		fts = append(fts, e.cfg.Repls[name].Filetypes...)
	}
	return fmt.Errorf("no REPL for filetype %q (configured: %s; add a [repl.NAME] table)", b.Filetype, strings.Join(fts, ", "))
}

// projectDir returns the directory used to start REPL processes.
func (e *Editor) projectDir(b *buffer.Buffer, markers ...string) string {
	dir := e.cwd
	if b.Path != "" {
		dir = filepath.Dir(b.Path)
	}
	for d := dir; ; {
		for _, m := range markers {
			if _, err := os.Stat(filepath.Join(d, m)); err == nil {
				return d
			}
		}
		p := filepath.Dir(d)
		if p == d {
			break
		}
		d = p
	}
	return dir
}

// session returns the running session of a REPL entry, starting one
// if possible.
func (e *Editor) session(b *buffer.Buffer, name string, entry config.Repl) (repl.Session, error) {
	if s := e.sessions[name]; s != nil && s.Alive() {
		return s, nil
	}
	onClose := func(err error) {
		e.Post(func() {
			if s := e.sessions[name]; s != nil && !s.Alive() {
				delete(e.sessions, name)
				e.warnf("%s REPL ended", name)
			}
		})
	}
	var s repl.Session
	var err error
	switch entry.Kind {
	case "chez":
		bin := ""
		if f := splitArgs(entry.Command); len(f) > 0 {
			bin = f[0]
		}
		s, err = repl.StartChez(bin, e.projectDir(b), onClose)
	case "nrepl":
		addr, ok := repl.FindNREPLPort(e.projectDir(b, "deps.edn", "project.clj", "bb.edn", "shadow-cljs.edn"))
		if !ok {
			return nil, errors.New("no .nrepl-port found: start nREPL (:repl jack-in) or :repl connect PORT")
		}
		s, err = repl.DialNREPL(addr, onClose)
	case "kernel":
		py, perr := e.pythonInterpreter()
		if perr != nil {
			return nil, perr
		}
		onImage := func(ev notebook.Event) { e.Post(func() { e.saveScriptImage(ev) }) }
		s, err = repl.StartKernel(py, e.projectDir(b), os.Environ(), onImage, onClose)
	default: // subprocess
		var prompt *regexp.Regexp
		if entry.PromptRegex != "" {
			prompt = regexp.MustCompile(entry.PromptRegex) // validated by config.Load
		}
		s, err = repl.StartSubprocess(name, splitArgs(entry.Command), e.projectDir(b), entry.Sentinel, prompt, onClose)
	}
	if err != nil {
		return nil, err
	}
	e.sessions[name] = s
	e.infof("started %s", s.Name())
	return s, nil
}

// replLog is the log buffer of a REPL entry.
func (e *Editor) replLog(name string) *buffer.Buffer {
	b := e.scratch("*repl:"+name+"*", buffer.KindRepl)
	if b.Filetype == "" {
		b.Filetype = name
		if r, ok := e.cfg.Repls[name]; ok && len(r.Filetypes) > 0 {
			b.Filetype = r.Filetypes[0]
		}
		b.ReadOnly = true
	}
	return b
}

func (e *Editor) pythonLog() *buffer.Buffer {
	b := e.scratch("*python*", buffer.KindRepl)
	if b.Filetype == "" {
		b.Filetype = "python"
		b.ReadOnly = true
	}
	return b
}

// showLog makes sure a log buffer is visible without stealing focus.
func (e *Editor) showLog(b *buffer.Buffer) {
	for _, w := range e.windows() {
		if w.buf == b {
			return
		}
	}
	cur := e.win
	nw := e.splitAt(b, false, true)
	nw.cur = Pos{b.LineCount() - 1, 0}
	e.layoutNow()
	if nw.h > 11 {
		e.resizeWindow(false, 11-nw.h)
	}
	e.prevWin = nw
	e.win = cur
}

// flashRange briefly highlights the evaluated region.
func (e *Editor) flashRange(b *buffer.Buffer, start, end Pos) {
	if start == end {
		return
	}
	e.flash = &flashRegion{buf: b, start: start, end: end, until: time.Now().Add(250 * time.Millisecond)}
}

func (e *Editor) annotate(b *buffer.Buffer, line int, text string, isErr bool) {
	if line < 0 || line >= b.LineCount() {
		return
	}
	text = strings.TrimSpace(text)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		n := strings.Count(text, "\n")
		text = text[:i] + fmt.Sprintf(" … (+%d lines)", n)
	}
	if utf8.RuneCountInString(text) > 160 {
		text = string([]rune(text)[:160]) + "…"
	}
	prefix := "=> "
	if isErr {
		prefix = "!! "
	}
	if e.annots[b.ID] == nil {
		e.annots[b.ID] = map[int]annotation{}
	}
	e.annots[b.ID][line] = annotation{text: prefix + text, err: isErr}
}

// evalText sends code from buffer b to its REPL. line is where the
// inline result is shown (-1: only log).
func (e *Editor) evalText(b *buffer.Buffer, code string, line int, start, end Pos) {
	if strings.TrimSpace(code) == "" {
		return
	}
	e.flashRange(b, start, end)
	if b.Kind == buffer.KindNotebook { // the notebook's own kernel
		ks := e.kernelFor(b)
		log := e.pythonLog()
		e.appendText(log, logBlock(">>> ", code))
		e.showLog(log)
		e.enqueue(ks, &nbRequest{code: dedent(code), line: line})
		return
	}
	name, entry, ok := e.replEntry(b)
	if !ok {
		e.errorf("%v", e.noReplError(b))
		return
	}
	s, err := e.session(b, name, entry)
	if err != nil {
		e.errorf("%v", err)
		return
	}
	log := e.replLog(name)
	ns := ""
	if entry.Reader == "clojure" {
		ns = repl.ClojureNS(b.Text())
	}
	prompt := name + "> "
	switch {
	case ns != "":
		prompt = ns + "=> "
	case entry.Reader == "python":
		prompt, code = ">>> ", dedent(code)
	}
	e.appendText(log, logBlock(prompt, code))
	e.showLog(log)
	var values []string
	var errText, out strings.Builder
	err = s.Eval(code, ns, func(r repl.Result) {
		e.Post(func() {
			if r.Out != "" {
				out.WriteString(r.Out)
				e.appendText(log, r.Out)
			}
			if r.Err != "" {
				errText.WriteString(r.Err)
				e.appendText(log, r.Err)
			}
			if r.Value != "" {
				values = append(values, r.Value)
				e.appendText(log, r.Value+"\n")
			}
			if r.Done && e.bufferAlive(b) {
				e.annotateResult(b, line, errText.String(), values, out.String())
			}
		})
	})
	if err != nil {
		e.errorf("%v", err)
	}
}

// annotateResult shows the outcome of an evaluation after line: the
// error, else the last value, else the last line of output, else ✓.
func (e *Editor) annotateResult(b *buffer.Buffer, line int, errText string, values []string, out string) {
	switch {
	case strings.TrimSpace(errText) != "":
		e.annotate(b, line, errText, true)
	case len(values) > 0:
		e.annotate(b, line, values[len(values)-1], false)
	case strings.TrimSpace(out) != "":
		lines := strings.Split(strings.TrimSpace(out), "\n")
		e.annotate(b, line, lines[len(lines)-1], false)
	default:
		e.annotate(b, line, "✓", false)
	}
}

func logBlock(prompt, code string) string {
	lines := strings.Split(strings.TrimRight(code, "\n"), "\n")
	var sb strings.Builder
	for i, l := range lines {
		if i == 0 {
			sb.WriteString(prompt)
		} else {
			sb.WriteString(strings.Repeat(" ", utf8.RuneCountInString(prompt)-4) + "... ")
		}
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	return sb.String()
}

// dedent removes common leading indentation (evaluating an indented block).
func dedent(code string) string {
	lines := strings.Split(code, "\n")
	minInd := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		ind := len(l) - len(strings.TrimLeft(l, " \t"))
		if minInd < 0 || ind < minInd {
			minInd = ind
		}
	}
	if minInd <= 0 {
		return code
	}
	for i, l := range lines {
		if len(l) >= minInd {
			lines[i] = l[minInd:]
		} else {
			lines[i] = strings.TrimLeft(l, " \t")
		}
	}
	return strings.Join(lines, "\n")
}

// finishScriptEval ends a line / block evaluation in a notebook kernel.
func (e *Editor) finishScriptEval(ks *kernelState, req *nbRequest) {
	log := e.pythonLog()
	if out := req.out.String(); out != "" {
		e.appendText(log, out)
		if !strings.HasSuffix(out, "\n") {
			e.appendText(log, "\n")
		}
	}
	if req.result != "" && !req.err {
		e.appendText(log, req.result+"\n")
	}
	if !e.bufferAlive(ks.buf) {
		return
	}
	var errText string
	var values []string
	if req.err {
		errText = req.result
	} else if req.result != "" {
		values = []string{req.result}
	}
	e.annotateResult(ks.buf, req.line, errText, values, req.out.String())
}

// evalCurrent evaluates the form/line (top=false) or top-level form/cell.
func (e *Editor) evalCurrent(top bool) {
	w := e.win
	b := w.buf
	if ks := e.kernels[b.ID]; ks != nil && ks.nb != nil && top {
		e.execEx("nbrun")
		return
	}
	reader := e.readerFor(b)
	if reader == "python" {
		var s, en int
		if top {
			s, en = pythonTopBlock(b, w.cur.Line)
		} else {
			s, en = pythonLineBlock(b, w.cur.Line)
		}
		code := e.linesText(b, s, en)
		e.evalText(b, code, en, Pos{s, 0}, Pos{en, b.LineLen(en)})
		return
	}
	if reader == "" {
		e.errorf("%v", e.noReplError(b))
		return
	}
	d := readerDialect(reader)
	text := []rune(b.Text())
	off := b.Offset(w.cur)
	var f repl.Form
	var ok bool
	if top {
		f, ok = repl.TopLevelAt(text, off, d)
	} else {
		f, ok = repl.FormAt(text, off, d)
	}
	if !ok {
		e.errorf("no form at cursor")
		return
	}
	start, end := b.PosAt(f.Start), b.PosAt(f.End)
	e.evalText(b, string(text[f.Start:f.End]), end.Line, start, end)
}

// pythonLineBlock returns the line, or the whole block when the line opens one.
func pythonLineBlock(b *buffer.Buffer, l int) (int, int) {
	line := strings.TrimRight(b.LineString(l), " \t")
	if !strings.HasSuffix(line, ":") && !strings.HasSuffix(line, "\\") && !openBrackets(line) {
		return l, l
	}
	ind := len(leadingWS(b.LineString(l)))
	end := l
	for i := l + 1; i < b.LineCount(); i++ {
		s := b.LineString(i)
		if strings.TrimSpace(s) == "" {
			continue
		}
		if len(leadingWS(s)) <= ind && !strings.HasPrefix(strings.TrimSpace(s), ")") && !strings.HasPrefix(strings.TrimSpace(s), "]") && !strings.HasPrefix(strings.TrimSpace(s), "}") {
			if !isContinuationKeyword(strings.TrimSpace(s)) {
				break
			}
		}
		end = i
	}
	return l, end
}

func openBrackets(s string) bool {
	depth := 0
	for _, r := range s {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		}
	}
	return depth > 0
}

func isContinuationKeyword(s string) bool {
	for _, k := range []string{"elif", "else", "except", "finally", "case"} {
		if strings.HasPrefix(s, k) {
			return true
		}
	}
	return false
}

// pythonTopBlock returns the "# %%" cell around l, or the top-level
// statement (with decorators and continuation lines) containing l.
func pythonTopBlock(b *buffer.Buffer, l int) (int, int) {
	hasCells := false
	for i := 0; i < b.LineCount(); i++ {
		if cellMarker(b, i) {
			hasCells = true
			break
		}
	}
	if hasCells {
		s := l
		for s > 0 && !cellMarker(b, s) {
			s--
		}
		if cellMarker(b, s) {
			s++
		}
		en := l
		for en+1 < b.LineCount() && !cellMarker(b, en+1) {
			en++
		}
		return s, max(en, s)
	}
	isTop := func(i int) bool {
		s := b.LineString(i)
		t := strings.TrimSpace(s)
		return t != "" && len(leadingWS(s)) == 0 && !isContinuationKeyword(t) && !strings.HasPrefix(t, ")") && !strings.HasPrefix(t, "]") && !strings.HasPrefix(t, "}") && !strings.HasPrefix(t, "#")
	}
	s := l
	for s > 0 && !isTop(s) {
		s--
	}
	for s > 0 && strings.HasPrefix(strings.TrimSpace(b.LineString(s-1)), "@") {
		s--
	}
	en := l
	for i := max(l, s) + 1; i < b.LineCount(); i++ {
		if isTop(i) && !strings.HasPrefix(strings.TrimSpace(b.LineString(i)), "@") {
			break
		}
		if strings.TrimSpace(b.LineString(i)) != "" {
			en = i
		}
	}
	return s, max(en, l)
}

func (e *Editor) evalBuffer() {
	b := e.win.buf
	if ks := e.kernels[b.ID]; ks != nil && ks.nb != nil {
		e.execEx("nbrunall")
		return
	}
	last := b.LineCount() - 1
	e.evalText(b, b.Text(), last, Pos{}, b.End())
}

func (e *Editor) evalSelection(c *ctx) {
	r := e.visualRange()
	e.exitVisual()
	text := e.rangeText(c.b, r).Text
	end := r.end
	if !r.linewise && end.Col == 0 && end.Line > r.start.Line {
		end.Line--
	}
	e.evalText(c.b, text, end.Line, r.start, r.end)
}

func exEval(e *Editor, a *exArgs) error {
	b := e.win.buf
	if a.arg != "" {
		e.evalText(b, a.arg, e.win.cur.Line, Pos{}, Pos{})
		return nil
	}
	from, to := a.from, a.to
	if !a.hasRange {
		from, to = 0, b.LineCount()-1
	}
	e.evalText(b, e.linesText(b, from, to), to, Pos{from, 0}, Pos{to, b.LineLen(to)})
	return nil
}

// exRepl: ":repl start [NAME] | restart | connect [addr] | jack-in |
// quit | log | interrupt". NAME picks a [repl.NAME] entry; by default
// the one serving the buffer's filetype. Notebooks use their kernel.
func exRepl(e *Editor, a *exArgs) error {
	b := e.win.buf
	sub, arg, _ := strings.Cut(strings.TrimSpace(a.arg), " ")
	arg = strings.TrimSpace(arg)
	if b.Kind == buffer.KindNotebook {
		ks := e.kernelFor(b)
		switch sub {
		case "", "start":
			return e.ensureKernel(ks)
		case "restart":
			return exNbRestart(e, a)
		case "quit", "stop":
			ks.close()
			return nil
		case "log":
			e.showLog(e.pythonLog())
			return nil
		case "interrupt":
			return exNbInterrupt(e, a)
		}
	}
	name, entry, ok := e.replEntry(b)
	if arg != "" && (sub == "" || sub == "start" || sub == "restart") {
		if arg == "chez" {
			arg = "scheme"
		}
		entry, ok = e.cfg.Repls[arg]
		name = arg
		if !ok {
			return fmt.Errorf("no [repl.%s] entry (configured: %s)", arg, strings.Join(sortedKeys(e.cfg.Repls), ", "))
		}
	}
	switch sub {
	case "", "start":
		if !ok {
			return e.noReplError(b)
		}
		_, err := e.session(b, name, entry)
		if err == nil {
			e.showLog(e.replLog(name))
		}
		return err
	case "restart":
		if !ok {
			return e.noReplError(b)
		}
		if s := e.sessions[name]; s != nil {
			s.Close()
			delete(e.sessions, name)
		}
		_, err := e.session(b, name, entry)
		return err
	case "connect":
		addr := arg
		if addr == "" {
			var found bool
			addr, found = repl.FindNREPLPort(e.projectDir(b, "deps.edn", "project.clj", "bb.edn"))
			if !found {
				return errors.New("usage: :repl connect [host:]port")
			}
		}
		key := e.nreplEntryName(b)
		s, err := e.connectNREPL(key, addr)
		if err != nil {
			return err
		}
		e.infof("connected to %s", s.Name())
		return nil
	case "jack-in":
		return e.jackIn(b)
	case "quit", "stop":
		if !ok {
			return errors.New("no REPL for this buffer")
		}
		if s := e.sessions[name]; s != nil {
			s.Close()
			delete(e.sessions, name)
		}
		return nil
	case "log":
		if !ok {
			return errors.New("no REPL for this buffer")
		}
		e.showLog(e.replLog(name))
		return nil
	case "interrupt":
		if s := e.sessions[name]; ok && s != nil {
			s.Interrupt()
		}
		return nil
	}
	return fmt.Errorf("unknown :repl subcommand %q", sub)
}

// connectNREPL replaces the session key with a connection to addr; a
// connection that drops later is removed (without a warning when it was
// closed on purpose).
func (e *Editor) connectNREPL(key, addr string) (repl.Session, error) {
	if old := e.sessions[key]; old != nil {
		delete(e.sessions, key)
		old.Close()
	}
	var s repl.Session
	s, err := repl.DialNREPL(addr, func(error) {
		e.Post(func() {
			if e.sessions[key] == s {
				delete(e.sessions, key)
				e.warnf("nREPL disconnected")
			}
		})
	})
	if err != nil {
		return nil, err
	}
	e.sessions[key] = s
	return s, nil
}

// nreplEntryName is the entry an nREPL connection belongs to: the
// buffer's when it is an nREPL entry, else "clojure".
func (e *Editor) nreplEntryName(b *buffer.Buffer) string {
	if name, r, ok := e.replEntry(b); ok && r.Kind == "nrepl" {
		return name
	}
	return "clojure"
}

// jackIn starts an nREPL server for a Clojure project and connects to it.
func (e *Editor) jackIn(b *buffer.Buffer) error {
	if e.jackInProc != nil {
		return errors.New("an nREPL server started by scihe is already running")
	}
	dir := e.projectDir(b, "deps.edn", "project.clj", "bb.edn")
	key := e.nreplEntryName(b)
	cmdline := e.cfg.ClojureJackIn
	if _, err := os.Stat(filepath.Join(dir, "project.clj")); err == nil {
		cmdline = "lein repl :headless"
	}
	// run the command directly, never through a shell: the config can
	// name a Clojure tool and its arguments, not a shell script
	fields := splitArgs(cmdline)
	if len(fields) == 0 {
		return errors.New("clojure_jack_in is empty")
	}
	bin := fields[0]
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("%s not found on PATH (configure clojure_jack_in)", bin)
	}
	os.Remove(filepath.Join(dir, ".nrepl-port"))
	cmd := exec.Command(bin, fields[1:]...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	log := e.scratch("*nrepl-server*", buffer.KindRepl)
	log.ReadOnly = true
	w := &logWriter{e: e, b: log}
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Start(); err != nil {
		return err
	}
	e.jackInProc = cmd.Process
	e.infof("starting nREPL in %s …", dir)
	e.safeGo(func() {
		deadline := time.Now().Add(3 * time.Minute)
		for time.Now().Before(deadline) {
			// only this project's port file: a stale one in a parent
			// directory belongs to another (or a dead) server
			if data, err := os.ReadFile(filepath.Join(dir, ".nrepl-port")); err == nil {
				if port := strings.TrimSpace(string(data)); port != "" {
					addr := "127.0.0.1:" + port
					time.Sleep(300 * time.Millisecond)
					e.Post(func() {
						s, err := e.connectNREPL(key, addr)
						if err != nil {
							e.errorf("nREPL connect: %v", err)
							return
						}
						e.infof("connected to %s", s.Name())
					})
					return
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
		e.Post(func() { e.errorf("nREPL did not start (see *nrepl-server*)") })
	})
	e.safeGo(func() {
		cmd.Wait()
		rest := w.flush()
		e.Post(func() {
			if rest != "" {
				e.appendText(log, rest)
			}
			e.jackInProc = nil
		})
	})
	return nil
}

// logWriter appends process output to a buffer on the main loop.
type logWriter struct {
	e    *Editor
	b    *buffer.Buffer
	mu   sync.Mutex
	tail []byte // incomplete UTF-8 sequence kept for the next write
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	data := append(w.tail, p...)
	cut := len(data)
	// hold back a character cut in two by the pipe
	for i := len(data) - 1; i >= 0 && i >= len(data)-utf8.UTFMax; i-- {
		if utf8.RuneStart(data[i]) {
			if !utf8.FullRune(data[i:]) {
				cut = i
			}
			break
		}
	}
	s := string(data[:cut])
	w.tail = append([]byte(nil), data[cut:]...)
	w.mu.Unlock()
	if s != "" {
		w.e.Post(func() { w.e.appendText(w.b, s) })
	}
	return len(p), nil
}

// flush returns what is still held back (the process has exited; its
// output ended in an incomplete or invalid UTF-8 sequence).
func (w *logWriter) flush() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := string(w.tail)
	w.tail = nil
	return s
}
