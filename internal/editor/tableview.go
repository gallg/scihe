package editor

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"

	"scihe/internal/buffer"
	"scihe/internal/keymap"
	"scihe/internal/syntax"
	"scihe/internal/table"
)

// The data viewer shows a CSV/TSV buffer as an aligned table in the same
// window. The cursor moves by cell (h/l); rows can be sorted and
// filtered without touching the file; selections (v, V, <C-v>, r, c, a)
// and cell marks (m, *, :mark) pick the cells that :stats, :sum, …,
// :hist and :recode work on.
//
// Edit mode (e) works like Emacs wdired: cell edits, new and deleted
// rows, column changes and recodings are staged in a working copy, shown
// highlighted, and written to the file only by :w / ZZ after
// confirmation. Unchanged records keep their exact text.

const (
	tableMaxCell = 40    // display width cap of one column
	tableSep     = " │ " // between columns
	tableSepW    = 3
	tableUndoMax = 100
)

// trow is one row of the working copy. Rows are copied before they are
// changed, so undo snapshots can share unchanged rows.
type trow struct {
	id    int      // stable identity (marks)
	orig  int      // index into t.Rows, -1 for a new row
	cells []string // len == number of columns
	dirty []bool   // cells changed by an edit
}

// tdata is the working copy of a table.
type tdata struct {
	header      []string
	hdirty      []bool
	rows        []*trow
	colsChanged bool // columns were added or deleted: all records are rewritten
	colsAdded   int
	colsDeleted int
	reordered   bool // rows were sorted for the file (:sort apply)
}

func (d tdata) clone() tdata {
	c := d
	c.header = append([]string(nil), d.header...)
	c.hdirty = append([]bool(nil), d.hdirty...)
	c.rows = append([]*trow(nil), d.rows...)
	return c
}

type cellKey struct{ id, col int }

type tableView struct {
	src, buf *buffer.Buffer
	version  int // source version last parsed
	delim    rune
	noHeader bool
	t        *table.Table // the source as parsed
	d        tdata        // working copy (equal to t outside edits)
	nextID   int
	order    []int // displayed rows: indices into d.rows
	start    []int // display column where each column starts
	width    []int
	numeric  []bool
	sortKeys []sortKey // empty: file order
	filters  map[int]*table.Filter
	marks    map[cellKey]bool
	editing  bool
	stale    bool // the source changed while edits were staged
	undo     []tstate
	redo     []tstate
	counted  *tableChanges // cache of changes(), dropped by every edit
	theme    *syntax.Theme
}

// tableFor returns the view whose buffer is b.
func (e *Editor) tableFor(b *buffer.Buffer) *tableView { return e.tables[b.ID] }

func (e *Editor) tableOfSource(src *buffer.Buffer) *tableView {
	for _, tv := range e.tables {
		if tv.src == src {
			return tv
		}
	}
	return nil
}

func (tv *tableView) ncols() int { return len(tv.d.header) }

// row returns the working row shown on display row r.
func (tv *tableView) row(r int) *trow { return tv.d.rows[tv.order[r]] }

// reset makes the working copy equal to the parsed source.
func (tv *tableView) reset() {
	t := tv.t
	tv.d = tdata{header: append([]string(nil), t.Header...), hdirty: make([]bool, len(t.Header))}
	tv.d.rows = make([]*trow, len(t.Rows))
	for i, r := range t.Rows {
		tv.nextID++
		tv.d.rows[i] = &trow{id: tv.nextID, orig: i, cells: r, dirty: make([]bool, len(r))}
	}
	tv.undo, tv.redo, tv.stale, tv.counted = nil, nil, false, nil
	tv.marks = map[cellKey]bool{}
}

// changes summarises the staged edits.
type tableChanges struct {
	cells, header, added, deleted, colsAdded, colsDeleted int
	reordered                                             bool
}

func (c tableChanges) total() int {
	n := c.cells + c.header + c.added + c.deleted + c.colsAdded + c.colsDeleted
	if c.reordered {
		n++
	}
	return n
}

func (c tableChanges) String() string {
	var parts []string
	add := func(n int, one, many string) {
		if n == 1 {
			parts = append(parts, "1 "+one)
		} else if n > 1 {
			parts = append(parts, fmt.Sprintf("%d %s", n, many))
		}
	}
	add(c.cells, "cell", "cells")
	add(c.header, "header", "headers")
	add(c.added, "row added", "rows added")
	add(c.deleted, "row deleted", "rows deleted")
	add(c.colsAdded, "column added", "columns added")
	add(c.colsDeleted, "column deleted", "columns deleted")
	if c.reordered {
		parts = append(parts, "rows reordered")
	}
	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, ", ")
}

// changes summarises the staged edits (cached until the next edit: the
// status line asks on every redraw).
func (tv *tableView) changes() tableChanges {
	if tv.counted != nil {
		return *tv.counted
	}
	c := tv.countChanges()
	tv.counted = &c
	return c
}

func (tv *tableView) countChanges() tableChanges {
	var c tableChanges
	kept := 0
	for _, r := range tv.d.rows {
		if r.orig < 0 {
			c.added++
			continue
		}
		kept++
		if !tv.d.colsChanged {
			for _, d := range r.dirty {
				if d {
					c.cells++
				}
			}
		}
	}
	if tv.t != nil {
		c.deleted = len(tv.t.Rows) - kept
	}
	if !tv.d.colsChanged {
		for _, d := range tv.d.hdirty {
			if d {
				c.header++
			}
		}
	}
	c.colsAdded, c.colsDeleted = tv.d.colsAdded, tv.d.colsDeleted
	c.reordered = tv.d.reordered
	return c
}

func (tv *tableView) pending() bool { return tv.changes().total() > 0 }

// tstate is what undo restores: the working copy and the view state
// that refers to its columns (marks, filters, sort keys).
type tstate struct {
	d       tdata
	marks   map[cellKey]bool
	filters map[int]*table.Filter
	sort    []sortKey
}

func (tv *tableView) state() tstate {
	s := tstate{d: tv.d.clone(), marks: map[cellKey]bool{}, filters: map[int]*table.Filter{}, sort: append([]sortKey(nil), tv.sortKeys...)}
	for k, v := range tv.marks {
		s.marks[k] = v
	}
	for k, v := range tv.filters {
		s.filters[k] = v
	}
	return s
}

func (tv *tableView) restore(s tstate) {
	tv.d, tv.marks, tv.filters, tv.sortKeys = s.d, s.marks, s.filters, s.sort
	tv.counted = nil
}

// snapshot records the state for undo before an edit. Each level copies
// the row pointers, so big tables keep fewer levels.
func (tv *tableView) snapshot() {
	tv.counted = nil
	tv.undo = append(tv.undo, tv.state())
	depth := min(tableUndoMax, max(10, 20_000_000/max(len(tv.d.rows), 1)))
	if len(tv.undo) > depth {
		tv.undo = tv.undo[len(tv.undo)-depth:]
	}
	tv.redo = nil
}

// rollback undoes a partly applied edit that failed (and forgets it).
func (tv *tableView) rollback() {
	if n := len(tv.undo); n > 0 {
		tv.restore(tv.undo[n-1])
		tv.undo = tv.undo[:n-1]
	}
}

// cow returns a private copy of working row i, ready to change.
func (tv *tableView) cow(i int) *trow {
	r := tv.d.rows[i]
	n := &trow{id: r.id, orig: r.orig, cells: append([]string(nil), r.cells...), dirty: append([]bool(nil), r.dirty...)}
	tv.d.rows[i] = n
	tv.counted = nil
	return n
}

// setCell changes a cell of working row i (call snapshot first).
func (tv *tableView) setCell(i, c int, v string) error {
	if tv.delim == '\t' && strings.ContainsAny(v, "\t\n\r") {
		return errors.New("tab-separated cells cannot contain tabs or line breaks")
	}
	if tv.d.rows[i].cells[c] == v {
		return nil
	}
	r := tv.cow(i)
	r.cells[c] = v
	r.dirty[c] = true
	return nil
}

