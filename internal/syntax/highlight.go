package syntax

import (
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/gdamore/tcell/v2"
)

// Span styles runes [Start, End) of a line.
type Span struct {
	Start, End int
	Style      tcell.Style
	// Literal marks a string or comment (brackets there don't nest)
	Literal bool
}

// extension overrides for files chroma misdetects.
var extOverride = map[string]string{
	".sls": "scheme", ".sps": "scheme", ".sld": "scheme", ".ss": "scheme",
	".ipynb": "python", ".cljs": "clojure", ".cljc": "clojure", ".bb": "clojure",
	".edn": "clojure",
}

// Detect returns the filetype (lowercased lexer name) for a file name,
// using content analysis as a fallback. It returns "" for plain text.
func Detect(name, content string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ft, ok := extOverride[ext]; ok {
		return ft
	}
	l := lexers.Match(filepath.Base(name))
	if l == nil && content != "" {
		if strings.HasPrefix(content, "#!") {
			first := content
			if i := strings.IndexByte(first, '\n'); i >= 0 {
				first = first[:i]
			}
			switch {
			case strings.Contains(first, "python"):
				return "python"
			case strings.Contains(first, "bash"), strings.Contains(first, "sh"):
				return "bash"
			case strings.Contains(first, "scheme"):
				return "scheme"
			}
		}
		l = lexers.Analyse(content)
	}
	if l == nil {
		return ""
	}
	return strings.ToLower(l.Config().Name)
}

// Lexer returns a lexer for a filetype or nil.
func Lexer(ft string) chroma.Lexer {
	if ft == "" {
		return nil
	}
	l := lexers.Get(ft)
	if l == nil {
		return nil
	}
	return chroma.Coalesce(l)
}

// LexerNames returns the number of supported languages (for :languages).
func LexerNames() []string { return lexers.Names(false) }

// Region assigns a filetype to a line range [Start, End).
type Region struct {
	Start, End int
	Filetype   string
}

// HighlightLines tokenises text (lines joined by \n) and returns spans per line.
func HighlightLines(lexer chroma.Lexer, text string, nlines int, theme *Theme) [][]Span {
	out := make([][]Span, nlines)
	if lexer == nil || nlines == 0 {
		return out
	}
	it, err := lexer.Tokenise(nil, text+"\n")
	if err != nil {
		return out
	}
	line, col := 0, 0
	for tok := it(); tok != chroma.EOF; tok = it() {
		st := theme.TokenStyle(tok.Type)
		// strings and comments (InCategory(LiteralString) would match
		// every literal, numbers included; preprocessor lines are code)
		literal := tok.Type.InSubCategory(chroma.LiteralString) ||
			tok.Type.InCategory(chroma.Comment) && !tok.Type.InSubCategory(chroma.CommentPreproc)
		plain := st == theme.Default && !literal
		v := tok.Value
		for len(v) > 0 && line < nlines {
			i := strings.IndexByte(v, '\n')
			seg := v
			if i >= 0 {
				seg = v[:i]
			}
			n := utf8.RuneCountInString(seg)
			if n > 0 && !plain {
				out[line] = append(out[line], Span{Start: col, End: col + n, Style: st, Literal: literal})
			}
			col += n
			if i < 0 {
				break
			}
			v = v[i+1:]
			line++
			col = 0
		}
		if line >= nlines {
			break
		}
	}
	return out
}

// Source supplies the text a Highlighter needs.
type Source interface {
	LineCount() int
	LineString(i int) string
}

// Highlighter caches per-line spans for one buffer.
type Highlighter struct {
	mu       sync.Mutex
	Filetype string
	lines    [][]Span
	version  int
	pending  bool
	theme    *Theme
	regions  func() []Region
	gen      int // bumped by SetTheme/SetFiletype; stale background results are dropped
}

// Version is the buffer version the current spans were computed for
// (-1 before the first result).
func (h *Highlighter) Version() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.version
}

// NewHighlighter creates a highlighter for ft. regions, if non-nil, splits
// the buffer into independently highlighted ranges (notebook cells).
func NewHighlighter(ft string, theme *Theme, regions func() []Region) *Highlighter {
	return &Highlighter{Filetype: ft, theme: theme, version: -1, regions: regions}
}

// SetTheme changes the theme and invalidates the cache.
func (h *Highlighter) SetTheme(t *Theme) {
	h.mu.Lock()
	h.theme = t
	h.version = -1
	h.gen++
	h.mu.Unlock()
}

// SetFiletype changes the language.
func (h *Highlighter) SetFiletype(ft string) {
	h.mu.Lock()
	h.Filetype = ft
	h.version = -1
	h.gen++
	h.mu.Unlock()
}

// Shift keeps cached spans aligned with an edit until re-highlighting.
func (h *Highlighter) Shift(line, removed, added int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if line > len(h.lines) {
		return
	}
	end := line + removed
	if end > len(h.lines) {
		end = len(h.lines)
	}
	repl := make([][]Span, added)
	for i := 0; i < added && line+i < end; i++ {
		repl[i] = h.lines[line+i]
	}
	rest := append([][]Span(nil), h.lines[end:]...)
	h.lines = append(append(h.lines[:line], repl...), rest...)
}

// syncLimit is the size up to which highlighting runs synchronously.
const syncLimit = 400_000

// Spans returns cached spans for line i, refreshing the cache if the
// buffer version changed. Large buffers are refreshed in the background;
// notify is called (from another goroutine) when new results are ready.
func (h *Highlighter) Spans(src Source, version, i int, notify func()) []Span {
	h.mu.Lock()
	if h.version != version && !h.pending {
		n := src.LineCount()
		texts := make([]string, n)
		size := 0
		for j := 0; j < n; j++ {
			texts[j] = src.LineString(j)
			size += len(texts[j]) + 1
		}
		var regions []Region
		if h.regions != nil {
			regions = h.regions()
		}
		ft, theme := h.Filetype, h.theme
		if size <= syncLimit || notify == nil {
			h.lines = compute(ft, theme, texts, regions)
			h.version = version
		} else {
			h.pending = true
			gen := h.gen
			go func() {
				defer func() {
					if recover() != nil {
						h.mu.Lock()
						h.pending = false
						h.mu.Unlock()
					}
				}()
				res := compute(ft, theme, texts, regions)
				h.mu.Lock()
				if gen == h.gen {
					h.lines = res
					h.version = version
				}
				h.pending = false
				h.mu.Unlock()
				notify()
			}()
		}
	}
	defer h.mu.Unlock()
	if i < 0 || i >= len(h.lines) {
		return nil
	}
	return h.lines[i]
}

func compute(ft string, theme *Theme, texts []string, regions []Region) [][]Span {
	if len(regions) == 0 {
		return HighlightLines(Lexer(ft), strings.Join(texts, "\n"), len(texts), theme)
	}
	out := make([][]Span, len(texts))
	for _, r := range regions {
		if r.Start < 0 || r.End > len(texts) || r.Start >= r.End {
			continue
		}
		res := HighlightLines(Lexer(r.Filetype), strings.Join(texts[r.Start:r.End], "\n"), r.End-r.Start, theme)
		copy(out[r.Start:r.End], res)
	}
	return out
}
