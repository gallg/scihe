package editor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"scihe/internal/buffer"
	"scihe/internal/keymap"
	"scihe/internal/syntax"
)

// The buffer list (SPC b l, :ls) works like Emacs ibuffer / dired on
// buffers: open, mark, close, rename, copy and save them, and share
// their files through the explorer's file clipboard. Line 0 is a
// header; buffer i of the listing is on line i+1.

type bufList struct {
	ids   []int        // listed buffers, in order
	marks map[int]rune // buffer id -> '*' marked, 'D' flagged
}

const bufListHelp = "<CR> open  o split  m/u/U/t marks  d flag  x close flagged  D close  R rename  C copy  S save  y/X file clipboard  p open clipboard  g refresh"

const bufListNameCol = 13 // rune column where names start

// exLs opens the buffer list in the current window.
func exLs(e *Editor, a *exArgs) error {
	b := e.bufListBuffer()
	cur := e.win.buf
	e.refreshBufList(b)
	e.setBuffer(e.win, b)
	e.bufListGoto(b, cur.ID)
	return nil
}

// bufListBuffer returns (creating) the *buffers* listing.
func (e *Editor) bufListBuffer() *buffer.Buffer {
	for _, b := range e.buffers {
		if e.buflists[b.ID] != nil {
			return b
		}
	}
	b := buffer.New("*buffers*")
	b.Kind = buffer.KindScratch
	b.ReadOnly = true
	e.addBuffer(b)
	e.buflists[b.ID] = &bufList{marks: map[int]rune{}}
	e.localMaps[b.ID] = e.bufListKeys()
	return b
}

func (e *Editor) bufListKeys() *keymap.Map[*Cmd] {
	m := keymap.NewMap[*Cmd]()
	bind := func(k, label string, f func(c *ctx, bl *bufList)) {
		m.Bind(keymap.Parse(k, e.leader), label, action(func(c *ctx) {
			if bl := e.buflists[c.b.ID]; bl != nil {
				f(c, bl)
			}
		}))
	}
	bind("<CR>", "open buffer", func(c *ctx, bl *bufList) { e.bufListOpen(c, bl, false) })
	bind("o", "open in a split", func(c *ctx, bl *bufList) { e.bufListOpen(c, bl, true) })
	bind("g", "refresh", func(c *ctx, bl *bufList) { e.refreshBufList(c.b) })
	bind("?", "show keys", func(c *ctx, bl *bufList) { e.infof("%s", bufListHelp) })
	mark := func(m rune) func(c *ctx, bl *bufList) {
		return func(c *ctx, bl *bufList) {
			l := c.w.cur.Line
			for i := 0; i < c.count; i++ {
				id, ok := bl.idAt(l)
				if !ok {
					break
				}
				if m == 0 {
					delete(bl.marks, id)
				} else {
					bl.marks[id] = m
				}
				l++
			}
			e.refreshBufList(c.b)
			e.setCursor(c.w, Pos{Line: min(l, c.b.LineCount()-1), Col: bufListNameCol})
		}
	}
	bind("m", "mark", mark('*'))
	bind("d", "flag for closing", mark('D'))
	bind("u", "unmark", mark(0))
	bind("U", "unmark all", func(c *ctx, bl *bufList) {
		bl.marks = map[int]rune{}
		e.refreshBufList(c.b)
	})
	bind("t", "toggle marks", func(c *ctx, bl *bufList) {
		for _, id := range bl.ids {
			switch bl.marks[id] {
			case '*':
				delete(bl.marks, id)
			case 0:
				bl.marks[id] = '*'
			}
		}
		e.refreshBufList(c.b)
	})
	bind("x", "close flagged buffers", func(c *ctx, bl *bufList) { e.bufListClose(c.b, e.bufListSel(bl, 'D', -1)) })
	bind("D", "close marked buffers / this one", func(c *ctx, bl *bufList) {
		e.bufListClose(c.b, e.bufListSel(bl, '*', c.w.cur.Line))
	})
	bind("R", "rename (move the file)", func(c *ctx, bl *bufList) {
		e.bufListRename(c.b, e.bufListSel(bl, '*', c.w.cur.Line))
	})
	bind("C", "copy to a new file", func(c *ctx, bl *bufList) {
		e.bufListCopy(c.b, e.bufListSel(bl, '*', c.w.cur.Line))
	})
	bind("S", "save marked buffers / this one", func(c *ctx, bl *bufList) {
		e.bufListSave(c.b, e.bufListSel(bl, '*', c.w.cur.Line))
	})
	clip := func(cut bool) func(c *ctx, bl *bufList) {
		return func(c *ctx, bl *bufList) {
			var paths []string
			skipped := 0
			for _, b := range e.bufListSel(bl, '*', c.w.cur.Line) {
				if b.Path == "" || b.Kind != buffer.KindFile && b.Kind != buffer.KindNotebook {
					skipped++
					continue
				}
				paths = append(paths, b.Path)
				delete(bl.marks, b.ID)
			}
			if len(paths) == 0 {
				e.warnf("no file buffers selected (buffers without a file cannot go in the file clipboard)")
				return
			}
			e.fileClip = fileClipboard{paths: paths, cut: cut}
			e.refreshAllExplorers()
			e.refreshBufList(c.b)
			verb := map[bool]string{true: "cut", false: "copied"}[cut]
			msg := fmt.Sprintf("%s %s to the file clipboard — p in a directory listing pastes", verb, describeNames(paths))
			if skipped > 0 {
				msg += fmt.Sprintf(" (%d without a file skipped)", skipped)
			}
			e.infof("%s", msg)
		}
	}
	bind("y", "copy files to the file clipboard", clip(false))
	bind("X", "cut files to the file clipboard", clip(true))
	bind("p", "open the clipboard's files", func(c *ctx, bl *bufList) {
		if len(e.fileClip.paths) == 0 {
			e.infof("file clipboard is empty (y copies, X cuts)")
			return
		}
		n := 0
		for _, p := range e.fileClip.paths {
			if _, err := e.loadBuffer(p); err != nil {
				e.errorf("%v", err)
				continue
			}
			n++
		}
		e.refreshBufList(c.b)
		if n > 0 && e.msgStyle != 1 {
			e.infof("opened %d buffer(s) from the file clipboard", n)
		}
	})
	return m
}

