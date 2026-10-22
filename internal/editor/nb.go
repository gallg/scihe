package editor

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"

	"scihe/internal/buffer"
	"scihe/internal/notebook"
	"scihe/internal/syntax"
)

// kernelState is a Python kernel attached to a buffer: a notebook
// (nb != nil) or a plain Python script evaluated piecewise.
type kernelState struct {
	e   *Editor
	buf *buffer.Buffer
	nb  *notebook.Notebook

	k        *notebook.Kernel
	python   string
	version  string
	starting bool

	queue   []*nbRequest
	running *nbRequest

	ranges        []notebook.Range
	rangesVersion int
	// outVer counts output changes per cell; dispCache memoizes display lines
	outVer    map[string]int
	dispCache map[string]cachedOutput
	startErr  strings.Builder // stderr seen before the kernel was ready
	folded    bool
	language  string
}

type cachedOutput struct {
	ver   int
	n     int
	lines []notebook.OutputLine
}

// touch invalidates the display cache of a cell.
func (k *kernelState) touch(id string) {
	if k.outVer == nil {
		k.outVer = map[string]int{}
	}
	k.outVer[id]++
}

// flushStreams copies accumulated stream text into the running cell's
// output objects (done lazily: appending per event would be quadratic).
func (k *kernelState) flushStreams() {
	r := k.running
	if r == nil || !r.dirty {
		return
	}
	for name, sb := range r.streams {
		if o := r.streamOut[name]; o != nil {
			o["text"] = sb.String()
		}
	}
	r.dirty = false
	k.touch(r.cellID)
}

func (k *kernelState) displayLines(c *notebook.Cell) []notebook.OutputLine {
	if k.running != nil && k.running.cellID == c.ID {
		k.flushStreams()
	}
	if k.dispCache == nil {
		k.dispCache = map[string]cachedOutput{}
	}
	ver := k.outVer[c.ID]
	if co, ok := k.dispCache[c.ID]; ok && co.ver == ver && co.n == len(c.Outputs) {
		return co.lines
	}
	lines := notebook.DisplayLines(c.Outputs)
	k.dispCache[c.ID] = cachedOutput{ver: ver, n: len(c.Outputs), lines: lines}
	return lines
}

type nbRequest struct {
	cellID string // notebook cell, or "" for script evaluation
	code   string
	line   int // script: line for the inline result
	id     int
	result string
	out    strings.Builder
	err    bool
	// notebook stream accumulation (see flushStreams)
	streams   map[string]*strings.Builder
	streamOut map[string]map[string]any
	streamIdx map[string]int // index of that output in the cell
	dirty     bool
}

func (k *kernelState) busy() bool { return k.running != nil || k.starting }

func (k *kernelState) close() {
	if k.k != nil {
		k.k.Close()
		k.k = nil
	}
	k.queue, k.running = nil, nil
}

func (k *kernelState) status() string {
	switch {
	case k.starting:
		return "kernel starting…"
	case k.running != nil:
		fr := spinner[int(time.Since(k.e.started)/(100*time.Millisecond))%len(spinner)]
		q := ""
		if len(k.queue) > 0 {
			q = fmt.Sprintf(" +%d", len(k.queue))
		}
		return fmt.Sprintf("%c busy%s", fr, q)
	case k.k != nil && k.k.Alive():
		return "py " + k.version + " idle"
	}
	return "no kernel"
}

// ---------------------------------------------------------------- open / save

func (e *Editor) openNotebook(path string) (*buffer.Buffer, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	nb, err := notebook.Load(abs)
	if err != nil {
		return nil, err
	}
	b := buffer.NewFromString(filepath.Base(abs), nb.ToText())
	b.Path = abs
	b.Kind = buffer.KindNotebook
	lang := notebookLanguage(nb)
	b.Filetype = lang
	e.addBuffer(b)
	ks := &kernelState{e: e, buf: b, nb: nb, rangesVersion: -1, language: lang}
	e.kernels[b.ID] = ks
	e.decor[b.ID] = ks
	e.recordModTime(b)
	if _, err := os.Stat(abs); err != nil {
		// new notebook: not saved yet, mark modified-free
		b.MarkSaved()
	}
	return b, nil
}

