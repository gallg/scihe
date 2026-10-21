package table

import (
	"math"
	"strings"
	"testing"
)

// Reference values computed with scipy 1.18 / pandas (scipy.stats
// pearsonr, spearmanr, kendalltau(method="asymptotic"), t.ppf, t.sf;
// pandas Series.skew / kurt).

func near(t *testing.T, what string, got, want, tol float64) {
	t.Helper()
	if math.IsNaN(got) || math.Abs(got-want) > tol*math.Max(1, math.Abs(want)) {
		t.Errorf("%s = %.15g, want %.15g", what, got, want)
	}
}

var (
	cx = strings.Split("2.1,3.4,1.9,5.6,4.4,3.3,2.8,6.1,4.9,3.0,NA,7.2", ",")
	cy = strings.Split("1.0,2.2,1.5,4.0,3.9,2.0,2.1,5.5,3.8,2.9,1.0,", ",")
)

func TestDistributions(t *testing.T) {
	near(t, "t.ppf(.975,10)", TQuantile(0.975, 10), 2.228138851986274, 1e-9)
	near(t, "t.ppf(.975,1)", TQuantile(0.975, 1), 12.706204736174694, 1e-9)
	near(t, "t.ppf(.975,30)", TQuantile(0.975, 30), 2.0422724563012378, 1e-9)
	near(t, "t.ppf(.9,3.5)", TQuantile(0.9, 3.5), 1.5765766051364052, 1e-9)
	near(t, "2·t.sf(2,10)", TTwoSided(2, 10), 0.07338803477074037, 1e-9)
	near(t, "2·t.sf(.5,2)", TTwoSided(0.5, 2), 0.6666666666666667, 1e-9)
	near(t, "2·t.sf(8,40)", TTwoSided(8, 40), 7.905084110104538e-10, 1e-6)
	near(t, "normal", NormalTwoSided(1.959963984540054), 0.05, 1e-12)
}

func TestCorrelations(t *testing.T) {
	p := Pearson(cx, cy)
	if p.N != 10 {
		t.Fatalf("pairs %d", p.N)
	}
	near(t, "pearson r", p.R, 0.9454112939719106, 1e-12)
	near(t, "pearson p", p.P, 3.636236031606537e-05, 1e-7)
	s := Spearman(cx, cy)
	near(t, "spearman r", s.R, 0.9151515151515152, 1e-12)
	near(t, "spearman p", s.P, 0.00020447240614883226, 1e-7)
	k := Kendall(cx, cy)
	near(t, "kendall tau", k.R, 0.7777777777777777, 1e-12)
	near(t, "kendall p", k.P, 0.001745118699528905, 1e-7)
	tx := strings.Split("1,2,2,3,3,3,4,5,5,6", ",")
	ty := strings.Split("2,1,3,3,4,4,6,5,7,7", ",")
	k = Kendall(tx, ty)
	near(t, "kendall tau-b ties", k.R, 0.829515062006253, 1e-12)
	near(t, "kendall p ties", k.P, 0.001645107673201864, 1e-7)
	s = Spearman(tx, ty)
	near(t, "spearman ties", s.R, 0.92838945510794, 1e-12)
	near(t, "spearman p ties", s.P, 0.00010545526421635557, 1e-7)
	c, n := Covariance(cx, cy)
	if n != 10 {
		t.Fatalf("cov pairs %d", n)
	}
	near(t, "cov", c, 1.885, 1e-12)
	// constant column: undefined
	if r := Pearson([]string{"1", "1", "1"}, []string{"1", "2", "3"}); !math.IsNaN(r.R) || !math.IsNaN(r.P) {
		t.Fatalf("constant: %+v", r)
	}
	if got := Ranks([]float64{10, 20, 10, 30}); got[0] != 1.5 || got[2] != 1.5 || got[3] != 4 {
		t.Fatalf("ranks %v", got)
	}
}

func TestDescribeExtras(t *testing.T) {
	st := Describe(strings.Split("2.1,3.4,1.9,5.6,4.4,3.3,2.8,6.1,4.9,3.0,7.2,NA", ","))
	near(t, "skew", st.Skew, 0.529424778602932, 1e-12)
	near(t, "kurt", st.Kurt, -0.7585861733301269, 1e-12)
	near(t, "sem", st.SEM, 0.5173837554593526, 1e-12)
	near(t, "ci low", st.CILow, 2.910833516710815, 1e-9)
	near(t, "ci high", st.CIHigh, 5.216439210561912, 1e-9)
	near(t, "range", st.Range, 5.3, 1e-12)
	near(t, "iqr", st.IQR, st.Q3-st.Q1, 0)
	near(t, "cv", st.CV, st.Std/st.Mean, 1e-15)
	for _, fn := range Funcs {
		if _, err := st.Value(fn); err != nil {
			t.Errorf("Value(%s): %v", fn, err)
		}
	}
}
