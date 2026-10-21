// Package notebook reads and writes Jupyter notebooks (nbformat 4),
// maps them to an editable text form with "# %% [type] id=..." cell
// markers, and runs code cells through an embedded Python driver.
package notebook

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Cell is one notebook cell.
type Cell struct {
	ID             string
	Type           string // "code", "markdown" or "raw"
	Source         string
	ExecutionCount *int
	Outputs        []map[string]any
	// Extra holds fields we do not interpret (metadata, attachments...).
	Extra map[string]json.RawMessage
}

// Notebook is a parsed .ipynb file.
type Notebook struct {
	Cells    []*Cell
	Metadata json.RawMessage
	Major    int
	Minor    int
	Extra    map[string]json.RawMessage
}

// NewID returns a random cell id.
func NewID() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// multiline decodes nbformat's "string or list of strings".
func multiline(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []string
	if json.Unmarshal(raw, &parts) == nil {
		return strings.Join(parts, "")
	}
	return ""
}

// SplitLines splits s into lines keeping "\n" (nbformat list style).
func SplitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	var out []string
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

// New returns an empty notebook with one code cell.
func New() *Notebook {
	return &Notebook{
		Major: 4, Minor: 5,
		Metadata: json.RawMessage(`{"kernelspec":{"display_name":"Python 3","language":"python","name":"python3"},"language_info":{"name":"python"}}`),
		Cells:    []*Cell{{ID: NewID(), Type: "code"}},
		Extra:    map[string]json.RawMessage{},
	}
}

// Parse decodes an .ipynb document.
func Parse(data []byte) (*Notebook, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("invalid notebook JSON: %w", err)
	}
	nb := &Notebook{Extra: map[string]json.RawMessage{}}
	for k, v := range top {
		switch k {
		case "cells":
		case "metadata":
			nb.Metadata = v
		case "nbformat":
			json.Unmarshal(v, &nb.Major)
		case "nbformat_minor":
			json.Unmarshal(v, &nb.Minor)
		default:
			nb.Extra[k] = v
		}
	}
	if nb.Major != 4 {
		return nil, fmt.Errorf("unsupported nbformat %d (only 4 is supported)", nb.Major)
	}
	var cells []map[string]json.RawMessage
	if raw, ok := top["cells"]; ok {
		if err := json.Unmarshal(raw, &cells); err != nil {
			return nil, fmt.Errorf("invalid cells: %w", err)
		}
	}
	seen := map[string]bool{}
	for _, rc := range cells {
		c := &Cell{Extra: map[string]json.RawMessage{}}
		for k, v := range rc {
			switch k {
			case "id":
				json.Unmarshal(v, &c.ID)
			case "cell_type":
				json.Unmarshal(v, &c.Type)
			case "source":
				c.Source = multiline(v)
			case "execution_count":
				var n *int
				json.Unmarshal(v, &n)
				c.ExecutionCount = n
			case "outputs":
				dec := json.NewDecoder(bytes.NewReader(v))
				dec.UseNumber()
				dec.Decode(&c.Outputs)
			default:
				c.Extra[k] = v
			}
		}
		if c.ID == "" || seen[c.ID] || !validID.MatchString(c.ID) {
			c.ID = NewID()
		}
		seen[c.ID] = true
		if c.Type == "" {
			c.Type = "code"
		}
		nb.Cells = append(nb.Cells, c)
	}
	if nb.Metadata == nil {
		nb.Metadata = json.RawMessage(`{}`)
	}
	return nb, nil
}

var validID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// Load reads an .ipynb file. A missing file yields a new notebook.
func Load(path string) (*Notebook, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return New(), nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return New(), nil
	}
	return Parse(data)
}

