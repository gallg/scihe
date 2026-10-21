package table

import (
	"math"
	"strings"
	"testing"
)

func TestExpressions(t *testing.T) {
	header := []string{"units", "price", "region", "units sold", "Fläche_km2", "e"}
	row := []string{"10", "2.5", "north", "7", "891.1", "3"}
	resolve := func(name string) (int, bool) {
		c := (&Table{Header: header}).Column(name)
		return c, c >= 0
	}
	for src, want := range map[string]string{
		"units * price":                 "25",
		"units*price - 5":               "20",
		"-units + 2^3^2":                "502",
		"(units + 2) * 3 % 7":           "1",
		"units / 4":                     "2.5",
		"units / 0":                     "",
		"log(-1)":                       "",
		"round(price * 3.333, 2)":       "8.33",
		"round(2.5)":                    "3",
		"max(units, price, 12)":         "12",
		"min(units, price)":             "2.5",
		"if(region == \"north\", 1, 0)": "1",
		"if(region = 'south', 1, 0)":    "0",
		"region":                        "north",
		"`units sold` + [units sold]":   "14",
		"$1 + $2":                       "12.5",
		"Fläche_km2 / 100":              "8.911",
		"e * 2":                         "6", // a column named e wins over the constant
		"units > 5 and price < 3":       "1",
		"not (units > 5) || 0":          "0",
		"isna(units)":                   "0",
		"coalesce(units, 0)":            "10",
		"sqrt(units * 10)":              "10",
		"pi > 3":                        "1",
		"1e3 + 1.5e-1":                  "1000.15",
		"region + 1":                    "",
	} {
		x, err := Compile(src, resolve)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if got := x.Eval(row).String(); got != want {
			t.Errorf("%q = %q, want %q", src, got, want)
		}
	}
	// missing values propagate
	x, _ := Compile("units * 2", resolve)
	if v := x.Eval([]string{"NA", "", "", "", "", ""}); !v.Missing {
		t.Error("NA * 2 not missing")
	}
	x, _ = Compile("coalesce(units, 0)", resolve)
	if v := x.Eval([]string{"", "", "", "", "", ""}).String(); v != "0" {
		t.Errorf("coalesce of missing = %q", v)
	}
	for _, bad := range []string{"", "units +", "nosuch * 2", "foo(1)", "round()", "(units", "units ) ", "\"open", "[units", "$0", "units @ 2"} {
		if _, err := Compile(bad, resolve); err == nil {
			t.Errorf("%q compiled", bad)
		}
	}
	x, _ = Compile("units * price + `units sold`", resolve)
	if len(x.Cols) != 3 {
		t.Errorf("columns used: %v", x.Cols)
	}
}

// applyExpr evaluates src on a one-column table whose column is x and _.
func applyExpr(t *testing.T, cells []string, src string) []string {
	t.Helper()
	x, err := Compile(src, func(n string) (int, bool) { return 0, n == "x" || n == "_" })
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	rows := make([][]string, len(cells))
	for i, c := range cells {
		rows[i] = []string{c}
	}
	x.Prepare(rows)
	out := make([]string, len(cells))
	for i := range cells {
		out[i] = x.EvalAt(i).String()
	}
	return out
}

func TestTransforms(t *testing.T) {
	cells := strings.Split("2,4,NA,6,x,8", ",")
	for kind, want := range map[string]string{
		"minmax": "0|0.3333333333333333||0.6666666666666666||1",
		"pct":    "10|20||30||40",
		"rank":   "1|2||3||4",
		"cumsum": "2|6||12||20",
		"log2":   "1|2||2.584962500721156||3",
		"abs":    "2|4||6||8",
		"square": "4|16||36||64",
	} {
		if got := strings.Join(applyExpr(t, cells, TransformExprs[kind]), "|"); got != want {
			t.Errorf("%s = %q, want %q", kind, got, want)
		}
	}
	if got := strings.Join(applyExpr(t, cells, TransformExprs["z"]), "|"); !strings.HasPrefix(got, "-1.161895003862225|-0.3872983346207417||0.3872983346207417") {
		t.Errorf("z = %q", got)
	}
	// diff follows the row order: missing neighbours give missing
	if got := strings.Join(applyExpr(t, cells, "diff(_)"), "|"); got != "|2||||" {
		t.Errorf("diff = %q", got)
	}
	if got := applyExpr(t, []string{"-1", "4"}, "sqrt(_)"); got[0] != "" || got[1] != "2" {
		t.Errorf("sqrt %q", got)
	}
	if got := applyExpr(t, []string{"3", "3"}, TransformExprs["z"]); got[0] != "" {
		t.Errorf("z of a constant column %q", got)
	}
}

