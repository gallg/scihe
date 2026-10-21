package table

import (
	"math"
	"strings"
	"testing"
)

func TestSniff(t *testing.T) {
	cases := map[string]rune{
		"a,b,c\n1,2,3\n":                  ',',
		"a;b;c\n1,5;2,5;3\n":              ';',
		"a\tb\n1\t2\n":                    '\t',
		"a|b|c\n1|2|3\n":                  '|',
		"name,note\nx,\"a;b;c;d;e\"\n":    ',',
		"x;y\n\"1,2,3,4\";5\n\"6,7\";8\n": ';',
	}
	for text, want := range cases {
		if got := Sniff("data.csv", text); got != want {
			t.Errorf("%q: got %q want %q", text, got, want)
		}
	}
	if Sniff("x.tsv", "a,b,c\n") != '\t' {
		t.Error("extension should decide for .tsv")
	}
}

func TestParse(t *testing.T) {
	tb := Parse("name,qty,note\nfoo,1,\"multi\nline\"\nbar,2\n\nbaz,3,x,extra\n", ',', true)
	if tb.Err != nil {
		t.Fatal(tb.Err)
	}
	if strings.Join(tb.Header, "|") != "name|qty|note|4" {
		t.Fatalf("header %q", tb.Header)
	}
	if len(tb.Rows) != 3 || tb.Rows[0][2] != "multi\nline" || tb.Rows[1][2] != "" || tb.Rows[2][3] != "extra" {
		t.Fatalf("rows %q", tb.Rows)
	}
	if tb.Lines[0] != 1 || tb.Lines[1] != 3 || tb.Lines[2] != 5 {
		t.Fatalf("lines %v", tb.Lines)
	}
	tsv := Parse("a\tb\n\"x\t2\n", '\t', false)
	if len(tsv.Rows) != 2 || tsv.Rows[1][0] != `"x` || tsv.Header[1] != "2" {
		t.Fatalf("tsv %q %q", tsv.Header, tsv.Rows)
	}
	if tb.Column("QTY") != 1 || tb.Column("3") != 2 || tb.Column("9") != -1 || tb.Column("nope") != -1 {
		t.Fatal("column lookup")
	}
}

func TestDescribe(t *testing.T) {
	st := Describe([]string{"1", "2", "3", "4", "NA", "", "x"})
	if st.Count != 5 || st.Missing != 2 || st.Numeric != 4 || st.Unique != 5 {
		t.Fatalf("counts %+v", st)
	}
	if st.Sum != 10 || st.Mean != 2.5 || st.Median != 2.5 || st.Min != 1 || st.Max != 4 {
		t.Fatalf("values %+v", st)
	}
	if math.Abs(st.Var-5.0/3) > 1e-12 || math.Abs(st.Std-math.Sqrt(5.0/3)) > 1e-12 {
		t.Fatalf("var %v std %v", st.Var, st.Std)
	}
	if st.Q1 != 1.75 || st.Q3 != 3.25 {
		t.Fatalf("quartiles %v %v", st.Q1, st.Q3)
	}
	empty := Describe([]string{"a", "b", "a"})
	if v, _ := empty.Value("sum"); !math.IsNaN(v) || empty.Top != "a" || empty.TopN != 2 {
		t.Fatalf("text column %+v", empty)
	}
	if v, _ := Describe([]string{"5"}).Value("std"); !math.IsNaN(v) {
		t.Fatal("std of one value should be NaN")
	}
}

func TestFormat(t *testing.T) {
	for f, want := range map[float64]string{3: "3", -2: "-2", 0.1 + 0.2: "0.3", 2.0 / 3: "0.666667", 1e20: "1e+20", math.NaN(): "—", 1234.5: "1234.5"} {
		if got := Format(f); got != want {
			t.Errorf("Format(%v) = %q, want %q", f, got, want)
		}
	}
}

func TestHistogram(t *testing.T) {
	h, num := Histogram([]string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "10"}, 2)
	if !num || len(h) != 2 || h[0].Count != 5 || h[1].Count != 5 || h[0].Label != "[0, 5)" || h[1].Label != "[5, 10]" {
		t.Fatalf("numeric %+v", h)
	}
	h, num = Histogram([]string{"b", "a", "b", "c", "b", "a"}, 2)
	if num || len(h) != 3 || h[0] != (Bin{"b", 3}) || h[1] != (Bin{"a", 2}) || h[2].Count != 1 {
		t.Fatalf("categorical %+v", h)
	}
}

