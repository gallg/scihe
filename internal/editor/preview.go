package editor

import (
	"errors"

	"scihe/internal/buffer"
	"scihe/internal/markdown"
	"scihe/internal/syntax"
)

// preview is a live markdown rendering of a source buffer.
type preview struct {
	src     *buffer.Buffer
	buf     *buffer.Buffer
	version int
	width   int
	theme   *syntax.Theme
	doc     *markdown.Doc
	lastSrc int // source cursor line last synced
}

func isMarkdown(b *buffer.Buffer) bool {
	return b.Filetype == "markdown"
}

func exPreview(e *Editor, a *exArgs) error {
	b := e.win.buf
	// toggling from the preview window closes it
	for src, p := range e.previews {
		if p.buf == b {
			e.closePreview(src)
			return nil
		}
	}
	if p := e.previews[b.ID]; p != nil {
		visible := false
		for _, w := range e.windows() {
			visible = visible || w.buf == p.buf
		}
		e.closePreview(b.ID)
		if visible {
			return nil
		}
		// the preview window had been closed (:only, <C-w>c): reopen
	}
	if !isMarkdown(b) {
		return errors.New("not a markdown buffer")
	}
	pb := buffer.New("preview: " + b.Name)
	pb.Kind = buffer.KindPreview
	pb.ReadOnly = true
	e.addBuffer(pb)
	p := &preview{src: b, buf: pb, version: -1, lastSrc: -1}
	e.previews[b.ID] = p
	src := e.win
	e.splitAt(pb, true, true)
	e.focus(src)
	return nil
}

func (e *Editor) closePreview(srcID int) {
	p := e.previews[srcID]
	if p == nil {
		return
	}
	delete(e.previews, srcID)
	for _, w := range e.windows() {
		if w.buf == p.buf {
			if !e.closeWindow(w) {
				e.setBuffer(w, p.src)
			}
		}
	}
	e.removeBuffer(p.buf)
}

// updatePreviews re-renders previews whose source changed and keeps
// their scroll position in sync with the source cursor.
func (e *Editor) updatePreviews() {
	if len(e.previews) == 0 {
		return
	}
	for id, p := range e.previews {
		if !e.bufferAlive(p.src) {
			e.closePreview(id)
			continue
		}
		var pw *Window
		for _, w := range e.windows() {
			if w.buf == p.buf {
				pw = w
			}
		}
		width := 80
		if pw != nil && pw.w > 0 {
			width = pw.w - e.gutterWidth(pw) - 1
		}
		if p.version != p.src.Version || p.width != width || p.theme != e.theme {
			p.doc = markdown.Render([]byte(p.src.Text()), width, e.theme)
			p.buf.SetText(p.doc.String())
			spans := make([][]syntax.Span, len(p.doc.Lines))
			for i, l := range p.doc.Lines {
				spans[i] = l.Spans
			}
			e.static[p.buf.ID] = spans
			e.opts[p.buf.ID] = previewOptions(e.options(p.src))
			p.version, p.width, p.theme = p.src.Version, width, e.theme
			p.lastSrc = -1
		}
		// follow the source cursor when the source window is focused
		if pw != nil && e.win.buf == p.src && e.win.cur.Line != p.lastSrc {
			p.lastSrc = e.win.cur.Line
			target := p.doc.RenderedLineFor(e.win.cur.Line)
			pw.cur = Pos{target, 0}
			pw.top = max(0, target-pw.textHeight()/3)
			pw.pinTop = true
		}
	}
}

// previewOptions are the display options of a preview: the markdown
// renderer already lays everything out for the window width (it always
// wraps), so the window itself never soft-wraps, whatever the source
// buffer's 'wrap' is.
func previewOptions(src *configOptions) *configOptions {
	o := *src
	o.Number = false
	o.RelativeNumber = false
	o.CursorLine = false
	o.Wrap = false
	return &o
}
