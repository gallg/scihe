// Package editor ties buffers, windows, vim keys, rendering and the
// data-science features (notebooks, REPLs, markdown preview) together.
package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"

	"scihe/internal/buffer"
	"scihe/internal/config"
	"scihe/internal/keymap"
	"scihe/internal/repl"
	"scihe/internal/syntax"
	"scihe/internal/table"
)

// Mode is the current input mode.
type Mode int

const (
	ModeNormal Mode = iota
	ModeInsert
	ModeReplace
	ModeVisual
	ModeVisualLine
	ModeVisualBlock
	ModeCmdline
)

func (m Mode) String() string {
	switch m {
	case ModeInsert:
		return "INSERT"
	case ModeReplace:
		return "REPLACE"
	case ModeVisual:
		return "VISUAL"
	case ModeVisualLine:
		return "V-LINE"
	case ModeVisualBlock:
		return "V-BLOCK"
	case ModeCmdline:
		return "COMMAND"
	}
	return "NORMAL"
}

func (m Mode) visual() bool {
	return m == ModeVisual || m == ModeVisualLine || m == ModeVisualBlock
}

// Editor is the whole application state.
type Editor struct {
	screen tcell.Screen
	cfg    *config.Config
	theme  *syntax.Theme
	leader keymap.Key

	buffers []*buffer.Buffer
	root    *node
	win     *Window
	prevWin *Window
	mode    Mode

	// key handling
	pending      []keymap.Key
	pendAmb      bool
	pendingOp    *pendingOperator
	pendingSince time.Time
	normalMap    *keymap.Map[*Cmd]
	visualMap    *keymap.Map[*Cmd]
	opMap        *keymap.Map[*Cmd]
	insertMap    *keymap.Map[*Cmd]
	localMaps    map[int]*keymap.Map[*Cmd] // per-buffer normal-mode keys
	localVisual  map[int]*keymap.Map[*Cmd] // per-buffer visual-mode keys
	readOnlyMap  *keymap.Map[*Cmd]         // q closes read-only special buffers
	whichKey     bool
	insertPend   []keymap.Key
	replaying    int
	abortReplay  bool // an error/failed motion stops the running replay
	lastKeyTime  time.Time
	opts         map[int]*config.Options
	highlighters map[int]*syntax.Highlighter
	static       map[int][][]syntax.Span
	decor        map[int]Decorator
	annots       map[int]map[int]annotation
	modTimes     map[int]time.Time
	disk         map[int]diskState // changes on disk already reported
	// lastDiskCheck is when checkDisk last looked at the files
	lastDiskCheck time.Time

	regs      *Registers
	search    searchState
	lastFind  findState
	lastSub   *substitution
	cmdline   *cmdline
	cmdHist   map[byte][]string
	picker    *picker
	messages  []string
	msg       string
	msgStyle  int // 0 info, 1 error, 2 warning
	dot       dotState
	macro     macroState
	insert    insertState
	visual    visualState
	lastExCmd string
	rangeZero bool // set by parseRange: the last address was line 0
	flash     *flashRegion

	kernels    map[int]*kernelState
	sessions   map[string]repl.Session // by [repl.NAME] entry
	jackInProc *os.Process
	jobs       map[*os.Process]string // :run / :git jobs -> output buffer name
	jobsMu     sync.Mutex
	tempFiles  []string // image files for :nbimage, removed on exit
	exitNotes  []string // printed by main after the terminal is restored
	previews   map[int]*preview
	gitViews   map[int]*gitView // status / log buffers
	gitCommits map[int]string   // commit message buffer -> repository root
	heads      gitHeads
	tables     map[int]*tableView  // table view buffer -> view
	explorer   map[int]string      // explorer buffer -> directory
	direds     map[int]*diredState // explorer buffer -> listing and marks
	fileClip   fileClipboard       // files copied (y) or cut (X) in an explorer
	buflists   map[int]*bufList    // *buffers* listing -> state
	// lastFigure is the most recent figure from evaluating a .py buffer
	lastFigure string

	pasting  bool
	pasteBuf strings.Builder

	// async work posted from other goroutines: an unbounded FIFO queue
	// plus a wake-up channel for the main loop
	asyncMu sync.Mutex
	asyncQ  []func()
	wake    chan struct{}
	quit    bool
	// mappingDepth > 0 while a config-defined mapping runs: shell
	// commands are refused so the config cannot launch other programs
	mappingDepth int
	// resize mode (tmux-like): direction keys keep resizing until then
	resizeUntil time.Time
	layoutName  string // last preset applied by :layout
	started     time.Time
	cwd         string
}