func (bl *bufList) idAt(line int) (int, bool) {
	if i := line - 1; i >= 0 && i < len(bl.ids) {
		return bl.ids[i], true
	}
	return 0, false
}

// bufListSel returns the buffers with mark m, else (line >= 0) the one
// on that line.
func (e *Editor) bufListSel(bl *bufList, m rune, line int) []*buffer.Buffer {
	var out []*buffer.Buffer
	for _, id := range bl.ids {
		if bl.marks[id] == m {
			if b := e.bufferByID(id); b != nil {
				out = append(out, b)
			}
		}
	}
	if len(out) == 0 && line >= 0 {
		if id, ok := bl.idAt(line); ok {
			if b := e.bufferByID(id); b != nil {
				out = []*buffer.Buffer{b}
			}
		}
	}
	return out
}

func (e *Editor) bufferByID(id int) *buffer.Buffer {
	for _, b := range e.buffers {
		if b.ID == id {
			return b
		}
	}
	return nil
}

// bufKind describes a buffer for the listing.
func (e *Editor) bufKind(b *buffer.Buffer) string {
	switch {
	case e.direds[b.ID] != nil:
		return "directory"
	case e.tables[b.ID] != nil:
		return "table view"
	case b.Kind == buffer.KindNotebook:
		return "notebook"
	case b.Kind == buffer.KindPreview:
		return "preview"
	case b.Kind == buffer.KindRepl:
		return "log"
	case b.Kind == buffer.KindScratch:
		return "scratch"
	case b.Filetype != "":
		return b.Filetype
	}
	return "text"
}

// renameable reports whether a buffer is backed (or can be backed) by a
// file of its own.
func renameable(b *buffer.Buffer) bool {
	return b.Kind == buffer.KindFile || b.Kind == buffer.KindNotebook
}

