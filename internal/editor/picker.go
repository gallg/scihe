package editor

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"scihe/internal/buffer"
	"scihe/internal/syntax"
)

// pickItem is one entry of a picker.
type pickItem struct {
	label  string
	detail string
	data   any
	score  int
	match  []int // rune indices of matched chars in label
}

// picker is a fuzzy-filter overlay.
type picker struct {
	title    string
	input    []rune
	all      []pickItem
	shown    []pickItem
	sel      int
	scroll   int
	onSelect func(pickItem)
	// dynamic sources are re-queried when the input changes (grep)
	dynamic func(query string, gen int64)
	gen     atomic.Int64
	loading bool
}

func (e *Editor) openPicker(p *picker) {
	if e.mode.visual() {
		e.exitVisual()
	}
	e.resetPending()
	e.picker = p
	p.refresh(e)
}

func (p *picker) refresh(e *Editor) {
	if p.dynamic != nil {
		gen := p.gen.Add(1)
		p.loading = true
		p.dynamic(string(p.input), gen)
		return
	}
	p.filter()
}

func (p *picker) filter() {
	q := string(p.input)
	p.shown = p.shown[:0]
	for _, it := range p.all {
		if q == "" {
			it.score, it.match = 0, nil
			p.shown = append(p.shown, it)
			continue
		}
		if s, m, ok := fuzzyScore(it.label, q); ok {
			it.score, it.match = s, m
			p.shown = append(p.shown, it)
		}
	}
	if q != "" {
		sort.SliceStable(p.shown, func(i, j int) bool { return p.shown[i].score > p.shown[j].score })
	}
	if len(p.shown) > 5000 {
		p.shown = p.shown[:5000]
	}
	p.sel, p.scroll = 0, 0
}

// fuzzyScore matches query as a subsequence of s (smart case). Spaces in
// the query are ignored. Matches at word boundaries, consecutive matches
// and matches in the file-name part score higher; every start position of
// the first query rune is tried and the best alignment wins.
func fuzzyScore(s, query string) (int, []int, bool) {
	query = strings.ReplaceAll(query, " ", "")
	ignore := strings.ToLower(query) == query
	orig := []rune(s)
	srs := []rune(s)
	qrs := []rune(query)
	if len(qrs) == 0 {
		return 0, nil, true
	}
	if ignore {
		for i := range srs {
			srs[i] = unicode.ToLower(srs[i])
		}
	}
	baseRune := utf8.RuneCountInString(s[:strings.LastIndexByte(s, '/')+1])
	best, bestMatch, found := 0, []int(nil), false
	for start := 0; start < len(srs); start++ {
		if srs[start] != qrs[0] {
			continue
		}
		var match []int
		score, qi, prev := 0, 0, -2
		for i := start; i < len(srs) && qi < len(qrs); i++ {
			if srs[i] != qrs[qi] {
				continue
			}
			match = append(match, i)
			score += 10
			if i == prev+1 {
				score += 15
			}
			if i == 0 || strings.ContainsRune("/_-. :", orig[i-1]) || (unicode.IsUpper(orig[i]) && unicode.IsLower(orig[i-1])) {
				score += 10
			}
			if i >= baseRune {
				score += 5
			}
			prev = i
			qi++
		}
		if qi < len(qrs) {
			break // later starts cannot match either
		}
		score -= len(srs) / 8
		if !found || score > best {
			best, bestMatch, found = score, match, true
		}
	}
	return best, bestMatch, found
}