type annotation struct {
	text string
	err  bool
}

type flashRegion struct {
	buf        *buffer.Buffer
	start, end buffer.Pos
	until      time.Time
}

// New creates an editor drawing to screen.
func New(screen tcell.Screen, cfg *config.Config) *Editor {
	cwd, _ := os.Getwd()
	e := &Editor{
		screen:       screen,
		cfg:          cfg,
		theme:        syntax.LoadTheme(cfg.Theme, cfg.Transparent),
		leader:       keymap.Parse(cfg.Leader, "<Space>")[0],
		localMaps:    map[int]*keymap.Map[*Cmd]{},
		localVisual:  map[int]*keymap.Map[*Cmd]{},
		opts:         map[int]*config.Options{},
		highlighters: map[int]*syntax.Highlighter{},
		static:       map[int][][]syntax.Span{},
		decor:        map[int]Decorator{},
		annots:       map[int]map[int]annotation{},
		modTimes:     map[int]time.Time{},
		disk:         map[int]diskState{},
		regs:         newRegisters(),
		cmdHist:      map[byte][]string{},
		kernels:      map[int]*kernelState{},
		sessions:     map[string]repl.Session{},
		previews:     map[int]*preview{},
		gitViews:     map[int]*gitView{},
		gitCommits:   map[int]string{},
		tables:       map[int]*tableView{},
		explorer:     map[int]string{},
		direds:       map[int]*diredState{},
		buflists:     map[int]*bufList{},
		jobs:         map[*os.Process]string{},
		wake:         make(chan struct{}, 1),
		started:      time.Now(),
		cwd:          cwd,
	}
	e.search.forward = true
	e.regs.noDeleteReg = !cfg.DeleteToRegister
	e.setupKeymaps()
	e.applyUserKeys()
	for _, w := range cfg.Warnings {
		e.warnf("config: %s", w)
	}
	return e
}

// Post schedules fn to run on the main loop (safe from any goroutine).
func (e *Editor) Post(fn func()) {
	e.asyncMu.Lock()
	e.asyncQ = append(e.asyncQ, fn)
	e.asyncMu.Unlock()
	select {
	case e.wake <- struct{}{}:
	default:
	}
}

// Open opens files given on the command line.
func (e *Editor) Open(paths []string) {
	if len(paths) == 0 {
		b := buffer.New("[No Name]")
		e.addBuffer(b)
		e.root = &node{win: e.newWindow(b)}
		e.win = e.root.win
		return
	}
	var first *buffer.Buffer
	for _, p := range paths {
		b, err := e.loadBuffer(p)
		if err != nil {
			e.errorf("%v", err)
			continue
		}
		if first == nil {
			first = b
		}
	}
	if first == nil {
		first = buffer.New("[No Name]")
		e.addBuffer(first)
	}
	e.root = &node{win: e.newWindow(first)}
	e.win = e.root.win
	e.afterEnter(e.win)
}

// Run is the main event loop. It returns when the editor quits.
func (e *Editor) Run() {
	events := make(chan tcell.Event, 64)
	quit := make(chan struct{})
	go e.screen.ChannelEvents(events, quit)
	defer close(quit)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	e.draw()
	for !e.quit {
		select {
		case ev := <-events:
			e.handleEvent(ev)
			// drain queued events before redrawing (fast typing, paste)
		drain:
			for i := 0; i < 256 && !e.quit; i++ {
				select {
				case ev := <-events:
					e.handleEvent(ev)
				default:
					break drain
				}
			}
		case <-e.wake:
		case <-ticker.C:
			if !e.tick() {
				continue
			}
		}
		e.drainAsync()
		if !e.quit {
			e.draw()
		}
	}
	e.shutdown()
}

// drainAsync runs all posted work in FIFO order on the main loop.
func (e *Editor) drainAsync() {
	for {
		e.asyncMu.Lock()
		q := e.asyncQ
		e.asyncQ = nil
		e.asyncMu.Unlock()
		if len(q) == 0 {
			return
		}
		for _, fn := range q {
			fn()
		}
	}
}

// safeGo runs fn in a goroutine; a panic is reported instead of killing
// the editor with the terminal left in raw mode.
func (e *Editor) safeGo(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				e.Post(func() { e.errorf("internal error in background task: %v", r) })
			}
		}()
		fn()
	}()
}