// exTable toggles the table view of the current buffer. Arguments:
// a delimiter (",", ";", "|", "tab"), "noheader", and "edit".
func exTable(e *Editor, a *exArgs) error {
	w := e.win
	if tv := e.tableFor(w.buf); tv != nil {
		if strings.TrimSpace(a.arg) == "edit" {
			e.setTableEditing(tv, true)
			return nil
		}
		e.leaveTable(w, tv)
		return nil
	}
	src := w.buf
	if src.Kind != buffer.KindFile && src.Kind != buffer.KindScratch {
		return errors.New("not a text buffer")
	}
	var delim rune
	noHeader, edit := false, false
	for _, f := range strings.Fields(a.arg) {
		switch strings.ToLower(f) {
		case "tab", `\t`:
			delim = '\t'
		case "noheader", "nohead":
			noHeader = true
		case "header":
		case "edit":
			edit = true
		default:
			if r := []rune(f); len(r) == 1 {
				delim = r[0]
			} else {
				return fmt.Errorf("usage: :table [, | ; | \"|\" | tab] [noheader] [edit]")
			}
		}
	}
	tv := e.tableOfSource(src)
	if tv != nil && (delim != 0 && delim != tv.delim || noHeader != tv.noHeader) {
		if tv.pending() {
			return errors.New("the table view has staged edits: apply (:w) or discard (ZQ) them first")
		}
		e.dropTable(tv)
		tv = nil
	}
	if tv == nil {
		if delim == 0 {
			delim = table.Sniff(src.Path, sampleText(src, 50))
		}
		pb := buffer.New("table: " + src.Name)
		pb.Kind = buffer.KindPreview
		pb.ReadOnly = true
		e.addBuffer(pb)
		tv = &tableView{src: src, buf: pb, version: -1, delim: delim, noHeader: noHeader,
			filters: map[int]*table.Filter{}, marks: map[cellKey]bool{}}
		e.tables[pb.ID] = tv
		e.localMaps[pb.ID], e.localVisual[pb.ID] = e.tableKeys()
	}
	line := w.cur.Line
	e.refreshTable(tv)
	e.setBuffer(w, tv.buf)
	// land on the row of the source line
	row, best := 0, -1
	for i, ri := range tv.order {
		if o := tv.d.rows[ri].orig; o >= 0 {
			if l := tv.t.Lines[o]; l <= line && l > best {
				row, best = i, l
			}
		}
	}
	e.setCursor(w, Pos{Line: min(row+1, tv.buf.LineCount()-1)})
	switch {
	case tv.t.Err != nil:
		e.warnf("table: parsed %d rows, then: %v", len(tv.t.Rows), tv.t.Err)
	case edit:
		e.setTableEditing(tv, true)
	default:
		e.infof("%d rows × %d columns (%s); h/l cells, r/c/a select, m mark, s sort, f filter, = stats, e edit, q back",
			len(tv.t.Rows), len(tv.t.Header), delimName(tv.delim))
	}
	return nil
}

func delimName(d rune) string {
	switch d {
	case '\t':
		return "tab-separated"
	case ',':
		return "comma-separated"
	}
	return fmt.Sprintf("%q-separated", d)
}

func sampleText(b *buffer.Buffer, n int) string {
	var sb strings.Builder
	for l := 0; l < b.LineCount() && l < n; l++ {
		sb.WriteString(b.LineString(l))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// leaveTable returns the window to the source at the current row; with
// staged edits it asks first.
func (e *Editor) leaveTable(w *Window, tv *tableView) {
	if tv.pending() {
		e.prompt(fmt.Sprintf("Discard %s staged in the table? (y/N) ", tv.changes()), "", func(s string, ok bool) {
			if confirmed(s, ok) {
				e.discardTable(tv)
				e.leaveTable(w, tv)
			}
		})
		return
	}
	line := -1
	if r := w.cur.Line - 1; r >= 0 && r < len(tv.order) {
		line = tv.sourceLine(tv.order[r])
	}
	if !e.bufferAlive(tv.src) {
		e.dropTable(tv)
		return
	}
	e.setBuffer(w, tv.src)
	if line >= 0 {
		e.setCursor(w, Pos{Line: min(line, tv.src.LineCount()-1)})
	}
}

// sourceLine is the source line of working row i (a new row: the line of
// the closest original row before it).
func (tv *tableView) sourceLine(i int) int {
	for ; i >= 0; i-- {
		if o := tv.d.rows[i].orig; o >= 0 && o < len(tv.t.Lines) {
			return tv.t.Lines[o]
		}
	}
	return 0
}

// dropTable deletes a view buffer.
func (e *Editor) dropTable(tv *tableView) {
	delete(e.tables, tv.buf.ID)
	if e.bufferAlive(tv.buf) {
		e.deleteBuffer(tv.buf, true)
	}
}

// updateTables re-renders visible views whose source or theme changed.
func (e *Editor) updateTables() {
	for _, tv := range e.tables {
		if !e.bufferAlive(tv.src) {
			e.dropTable(tv)
			continue
		}
		if tv.version != tv.src.Version && !tv.stale || tv.theme != e.theme {
			visible := false
			e.forWindows(func(w *Window) { visible = visible || w.buf == tv.buf })
			if visible {
				e.refreshTable(tv)
			}
		}
	}
}

// refreshTable parses the source (when it changed) and redraws the view.
// Staged edits are never thrown away: the view is marked stale instead.
func (e *Editor) refreshTable(tv *tableView) {
	if tv.version != tv.src.Version || tv.t == nil {
		if tv.t != nil && tv.pending() {
			tv.stale = true
		} else {
			e.reparseTable(tv)
		}
	}
	tv.theme = e.theme
	e.arrangeTable(tv)
	e.renderTable(tv)
}

func (e *Editor) reparseTable(tv *tableView) {
	tv.t = table.Parse(tv.src.Text(), tv.delim, !tv.noHeader)
	tv.version = tv.src.Version
	tv.reset()
	for c := range tv.filters {
		if c >= tv.ncols() {
			delete(tv.filters, c)
		}
	}
	tv.dropSortKeysFrom(tv.ncols())
}

// arrangeTable applies filters and sorting to compute the row order.
func (e *Editor) arrangeTable(tv *tableView) {
	rowsData := tv.d.rows
	tv.order = tv.order[:0]
rows:
	for i, r := range rowsData {
		for c, f := range tv.filters {
			if c < len(r.cells) && !f.Match(r.cells[c]) {
				continue rows
			}
		}
		tv.order = append(tv.order, i)
	}
	if len(tv.sortKeys) > 0 {
		sort.SliceStable(tv.order, func(i, j int) bool {
			return tv.rowLess(rowsData[tv.order[i]], rowsData[tv.order[j]])
		})
	}
}

// sortKey is one level of a multi-column sort.
type sortKey struct {
	col  int
	desc bool
}

// rowLess orders rows by the sort keys in turn; missing values are
// always last, whatever the direction.
func (tv *tableView) rowLess(x, y *trow) bool {
	for _, k := range tv.sortKeys {
		if k.col >= len(x.cells) {
			continue
		}
		a, b := x.cells[k.col], y.cells[k.col]
		am, bm := table.IsMissing(a), table.IsMissing(b)
		if am || bm {
			if am != bm {
				return bm
			}
			continue
		}
		r := table.Compare(a, b)
		if r == 0 {
			continue
		}
		if k.desc {
			return r > 0
		}
		return r < 0
	}
	return false
}

// sortIndex is the position of column c among the sort keys, or -1.
func (tv *tableView) sortIndex(c int) int {
	for i, k := range tv.sortKeys {
		if k.col == c {
			return i
		}
	}
	return -1
}

func (tv *tableView) dropSortKeysFrom(ncol int) {
	keys := tv.sortKeys[:0:0]
	for _, k := range tv.sortKeys {
		if k.col < ncol {
			keys = append(keys, k)
		}
	}
	tv.sortKeys = keys
}

// cellText is how a cell is displayed: one line without control or
// zero-width runes (which the renderer would draw with a different width
// than they are measured), at most tableMaxCell wide.
func cellText(s string) string {
	clean := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			r = '⏎'
		case r == '\t':
			r = ' '
		case r < 32 || r == 0x7f || (r >= 0x80 && r < 0xa0) || runewidth.RuneWidth(r) == 0:
			continue
		}
		clean = append(clean, r)
	}
	if textWidth(string(clean)) <= tableMaxCell {
		return string(clean)
	}
	w := 0
	for i, r := range clean {
		if w+runewidth.RuneWidth(r) > tableMaxCell-1 {
			return string(clean[:i]) + "…"
		}
		w += runewidth.RuneWidth(r)
	}
	return string(clean)
}

// textWidth is the display width of cleaned text, rune by rune exactly as
// the window renderer measures it.
func textWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r, w, 8)
	}
	return w
}

