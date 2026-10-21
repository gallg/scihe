package table

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// A small expression language for computed columns (:compute) and
// column transforms (:transform):
//
//	numbers 3 2.5 1e-3, text "north" or 'north', constants pi e
//	columns by name (units, Fläche_km2), `name with spaces`, [name] or $3
//	+ - * / % and ^ or ** (power), comparisons < <= > >= == = != (1 or 0),
//	and or not (also && || !), parentheses
//	row functions: abs sqrt square exp log (natural) ln log10 log2 floor
//	  ceil round(x[, digits]) pow(x, y) min(a, b, …) max(a, b, …)
//	  if(cond, a, b) isna(x) coalesce(a, b, …)
//	aggregates, one value over all the rows of the command: mean median
//	  sum count std var sem range iqr skew kurt quantile(x, p) n() and
//	  min(x) / max(x) with one argument — x can be any expression, so
//	  (x - mean(x)) / std(x) standardizes a column
//	window functions, following the order of the rows: rank(x)
//	  cumsum(x) diff(x) lag(x[, k]) lead(x[, k])
//
// A missing or non-numeric operand makes an arithmetic result missing;
// so do division by zero and math errors (log of a negative number).

// Value is the result of evaluating an expression on a row.
type Value struct {
	Num     float64
	Text    string
	IsText  bool
	Missing bool
}

func num(f float64) Value {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return Value{Missing: true}
	}
	return Value{Num: f}
}

var missing = Value{Missing: true}

// number reads a value as a number (text that parses counts).
func (v Value) number() (float64, bool) {
	if v.Missing {
		return 0, false
	}
	if v.IsText {
		return Number(v.Text)
	}
	return v.Num, true
}

// String formats a value as a cell.
func (v Value) String() string {
	switch {
	case v.Missing:
		return ""
	case v.IsText:
		return v.Text
	}
	return CellValue(v.Num)
}

func truth(v Value) bool {
	if f, ok := v.number(); ok {
		return f != 0
	}
	return v.IsText && v.Text != ""
}

func boolean(b bool) Value {
	if b {
		return Value{Num: 1}
	}
	return Value{Num: 0}
}

// Expr is a compiled expression.
type Expr struct {
	src  string
	Self bool // the expression uses _ (the transformed column)
	eval func(rc rowCtx) Value
	pre  []*preNode // aggregates and window functions, inner ones first
	rows [][]string // the rows of the last Prepare
	Cols []int      // columns used
}

// rowCtx is the row being evaluated: its cells and, after Prepare, its
// position (-1 for a stand-alone row).
type rowCtx struct {
	cells []string
	i     int
}

// preNode is a function computed over all rows before the row-by-row
// pass: an aggregate (one value) or a window function (one per row).
type preNode struct {
	fn    string
	args  []node
	value Value   // aggregate result
	per   []Value // window results, by row
}

// Prepare computes the aggregates (mean, sum, …) and window functions
// (rank, cumsum, lag, …) over rows, in this order; EvalAt then evaluates
// row i of them. An expression without such functions needs no Prepare.
func (x *Expr) Prepare(rows [][]string) {
	x.rows = rows
	for _, p := range x.pre {
		p.prepare(rows)
	}
}

// EvalAt evaluates row i of the rows given to Prepare.
func (x *Expr) EvalAt(i int) Value {
	if i < 0 || i >= len(x.rows) {
		return missing
	}
	return x.eval(rowCtx{x.rows[i], i})
}

// Eval evaluates a stand-alone row: window functions are missing, and
// aggregates hold the values of the last Prepare (missing before one).
func (x *Expr) Eval(row []string) Value { return x.eval(rowCtx{row, -1}) }

func (x *Expr) String() string { return x.src }

type token struct {
	kind string // num str ident col op ( ) ,
	text string
	num  float64
	col  int
}