// tick handles timers; it returns true if a redraw is needed.
func (e *Editor) tick() bool {
	redraw := false
	if len(e.pending) > 0 && !e.whichKey && time.Since(e.pendingSince) >= time.Duration(e.cfg.WhichKeyDelay)*time.Millisecond {
		e.whichKey = true
		redraw = true
	}
	if e.pendAmb && time.Since(e.lastKeyTime) >= time.Second {
		e.flushAmbiguous()
		redraw = true
	}
	if len(e.insertPend) > 0 && time.Since(e.lastKeyTime) > time.Second {
		e.flushInsertPending()
		redraw = true
	}
	if e.checkDisk() {
		redraw = true
	}
	if !e.resizeUntil.IsZero() && time.Now().After(e.resizeUntil) {
		e.endResize()
		redraw = true
	}
	if e.flash != nil && time.Now().After(e.flash.until) {
		e.flash = nil
		redraw = true
	}
	for _, k := range e.kernels {
		if k.busy() {
			redraw = true // spinner
		}
	}
	return redraw
}

func (e *Editor) handleEvent(ev tcell.Event) {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		e.lastKeyTime = time.Now()
		k := keymap.FromEvent(ev)
		if e.pasting {
			e.pasteBuf.WriteString(keyText(k))
			return
		}
		e.handleKey(k)
	case *tcell.EventPaste:
		// bracketed paste: the content arrives as key events between a
		// start and an end marker; collect it and insert it verbatim.
		if ev.Start() {
			e.pasting = true
			e.pasteBuf.Reset()
		} else if e.pasting {
			e.pasting = false
			e.pasteText(e.pasteBuf.String())
		}
	case *tcell.EventResize:
		e.screen.Sync()
	case *tcell.EventMouse:
		e.handleMouse(ev)
	case *tcell.EventInterrupt:
	}
}

func (e *Editor) shutdown() {
	e.stopChildren()
	for _, f := range e.tempFiles {
		os.Remove(f)
	}
	e.tempFiles = nil
}

// stopChildren ends every process the editor started: kernels, REPLs,
// the nREPL server and :run / :git background jobs.
func (e *Editor) stopChildren() {
	for _, k := range e.kernels {
		k.close()
	}
	for _, s := range e.sessions {
		s.Close()
	}
	e.sessions = map[string]repl.Session{} // closed once
	if e.jackInProc != nil {
		syscall.Kill(-e.jackInProc.Pid, syscall.SIGTERM)
		e.jackInProc = nil
	}
	e.jobsMu.Lock()
	for p := range e.jobs {
		syscall.Kill(-p.Pid, syscall.SIGTERM)
	}
	e.jobsMu.Unlock()
}

// Terminate saves recovery files and quits (the terminal went away:
// SIGHUP, or SIGTERM). The notices are kept for ExitNotes: the screen
// still belongs to the editor.
func (e *Editor) Terminate() {
	e.exitNotes = e.saveRecovery()
	e.stopChildren()
	e.quit = true
}

// ExitNotes are messages to print once the terminal is restored.
func (e *Editor) ExitNotes() []string { return e.exitNotes }

// ---------------------------------------------------------------- buffers

func (e *Editor) addBuffer(b *buffer.Buffer) {
	e.buffers = append(e.buffers, b)
	b.AddListener(func(line, removed, added int) { e.onChange(b, line, removed, added) })
}

func (e *Editor) onChange(b *buffer.Buffer, line, removed, added int) {
	if h := e.highlighters[b.ID]; h != nil {
		h.Shift(line, removed, added)
	}
	if an := e.annots[b.ID]; len(an) > 0 {
		delta := added - removed
		moved := map[int]annotation{}
		for l, a := range an {
			switch {
			case l < line:
				moved[l] = a
			case l >= line+removed:
				moved[l+delta] = a
			}
		}
		e.annots[b.ID] = moved
	}
	if removed != added {
		delta := added - removed
		e.forWindows(func(w *Window) {
			if w.buf != b || w == e.win {
				return
			}
			if w.cur.Line >= line+removed {
				w.cur.Line += delta
			} else if w.cur.Line >= line+added {
				w.cur.Line = line + added - 1
			}
			w.cur = b.Clamp(w.cur)
		})
	}
	if k := e.kernels[b.ID]; k != nil && k.nb != nil {
		k.rangesVersion = -1
	}
}