// References: pandas Series([10, 4, NaN, 7, 1]) — mean 5.5, std
// 3.872983346207417, median 5.5, var 15, sem 1.9364916731037085,
// skew 0, quantile(0.9) 9.100000000000001, rank 4 2 NaN 3 1, cumsum 10 14 NaN 21 22,
// shift(1) NaN 10 4 NaN 7.
func TestAggregatesAndWindows(t *testing.T) {
	cells := []string{"10", "4", "NA", "7", "1"}
	for src, want := range map[string]string{
		"mean(x)":                     "5.5|5.5|5.5|5.5|5.5",
		"x - mean(x)":                 "4.5|-1.5||1.5|-4.5",
		"std(x)":                      "3.872983346207417|3.872983346207417|3.872983346207417|3.872983346207417|3.872983346207417",
		"median(x) + var(x)":          "20.5|20.5|20.5|20.5|20.5",
		"round(sem(x), 6)":            "1.936492|1.936492|1.936492|1.936492|1.936492",
		"count(x) * 10 + n()":         "45|45|45|45|45",
		"min(x) + max(x)":             "11|11|11|11|11",
		"max(x, 5)":                   "10|5||7|5",
		"quantile(x, 0.9)":            "9.100000000000001|9.100000000000001|9.100000000000001|9.100000000000001|9.100000000000001", // numpy: 9.100000000000001
		"quantile(x, 90)":             "||||",                                                                                      // p is a fraction 0…1
		"not x > 5":                   "0|1||0|1",
		"x > 1 and not x > 5":         "0|1|0|0|0", // and / or treat missing as false
		"lag(x, 1.5)":                 "||||",
		"count(if(x > 5, x, \"NA\"))": "2|2|2|2|2",
		"sum(x) / count(x)":           "5.5|5.5|5.5|5.5|5.5",
		"rank(x)":                     "4|2||3|1",
		"cumsum(x)":                   "10|14||21|22",
		"lag(x)":                      "|10|4||7",
		"lead(x, 2)":                  "|7|1||",
		"x ** 2":                      "100|16||49|1",
		"2 ** 3 ** 2":                 "512|512|512|512|512",
		"square(x) - x ^ 2":           "0|0||0|0",
		"sqrt(x ** 2)":                "10|4||7|1",
		"mean(abs(x - mean(x)))":      "3|3|3|3|3",
		"x > mean(x)":                 "1|0||1|0",
		"if(x > median(x), 1, 0)":     "1|0||1|0",
		"cumsum(x) / sum(x)":          "0.45454545454545453|0.6363636363636364||0.9545454545454546|1",
		"rank(x) / count(x)":          "1|0.5||0.75|0.25",
		"x - lag(x)":                  "|-6|||-6",
	} {
		if got := strings.Join(applyExpr(t, cells, src), "|"); got != want {
			t.Errorf("%s = %q, want %q", src, got, want)
		}
	}
	// stand-alone rows: aggregates need Prepare
	x, _ := Compile("mean(x)", func(n string) (int, bool) { return 0, n == "x" })
	if v := x.Eval([]string{"5"}); !v.Missing {
		t.Error("aggregate without Prepare")
	}
	for _, bad := range []string{"mean()", "mean(x, 2)", "quantile(x)", "n(1)", "lag()"} {
		if _, err := Compile(bad, func(n string) (int, bool) { return 0, n == "x" }); err == nil {
			t.Errorf("%q compiled", bad)
		}
	}
}