func (e *Editor) renderTable(tv *tableView) {
	th := e.theme
	n := tv.ncols()
	tv.width = make([]int, n)
	tv.numeric = make([]bool, n)
	heads := make([]string, n)
	for c, h := range tv.d.header {
		heads[c] = cellText(h)
		if i := tv.sortIndex(c); i >= 0 {
			heads[c] += map[bool]string{false: " ▲", true: " ▼"}[tv.sortKeys[i].desc]
			if len(tv.sortKeys) > 1 {
				heads[c] += strconv.Itoa(i + 1)
			}
		}
		if tv.filters[c] != nil {
			heads[c] += " ⧩"
		}
		tv.width[c] = textWidth(heads[c])
	}
	cells := make([][]string, len(tv.order))
	nums := make([]int, n)
	filled := make([]int, n)
	for i := range tv.order {
		r := tv.row(i)
		row := make([]string, n)
		for c, v := range r.cells {
			row[c] = cellText(v)
			tv.width[c] = max(tv.width[c], textWidth(row[c]))
			if !table.IsMissing(v) {
				filled[c]++
				if _, ok := table.Number(v); ok {
					nums[c]++
				}
			}
		}
		cells[i] = row
	}
	tv.start = make([]int, n)
	x := 0
	for c := range n {
		tv.width[c] = max(tv.width[c], 1)
		tv.numeric[c] = filled[c] > 0 && nums[c]*10 >= filled[c]*8
		tv.start[c] = x
		x += tv.width[c] + tableSepW
	}
	numSt := th.TokenStyle(chroma.LiteralNumber)
	head := th.CellHeader.Underline(true)
	changed := th.Warning.Bold(true)
	markBg := bgOf(th.Search)
	lines := make([]string, 0, len(cells)+1)
	spans := make([][]syntax.Span, 0, len(cells)+1)
	build := func(row []string, r *trow, header bool) {
		var sb strings.Builder
		var sp []syntax.Span
		pos := 0 // rune index
		for c := range n {
			if c > 0 {
				sb.WriteString(tableSep)
				sp = append(sp, syntax.Span{Start: pos, End: pos + 3, Style: th.Border.Background(bgOf(th.Default))})
				pos += 3
			}
			v := row[c]
			padN := tv.width[c] - textWidth(v)
			right := tv.numeric[c] && !header
			cellStart := pos
			if right {
				sb.WriteString(strings.Repeat(" ", padN))
				pos += padN
			}
			vs := pos
			sb.WriteString(v)
			pos += len([]rune(v))
			ve := pos
			if !right {
				sb.WriteString(strings.Repeat(" ", padN))
				pos += padN
			}
			var st tcell.Style
			has := true
			switch {
			case header && tv.d.hdirty[c]:
				st = head.Foreground(fgOf(changed))
			case header:
				st = head
			case r.dirty[c]:
				st = changed
			case table.IsMissing(v):
				st = th.Dim
			case tv.numeric[c]:
				_, has = table.Number(v)
				st = numSt
			default:
				has = false
			}
			if !header && tv.marks[cellKey{r.id, c}] {
				if !has {
					st = th.Default
				}
				sp = append(sp, syntax.Span{Start: cellStart, End: pos, Style: st.Background(markBg)})
				continue
			}
			if has {
				if header {
					sp = append(sp, syntax.Span{Start: cellStart, End: pos, Style: st})
				} else {
					sp = append(sp, syntax.Span{Start: vs, End: ve, Style: st})
				}
			}
		}
		// keep the padding: a trailing empty cell must stay reachable
		lines = append(lines, sb.String())
		spans = append(spans, sp)
	}
	build(heads, nil, true)
	for i, row := range cells {
		build(row, tv.row(i), false)
	}
	b := tv.buf
	b.SetText(strings.Join(lines, "\n"))
	e.static[b.ID] = spans
	o := *e.options(tv.src)
	o.Number, o.RelativeNumber, o.TabStop = true, false, 8
	e.opts[b.ID] = &o
	e.forWindows(func(w *Window) {
		if w.buf == b {
			e.clampCursor(w)
		}
	})
}

// column returns the column under display column vc.
func (tv *tableView) column(vc int) int {
	for c := len(tv.start) - 1; c >= 0; c-- {
		if vc >= tv.start[c] {
			return c
		}
	}
	return 0
}

// cursorColumn is the column of the cursor in w.
func (e *Editor) cursorColumn(w *Window, tv *tableView) int {
	return tv.column(displayCol(w.buf.Line(w.cur.Line), w.cur.Col, 8))
}

// cellPos is the buffer position of the start of a cell on view line l.
func (tv *tableView) cellPos(b *buffer.Buffer, l, c int) Pos {
	line := b.Line(l)
	vc := tv.start[c]
	if tv.numeric[c] && l > 0 {
		// numbers are right-aligned: land on the first digit
		for i := colForDisplay(line, vc, 8); i < len(line) && line[i] == ' ' && displayCol(line, i, 8) < vc+tv.width[c]-1; i++ {
			vc = displayCol(line, i+1, 8)
		}
	}
	return Pos{Line: l, Col: colForDisplay(line, vc, 8)}
}

func (e *Editor) gotoColumn(w *Window, tv *tableView, c int) {
	if len(tv.start) == 0 {
		return
	}
	c = min(max(c, 0), len(tv.start)-1)
	e.setCursor(w, tv.cellPos(w.buf, w.cur.Line, c))
	// show the whole column when it fits
	textW := e.textWidth(w)
	if end := tv.start[c] + tv.width[c]; end > w.left+textW {
		w.left = max(end-textW, 0)
	}
	if tv.start[c] < w.left {
		w.left = tv.start[c]
	}
}

// tableStatus describes the cursor cell for the status line.
func (e *Editor) tableStatus(w *Window, tv *tableView) string {
	if tv.ncols() == 0 {
		return "empty table"
	}
	c := e.cursorColumn(w, tv)
	s := fmt.Sprintf("col %d/%d %s", c+1, tv.ncols(), cellText(tv.d.header[c]))
	if r := w.cur.Line - 1; r >= 0 {
		s += fmt.Sprintf(" │ row %d/%d", r+1, len(tv.order))
	}
	if len(tv.order) != len(tv.d.rows) {
		s += fmt.Sprintf(" (of %d)", len(tv.d.rows))
	}
	if n := len(tv.marks); n > 0 {
		s += fmt.Sprintf(" │ %d marked", n)
	}
	if tv.editing {
		s += " │ EDIT"
		if ch := tv.changes(); ch.total() > 0 {
			s += fmt.Sprintf(" %d changes", ch.total())
		}
	}
	if tv.stale {
		s += " │ SOURCE CHANGED"
	}
	return s
}