func lex(s string) ([]token, error) {
	var toks []token
	rs := []rune(s)
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case unicode.IsDigit(r) || r == '.' && i+1 < len(rs) && unicode.IsDigit(rs[i+1]):
			j := i
			for j < len(rs) && (unicode.IsDigit(rs[j]) || rs[j] == '.' ||
				(rs[j] == 'e' || rs[j] == 'E') && j+1 < len(rs) && (unicode.IsDigit(rs[j+1]) || rs[j+1] == '-' || rs[j+1] == '+') ||
				(rs[j] == '-' || rs[j] == '+') && j > i && (rs[j-1] == 'e' || rs[j-1] == 'E')) {
				j++
			}
			f, err := strconv.ParseFloat(string(rs[i:j]), 64)
			if err != nil {
				return nil, fmt.Errorf("bad number %q", string(rs[i:j]))
			}
			toks = append(toks, token{kind: "num", num: f})
			i = j
		case r == '"' || r == '\'':
			j := i + 1
			for j < len(rs) && rs[j] != r {
				j++
			}
			if j >= len(rs) {
				return nil, errors.New("unterminated text")
			}
			toks = append(toks, token{kind: "str", text: string(rs[i+1 : j])})
			i = j + 1
		case r == '`' || r == '[':
			end := '`'
			if r == '[' {
				end = ']'
			}
			j := i + 1
			for j < len(rs) && rs[j] != end {
				j++
			}
			if j >= len(rs) {
				return nil, fmt.Errorf("unterminated column name %c…%c", r, end)
			}
			toks = append(toks, token{kind: "ident", text: string(rs[i+1 : j]), col: -2})
			i = j + 1
		case r == '$':
			j := i + 1
			for j < len(rs) && unicode.IsDigit(rs[j]) {
				j++
			}
			n, err := strconv.Atoi(string(rs[i+1 : j]))
			if err != nil || n < 1 {
				return nil, errors.New("$ needs a column number ($1, $2, …)")
			}
			toks = append(toks, token{kind: "col", col: n - 1})
			i = j
		case unicode.IsLetter(r) || r == '_':
			j := i
			for j < len(rs) && (unicode.IsLetter(rs[j]) || unicode.IsDigit(rs[j]) || rs[j] == '_') {
				j++
			}
			toks = append(toks, token{kind: "ident", text: string(rs[i:j])})
			i = j
		default:
			two := ""
			if i+1 < len(rs) {
				two = string(rs[i : i+2])
			}
			switch two {
			case "<=", ">=", "==", "!=", "&&", "||", "**":
				toks = append(toks, token{kind: "op", text: two})
				i += 2
				continue
			}
			if strings.ContainsRune("+-*/%^<>=!(),", r) {
				k := "op"
				if strings.ContainsRune("(),", r) {
					k = string(r)
				}
				toks = append(toks, token{kind: k, text: string(r)})
				i++
				continue
			}
			return nil, fmt.Errorf("unexpected %q", r)
		}
	}
	return toks, nil
}

type parser struct {
	toks    []token
	pos     int
	resolve func(name string) (int, bool)
	cols    map[int]bool
	pre     []*preNode
	self    bool // _ was used
}

type node func(rc rowCtx) Value

// Compile parses an expression; resolve maps a column name to its index.
func Compile(src string, resolve func(name string) (int, bool)) (*Expr, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, errors.New("empty expression")
	}
	p := &parser{toks: toks, resolve: resolve, cols: map[int]bool{}}
	n, err := p.or()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.toks) {
		return nil, fmt.Errorf("unexpected %q", p.toks[p.pos].show())
	}
	x := &Expr{src: src, eval: n, pre: p.pre, Self: p.self}
	for c := range p.cols {
		x.Cols = append(x.Cols, c)
	}
	return x, nil
}

func (t token) show() string {
	switch t.kind {
	case "num":
		return Format(t.num)
	case "col":
		return fmt.Sprintf("$%d", t.col+1)
	}
	return t.text
}

