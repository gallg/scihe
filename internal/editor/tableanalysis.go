package editor

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"

	"scihe/internal/buffer"
	"scihe/internal/syntax"
	"scihe/internal/table"
)

// Analysis commands of the table view: multi-column sorting, correlation
// and covariance matrices, quantiles, group-by summaries, frequency
// tables, computed and transformed columns.

func (tv *tableView) sortDescription() string {
	if len(tv.sortKeys) == 0 {
		return "file order"
	}
	parts := make([]string, len(tv.sortKeys))
	for i, k := range tv.sortKeys {
		dir := "asc"
		if k.desc {
			dir = "desc"
		}
		parts[i] = tv.d.header[k.col] + " " + dir
	}
	return strings.Join(parts, ", ")
}

// exTableSort: ":sort COL [asc|desc] [COL [asc|desc] …]" sorts the view
// by several columns; ":sort" alone restores the file order; ":sort
// apply" (edit mode) stages the shown order as the new row order.
func exTableSort(e *Editor, tv *tableView, a *exArgs) error {
	w := e.win
	args := splitArgs(a.arg)
	col := e.cursorColumn(w, tv)
	if len(args) == 1 && args[0] == "apply" {
		return e.tableSortApply(w, tv)
	}
	var keys []sortKey
	for _, f := range args {
		switch strings.ToLower(f) {
		case "asc", "ascending":
			if len(keys) == 0 {
				return errors.New("asc / desc follow a column name")
			}
			keys[len(keys)-1].desc = false
			continue
		case "desc", "descending":
			if len(keys) == 0 {
				return errors.New("asc / desc follow a column name")
			}
			keys[len(keys)-1].desc = true
			continue
		}
		c := tv.columnByName(f)
		if c < 0 {
			return fmt.Errorf("no column %q", f)
		}
		keys = append(keys, sortKey{col: c})
	}
	tv.sortKeys = keys
	e.rearrange(w, tv, col)
	e.infof("sorted by %s", tv.sortDescription())
	return nil
}

// tableSortApply makes the current sort the row order of the file
// (staged like any edit).
func (e *Editor) tableSortApply(w *Window, tv *tableView) error {
	if len(tv.sortKeys) == 0 {
		return errors.New("nothing to apply: sort first (s, S, gs, :sort COL …)")
	}
	e.ensureEditing(tv)
	tv.snapshot()
	rows := append([]*trow(nil), tv.d.rows...)
	sort.SliceStable(rows, func(i, j int) bool { return tv.rowLess(rows[i], rows[j]) })
	tv.d.rows = rows
	tv.d.reordered = true
	desc := tv.sortDescription()
	tv.sortKeys = nil
	e.rearrange(w, tv, e.cursorColumn(w, tv))
	e.infof("row order by %s staged: :w applies it to the file", desc)
	return nil
}

// ---------------------------------------------------------------- correlation

// numericColumns keeps the columns with at least one number in the
// given rows.
func (tv *tableView) numericColumns(set cellSet) (keep, dropped []int) {
	for _, c := range set.cols {
		ok := false
		for _, r := range set.rows[c] {
			if _, isNum := table.Number(tv.row(r).cells[c]); isNum {
				ok = true
				break
			}
		}
		if ok {
			keep = append(keep, c)
		} else {
			dropped = append(dropped, c)
		}
	}
	return keep, dropped
}

// commonRows are the rows present in every column of the set (marked
// cells can differ per column).
func commonRows(set cellSet, cols []int) []int {
	count := map[int]int{}
	for _, c := range cols {
		for _, r := range set.rows[c] {
			count[r]++
		}
	}
	var rows []int
	for r, n := range count {
		if n == len(cols) {
			rows = append(rows, r)
		}
	}
	sort.Ints(rows)
	return rows
}

