package editor

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/alecthomas/chroma/v2"

	"scihe/internal/buffer"
	"scihe/internal/keymap"
	"scihe/internal/syntax"
)

// The directory explorer works like Emacs dired: an `ls -l`-style
// listing where files can be opened, marked, flagged, renamed, copied
// and deleted. Line 0 is the directory, line 1 is "../", entry i of the
// listing is on line i+1. Everything uses the Go standard library; no
// external program is started.

type diredEntry struct {
	name string // without trailing slash; ".." for the parent
	dir  bool
	link string // symlink target
	info fs.FileInfo
}

type diredState struct {
	dir     string
	hide    bool // hide dotfiles
	entries []diredEntry
	marks   map[string]rune // '*' marked, 'D' flagged for deletion
}

const diredNameCol = 40 // rune column where names start

const diredHelp = "m mark  u/U unmark  t toggle  d flag  x delete flagged  D delete  R rename/move  C copy to  y copy  X cut  p paste  w copy path  + mkdir  % new file  . hidden  g refresh  - parent"

// fileClipboard holds absolute paths copied (y) or cut (X) in an
// explorer, to paste (p) in another directory.
type fileClipboard struct {
	paths []string
	cut   bool
}

// openExplorer shows a directory listing buffer.
func (e *Editor) openExplorer(dir string) (*buffer.Buffer, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if st, err := os.Stat(abs); err != nil {
		return nil, err
	} else if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	for id, d := range e.explorer {
		if b := e.bufferByID(id); b != nil && d == abs {
			e.refreshExplorer(b)
			return b, nil
		}
	}
	b := buffer.New(abs + "/")
	b.Kind = buffer.KindScratch
	b.Filetype = ""
	e.addBuffer(b)
	e.explorer[b.ID] = abs
	e.direds[b.ID] = &diredState{dir: abs, marks: map[string]rune{}}
	e.refreshExplorer(b)
	e.localMaps[b.ID] = e.diredKeys()
	return b, nil
}

