package table

import "math"

// Probability distributions needed for p-values and confidence
// intervals, using only the math package. The regularized incomplete
// beta function follows the continued fraction of Numerical Recipes
// (Lentz's method), computed in log space for stability.

// betaInc is the regularized incomplete beta function I_x(a, b).
func betaInc(x, a, b float64) float64 {
	switch {
	case math.IsNaN(x) || a <= 0 || b <= 0:
		return math.NaN()
	case x <= 0:
		return 0
	case x >= 1:
		return 1
	}
	la, _ := math.Lgamma(a)
	lb, _ := math.Lgamma(b)
	lab, _ := math.Lgamma(a + b)
	front := math.Exp(lab - la - lb + a*math.Log(x) + b*math.Log1p(-x))
	// the continued fraction converges fast for x < (a+1)/(a+b+2)
	if x < (a+1)/(a+b+2) {
		return front * betaCF(x, a, b) / a
	}
	return 1 - front*betaCF(1-x, b, a)/b
}

func betaCF(x, a, b float64) float64 {
	const (
		maxIter = 300
		eps     = 1e-15
		tiny    = 1e-300
	)
	qab, qap, qam := a+b, a+1, a-1
	c, d := 1.0, 1-qab*x/qap
	if math.Abs(d) < tiny {
		d = tiny
	}
	d = 1 / d
	h := d
	for m := 1; m <= maxIter; m++ {
		fm := float64(m)
		m2 := 2 * fm
		aa := fm * (b - fm) * x / ((qam + m2) * (a + m2))
		d = 1 + aa*d
		if math.Abs(d) < tiny {
			d = tiny
		}
		c = 1 + aa/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		d = 1 / d
		h *= d * c
		aa = -(a + fm) * (qab + fm) * x / ((a + m2) * (qap + m2))
		d = 1 + aa*d
		if math.Abs(d) < tiny {
			d = tiny
		}
		c = 1 + aa/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		d = 1 / d
		del := d * c
		h *= del
		if math.Abs(del-1) < eps {
			break
		}
	}
	return h
}

// TTwoSided is the two-sided p-value P(|T| >= |t|) of Student's t with
// df degrees of freedom.
func TTwoSided(t, df float64) float64 {
	if math.IsNaN(t) || df <= 0 {
		return math.NaN()
	}
	if math.IsInf(t, 0) {
		return 0
	}
	return betaInc(df/(df+t*t), df/2, 0.5)
}

// TCDF is P(T <= t) for Student's t with df degrees of freedom.
func TCDF(t, df float64) float64 {
	p := TTwoSided(t, df) / 2
	if t > 0 {
		return 1 - p
	}
	return p
}

// TQuantile is the t value with P(T <= t) = p (bisection on TCDF).
func TQuantile(p, df float64) float64 {
	if p <= 0 || p >= 1 || df <= 0 {
		return math.NaN()
	}
	lo, hi := -1.0, 1.0
	for TCDF(lo, df) > p {
		lo *= 2
	}
	for TCDF(hi, df) < p {
		hi *= 2
	}
	for i := 0; i < 200 && hi-lo > 1e-12*math.Max(1, math.Abs(hi)); i++ {
		mid := (lo + hi) / 2
		if TCDF(mid, df) < p {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

// NormalTwoSided is the two-sided p-value of a standard normal z.
func NormalTwoSided(z float64) float64 {
	return math.Erfc(math.Abs(z) / math.Sqrt2)
}