// pairTarget resolves the columns of :corr / :cov: the selection or the
// named columns ("*": all numeric columns).
func (e *Editor) pairTarget(a *exArgs) (*tableView, []int, []int, string, error) {
	tv, set, err := e.statsTarget(a, defColumn)
	if err != nil {
		return nil, nil, nil, "", err
	}
	cols, dropped := tv.numericColumns(set)
	note := ""
	if len(dropped) > 0 {
		names := make([]string, len(dropped))
		for i, c := range dropped {
			names[i] = tv.d.header[c]
		}
		note = "skipped (no numbers): " + strings.Join(names, ", ")
	}
	if len(cols) < 2 {
		return nil, nil, nil, "", errors.New("select two or more numeric columns (c then l to extend, or :corr COL COL …, or * for all)")
	}
	rows := commonRows(set, cols)
	if len(rows) == 0 {
		return nil, nil, nil, "", errors.New("no rows are common to all the columns (marks differ per column)")
	}
	return tv, cols, rows, note, nil
}

// exCorr: ":corr [pearson|spearman|kendall] [cols…]".
func exCorr(e *Editor, a *exArgs) error {
	method := "pearson"
	args := splitArgs(a.arg)
	if len(args) > 0 {
		switch strings.ToLower(args[0]) {
		case "pearson", "spearman", "kendall":
			method, args = strings.ToLower(args[0]), args[1:]
		}
	}
	b := *a
	b.arg = joinArgs(args)
	tv, cols, rows, note, err := e.pairTarget(&b)
	if err != nil {
		return err
	}
	if method == "kendall" && len(rows) > table.KendallMax {
		return fmt.Errorf("kendall is quadratic: %d rows is too many (max %d); use spearman", len(rows), table.KendallMax)
	}
	fn := map[string]func(x, y []string) table.Correlation{
		"pearson": table.Pearson, "spearman": table.Spearman, "kendall": table.Kendall,
	}[method]
	n := len(cols)
	res := make([][]table.Correlation, n)
	cells := make([][]string, n)
	for i, c := range cols {
		cells[i] = tv.cells(rows, c)
	}
	for i := range cols {
		res[i] = make([]table.Correlation, n)
		for j := range cols {
			switch {
			case j < i:
				res[i][j] = res[j][i]
			case i == j:
				st := table.Describe(cells[i])
				res[i][j] = table.Correlation{R: 1, N: st.Numeric, P: 0}
			default:
				res[i][j] = fn(cells[i], cells[j])
			}
		}
	}
	names := make([]string, n)
	for i, c := range cols {
		names[i] = cellText(tv.d.header[c])
	}
	symbol := map[string]string{"pearson": "r", "spearman": "ρ", "kendall": "τ"}[method]
	// every value ends with a 3-column significance field so the
	// numbers stay aligned
	cell := func(i, j int) string {
		c := res[i][j]
		switch {
		case i == j:
			return "1   "
		case math.IsNaN(c.R):
			return "—   "
		}
		return fmt.Sprintf("%.3f%s", c.R, stars(c.P))
	}
	var title []string
	title = append(title, fmt.Sprintf("%s correlation (%s) of %s — pairwise complete rows", strings.ToUpper(method[:1])+method[1:], symbol, rowsLabel(rows, tv)))
	if note != "" {
		title = append(title, note)
	}
	text, spans := e.matrix(title, names, cell, func(i, j int) tcell.Style { return e.corrStyle(res[i][j].R, i == j) })
	// n per pair when not all equal
	sameN := true
	for i := range cols {
		for j := range cols {
			if i != j {
				sameN = sameN && res[i][j].N == res[0][1].N
			}
		}
	}
	var extra []string
	if sameN {
		extra = append(extra, fmt.Sprintf("n = %d pairs", res[0][1].N))
	} else {
		ntext, _ := e.matrix([]string{"pairs used (n)"}, names, func(i, j int) string { return strconv.Itoa(res[i][j].N) }, nil)
		extra = append(extra, ntext)
	}
	extra = append(extra, "* p < 0.05   ** p < 0.01   *** p < 0.001   (two-sided, H0: no correlation)")
	if method == "kendall" {
		extra = append(extra, "p-values: normal approximation with tie corrections")
	}
	e.showStats(text+"\n\n"+strings.Join(extra, "\n"), spans)
	return nil
}

func stars(p float64) string {
	switch {
	case math.IsNaN(p):
		return "   "
	case p < 0.001:
		return "***"
	case p < 0.01:
		return "** "
	case p < 0.05:
		return "*  "
	}
	return "   "
}