func (e *Editor) diredKeys() *keymap.Map[*Cmd] {
	m := keymap.NewMap[*Cmd]()
	bind := func(k, label string, f func(c *ctx, d *diredState)) {
		m.Bind(keymap.Parse(k, e.leader), label, action(func(c *ctx) {
			if d := e.direds[c.b.ID]; d != nil {
				f(c, d)
			}
		}))
	}
	bind("<CR>", "open", func(c *ctx, d *diredState) { e.explorerOpen(c, false) })
	bind("l", "open", func(c *ctx, d *diredState) { e.explorerOpen(c, false) })
	bind("o", "open in split", func(c *ctx, d *diredState) { e.explorerOpen(c, true) })
	bind("-", "parent directory", func(c *ctx, d *diredState) { e.explorerUp(c) })
	bind("h", "parent directory", func(c *ctx, d *diredState) { e.explorerUp(c) })
	bind("^", "parent directory", func(c *ctx, d *diredState) { e.explorerUp(c) })
	bind("g", "refresh", func(c *ctx, d *diredState) { e.refreshExplorer(c.b) })
	bind(".", "show / hide dotfiles", func(c *ctx, d *diredState) {
		d.hide = !d.hide
		e.refreshExplorer(c.b)
	})
	bind("m", "mark", func(c *ctx, d *diredState) { e.diredMark(c, d, '*') })
	bind("d", "flag for deletion", func(c *ctx, d *diredState) { e.diredMark(c, d, 'D') })
	bind("u", "unmark", func(c *ctx, d *diredState) { e.diredMark(c, d, 0) })
	bind("U", "unmark all", func(c *ctx, d *diredState) {
		d.marks = map[string]rune{}
		e.refreshExplorer(c.b)
	})
	bind("t", "toggle marks", func(c *ctx, d *diredState) {
		for _, en := range d.entries {
			if en.name == ".." {
				continue
			}
			if d.marks[en.name] == '*' {
				delete(d.marks, en.name)
			} else if d.marks[en.name] == 0 {
				d.marks[en.name] = '*'
			}
		}
		e.refreshExplorer(c.b)
	})
	bind("x", "delete flagged", func(c *ctx, d *diredState) { e.diredDelete(c.b, d, d.selected('D', -1)) })
	bind("D", "delete marked / this", func(c *ctx, d *diredState) {
		e.diredDelete(c.b, d, d.selected('*', c.w.cur.Line))
	})
	bind("R", "rename / move", func(c *ctx, d *diredState) {
		e.diredTransfer(c.b, d, d.selected('*', c.w.cur.Line), false)
	})
	bind("C", "copy", func(c *ctx, d *diredState) {
		e.diredTransfer(c.b, d, d.selected('*', c.w.cur.Line), true)
	})
	clip := func(cut bool) func(c *ctx, d *diredState) {
		return func(c *ctx, d *diredState) {
			names := d.selected('*', c.w.cur.Line)
			if len(names) == 0 {
				return
			}
			paths := make([]string, len(names))
			for i, n := range names {
				paths[i] = filepath.Join(d.dir, n)
			}
			e.fileClip = fileClipboard{paths: paths, cut: cut}
			for _, n := range names {
				delete(d.marks, n)
			}
			e.refreshAllExplorers()
			verb := map[bool]string{true: "cut", false: "copied"}[cut]
			e.infof("%s %s — p pastes in any directory listing", verb, describeNames(names))
		}
	}
	bind("y", "copy to file clipboard", clip(false))
	bind("X", "cut to file clipboard", clip(true))
	bind("p", "paste file clipboard here", func(c *ctx, d *diredState) { e.diredPaste(c.b, d) })
	bind("w", "copy path as text", func(c *ctx, d *diredState) {
		names := d.selected('*', c.w.cur.Line)
		if len(names) == 0 {
			return
		}
		paths := make([]string, len(names))
		for i, n := range names {
			paths[i] = filepath.Join(d.dir, n)
		}
		text := strings.Join(paths, "\n")
		e.regs.Yank(c.reg, Register{Text: text})
		e.infof("yanked %s", describeNames(paths))
	})
	bind("+", "new directory", func(c *ctx, d *diredState) {
		e.prompt("New directory: ", "", func(s string, ok bool) {
			if !ok || strings.TrimSpace(s) == "" {
				return
			}
			p := resolveIn(d.dir, s)
			if err := os.MkdirAll(p, 0o755); err != nil {
				e.errorf("%v", err)
			}
			e.refreshExplorer(c.b)
			e.diredGoto(c.b, filepath.Base(p))
		})
	})
	bind("%", "new file", func(c *ctx, d *diredState) {
		e.prompt("New file: ", "", func(s string, ok bool) {
			if ok && strings.TrimSpace(s) != "" {
				e.openPath(c.w, resolveIn(d.dir, s))
			}
		})
	})
	bind("?", "show keys", func(c *ctx, d *diredState) { e.infof("%s", diredHelp) })
	return m
}

// resolveIn makes a typed path absolute, relative to base.
func resolveIn(base, s string) string {
	s = expandHome(strings.TrimSpace(s))
	if !filepath.IsAbs(s) {
		s = filepath.Join(base, s)
	}
	return filepath.Clean(s)
}

// entryAt returns the entry on a buffer line.
func (d *diredState) entryAt(line int) (diredEntry, bool) {
	if i := line - 1; i >= 0 && i < len(d.entries) {
		return d.entries[i], true
	}
	return diredEntry{}, false
}

// selected returns the names with mark m, else (line >= 0) the entry on
// that line; ".." is never selected.
func (d *diredState) selected(m rune, line int) []string {
	var out []string
	for _, en := range d.entries {
		if d.marks[en.name] == m && en.name != ".." {
			out = append(out, en.name)
		}
	}
	if len(out) == 0 && line >= 0 {
		if en, ok := d.entryAt(line); ok && en.name != ".." {
			out = []string{en.name}
		}
	}
	return out
}

// diredMark sets mark m (0 clears) on count entries and moves down.
func (e *Editor) diredMark(c *ctx, d *diredState, m rune) {
	l := c.w.cur.Line
	for i := 0; i < c.count; i++ {
		en, ok := d.entryAt(l)
		if !ok {
			break
		}
		if en.name != ".." {
			if m == 0 {
				delete(d.marks, en.name)
			} else {
				d.marks[en.name] = m
			}
		}
		l++
	}
	e.refreshExplorer(c.b)
	e.setCursor(c.w, Pos{Line: min(l, c.b.LineCount()-1), Col: diredNameCol})
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d", n)
	}
	f := float64(n)
	for _, u := range []string{"K", "M", "G", "T", "P"} {
		f /= unit
		if f < unit || u == "P" {
			if f < 10 {
				return fmt.Sprintf("%.1f%s", f, u)
			}
			return fmt.Sprintf("%.0f%s", f, u)
		}
	}
	return ""
}

