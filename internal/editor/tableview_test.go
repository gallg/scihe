package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const salesCSV = `region,product,units,price
north,apple,10,1.5
south,pear,4,2
north,pear,NA,2.25
east,apple,7,1.5
south,apple,1,1.75
`

func openTable(t *testing.T) (*Editor, *tableView, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sales.csv")
	os.WriteFile(path, []byte(salesCSV), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>:table<CR>")
	tv := e.tableFor(e.win.buf)
	if tv == nil {
		t.Fatalf("no table view (msg %q)", e.msg)
	}
	return e, tv, path
}

func TestTableViewLayoutAndMotion(t *testing.T) {
	e, tv, _ := openTable(t)
	b := e.win.buf
	if b.LineCount() != 6 || !strings.HasPrefix(b.LineString(0), "region │ product │ units │ price") {
		t.Fatalf("view:\n%s", b.Text())
	}
	// numbers are right-aligned under their header
	if b.LineString(1) != "north  │ apple   │    10 │   1.5" {
		t.Fatalf("row 1 %q", b.LineString(1))
	}
	if e.win.cur.Line != 1 {
		t.Fatalf("cursor should start on the first data row, at %d", e.win.cur.Line)
	}
	typeKeys(e, "ll")
	if c := e.cursorColumn(e.win, tv); c != 2 {
		t.Fatalf("column %d", c)
	}
	e.draw()
	if st := e.tableStatus(e.win, tv); !strings.Contains(st, "col 3/4 units") || !strings.Contains(st, "row 1/5") {
		t.Fatalf("status %q", st)
	}
	typeKeys(e, "h$")
	if c := e.cursorColumn(e.win, tv); c != 3 {
		t.Fatalf("$ column %d", c)
	}
	// the buffer cannot be edited
	typeKeys(e, "x")
	if !strings.HasPrefix(b.LineString(1), "north") || b.Modified() {
		t.Fatal("table view was modified")
	}
}

func TestTableStatsAndSelection(t *testing.T) {
	e, _, _ := openTable(t)
	typeKeys(e, "ll:sum<CR>")
	if !strings.Contains(e.msg, "sum(units) = 22") || !strings.Contains(e.msg, "all 5 rows") {
		t.Fatalf("sum msg %q", e.msg)
	}
	if r, _ := e.regs.Get('"'); r.Text != "22" {
		t.Fatalf("result not yanked: %q", r.Text)
	}
	typeKeys(e, ":mean units price<CR>")
	if !strings.Contains(e.msg, "mean(units) = 5.5") || !strings.Contains(e.msg, "mean(price) = 1.8") {
		t.Fatalf("mean msg %q", e.msg)
	}
	// block selection of 2 rows x 2 columns (units, price of rows 1-2)
	typeKeys(e, "gg")
	typeKeys(e, "j0ll<C-v>jl<Space>du")
	if !strings.Contains(e.msg, "sum(units) = 14") || !strings.Contains(e.msg, "sum(price) = 3.5") || !strings.Contains(e.msg, "rows 1-2") {
		t.Fatalf("block sum %q", e.msg)
	}
	if e.mode != ModeNormal {
		t.Fatal("still in visual mode")
	}
	// ex command on the last visual selection ('<,'> range)
	typeKeys(e, "gvl:max<CR>")
	if !strings.Contains(e.msg, "max(units) = 10") || !strings.Contains(e.msg, "max(price) = 2") {
		t.Fatalf("'<,'> max %q", e.msg)
	}
	// row-wise with !
	typeKeys(e, "4G0ll:sum! units price<CR>")
	if !strings.Contains(e.msg, "sum(row 3) = 2.25") {
		t.Fatalf("row sum %q", e.msg)
	}
	// describe
	typeKeys(e, ":stats *<CR>")
	sb := e.win.buf
	txt := sb.Text()
	if sb.Name != "*stats*" || !strings.Contains(txt, "median") || !strings.Contains(txt, "missing") {
		t.Fatalf("stats:\n%s", txt)
	}
	fields := map[string]string{}
	for _, l := range strings.Split(txt, "\n") {
		if f := strings.Fields(l); len(f) > 1 {
			fields[f[0]] = strings.Join(f[1:], " ")
		}
	}
	for row, want := range map[string]string{
		"count": "5 5 4 5", "std": "— — 3.872983 0.32596", "top": "north apple 1 1.5",
		// skew / kurt / range / iqr checked with pandas
		"skew": "— — 0 0.541387", "kurt": "— — -1.2 -1.487889", "range": "— — 9 0.75", "iqr": "— — 4.5 0.5",
	} {
		if fields[row] != want {
			t.Fatalf("stats row %s = %q, want %q:\n%s", row, fields[row], want, txt)
		}
	}
	if !strings.Contains(txt, "95% CI") || !strings.Contains(txt, "1 (20.0%)") {
		t.Fatalf("CI / missing rows:\n%s", txt)
	}
}

func TestTableSortFilterAndBack(t *testing.T) {
	e, tv, path := openTable(t)
	b := e.win.buf
	typeKeys(e, "llS")
	if !strings.HasPrefix(b.LineString(1), "north  │ apple") || !strings.HasPrefix(b.LineString(5), "north  │ pear") {
		t.Fatalf("sorted desc (NA last):\n%s", b.Text())
	}
	if !strings.Contains(b.LineString(0), "units ▼") {
		t.Fatalf("sort marker: %q", b.LineString(0))
	}
	typeKeys(e, "o")
	typeKeys(e, "0lf=apple<CR>")
	if len(tv.order) != 3 || b.LineCount() != 4 {
		t.Fatalf("filter rows %d:\n%s", len(tv.order), b.Text())
	}
	typeKeys(e, ":where price > 1.5<CR>")
	if len(tv.order) != 1 || !strings.Contains(b.LineString(1), "south") {
		t.Fatalf("two filters:\n%s", b.Text())
	}
	typeKeys(e, ":sum units<CR>")
	if !strings.Contains(e.msg, "sum(units) = 1") {
		t.Fatalf("stats ignore filtered rows? %q", e.msg)
	}
	typeKeys(e, "F")
	if len(tv.order) != 5 {
		t.Fatal("filters not cleared")
	}
	// q returns to the text on the same record
	typeKeys(e, "4Gq")
	if e.win.buf.Path != path || e.win.cur.Line != 3 {
		t.Fatalf("back in %q line %d", e.win.buf.Name, e.win.cur.Line)
	}
	// edits to the text show up when the view is opened again
	typeKeys(e, "Go west,plum,100,3<Esc>:table<CR>")
	if e.tableFor(e.win.buf) != tv || len(tv.t.Rows) != 6 || tv.t.Rows[5][1] != "plum" {
		t.Fatalf("view not refreshed: %v", tv.t.Rows)
	}
	if e.win.cur.Line != 6 {
		t.Fatalf("cursor row %d", e.win.cur.Line)
	}
	typeKeys(e, ":hist<CR>")
	if txt := e.win.buf.Text(); !strings.Contains(txt, "value counts of region") || !strings.Contains(txt, "north") || !strings.Contains(txt, "█") {
		t.Fatalf("hist:\n%s", txt)
	}
}

func TestTableDeletedSource(t *testing.T) {
	e, tv, _ := openTable(t)
	typeKeys(e, "q:bd<CR>")
	e.draw()
	if e.bufferAlive(tv.buf) || len(e.tables) != 0 {
		t.Fatal("view of a deleted buffer kept")
	}
}

func TestTableNotATable(t *testing.T) {
	e := newTestEditor(t, "plain")
	typeKeys(e, ":sum<CR>")
	if !strings.Contains(e.msg, "not a table view") {
		t.Fatalf("msg %q", e.msg)
	}
}

func TestTableEdgeCases(t *testing.T) {
	dir := t.TempDir()
	e := newTestEditor(t, "")
	// empty file: keys must not crash
	empty := filepath.Join(dir, "empty.csv")
	os.WriteFile(empty, []byte("\n\n"), 0o644)
	typeKeys(e, ":e "+empty+"<CR>:table<CR>")
	typeKeys(e, "<CR>fx<CR>lhsS=q")
	// trailing empty cell is reachable
	tr := filepath.Join(dir, "trail.csv")
	os.WriteFile(tr, []byte("a,b,c\nx,y,\n"), 0o644)
	typeKeys(e, ":e "+tr+"<CR>:table<CR>$")
	tv := e.tableFor(e.win.buf)
	if c := e.cursorColumn(e.win, tv); c != 2 {
		t.Fatalf("$ on trailing empty cell: column %d", c)
	}
	// BOM and zero-width runes keep columns aligned
	bom := filepath.Join(dir, "bom.csv")
	os.WriteFile(bom, []byte("\ufeffname,x\ne\u0301e,1\nab,2\n"), 0o644)
	typeKeys(e, ":e "+bom+"<CR>:table<CR>")
	tv = e.tableFor(e.win.buf)
	if tv.t.Column("name") != 0 {
		t.Fatal("BOM kept in header")
	}
	for l := 0; l < e.win.buf.LineCount(); l++ {
		line := e.win.buf.Line(l)
		i := strings.IndexRune(string(line), '│')
		if vc := displayCol(line, len([]rune(string(line)[:i])), 8); vc != tv.start[1]-2 {
			t.Fatalf("line %d separator at %d, want %d: %q", l, vc, tv.start[1]-2, string(line))
		}
	}
	// re-entering a sorted view lands on the cursor's record
	nums := filepath.Join(dir, "n.csv")
	os.WriteFile(nums, []byte("n\n1\n2\n3\n4\n"), 0o644)
	typeKeys(e, ":e "+nums+"<CR>:table<CR>Sq3G:table<CR>")
	tv = e.tableFor(e.win.buf)
	if r := tv.order[e.win.cur.Line-1]; tv.t.Rows[r][0] != "2" {
		t.Fatalf("landed on %q", tv.t.Rows[r][0])
	}
	// overflowing range and huge bin counts
	big := filepath.Join(dir, "big.csv")
	os.WriteFile(big, []byte("v\n-1.7e308\n1.7e308\n0\n"), 0o644)
	typeKeys(e, ":e "+big+"<CR>:table<CR>:hist<CR>")
	if !strings.Contains(e.win.buf.Text(), "histogram of v") {
		t.Fatalf("hist: %q / %q", e.win.buf.Text(), e.msg)
	}
	typeKeys(e, "<C-w>p:hist 1000000000<CR>")
	if n := strings.Count(e.win.buf.Text(), "\n"); n > 300 {
		t.Fatalf("%d histogram lines", n)
	}
}