// corrStyle colours a coefficient by sign and strength.
func (e *Editor) corrStyle(r float64, diag bool) tcell.Style {
	th := e.theme
	switch {
	case diag || math.IsNaN(r):
		return th.Dim
	case math.Abs(r) < 0.3:
		return th.Dim
	case r > 0 && r >= 0.7:
		return th.Info.Bold(true)
	case r > 0:
		return th.Info
	case r <= -0.7:
		return th.Error.Bold(true)
	}
	return th.Error
}

// matrix lays out a square table of names × names; style (optional)
// colours each value.
func (e *Editor) matrix(title []string, names []string, cell func(i, j int) string, style func(i, j int) tcell.Style) (string, [][]syntax.Span) {
	n := len(names)
	labW := 0
	for _, s := range names {
		labW = max(labW, textWidth(s))
	}
	colW := make([]int, n)
	for j := range names {
		colW[j] = textWidth(names[j])
		for i := range names {
			colW[j] = max(colW[j], textWidth(cell(i, j)))
		}
	}
	var lines []string
	var spans [][]syntax.Span
	for _, t := range title {
		lines = append(lines, t)
		spans = append(spans, nil)
	}
	lines = append(lines, "")
	spans = append(spans, nil)
	head := strings.Repeat(" ", labW)
	for j, s := range names {
		head += "  " + padLeft(s, colW[j])
	}
	lines = append(lines, head)
	spans = append(spans, []syntax.Span{{Start: 0, End: len([]rune(head)), Style: e.theme.Default.Bold(true)}})
	for i := range names {
		line := names[i] + strings.Repeat(" ", labW-textWidth(names[i]))
		sp := []syntax.Span{{Start: 0, End: len([]rune(line)), Style: e.theme.Default.Bold(true)}}
		for j := range names {
			v := padLeft(cell(i, j), colW[j])
			start := len([]rune(line)) + 2
			line += "  " + v
			if style != nil {
				sp = append(sp, syntax.Span{Start: start, End: start + len([]rune(v)), Style: style(i, j)})
			}
		}
		lines = append(lines, line)
		spans = append(spans, sp)
	}
	return strings.Join(lines, "\n"), spans
}

// showStats shows text in *stats*, coloured by spans for its first lines.
func (e *Editor) showStats(text string, spans [][]syntax.Span) {
	e.showOutput("*stats*", text)
	if b := e.win.buf; b.Name == "*stats*" && spans != nil {
		e.static[b.ID] = spans
	}
}

// exCov: ":cov [cols…]" — sample covariance matrix.
func exCov(e *Editor, a *exArgs) error {
	tv, cols, rows, note, err := e.pairTarget(a)
	if err != nil {
		return err
	}
	n := len(cols)
	val := make([][]string, n)
	for i := range cols {
		val[i] = make([]string, n)
	}
	for i, ci := range cols {
		for j := i; j < n; j++ {
			c, _ := table.Covariance(tv.cells(rows, ci), tv.cells(rows, cols[j]))
			val[i][j] = table.Format(c)
			val[j][i] = val[i][j]
		}
	}
	names := make([]string, n)
	for i, c := range cols {
		names[i] = cellText(tv.d.header[c])
	}
	title := []string{"Covariance (sample, n−1) of " + rowsLabel(rows, tv) + " — pairwise complete rows"}
	if note != "" {
		title = append(title, note)
	}
	text, spans := e.matrix(title, names, func(i, j int) string { return val[i][j] }, nil)
	e.showStats(text, spans)
	return nil
}

// exQuantile: ":quantile P [cols…]" with P in 0…1 or a percentage.
func exQuantile(e *Editor, a *exArgs) error {
	args := splitArgs(a.arg)
	if len(args) == 0 {
		return errors.New("usage: :quantile P [columns] (P = 0.9 or 90)")
	}
	p, ok := table.Number(args[0])
	if !ok || p < 0 || p > 100 {
		return fmt.Errorf("bad quantile %q", args[0])
	}
	if p > 1 {
		p /= 100
	}
	b := *a
	b.arg = joinArgs(args[1:])
	tv, set, err := e.statsTarget(&b, defColumn)
	if err != nil {
		return err
	}
	var parts, vals []string
	for _, c := range set.cols {
		var nums []float64
		for _, s := range tv.cells(set.rows[c], c) {
			if f, ok := table.Number(s); ok {
				nums = append(nums, f)
			}
		}
		sort.Float64s(nums)
		v := table.Format(table.Quantile(nums, p))
		vals = append(vals, v)
		parts = append(parts, fmt.Sprintf("q%s(%s) = %s", table.Format(p), tv.d.header[c], v))
	}
	e.regs.Yank(0, Register{Text: strings.Join(vals, "\t")})
	e.infof("%s   [%s]", strings.Join(parts, "   "), setLabel(set, set.cols[0], tv))
	return nil
}