func (e *Editor) refreshExplorer(b *buffer.Buffer) {
	d := e.direds[b.ID]
	if d == nil {
		return
	}
	// keep the cursor on the same name
	keep := map[*Window]string{}
	e.forWindows(func(w *Window) {
		if w.buf == b {
			if en, ok := d.entryAt(w.cur.Line); ok {
				keep[w] = en.name
			}
		}
	})
	ents, err := os.ReadDir(d.dir)
	d.entries = []diredEntry{{name: "..", dir: true}}
	if st, err := os.Stat(filepath.Dir(d.dir)); err == nil {
		d.entries[0].info = st
	}
	var dirs, files []diredEntry
	for _, en := range ents {
		name := en.Name()
		if d.hide && strings.HasPrefix(name, ".") {
			continue
		}
		info, ierr := en.Info()
		if ierr != nil {
			continue // vanished while listing
		}
		de := diredEntry{name: name, dir: en.IsDir(), info: info}
		if en.Type()&os.ModeSymlink != 0 {
			de.link, _ = os.Readlink(filepath.Join(d.dir, name))
			if st, err := os.Stat(filepath.Join(d.dir, name)); err == nil && st.IsDir() {
				de.dir = true
			}
		}
		if de.dir {
			dirs = append(dirs, de)
		} else {
			files = append(files, de)
		}
	}
	byName := func(s []diredEntry) {
		sort.Slice(s, func(i, j int) bool { return s[i].name < s[j].name })
	}
	byName(dirs)
	byName(files)
	d.entries = append(append(d.entries, dirs...), files...)
	// forget marks of names that are gone
	present := map[string]bool{}
	for _, en := range d.entries {
		present[en.name] = true
	}
	for n := range d.marks {
		if !present[n] {
			delete(d.marks, n)
		}
	}
	th := e.theme
	marked, flagged := 0, 0
	lines := []string{"  " + d.dir + "/"}
	spans := [][]syntax.Span{{{Start: 0, End: len([]rune(lines[0])), Style: th.CurLineNr}}}
	if err != nil {
		lines[0] += "  (" + err.Error() + ")"
	}
	dirSt := th.TokenStyle(chroma.NameFunction).Bold(true)
	linkSt := th.TokenStyle(chroma.NameBuiltin)
	for _, en := range d.entries {
		mark := d.marks[en.name]
		switch mark {
		case '*':
			marked++
		case 'D':
			flagged++
		default:
			mark = ' '
		}
		perm, size, date := strings.Repeat(" ", 10), "", ""
		if en.info != nil {
			perm = en.info.Mode().String()
			if len(perm) > 10 {
				perm = perm[len(perm)-10:]
			}
			if en.dir {
				size = "-"
			} else {
				size = humanSize(en.info.Size())
			}
			date = en.info.ModTime().Format("2006-01-02 15:04")
		}
		name := en.name
		if en.dir {
			name += "/"
		}
		if en.link != "" {
			name += " -> " + en.link
		}
		prefix := fmt.Sprintf("%c %-10s %7s  %-16s  ", mark, perm, size, date)
		prefix = fmt.Sprintf("%-*s", diredNameCol, prefix)
		lines = append(lines, prefix+name)
		var sp []syntax.Span
		nameSt := th.Default
		switch {
		case en.dir:
			nameSt = dirSt
		case en.link != "":
			nameSt = linkSt
		}
		total := diredNameCol + len([]rune(name))
		sp = append(sp, syntax.Span{Start: 2, End: diredNameCol, Style: th.Dim})
		sp = append(sp, syntax.Span{Start: diredNameCol, End: total, Style: nameSt})
		switch mark {
		case '*':
			sp = append(sp, syntax.Span{Start: 0, End: total, Style: th.Info.Bold(true)})
		case 'D':
			sp = append(sp, syntax.Span{Start: 0, End: total, Style: th.Error.Bold(true)})
		}
		spans = append(spans, sp)
	}
	if marked+flagged > 0 {
		lines[0] += fmt.Sprintf("   [%d marked, %d flagged]", marked, flagged)
	}
	if n := len(e.fileClip.paths); n > 0 {
		verb := map[bool]string{true: "to move", false: "to copy"}[e.fileClip.cut]
		lines[0] += fmt.Sprintf("   [clipboard: %d %s, p pastes]", n, verb)
	}
	spans[0][0].End = len([]rune(lines[0]))
	b.SetText(strings.Join(lines, "\n"))
	e.static[b.ID] = spans
	e.forWindows(func(w *Window) {
		if w.buf != b {
			return
		}
		l := min(max(w.cur.Line, 1), b.LineCount()-1)
		if name, ok := keep[w]; ok {
			for i, en := range d.entries {
				if en.name == name {
					l = i + 1
				}
			}
		}
		w.cur = Pos{Line: l, Col: diredNameCol}
		e.clampCursor(w)
	})
}