// refreshBufList rebuilds the listing (only touching the text when it
// changed) and keeps each window's cursor on the same buffer.
func (e *Editor) refreshBufList(lb *buffer.Buffer) {
	bl := e.buflists[lb.ID]
	if bl == nil {
		return
	}
	keep := map[*Window]int{}
	var from *buffer.Buffer // the buffer the list was opened from (%)
	e.forWindows(func(w *Window) {
		if w.buf == lb {
			if id, ok := bl.idAt(w.cur.Line); ok {
				keep[w] = id
			}
			if from == nil {
				from = w.alt
			}
		}
	})
	type row struct {
		id                            int
		mark, flags, name, kind, file string
		lines                         int
	}
	var rows []row
	bl.ids = bl.ids[:0]
	for _, b := range e.listedBuffers() {
		if b == lb {
			continue
		}
		bl.ids = append(bl.ids, b.ID)
		mark := " "
		if m := bl.marks[b.ID]; m != 0 {
			mark = string(m)
		}
		flags := ""
		if b == from {
			flags = "%"
		}
		if e.modified(b) {
			flags += "+"
		}
		if b.ReadOnly {
			flags += "RO"
		}
		rows = append(rows, row{b.ID, mark, flags, cellText(b.Name), e.bufKind(b), e.displayFile(b), b.LineCount()})
	}
	for id := range bl.marks {
		if e.bufferByID(id) == nil {
			delete(bl.marks, id)
		}
	}
	nameW, kindW := 4, 4
	for _, r := range rows {
		nameW = max(nameW, textWidth(r.name))
		kindW = max(kindW, textWidth(r.kind))
	}
	pad := func(s string, w int) string { return s + strings.Repeat(" ", max(w-textWidth(s), 0)) }
	th := e.theme
	// "M  ID  flags " is bufListNameCol wide
	header := fmt.Sprintf("%-*s", bufListNameCol, "   ID  flags") + pad("name", nameW) + "   lines  " + pad("type", kindW) + "  file"
	lines := []string{header}
	spans := [][]syntax.Span{{{Start: 0, End: len([]rune(header)), Style: th.CurLineNr}}}
	marked, flagged := 0, 0
	for _, r := range rows {
		l := fmt.Sprintf("%s %3d  %-5s ", r.mark, r.id, r.flags) + pad(r.name, nameW) +
			fmt.Sprintf("  %6d  ", r.lines) + pad(r.kind, kindW) + "  " + r.file
		lines = append(lines, l)
		end := len([]rune(l))
		nameEnd := bufListNameCol + len([]rune(pad(r.name, nameW)))
		sp := []syntax.Span{
			{Start: 2, End: bufListNameCol, Style: th.Dim},
			{Start: nameEnd, End: end, Style: th.Dim},
		}
		switch r.mark {
		case "*":
			marked++
			sp = append(sp, syntax.Span{Start: 0, End: end, Style: th.Info.Bold(true)})
		case "D":
			flagged++
			sp = append(sp, syntax.Span{Start: 0, End: end, Style: th.Error.Bold(true)})
		}
		spans = append(spans, sp)
	}
	if marked+flagged > 0 {
		lines[0] += fmt.Sprintf("   [%d marked, %d flagged]", marked, flagged)
	}
	if n := len(e.fileClip.paths); n > 0 {
		lines[0] += fmt.Sprintf("   [file clipboard: %d]", n)
	}
	spans[0][0].End = len([]rune(lines[0]))
	if text := strings.Join(lines, "\n"); text != lb.Text() {
		lb.SetText(text)
	}
	e.static[lb.ID] = spans
	e.forWindows(func(w *Window) {
		if w.buf != lb {
			return
		}
		l := min(max(w.cur.Line, 1), lb.LineCount()-1)
		if id, ok := keep[w]; ok {
			for i, x := range bl.ids {
				if x == id {
					l = i + 1
				}
			}
		}
		w.cur = Pos{Line: l, Col: bufListNameCol}
		e.clampCursor(w)
	})
}

// updateBufLists keeps visible buffer lists current (buffers opened,
// closed, modified or renamed elsewhere).
func (e *Editor) updateBufLists() {
	for id := range e.buflists {
		lb := e.bufferByID(id)
		if lb == nil {
			delete(e.buflists, id)
			continue
		}
		visible := false
		e.forWindows(func(w *Window) { visible = visible || w.buf == lb })
		if visible {
			e.refreshBufList(lb)
		}
	}
}