func notebookLanguage(nb *notebook.Notebook) string {
	var meta struct {
		LanguageInfo struct {
			Name string `json:"name"`
		} `json:"language_info"`
		Kernelspec struct {
			Language string `json:"language"`
		} `json:"kernelspec"`
	}
	json.Unmarshal(nb.Metadata, &meta)
	lang := strings.ToLower(meta.LanguageInfo.Name)
	if lang == "" {
		lang = strings.ToLower(meta.Kernelspec.Language)
	}
	if lang == "" || syntax.Lexer(lang) == nil {
		lang = "python"
	}
	return lang
}

func (e *Editor) reloadNotebook(b *buffer.Buffer) error {
	ks := e.kernels[b.ID]
	nb, err := notebook.Load(b.Path)
	if err != nil {
		return err
	}
	ks.nb = nb
	b.SetText(nb.ToText())
	b.MarkSaved()
	delete(b.Data, "outputsDirty")
	e.recordModTime(b)
	return nil
}

// syncNotebook updates the cell model from the buffer and normalizes markers.
func (e *Editor) syncNotebook(ks *kernelState) {
	b := ks.buf
	ranges := ks.nb.Sync(b.LineCount(), b.LineString)
	// rewrite markers without ids / leading text without a marker,
	// bottom-up so line numbers stay valid
	for i := len(ranges) - 1; i >= 0; i-- {
		r := ranges[i]
		c := ks.nb.Cell(r.ID)
		want := notebook.Marker(c)
		if r.Header < 0 {
			b.Insert(Pos{0, 0}, want+"\n")
		} else if b.LineString(r.Header) != want {
			b.Replace(Pos{r.Header, 0}, Pos{r.Header, b.LineLen(r.Header)}, want)
		}
	}
	ks.rangesVersion = -1
}

