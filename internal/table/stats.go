package table

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// IsMissing reports whether a cell counts as a missing value.
func IsMissing(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "na", "n/a", "nan", "null", "none", "nil", "-":
		return true
	}
	return false
}

// Number parses a numeric cell ("1,234" thousands separators are not
// accepted: they are ambiguous with decimal commas).
func Number(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// Stats summarises one column (or row) of cells.
type Stats struct {
	Count   int // non-missing cells
	Missing int
	Numeric int // cells parsed as numbers
	Sum     float64
	Mean    float64
	Median  float64
	Min     float64
	Max     float64
	Var     float64 // sample variance (n-1); NaN when Numeric < 2
	Std     float64
	Q1, Q3  float64
	Range   float64 // max − min
	IQR     float64 // Q3 − Q1
	CV      float64 // std / |mean|
	SEM     float64 // standard error of the mean
	CILow   float64 // 95% confidence interval of the mean (t-based)
	CIHigh  float64
	Skew    float64 // adjusted Fisher–Pearson skewness (as pandas)
	Kurt    float64 // excess kurtosis, sample-adjusted (as pandas)
	Unique  int
	Top     string // most frequent value
	TopN    int
}

// Describe computes Stats over cells.
func Describe(cells []string) Stats {
	var st Stats
	var nums []float64
	freq := map[string]int{}
	for _, c := range cells {
		if IsMissing(c) {
			st.Missing++
			continue
		}
		st.Count++
		c = strings.TrimSpace(c)
		freq[c]++
		if f, ok := Number(c); ok {
			nums = append(nums, f)
		}
	}
	st.Unique = len(freq)
	for v, n := range freq {
		if n > st.TopN || (n == st.TopN && v < st.Top) {
			st.Top, st.TopN = v, n
		}
	}
	st.Numeric = len(nums)
	nan := math.NaN()
	st.Mean, st.Median, st.Min, st.Max, st.Var, st.Std, st.Q1, st.Q3 = nan, nan, nan, nan, nan, nan, nan, nan
	st.Range, st.IQR, st.CV, st.SEM, st.CILow, st.CIHigh, st.Skew, st.Kurt = nan, nan, nan, nan, nan, nan, nan, nan
	if len(nums) == 0 {
		return st
	}
	sort.Float64s(nums)
	// Kahan summation keeps long columns accurate
	var sum, comp float64
	for _, f := range nums {
		y := f - comp
		t := sum + y
		comp = (t - sum) - y
		sum = t
	}
	n := float64(len(nums))
	st.Sum = sum
	st.Mean = sum / n
	st.Min, st.Max = nums[0], nums[len(nums)-1]
	st.Median = Quantile(nums, 0.5)
	st.Q1, st.Q3 = Quantile(nums, 0.25), Quantile(nums, 0.75)
	st.Range = st.Max - st.Min
	st.IQR = st.Q3 - st.Q1
	if len(nums) > 1 {
		var m2, m3, m4 float64
		for _, f := range nums {
			d := f - st.Mean
			m2 += d * d
			m3 += d * d * d
			m4 += d * d * d * d
		}
		st.Var = m2 / (n - 1)
		st.Std = math.Sqrt(st.Var)
		st.SEM = st.Std / math.Sqrt(n)
		half := TQuantile(0.975, n-1) * st.SEM
		st.CILow, st.CIHigh = st.Mean-half, st.Mean+half
		if st.Mean != 0 {
			st.CV = st.Std / math.Abs(st.Mean)
		}
		m2, m3, m4 = m2/n, m3/n, m4/n
		if len(nums) > 2 && m2 > 0 {
			g1 := m3 / math.Pow(m2, 1.5)
			st.Skew = math.Sqrt(n*(n-1)) / (n - 2) * g1
		}
		if len(nums) > 3 && m2 > 0 {
			g2 := m4/(m2*m2) - 3
			st.Kurt = ((n+1)*g2 + 6) * (n - 1) / ((n - 2) * (n - 3))
		}
	}
	return st
}

// Quantile returns the q-quantile of sorted values with linear
// interpolation (the default of numpy and pandas).
func Quantile(sorted []float64, q float64) float64 {
	if len(sorted) == 0 {
		return math.NaN()
	}
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return sorted[lo]
	}
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}

// Funcs are the aggregate functions available as commands.
var Funcs = []string{"count", "sum", "mean", "median", "min", "max", "std", "var",
	"range", "iqr", "sem", "cv", "skew", "kurt"}