// diredGoto puts the cursor of windows showing b on name.
func (e *Editor) diredGoto(b *buffer.Buffer, name string) {
	d := e.direds[b.ID]
	if d == nil {
		return
	}
	for i, en := range d.entries {
		if en.name == name {
			e.forWindows(func(w *Window) {
				if w.buf == b {
					e.setCursor(w, Pos{Line: i + 1, Col: diredNameCol})
				}
			})
		}
	}
}

func (e *Editor) explorerOpen(c *ctx, split bool) {
	d := e.direds[c.b.ID]
	if d == nil {
		return
	}
	en, ok := d.entryAt(c.w.cur.Line)
	if !ok {
		return
	}
	path := filepath.Clean(filepath.Join(d.dir, en.name))
	b, err := e.loadBuffer(path)
	if err != nil {
		e.errorf("%v", err)
		return
	}
	if split {
		e.split(b, true)
		return
	}
	e.setBuffer(c.w, b)
	if en.name == ".." {
		// put the cursor on the directory we came from
		e.diredGoto(b, filepath.Base(d.dir))
	}
}

func (e *Editor) explorerUp(c *ctx) {
	c.w.cur = Pos{Line: 1}
	e.explorerOpen(c, false)
}

// ---------------------------------------------------------------- operations

func describeNames(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return fmt.Sprintf("%d files", len(names))
}

// diredDelete removes names (recursively) after confirmation.
func (e *Editor) diredDelete(b *buffer.Buffer, d *diredState, names []string) {
	if len(names) == 0 {
		e.infof("nothing to delete (flag with d, then x; or D on a file)")
		return
	}
	dirs := 0
	for _, n := range names {
		if st, err := os.Lstat(filepath.Join(d.dir, n)); err == nil && st.IsDir() {
			dirs++
		}
	}
	what := describeNames(names)
	if dirs > 0 {
		what += fmt.Sprintf(" (%d director%s, recursively)", dirs, map[bool]string{true: "y", false: "ies"}[dirs == 1])
	}
	e.prompt("Delete "+what+"? (y/N) ", "", func(s string, ok bool) {
		if !confirmed(s, ok) {
			e.infof("nothing deleted")
			return
		}
		n := 0
		for _, name := range names {
			p := filepath.Join(d.dir, name)
			if err := os.RemoveAll(p); err != nil {
				e.errorf("%v", err)
				continue
			}
			delete(d.marks, name)
			e.forgetPath(p)
			n++
		}
		e.refreshExplorer(b)
		if n > 0 && e.msgStyle != 1 {
			e.infof("deleted %s", describeNames(names[:n]))
		}
	})
}

// forgetPath closes unmodified buffers of a deleted file or directory
// (and anything below it); modified ones are kept with a warning.
func (e *Editor) forgetPath(p string) {
	for _, b := range append([]*buffer.Buffer(nil), e.buffers...) {
		path := b.Path
		if dir, ok := e.explorer[b.ID]; ok {
			path = dir
		}
		if path == "" || !within(path, p) {
			continue
		}
		if e.modified(b) {
			e.warnf("%s was deleted; its buffer has unsaved changes and was kept", b.Name)
			continue
		}
		e.deleteBuffer(b, true)
	}
}

