package editor

import (
	"errors"
	"os"
	"time"

	"scihe/internal/buffer"
)

// Auto-reload: about once a second the files of open buffers are
// compared with the time they were last read or written. A buffer with
// 'autoreload' (off by default), or a markdown buffer while its preview
// is open, is reloaded when it has no unsaved changes;
// otherwise the change is reported once per new version of the file and
// the buffer is left alone. Notebooks are never reloaded automatically.

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
		if b.Kind != buffer.KindFile || b.Path == "" {
			continue // notebooks, listings, logs, previews
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