func (p *parser) peek() (token, bool) {
	if p.pos < len(p.toks) {
		return p.toks[p.pos], true
	}
	return token{}, false
}

// accept consumes an operator or keyword.
func (p *parser) accept(ops ...string) (string, bool) {
	t, ok := p.peek()
	if !ok {
		return "", false
	}
	for _, o := range ops {
		if (t.kind == "op" || t.kind == "ident" && t.col == 0) && strings.EqualFold(t.text, o) {
			p.pos++
			return o, true
		}
	}
	return "", false
}

func (p *parser) or() (node, error) {
	l, err := p.and()
	if err != nil {
		return nil, err
	}
	for {
		if _, ok := p.accept("||", "or"); !ok {
			return l, nil
		}
		r, err := p.and()
		if err != nil {
			return nil, err
		}
		a, b := l, r
		l = func(rc rowCtx) Value { return boolean(truth(a(rc)) || truth(b(rc))) }
	}
}

func (p *parser) and() (node, error) {
	l, err := p.not()
	if err != nil {
		return nil, err
	}
	for {
		if _, ok := p.accept("&&", "and"); !ok {
			return l, nil
		}
		r, err := p.not()
		if err != nil {
			return nil, err
		}
		a, b := l, r
		l = func(rc rowCtx) Value { return boolean(truth(a(rc)) && truth(b(rc))) }
	}
}

// not binds looser than comparisons: not x > 5 is not (x > 5).
func (p *parser) not() (node, error) {
	if _, ok := p.accept("!", "not"); ok {
		x, err := p.not()
		if err != nil {
			return nil, err
		}
		return func(rc rowCtx) Value {
			v := x(rc)
			if v.Missing {
				return missing
			}
			return boolean(!truth(v))
		}, nil
	}
	return p.cmp()
}

func (p *parser) cmp() (node, error) {
	l, err := p.add()
	if err != nil {
		return nil, err
	}
	op, ok := p.accept("<=", ">=", "==", "!=", "<", ">", "=")
	if !ok {
		return l, nil
	}
	r, err := p.add()
	if err != nil {
		return nil, err
	}
	return func(rc rowCtx) Value {
		a, b := l(rc), r(rc)
		if a.Missing || b.Missing {
			return missing
		}
		fa, oka := a.number()
		fb, okb := b.number()
		var c int
		if oka && okb {
			switch {
			case fa < fb:
				c = -1
			case fa > fb:
				c = 1
			}
		} else {
			c = strings.Compare(strings.TrimSpace(a.String()), strings.TrimSpace(b.String()))
		}
		switch op {
		case "<":
			return boolean(c < 0)
		case "<=":
			return boolean(c <= 0)
		case ">":
			return boolean(c > 0)
		case ">=":
			return boolean(c >= 0)
		case "!=":
			return boolean(c != 0)
		}
		return boolean(c == 0)
	}, nil
}

func arith(a, b node, f func(x, y float64) float64) node {
	return func(rc rowCtx) Value {
		x, ok1 := a(rc).number()
		y, ok2 := b(rc).number()
		if !ok1 || !ok2 {
			return missing
		}
		return num(f(x, y))
	}
}

func (p *parser) add() (node, error) {
	l, err := p.mul()
	if err != nil {
		return nil, err
	}
	for {
		op, ok := p.accept("+", "-")
		if !ok {
			return l, nil
		}
		r, err := p.mul()
		if err != nil {
			return nil, err
		}
		if op == "+" {
			l = arith(l, r, func(x, y float64) float64 { return x + y })
		} else {
			l = arith(l, r, func(x, y float64) float64 { return x - y })
		}
	}
}

func (p *parser) mul() (node, error) {
	l, err := p.unary()
	if err != nil {
		return nil, err
	}
	for {
		op, ok := p.accept("*", "/", "%")
		if !ok {
			return l, nil
		}
		r, err := p.unary()
		if err != nil {
			return nil, err
		}
		switch op {
		case "*":
			l = arith(l, r, func(x, y float64) float64 { return x * y })
		case "/":
			l = arith(l, r, func(x, y float64) float64 { return x / y }) // ±Inf → missing
		default:
			l = arith(l, r, math.Mod)
		}
	}
}