// diredTransfer renames/moves (or copies) names to a destination: an
// existing directory receives them, otherwise (one name) it is the new
// name. Existing targets are never overwritten.
func (e *Editor) diredTransfer(b *buffer.Buffer, d *diredState, names []string, copyFiles bool) {
	if len(names) == 0 {
		return
	}
	verb := "Rename"
	if copyFiles {
		verb = "Copy"
	}
	initial := d.dir + string(filepath.Separator)
	if len(names) == 1 {
		initial += names[0]
	}
	e.prompt(verb+" "+describeNames(names)+" to: ", initial, func(s string, ok bool) {
		if !ok || strings.TrimSpace(s) == "" {
			return
		}
		dest := resolveIn(d.dir, s)
		destIsDir := false
		if st, err := os.Stat(dest); err == nil && st.IsDir() {
			destIsDir = true
		}
		if len(names) > 1 && !destIsDir {
			e.errorf("%s is not a directory", dest)
			return
		}
		done := 0
		for _, name := range names {
			src := filepath.Join(d.dir, name)
			dst := dest
			if destIsDir {
				dst = filepath.Join(dest, name)
			}
			if err := e.transfer(src, dst, copyFiles); !moveDone(err) {
				e.errorf("%v", err)
				continue
			} else if err != nil {
				e.warnf("%v", err)
			}
			delete(d.marks, name)
			if !copyFiles {
				e.renamePath(src, dst)
			}
			done++
		}
		e.refreshExplorer(b)
		for id, dir := range e.explorer {
			if id != b.ID {
				for _, x := range e.buffers {
					if x.ID == id && (dir == dest || dir == filepath.Dir(dest)) {
						e.refreshExplorer(x)
					}
				}
			}
		}
		if len(names) == 1 && filepath.Dir(dest) == d.dir {
			e.diredGoto(b, filepath.Base(dest))
		}
		if done > 0 && e.msgStyle != 1 {
			past := map[bool]string{true: "copied", false: "moved"}[copyFiles]
			e.infof("%s %s to %s", past, describeNames(names[:done]), dest)
		}
	})
}

// diredPaste copies (or, after a cut, moves) the clipboard files into
// the listed directory. A cut clipboard is emptied by the paste.
func (e *Editor) diredPaste(b *buffer.Buffer, d *diredState) {
	clip := e.fileClip
	if len(clip.paths) == 0 {
		e.infof("file clipboard is empty (y copies, X cuts)")
		return
	}
	var done []string
	var failed []string
	for _, src := range clip.paths {
		dst := filepath.Join(d.dir, filepath.Base(src))
		if err := e.transfer(src, dst, !clip.cut); !moveDone(err) {
			failed = append(failed, err.Error())
			continue
		} else if err != nil {
			e.warnf("%v", err)
		}
		if clip.cut {
			e.renamePath(src, dst)
		}
		done = append(done, filepath.Base(src))
	}
	if clip.cut {
		e.fileClip = fileClipboard{}
	}
	e.refreshAllExplorers()
	if len(done) == 1 {
		e.diredGoto(b, done[0])
	}
	verb := map[bool]string{true: "moved", false: "copied"}[clip.cut]
	switch {
	case len(failed) > 0 && len(done) > 0:
		e.errorf("%s %s; failed: %s", verb, describeNames(done), strings.Join(failed, "; "))
	case len(failed) > 0:
		e.errorf("%s", strings.Join(failed, "; "))
	default:
		e.infof("%s %s here", verb, describeNames(done))
	}
}

// refreshAllExplorers redraws every directory listing (after operations
// that may touch several directories).
func (e *Editor) refreshAllExplorers() {
	for _, b := range e.buffers {
		if e.direds[b.ID] != nil {
			e.refreshExplorer(b)
		}
	}
}

// transferPath moves or copies src to dst, refusing to overwrite.
// transfer is transferPath that also refuses a destination an open
// buffer already uses (its next :w would overwrite the moved file).
func (e *Editor) transfer(src, dst string, copyFiles bool) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	}
	if b := e.findBuffer(dst); b != nil {
		return fmt.Errorf("%s is open in buffer %q: close it first", dst, b.Name)
	}
	return transferPath(src, dst, copyFiles)
}

// within reports whether p is root or inside it.
func within(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+string(filepath.Separator))
}

// realPath resolves symlinks of p, or of its directory when p does not
// exist yet.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if d, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		return filepath.Join(d, filepath.Base(p))
	}
	return p
}