// tableKeys returns the normal- and visual-mode keys of a table view;
// cell motions work in both, so selections grow by whole cells.
func (e *Editor) tableKeys() (normal, visual *keymap.Map[*Cmd]) {
	m := keymap.NewMap[*Cmd]()
	vm := keymap.NewMap[*Cmd]()
	cmd := func(f func(c *ctx, tv *tableView)) *Cmd {
		return action(func(c *ctx) {
			if tv := e.tableFor(c.b); tv != nil && tv.ncols() > 0 {
				f(c, tv)
			}
		})
	}
	bind := func(k, label string, f func(c *ctx, tv *tableView)) {
		m.Bind(keymap.Parse(k, e.leader), label, cmd(f))
	}
	vbind := func(k, label string, f func(c *ctx, tv *tableView)) {
		vm.Bind(keymap.Parse(k, e.leader), label, cmd(f))
	}
	both := func(k, label string, f func(c *ctx, tv *tableView)) {
		bind(k, label, f)
		vbind(k, label, f)
	}
	// editing keys say how to start editing when the view is read-only
	edit := func(f func(c *ctx, tv *tableView)) func(c *ctx, tv *tableView) {
		return func(c *ctx, tv *tableView) {
			if !tv.editing {
				e.infof("read-only view: press e to edit")
				return
			}
			f(c, tv)
		}
	}
	move := func(d int) func(c *ctx, tv *tableView) {
		return func(c *ctx, tv *tableView) { e.gotoColumn(c.w, tv, e.cursorColumn(c.w, tv)+d*c.count) }
	}
	for _, k := range []string{"l", "w", "<Tab>", "<Right>"} {
		both(k, "next column", move(1))
	}
	for _, k := range []string{"h", "b", "<S-Tab>", "<Left>"} {
		both(k, "previous column", move(-1))
	}
	both("0", "first column", func(c *ctx, tv *tableView) { e.gotoColumn(c.w, tv, 0) })
	both("$", "last column", func(c *ctx, tv *tableView) { e.gotoColumn(c.w, tv, len(tv.start)-1) })

	// selections: r row, c column, a everything
	bind("r", "select row", func(c *ctx, tv *tableView) { e.selectTable(c.w, tv, "row") })
	bind("c", "select column", func(c *ctx, tv *tableView) { e.selectTable(c.w, tv, "column") })
	bind("a", "select all", func(c *ctx, tv *tableView) { e.selectTable(c.w, tv, "all") })

	// marks
	bind("m", "mark / unmark cell", func(c *ctx, tv *tableView) {
		r := c.w.cur.Line - 1
		if r < 0 || r >= len(tv.order) {
			return
		}
		k := cellKey{tv.row(r).id, e.cursorColumn(c.w, tv)}
		if tv.marks[k] {
			delete(tv.marks, k)
		} else {
			tv.marks[k] = true
		}
		e.renderTable(tv)
	})
	vbind("m", "mark selection", func(c *ctx, tv *tableView) {
		set := e.tableTarget(c.w, tv, nil, defColumn)
		e.exitVisual()
		n := 0
		set.each(func(r, col int) {
			tv.marks[cellKey{tv.row(r).id, col}] = true
			n++
		})
		e.renderTable(tv)
		e.infof("%d cells marked", n)
	})
	bind("*", "mark cells with this value", func(c *ctx, tv *tableView) {
		r := c.w.cur.Line - 1
		if r < 0 || r >= len(tv.order) {
			return
		}
		col := e.cursorColumn(c.w, tv)
		v := strings.TrimSpace(tv.row(r).cells[col])
		n := 0
		for i := range tv.order {
			if strings.TrimSpace(tv.row(i).cells[col]) == v {
				tv.marks[cellKey{tv.row(i).id, col}] = true
				n++
			}
		}
		e.renderTable(tv)
		e.infof("%d cells = %q marked in %s", n, v, tv.d.header[col])
	})
	bind("M", "invert marks in column", func(c *ctx, tv *tableView) {
		col := e.cursorColumn(c.w, tv)
		for i := range tv.order {
			k := cellKey{tv.row(i).id, col}
			if tv.marks[k] {
				delete(tv.marks, k)
			} else {
				tv.marks[k] = true
			}
		}
		e.renderTable(tv)
	})
	bind("U", "remove all marks", func(c *ctx, tv *tableView) {
		tv.marks = map[cellKey]bool{}
		e.renderTable(tv)
	})

	// sorting and filtering (view only)
	sortBy := func(desc, add bool) func(c *ctx, tv *tableView) {
		return func(c *ctx, tv *tableView) {
			col := e.cursorColumn(c.w, tv)
			key := sortKey{col, desc}
			switch i := tv.sortIndex(col); {
			case !add:
				tv.sortKeys = []sortKey{key}
			case i >= 0:
				tv.sortKeys[i] = key // change the direction of a level
			default:
				tv.sortKeys = append(tv.sortKeys, key)
			}
			e.rearrange(c.w, tv, col)
			e.infof("sorted by %s", tv.sortDescription())
		}
	}
	bind("s", "sort ascending", sortBy(false, false))
	bind("S", "sort descending", sortBy(true, false))
	bind("gs", "add column to the sort (ascending)", sortBy(false, true))
	bind("gS", "add column to the sort (descending)", sortBy(true, true))
	bind("o", "original order / (edit) add row below", func(c *ctx, tv *tableView) {
		if tv.editing {
			e.tableAddRow(c.w, tv, true)
			return
		}
		col := e.cursorColumn(c.w, tv)
		tv.sortKeys = nil
		e.rearrange(c.w, tv, col)
	})
	bind("O", "(edit) add row above", edit(func(c *ctx, tv *tableView) { e.tableAddRow(c.w, tv, false) }))
	bind("f", "filter column", func(c *ctx, tv *tableView) {
		col := e.cursorColumn(c.w, tv)
		cur := ""
		if f := tv.filters[col]; f != nil {
			cur = f.Expr
		}
		e.prompt("filter "+tv.d.header[col]+" (=, !=, <, >, ~re, text, empty): ", cur, func(s string, ok bool) {
			if ok {
				if err := e.setTableFilter(c.w, tv, col, s); err != nil {
					e.errorf("%v", err)
				}
			}
		})
	})
	bind("F", "clear filters", func(c *ctx, tv *tableView) {
		col := e.cursorColumn(c.w, tv)
		tv.filters = map[int]*table.Filter{}
		e.rearrange(c.w, tv, col)
	})
	bind("=", "statistics of column / selection", func(c *ctx, tv *tableView) { e.execEx("stats") })
	vbind("=", "statistics of selection", func(c *ctx, tv *tableView) { e.execEx("stats") })
	bind("<CR>", "show cell / (edit) edit cell", func(c *ctx, tv *tableView) {
		if tv.editing {
			e.tableEditCell(c.w, tv)
			return
		}
		col := e.cursorColumn(c.w, tv)
		if r := c.w.cur.Line - 1; r >= 0 && r < len(tv.order) {
			e.infof("%s = %s", tv.d.header[col], tv.row(r).cells[col])
		} else {
			e.infof("column %d: %s", col+1, tv.d.header[col])
		}
	})

	// editing (wdired-like)
	bind("e", "toggle edit mode", func(c *ctx, tv *tableView) { e.setTableEditing(tv, !tv.editing) })
	bind("i", "(edit) edit cell", edit(func(c *ctx, tv *tableView) { e.tableEditCell(c.w, tv) }))
	bind("x", "(edit) clear cell", edit(func(c *ctx, tv *tableView) { e.tableClear(c.w, tv, defCell) }))
	vbind("x", "(edit) clear selection", edit(func(c *ctx, tv *tableView) { e.tableClear(c.w, tv, defCell) }))
	bind("dd", "(edit) delete row", edit(func(c *ctx, tv *tableView) { e.tableDeleteRows(c.w, tv, c.count) }))
	vbind("d", "(edit) delete selected rows", edit(func(c *ctx, tv *tableView) { e.tableDeleteRows(c.w, tv, 1) }))
	bind("u", "(edit) undo", edit(func(c *ctx, tv *tableView) { e.tableUndo(c.w, tv, true) }))
	bind("<C-r>", "(edit) redo", edit(func(c *ctx, tv *tableView) { e.tableUndo(c.w, tv, false) }))
	bind("ZZ", "apply edits to the file", func(c *ctx, tv *tableView) {
		if err := e.tableApply(tv); err != nil {
			e.errorf("%v", err)
		}
	})
	bind("ZQ", "discard edits", func(c *ctx, tv *tableView) { e.tableDiscardPrompt(tv) })
	bind("q", "back to the text", func(c *ctx, tv *tableView) { e.leaveTable(c.w, tv) })
	return m, vm
}

func (e *Editor) setTableFilter(w *Window, tv *tableView, col int, expr string) error {
	if strings.TrimSpace(expr) == "" {
		delete(tv.filters, col)
	} else {
		f, err := table.ParseFilter(expr)
		if err != nil {
			return err
		}
		tv.filters[col] = f
	}
	e.rearrange(w, tv, col)
	e.infof("%d of %d rows", len(tv.order), len(tv.d.rows))
	return nil
}

// rearrange re-renders after a sort / filter / edit, keeping column col.
func (e *Editor) rearrange(w *Window, tv *tableView, col int) {
	e.arrangeTable(tv)
	e.renderTable(tv)
	if w.cur.Line > 0 {
		w.cur.Line = min(w.cur.Line, max(len(tv.order), 1))
	}
	if col >= 0 {
		e.gotoColumn(w, tv, col)
	}
}

// exWhere filters the table: ":where [column] EXPR", no argument clears.
func exWhere(e *Editor, a *exArgs) error {
	w := e.win
	tv := e.tableFor(w.buf)
	if tv == nil {
		return errors.New("not a table view (use :table)")
	}
	arg := strings.TrimSpace(a.arg)
	col := e.cursorColumn(w, tv)
	if arg == "" {
		tv.filters = map[int]*table.Filter{}
		e.rearrange(w, tv, col)
		return nil
	}
	if name, rest, ok := strings.Cut(arg, " "); ok {
		if c := tv.columnByName(name); c >= 0 {
			col, arg = c, rest
		}
	}
	return e.setTableFilter(w, tv, col, arg)
}