func (p *parser) unary() (node, error) {
	if op, ok := p.accept("-", "+"); ok {
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		if op == "+" {
			return x, nil
		}
		return func(rc rowCtx) Value {
			if f, ok := x(rc).number(); ok {
				return num(-f)
			}
			return missing
		}, nil
	}
	return p.pow()
}

func (p *parser) pow() (node, error) {
	base, err := p.primary()
	if err != nil {
		return nil, err
	}
	if _, ok := p.accept("^", "**"); ok {
		exp, err := p.unary() // right-associative: 2^3^2 = 2^9
		if err != nil {
			return nil, err
		}
		return arith(base, exp, math.Pow), nil
	}
	return base, nil
}

func (p *parser) column(c int) node {
	p.cols[c] = true
	return func(rc rowCtx) Value {
		if c >= len(rc.cells) || IsMissing(rc.cells[c]) {
			return missing
		}
		s := strings.TrimSpace(rc.cells[c])
		if f, ok := Number(s); ok {
			return Value{Num: f}
		}
		return Value{Text: s, IsText: true}
	}
}

func (p *parser) primary() (node, error) {
	t, ok := p.peek()
	if !ok {
		return nil, errors.New("expression ends too early")
	}
	p.pos++
	switch t.kind {
	case "num":
		v := Value{Num: t.num}
		return func(rowCtx) Value { return v }, nil
	case "str":
		v := Value{Text: t.text, IsText: true}
		return func(rowCtx) Value { return v }, nil
	case "col":
		if _, ok := p.resolve(strconv.Itoa(t.col + 1)); !ok {
			return nil, fmt.Errorf("no column $%d", t.col+1)
		}
		return p.column(t.col), nil
	case "(":
		n, err := p.or()
		if err != nil {
			return nil, err
		}
		if nt, ok := p.peek(); !ok || nt.kind != ")" {
			return nil, errors.New("missing )")
		}
		p.pos++
		return n, nil
	case "ident":
		if t.col == -2 { // `quoted` or [bracketed] column name
			return p.columnNamed(t.text)
		}
		if nt, ok := p.peek(); ok && nt.kind == "(" {
			p.pos++
			return p.call(strings.ToLower(t.text))
		}
		// the constants pi and e, unless a column has that name
		if _, isCol := p.resolve(t.text); !isCol {
			switch strings.ToLower(t.text) {
			case "pi":
				return func(rowCtx) Value { return Value{Num: math.Pi} }, nil
			case "e":
				return func(rowCtx) Value { return Value{Num: math.E} }, nil
			}
		}
		return p.columnNamed(t.text)
	}
	return nil, fmt.Errorf("unexpected %q", t.show())
}

func (p *parser) columnNamed(name string) (node, error) {
	if name == "_" {
		p.self = true
	}
	c, ok := p.resolve(name)
	if !ok {
		return nil, fmt.Errorf("no column %q (use `name` or [name] for names with spaces)", name)
	}
	return p.column(c), nil
}

var funcArity = map[string][2]int{ // min, max (-1: any)
	"abs": {1, 1}, "sqrt": {1, 1}, "exp": {1, 1}, "log": {1, 1}, "ln": {1, 1},
	"log10": {1, 1}, "log2": {1, 1}, "floor": {1, 1}, "ceil": {1, 1}, "round": {1, 2},
	"min": {1, -1}, "max": {1, -1}, "pow": {2, 2}, "if": {3, 3}, "isna": {1, 1},
	"coalesce": {1, -1}, "square": {1, 1},
	// aggregates (one value over all rows; min / max with one argument)
	"mean": {1, 1}, "median": {1, 1}, "sum": {1, 1}, "count": {1, 1}, "std": {1, 1},
	"var": {1, 1}, "sem": {1, 1}, "range": {1, 1}, "iqr": {1, 1}, "skew": {1, 1},
	"kurt": {1, 1}, "quantile": {2, 2}, "n": {0, 0},
	// window functions (follow the order of the rows)
	"rank": {1, 1}, "cumsum": {1, 1}, "diff": {1, 1}, "lag": {1, 2}, "lead": {1, 2},
}

