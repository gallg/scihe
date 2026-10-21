package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const editCSV = `id,region,answer,price,note
1,north,yes,"1.234,5","keep ""quoted"", please"
2,south,no,"891,1",plain
3,east,Yes,"15,9",x
4,north,NA,"2,25",y
`

func openEditTable(t *testing.T, text, name string) (*Editor, *tableView, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	os.WriteFile(path, []byte(text), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>:table<CR>")
	tv := e.tableFor(e.win.buf)
	if tv == nil {
		t.Fatalf("no table (msg %q)", e.msg)
	}
	return e, tv, path
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestTableSelectRowColumnAll(t *testing.T) {
	e, _, _ := openTable(t) // sales: region,product,units,price
	typeKeys(e, "llc")
	if e.mode != ModeVisualBlock {
		t.Fatalf("c: mode %v", e.mode)
	}
	typeKeys(e, "<Space>du")
	if !strings.Contains(e.msg, "sum(units) = 22") || !strings.Contains(e.msg, "all 5 rows") {
		t.Fatalf("column selection sum %q", e.msg)
	}
	// narrow the column selection with k, widen with l
	typeKeys(e, "ckkl:sum<CR>")
	if !strings.Contains(e.msg, "sum(units) = 14") || !strings.Contains(e.msg, "sum(price) = 5.75") {
		t.Fatalf("narrowed selection %q", e.msg)
	}
	typeKeys(e, "3Gr")
	if e.mode != ModeVisualLine {
		t.Fatal("r: not a row selection")
	}
	typeKeys(e, "<Esc>a=")
	if txt := e.win.buf.Text(); !strings.Contains(txt, "all 5 rows") || !strings.Contains(txt, "region") || !strings.Contains(txt, "price") {
		t.Fatalf("a + =:\n%s", txt)
	}
}

func TestTableMarks(t *testing.T) {
	e, tv, _ := openTable(t)
	// mark the two pear rows' units with * on product, then stats on units
	typeKeys(e, "2Gl*") // apple: rows 1, 4, 5
	if len(tv.marks) != 3 {
		t.Fatalf("* marks %v", tv.marks)
	}
	typeKeys(e, "U")
	typeKeys(e, "ll:mark units > 5<CR>")
	if !strings.Contains(e.msg, "2 cells marked in units") {
		t.Fatalf(":mark %q", e.msg)
	}
	typeKeys(e, ":sum<CR>")
	if !strings.Contains(e.msg, "sum(units) = 17") || !strings.Contains(e.msg, "2 marked") {
		t.Fatalf("sum of marks %q", e.msg)
	}
	// m toggles one cell, M inverts the column
	typeKeys(e, "6Gllm:count<CR>")
	if !strings.Contains(e.msg, "count(units) = 3") {
		t.Fatalf("m %q", e.msg)
	}
	typeKeys(e, "M:count<CR>")
	if !strings.Contains(e.msg, "count(units) = 1") { // NA is not counted
		t.Fatalf("M %q", e.msg)
	}
	// a visual selection wins over marks
	typeKeys(e, "2Gll<C-v>j<Space>du")
	if !strings.Contains(e.msg, "sum(units) = 14") {
		t.Fatalf("visual over marks %q", e.msg)
	}
	// marking a visual selection
	typeKeys(e, "U2G0<C-v>jlm")
	if len(tv.marks) != 4 {
		t.Fatalf("visual m: %d marks", len(tv.marks))
	}
}

func TestTableRecode(t *testing.T) {
	e, tv, path := openEditTable(t, editCSV, "r.csv")
	before := readFile(t, path)
	// decimal commas → new numeric column
	typeKeys(e, "lll:recode<CR>")
	if tv.d.header[4] != "price_num" || tv.row(0).cells[4] != "1234.5" || tv.row(1).cells[4] != "891.1" {
		t.Fatalf("number recode: %q / %q", tv.d.header, tv.row(0).cells)
	}
	if !tv.editing || readFile(t, path) != before {
		t.Fatal("recode must only stage")
	}
	// yes/no in place
	e.focus(e.windows()[0])
	for _, w := range e.windows() {
		if w.buf == tv.buf {
			e.focus(w)
		}
	}
	typeKeys(e, "0ll:recode!<CR>")
	if got := []string{tv.row(0).cells[2], tv.row(1).cells[2], tv.row(2).cells[2], tv.row(3).cells[2]}; strings.Join(got, ",") != "1,0,1,NA" {
		t.Fatalf("bool in place %v", got)
	}
	// categories → codes, sorted, mapping shown
	for _, w := range e.windows() {
		if w.buf == tv.buf {
			e.focus(w)
		}
	}
	typeKeys(e, "0l:recode codes<CR>")
	if tv.d.header[2] != "region_code" || tv.row(0).cells[2] != "2" || tv.row(2).cells[2] != "1" || tv.row(1).cells[2] != "3" {
		t.Fatalf("codes: %q %q", tv.d.header, tv.row(2).cells)
	}
	rb := e.scratch("*recode*", 3)
	if !strings.Contains(rb.Text(), "region → region_code (codes") || !strings.Contains(rb.Text(), "east  (1)") {
		t.Fatalf("mapping:\n%s", rb.Text())
	}
	// user mapping with an unknown value
	for _, w := range e.windows() {
		if w.buf == tv.buf {
			e.focus(w)
		}
	}
	typeKeys(e, "0l:recode! map north=10 south=20<CR>")
	if tv.row(0).cells[1] != "10" || tv.row(2).cells[1] != "" {
		t.Fatalf("map: %q", tv.row(2).cells)
	}
	if !strings.Contains(rb.Text(), "1 not convertible") {
		t.Fatalf("failures not reported:\n%s", rb.Text())
	}
	// undo everything
	for _, w := range e.windows() {
		if w.buf == tv.buf {
			e.focus(w)
		}
	}
	typeKeys(e, "uuuu")
	if tv.pending() {
		t.Fatalf("after undo: %s", tv.changes())
	}
	typeKeys(e, "<C-r>")
	if tv.d.header[4] != "price_num" {
		t.Fatal("redo")
	}
}

func TestTableEditAndApply(t *testing.T) {
	e, tv, path := openEditTable(t, editCSV, "e.csv")
	// read-only by default
	typeKeys(e, "x")
	if !strings.Contains(e.msg, "press e to edit") || tv.pending() {
		t.Fatalf("x outside edit mode: %q", e.msg)
	}
	typeKeys(e, "e")
	// edit the region of row 2 and the price of row 3
	typeKeys(e, "3G0li<C-u>west<CR>")
	typeKeys(e, "4G0lllx")
	// delete row 1 (with the quoted note), add a row after the last
	typeKeys(e, "2Gdd")
	typeKeys(e, "Go")
	typeKeys(e, "0i5<CR>li<C-u>mid, central<CR>")
	ch := tv.changes()
	if ch.cells != 2 || ch.deleted != 1 || ch.added != 1 {
		t.Fatalf("changes %+v", ch)
	}
	if readFile(t, path) != editCSV {
		t.Fatal("file written before applying")
	}
	// :w asks; "n" keeps the changes staged
	typeKeys(e, ":w<CR>n<CR>")
	if !tv.pending() || readFile(t, path) != editCSV {
		t.Fatalf("n applied anyway (msg %q)", e.msg)
	}
	typeKeys(e, ":w<CR>y<CR>")
	want := `id,region,answer,price,note
2,west,no,"891,1",plain
3,east,Yes,,x
4,north,NA,"2,25",y
5,"mid, central",,,
`
	if got := readFile(t, path); got != want {
		t.Fatalf("applied file:\n%s\nwant:\n%s\n(msg %q)", got, want, e.msg)
	}
	if tv.pending() || len(tv.t.Rows) != 4 {
		t.Fatalf("view not reloaded: %s", tv.changes())
	}
}

func TestTableApplyKeepsUntouchedText(t *testing.T) {
	text := "a;b\n\"x\";  spaced  \n\n1;2\n3;4\n"
	e, tv, path := openEditTable(t, text, "k.csv")
	if tv.delim != ';' {
		t.Fatalf("delim %q", tv.delim)
	}
	typeKeys(e, "e4G0i<C-u>9<CR>:w<CR>y<CR>")
	got := readFile(t, path)
	if !strings.HasPrefix(got, "a;b\n\"x\";  spaced  \n\n") || !strings.Contains(got, "\n9;4") {
		t.Fatalf("untouched records changed:\n%q", got)
	}
}

func TestTableColumnsAndHeader(t *testing.T) {
	e, tv, path := openEditTable(t, "\ufeffa,b,c\n1,2,3\n", "c.csv")
	typeKeys(e, "l:column rename beta<CR>")
	if !tv.editing || tv.d.header[1] != "beta" {
		t.Fatalf("rename %q", tv.d.header)
	}
	typeKeys(e, "l:column delete<CR>:column add z<CR>")
	if strings.Join(tv.d.header, ",") != "a,beta,z" {
		t.Fatalf("columns %q", tv.d.header)
	}
	// header line: i renames the column under the cursor
	typeKeys(e, "gg$i<C-u>zz<CR>")
	if tv.d.header[2] != "zz" {
		t.Fatalf("header edit %q", tv.d.header)
	}
	typeKeys(e, "ZZy<CR>")
	if got := readFile(t, path); got != "\ufeffa,beta,zz\n1,2,\n" {
		t.Fatalf("file %q (msg %q)", got, e.msg)
	}
}

func TestTableDiscardAndSafety(t *testing.T) {
	e, tv, path := openEditTable(t, editCSV, "d.csv")
	typeKeys(e, "e2Gdd")
	// q asks before throwing edits away
	typeKeys(e, "qn<CR>")
	if e.tableFor(e.win.buf) != tv || !tv.pending() {
		t.Fatal("q discarded without asking")
	}
	// leaving edit mode asks too
	typeKeys(e, "en<CR>")
	if !tv.editing {
		t.Fatal("e left edit mode with pending edits")
	}
	typeKeys(e, "ZQy<CR>")
	if tv.pending() || len(tv.d.rows) != 4 {
		t.Fatal("ZQ did not discard")
	}
	// the text changing under staged edits blocks applying
	typeKeys(e, "2Gdd")
	typeKeys(e, "q") // asks
	typeKeys(e, "<Esc>")
	src := tv.src
	src.Insert(src.End(), "\n9,west,no,1,z")
	e.draw()
	if !tv.stale {
		t.Fatal("not marked stale")
	}
	typeKeys(e, ":w<CR>")
	if !strings.Contains(e.msg, "text changed") || readFile(t, path) != editCSV {
		t.Fatalf("applied over a changed source: %q", e.msg)
	}
	typeKeys(e, "ZQy<CR>")
	if tv.stale || len(tv.d.rows) != 5 {
		t.Fatalf("discard did not reload the text: stale %v rows %d", tv.stale, len(tv.d.rows))
	}
}

func TestTableTSVRefusesTabs(t *testing.T) {
	_, tv, _ := openEditTable(t, "a\tb\n1\t2\n", "t.tsv")
	tv.snapshot()
	if err := tv.setCell(0, 0, "x\ty"); err == nil || tv.pending() {
		t.Fatalf("tab accepted: %v", err)
	}
	if err := tv.setCell(0, 0, "x\ny"); err == nil {
		t.Fatal("line break accepted")
	}
}