func (e *Editor) pickerKey(k Key) {
	p := e.picker
	switch k {
	case "<Esc>", "<C-c>":
		e.picker = nil
	case "<CR>":
		e.picker = nil
		if p.sel < len(p.shown) && p.onSelect != nil {
			p.onSelect(p.shown[p.sel])
		}
	case "<Down>", "<C-n>", "<C-j>", "<Tab>":
		if p.sel < len(p.shown)-1 {
			p.sel++
		}
	case "<Up>", "<C-p>", "<C-k>", "<S-Tab>":
		if p.sel > 0 {
			p.sel--
		}
	case "<PageDown>", "<C-d>":
		p.sel = min(p.sel+10, max(len(p.shown)-1, 0))
	case "<PageUp>", "<C-u>":
		p.sel = max(p.sel-10, 0)
	case "<BS>":
		if len(p.input) > 0 {
			p.input = p.input[:len(p.input)-1]
			p.refresh(e)
		}
	case "<C-w>":
		i := len(p.input)
		for i > 0 && p.input[i-1] == ' ' {
			i--
		}
		for i > 0 && p.input[i-1] != ' ' && p.input[i-1] != '/' {
			i--
		}
		p.input = p.input[:i]
		p.refresh(e)
	default:
		if r, ok := k.Rune(); ok && r != '\t' {
			p.input = append(p.input, r)
			p.refresh(e)
		}
	}
}

// ---------------------------------------------------------------- sources

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".venv": true, "venv": true, "__pycache__": true,
	".mypy_cache": true, ".pytest_cache": true, ".ruff_cache": true, "target": true,
	".cpcache": true, ".clj-kondo": true, ".lsp": true, ".ipynb_checkpoints": true, ".tox": true,
	".idea": true, ".vscode": true,
}

func walkFiles(root string, limit int, fn func(rel string) bool) {
	n := 0
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".")) {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "pyvenv.cfg")); err == nil && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		n++
		if n > limit || !fn(rel) {
			return filepath.SkipAll
		}
		return nil
	})
}

func (e *Editor) pickFile() {
	root := e.cwd
	p := &picker{title: "Files"}
	p.onSelect = func(it pickItem) { e.execEx("e " + it.data.(string)) }
	p.loading = true
	e.openPicker(p)
	e.safeGo(func() {
		var items []pickItem
		walkFiles(root, 100000, func(rel string) bool {
			items = append(items, pickItem{label: rel, data: filepath.Join(root, rel)})
			return true
		})
		sort.Slice(items, func(i, j int) bool { return items[i].label < items[j].label })
		e.Post(func() {
			p.all = items
			p.loading = false
			if e.picker == p {
				p.filter()
			}
		})
	})
}

func (e *Editor) pickBuffer() {
	var items []pickItem
	for _, b := range e.listedBuffers() {
		if b == e.win.buf {
			continue
		}
		label := b.Name
		if b.Path != "" {
			if rel, err := filepath.Rel(e.cwd, b.Path); err == nil && !strings.HasPrefix(rel, "..") {
				label = rel
			} else {
				label = b.Path
			}
		}
		detail := ""
		if e.modified(b) {
			detail = "[+]"
		}
		items = append(items, pickItem{label: label, detail: detail, data: b})
	}
	items = append(items, pickItem{label: e.win.buf.Name, detail: "(current)", data: e.win.buf})
	p := &picker{title: "Buffers", all: items}
	p.onSelect = func(it pickItem) {
		b := it.data.(*buffer.Buffer)
		if e.bufferAlive(b) {
			e.setBuffer(e.win, b)
		}
	}
	e.openPicker(p)
}

func (e *Editor) pickLines() {
	b := e.win.buf
	items := make([]pickItem, 0, b.LineCount())
	for l := 0; l < b.LineCount(); l++ {
		s := b.LineString(l)
		if strings.TrimSpace(s) == "" {
			continue
		}
		items = append(items, pickItem{label: s, detail: fmt.Sprint(l + 1), data: l})
	}
	p := &picker{title: "Lines", all: items}
	p.onSelect = func(it pickItem) {
		e.pushJump(e.win)
		e.setCursor(e.win, Pos{it.data.(int), firstNonBlank(b, it.data.(int))})
	}
	e.openPicker(p)
}