func joinArgs(args []string) string {
	q := make([]string, len(args))
	for i, s := range args {
		if strings.ContainsAny(s, " \t") {
			s = `"` + s + `"`
		}
		q[i] = s
	}
	return strings.Join(q, " ")
}

// ---------------------------------------------------------------- group-by, freq

// exGroupBy: ":groupby KEY[,KEY…] [FN COL …]" over the shown rows; the
// result opens as a new table view (*groupby*).
func exGroupBy(e *Editor, a *exArgs) error {
	w := e.win
	tv := e.tableFor(w.buf)
	if tv == nil {
		return errors.New("not a table view (use :table)")
	}
	args := splitArgs(a.arg)
	if len(args) == 0 {
		return errors.New("usage: :groupby KEY[,KEY…] [FN COLUMN …]   e.g. :groupby region sum units mean price")
	}
	var keys []int
	for _, k := range strings.Split(args[0], ",") {
		c := tv.columnByName(strings.TrimSpace(k))
		if c < 0 {
			return fmt.Errorf("no column %q", k)
		}
		keys = append(keys, c)
	}
	rest := args[1:]
	if len(rest)%2 != 0 {
		return errors.New("aggregates come in pairs: FN COLUMN (e.g. sum units)")
	}
	var aggs []table.Agg
	header := make([]string, 0, len(keys)+1+len(rest)/2)
	for _, k := range keys {
		header = append(header, tv.d.header[k])
	}
	header = append(header, "n")
	for i := 0; i < len(rest); i += 2 {
		fn := strings.ToLower(rest[i])
		if _, err := (table.Stats{}).Value(fn); err != nil {
			return fmt.Errorf("unknown function %q (%s)", rest[i], strings.Join(table.Funcs, ", "))
		}
		c := tv.columnByName(rest[i+1])
		if c < 0 {
			return fmt.Errorf("no column %q", rest[i+1])
		}
		aggs = append(aggs, table.Agg{Fn: fn, Col: c})
		header = append(header, fn+"_"+tv.d.header[c])
	}
	rows := make([][]string, len(tv.order))
	for i := range tv.order {
		rows[i] = tv.row(i).cells
	}
	out := table.GroupBy(rows, keys, aggs)
	lines := make([]string, 0, len(out)+1)
	for _, r := range append([][]string{header}, out...) {
		s, err := table.Encode(r, ',')
		if err != nil {
			return err
		}
		lines = append(lines, s)
	}
	gb := e.scratch("*groupby*", buffer.KindScratch)
	if old := e.tableOfSource(gb); old != nil {
		e.dropTable(old)
	}
	gb.SetText(strings.Join(lines, "\n"))
	gb.ReadOnly = true
	e.showBuffer(gb)
	if err := exTable(e, &exArgs{arg: ","}); err != nil {
		return err
	}
	filtered := ""
	if len(tv.order) != len(tv.d.rows) {
		filtered = fmt.Sprintf(" (filtered: %d of %d rows)", len(tv.order), len(tv.d.rows))
	}
	e.infof("%d groups%s; q shows the CSV text (:w FILE saves it)", len(out), filtered)
	return nil
}