// Marshal encodes the notebook like Jupyter does (sorted keys, indent 1).
func (nb *Notebook) Marshal() ([]byte, error) {
	top := map[string]any{}
	for k, v := range nb.Extra {
		top[k] = v
	}
	top["nbformat"] = 4
	// cell ids require nbformat >= 4.5
	minor := nb.Minor
	if minor < 5 {
		minor = 5
	}
	top["nbformat_minor"] = minor
	top["metadata"] = nb.Metadata
	cells := make([]map[string]any, 0, len(nb.Cells))
	for _, c := range nb.Cells {
		m := map[string]any{}
		for k, v := range c.Extra {
			m[k] = v
		}
		if _, ok := m["metadata"]; !ok {
			m["metadata"] = map[string]any{}
		}
		m["id"] = c.ID
		m["cell_type"] = c.Type
		m["source"] = SplitLines(c.Source)
		if c.Type == "code" {
			outs := c.Outputs
			if outs == nil {
				outs = []map[string]any{}
			}
			m["outputs"] = outs
			m["execution_count"] = c.ExecutionCount
			delete(m, "attachments") // only markdown/raw cells may have them
		} else {
			delete(m, "outputs")
			delete(m, "execution_count")
		}
		cells = append(cells, m)
	}
	top["cells"] = cells
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", " ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(top); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ---- text form ----

// MarkerRe matches a cell marker line.
var MarkerRe = regexp.MustCompile(`^# %%(?:\s*\[(code|markdown|raw)\])?(?:\s+id=([A-Za-z0-9_-]+))?\s*$`)

// Marker returns the marker line for a cell.
func Marker(c *Cell) string {
	return fmt.Sprintf("# %%%% [%s] id=%s", c.Type, c.ID)
}

// ToText renders the cells to editable text.
func (nb *Notebook) ToText() string {
	var sb strings.Builder
	for i, c := range nb.Cells {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(Marker(c))
		sb.WriteByte('\n')
		sb.WriteString(c.Source)
	}
	return sb.String()
}

// Range locates a cell in the buffer text. Header is the marker line
// (-1 for leading text without a marker); content is [Start, End).
type Range struct {
	ID     string
	Type   string
	Header int
	Start  int
	End    int
}

// Ranges finds cells in buffer lines.
func Ranges(lineCount int, line func(int) string) []Range {
	var out []Range
	cur := Range{Header: -1, Type: "code"}
	open := false
	for i := 0; i < lineCount; i++ {
		l := line(i)
		if !strings.HasPrefix(l, "# %%") {
			continue
		}
		m := MarkerRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if open || i > 0 {
			cur.End = i
			if open || cur.End > cur.Start {
				out = append(out, cur)
			}
		}
		typ := m[1]
		if typ == "" {
			typ = "code"
		}
		cur = Range{ID: m[2], Type: typ, Header: i, Start: i + 1}
		open = true
	}
	cur.End = lineCount
	if open || cur.End > cur.Start {
		out = append(out, cur)
	}
	return out
}

// Sync rebuilds nb.Cells from buffer text, keeping outputs and metadata
// of cells whose ids are still present. It returns the ranges with ids
// filled in; ranges whose marker lacked an id get a fresh one (the caller
// should rewrite those marker lines).
func (nb *Notebook) Sync(lineCount int, line func(int) string) []Range {
	ranges := Ranges(lineCount, line)
	old := map[string]*Cell{}
	for _, c := range nb.Cells {
		old[c.ID] = c
	}
	seen := map[string]bool{}
	cells := make([]*Cell, 0, len(ranges))
	for i := range ranges {
		r := &ranges[i]
		if r.ID == "" || seen[r.ID] {
			r.ID = NewID()
		}
		seen[r.ID] = true
		lines := make([]string, 0, r.End-r.Start)
		for j := r.Start; j < r.End; j++ {
			lines = append(lines, line(j))
		}
		src := strings.Join(lines, "\n")
		c, ok := old[r.ID]
		if !ok {
			c = &Cell{ID: r.ID, Extra: map[string]json.RawMessage{}}
		}
		c.Type = r.Type
		c.Source = src
		cells = append(cells, c)
	}
	nb.Cells = cells
	return ranges
}

// Cell returns the cell with the given id.
func (nb *Notebook) Cell(id string) *Cell {
	for _, c := range nb.Cells {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// ---- output display ----

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var sb strings.Builder
		for _, x := range t {
			if s, ok := x.(string); ok {
				sb.WriteString(s)
			}
		}
		return sb.String()
	case []string:
		return strings.Join(t, "")
	}
	return ""
}

// OutputLine is one display line of a cell output.
type OutputLine struct {
	Text string
	Kind string // "stdout", "stderr", "result", "error", "image", "info"
}

// DisplayLines converts outputs to plain display lines.
func DisplayLines(outs []map[string]any) []OutputLine {
	var out []OutputLine
	add := func(text, kind string) {
		text = ansiRe.ReplaceAllString(text, "")
		text = strings.TrimSuffix(text, "\n")
		for _, l := range strings.Split(text, "\n") {
			out = append(out, OutputLine{strings.ReplaceAll(l, "\t", "    "), kind})
		}
	}
	for _, o := range outs {
		switch o["output_type"] {
		case "stream":
			kind := "stdout"
			if o["name"] == "stderr" {
				kind = "stderr"
			}
			add(str(o["text"]), kind)
		case "execute_result", "display_data":
			data, _ := o["data"].(map[string]any)
			kind := "result"
			if o["output_type"] == "display_data" {
				kind = "info"
			}
			for _, mime := range []string{"image/png", "image/jpeg", "image/svg+xml"} {
				if v, ok := data[mime]; ok {
					size := len(str(v))
					if mime != "image/svg+xml" {
						size = size * 3 / 4
					}
					add(fmt.Sprintf("[%s %.1f KB — :nbimage to view]", mime, float64(size)/1024), "image")
				}
			}
			if t, ok := data["text/plain"]; ok {
				add(str(t), kind)
			} else if _, ok := data["text/html"]; ok {
				add("[text/html output]", "info")
			}
		case "error":
			if tb, ok := o["traceback"].([]any); ok && len(tb) > 0 {
				var lines []string
				for _, l := range tb {
					if s, ok := l.(string); ok {
						lines = append(lines, s)
					}
				}
				add(strings.Join(lines, "\n"), "error")
			} else {
				add(fmt.Sprintf("%v: %v", o["ename"], o["evalue"]), "error")
			}
		}
	}
	return out
}

// Images returns decoded image outputs (mime, bytes) of a cell.
func Images(outs []map[string]any) []Image {
	var imgs []Image
	for _, o := range outs {
		data, _ := o["data"].(map[string]any)
		for _, mime := range []string{"image/png", "image/jpeg", "image/svg+xml"} {
			if v, ok := data[mime]; ok {
				imgs = append(imgs, Image{Mime: mime, Data: str(v)})
			}
		}
	}
	return imgs
}

// Image is an image output; Data is base64 (or raw SVG text).
type Image struct {
	Mime string
	Data string
}
