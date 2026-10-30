// Package buffer implements the text storage used by every editor window:
// a slice of rune lines, an undo/redo history grouped by command, and
// UTF-8 file loading/saving that preserves line endings.
package buffer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf8"
)

// Pos is a position in a buffer. Col is a rune index into the line.
type Pos struct {
	Line, Col int
}

// Less reports whether p is before q.
func (p Pos) Less(q Pos) bool {
	return p.Line < q.Line || (p.Line == q.Line && p.Col < q.Col)
}

// OrderPos returns a and b sorted.
func OrderPos(a, b Pos) (Pos, Pos) {
	if b.Less(a) {
		return b, a
	}
	return a, b
}

// Kind distinguishes file buffers from special buffers.
type Kind int

const (
	KindFile Kind = iota
	KindScratch
	KindNotebook
	KindPreview
	KindRepl
)

// ChangeListener is notified after every edit. line is the first affected
// line, removed is the number of old lines replaced, added the number of
// new lines in their place.
type ChangeListener func(line, removed, added int)

// Buffer holds the text of one file or special buffer.
type Buffer struct {
	ID       int
	Name     string // display name
	Path     string // absolute path, empty for special buffers
	Kind     Kind
	Filetype string
	ReadOnly bool
	CRLF     bool
	NoEOL    bool // file did not end with a newline
	// BlankLine: the file was one empty line ("\n"), which reads as the
	// same empty text as an empty file but is written back as "\n"
	BlankLine bool

	lines [][]rune

	undo      []*undoGroup
	redo      []*undoGroup
	refused   bool // an edit of the read-only buffer was refused
	cur       *undoGroup
	nextGroup int
	holds     int // EndUndo is ignored while > 0 (see HoldUndo)
	savedID   int // id of the top undo group when last saved (0 = empty)

	Version   int // increments on every change
	listeners []ChangeListener

	Marks map[rune]Pos

	// Data lets feature packages (notebook, repl) attach state.
	Data map[string]any
}

type opKind int

const (
	opInsert opKind = iota
	opDelete
)

type op struct {
	kind opKind
	pos  Pos
	text string
}

type undoGroup struct {
	id           int
	ops          []op
	cursorBefore Pos
	cursorAfter  Pos
}

var nextID = 1

// New returns an empty buffer.
func New(name string) *Buffer {
	b := &Buffer{Name: name, lines: [][]rune{{}}, Marks: map[rune]Pos{}, Data: map[string]any{}}
	b.ID = nextID
	nextID++
	return b
}

// NewFromString returns a buffer containing s.
func NewFromString(name, s string) *Buffer {
	b := New(name)
	b.setText(s)
	return b
}

// ErrBinary is returned by Load for files that are not valid UTF-8.
var ErrBinary = errors.New("file is not valid UTF-8")

// Load reads path into a new buffer. A missing file yields an empty buffer
// bound to path (it is created on first save).
func Load(path string) (*Buffer, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	b := New(filepath.Base(abs))
	b.Path = abs
	data, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return b, nil
		}
		return nil, err
	}
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("%s: %w", path, ErrBinary)
	}
	s := string(data)
	// CRLF only when every line ends so; with mixed endings the \r stay
	// in the text (shown as ^M) and the file is written back unchanged
	if n := strings.Count(s, "\r\n"); n > 0 && n == strings.Count(s, "\n") {
		b.CRLF = true
		s = strings.ReplaceAll(s, "\r\n", "\n")
	}
	b.BlankLine = s == "\n"
	if s != "" && !strings.HasSuffix(s, "\n") {
		b.NoEOL = true
	}
	s = strings.TrimSuffix(s, "\n")
	b.setText(s)
	return b, nil
}

func (b *Buffer) setText(s string) {
	parts := strings.Split(s, "\n")
	b.lines = make([][]rune, len(parts))
	for i, p := range parts {
		b.lines[i] = []rune(p)
	}
}

// SetText replaces the whole content without recording undo history.
// It is meant for generated buffers (rendered views, logs).
func (b *Buffer) SetText(s string) {
	old := len(b.lines)
	b.setText(s)
	for k, m := range b.Marks {
		b.Marks[k] = b.Clamp(m)
	}
	b.undo, b.redo, b.cur = nil, nil, nil
	b.savedID = 0
	b.changed(0, old, len(b.lines))
}