// columnByName finds a column of the working copy by name or 1-based
// number, or -1.
func (tv *tableView) columnByName(s string) int {
	return (&table.Table{Header: tv.d.header}).Column(s)
}

// exMark marks the cells of a column that match a condition:
// ":mark [column] EXPR".
func exMark(e *Editor, a *exArgs) error {
	w := e.win
	tv := e.tableFor(w.buf)
	if tv == nil {
		return errors.New("not a table view (use :table)")
	}
	if tv.ncols() == 0 {
		return errors.New("empty table")
	}
	arg := strings.TrimSpace(a.arg)
	if arg == "" {
		return errors.New("usage: :mark [column] =, !=, <, >, ~re, text or empty")
	}
	col := e.cursorColumn(w, tv)
	if name, rest, ok := strings.Cut(arg, " "); ok {
		if c := tv.columnByName(name); c >= 0 {
			col, arg = c, rest
		}
	}
	f, err := table.ParseFilter(arg)
	if err != nil {
		return err
	}
	n := 0
	for i := range tv.order {
		if r := tv.row(i); f.Match(r.cells[col]) {
			tv.marks[cellKey{r.id, col}] = true
			n++
		}
	}
	e.renderTable(tv)
	e.infof("%d cells marked in %s", n, tv.d.header[col])
	return nil
}

// selectTable starts a visual selection of the cursor row ("row"), the
// cursor column over all shown rows ("column") or everything ("all").
func (e *Editor) selectTable(w *Window, tv *tableView, what string) {
	last := w.buf.LineCount() - 1
	if last < 1 {
		return
	}
	col := e.cursorColumn(w, tv)
	line := min(max(w.cur.Line, 1), last)
	if e.mode.visual() {
		e.exitVisual()
	}
	switch what {
	case "row":
		w.cur = Pos{Line: line, Col: w.cur.Col}
		e.enterVisual(ModeVisualLine)
	case "all":
		w.cur = Pos{Line: 1}
		e.enterVisual(ModeVisualLine)
		w.cur = Pos{Line: last}
	case "column":
		w.cur = tv.cellPos(w.buf, 1, col)
		e.enterVisual(ModeVisualBlock)
		w.cur = tv.cellPos(w.buf, last, col)
	}
	e.clampCursor(w)
}

// ---------------------------------------------------------------- targets

const (
	defColumn = iota // without a selection: the cursor column
	defRow           // the cursor row (row-wise aggregates)
	defCell          // the cursor cell (clearing)
)

// cellSet is a set of cells: for each column, displayed rows (indices
// into tv.order, ascending).
type cellSet struct {
	cols []int
	rows map[int][]int
	how  string // "selection", "marks", "range", "column", "row", "cell"
}

func (s cellSet) each(f func(r, col int)) {
	for _, c := range s.cols {
		for _, r := range s.rows[c] {
			f(r, c)
		}
	}
}

func (s cellSet) count() int {
	n := 0
	for _, c := range s.cols {
		n += len(s.rows[c])
	}
	return n
}

// allRows is the union of the rows of all columns, ascending.
func (s cellSet) allRows() []int {
	seen := map[int]bool{}
	var out []int
	for _, c := range s.cols {
		for _, r := range s.rows[c] {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	sort.Ints(out)
	return out
}

// rect makes a set of rows × columns.
func rect(rows, cols []int, how string) cellSet {
	s := cellSet{cols: cols, rows: map[int][]int{}, how: how}
	for _, c := range cols {
		s.rows[c] = rows
	}
	return s
}

// tableTarget resolves the cells a command works on, in this order: the
// visual selection (active, or the last one when the command got its
// '<,'> range), the marked cells, the line range, else the default.
func (e *Editor) tableTarget(w *Window, tv *tableView, a *exArgs, def int) cellSet {
	b := w.buf
	ncol := tv.ncols()
	mode := ModeNormal
	var anchor, cur Pos
	switch {
	case e.mode.visual():
		mode, anchor, cur = e.mode, e.visual.anchor, w.cur
	case a != nil && a.hasRange && e.visual.lastBuf == b &&
		b.Marks['<'].Line == a.from && b.Marks['>'].Line == a.to:
		mode, anchor, cur = e.visual.lastMode, e.visual.lastAnchor, e.visual.lastCur
	}
	lineRows := func(l1, l2 int) []int {
		if l2 == 0 { // only the header line: the whole column(s)
			l1, l2 = 1, b.LineCount()-1
		}
		var rows []int
		for l := max(l1, 1); l <= l2 && l-1 < len(tv.order); l++ {
			rows = append(rows, l-1)
		}
		return rows
	}
	span := func(c1, c2 int) []int {
		var cols []int
		for c := c1; c <= c2 && c < ncol; c++ {
			cols = append(cols, c)
		}
		return cols
	}
	if mode != ModeNormal {
		rows := lineRows(min(anchor.Line, cur.Line), max(anchor.Line, cur.Line))
		if mode == ModeVisualLine {
			return rect(rows, span(0, ncol-1), "selection")
		}
		ca := tv.column(displayCol(b.Line(anchor.Line), anchor.Col, 8))
		cb := tv.column(displayCol(b.Line(cur.Line), cur.Col, 8))
		return rect(rows, span(min(ca, cb), max(ca, cb)), "selection")
	}
	if len(tv.marks) > 0 {
		s := cellSet{rows: map[int][]int{}, how: "marks"}
		for c := 0; c < ncol; c++ {
			for i := range tv.order {
				if tv.marks[cellKey{tv.row(i).id, c}] {
					s.rows[c] = append(s.rows[c], i)
				}
			}
			if len(s.rows[c]) > 0 {
				s.cols = append(s.cols, c)
			}
		}
		if len(s.cols) > 0 {
			return s
		}
	}
	col := e.cursorColumn(w, tv)
	if a != nil && a.hasRange {
		return rect(lineRows(a.from, a.to), []int{col}, "range")
	}
	r := w.cur.Line - 1
	switch def {
	case defRow:
		if r >= 0 && r < len(tv.order) {
			return rect([]int{r}, span(0, ncol-1), "row")
		}
	case defCell:
		if r >= 0 && r < len(tv.order) {
			return rect([]int{r}, []int{col}, "cell")
		}
		return cellSet{rows: map[int][]int{}, how: "cell"}
	}
	return rect(lineRows(1, b.LineCount()-1), []int{col}, "column")
}

// statsTarget resolves the data for a statistics command; explicit
// column names/numbers in the argument ("*" = all) replace the columns
// and use the rows of the selection.
func (e *Editor) statsTarget(a *exArgs, def int) (*tableView, cellSet, error) {
	w := e.win
	tv := e.tableFor(w.buf)
	if tv == nil {
		if e.tableOfSource(w.buf) == nil {
			return nil, cellSet{}, errors.New("not a table view (open one with :table)")
		}
		return nil, cellSet{}, errors.New("statistics work in the table view (:table)")
	}
	set := e.tableTarget(w, tv, a, def)
	if e.mode.visual() {
		e.exitVisual()
	}
	if arg := strings.TrimSpace(a.arg); arg != "" {
		rows := set.allRows()
		var cols []int
		for _, f := range splitArgs(arg) {
			if f == "*" {
				for c := 0; c < tv.ncols(); c++ {
					cols = append(cols, c)
				}
				continue
			}
			c := tv.columnByName(f)
			if c < 0 {
				return nil, cellSet{}, fmt.Errorf("no column %q", f)
			}
			cols = append(cols, c)
		}
		set = rect(rows, cols, set.how)
	}
	if set.count() == 0 {
		return nil, cellSet{}, errors.New("no cells selected")
	}
	return tv, set, nil
}

func (tv *tableView) cells(rows []int, col int) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = tv.row(r).cells[col]
	}
	return out
}

func rowsLabel(rows []int, tv *tableView) string {
	switch {
	case len(rows) == 0:
		return "no rows"
	case len(rows) == len(tv.order):
		return fmt.Sprintf("all %d rows", len(rows))
	case len(rows) == 1:
		return fmt.Sprintf("row %d", rows[0]+1)
	case rows[len(rows)-1]-rows[0] == len(rows)-1:
		return fmt.Sprintf("rows %d-%d", rows[0]+1, rows[len(rows)-1]+1)
	}
	return fmt.Sprintf("%d rows", len(rows))
}

