package editor

import (
	"strings"
	"testing"
)

// salesCSV (tableview_test.go):
//   region,product,units,price
//   north,apple,10,1.5 / south,pear,4,2 / north,pear,NA,2.25 /
//   east,apple,7,1.5 / south,apple,1,1.75

func viewColumn(tv *tableView, col int) string {
	var out []string
	for i := range tv.order {
		out = append(out, tv.row(i).cells[col])
	}
	return strings.Join(out, ",")
}

func TestTableMultiSort(t *testing.T) {
	e, tv, _ := openTable(t)
	typeKeys(e, ":sort region units desc<CR>")
	if got := viewColumn(tv, 2); got != "7,10,NA,4,1" {
		t.Fatalf("region asc, units desc: %s", got)
	}
	if h := e.win.buf.LineString(0); !strings.Contains(h, "region ▲1") || !strings.Contains(h, "units ▼2") {
		t.Fatalf("header %q", h)
	}
	if !strings.Contains(e.msg, "region asc, units desc") {
		t.Fatalf("msg %q", e.msg)
	}
	// s replaces the sort, gs / gS add levels (or flip one)
	typeKeys(e, "0ls")
	if len(tv.sortKeys) != 1 || viewColumn(tv, 1) != "apple,apple,apple,pear,pear" {
		t.Fatalf("s: %v %s", tv.sortKeys, viewColumn(tv, 1))
	}
	typeKeys(e, "lgS")
	if got := viewColumn(tv, 2); got != "10,7,1,4,NA" {
		t.Fatalf("product asc, units desc: %s", got)
	}
	typeKeys(e, "gs")
	if len(tv.sortKeys) != 2 || tv.sortKeys[1].desc {
		t.Fatalf("gs on a sort column flips it: %v", tv.sortKeys)
	}
	typeKeys(e, ":sort<CR>")
	if len(tv.sortKeys) != 0 || viewColumn(tv, 2) != "10,4,NA,7,1" {
		t.Fatal(":sort did not restore the file order")
	}
	typeKeys(e, ":sort nope<CR>")
	if !strings.Contains(e.msg, "no column") {
		t.Fatalf("msg %q", e.msg)
	}
}

func TestTableSortApply(t *testing.T) {
	e, tv, path := openEditTable(t, "k,v\nb,2\n\"a, quoted\",1\nc,3\n", "s.csv")
	typeKeys(e, ":sort apply<CR>")
	if !strings.Contains(e.msg, "sort first") {
		t.Fatalf("msg %q", e.msg)
	}
	typeKeys(e, "ls:sort apply<CR>")
	if !tv.changes().reordered || len(tv.sortKeys) != 0 {
		t.Fatalf("not staged: %+v", tv.changes())
	}
	typeKeys(e, ":w<CR>y<CR>")
	if got := readFile(t, path); got != "k,v\n\"a, quoted\",1\nb,2\nc,3\n" {
		t.Fatalf("file %q", got)
	}
}

func TestTableCorrCov(t *testing.T) {
	e, _, _ := openTable(t)
	typeKeys(e, "ll:corr<CR>")
	if !strings.Contains(e.msg, "two or more numeric columns") {
		t.Fatalf("single column: %q", e.msg)
	}
	// c then l: units and price
	typeKeys(e, "0llcl:corr<CR>")
	sb := e.win.buf
	txt := sb.Text()
	if sb.Name != "*stats*" || !strings.Contains(txt, "Pearson correlation (r)") || !strings.Contains(txt, "-0.674") || !strings.Contains(txt, "n = 4 pairs") {
		t.Fatalf("corr:\n%s", txt)
	}
	if e.static[sb.ID] == nil {
		t.Fatal("matrix not coloured")
	}
	typeKeys(e, "<C-w>p:corr spearman units price<CR>")
	if txt := e.win.buf.Text(); !strings.Contains(txt, "-0.738") || !strings.Contains(txt, "Spearman") {
		t.Fatalf("spearman:\n%s", txt)
	}
	typeKeys(e, "<C-w>p:corr kendall *<CR>")
	if txt := e.win.buf.Text(); !strings.Contains(txt, "skipped (no numbers): region, product") || !strings.Contains(txt, "Kendall") {
		t.Fatalf("kendall *:\n%s", txt)
	}
	typeKeys(e, "<C-w>p:cov units price<CR>")
	if txt := e.win.buf.Text(); !strings.Contains(txt, "-0.625") || !strings.Contains(txt, "Covariance") {
		t.Fatalf("cov:\n%s", txt)
	}
	// another output in *stats* is not coloured by the old matrix
	typeKeys(e, "<C-w>p:freq region<CR>")
	if e.static[e.win.buf.ID] != nil {
		t.Fatal("stale colours")
	}
	if txt := e.win.buf.Text(); !strings.Contains(txt, "north") || !strings.Contains(txt, "40.0") || !strings.Contains(txt, "100.0") {
		t.Fatalf("freq:\n%s", txt)
	}
}