// Save writes the buffer to its Path (or to path if not empty).
func (b *Buffer) Save(path string) error {
	if path == "" {
		path = b.Path
	}
	if path == "" {
		return errors.New("no file name")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return b.WriteFile(abs, []byte(b.fileText()))
}

// WriteFile atomically writes data to path and, if path is the buffer's
// own file (or the buffer has none yet), makes it the buffer's file and
// marks the buffer unmodified.
func (b *Buffer) WriteFile(abs string, data []byte) error {
	if err := writeAtomic(abs, data); err != nil {
		return err
	}
	if b.Path == "" || b.Path == abs {
		b.Path = abs
		b.Name = filepath.Base(abs)
		b.MarkSaved()
	}
	return nil
}

// SaveCopy writes the buffer's text to path without making it the
// buffer's file.
func (b *Buffer) SaveCopy(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return writeAtomic(abs, []byte(b.fileText()))
}

// umask is the process file-creation mask, read once (new files get
// 0666 minus it, like any other program's).
var umask = func() os.FileMode {
	m := syscall.Umask(0)
	syscall.Umask(m)
	return os.FileMode(m)
}()

// resolveLink follows symlinks of path, also to a target that does not
// exist yet (a dangling link is written through, not replaced).
func resolveLink(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	p := path
	for hops := 0; hops < 40; hops++ {
		st, err := os.Lstat(p)
		if err != nil || st.Mode()&os.ModeSymlink == 0 {
			return p
		}
		link, err := os.Readlink(p)
		if err != nil {
			return p
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(p), link)
		}
		p = link
	}
	return path // a symlink loop: fails at write time
}

// writeAtomic replaces path with data through a temporary file in the
// same directory. A symlink is written through (its target changes, the
// link stays); the mode and, when possible, the owner of an existing file
// are kept; a new file gets 0666 minus the umask. The directory must
// exist. When the directory cannot hold a temporary file but the file
// itself is writable, it is rewritten in place.
func writeAtomic(path string, data []byte) error {
	target := resolveLink(path)
	mode := 0o666 &^ umask
	st, statErr := os.Stat(target)
	if statErr == nil {
		mode = st.Mode().Perm()
	}
	dir := filepath.Dir(target)
	if d, err := os.Stat(dir); err != nil || !d.IsDir() {
		return fmt.Errorf("directory %s does not exist", dir)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".scihe-*")
	if err != nil {
		if errors.Is(err, os.ErrPermission) && statErr == nil {
			return writeInPlace(target, data)
		}
		return err
	}
	tmpName := tmp.Name()
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmpName, mode)
	}
	if werr == nil && statErr == nil {
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			os.Chown(tmpName, int(sys.Uid), int(sys.Gid)) // best effort (needs permission)
		}
	}
	if werr == nil {
		werr = os.Rename(tmpName, target)
	}
	if werr != nil {
		os.Remove(tmpName)
		return werr
	}
	return nil
}