type grepHit struct {
	path string
	line int
	col  int
}

func (e *Editor) pickGrep(initial string) {
	root := e.cwd
	p := &picker{title: "Grep (Go regexp)", input: []rune(initial)}
	p.onSelect = func(it pickItem) {
		h := it.data.(grepHit)
		b, err := e.loadBuffer(h.path)
		if err != nil {
			e.errorf("%v", err)
			return
		}
		e.pushJump(e.win)
		e.setBuffer(e.win, b)
		e.setCursor(e.win, Pos{h.line, h.col})
		e.search.pattern = string(p.input)
		e.search.noHL = false
	}
	p.dynamic = func(q string, gen int64) {
		if len(q) < 2 {
			p.all, p.shown, p.loading = nil, nil, false
			return
		}
		re, err := e.compilePattern(q)
		if err != nil {
			p.all, p.shown, p.loading = nil, nil, false
			return
		}
		e.safeGo(func() {
			var items []pickItem
			walkFiles(root, 20000, func(rel string) bool {
				if gen != p.gen.Load() {
					return false
				}
				f, err := os.Open(filepath.Join(root, rel))
				if err != nil {
					return true
				}
				defer f.Close()
				sc := bufio.NewScanner(f)
				sc.Buffer(make([]byte, 64*1024), 1<<20)
				ln := 0
				for sc.Scan() {
					line := sc.Text()
					if ln == 0 && !utf8.ValidString(line) {
						break
					}
					if loc := re.FindStringIndex(line); loc != nil {
						col := runeIndex(line, loc[0])
						text := strings.TrimSpace(line)
						if len(text) > 200 {
							text = text[:200]
						}
						items = append(items, pickItem{
							label:  fmt.Sprintf("%s:%d: %s", rel, ln+1, text),
							data:   grepHit{filepath.Join(root, rel), ln, col},
							detail: "",
						})
						if len(items) >= 2000 {
							return false
						}
					}
					ln++
				}
				return true
			})
			e.Post(func() {
				if gen != p.gen.Load() {
					return
				}
				p.all = items
				p.shown = append([]pickItem(nil), items...)
				p.sel, p.scroll = 0, 0
				p.loading = false
			})
		})
	}
	e.openPicker(p)
}

func (e *Editor) pickTheme() {
	var items []pickItem
	for _, t := range syntax.ThemeNames() {
		d := ""
		if t == e.theme.Name {
			d = "(current)"
		}
		items = append(items, pickItem{label: t, detail: d, data: t})
	}
	p := &picker{title: "Theme", all: items}
	p.onSelect = func(it pickItem) { e.setTheme(it.data.(string)) }
	e.openPicker(p)
}

func (e *Editor) pickFiletype() {
	var items []pickItem
	for _, n := range syntax.LexerNames() {
		items = append(items, pickItem{label: strings.ToLower(n), data: n})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].label < items[j].label })
	p := &picker{title: "Language", all: items}
	p.onSelect = func(it pickItem) { e.execEx("setfiletype " + it.label) }
	e.openPicker(p)
}

func (e *Editor) pickHistory() {
	h := e.cmdHist[':']
	var items []pickItem
	for i := len(h) - 1; i >= 0; i-- {
		items = append(items, pickItem{label: h[i], data: h[i]})
	}
	p := &picker{title: "Command history", all: items}
	p.onSelect = func(it pickItem) { e.execEx(it.data.(string)) }
	e.openPicker(p)
}

func (e *Editor) pickCommand() {
	var items []pickItem
	seen := map[string]bool{}
	for _, c := range exCommands {
		if seen[c.name] {
			continue
		}
		seen[c.name] = true
		items = append(items, pickItem{label: c.name, detail: c.desc, data: c.name})
	}
	p := &picker{title: "Commands", all: items}
	p.onSelect = func(it pickItem) { e.startCmdline(':', it.data.(string)+" ") }
	e.openPicker(p)
}