func TestTableQuantileGroupBy(t *testing.T) {
	e, _, _ := openTable(t)
	typeKeys(e, ":quantile 90 units<CR>")
	if !strings.Contains(e.msg, "q0.9(units) = 9.1") {
		t.Fatalf("quantile %q", e.msg)
	}
	typeKeys(e, ":quantile 0.5 units<CR>")
	if !strings.Contains(e.msg, "= 5.5") {
		t.Fatalf("median %q", e.msg)
	}
	typeKeys(e, ":groupby region sum units mean price<CR>")
	gv := e.tableFor(e.win.buf)
	if gv == nil || gv.src.Name != "*groupby*" {
		t.Fatalf("no group table (msg %q)", e.msg)
	}
	if got := strings.Join(gv.d.header, ","); got != "region,n,sum_units,mean_price" {
		t.Fatalf("header %s", got)
	}
	var rows []string
	for _, r := range gv.d.rows {
		rows = append(rows, strings.Join(r.cells, ","))
	}
	if got := strings.Join(rows, "|"); got != "east,1,7,1.5|north,2,10,1.875|south,2,5,1.875" {
		t.Fatalf("groups %s", got)
	}
	typeKeys(e, ":groupby nope<CR>")
	if !strings.Contains(e.msg, "not a table view") && !strings.Contains(e.msg, "no column") {
		t.Fatalf("msg %q", e.msg)
	}
}

func TestTableComputeTransform(t *testing.T) {
	e, tv, path := openEditTable(t, "units,price\n10,1.5\n4,2\nNA,2.25\n", "c.csv")
	typeKeys(e, ":compute total = units * price<CR>")
	if strings.Join(tv.d.header, ",") != "units,price,total" || viewColumn(tv, 2) != "15,8," {
		t.Fatalf("compute: %q %s (msg %q)", tv.d.header, viewColumn(tv, 2), e.msg)
	}
	if !strings.Contains(e.msg, "1 missing") || !tv.editing {
		t.Fatalf("msg %q", e.msg)
	}
	typeKeys(e, ":compute total = 1<CR>")
	if !strings.Contains(e.msg, ":compute! replaces") {
		t.Fatalf("overwrite without !: %q", e.msg)
	}
	typeKeys(e, ":compute! total = round(units / 3, 1)<CR>")
	if viewColumn(tv, 2) != "3.3,1.3," {
		t.Fatalf("compute!: %s", viewColumn(tv, 2))
	}
	typeKeys(e, ":compute bad = nosuch + 1<CR>")
	if !strings.Contains(e.msg, "no column") {
		t.Fatalf("msg %q", e.msg)
	}
	typeKeys(e, "0:transform pct<CR>")
	if strings.Join(tv.d.header, ",") != "units,units_pct,price,total" || viewColumn(tv, 1) != "71.42857142857143,28.57142857142857," {
		t.Fatalf("transform: %q %s", tv.d.header, viewColumn(tv, 1))
	}
	typeKeys(e, ":transform! rank price<CR>")
	if viewColumn(tv, 2) != "1,2,3" {
		t.Fatalf("rank in place: %s", viewColumn(tv, 2))
	}
	typeKeys(e, ":transform nope<CR>")
	if !strings.Contains(e.msg, "unknown transform") {
		t.Fatalf("msg %q", e.msg)
	}
	typeKeys(e, ":w<CR>y<CR>")
	if got := readFile(t, path); got != "units,units_pct,price,total\n10,71.42857142857143,1,3.3\n4,28.57142857142857,2,1.3\nNA,,3,\n" {
		t.Fatalf("file %q (msg %q)", got, e.msg)
	}
}

func TestTableDescribeExtrasCommands(t *testing.T) {
	e, _, _ := openTable(t)
	typeKeys(e, ":skew units<CR>")
	if !strings.Contains(e.msg, "skew(units) = 0") {
		t.Fatalf("skew %q", e.msg)
	}
	typeKeys(e, ":iqr price<CR>")
	if !strings.Contains(e.msg, "iqr(price) = 0.5") { // pandas: 0.5
		t.Fatalf("iqr %q", e.msg)
	}
}