// aggregateFuncs are computed over all rows; Funcs names map onto Stats.
var aggregateFuncs = map[string]bool{"mean": true, "median": true, "sum": true, "count": true, "std": true,
	"var": true, "sem": true, "range": true, "iqr": true, "skew": true, "kurt": true, "quantile": true, "n": true}

var windowFuncs = map[string]bool{"rank": true, "cumsum": true, "diff": true, "lag": true, "lead": true}

func (p *parser) call(name string) (node, error) {
	ar, ok := funcArity[name]
	if !ok {
		return nil, fmt.Errorf("unknown function %s()", name)
	}
	var args []node
	if t, ok := p.peek(); ok && t.kind == ")" {
		p.pos++
	} else {
		for {
			a, err := p.or()
			if err != nil {
				return nil, err
			}
			args = append(args, a)
			t, ok := p.peek()
			if !ok {
				return nil, fmt.Errorf("missing ) after %s(", name)
			}
			p.pos++
			if t.kind == ")" {
				break
			}
			if t.kind != "," {
				return nil, fmt.Errorf("expected , or ) in %s()", name)
			}
		}
	}
	if len(args) < ar[0] || ar[1] >= 0 && len(args) > ar[1] {
		return nil, fmt.Errorf("%s() takes %s argument(s)", name, arityText(ar))
	}
	// one-argument min / max, aggregates and window functions are
	// computed by Prepare; the row pass reads their results
	if aggregateFuncs[name] || windowFuncs[name] || (name == "min" || name == "max") && len(args) == 1 {
		pn := &preNode{fn: name, args: args, value: missing}
		p.pre = append(p.pre, pn) // after the ones nested in args
		if windowFuncs[name] {
			return func(rc rowCtx) Value {
				if rc.i < 0 || rc.i >= len(pn.per) {
					return missing
				}
				return pn.per[rc.i]
			}, nil
		}
		return func(rowCtx) Value { return pn.value }, nil
	}
	unary := func(f func(float64) float64) node {
		a := args[0]
		return func(rc rowCtx) Value {
			if x, ok := a(rc).number(); ok {
				return num(f(x))
			}
			return missing
		}
	}
	switch name {
	case "abs":
		return unary(math.Abs), nil
	case "sqrt":
		return unary(math.Sqrt), nil
	case "exp":
		return unary(math.Exp), nil
	case "log", "ln":
		return unary(math.Log), nil
	case "log10":
		return unary(math.Log10), nil
	case "log2":
		return unary(math.Log2), nil
	case "floor":
		return unary(math.Floor), nil
	case "ceil":
		return unary(math.Ceil), nil
	case "round":
		if len(args) == 1 {
			return unary(math.Round), nil
		}
		return arith(args[0], args[1], func(x, d float64) float64 {
			m := math.Pow(10, math.Round(d))
			return math.Round(x*m) / m
		}), nil
	case "pow":
		return arith(args[0], args[1], math.Pow), nil
	case "square":
		return unary(func(x float64) float64 { return x * x }), nil
	case "min", "max":
		isMax := name == "max"
		return func(rc rowCtx) Value {
			best, any := 0.0, false
			for _, a := range args {
				x, ok := a(rc).number()
				if !ok {
					return missing
				}
				if !any || isMax && x > best || !isMax && x < best {
					best, any = x, true
				}
			}
			return num(best)
		}, nil
	case "if":
		return func(rc rowCtx) Value {
			c := args[0](rc)
			if c.Missing {
				return missing
			}
			if truth(c) {
				return args[1](rc)
			}
			return args[2](rc)
		}, nil
	case "isna":
		return func(rc rowCtx) Value { return boolean(args[0](rc).Missing) }, nil
	case "coalesce":
		return func(rc rowCtx) Value {
			for _, a := range args {
				if v := a(rc); !v.Missing {
					return v
				}
			}
			return missing
		}, nil
	}
	return nil, fmt.Errorf("unknown function %s()", name)
}

