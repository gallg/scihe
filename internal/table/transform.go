package table

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Transforms are the named shortcuts of :transform: expressions in which
// _ is the transformed column.
var Transforms = []string{"z", "minmax", "log", "log10", "log2", "sqrt", "square", "exp", "abs", "rank", "pct", "cumsum", "diff"}

// TransformExprs gives the expression of each shortcut.
var TransformExprs = map[string]string{
	"z": "(_ - mean(_)) / std(_)", "minmax": "(_ - min(_)) / (max(_) - min(_))", "pct": "_ / sum(_) * 100",
	"log": "log(_)", "log10": "log10(_)", "log2": "log2(_)", "sqrt": "sqrt(_)", "square": "_ ** 2",
	"exp": "exp(_)", "abs": "abs(_)", "rank": "rank(_)", "cumsum": "cumsum(_)", "diff": "diff(_)",
}

// Agg is one aggregate of a group-by: Fn (a name of Funcs) of column Col.
type Agg struct {
	Fn  string
	Col int
}

// GroupBy groups rows by the key columns (sorted by key) and computes
// the aggregates per group. Each output row is: keys…, n, aggregates….
func GroupBy(rows [][]string, keys []int, aggs []Agg) [][]string {
	type group struct {
		key  []string
		rows [][]string
	}
	byKey := map[string]*group{}
	var order []*group
	for _, r := range rows {
		k := make([]string, len(keys))
		for i, c := range keys {
			k[i] = strings.TrimSpace(r[c])
		}
		id := strings.Join(k, "\x00")
		g := byKey[id]
		if g == nil {
			g = &group{key: k}
			byKey[id] = g
			order = append(order, g)
		}
		g.rows = append(g.rows, r)
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i].key, order[j].key
		for k := range a {
			if a[k] != b[k] {
				// missing keys last
				if IsMissing(a[k]) != IsMissing(b[k]) {
					return !IsMissing(a[k])
				}
				return lessCell(a[k], b[k])
			}
		}
		return false
	})
	out := make([][]string, 0, len(order))
	for _, g := range order {
		row := append(append([]string(nil), g.key...), fmt.Sprint(len(g.rows)))
		for _, a := range aggs {
			cells := make([]string, len(g.rows))
			for i, r := range g.rows {
				cells[i] = r[a.Col]
			}
			v, _ := Describe(cells).Value(a.Fn)
			if math.IsNaN(v) {
				row = append(row, "")
			} else {
				row = append(row, CellValue(v))
			}
		}
		out = append(out, row)
	}
	return out
}

// FreqRow is one line of a frequency table.
type FreqRow struct {
	Value       string
	Count       int
	Pct, CumPct float64
	Missing     bool
}

// Frequencies counts the values of cells, most frequent first; missing
// values come last (and are included in the percentages).
func Frequencies(cells []string) []FreqRow {
	counts := map[string]int{}
	miss := 0
	for _, c := range cells {
		if IsMissing(c) {
			miss++
			continue
		}
		counts[strings.TrimSpace(c)]++
	}
	var rows []FreqRow
	for v, n := range counts {
		rows = append(rows, FreqRow{Value: v, Count: n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return lessCell(rows[i].Value, rows[j].Value)
	})
	if miss > 0 {
		rows = append(rows, FreqRow{Value: "(missing)", Count: miss, Missing: true})
	}
	total := float64(len(cells))
	cum := 0.0
	for i := range rows {
		rows[i].Pct = float64(rows[i].Count) / total * 100
		cum += rows[i].Pct
		rows[i].CumPct = cum
	}
	return rows
}
