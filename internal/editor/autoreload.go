package editor

import (
	"errors"
	"os"
	"time"

	"scihe/internal/buffer"
)

// Auto-reload: about once a second the files of open buffers (files and
// notebooks) are compared with the time they were last read or written.
// A buffer with 'autoreload' (off by default), or a markdown buffer while
// its preview is open, is reloaded when it has no unsaved changes (and,
// for a notebook, no cell running or queued); otherwise the change is
// reported once per new version of the file and the buffer is left alone.

const diskCheckEvery = time.Second

// diskState remembers what was already reported for a buffer.
type diskState struct {
	warned  time.Time // modification time last reported
	missing bool      // "file deleted" was reported
}

// checkDisk runs from tick; it reports whether anything changed.
func (e *Editor) checkDisk() bool {
	if time.Since(e.lastDiskCheck) < diskCheckEvery {
		return false
	}
	e.lastDiskCheck = time.Now()
	changed := false
	for _, b := range e.buffers {
		if b.Kind != buffer.KindFile && b.Kind != buffer.KindNotebook || b.Path == "" {
			continue // listings, logs, previews
		}
		known, ok := e.modTimes[b.ID]
		if !ok {
			continue // never read from or written to disk
		}
		st := e.disk[b.ID]
		info, err := os.Stat(b.Path)
		if errors.Is(err, os.ErrNotExist) {
			if !st.missing {
				st.missing = true
				e.disk[b.ID] = st
				e.warnf("%s was deleted on disk; the buffer is kept (:w writes it again)", b.Name)
				changed = true
			}
			continue
		}
		if err != nil {
			continue
		}
		st.missing = false
		mt := info.ModTime()
		// any different time counts (cp -p and rsync -t keep older ones)
		if mt.Equal(known) || mt.Equal(st.warned) {
			e.disk[b.ID] = st
			continue
		}
		// never under an edit in progress: insert / visual mode or an
		// open undo group; the next check retries
		if b == e.win.buf && e.mode != ModeNormal || b.InUndoGroup() {
			continue
		}
		// nor while notebook cells run: their outputs go to the old cells
		if ks := e.kernels[b.ID]; ks != nil && (ks.running != nil || ks.starting || len(ks.queue) > 0) {
			continue
		}
		switch {
		case !e.options(b).Autoreload && !e.previewShown(b):
			st.warned = mt
			e.infof("%s changed on disk (SPC f y reloads)", b.Name)
		case e.modified(b):
			st.warned = mt
			e.warnf("%s changed on disk; your unsaved changes are kept (:e! discards them)", b.Name)
		default:
			if err := e.reload(b); err != nil {
				st.warned = mt
				e.errorf("%s: %v", b.Name, err)
			} else {
				e.infof("%s reloaded (changed on disk)", b.Name)
			}
		}
		e.disk[b.ID] = st
		changed = true
	}
	return changed
}

// previewShown reports a markdown preview of b visible in a window.
func (e *Editor) previewShown(b *buffer.Buffer) bool {
	p := e.previews[b.ID]
	if p == nil {
		return false
	}
	shown := false
	e.forWindows(func(w *Window) { shown = shown || w.buf == p.buf })
	return shown
}

// autoreloadUnsupported says why 'autoreload' can't work in b ("" when
// it can). Messages stay short to fit the message line.
func (e *Editor) autoreloadUnsupported(b *buffer.Buffer) string {
	for _, p := range e.previews {
		if p.buf == b {
			return "auto-reload: the preview follows its file"
		}
	}
	if tv := e.tableFor(b); tv != nil {
		if tv.src.Kind == buffer.KindFile {
			return "" // set on its file buffer
		}
		return "auto-reload: not available in this table"
	}
	what := ""
	switch {
	case b.Kind == buffer.KindFile || b.Kind == buffer.KindNotebook:
		return ""
	case e.gitViews[b.ID] != nil || b.Data["git"] == true:
		what = "git buffers"
	case e.direds[b.ID] != nil:
		what = "the file explorer"
	case e.buflists[b.ID] != nil:
		what = "the buffer list"
	case b.Kind == buffer.KindRepl:
		what = "logs"
	default:
		what = "output buffers"
	}
	return "auto-reload: not available in " + what
}