// findBuffer returns the buffer of path: the same name, or the same file
// reached another way (a symlink, /home vs /var/home, a hard link).
func (e *Editor) findBuffer(path string) *buffer.Buffer {
	abs, _ := filepath.Abs(path)
	for _, b := range e.buffers {
		if b.Path == abs {
			return b
		}
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil
	}
	for _, b := range e.buffers {
		if b.Path == "" {
			continue
		}
		if bs, err := os.Stat(b.Path); err == nil && os.SameFile(st, bs) {
			return b
		}
	}
	return nil
}

// loadBuffer opens (or returns the already open) buffer for path.
func (e *Editor) loadBuffer(path string) (*buffer.Buffer, error) {
	path = expandHome(path)
	if b := e.findBuffer(path); b != nil {
		return b, nil
	}
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return e.openExplorer(path)
	}
	if strings.EqualFold(filepath.Ext(path), ".ipynb") {
		return e.openNotebook(path)
	}
	b, err := buffer.Load(path)
	if err != nil {
		if errors.Is(err, buffer.ErrBinary) {
			return nil, fmt.Errorf("%s: not a UTF-8 text file (refusing to open)", path)
		}
		return nil, err
	}
	first := ""
	if b.LineCount() > 0 {
		first = b.LineString(0)
	}
	b.Filetype = syntax.Detect(b.Path, first)
	e.addBuffer(b)
	e.recordModTime(b)
	if table.IsDataFile(b.Path) {
		e.infof("%q: <leader>dt (:table) shows it as a table", b.Name)
	}
	return b, nil
}

func (e *Editor) recordModTime(b *buffer.Buffer) {
	if b.Path == "" {
		return
	}
	if st, err := os.Stat(b.Path); err == nil {
		e.modTimes[b.ID] = st.ModTime()
	}
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			full := filepath.Join(h, p[1:])
			if strings.HasSuffix(p, "/") && !strings.HasSuffix(full, "/") {
				full += "/" // keep "~/" a directory prefix for completion
			}
			return full
		}
	}
	return p
}

// scratch returns (creating if needed) a named special buffer.
func (e *Editor) scratch(name string, kind buffer.Kind) *buffer.Buffer {
	for _, b := range e.buffers {
		if b.Name == name && b.Path == "" {
			return b
		}
	}
	b := buffer.New(name)
	b.Kind = kind
	e.addBuffer(b)
	return b
}

// appendText appends text to the end of a special buffer.
func (e *Editor) appendText(b *buffer.Buffer, text string) {
	ro := b.ReadOnly
	b.ReadOnly = false
	b.Insert(b.End(), text)
	b.ClearUndo() // logs only grow: no history to keep
	b.MarkSaved()
	b.ReadOnly = ro
	// keep windows showing the log scrolled to the bottom
	e.forWindows(func(w *Window) {
		if w.buf == b && w != e.win {
			w.cur = buffer.Pos{Line: b.LineCount() - 1}
		}
	})
}

func (e *Editor) removeBuffer(b *buffer.Buffer) {
	for i, x := range e.buffers {
		if x == b {
			e.buffers = append(e.buffers[:i], e.buffers[i+1:]...)
			break
		}
	}
	if k := e.kernels[b.ID]; k != nil {
		k.close()
		delete(e.kernels, b.ID)
	}
	preview := e.previews[b.ID]
	delete(e.previews, b.ID)
	for src, p := range e.previews {
		if p.buf == b {
			delete(e.previews, src)
		}
	}
	delete(e.highlighters, b.ID)
	delete(e.static, b.ID)
	delete(e.decor, b.ID)
	delete(e.annots, b.ID)
	delete(e.opts, b.ID)
	delete(e.localMaps, b.ID)
	delete(e.localVisual, b.ID)
	delete(e.explorer, b.ID)
	delete(e.direds, b.ID)
	delete(e.buflists, b.ID)
	delete(e.modTimes, b.ID)
	delete(e.disk, b.ID)
	delete(e.gitViews, b.ID)
	delete(e.gitCommits, b.ID)
	delete(e.tables, b.ID)
	// the preview of a deleted markdown buffer goes with it
	if preview != nil && e.bufferAlive(preview.buf) {
		e.deleteBuffer(preview.buf, true)
	}
}