func TestTableAuditRegressions(t *testing.T) {
	// :corr with marks sharing no rows reports instead of crashing
	e, tv, _ := openEditTable(t, "a,b\n1,2\n3,4\n5,6\n7,8\n", "m.csv")
	typeKeys(e, "2Gmjm")  // a: rows 1-2
	typeKeys(e, "4Glmjm") // b: rows 3-4
	if len(tv.marks) != 4 {
		t.Fatalf("marks %v", tv.marks)
	}
	typeKeys(e, ":corr<CR>")
	if !strings.Contains(e.msg, "no rows are common") {
		t.Fatalf("msg %q", e.msg)
	}
	// undo of a column insert restores filters and marks on their columns
	e, tv, _ = openEditTable(t, "a,b,c\n1,x,3\n2,y,4\n", "u.csv")
	typeKeys(e, ":where b = x<CR>")
	if len(tv.order) != 1 {
		t.Fatal("filter")
	}
	typeKeys(e, "0:column add new<CR>u")
	if len(tv.order) != 1 || tv.filters[1] == nil || strings.Join(tv.d.header, ",") != "a,b,c" {
		t.Fatalf("after undo: header %v filters %v rows %d", tv.d.header, tv.filters, len(tv.order))
	}
	// empty table: no crash
	e, _, _ = openEditTable(t, "\n", "empty.csv")
	typeKeys(e, ":mark > 1<CR>:column rename x<CR>:column add z<CR>")
	// BOM + noheader + reorder: the mark is written once
	e, _, path := openEditTable(t, "\ufeffb,2\na,1\n", "bom.csv")
	_ = e
	e2 := newTestEditor(t, "")
	typeKeys(e2, ":e "+path+"<CR>:table noheader<CR>:sort 1<CR>:sort apply<CR>:w<CR>y<CR>")
	if got := readFile(t, path); got != "\ufeffa,1\nb,2\n" {
		t.Fatalf("BOM file %q (msg %q)", got, e2.msg)
	}
	// one-column CSV: a cleared cell is written as "" and kept as a row
	e, tv, path = openEditTable(t, "a\n1\n2\n3\n", "one.csv")
	typeKeys(e, "e3Gx:w<CR>y<CR>")
	if got := readFile(t, path); got != "a\n1\n\"\"\n3\n" {
		t.Fatalf("one-column file %q", got)
	}
	if len(tv.t.Rows) != 3 {
		t.Fatalf("rows after reload %d", len(tv.t.Rows))
	}
	// :transform! leaves missing and non-numeric cells as written
	e, tv, _ = openEditTable(t, "v\n1\nNA\n<5\n3\n", "z.csv")
	typeKeys(e, ":transform! abs<CR>")
	if got := viewColumn(tv, 0); got != "1,NA,<5,3" {
		t.Fatalf("transform! %s", got)
	}
	// ZZ reports errors (stale source)
	e, tv, _ = openEditTable(t, "a\n1\n2\n", "zz.csv")
	typeKeys(e, "e2Gdd")
	tv.src.Insert(tv.src.End(), "\n9")
	e.draw()
	typeKeys(e, "ZZ")
	if !strings.Contains(e.msg, "text changed") {
		t.Fatalf("ZZ msg %q", e.msg)
	}
}

func TestComputeTransformAggregates(t *testing.T) {
	e, tv, _ := openEditTable(t, "a,b\n10,1\n4,2\nNA,3\n7,4\n1,5\n", "g.csv")
	// the example from the request: a one-liner with a column aggregate
	typeKeys(e, ":compute dev = a - mean(a)<CR>")
	col := func(name string) int { return tv.columnByName(name) }
	if got := viewColumn(tv, col("dev")); got != "4.5,-1.5,,1.5,-4.5" {
		t.Fatalf("dev %s (msg %q)", got, e.msg)
	}
	typeKeys(e, ":compute sq = a ** 2 + square(b) - sqrt(pow(b, 2))<CR>")
	if got := viewColumn(tv, col("sq")); got != "100,18,,61,21" {
		t.Fatalf("sq %s", got)
	}
	// aggregates are over all rows, also when a filter hides some
	typeKeys(e, ":where b > 2<CR>:compute share = a / sum(a)<CR>F")
	if got := viewColumn(tv, col("share")); got != "0.45454545454545453,0.18181818181818182,,0.3181818181818182,0.045454545454545456" {
		t.Fatalf("share %s", got)
	}
	// :transform with _ on two selected columns; aggregates per column
	e2, tv2, _ := openEditTable(t, "x,y\n1,10\n2,20\n3,60\n", "z.csv")
	typeKeys(e2, "0cl:transform (_ - mean(_)) / std(_)<CR>")
	if h := strings.Join(tv2.d.header, ","); h != "x,x_t,y,y_t" {
		t.Fatalf("header %s (msg %q)", h, e2.msg)
	}
	if got := viewColumn(tv2, 1); got != "-1,0,1" {
		t.Fatalf("x_t %s", got)
	}
	if got := viewColumn(tv2, 3); got != "-0.7559289460184545,-0.37796447300922725,1.1338934190276817" {
		t.Fatalf("y_t %s", got)
	}
	// window functions follow the shown (sorted) order
	typeKeys(e2, "0S:transform cumsum x<CR>")
	if got := viewColumn(tv2, 1); got != "3,5,6" {
		t.Fatalf("cumsum in sorted order %s", got)
	}
	// a shortcut kind still works, and expressions need _
	typeKeys(e2, ":transform x + 1<CR>")
	if !strings.Contains(e2.msg, "use _") {
		t.Fatalf("msg %q", e2.msg)
	}
	typeKeys(e2, ":transform z y<CR>")
	if !strings.Contains(strings.Join(tv2.d.header, ","), "y_z") {
		t.Fatalf("z kind: %v", tv2.d.header)
	}
}