// Value returns one aggregate of st by name.
func (st Stats) Value(fn string) (float64, error) {
	switch fn {
	case "count":
		return float64(st.Count), nil
	case "sum":
		if st.Numeric == 0 {
			return math.NaN(), nil
		}
		return st.Sum, nil
	case "mean":
		return st.Mean, nil
	case "median":
		return st.Median, nil
	case "min":
		return st.Min, nil
	case "max":
		return st.Max, nil
	case "std":
		return st.Std, nil
	case "var":
		return st.Var, nil
	case "range":
		return st.Range, nil
	case "iqr":
		return st.IQR, nil
	case "sem":
		return st.SEM, nil
	case "cv":
		return st.CV, nil
	case "skew":
		return st.Skew, nil
	case "kurt":
		return st.Kurt, nil
	}
	return 0, fmt.Errorf("unknown function %q", fn)
}

// Format prints a number compactly: integers without decimals, others
// with up to 6 significant decimals; NaN as "—".
func Format(f float64) string {
	f += 0 // -0 → 0
	switch {
	case math.IsNaN(f):
		return "—"
	case f == math.Trunc(f) && math.Abs(f) < 1e15:
		return strconv.FormatFloat(f, 'f', 0, 64)
	}
	s := strconv.FormatFloat(f, 'g', 10, 64)
	if strings.ContainsAny(s, "e") {
		return s
	}
	if strings.Contains(s, ".") {
		// at most 6 decimals
		if i := strings.IndexByte(s, '.'); len(s)-i-1 > 6 {
			s = strconv.FormatFloat(f, 'f', 6, 64)
			s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
		}
	}
	return s
}

// CellValue formats a number written into a cell: every significant
// digit is kept (unlike Format, which is for display), no exponent for
// ordinary magnitudes, no negative zero.
func CellValue(f float64) string {
	f += 0
	if a := math.Abs(f); a != 0 && (a < 1e-4 || a >= 1e21) {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// MaxBins caps the number of histogram buckets.
const MaxBins = 200

// Bin is one histogram bucket.
type Bin struct {
	Label string
	Count int
}

// Histogram buckets numeric cells into bins equal-width bins; columns
// without numbers get the most frequent values instead (at most bins).
func Histogram(cells []string, bins int) (out []Bin, numeric bool) {
	var nums []float64
	freq := map[string]int{}
	for _, c := range cells {
		if IsMissing(c) {
			continue
		}
		c = strings.TrimSpace(c)
		freq[c]++
		if f, ok := Number(c); ok {
			nums = append(nums, f)
		}
	}
	nonMissing := 0
	for _, n := range freq {
		nonMissing += n
	}
	bins = min(bins, MaxBins)
	if len(nums) > 0 && len(nums)*2 >= nonMissing {
		if bins <= 0 {
			bins = int(math.Ceil(math.Log2(float64(len(nums))))) + 1 // Sturges
			bins = min(max(bins, 1), 20)
		}
		lo, hi := nums[0], nums[0]
		for _, f := range nums {
			lo, hi = math.Min(lo, f), math.Max(hi, f)
		}
		if lo == hi {
			return []Bin{{Label: Format(lo), Count: len(nums)}}, true
		}
		// scaled so that a range wider than MaxFloat64 does not overflow
		width := (hi/2 - lo/2) / float64(bins) * 2
		counts := make([]int, bins)
		for _, f := range nums {
			i := int((f/2 - lo/2) / (width / 2))
			if i >= bins || i < 0 {
				i = bins - 1
			}
			counts[i]++
		}
		for i, n := range counts {
			a, b := lo+float64(i)*width, lo+float64(i+1)*width
			closing := ")"
			if i == bins-1 {
				closing = "]"
			}
			out = append(out, Bin{Label: "[" + Format(a) + ", " + Format(b) + closing, Count: n})
		}
		return out, true
	}
	if bins <= 0 {
		bins = 15
	}
	type kv struct {
		k string
		n int
	}
	var kvs []kv
	for k, n := range freq {
		kvs = append(kvs, kv{k, n})
	}
	sort.Slice(kvs, func(i, j int) bool {
		if kvs[i].n != kvs[j].n {
			return kvs[i].n > kvs[j].n
		}
		return kvs[i].k < kvs[j].k
	})
	rest := 0
	for i, x := range kvs {
		if i < bins {
			out = append(out, Bin{Label: x.k, Count: x.n})
		} else {
			rest += x.n
		}
	}
	if rest > 0 {
		out = append(out, Bin{Label: fmt.Sprintf("(%d others)", len(kvs)-bins), Count: rest})
	}
	return out, false
}