func TestGroupByAndFrequencies(t *testing.T) {
	rows := [][]string{
		{"north", "apple", "10"}, {"south", "pear", "4"}, {"north", "pear", "NA"},
		{"east", "apple", "7"}, {"north", "apple", "2"}, {"", "fig", "1"},
	}
	got := GroupBy(rows, []int{0}, []Agg{{"sum", 2}, {"mean", 2}, {"count", 2}})
	want := "east,1,7,7,1|north,3,12,6,2|south,1,4,4,1|,1,1,1,1"
	var lines []string
	for _, r := range got {
		lines = append(lines, strings.Join(r, ","))
	}
	if strings.Join(lines, "|") != want {
		t.Fatalf("groupby:\n%s\nwant\n%s", strings.Join(lines, "|"), want)
	}
	got = GroupBy(rows, []int{0, 1}, []Agg{{"sum", 2}})
	if len(got) != 5 || strings.Join(got[1], ",") != "north,apple,2,12" {
		t.Fatalf("two keys: %q", got)
	}
	fr := Frequencies([]string{"b", "a", "b", "", "c", "b", "a"})
	if fr[0].Value != "b" || fr[0].Count != 3 || fr[1].Value != "a" || fr[len(fr)-1].Value != "(missing)" {
		t.Fatalf("freq %+v", fr)
	}
	if last := fr[len(fr)-1]; last.CumPct < 99.999 {
		t.Fatalf("cumulative %v", last.CumPct)
	}
}

func TestReleaseFixes(t *testing.T) {
	// lossless cell values
	for f, want := range map[float64]string{1234567890123456: "1234567890123456", 1234567.891234: "1234567.891234",
		0.000123456789: "0.000123456789", 1e-7: "1e-07", 3.14159265358979: "3.14159265358979"} {
		if got := CellValue(f); got != want {
			t.Errorf("CellValue(%v) = %q, want %q", f, got, want)
		}
	}
	if negZero := math.Copysign(0, -1); Format(negZero) != "0" || CellValue(negZero) != "0" {
		t.Error("negative zero")
	}
	// auto recode keeps codes such as A1 / B1 distinct
	r, _ := Recode([]string{"A1", "B1", "C2"}, "auto", nil)
	if r.Mode != "codes" {
		t.Errorf("A1/B1/C2 recoded as %s", r.Mode)
	}
	r, _ = Recode([]string{"USD 1,000", "USD 2,500"}, "auto", nil)
	if r.Mode != "number" || r.Values[0] != "1000" {
		t.Errorf("currency: %+v", r)
	}
	r, _ = Recode([]string{"1234567890123456", "3,14159265358979"}, "number", nil)
	if r.Values[1] != "3.14159265358979" {
		t.Errorf("precision: %+v", r)
	}
	// decimal-comma column: "1.234" is a thousands grouping
	if f, _ := LooseNumber("1.234", true); f != 1234 {
		t.Errorf("1.234 with decimal comma = %v", f)
	}
	// one-column CSV: an empty cell is written as ""
	if s, _ := Encode([]string{""}, ','); s != `""` {
		t.Errorf("empty single cell %q", s)
	}
	if _, err := Encode([]string{""}, '\t'); err == nil {
		t.Error("empty single TSV cell accepted")
	}
	// constant column: undefined correlation
	if c := Pearson([]string{"0.1", "0.1", "0.1"}, []string{"1", "2", "3"}); !math.IsNaN(c.R) {
		t.Errorf("constant pearson %v", c.R)
	}
	// filters never match missing values (except empty)
	f, _ := ParseFilter("!= 5")
	if f.Match("") || f.Match("NA") || !f.Match("4") {
		t.Error("!= on missing")
	}
	// $N must exist; a column named pi wins over the constant
	resolve := func(name string) (int, bool) {
		c := (&Table{Header: []string{"pi", "x"}}).Column(name)
		return c, c >= 0
	}
	if _, err := Compile("$9", resolve); err == nil {
		t.Error("$9 compiled")
	}
	x, _ := Compile("pi * 2", resolve)
	if got := x.Eval([]string{"5", "1"}).String(); got != "10" {
		t.Errorf("column pi: %q", got)
	}
}
