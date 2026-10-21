package table

import (
	"math"
	"sort"
)

// Correlation of two columns over the rows where both are numbers
// ("pairwise complete observations").
type Correlation struct {
	R float64 // coefficient (NaN when undefined)
	N int     // pairs used
	P float64 // two-sided p-value of R = 0 (NaN when undefined)
}

// Pairs returns the rows where both cells are numbers.
func Pairs(x, y []string) (xs, ys []float64) {
	for i := range x {
		if i >= len(y) {
			break
		}
		a, ok1 := Number(x[i])
		b, ok2 := Number(y[i])
		if ok1 && ok2 {
			xs = append(xs, a)
			ys = append(ys, b)
		}
	}
	return xs, ys
}

func constant(v []float64) bool {
	for _, f := range v[1:] {
		if f != v[0] {
			return false
		}
	}
	return true
}

func meanOf(v []float64) float64 {
	s := 0.0
	for _, f := range v {
		s += f
	}
	return s / float64(len(v))
}

// pearson computes r with two passes (centred sums).
func pearson(xs, ys []float64) float64 {
	n := len(xs)
	if n < 2 {
		return math.NaN()
	}
	if constant(xs) || constant(ys) {
		return math.NaN()
	}
	mx, my := meanOf(xs), meanOf(ys)
	var sxy, sxx, syy float64
	for i := range xs {
		dx, dy := xs[i]-mx, ys[i]-my
		sxy += dx * dy
		sxx += dx * dx
		syy += dy * dy
	}
	if sxx == 0 || syy == 0 {
		return math.NaN()
	}
	r := sxy / math.Sqrt(sxx*syy)
	return math.Max(-1, math.Min(1, r))
}

// tTest gives the p-value of a correlation via t = r√((n−2)/(1−r²)).
func tTest(r float64, n int) float64 {
	if math.IsNaN(r) || n < 3 {
		return math.NaN()
	}
	if math.Abs(r) >= 1 {
		return 0
	}
	df := float64(n - 2)
	return TTwoSided(r*math.Sqrt(df/(1-r*r)), df)
}

// Pearson is the linear correlation coefficient.
func Pearson(x, y []string) Correlation {
	xs, ys := Pairs(x, y)
	r := pearson(xs, ys)
	return Correlation{R: r, N: len(xs), P: tTest(r, len(xs))}
}

// Ranks returns 1-based ranks; ties get the average of their ranks.
func Ranks(v []float64) []float64 {
	idx := make([]int, len(v))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return v[idx[a]] < v[idx[b]] })
	r := make([]float64, len(v))
	for i := 0; i < len(idx); {
		j := i
		for j+1 < len(idx) && v[idx[j+1]] == v[idx[i]] {
			j++
		}
		avg := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			r[idx[k]] = avg
		}
		i = j + 1
	}
	return r
}

// Spearman is the rank correlation (Pearson's r of the ranks); its
// p-value uses the same t approximation as scipy.
func Spearman(x, y []string) Correlation {
	xs, ys := Pairs(x, y)
	r := pearson(Ranks(xs), Ranks(ys))
	return Correlation{R: r, N: len(xs), P: tTest(r, len(xs))}
}

// KendallMax is the largest number of pairs Kendall's tau is computed
// for (the algorithm is quadratic).
const KendallMax = 20000

// Kendall is tau-b (adjusted for ties) with the normal approximation of
// its variance under independence (with tie corrections).
func Kendall(x, y []string) Correlation {
	xs, ys := Pairs(x, y)
	n := len(xs)
	if n < 2 || n > KendallMax {
		return Correlation{R: math.NaN(), N: n, P: math.NaN()}
	}
	var conc, disc, tx, ty float64 // tx/ty: pairs tied only in x / only in y
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			dx, dy := xs[i]-xs[j], ys[i]-ys[j]
			switch {
			case dx == 0 && dy == 0:
			case dx == 0:
				tx++
			case dy == 0:
				ty++
			case dx*dy > 0:
				conc++
			default:
				disc++
			}
		}
	}
	den := math.Sqrt((conc + disc + tx) * (conc + disc + ty))
	if den == 0 {
		return Correlation{R: math.NaN(), N: n, P: math.NaN()}
	}
	tau := (conc - disc) / den
	// variance of S = C − D with ties (Kendall 1970), as in scipy
	tieTerms := func(v []float64) (t0, t1, t2 float64) {
		s := append([]float64(nil), v...)
		sort.Float64s(s)
		for i := 0; i < len(s); {
			j := i
			for j+1 < len(s) && s[j+1] == s[i] {
				j++
			}
			c := float64(j - i + 1)
			t0 += c * (c - 1) / 2
			t1 += c * (c - 1) * (c - 2)
			t2 += c * (c - 1) * (2*c + 5)
			i = j + 1
		}
		return
	}
	xt0, xt1, xt2 := tieTerms(xs)
	yt0, yt1, yt2 := tieTerms(ys)
	fn := float64(n)
	v := (fn*(fn-1)*(2*fn+5)-xt2-yt2)/18 +
		xt1*yt1/(9*fn*(fn-1)*(fn-2)) +
		2*xt0*yt0/(fn*(fn-1))
	p := math.NaN()
	if v > 0 {
		p = NormalTwoSided((conc - disc) / math.Sqrt(v))
	}
	return Correlation{R: tau, N: n, P: p}
}

// Covariance is the sample covariance (n−1) over pairwise rows.
func Covariance(x, y []string) (float64, int) {
	xs, ys := Pairs(x, y)
	n := len(xs)
	if n < 2 {
		return math.NaN(), n
	}
	mx, my := meanOf(xs), meanOf(ys)
	s := 0.0
	for i := range xs {
		s += (xs[i] - mx) * (ys[i] - my)
	}
	return s / float64(n-1), n
}