func TestFilter(t *testing.T) {
	for expr, cases := range map[string]map[string]bool{
		">10":    {"11": true, "10": false, "abc": false, "": false},
		"<=2.5":  {"2.5": true, "3": false},
		"=foo":   {"foo": true, " foo ": true, "Foo": false},
		"!=foo":  {"bar": true, "foo": false},
		"~^a.c$": {"abc": true, "abcd": false},
		"!~x":    {"abc": true, "xyz": false},
		"oo":     {"FOO": true, "bar": false},
		"empty":  {"": true, "NA": true, "1": false},
		"!empty": {"": false, "1": true},
		"= 3":    {"3.0": true, "4": false},
		`="a b"`: {"a b": true},
		">=b":    {"c": true, "a": false},
	} {
		f, err := ParseFilter(expr)
		if err != nil {
			t.Fatalf("%q: %v", expr, err)
		}
		for cell, want := range cases {
			if got := f.Match(cell); got != want {
				t.Errorf("%q on %q: got %v", expr, cell, got)
			}
		}
	}
	if _, err := ParseFilter("~("); err == nil {
		t.Error("bad regexp accepted")
	}
}

func TestLooseNumberAndRecode(t *testing.T) {
	for _, c := range []struct {
		s     string
		comma bool
		want  float64
		ok    bool
	}{
		{"3.5", false, 3.5, true}, {"891,1", true, 891.1, true}, {"1.234,5", true, 1234.5, true},
		{"1,234.5", false, 1234.5, true}, {"1 234", false, 1234, true}, {"12%", false, 0.12, true},
		{"€3.50", false, 3.5, true}, {"-€3", false, -3, true}, {"3.5 kg", false, 3.5, true},
		{"USD 1,000", false, 1000, true}, {"−2,5", true, -2.5, true}, {"abc", false, 0, false}, {"%", false, 0, false},
	} {
		got, ok := LooseNumber(c.s, c.comma)
		if ok != c.ok || (ok && math.Abs(got-c.want) > 1e-12) {
			t.Errorf("LooseNumber(%q, %v) = %v %v, want %v %v", c.s, c.comma, got, ok, c.want, c.ok)
		}
	}
	if !decimalComma([]string{"1", "891,1"}) || decimalComma([]string{"1,234", "5"}) || !decimalComma([]string{"1.234,5"}) || decimalComma([]string{"1,234.5"}) {
		t.Error("decimalComma")
	}
	join := func(r *Recoding) string { return strings.Join(r.Values, "|") }
	r, _ := Recode([]string{"891,1", "15,9", "", "1.234,5"}, "auto", nil)
	if r.Mode != "number" || join(r) != "891.1|15.9||1234.5" || r.Failed != 0 {
		t.Fatalf("auto number: %+v", r)
	}
	r, _ = Recode([]string{"Yes", "no", "NA", "y"}, "auto", nil)
	if r.Mode != "bool" || join(r) != "1|0||1" {
		t.Fatalf("auto bool: %+v", r)
	}
	r, _ = Recode([]string{"south", "north", "east", "north", ""}, "auto", nil)
	if r.Mode != "codes" || join(r) != "3|2|1|2|" || r.Mapping[1] != (Code{"north", 2, 2}) {
		t.Fatalf("auto codes: %+v", r)
	}
	r, _ = Recode([]string{"10", "2", "x"}, "codes", nil)
	if join(r) != "2|1|3" {
		t.Fatalf("codes numeric first: %+v", r)
	}
	r, _ = Recode([]string{"3", "x", "4"}, "number", nil)
	if join(r) != "3||4" || r.Failed != 1 {
		t.Fatalf("number with failure: %+v", r)
	}
	m, err := ParseMapping([]string{"north=1", "south=2", "a=b=3"})
	if err != nil || m["a=b"] != 3 {
		t.Fatalf("mapping %v %v", m, err)
	}
	r, _ = Recode([]string{"north", "south", "west"}, "map", m)
	if join(r) != "1|2|" || r.Failed != 1 {
		t.Fatalf("map: %+v", r)
	}
	if _, err := ParseMapping([]string{"north"}); err == nil {
		t.Error("bad mapping accepted")
	}
	if _, err := Recode(nil, "nope", nil); err == nil {
		t.Error("bad mode accepted")
	}
}

func TestEncodeAndRecordLines(t *testing.T) {
	for cells, want := range map[string]string{"a|b": "a,b", "x,y|z": `"x,y",z`, `say "hi"|1`: `"say ""hi""",1`, "two\nlines|": "\"two\nlines\","} {
		got, err := Encode(strings.Split(cells, "|"), ',')
		if err != nil || got != want {
			t.Errorf("Encode(%q) = %q %v, want %q", cells, got, err, want)
		}
	}
	if _, err := Encode([]string{"a\tb"}, '\t'); err == nil {
		t.Error("tab in TSV cell accepted")
	}
	tb := Parse("\ufeffh1,h2\n\na,\"x\ny\"\nb,2", ',', true)
	if !tb.BOM || tb.HeaderLine != 0 || tb.HeaderEnd != 1 || tb.Lines[0] != 2 || tb.Ends[0] != 4 || tb.Lines[1] != 4 || tb.Ends[1] != 5 {
		t.Fatalf("lines %+v", tb)
	}
}