// exFreq: ":freq [col]" — frequency table of a column.
func exFreq(e *Editor, a *exArgs) error {
	tv, set, err := e.statsTarget(a, defColumn)
	if err != nil {
		return err
	}
	col := set.cols[0]
	rows := table.Frequencies(tv.cells(set.rows[col], col))
	valW := textWidth("value")
	for _, r := range rows {
		valW = max(valW, textWidth(cellText(r.Value)))
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Frequencies of %s (%s)\n\n", tv.d.header[col], setLabel(set, col, tv))
	fmt.Fprintf(&sb, "%-*s  %7s  %7s  %7s\n", valW, "value", "count", "%", "cum %")
	for _, r := range rows {
		v := cellText(r.Value)
		fmt.Fprintf(&sb, "%s%s  %7d  %7.1f  %7.1f\n", v, strings.Repeat(" ", valW-textWidth(v)), r.Count, r.Pct, r.CumPct)
	}
	e.showOutput("*stats*", strings.TrimRight(sb.String(), "\n"))
	return nil
}

// ---------------------------------------------------------------- derived columns

// splitAssign cuts "NAME = EXPR" at the first single "=".
func splitAssign(s string) (string, string, bool) {
	rs := []rune(s)
	for i, r := range rs {
		if r != '=' {
			continue
		}
		if i+1 < len(rs) && rs[i+1] == '=' {
			return "", "", false
		}
		if i > 0 && strings.ContainsRune("<>!=", rs[i-1]) {
			return "", "", false
		}
		name := strings.TrimSpace(string(rs[:i]))
		name = strings.Trim(name, "`\"'[]")
		return name, strings.TrimSpace(string(rs[i+1:])), name != ""
	}
	return "", "", false
}

// evalOrder is the order :compute evaluates rows in (indices into
// d.rows): the shown rows as shown, then the hidden ones, so window
// functions (cumsum, lag, …) follow what is on screen.
func (tv *tableView) evalOrder() []int {
	seen := make([]bool, len(tv.d.rows))
	order := make([]int, 0, len(tv.d.rows))
	for _, i := range tv.order {
		seen[i] = true
		order = append(order, i)
	}
	for i := range tv.d.rows {
		if !seen[i] {
			order = append(order, i)
		}
	}
	return order
}

// compileFor compiles an expression over the table's columns; "_" (when
// self >= 0) is column self.
func (tv *tableView) compileFor(src string, self int) (*table.Expr, error) {
	return table.Compile(src, func(n string) (int, bool) {
		if n == "_" && self >= 0 {
			return self, true
		}
		c := tv.columnByName(n)
		return c, c >= 0
	})
}

// exCompute: ":compute NAME = EXPR" adds a column computed for every row
// (":compute! NAME = EXPR" replaces an existing column's values).
// Aggregates in EXPR (mean, std, …) are over all rows.
func exCompute(e *Editor, a *exArgs) error {
	w := e.win
	tv := e.tableFor(w.buf)
	if tv == nil {
		return errors.New("not a table view (use :table)")
	}
	name, src, ok := splitAssign(a.arg)
	if !ok {
		return errors.New("usage: :compute NAME = EXPRESSION   e.g. :compute dev = price - mean(price)")
	}
	x, err := tv.compileFor(src, -1)
	if err != nil {
		return err
	}
	existing := tv.columnByName(name)
	if existing >= 0 && !strings.EqualFold(tv.d.header[existing], name) {
		existing = -1 // a number, not a name
	}
	if existing >= 0 && !a.bang {
		return fmt.Errorf("column %q exists: :compute! replaces its values", name)
	}
	order := tv.evalOrder()
	rows := make([][]string, len(order))
	for k, i := range order {
		rows[k] = tv.d.rows[i].cells
	}
	x.Prepare(rows)
	vals := map[int]string{}
	missing := 0
	for k, i := range order {
		v := x.EvalAt(k)
		if v.Missing {
			missing++
		}
		vals[tv.d.rows[i].id] = v.String()
	}
	e.ensureEditing(tv)
	tv.snapshot()
	col := existing
	if existing >= 0 {
		for i, r := range tv.d.rows {
			if err := tv.setCell(i, existing, vals[r.id]); err != nil {
				tv.rollback()
				return err
			}
		}
	} else {
		// after the right-most column it uses, else at the end
		at := tv.ncols()
		if len(x.Cols) > 0 {
			at = 0
			for _, c := range x.Cols {
				at = max(at, c+1)
			}
		}
		tv.insertColumn(at, name, vals)
		col = at
	}
	e.rearrange(w, tv, col)
	msg := fmt.Sprintf("%s = %s for %d rows", name, x, len(tv.d.rows))
	if missing > 0 {
		msg += fmt.Sprintf(" (%d missing)", missing)
	}
	e.infof("%s; staged", msg)
	return nil
}

// exTransform: ":transform[!] KIND [cols…]" or ":transform[!] EXPR"
// applies a formula to each target column, _ standing for that column
// (KIND is a shortcut, e.g. z = (_ - mean(_)) / std(_)). The result goes
// to NAME_KIND (NAME_t for an expression) next to the column, or with !
// in place. Aggregates and window functions use the target rows, in the
// shown order.
func exTransform(e *Editor, a *exArgs) error {
	args := splitArgs(a.arg)
	if len(args) == 0 {
		var kinds []string
		for _, k := range table.Transforms {
			kinds = append(kinds, k+" = "+table.TransformExprs[k])
		}
		return errors.New("usage: :transform[!] KIND [columns] | EXPRESSION with _ for the column; KIND: " + strings.Join(kinds, ", "))
	}
	src, suffix := strings.TrimSpace(a.arg), "t"
	b := *a
	b.arg = ""
	if expr, ok := table.TransformExprs[strings.ToLower(args[0])]; ok {
		src, suffix = expr, strings.ToLower(args[0])
		b.arg = joinArgs(args[1:])
	}
	tv, set, err := e.statsTarget(&b, defColumn)
	if err != nil {
		return err
	}
	if x, err := tv.compileFor(src, 0); err != nil || !x.Self {
		switch {
		case err != nil && len(args) == 1 && !strings.ContainsAny(src, "()_+-*/^<>="):
			return fmt.Errorf("unknown transform %q (%s), or an expression with _ for the column", args[0], strings.Join(table.Transforms, ", "))
		case err != nil:
			return err
		}
		return errors.New("use _ for the column being transformed, e.g. :transform (_ - mean(_)) / std(_)   (:compute makes a new column from any columns)")
	}
	// columns left to right, so inserting results never shifts the ones still to do
	sort.Ints(set.cols)
	type result struct {
		col    int
		out    []string
		failed int
	}
	var results []result
	for _, c := range set.cols {
		x, err := tv.compileFor(src, c)
		if err != nil {
			return err
		}
		rows := make([][]string, len(set.rows[c]))
		for k, r := range set.rows[c] {
			rows[k] = tv.row(r).cells
		}
		x.Prepare(rows)
		res := result{col: c, out: make([]string, len(rows))}
		defined := false
		for k := range rows {
			v := x.EvalAt(k)
			res.out[k] = v.String()
			switch {
			case !v.Missing:
				defined = true
			case !table.IsMissing(rows[k][c]):
				res.failed++ // a value that gave no result
			}
		}
		if !defined && res.failed > 0 {
			return fmt.Errorf("%s: %s is undefined for this column (do its values vary? are they numbers?)", tv.d.header[c], src)
		}
		results = append(results, res)
	}
	w := e.win
	e.ensureEditing(tv)
	tv.snapshot()
	cursorCol := e.cursorColumn(w, tv)
	var names []string
	failed := 0
	for k := len(results) - 1; k >= 0; k-- {
		res := results[k]
		rows := set.rows[res.col]
		failed += res.failed
		if a.bang {
			for j, r := range rows {
				// in place, nothing is destroyed: missing cells and values
				// without a result (text, log of -5) stay as written
				if i := tv.order[r]; res.out[j] != "" && !table.IsMissing(tv.d.rows[i].cells[res.col]) {
					tv.setCell(i, res.col, res.out[j])
				}
			}
			names = append([]string{tv.d.header[res.col]}, names...)
			continue
		}
		name := tv.uniqueName(tv.d.header[res.col] + "_" + suffix)
		vals := map[int]string{}
		for j, r := range rows {
			vals[tv.row(r).id] = res.out[j]
		}
		tv.insertColumn(res.col+1, name, vals)
		if cursorCol > res.col {
			cursorCol++
		}
		names = append([]string{name}, names...)
	}
	e.rearrange(w, tv, cursorCol)
	msg := fmt.Sprintf("%s: %s (%d rows)", src, strings.Join(names, ", "), len(set.allRows()))
	switch {
	case failed > 0 && a.bang:
		msg += fmt.Sprintf("; %d cells not numeric or undefined, kept as written", failed)
	case failed > 0:
		msg += fmt.Sprintf("; %d cells not numeric or undefined → missing", failed)
	}
	e.infof("%s; staged", msg)
	return nil
}