func (e *Editor) saveNotebook(b *buffer.Buffer, path string) error {
	ks := e.kernels[b.ID]
	ks.flushStreams()
	e.syncNotebook(ks)
	data, err := ks.nb.Marshal()
	if err != nil {
		return err
	}
	if path == "" {
		path = b.Path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := b.WriteFile(abs, data); err != nil {
		return err
	}
	if abs == b.Path {
		delete(b.Data, "outputsDirty")
	}
	return nil
}

// ---------------------------------------------------------------- ranges / decorations

func (k *kernelState) cellRanges() []notebook.Range {
	if k.rangesVersion != k.buf.Version {
		k.ranges = notebook.Ranges(k.buf.LineCount(), k.buf.LineString)
		k.rangesVersion = k.buf.Version
	}
	return k.ranges
}

func (k *kernelState) rangeAt(line int) (notebook.Range, bool) {
	for _, r := range k.cellRanges() {
		if (line >= r.Start && line < r.End) || line == r.Header {
			return r, true
		}
	}
	rs := k.cellRanges()
	if len(rs) > 0 && line >= rs[len(rs)-1].Start {
		return rs[len(rs)-1], true
	}
	return notebook.Range{}, false
}

func (e *Editor) notebookRegions(b *buffer.Buffer) []syntax.Region {
	k := e.kernels[b.ID]
	if k == nil || k.nb == nil {
		return nil
	}
	var out []syntax.Region
	for _, r := range k.cellRanges() {
		ft := k.language
		switch r.Type {
		case "markdown":
			ft = "markdown"
		case "raw":
			ft = ""
		}
		out = append(out, syntax.Region{Start: r.Start, End: r.End, Filetype: ft})
	}
	return out
}

// Header implements Decorator: a cell marker becomes the top edge of the
// cell's box, labelled "In [n]  python", "Markdown" or "Raw".
func (k *kernelState) Header(line int) (string, tcell.Style, bool) {
	if k.nb == nil {
		return "", tcell.Style{}, false
	}
	l := k.buf.LineString(line)
	if !strings.HasPrefix(l, "# %%") {
		return "", tcell.Style{}, false
	}
	r, ok := k.rangeAt(line)
	if !ok || r.Header != line {
		return "", tcell.Style{}, false
	}
	th := k.e.theme
	st := th.CurLineNr // accent, bold
	switch r.Type {
	case "code":
		return fmt.Sprintf("In [%s]  %s", k.countLabel(r.ID), k.language), st, true
	case "markdown":
		return "Markdown", st, true
	}
	return "Raw", st, true
}

// countLabel is the execution count shown in In [n] / Out [n]: "*" while
// queued or running, " " before the first run.
func (k *kernelState) countLabel(id string) string {
	if k.running != nil && k.running.cellID == id {
		return "*"
	}
	for _, q := range k.queue {
		if q.cellID == id {
			return "*"
		}
	}
	if c := k.nb.Cell(id); c != nil && c.ExecutionCount != nil {
		return fmt.Sprint(*c.ExecutionCount)
	}
	return " "
}

// Frame implements framer: lines of a cell (marker included) are boxed;
// the box of the cell under the cursor uses the accent colour.
func (k *kernelState) Frame(line int) (tcell.Style, bool) {
	if k.nb == nil {
		return tcell.Style{}, false
	}
	r, ok := k.rangeAt(line)
	if !ok || r.Header < 0 {
		return tcell.Style{}, false
	}
	return k.frameStyle(r), true
}

func (k *kernelState) frameStyle(r notebook.Range) tcell.Style {
	th := k.e.theme
	if w := k.e.win; w != nil && w.buf == k.buf {
		if cur, ok := k.rangeAt(w.cur.Line); ok && cur.Header == r.Header {
			return th.Default.Foreground(fgOf(th.CurLineNr))
		}
	}
	return th.Default.Foreground(fgOf(th.LineNr))
}

func fgOf(st tcell.Style) tcell.Color {
	fg, _, _ := st.Decompose()
	return fg
}

// Virtual implements Decorator: after the last line of a cell come the
// bottom edge of its box, the output box of code cells and a blank row.
func (k *kernelState) Virtual(line int) []vline {
	if k.nb == nil {
		return nil
	}
	r, ok := k.rangeAt(line)
	if !ok || r.Header < 0 || max(r.End-1, r.Header) != line {
		return nil
	}
	fs := k.frameStyle(r)
	rows := []vline{{kind: vBoxBottom, frame: fs}}
	if r.Type == "code" && r.ID != "" {
		rows = append(rows, k.outputRows(r.ID)...)
	}
	return append(rows, vline{style: k.e.theme.Default})
}

// outputRows is the "Out [n]" box of a cell (nothing without output).
func (k *kernelState) outputRows(id string) []vline {
	th := k.e.theme
	c := k.nb.Cell(id)
	if c == nil {
		return nil
	}
	running := k.running != nil && k.running.cellID == id
	lines := k.displayLines(c)
	if len(lines) == 0 && !running {
		return nil
	}
	fs := th.Default.Foreground(fgOf(th.LineNr))
	row := func(text string, st tcell.Style) vline {
		return vline{text: text, style: st, kind: vBoxLine, frame: fs}
	}
	rows := []vline{{text: "Out [" + k.countLabel(id) + "]", style: th.Dim.Bold(true), kind: vBoxTop, frame: fs}}
	switch {
	case len(lines) == 0:
	case k.folded:
		rows = append(rows, row(fmt.Sprintf("… %d lines of output (folded)", len(lines)), th.Dim))
	default:
		maxN := k.e.cfg.MaxOutputLines
		shown := lines
		if len(shown) > maxN {
			shown = shown[:maxN]
		}
		for _, l := range shown {
			st := th.Default
			switch l.Kind {
			case "stderr":
				st = th.Warning
			case "error":
				st = th.Error
			case "result":
				st = th.Info
			case "image", "info":
				st = th.Dim
			}
			rows = append(rows, row(l.Text, st))
		}
		if len(lines) > maxN {
			rows = append(rows, row(fmt.Sprintf("… %d more lines (<leader>nv to view)", len(lines)-maxN), th.Dim))
		}
	}
	if running {
		rows = append(rows, row("running…", th.Dim))
	}
	return append(rows, vline{kind: vBoxBottom, frame: fs})
}

// ---------------------------------------------------------------- kernel

func (e *Editor) kernelFor(b *buffer.Buffer) *kernelState {
	ks := e.kernels[b.ID]
	if ks == nil {
		ks = &kernelState{e: e, buf: b, rangesVersion: -1, language: "python"}
		e.kernels[b.ID] = ks
	}
	return ks
}

func (e *Editor) ensureKernel(ks *kernelState) error {
	if ks.k != nil && ks.k.Alive() {
		return nil
	}
	if ks.starting {
		return nil
	}
	py, err := e.pythonInterpreter()
	if err != nil {
		return err
	}
	dir := e.cwd
	if ks.buf.Path != "" {
		dir = filepath.Dir(ks.buf.Path)
	}
	ks.starting = true
	ks.python = py
	ks.startErr.Reset()
	var k *notebook.Kernel
	k, err = notebook.Start(py, dir, os.Environ(), func(ev notebook.Event) {
		e.Post(func() {
			// events of a kernel that was restarted/replaced are stale
			if ks.k == k {
				e.kernelEvent(ks, ev)
			}
		})
	})
	if err != nil {
		ks.starting = false
		return err
	}
	ks.k = k
	return nil
}

func (e *Editor) kernelEvent(ks *kernelState, ev notebook.Event) {
	if e.kernels[ks.buf.ID] != ks {
		return // buffer closed
	}
	switch ev.Type {
	case "ready":
		ks.starting = false
		ks.version = ev.Python
		e.infof("kernel ready: python %s (%s)", ev.Python, ks.python)
		e.pumpQueue(ks)
		return
	case "exit":
		if ks.running != nil || ks.starting || len(ks.queue) > 0 {
			msg := ev.Text
			if tail := strings.TrimSpace(ks.startErr.String()); tail != "" {
				if i := strings.LastIndexByte(tail, '\n'); i >= 0 {
					tail = tail[i+1:]
				}
				msg += ": " + tail
			}
			e.errorf("%s", msg)
		}
		ks.flushStreams()
		ks.starting = false
		ks.running = nil
		ks.queue = nil
		ks.k = nil
		return
	}
	req := ks.running
	if ks.starting && ev.Type == "stream" && ks.startErr.Len() < 8192 {
		ks.startErr.WriteString(ev.Text)
	}
	if req == nil || ev.ID != req.id {
		return
	}
	if ev.Type == "done" {
		ks.flushStreams()
		if req.cellID != "" && ks.nb != nil {
			if c := ks.nb.Cell(req.cellID); c != nil {
				n := ev.ExecutionCount
				c.ExecutionCount = &n
			}
			ks.buf.Version++ // force redraw of outputs and mark notebook changed
			e.markNotebookDirty(ks)
		} else {
			e.finishScriptEval(ks, req)
		}
		ks.running = nil
		if ev.Status == "error" {
			if n := len(ks.queue); n > 0 {
				e.warnf("execution stopped: %d queued cell(s) skipped", n)
			}
			ks.queue = nil
		}
		e.pumpQueue(ks)
		return
	}
	out := ev.Output()
	if out == nil {
		return
	}
	if req.cellID != "" && ks.nb != nil {
		c := ks.nb.Cell(req.cellID)
		if c == nil {
			return
		}
		// merge consecutive stream outputs of the same name
		if ev.Type == "stream" {
			if len(c.Outputs) > 0 {
				if sb := req.streams[ev.Name]; sb != nil && req.streamIdx[ev.Name] == len(c.Outputs)-1 {
					sb.WriteString(ev.Text)
					req.dirty = true
					return
				}
			}
			if req.streams == nil {
				req.streams = map[string]*strings.Builder{}
				req.streamOut = map[string]map[string]any{}
				req.streamIdx = map[string]int{}
			}
			ks.flushStreams()
			sb := &strings.Builder{}
			sb.WriteString(ev.Text)
			req.streams[ev.Name] = sb
			req.streamOut[ev.Name] = out
			req.streamIdx[ev.Name] = len(c.Outputs)
		} else {
			ks.flushStreams()
		}
		c.Outputs = append(c.Outputs, out)
		ks.touch(c.ID)
		return
	}
	// script evaluation
	switch ev.Type {
	case "stream":
		req.out.WriteString(ev.Text)
	case "execute_result":
		if t, ok := ev.Data["text/plain"].(string); ok {
			req.result = t
		}
	case "display_data":
		if _, ok := ev.Data["image/png"]; ok {
			req.out.WriteString("[image/png output — use a notebook to keep figures]\n")
			e.saveScriptImage(ev)
		} else if t, ok := ev.Data["text/plain"].(string); ok {
			req.out.WriteString(t + "\n")
		}
	case "error":
		req.err = true
		req.result = ev.Ename + ": " + ev.Evalue
		req.out.WriteString(strings.Join(ev.Traceback, "\n") + "\n")
	}
}

// markNotebookDirty records that outputs changed so :w is required
// (outputs live outside the buffer text).
func (e *Editor) markNotebookDirty(ks *kernelState) {
	ks.buf.Data["outputsDirty"] = true
}

func (e *Editor) pumpQueue(ks *kernelState) {
	if ks.running != nil || ks.starting || len(ks.queue) == 0 {
		return
	}
	if ks.k == nil || !ks.k.Alive() {
		if err := e.ensureKernel(ks); err != nil {
			e.errorf("kernel: %v", err)
			ks.queue = nil
		}
		return
	}
	req := ks.queue[0]
	ks.queue = ks.queue[1:]
	id, err := ks.k.Execute(req.code)
	if err != nil {
		e.errorf("kernel: %v", err)
		ks.queue = nil
		return
	}
	req.id = id
	ks.running = req
	if req.cellID != "" && ks.nb != nil {
		if c := ks.nb.Cell(req.cellID); c != nil {
			c.Outputs = nil
			ks.touch(c.ID)
		}
	}
}

func (e *Editor) enqueue(ks *kernelState, req *nbRequest) {
	ks.queue = append(ks.queue, req)
	if err := e.ensureKernel(ks); err != nil {
		e.errorf("kernel: %v", err)
		ks.queue = nil
		return
	}
	e.pumpQueue(ks)
}

// ---------------------------------------------------------------- commands

func (e *Editor) notebookHere() (*kernelState, error) {
	ks := e.kernels[e.win.buf.ID]
	if ks == nil || ks.nb == nil {
		return nil, errors.New("not a notebook buffer (open an .ipynb file)")
	}
	return ks, nil
}

func (e *Editor) runCells(ks *kernelState, ranges []notebook.Range) {
	for _, r := range ranges {
		if r.Type != "code" {
			continue
		}
		c := ks.nb.Cell(r.ID)
		if c == nil || strings.TrimSpace(c.Source) == "" {
			continue
		}
		e.enqueue(ks, &nbRequest{cellID: r.ID, code: c.Source})
	}
}

// currentCellRanges syncs and returns all ranges plus the index at the cursor.
func (e *Editor) currentCellRanges(ks *kernelState) ([]notebook.Range, int) {
	e.syncNotebook(ks)
	rs := ks.cellRanges()
	idx := -1
	for i, r := range rs {
		if e.win.cur.Line == r.Header || (e.win.cur.Line >= r.Start && e.win.cur.Line < r.End) {
			idx = i
		}
	}
	if idx < 0 && len(rs) > 0 {
		idx = len(rs) - 1
	}
	return rs, idx
}

func exNbRun(e *Editor, a *exArgs) error {
	ks, err := e.notebookHere()
	if err != nil {
		return err
	}
	rs, i := e.currentCellRanges(ks)
	if i < 0 {
		return nil
	}
	e.runCells(ks, rs[i:i+1])
	return nil
}

func (e *Editor) nbRunAndNext() {
	ks, err := e.notebookHere()
	if err != nil {
		e.errorf("%v", err)
		return
	}
	rs, i := e.currentCellRanges(ks)
	if i < 0 {
		return
	}
	e.runCells(ks, rs[i:i+1])
	if i+1 < len(rs) {
		e.setCursor(e.win, Pos{rs[i+1].Start, 0})
	} else {
		e.nbInsertCell(true, "code")
	}
}

func exNbRunAll(e *Editor, a *exArgs) error {
	ks, err := e.notebookHere()
	if err != nil {
		return err
	}
	rs, _ := e.currentCellRanges(ks)
	e.runCells(ks, rs)
	return nil
}

func (e *Editor) nbRunRange(below bool) {
	ks, err := e.notebookHere()
	if err != nil {
		e.errorf("%v", err)
		return
	}
	rs, i := e.currentCellRanges(ks)
	if i < 0 {
		return
	}
	if below {
		e.runCells(ks, rs[i:])
	} else {
		e.runCells(ks, rs[:i])
	}
}

func (e *Editor) nbInsertCell(below bool, typ string) {
	ks, err := e.notebookHere()
	if err != nil {
		e.errorf("%v", err)
		return
	}
	b := e.win.buf
	r, ok := ks.rangeAt(e.win.cur.Line)
	marker := notebook.Marker(&notebook.Cell{ID: notebook.NewID(), Type: typ})
	switch {
	case !ok:
		end := b.End()
		b.Insert(end, "\n"+marker+"\n")
		e.win.cur = Pos{b.LineCount() - 1, 0}
	case below:
		last := max(r.End-1, r.Header)
		b.Insert(Pos{last, b.LineLen(last)}, "\n"+marker+"\n")
		e.win.cur = Pos{last + 2, 0}
	default:
		h := max(r.Header, 0)
		b.Insert(Pos{h, 0}, marker+"\n\n")
		e.win.cur = Pos{h + 1, 0}
	}
	e.enterInsert()
}

// nbToggleType switches the current cell between code and markdown.
func (e *Editor) nbToggleType() {
	ks, err := e.notebookHere()
	if err != nil {
		e.errorf("%v", err)
		return
	}
	typ := "markdown"
	if r, ok := ks.rangeAt(e.win.cur.Line); ok && r.Type == "markdown" {
		typ = "code"
	}
	if err := e.setCellType(ks, typ); err != nil {
		e.errorf("%v", err)
	}
}

// setCellType rewrites the current cell's marker; a cell that stops
// being code loses its outputs, as in Jupyter.
func (e *Editor) setCellType(ks *kernelState, typ string) error {
	b := e.win.buf
	r, ok := ks.rangeAt(e.win.cur.Line)
	if !ok || r.Header < 0 {
		return errors.New("no cell here")
	}
	if r.Type == typ {
		return nil
	}
	id := r.ID
	if id == "" {
		id = notebook.NewID()
	}
	b.Replace(Pos{r.Header, 0}, Pos{r.Header, b.LineLen(r.Header)}, notebook.Marker(&notebook.Cell{ID: id, Type: typ}))
	if c := ks.nb.Cell(id); c != nil && typ != "code" {
		c.Outputs = nil
		c.ExecutionCount = nil
		ks.touch(id)
	}
	return nil
}

// exNbType sets the cell type: code (or the kernel language, e.g.
// python), markdown or raw; without an argument it toggles code/markdown.
func exNbType(e *Editor, a *exArgs) error {
	ks, err := e.notebookHere()
	if err != nil {
		return err
	}
	arg := strings.ToLower(strings.TrimSpace(a.arg))
	switch arg {
	case "":
		e.nbToggleType()
		return nil
	case "code", ks.language, "py", "python":
		return e.setCellType(ks, "code")
	case "markdown", "md":
		return e.setCellType(ks, "markdown")
	case "raw":
		return e.setCellType(ks, "raw")
	}
	return fmt.Errorf("unknown cell type %q (code, %s, markdown, raw)", arg, ks.language)
}

func (e *Editor) nbDeleteCell() {
	ks, err := e.notebookHere()
	if err != nil {
		e.errorf("%v", err)
		return
	}
	b := e.win.buf
	r, ok := ks.rangeAt(e.win.cur.Line)
	if !ok {
		return
	}
	start := max(r.Header, 0)
	end := max(r.End-1, start)
	reg := opRange{start: Pos{start, 0}, end: Pos{end, b.LineLen(end)}, linewise: true}
	e.regs.Delete(0, e.rangeText(b, reg))
	e.win.cur = e.deleteRange(b, reg)
	if b.LineCount() == 1 && b.LineLen(0) == 0 {
		b.Insert(Pos{}, notebook.Marker(&notebook.Cell{ID: notebook.NewID(), Type: "code"})+"\n")
		e.win.cur = Pos{1, 0}
	}
}

func (e *Editor) nbMoveCell(down bool) {
	ks, err := e.notebookHere()
	if err != nil {
		e.errorf("%v", err)
		return
	}
	rs, i := e.currentCellRanges(ks)
	j := i + 1
	if !down {
		j = i - 1
	}
	if i < 0 || j < 0 || j >= len(rs) || rs[i].Header < 0 || rs[j].Header < 0 {
		return
	}
	b := e.win.buf
	a, c := rs[min(i, j)], rs[max(i, j)]
	textA := e.linesText(b, a.Header, a.End-1)
	textC := e.linesText(b, c.Header, c.End-1)
	offset := e.win.cur.Line - rs[i].Header
	b.Replace(Pos{a.Header, 0}, Pos{c.End - 1, b.LineLen(c.End - 1)}, textC+"\n"+textA)
	if moved := ks.cellRanges(); j < len(moved) {
		e.win.cur = b.Clamp(Pos{moved[j].Header + offset, 0})
	}
}

func (e *Editor) nbToggleFold() {
	if ks := e.kernels[e.win.buf.ID]; ks != nil {
		ks.folded = !ks.folded
	}
}

func exNbClear(e *Editor, a *exArgs) error {
	ks, err := e.notebookHere()
	if err != nil {
		return err
	}
	if a.bang {
		for _, c := range ks.nb.Cells {
			c.Outputs = nil
			c.ExecutionCount = nil
			ks.touch(c.ID)
		}
	} else if r, ok := ks.rangeAt(e.win.cur.Line); ok {
		if c := ks.nb.Cell(r.ID); c != nil {
			c.Outputs = nil
			c.ExecutionCount = nil
			ks.touch(c.ID)
		}
	}
	e.markNotebookDirty(ks)
	ks.buf.Version++
	return nil
}

func exNbInterrupt(e *Editor, a *exArgs) error {
	ks := e.kernels[e.win.buf.ID]
	if ks == nil {
		if _, _, ok := e.replEntry(e.win.buf); ok { // a script: its REPL session
			return exRepl(e, &exArgs{arg: "interrupt"})
		}
	}
	if ks == nil || ks.k == nil {
		return errors.New("no kernel for this buffer")
	}
	ks.queue = nil
	ks.k.Interrupt()
	return nil
}

func exNbRestart(e *Editor, a *exArgs) error {
	ks := e.kernels[e.win.buf.ID]
	if ks == nil {
		if _, _, ok := e.replEntry(e.win.buf); ok { // a script: its REPL session
			return exRepl(e, &exArgs{arg: "restart"})
		}
		return errors.New("no kernel for this buffer")
	}
	ks.close()
	if err := e.ensureKernel(ks); err != nil {
		return err
	}
	e.infof("kernel restarting…")
	return nil
}

func (e *Editor) currentCellOutputs() ([]map[string]any, error) {
	ks, err := e.notebookHere()
	if err != nil {
		return nil, err
	}
	r, ok := ks.rangeAt(e.win.cur.Line)
	if !ok {
		return nil, errors.New("no cell here")
	}
	ks.flushStreams()
	c := ks.nb.Cell(r.ID)
	if c == nil {
		e.syncNotebook(ks)
		if r2, ok := ks.rangeAt(e.win.cur.Line); ok {
			c = ks.nb.Cell(r2.ID)
		}
	}
	if c == nil {
		return nil, errors.New("no cell here")
	}
	return c.Outputs, nil
}

func exNbImage(e *Editor, a *exArgs) error {
	if e.mappingDepth > 0 {
		return errMappingProgram
	}
	if ks := e.kernels[e.win.buf.ID]; (ks == nil || ks.nb == nil) && e.lastFigure != "" {
		return openWithSystem(e.lastFigure)
	}
	outs, err := e.currentCellOutputs()
	if err != nil {
		return err
	}
	imgs := notebook.Images(outs)
	if len(imgs) == 0 {
		return errors.New("no image output in this cell")
	}
	for _, img := range imgs {
		var data []byte
		ext := ".png"
		switch img.Mime {
		case "image/svg+xml":
			data, ext = []byte(img.Data), ".svg"
		default:
			if img.Mime == "image/jpeg" {
				ext = ".jpg"
			}
			data, err = base64.StdEncoding.DecodeString(strings.ReplaceAll(img.Data, "\n", ""))
			if err != nil {
				return fmt.Errorf("bad image data: %v", err)
			}
		}
		f, err := os.CreateTemp("", "scihe-output-*"+ext)
		if err != nil {
			return err
		}
		f.Write(data)
		f.Close()
		e.tempFiles = append(e.tempFiles, f.Name())
		if err := openWithSystem(f.Name()); err != nil {
			return fmt.Errorf("%v (image saved to %s)", err, f.Name())
		}
		e.infof("opened %s", f.Name())
	}
	return nil
}

// saveScriptImage stores a figure produced while evaluating a .py buffer.
func (e *Editor) saveScriptImage(ev notebook.Event) {
	s, _ := ev.Data["image/png"].(string)
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return
	}
	f, err := os.CreateTemp("", "scihe-figure-*.png")
	if err != nil {
		return
	}
	f.Write(data)
	f.Close()
	e.tempFiles = append(e.tempFiles, f.Name())
	e.lastFigure = f.Name()
}