// writeInPlace truncates and rewrites an existing file (not atomic).
func writeInPlace(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// MarkSaved records the current state as the unmodified state.
func (b *Buffer) MarkSaved() {
	b.savedID = b.topID()
}

// FileText is the text as written to the file (line endings, final
// newline).
func (b *Buffer) FileText() string { return b.fileText() }

func (b *Buffer) fileText() string {
	s := b.Text()
	switch {
	case s == "":
		// no text writes an empty file (like vim), unless the file was
		// one empty line
		if !b.BlankLine {
			return ""
		}
		s = "\n"
	case !b.NoEOL:
		s += "\n"
	}
	if b.CRLF {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

func (b *Buffer) topID() int {
	if b.cur != nil && len(b.cur.ops) > 0 {
		return b.cur.id
	}
	if len(b.undo) == 0 {
		return 0
	}
	return b.undo[len(b.undo)-1].id
}

// Modified reports whether the buffer differs from its saved state.
func (b *Buffer) Modified() bool {
	return b.topID() != b.savedID
}

// AddListener registers fn to be called after each change.
func (b *Buffer) AddListener(fn ChangeListener) {
	b.listeners = append(b.listeners, fn)
}

func (b *Buffer) changed(line, removed, added int) {
	b.Version++
	for _, fn := range b.listeners {
		fn(line, removed, added)
	}
}

// marksInserted moves marks after an insertion at p that added nl
// newlines and whose last line has lastLen runes.
func (b *Buffer) marksInserted(p Pos, nl, n, lastLen int) {
	for k, m := range b.Marks {
		switch {
		case m.Line > p.Line:
			m.Line += nl
		case m.Line == p.Line && m.Col >= p.Col:
			if nl == 0 {
				m.Col += n
			} else {
				m.Line += nl
				m.Col = m.Col - p.Col + lastLen
			}
		default:
			continue
		}
		b.Marks[k] = m
	}
}

// marksDeleted moves marks after deleting [start, end); marks strictly
// inside the deleted text are removed (like vim).
func (b *Buffer) marksDeleted(start, end Pos) {
	// deleting from column 0 to column 0 of a later line removes whole
	// lines: marks at the start position die with them
	wholeLines := start.Col == 0 && end.Col == 0 && end.Line > start.Line
	for k, m := range b.Marks {
		switch {
		case m.Less(start):
			continue
		case m.Less(end):
			// inside: keep marks at the very start, drop the rest
			if m != start || wholeLines {
				// a line mark (col 0) on a fully deleted line disappears
				delete(b.Marks, k)
				continue
			}
		case m.Line == end.Line:
			m.Line = start.Line
			m.Col = start.Col + (m.Col - end.Col)
		default:
			m.Line -= end.Line - start.Line
		}
		b.Marks[k] = m
	}
}

// LineCount returns the number of lines (always >= 1).
func (b *Buffer) LineCount() int { return len(b.lines) }

// Line returns line i. The slice must not be modified.
func (b *Buffer) Line(i int) []rune {
	if i < 0 || i >= len(b.lines) {
		return nil
	}
	return b.lines[i]
}

// LineString returns line i as a string.
func (b *Buffer) LineString(i int) string { return string(b.Line(i)) }

// LineLen returns the rune length of line i.
func (b *Buffer) LineLen(i int) int { return len(b.Line(i)) }

// Text returns the full content joined by "\n" (no trailing newline).
func (b *Buffer) Text() string {
	var sb strings.Builder
	for i, l := range b.lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(string(l))
	}
	return sb.String()
}

// Clamp returns p limited to valid buffer positions (Col may equal line length).
func (b *Buffer) Clamp(p Pos) Pos {
	if p.Line < 0 {
		p.Line = 0
	}
	if p.Line >= len(b.lines) {
		p.Line = len(b.lines) - 1
	}
	if p.Col < 0 {
		p.Col = 0
	}
	if n := len(b.lines[p.Line]); p.Col > n {
		p.Col = n
	}
	return p
}

// End returns the position just after the last character.
func (b *Buffer) End() Pos {
	n := len(b.lines) - 1
	return Pos{n, len(b.lines[n])}
}

// RuneAt returns the rune at p, '\n' at end of line, 0 past end of buffer.
func (b *Buffer) RuneAt(p Pos) rune {
	if p.Line < 0 || p.Line >= len(b.lines) {
		return 0
	}
	l := b.lines[p.Line]
	if p.Col < 0 {
		return 0
	}
	if p.Col >= len(l) {
		if p.Line == len(b.lines)-1 {
			return 0
		}
		return '\n'
	}
	return l[p.Col]
}

// Next returns the position after p, treating line ends as a character.
// ok is false at end of buffer.
func (b *Buffer) Next(p Pos) (Pos, bool) {
	if p.Col < len(b.lines[p.Line]) {
		return Pos{p.Line, p.Col + 1}, true
	}
	if p.Line+1 < len(b.lines) {
		return Pos{p.Line + 1, 0}, true
	}
	return p, false
}

// Prev returns the position before p. ok is false at start of buffer.
func (b *Buffer) Prev(p Pos) (Pos, bool) {
	if p.Col > 0 {
		return Pos{p.Line, p.Col - 1}, true
	}
	if p.Line > 0 {
		return Pos{p.Line - 1, len(b.lines[p.Line-1])}, true
	}
	return p, false
}

// Slice returns the text between start (inclusive) and end (exclusive).
func (b *Buffer) Slice(start, end Pos) string {
	start, end = OrderPos(b.Clamp(start), b.Clamp(end))
	if start.Line == end.Line {
		return string(b.lines[start.Line][start.Col:end.Col])
	}
	var sb strings.Builder
	sb.WriteString(string(b.lines[start.Line][start.Col:]))
	for i := start.Line + 1; i < end.Line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(string(b.lines[i]))
	}
	sb.WriteByte('\n')
	sb.WriteString(string(b.lines[end.Line][:end.Col]))
	return sb.String()
}

// Offset converts p to a rune offset from the buffer start.
func (b *Buffer) Offset(p Pos) int {
	p = b.Clamp(p)
	off := 0
	for i := 0; i < p.Line; i++ {
		off += len(b.lines[i]) + 1
	}
	return off + p.Col
}

// PosAt converts a rune offset into a position.
func (b *Buffer) PosAt(off int) Pos {
	for i, l := range b.lines {
		if off <= len(l) {
			return Pos{i, off}
		}
		off -= len(l) + 1
	}
	return b.End()
}

// ---- editing primitives ----

// BeginUndo starts an undo group. Nested calls are ignored.
func (b *Buffer) BeginUndo(cursor Pos) {
	if b.cur != nil {
		return
	}
	b.nextGroup++
	b.cur = &undoGroup{id: b.nextGroup, cursorBefore: cursor}
}

// HoldUndo keeps the current undo group open across nested commands
// (:g, :normal) until the matching ReleaseUndo.
func (b *Buffer) HoldUndo() { b.holds++ }

// ReleaseUndo undoes one HoldUndo and closes the group when the last
// hold is released.
func (b *Buffer) ReleaseUndo(cursor Pos) {
	if b.holds > 0 {
		b.holds--
	}
	if b.holds == 0 {
		b.EndUndo(cursor)
	}
}

// EndUndo closes the current undo group.
func (b *Buffer) EndUndo(cursor Pos) {
	if b.cur == nil || b.holds > 0 {
		return
	}
	b.endUndo(cursor)
}

func (b *Buffer) endUndo(cursor Pos) {
	if b.cur == nil {
		return
	}
	g := b.cur
	b.cur = nil
	if len(g.ops) == 0 {
		return
	}
	g.cursorAfter = cursor
	b.undo = append(b.undo, g)
	b.redo = nil
}

// InUndoGroup reports whether a group is open.
func (b *Buffer) InUndoGroup() bool { return b.cur != nil }

func (b *Buffer) record(o op) {
	if b.cur == nil {
		// standalone change: its own group
		b.BeginUndo(o.pos)
		b.cur.ops = append(b.cur.ops, o)
		b.endUndo(o.pos)
		return
	}
	b.cur.ops = append(b.cur.ops, o)
}

// Insert inserts text at p and returns the position after the inserted text.
func (b *Buffer) Insert(p Pos, text string) Pos {
	if text == "" || b.refuse() {
		return b.Clamp(p)
	}
	p = b.Clamp(p)
	end := b.rawInsert(p, text)
	b.record(op{opInsert, p, text})
	return end
}

// Delete removes text between start and end (exclusive) and returns it.
func (b *Buffer) Delete(start, end Pos) string {
	start, end = OrderPos(b.Clamp(start), b.Clamp(end))
	if start == end || b.refuse() {
		return ""
	}
	text := b.rawDelete(start, end)
	b.record(op{opDelete, start, text})
	return text
}

// Replace deletes [start,end) and inserts text; returns end of insertion.
func (b *Buffer) Replace(start, end Pos, text string) Pos {
	start, _ = OrderPos(start, end)
	b.Delete(start, end)
	return b.Insert(start, text)
}

func (b *Buffer) rawInsert(p Pos, text string) Pos {
	parts := strings.Split(text, "\n")
	line := b.lines[p.Line]
	tail := append([]rune(nil), line[p.Col:]...)
	if len(parts) == 1 {
		r := []rune(parts[0])
		nl := make([]rune, 0, len(line)+len(r))
		nl = append(nl, line[:p.Col]...)
		nl = append(nl, r...)
		nl = append(nl, tail...)
		b.lines[p.Line] = nl
		b.marksInserted(p, 0, len(r), 0)
		b.changed(p.Line, 1, 1)
		return Pos{p.Line, p.Col + len(r)}
	}
	newLines := make([][]rune, len(parts))
	first := append(append([]rune(nil), line[:p.Col]...), []rune(parts[0])...)
	newLines[0] = first
	for i := 1; i < len(parts)-1; i++ {
		newLines[i] = []rune(parts[i])
	}
	lastR := []rune(parts[len(parts)-1])
	newLines[len(parts)-1] = append(append([]rune(nil), lastR...), tail...)
	rest := append([][]rune(nil), b.lines[p.Line+1:]...)
	b.lines = append(append(b.lines[:p.Line], newLines...), rest...)
	b.marksInserted(p, len(parts)-1, 0, len(lastR))
	b.changed(p.Line, 1, len(parts))
	return Pos{p.Line + len(parts) - 1, len(lastR)}
}

func (b *Buffer) rawDelete(start, end Pos) string {
	text := b.Slice(start, end)
	first := b.lines[start.Line][:start.Col]
	last := b.lines[end.Line][end.Col:]
	merged := make([]rune, 0, len(first)+len(last))
	merged = append(merged, first...)
	merged = append(merged, last...)
	removed := end.Line - start.Line + 1
	rest := append([][]rune(nil), b.lines[end.Line+1:]...)
	b.lines = append(append(b.lines[:start.Line], merged), rest...)
	b.marksDeleted(start, end)
	b.changed(start.Line, removed, 1)
	return text
}

func endOf(p Pos, text string) Pos {
	parts := strings.Split(text, "\n")
	if len(parts) == 1 {
		return Pos{p.Line, p.Col + utf8.RuneCountInString(text)}
	}
	return Pos{p.Line + len(parts) - 1, utf8.RuneCountInString(parts[len(parts)-1])}
}

// Undo reverts the last group. It returns the cursor to restore.
func (b *Buffer) Undo() (Pos, bool) {
	if b.refuse() {
		return Pos{}, false
	}
	if b.cur != nil {
		b.endUndo(b.cur.cursorBefore)
	}
	if len(b.undo) == 0 {
		return Pos{}, false
	}
	g := b.undo[len(b.undo)-1]
	b.undo = b.undo[:len(b.undo)-1]
	for i := len(g.ops) - 1; i >= 0; i-- {
		o := g.ops[i]
		if o.kind == opInsert {
			b.rawDelete(o.pos, endOf(o.pos, o.text))
		} else {
			b.rawInsert(o.pos, o.text)
		}
	}
	b.redo = append(b.redo, g)
	return b.Clamp(g.cursorBefore), true
}

// Redo re-applies the last undone group.
func (b *Buffer) Redo() (Pos, bool) {
	if b.refuse() {
		return Pos{}, false
	}
	if len(b.redo) == 0 {
		return Pos{}, false
	}
	g := b.redo[len(b.redo)-1]
	b.redo = b.redo[:len(b.redo)-1]
	for _, o := range g.ops {
		if o.kind == opInsert {
			b.rawInsert(o.pos, o.text)
		} else {
			b.rawDelete(o.pos, endOf(o.pos, o.text))
		}
	}
	b.undo = append(b.undo, g)
	return b.Clamp(g.cursorBefore), true
}

// refuse reports (and remembers, for Refused) an edit of a read-only
// buffer. Every change goes through Insert / Delete / Undo / Redo, so no
// command can modify a read-only buffer, whatever it checks itself.
func (b *Buffer) refuse() bool {
	if b.ReadOnly {
		b.refused = true
	}
	return b.ReadOnly
}

// Refused reports whether an edit was refused since the last call.
func (b *Buffer) Refused() bool {
	r := b.refused
	b.refused = false
	return r
}

// ClearUndo forgets the undo history (log buffers that only grow).
func (b *Buffer) ClearUndo() {
	if b.cur == nil {
		b.undo, b.redo = nil, nil
	}
}