// setLabel describes the rows of one column of a set.
func setLabel(s cellSet, col int, tv *tableView) string {
	if s.how == "marks" {
		return fmt.Sprintf("%d marked", len(s.rows[col]))
	}
	return rowsLabel(s.rows[col], tv)
}

// exAggregate runs one function (sum, mean, …) per selected column, or
// with ! per selected row across the selected columns.
func exAggregate(fn string) func(e *Editor, a *exArgs) error {
	return func(e *Editor, a *exArgs) error {
		def := defColumn
		if a.bang {
			def = defRow
		}
		tv, set, err := e.statsTarget(a, def)
		if err != nil {
			return err
		}
		if a.bang { // row-wise
			rows := set.allRows()
			names := make([]string, len(set.cols))
			for i, c := range set.cols {
				names[i] = tv.d.header[c]
			}
			lines := []string{fmt.Sprintf("%s per row over %s", fn, strings.Join(names, ", "))}
			var vals []string
			in := map[int]map[int]bool{} // column -> rows of the set
			for _, c := range set.cols {
				in[c] = map[int]bool{}
				for _, r := range set.rows[c] {
					in[c][r] = true
				}
			}
			for _, r := range rows {
				var cells []string
				for _, c := range set.cols {
					if in[c][r] {
						cells = append(cells, tv.row(r).cells[c])
					}
				}
				v, _ := table.Describe(cells).Value(fn)
				vals = append(vals, table.Format(v))
				lines = append(lines, fmt.Sprintf("  row %-6d %s", r+1, table.Format(v)))
			}
			if len(rows) == 1 {
				e.infof("%s(row %d) = %s", fn, rows[0]+1, vals[0])
				e.regs.Yank(0, Register{Text: vals[0]})
				return nil
			}
			e.showOutput("*stats*", strings.Join(lines, "\n"))
			return nil
		}
		var parts, vals []string
		for _, c := range set.cols {
			st := table.Describe(tv.cells(set.rows[c], c))
			v, _ := st.Value(fn)
			vals = append(vals, table.Format(v))
			parts = append(parts, fmt.Sprintf("%s(%s) = %s", fn, tv.d.header[c], table.Format(v)))
		}
		e.regs.Yank(0, Register{Text: strings.Join(vals, "\t")})
		label := setLabel(set, set.cols[0], tv)
		if set.how != "marks" {
			label = rowsLabel(set.allRows(), tv)
		}
		if len(set.cols) > 4 {
			e.showOutput("*stats*", strings.Join(parts, "\n")+"\n["+label+"]")
			return nil
		}
		e.infof("%s   [%s]", strings.Join(parts, "   "), label)
		return nil
	}
}

// exStats shows a summary table (like pandas describe) of the selection.
func exStats(e *Editor, a *exArgs) error {
	tv, set, err := e.statsTarget(a, defColumn)
	if err != nil {
		return err
	}
	type rowFn struct {
		name string
		get  func(st table.Stats) string
	}
	f := table.Format
	fns := []rowFn{
		{"count", func(st table.Stats) string { return strconv.Itoa(st.Count) }},
		{"missing", func(st table.Stats) string {
			if st.Missing == 0 {
				return "0"
			}
			return fmt.Sprintf("%d (%.1f%%)", st.Missing, 100*float64(st.Missing)/float64(st.Missing+st.Count))
		}},
		{"numeric", func(st table.Stats) string { return strconv.Itoa(st.Numeric) }},
		{"sum", func(st table.Stats) string { v, _ := st.Value("sum"); return f(v) }},
		{"mean", func(st table.Stats) string { return f(st.Mean) }},
		{"sem", func(st table.Stats) string { return f(st.SEM) }},
		{"95% CI", func(st table.Stats) string {
			if math.IsNaN(st.CILow) {
				return "—"
			}
			return f(st.CILow) + " … " + f(st.CIHigh)
		}},
		{"std", func(st table.Stats) string { return f(st.Std) }},
		{"cv", func(st table.Stats) string { return f(st.CV) }},
		{"min", func(st table.Stats) string { return f(st.Min) }},
		{"25%", func(st table.Stats) string { return f(st.Q1) }},
		{"median", func(st table.Stats) string { return f(st.Median) }},
		{"75%", func(st table.Stats) string { return f(st.Q3) }},
		{"max", func(st table.Stats) string { return f(st.Max) }},
		{"range", func(st table.Stats) string { return f(st.Range) }},
		{"iqr", func(st table.Stats) string { return f(st.IQR) }},
		{"skew", func(st table.Stats) string { return f(st.Skew) }},
		{"kurt", func(st table.Stats) string { return f(st.Kurt) }},
		{"unique", func(st table.Stats) string { return strconv.Itoa(st.Unique) }},
		{"top", func(st table.Stats) string { return cellText(st.Top) }},
		{"freq", func(st table.Stats) string { return strconv.Itoa(st.TopN) }},
	}
	cols := set.cols
	stats := make([]table.Stats, len(cols))
	widths := make([]int, len(cols))
	for i, c := range cols {
		stats[i] = table.Describe(tv.cells(set.rows[c], c))
		widths[i] = textWidth(cellText(tv.d.header[c]))
		for _, fn := range fns {
			widths[i] = max(widths[i], textWidth(fn.get(stats[i])))
		}
	}
	var sb strings.Builder
	what := rowsLabel(set.allRows(), tv)
	if set.how == "marks" {
		what = fmt.Sprintf("%d marked cells", set.count())
	}
	fmt.Fprintf(&sb, "%s of %s — %s\n\n", tv.src.Name, what, map[bool]string{true: "filtered", false: "unfiltered"}[len(tv.filters) > 0])
	sb.WriteString(fmt.Sprintf("%-8s", ""))
	for i, c := range cols {
		sb.WriteString("  " + padLeft(cellText(tv.d.header[c]), widths[i]))
	}
	sb.WriteByte('\n')
	for _, fn := range fns {
		sb.WriteString(fmt.Sprintf("%-8s", fn.name))
		for i := range cols {
			sb.WriteString("  " + padLeft(fn.get(stats[i]), widths[i]))
		}
		sb.WriteByte('\n')
	}
	e.showOutput("*stats*", strings.TrimRight(sb.String(), "\n"))
	return nil
}

func padLeft(s string, w int) string {
	return strings.Repeat(" ", max(w-textWidth(s), 0)) + s
}

