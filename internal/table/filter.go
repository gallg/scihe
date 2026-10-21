package table

import (
	"errors"
	"regexp"
	"strings"
)

// Filter keeps the rows whose cell matches a condition.
type Filter struct {
	Expr  string
	match func(cell string) bool
}

// Match applies the filter to a cell.
func (f *Filter) Match(cell string) bool { return f.match(cell) }

// ParseFilter parses "OP VALUE" with OP one of = == != < <= > >= (numeric
// when both sides are numbers, else text), ~ / !~ (regular expression),
// or a bare VALUE meaning "contains" (case-insensitive). "empty" and
// "!empty" test for missing values.
func ParseFilter(expr string) (*Filter, error) {
	e := strings.TrimSpace(expr)
	if e == "" {
		return nil, errors.New("empty filter")
	}
	switch strings.ToLower(e) {
	case "empty":
		return &Filter{Expr: e, match: IsMissing}, nil
	case "!empty":
		return &Filter{Expr: e, match: func(c string) bool { return !IsMissing(c) }}, nil
	}
	for _, op := range []string{"!~", "~", ">=", "<=", "!=", "==", "=", ">", "<"} {
		if !strings.HasPrefix(e, op) {
			continue
		}
		val := strings.TrimSpace(e[len(op):])
		val = strings.Trim(val, `"`)
		if op == "~" || op == "!~" {
			re, err := regexp.Compile(val)
			if err != nil {
				return nil, err
			}
			neg := op == "!~"
			return &Filter{Expr: e, match: func(c string) bool { return re.MatchString(c) != neg }}, nil
		}
		vn, vNum := Number(val)
		cmp := func(c string) (int, bool) {
			c = strings.TrimSpace(c)
			if cn, ok := Number(c); ok && vNum {
				switch {
				case cn < vn:
					return -1, true
				case cn > vn:
					return 1, true
				}
				return 0, true
			}
			if op != "=" && op != "==" && op != "!=" && (vNum || IsMissing(c)) {
				return 0, false // ordering text against a number
			}
			return strings.Compare(c, val), true
		}
		return &Filter{Expr: e, match: func(c string) bool {
			if IsMissing(c) {
				return false // only "empty" selects missing values
			}
			r, ok := cmp(c)
			if !ok {
				return false
			}
			switch op {
			case "=", "==":
				return r == 0
			case "!=":
				return r != 0
			case "<":
				return r < 0
			case "<=":
				return r <= 0
			case ">":
				return r > 0
			}
			return r >= 0
		}}, nil
	}
	low := strings.ToLower(e)
	return &Filter{Expr: e, match: func(c string) bool { return strings.Contains(strings.ToLower(c), low) }}, nil
}