func exNbOutput(e *Editor, a *exArgs) error {
	outs, err := e.currentCellOutputs()
	if err != nil {
		return err
	}
	var lines []string
	for _, l := range notebook.DisplayLines(outs) {
		lines = append(lines, l.Text)
	}
	if len(lines) == 0 {
		return errors.New("cell has no output")
	}
	e.showOutput("*cell-output*", strings.Join(lines, "\n"))
	return nil
}

// shellLineRe matches a `!command` line of a code cell.
var shellLineRe = regexp.MustCompile(`(?m)^([ \t]*)!([^=]|$)`)

func exNbExport(e *Editor, a *exArgs) error {
	ks, err := e.notebookHere()
	if err != nil {
		return err
	}
	e.syncNotebook(ks)
	out := a.arg
	if out == "" {
		out = strings.TrimSuffix(ks.buf.Path, filepath.Ext(ks.buf.Path)) + ".py"
	}
	if _, err := os.Stat(out); err == nil && !a.bang {
		return fmt.Errorf("E13: %s exists (add ! to override)", out)
	}
	var sb strings.Builder
	for i, c := range ks.nb.Cells {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		switch c.Type {
		case "code":
			// `!command` lines are not Python: commented out like jupytext
			sb.WriteString("# %%\n" + shellLineRe.ReplaceAllString(c.Source, "$1# !$2"))
		default:
			sb.WriteString("# %% [" + c.Type + "]\n")
			for j, l := range strings.Split(c.Source, "\n") {
				if j > 0 {
					sb.WriteByte('\n')
				}
				sb.WriteString(strings.TrimRight("# "+l, " "))
			}
		}
	}
	sb.WriteByte('\n')
	if err := os.WriteFile(out, []byte(sb.String()), 0o644); err != nil {
		return err
	}
	e.infof("exported to %s", out)
	return nil
}