func transferPath(src, dst string, copyFiles bool) error {
	if src == dst {
		return fmt.Errorf("%s: source and destination are the same", src)
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	}
	// a directory cannot go inside itself (symlinks resolved); a symlink
	// is moved or copied as a link, so only its directory is resolved
	rsrc := filepath.Join(realPath(filepath.Dir(src)), filepath.Base(src))
	if st, err := os.Lstat(src); err == nil && st.IsDir() {
		rsrc = realPath(src)
	}
	if within(dst, src) || within(realPath(dst), rsrc) {
		return fmt.Errorf("cannot put %s inside itself", src)
	}
	if !copyFiles {
		err := os.Rename(src, dst)
		if err == nil || !errors.Is(err, syscall.EXDEV) {
			return err
		}
		// another file system: copy (refusing what cannot be copied),
		// then remove the original
		if err := copyTree(src, dst, true); err != nil {
			os.RemoveAll(dst)
			return err
		}
		if err := os.RemoveAll(src); err != nil {
			return &partialMove{err}
		}
		return nil
	}
	return copyTree(src, dst, false)
}

// partialMove: a cross-device move copied everything but could not
// remove all of the source. The destination is complete.
type partialMove struct{ err error }

func (p *partialMove) Error() string {
	return "moved, but the original could not be removed completely: " + p.err.Error()
}

// moveDone reports whether a transfer error still left the files at the
// destination (buffers must follow).
func moveDone(err error) bool {
	var pm *partialMove
	return err == nil || errors.As(err, &pm)
}

// copyTree copies a file, symlink or directory tree, keeping modes
// (directory modes are set last, so read-only directories can be filled).
// Sockets, devices and fifos are skipped, or with strict (a move) refused.
func copyTree(src, dst string, strict bool) error {
	type dirMode struct {
		path string
		mode os.FileMode
	}
	var dirs []dirMode
	err := filepath.WalkDir(src, func(p string, de fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.IsDir():
			dirs = append(dirs, dirMode{target, info.Mode().Perm()})
			return os.Mkdir(target, 0o700)
		case !info.Mode().IsRegular():
			if strict {
				return fmt.Errorf("%s is a special file (fifo, socket or device) and cannot be moved to another file system", p)
			}
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		os.Chmod(dirs[i].path, dirs[i].mode)
	}
	return err
}

// renamePath points buffers of a moved file or directory (and anything
// below it) at the new location.
func (e *Editor) renamePath(src, dst string) {
	rsrc := realPath(filepath.Dir(src))
	rsrc = filepath.Join(rsrc, filepath.Base(src))
	moved := func(p string) (string, bool) {
		for _, from := range []string{src, rsrc} {
			switch {
			case p == from:
				return dst, true
			case strings.HasPrefix(p, from+string(filepath.Separator)):
				return dst + p[len(from):], true
			}
		}
		// the same file reached another way (/home vs /var/home)
		rp := filepath.Join(realPath(filepath.Dir(p)), filepath.Base(p))
		if rp != p && (rp == rsrc || strings.HasPrefix(rp, rsrc+string(filepath.Separator))) {
			return dst + rp[len(rsrc):], true
		}
		return "", false
	}
	for _, b := range e.buffers {
		if dir, ok := e.explorer[b.ID]; ok {
			if np, ok := moved(dir); ok {
				e.explorer[b.ID] = np
				e.direds[b.ID].dir = np
				b.Name = np + "/"
				e.refreshExplorer(b)
			}
			continue
		}
		if np, ok := moved(b.Path); ok {
			b.Path = np
			b.Name = filepath.Base(np)
			e.recordModTime(b)
		}
	}
}

// exploreHere opens the explorer on the current file's directory.
func (e *Editor) exploreHere() {
	dir := e.cwd
	if p := e.win.buf.Path; p != "" {
		dir = filepath.Dir(p)
	}
	b, err := e.openExplorer(dir)
	if err != nil {
		e.errorf("%v", err)
		return
	}
	name := filepath.Base(e.win.buf.Path)
	e.setBuffer(e.win, b)
	e.diredGoto(b, name)
}

func exExplore(e *Editor, a *exArgs) error {
	if a.arg == "" {
		e.exploreHere()
		return nil
	}
	b, err := e.openExplorer(expandHome(a.arg))
	if err != nil {
		return err
	}
	e.setBuffer(e.win, b)
	return nil
}