// displayFile is the file of a buffer as shown in the list.
func (e *Editor) displayFile(b *buffer.Buffer) string {
	if b.Path == "" {
		if d, ok := e.explorer[b.ID]; ok {
			return e.displayName(&buffer.Buffer{Path: d}) + "/"
		}
		return ""
	}
	return e.displayName(b)
}

func (e *Editor) bufListGoto(lb *buffer.Buffer, id int) {
	bl := e.buflists[lb.ID]
	for i, x := range bl.ids {
		if x == id {
			e.forWindows(func(w *Window) {
				if w.buf == lb {
					e.setCursor(w, Pos{Line: i + 1, Col: bufListNameCol})
				}
			})
		}
	}
}

func (e *Editor) bufListOpen(c *ctx, bl *bufList, split bool) {
	id, ok := bl.idAt(c.w.cur.Line)
	if !ok {
		return
	}
	b := e.bufferByID(id)
	if b == nil {
		return
	}
	if split {
		e.split(b, true)
		return
	}
	e.setBuffer(c.w, b)
}

// bufListClose closes buffers; modified ones are only discarded after a
// confirmation.
func (e *Editor) bufListClose(lb *buffer.Buffer, bufs []*buffer.Buffer) {
	if len(bufs) == 0 {
		e.infof("nothing to close (flag with d, then x; or D on a buffer)")
		return
	}
	var modified []string
	for _, b := range bufs {
		if e.modified(b) && (b.Kind == buffer.KindFile || b.Kind == buffer.KindNotebook) {
			modified = append(modified, b.Name)
		}
	}
	close := func() {
		bl := e.buflists[lb.ID]
		n := 0
		for _, b := range bufs {
			if !e.bufferAlive(b) {
				continue
			}
			if err := e.deleteBuffer(b, true); err != nil {
				e.errorf("%v", err)
				continue
			}
			delete(bl.marks, b.ID)
			n++
		}
		e.refreshBufList(lb)
		if n > 0 && e.msgStyle != 1 {
			e.infof("closed %d buffer(s)", n)
		}
	}
	if len(modified) == 0 {
		close()
		return
	}
	e.prompt(fmt.Sprintf("Discard unsaved changes to %s? (y/N) ", strings.Join(modified, ", ")), "", func(s string, ok bool) {
		if confirmed(s, ok) {
			close()
		} else {
			e.infof("nothing closed")
		}
	})
}

// bufListRename moves the files of buffers (a nameless buffer is saved
// under the new name); the buffers follow.
func (e *Editor) bufListRename(lb *buffer.Buffer, bufs []*buffer.Buffer) {
	for _, b := range bufs {
		if !renameable(b) {
			e.errorf("%s is not a file buffer", b.Name)
			return
		}
	}
	if len(bufs) == 0 {
		return
	}
	initial := e.cwd + string(filepath.Separator)
	if len(bufs) == 1 && bufs[0].Path != "" {
		initial = bufs[0].Path
	}
	e.prompt("Rename "+describeBufs(bufs)+" to: ", initial, func(s string, ok bool) {
		if !ok || strings.TrimSpace(s) == "" {
			return
		}
		dest := resolveIn(e.cwd, s)
		destIsDir := false
		if st, err := os.Stat(dest); err == nil && st.IsDir() {
			destIsDir = true
		}
		if len(bufs) > 1 && !destIsDir {
			e.errorf("%s is not a directory", dest)
			return
		}
		done := 0
		for _, b := range bufs {
			dst := dest
			if destIsDir {
				name := filepath.Base(b.Path)
				if b.Path == "" {
					name = b.Name
				}
				dst = filepath.Join(dest, name)
			}
			if err := e.renameBuffer(b, dst); err != nil {
				e.errorf("%s: %v", b.Name, err)
				continue
			}
			done++
		}
		e.refreshAllExplorers()
		e.refreshBufList(lb)
		if done > 0 && e.msgStyle != 1 {
			e.infof("renamed %d buffer(s)", done)
		}
	})
}