func arityText(ar [2]int) string {
	switch {
	case ar[1] < 0:
		return fmt.Sprintf("%d or more", ar[0])
	case ar[0] == ar[1]:
		return strconv.Itoa(ar[0])
	}
	return fmt.Sprintf("%d to %d", ar[0], ar[1])
}

// prepare computes the node over rows: the argument is evaluated on
// every row (with its position, so nested window functions work).
func (pn *preNode) prepare(rows [][]string) {
	vals := make([]Value, len(rows))
	if len(pn.args) > 0 {
		for i, r := range rows {
			vals[i] = pn.args[0](rowCtx{r, i})
		}
	}
	cells := make([]string, len(vals))
	for i, v := range vals {
		cells[i] = v.String()
	}
	switch pn.fn {
	case "n":
		pn.value = Value{Num: float64(len(rows))}
	case "count":
		n := 0
		for _, v := range vals {
			if !v.Missing && !IsMissing(v.String()) {
				n++
			}
		}
		pn.value = Value{Num: float64(n)}
	case "min", "max", "mean", "median", "sum", "std", "var", "sem", "range", "iqr", "skew", "kurt":
		f, _ := Describe(cells).Value(pn.fn)
		if pn.fn == "sum" && Describe(cells).Numeric == 0 {
			f = math.NaN()
		}
		pn.value = num(f)
	case "quantile":
		q := math.NaN()
		if len(rows) > 0 {
			if f, ok := pn.args[1](rowCtx{rows[0], 0}).number(); ok {
				q = f
			}
		}
		var nums []float64
		for _, v := range vals {
			if f, ok := v.number(); ok {
				nums = append(nums, f)
			}
		}
		sort.Float64s(nums)
		if q < 0 || q > 1 || math.IsNaN(q) {
			pn.value = missing
		} else {
			pn.value = num(Quantile(nums, q))
		}
	default: // window functions
		pn.per = window(pn, rows, vals)
	}
}

// window computes rank, cumsum, diff, lag and lead in row order.
func window(pn *preNode, rows [][]string, vals []Value) []Value {
	out := make([]Value, len(vals))
	for i := range out {
		out[i] = missing
	}
	k := 1
	if len(pn.args) > 1 && len(rows) > 0 {
		f, ok := pn.args[1](rowCtx{rows[0], 0}).number()
		if !ok || f != math.Trunc(f) {
			return out // k must be a whole number
		}
		k = int(f)
	}
	switch pn.fn {
	case "rank":
		var nums []float64
		var at []int
		for i, v := range vals {
			if f, ok := v.number(); ok {
				nums = append(nums, f)
				at = append(at, i)
			}
		}
		for j, r := range Ranks(nums) {
			out[at[j]] = num(r)
		}
	case "cumsum":
		run := 0.0
		for i, v := range vals {
			if f, ok := v.number(); ok {
				run += f
				out[i] = num(run)
			}
		}
	case "diff":
		for i := 1; i < len(vals); i++ {
			a, ok1 := vals[i].number()
			b, ok2 := vals[i-1].number()
			if ok1 && ok2 {
				out[i] = num(a - b)
			}
		}
	case "lag", "lead":
		if pn.fn == "lead" {
			k = -k
		}
		for i := range vals {
			if j := i - k; j >= 0 && j < len(vals) {
				out[i] = vals[j]
			}
		}
	}
	return out
}