// sortedBuffers returns listed buffers ordered by id.
func (e *Editor) listedBuffers() []*buffer.Buffer {
	out := append([]*buffer.Buffer(nil), e.buffers...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// options returns the per-buffer options, creating them from config.
func (e *Editor) options(b *buffer.Buffer) *config.Options {
	if o, ok := e.opts[b.ID]; ok {
		return o
	}
	o := e.cfg.ForFiletype(b.Filetype)
	e.opts[b.ID] = &o
	return &o
}

func (e *Editor) highlighter(b *buffer.Buffer) *syntax.Highlighter {
	h := e.highlighters[b.ID]
	if h == nil || h.Filetype != b.Filetype {
		var regions func() []syntax.Region
		if k := e.kernels[b.ID]; k != nil && k.nb != nil {
			regions = func() []syntax.Region { return e.notebookRegions(b) }
		}
		h = syntax.NewHighlighter(b.Filetype, e.theme, regions)
		e.highlighters[b.ID] = h
	}
	return h
}

// ---------------------------------------------------------------- messages

func (e *Editor) setMsg(style int, format string, args ...any) {
	e.msg = fmt.Sprintf(format, args...)
	e.msgStyle = style
	if e.msg != "" {
		e.messages = append(e.messages, e.msg)
		if len(e.messages) > 200 {
			e.messages = e.messages[len(e.messages)-200:]
		}
	}
}

func (e *Editor) infof(format string, args ...any) { e.setMsg(0, format, args...) }
func (e *Editor) errorf(format string, args ...any) {
	e.setMsg(1, format, args...)
	if e.replaying > 0 {
		e.abortReplay = true
	}
}
func (e *Editor) warnf(format string, args ...any) { e.setMsg(2, format, args...) }

// ---------------------------------------------------------------- quitting

// modified reports unsaved changes, including notebook outputs.
func (e *Editor) modified(b *buffer.Buffer) bool {
	if b.Modified() {
		return true
	}
	dirty, _ := b.Data["outputsDirty"].(bool)
	return dirty
}

func (e *Editor) modifiedBuffers() []*buffer.Buffer {
	var out []*buffer.Buffer
	for _, b := range e.buffers {
		if e.modified(b) && (b.Kind == buffer.KindFile || b.Kind == buffer.KindNotebook) && !b.ReadOnly {
			out = append(out, b)
		}
	}
	return out
}

func (e *Editor) quitAll(force bool) {
	if !force {
		if mod := e.modifiedBuffers(); len(mod) > 0 {
			e.errorf("E37: No write since last change for %q (add ! to override)", mod[0].Name)
			return
		}
		for _, tv := range e.tables {
			if tv.pending() {
				e.errorf("E37: %s staged in %q: apply (:w) or discard (ZQ) them (add ! to override)", tv.changes(), tv.buf.Name)
				return
			}
		}
	}
	e.quit = true
}

// suspend stops the process like Ctrl-Z in a shell.
func (e *Editor) suspend() {
	if err := e.screen.Suspend(); err != nil {
		return
	}
	syscall.Kill(0, syscall.SIGTSTP)
	e.screen.Resume()
}

// GotoLine moves the cursor to a 1-based line (for +N on the command line).
func (e *Editor) GotoLine(n int) {
	w := e.win
	l := min(max(n-1, 0), w.buf.LineCount()-1)
	e.setCursor(w, Pos{l, firstNonBlank(w.buf, l)})
}

// EmergencySave writes modified file buffers to "<name>.scihe-recover"
// after a crash so no work is lost.
// EmergencySave writes modified file buffers to recovery files after a
// crash (the terminal is already restored) and stops the child processes.
func (e *Editor) EmergencySave() {
	for _, n := range e.saveRecovery() {
		fmt.Fprintln(os.Stderr, n)
	}
	e.stopChildren()
}

// saveRecovery writes NAME.scihe-recover (then .1, .2 … — never over an
// older one or a planted symlink) next to each modified file, or in the
// temp directory when that is not writable; it returns the notices.
func (e *Editor) saveRecovery() []string {
	var notes []string
	for _, b := range e.modifiedBuffers() {
		if b.Path == "" {
			continue
		}
		p, err := writeRecovery(b.Path+".scihe-recover", b.FileText())
		if err != nil {
			p, err = writeRecovery(filepath.Join(os.TempDir(), filepath.Base(b.Path)+".scihe-recover"), b.FileText())
		}
		if err != nil {
			notes = append(notes, fmt.Sprintf("scihe: could not save the unsaved changes of %s: %v", b.Path, err))
		} else {
			notes = append(notes, fmt.Sprintf("scihe: unsaved changes of %s written to %s", b.Path, p))
		}
	}
	return notes
}

func writeRecovery(base, text string) (string, error) {
	var err error
	for i := 0; i < 100; i++ {
		p := base
		if i > 0 {
			p = fmt.Sprintf("%s.%d", base, i)
		}
		var f *os.File
		f, err = os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.WriteString(text)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return p, werr
	}
	return "", err
}
