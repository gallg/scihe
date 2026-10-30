package editor

import (
	"errors"

	"scihe/internal/buffer"
)

// mdRender is a read-only view of a markdown buffer with its formulas
// rendered (see mdmath.go). It takes the place of the source in the
// window, line for line, until rendering is switched off.
type mdRender struct {
	src     *buffer.Buffer
	buf     *buffer.Buffer
	version int
}

func isMarkdown(b *buffer.Buffer) bool {
	return b.Filetype == "markdown"
}

// exMdRender toggles the rendered view in the current window.
func exMdRender(e *Editor, a *exArgs) error {
	w := e.win
	if r := e.renders[w.buf.ID]; r != nil {
		e.leaveRender(r)
		return nil
	}
	src := w.buf
	if !isMarkdown(src) {
		return errors.New("not a markdown buffer")
	}
	var r *mdRender
	for _, x := range e.renders {
		if x.src == src {
			r = x // already shown in another window
		}
	}
	if r == nil {
		rb := buffer.New("render: " + src.Name)
		rb.Kind = buffer.KindPreview
		rb.Filetype = src.Filetype
		rb.ReadOnly = true
		e.addBuffer(rb)
		r = &mdRender{src: src, buf: rb, version: -1}
		e.renders[rb.ID] = r
		e.refreshRender(r)
	}
	cur, alt := w.cur, w.alt
	e.setBuffer(w, r.buf)
	w.alt = alt
	e.setCursor(w, r.buf.Clamp(cur))
	e.infof("formulas rendered (read-only): <leader>mr to edit")
	return nil
}

// leaveRender puts the source back in every window showing the view.
func (e *Editor) leaveRender(r *mdRender) {
	e.forWindows(func(w *Window) {
		if w.buf == r.buf {
			cur, alt := w.cur, w.alt
			e.setBuffer(w, r.src)
			w.alt = alt
			e.setCursor(w, r.src.Clamp(cur))
		}
	})
	e.dropRender(r)
}

func (e *Editor) dropRender(r *mdRender) {
	delete(e.renders, r.buf.ID)
	if e.bufferAlive(r.buf) {
		e.deleteBuffer(r.buf, true)
	}
}

// updateRenders follows changes of the sources (reloads from disk).
func (e *Editor) updateRenders() {
	for _, r := range e.renders {
		if !e.bufferAlive(r.src) {
			e.dropRender(r)
		} else if r.version != r.src.Version {
			e.refreshRender(r)
		}
	}
}

func (e *Editor) refreshRender(r *mdRender) {
	r.buf.SetText(renderMath(r.src.Text()))
	r.version = r.src.Version
	// the source may have shrunk (a reload)
	e.forWindows(func(w *Window) {
		if w.buf == r.buf {
			e.clampCursor(w)
		}
	})
}