func describeBufs(bufs []*buffer.Buffer) string {
	names := make([]string, len(bufs))
	for i, b := range bufs {
		names[i] = b.Name
	}
	return describeNames(names)
}

// renameBuffer gives b the file dst: an existing file is moved on disk
// (unsaved changes stay in the buffer), a nameless buffer is saved there.
func (e *Editor) renameBuffer(b *buffer.Buffer, dst string) error {
	if other := e.findBuffer(dst); other != nil && other != b {
		return errors.New("another buffer has that file")
	}
	if b.Path == "" {
		if _, err := os.Lstat(dst); err == nil {
			return fmt.Errorf("%s already exists", dst)
		}
		return e.writeBuffer(b, dst, false)
	}
	if _, err := os.Stat(b.Path); err != nil {
		// never saved: just point the buffer at the new name
		if _, err := os.Lstat(dst); err == nil {
			return fmt.Errorf("%s already exists", dst)
		}
		b.Path, b.Name = dst, filepath.Base(dst)
		return nil
	}
	if err := e.transfer(b.Path, dst, false); !moveDone(err) {
		return err
	} else if err != nil {
		e.warnf("%v", err)
	}
	e.renamePath(b.Path, dst)
	if e.modified(b) {
		e.warnf("%s moved; its unsaved changes are still only in the buffer (:w saves them)", b.Name)
	}
	return nil
}

// bufListCopy writes the current content of buffers (saved or not) to
// new files and opens them.
func (e *Editor) bufListCopy(lb *buffer.Buffer, bufs []*buffer.Buffer) {
	for _, b := range bufs {
		if !renameable(b) {
			e.errorf("%s is not a file buffer", b.Name)
			return
		}
	}
	if len(bufs) == 0 {
		return
	}
	initial := e.cwd + string(filepath.Separator)
	if len(bufs) == 1 && bufs[0].Path != "" {
		initial = bufs[0].Path
	}
	e.prompt("Copy "+describeBufs(bufs)+" to: ", initial, func(s string, ok bool) {
		if !ok || strings.TrimSpace(s) == "" {
			return
		}
		dest := resolveIn(e.cwd, s)
		destIsDir := false
		if st, err := os.Stat(dest); err == nil && st.IsDir() {
			destIsDir = true
		}
		if len(bufs) > 1 && !destIsDir {
			e.errorf("%s is not a directory", dest)
			return
		}
		done := 0
		for _, b := range bufs {
			dst := dest
			if destIsDir {
				name := filepath.Base(b.Path)
				if b.Path == "" {
					name = b.Name
				}
				dst = filepath.Join(dest, name)
			}
			if err := e.copyBufferTo(b, dst); err != nil {
				e.errorf("%s: %v", b.Name, err)
				continue
			}
			if _, err := e.loadBuffer(dst); err != nil {
				e.errorf("%v", err)
				continue
			}
			done++
		}
		e.refreshAllExplorers()
		e.refreshBufList(lb)
		if done > 0 && e.msgStyle != 1 {
			e.infof("copied %d buffer(s)", done)
		}
	})
}

// copyBufferTo writes the buffer's content to a new file dst.
func (e *Editor) copyBufferTo(b *buffer.Buffer, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	}
	if k := e.kernels[b.ID]; k != nil && k.nb != nil {
		k.flushStreams()
		e.syncNotebook(k)
		data, err := k.nb.Marshal()
		if err != nil {
			return err
		}
		return b.WriteFile(dst, data)
	}
	return b.SaveCopy(dst)
}

func (e *Editor) bufListSave(lb *buffer.Buffer, bufs []*buffer.Buffer) {
	n := 0
	for _, b := range bufs {
		if !renameable(b) || b.Path == "" {
			e.warnf("%s has no file: R gives it a name", b.Name)
			continue
		}
		if !e.modified(b) {
			continue
		}
		if err := e.writeBuffer(b, "", false); err != nil {
			e.errorf("%s: %v", b.Name, err)
			continue
		}
		n++
	}
	e.refreshBufList(lb)
	if e.msgStyle == 0 {
		e.infof("saved %d buffer(s)", n)
	}
}