// exHist draws a text histogram of the first selected column (value
// counts for text columns). An argument sets the number of bins.
func exHist(e *Editor, a *exArgs) error {
	bins := 0
	if n, err := strconv.Atoi(strings.TrimSpace(a.arg)); err == nil {
		bins = n
		a = &exArgs{hasRange: a.hasRange, from: a.from, to: a.to}
	}
	tv, set, err := e.statsTarget(a, defColumn)
	if err != nil {
		return err
	}
	col := set.cols[0]
	hist, numeric := table.Histogram(tv.cells(set.rows[col], col), bins)
	if len(hist) == 0 {
		return errors.New("no values")
	}
	labW, maxN, total := 0, 0, 0
	for _, h := range hist {
		labW = max(labW, textWidth(cellText(h.Label)))
		maxN = max(maxN, h.Count)
		total += h.Count
	}
	kind := "histogram"
	if !numeric {
		kind = "value counts"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s of %s (%s, %d values)\n\n", kind, tv.d.header[col], setLabel(set, col, tv), total)
	const barW = 40
	for _, h := range hist {
		n := float64(h.Count) / float64(max(maxN, 1)) * barW
		full := int(n)
		bar := strings.Repeat("█", full)
		if frac := n - float64(full); frac > 0 && full < barW {
			bar += string([]rune("▏▎▍▌▋▊▉")[min(int(frac*8), 6)])
		}
		lab := cellText(h.Label)
		fmt.Fprintf(&sb, "%s%s │%-*s %d (%.1f%%)\n", lab, strings.Repeat(" ", labW-textWidth(lab)), barW+1, bar, h.Count, 100*float64(h.Count)/math.Max(float64(total), 1))
	}
	e.showOutput("*stats*", strings.TrimRight(sb.String(), "\n"))
	return nil
}

// ---------------------------------------------------------------- editing

func (e *Editor) setTableEditing(tv *tableView, on bool) {
	if !on && tv.pending() {
		e.prompt(fmt.Sprintf("Leave edit mode and discard %s? (y/N) ", tv.changes()), "", func(s string, ok bool) {
			if confirmed(s, ok) {
				e.discardTable(tv)
				tv.editing = false
			}
		})
		return
	}
	tv.editing = on
	if on {
		e.infof("EDIT: i/<CR> edit cell, x clear, dd delete row, o/O add row, u/<C-r> undo/redo, :recode, :column; :w or ZZ applies, ZQ discards")
	} else {
		e.infof("read-only view")
	}
}

// ensureEditing turns edit mode on for commands that stage edits.
func (e *Editor) ensureEditing(tv *tableView) {
	if !tv.editing {
		tv.editing = true
		e.infof("edit mode on: :w applies the staged changes, ZQ discards them")
	}
}

func (e *Editor) tableEditCell(w *Window, tv *tableView) {
	col := e.cursorColumn(w, tv)
	if w.cur.Line == 0 {
		if tv.noHeader {
			e.errorf("this table has no header row to rename")
			return
		}
		e.prompt("column name: ", tv.d.header[col], func(s string, ok bool) {
			if ok && s != tv.d.header[col] {
				tv.snapshot()
				tv.d.header[col] = s
				tv.d.hdirty[col] = true
				e.rearrange(w, tv, col)
			}
		})
		return
	}
	r := w.cur.Line - 1
	if r < 0 || r >= len(tv.order) {
		return
	}
	i := tv.order[r]
	e.prompt(tv.d.header[col]+" = ", tv.d.rows[i].cells[col], func(s string, ok bool) {
		if !ok || s == tv.d.rows[i].cells[col] {
			return
		}
		tv.snapshot()
		if err := tv.setCell(i, col, s); err != nil {
			tv.rollback()
			e.errorf("%v", err)
			return
		}
		e.rearrange(w, tv, col)
	})
}

func (e *Editor) tableClear(w *Window, tv *tableView, def int) {
	set := e.tableTarget(w, tv, nil, def)
	if e.mode.visual() {
		e.exitVisual()
	}
	if set.count() == 0 {
		return
	}
	col := e.cursorColumn(w, tv)
	tv.snapshot()
	n := 0
	set.each(func(r, c int) {
		if i := tv.order[r]; tv.d.rows[i].cells[c] != "" {
			tv.setCell(i, c, "")
			n++
		}
	})
	e.rearrange(w, tv, col)
	e.infof("%d cells cleared", n)
}

func (e *Editor) tableDeleteRows(w *Window, tv *tableView, count int) {
	var rows []int
	if e.mode.visual() {
		rows = e.tableTarget(w, tv, nil, defRow).allRows()
		e.exitVisual()
	} else {
		for r := w.cur.Line - 1; r < w.cur.Line-1+count && r < len(tv.order); r++ {
			if r >= 0 {
				rows = append(rows, r)
			}
		}
	}
	if len(rows) == 0 {
		return
	}
	col := e.cursorColumn(w, tv)
	del := map[int]bool{}
	for _, r := range rows {
		del[tv.order[r]] = true
	}
	tv.snapshot()
	kept := tv.d.rows[:0:0]
	for i, r := range tv.d.rows {
		if del[i] {
			for k := range tv.marks {
				if k.id == r.id {
					delete(tv.marks, k)
				}
			}
			continue
		}
		kept = append(kept, r)
	}
	tv.d.rows = kept
	e.rearrange(w, tv, col)
	e.infof("%d rows deleted (u undoes)", len(rows))
}

func (e *Editor) tableAddRow(w *Window, tv *tableView, below bool) {
	col := e.cursorColumn(w, tv)
	at := 0 // index into d.rows
	if r := w.cur.Line - 1; r >= 0 && r < len(tv.order) {
		at = tv.order[r]
		if below {
			at++
		}
	}
	tv.snapshot()
	tv.nextID++
	n := tv.ncols()
	nr := &trow{id: tv.nextID, orig: -1, cells: make([]string, n), dirty: make([]bool, n)}
	for c := range nr.dirty {
		nr.dirty[c] = true
	}
	tv.d.rows = append(tv.d.rows[:at:at], append([]*trow{nr}, tv.d.rows[at:]...)...)
	e.arrangeTable(tv)
	e.renderTable(tv)
	for k, i := range tv.order {
		if i == at {
			w.cur.Line = k + 1
		}
	}
	e.gotoColumn(w, tv, col)
	if len(tv.filters) > 0 {
		e.infof("row added (hidden by the filters? F clears them)")
	}
}

func (e *Editor) tableUndo(w *Window, tv *tableView, undo bool) {
	from, to := &tv.undo, &tv.redo
	if !undo {
		from, to = to, from
	}
	if len(*from) == 0 {
		e.infof("nothing to %s", map[bool]string{true: "undo", false: "redo"}[undo])
		return
	}
	*to = append(*to, tv.state())
	tv.restore((*from)[len(*from)-1])
	*from = (*from)[:len(*from)-1]
	e.rearrange(w, tv, e.cursorColumn(w, tv))
	e.infof("%s staged", tv.changes())
}

func (e *Editor) discardTable(tv *tableView) {
	e.reparseTable(tv)
	e.arrangeTable(tv)
	e.renderTable(tv)
	e.infof("staged changes discarded")
}

func (e *Editor) tableDiscardPrompt(tv *tableView) {
	if !tv.pending() {
		e.infof("no staged changes")
		return
	}
	e.prompt(fmt.Sprintf("Discard %s? (y/N) ", tv.changes()), "", func(s string, ok bool) {
		if confirmed(s, ok) {
			e.discardTable(tv)
		}
	})
}

// tableApply asks for confirmation, writes the staged edits into the
// source buffer and saves the file.
func (e *Editor) tableApply(tv *tableView) error {
	ch := tv.changes()
	if ch.total() == 0 {
		e.infof("no staged changes")
		return nil
	}
	if tv.stale || tv.version != tv.src.Version {
		return errors.New("the text changed since the edits were staged: discard them (ZQ) and edit again")
	}
	text, err := e.buildTableText(tv)
	if err != nil {
		return err
	}
	extra := ""
	if e.modified(tv.src) {
		extra = " (its other unsaved changes are saved too)"
	}
	e.prompt(fmt.Sprintf("Apply %s to %s and save%s? (y/N) ", ch, tv.src.Name, extra), "", func(s string, ok bool) {
		if !confirmed(s, ok) {
			e.infof("nothing applied; the changes stay staged")
			return
		}
		src := tv.src
		ro := src.ReadOnly
		src.ReadOnly = false
		src.BeginUndo(Pos{})
		src.Replace(Pos{}, src.End(), text)
		src.EndUndo(Pos{})
		src.ReadOnly = ro
		err := e.writeBuffer(src, "", false)
		e.reparseTable(tv)
		e.arrangeTable(tv)
		e.renderTable(tv)
		if err != nil {
			e.errorf("changes applied to the text but not saved: %v", err)
			return
		}
		e.infof("applied %s to %s", ch, src.Name)
	})
	return nil
}

// buildTableText produces the new source text: unchanged records keep
// their exact lines, changed and new ones are encoded again.
func (e *Editor) buildTableText(tv *tableView) (string, error) {
	t := tv.t
	src := tv.src
	n := src.LineCount()
	// the byte order mark is put back once, in front of the new text
	line := func(l int) string {
		s := src.LineString(l)
		if l == 0 {
			s = strings.TrimPrefix(s, "\ufeff")
		}
		return s
	}
	var out []string
	verbatim := func(a, b int) {
		for l := a; l < b && l < n; l++ {
			out = append(out, line(l))
		}
	}
	encode := func(cells []string) error {
		s, err := table.Encode(cells, tv.delim)
		if err != nil {
			return err
		}
		out = append(out, strings.Split(s, "\n")...)
		return nil
	}
	// next record start after record i (or after the header)
	next := func(i int) int {
		if i+1 < len(t.Lines) {
			return t.Lines[i+1]
		}
		return n
	}
	first := n
	if t.HeaderLine >= 0 {
		first = t.HeaderLine
	} else if len(t.Lines) > 0 {
		first = t.Lines[0]
	}
	verbatim(0, first)
	if t.HeaderLine >= 0 {
		hEnd := n
		if len(t.Lines) > 0 {
			hEnd = t.Lines[0]
		}
		headerChanged := tv.d.colsChanged
		for _, d := range tv.d.hdirty {
			headerChanged = headerChanged || d
		}
		if headerChanged {
			if err := encode(tv.d.header); err != nil {
				return "", err
			}
			verbatim(t.HeaderEnd, hEnd)
		} else {
			verbatim(t.HeaderLine, hEnd)
		}
	}
	for _, r := range tv.d.rows {
		changed := r.orig < 0 || tv.d.colsChanged
		for _, d := range r.dirty {
			changed = changed || d
		}
		switch {
		case !changed:
			verbatim(t.Lines[r.orig], next(r.orig))
		case r.orig >= 0:
			if err := encode(r.cells); err != nil {
				return "", err
			}
			verbatim(t.Ends[r.orig], next(r.orig)) // blank lines after it
		default:
			if err := encode(r.cells); err != nil {
				return "", err
			}
		}
	}
	text := strings.Join(out, "\n")
	if t.BOM {
		text = "\ufeff" + text
	}
	return text, nil
}

// ---------------------------------------------------------------- columns

// insertColumn adds a column at index at; vals maps row ids to cells.
func (tv *tableView) insertColumn(at int, name string, vals map[int]string) {
	d := &tv.d
	d.header = append(d.header[:at:at], append([]string{name}, d.header[at:]...)...)
	d.hdirty = append(d.hdirty[:at:at], append([]bool{true}, d.hdirty[at:]...)...)
	for i, r := range d.rows {
		n := &trow{id: r.id, orig: r.orig}
		n.cells = append(append(append([]string(nil), r.cells[:at]...), vals[r.id]), r.cells[at:]...)
		n.dirty = append(append(append([]bool(nil), r.dirty[:at]...), true), r.dirty[at:]...)
		d.rows[i] = n
	}
	d.colsChanged = true
	d.colsAdded++
	tv.shiftColumns(at, 1)
}

func (tv *tableView) deleteColumn(at int) {
	d := &tv.d
	d.header = append(d.header[:at:at], d.header[at+1:]...)
	d.hdirty = append(d.hdirty[:at:at], d.hdirty[at+1:]...)
	for i, r := range d.rows {
		n := &trow{id: r.id, orig: r.orig}
		n.cells = append(append([]string(nil), r.cells[:at]...), r.cells[at+1:]...)
		n.dirty = append(append([]bool(nil), r.dirty[:at]...), r.dirty[at+1:]...)
		d.rows[i] = n
	}
	d.colsChanged = true
	d.colsDeleted++
	tv.shiftColumns(at, -1)
}

// shiftColumns moves marks, filters and the sort column after a column
// was inserted (delta 1) or deleted (delta -1) at index at.
func (tv *tableView) shiftColumns(at, delta int) {
	move := func(c int) (int, bool) {
		switch {
		case c < at:
			return c, true
		case delta < 0 && c == at:
			return 0, false
		}
		return c + delta, true
	}
	marks := map[cellKey]bool{}
	for k := range tv.marks {
		if c, ok := move(k.col); ok {
			marks[cellKey{k.id, c}] = true
		}
	}
	tv.marks = marks
	filters := map[int]*table.Filter{}
	for c, f := range tv.filters {
		if nc, ok := move(c); ok {
			filters[nc] = f
		}
	}
	tv.filters = filters
	keys := tv.sortKeys[:0:0]
	for _, k := range tv.sortKeys {
		if c, ok := move(k.col); ok {
			keys = append(keys, sortKey{c, k.desc})
		}
	}
	tv.sortKeys = keys
}

// uniqueName returns base, or base2, base3 … when a column has that name.
func (tv *tableView) uniqueName(base string) string {
	taken := func(n string) bool {
		for _, h := range tv.d.header {
			if strings.EqualFold(h, n) {
				return true
			}
		}
		return false
	}
	name := base
	for n := 2; taken(name); n++ {
		name = fmt.Sprintf("%s%d", base, n)
	}
	return name
}

// exColumn: ":column rename NAME | delete | add NAME" on the cursor column.
func exColumn(e *Editor, a *exArgs) error {
	w := e.win
	tv := e.tableFor(w.buf)
	if tv == nil {
		return errors.New("not a table view (use :table)")
	}
	sub, arg, _ := strings.Cut(strings.TrimSpace(a.arg), " ")
	arg = strings.TrimSpace(arg)
	col := e.cursorColumn(w, tv)
	if tv.ncols() == 0 && sub != "add" {
		return errors.New("empty table")
	}
	switch sub {
	case "rename":
		if arg == "" {
			return errors.New("usage: :column rename NAME")
		}
		if tv.noHeader {
			return errors.New("this table has no header row to rename")
		}
		e.ensureEditing(tv)
		tv.snapshot()
		tv.d.header[col] = arg
		tv.d.hdirty[col] = true
	case "delete":
		if tv.ncols() <= 1 {
			return errors.New("cannot delete the last column")
		}
		e.ensureEditing(tv)
		tv.snapshot()
		tv.deleteColumn(col)
		col = min(col, tv.ncols()-1)
	case "add":
		if arg == "" {
			arg = tv.uniqueName("new")
		}
		e.ensureEditing(tv)
		tv.snapshot()
		col = min(col+1, tv.ncols())
		tv.insertColumn(col, arg, nil)
		if tv.noHeader {
			e.warnf("column added; this table has no header row, so its name is not saved")
		}
	default:
		return errors.New("usage: :column rename NAME | delete | add [NAME]")
	}
	e.rearrange(w, tv, col)
	return nil
}

// exRecode: ":recode[!] [auto|number|bool|codes|map V=C …]" converts the
// target cells to numbers: into a new column after each column, or in
// place with !. The mapping of codes is shown in *recode*.
func exRecode(e *Editor, a *exArgs) error {
	w := e.win
	tv := e.tableFor(w.buf)
	if tv == nil {
		return errors.New("not a table view (use :table)")
	}
	args := splitArgs(a.arg)
	mode := "auto"
	if len(args) > 0 {
		mode, args = args[0], args[1:]
	}
	var mapping map[string]float64
	if mode == "map" {
		m, err := table.ParseMapping(args)
		if err != nil {
			return err
		}
		mapping = m
	} else if len(args) > 0 {
		return errors.New("usage: :recode[!] [auto | number | bool | codes | map VALUE=CODE …]")
	}
	set := e.tableTarget(w, tv, a, defColumn)
	if e.mode.visual() {
		e.exitVisual()
	}
	if set.count() == 0 {
		return errors.New("no cells selected")
	}
	type result struct {
		col  int
		name string
		rec  *table.Recoding
	}
	var results []result
	for _, c := range set.cols {
		rec, err := table.Recode(tv.cells(set.rows[c], c), mode, mapping)
		if err != nil {
			return err
		}
		results = append(results, result{c, tv.d.header[c], rec})
	}
	e.ensureEditing(tv)
	tv.snapshot()
	cursorCol := e.cursorColumn(w, tv)
	var report []string
	// right to left, so inserted columns do not move the ones still to do
	for k := len(results) - 1; k >= 0; k-- {
		res := results[k]
		rows := set.rows[res.col]
		target := res.name
		if a.bang {
			for j, r := range rows {
				// missing values stay as written ("NA", "")
				if i := tv.order[r]; !table.IsMissing(tv.d.rows[i].cells[res.col]) {
					tv.setCell(i, res.col, res.rec.Values[j])
				}
			}
		} else {
			suffix := "_num"
			if res.rec.Mode == "codes" {
				suffix = "_code"
			}
			target = tv.uniqueName(res.name + suffix)
			vals := map[int]string{}
			for j, r := range rows {
				vals[tv.row(r).id] = res.rec.Values[j]
			}
			tv.insertColumn(res.col+1, target, vals)
			if cursorCol > res.col {
				cursorCol++
			}
		}
		line := fmt.Sprintf("%s → %s (%s, %d values", res.name, target, res.rec.Mode, len(rows))
		if res.rec.Failed > 0 {
			line += fmt.Sprintf(", %d not convertible → missing", res.rec.Failed)
		}
		line += ")"
		block := []string{line}
		for _, m := range res.rec.Mapping {
			block = append(block, fmt.Sprintf("  %6s  %s  (%d)", table.Format(m.Code), m.Value, m.Count))
		}
		report = append(block, report...)
	}
	e.rearrange(w, tv, cursorCol)
	summary := strings.Join(strings.Split(report[0], "\n")[:1], "")
	if len(report) > 1 || len(results[0].rec.Mapping) > 0 || results[0].rec.Failed > 0 {
		cur := e.win
		e.showOutput("*recode*", strings.Join(report, "\n")+"\n\nStaged: :w (or ZZ in the table) applies, u undoes.")
		e.focus(cur)
	}
	e.infof("recoded %s; staged", summary)
	return nil
}
