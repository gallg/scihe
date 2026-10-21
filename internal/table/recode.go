package table

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Recoding turns text cells into numbers. Modes:
//
//	number: other notations of numbers — decimal commas ("891,1"),
//	        thousands separators ("1.234,5", "1,234.5", "1 234"),
//	        percentages ("12%" → 0.12), currency signs and units
//	        ("€3.50", "3.5 kg")
//	bool:   yes/no, true/false, y/n, t/f, on/off, 1/0 → 1/0
//	codes:  each distinct value gets an integer, 1… in sorted order
//	map:    codes given by the user ("north=1 south=2")
//	auto:   number if every value parses, else bool, else codes
//
// Missing values stay missing (""). Values a mode cannot convert become
// missing and are counted in Recoding.Failed.

// Recoding is the result of recoding one column.
type Recoding struct {
	Mode    string
	Values  []string // the new cells, aligned with the input
	Failed  int      // non-missing inputs that could not be converted
	Mapping []Code   // codes / bool / map: value → code
}

// Code is one entry of a recoding table.
type Code struct {
	Value string
	Code  float64
	Count int
}

var boolValues = map[string]float64{
	"yes": 1, "no": 0, "y": 1, "n": 0, "true": 1, "false": 0, "t": 1, "f": 0,
	"on": 1, "off": 0, "1": 1, "0": 0,
}

// Recode converts cells with the given mode ("auto", "number", "bool",
// "codes" or "map"); mapping is used by "map" (value → code).
func Recode(cells []string, mode string, mapping map[string]float64) (*Recoding, error) {
	switch mode {
	case "auto":
		// numbers only when every value reads as one, no value has a word
		// in front ("Room 12", "A1") and distinct values stay distinct
		if r := recodeNumber(cells, true); r.Failed == 0 && nonMissing(cells) > 0 && injective(cells, r.Values) {
			return r, nil
		}
		if r := recodeBool(cells); r.Failed == 0 && nonMissing(cells) > 0 {
			return r, nil
		}
		return recodeCodes(cells), nil
	case "number", "num":
		return recodeNumber(cells, false), nil
	case "bool":
		return recodeBool(cells), nil
	case "codes", "code", "factor":
		return recodeCodes(cells), nil
	case "map":
		if len(mapping) == 0 {
			return nil, errors.New("map needs VALUE=CODE pairs")
		}
		return recodeMap(cells, mapping), nil
	}
	return nil, fmt.Errorf("unknown recode mode %q (auto, number, bool, codes, map)", mode)
}

// injective reports whether distinct inputs got distinct results.
func injective(cells, values []string) bool {
	seen := map[string]string{}
	for i, c := range cells {
		if IsMissing(c) {
			continue
		}
		c = strings.TrimSpace(c)
		if prev, ok := seen[values[i]]; ok && prev != c {
			return false
		}
		seen[values[i]] = c
	}
	return true
}

func nonMissing(cells []string) int {
	n := 0
	for _, c := range cells {
		if !IsMissing(c) {
			n++
		}
	}
	return n
}

var (
	reLead       = regexp.MustCompile(`^[^\d.,]+`) // currency signs, words: "€", "USD "
	reTrail      = regexp.MustCompile(`[^\d]+$`)   // units and signs: " kg", "€", "%"
	reGroupComma = regexp.MustCompile(`^\d{1,3}(,\d{3})+$`)
)

// decimalComma guesses whether a column writes decimals with a comma:
// some value has both separators with the comma last ("1.234,5"), or a
// comma that is not a thousands grouping ("891,1"). "1,234" alone is
// read as a thousands separator.
func decimalComma(cells []string) bool {
	for _, c := range cells {
		s, _ := numericCore(c)
		i := strings.LastIndexByte(s, ',')
		if i < 0 {
			continue
		}
		if j := strings.LastIndexByte(s, '.'); j >= 0 {
			return i > j
		}
		if !reGroupComma.MatchString(s) {
			return true
		}
	}
	return false
}

// numericCore strips the sign, currency signs, units and a percent
// sign, returning the digits and separators and whether it was negative.
func numericCore(s string) (string, bool) {
	s = strings.TrimSpace(s)
	neg := false
	for _, sign := range []string{"-", "−", "+"} {
		if strings.HasPrefix(s, sign) {
			neg, s = sign != "+", strings.TrimSpace(s[len(sign):])
			break
		}
	}
	s = reLead.ReplaceAllString(s, "")
	s = reTrail.ReplaceAllString(s, "")
	s = strings.NewReplacer(" ", "", "'", "", "_", "", "\u00a0", "", "\u202f", "").Replace(s)
	return s, neg
}

// LooseNumber parses a number in the notations described above; comma
// says whether a comma is the decimal separator.
func LooseNumber(s string, comma bool) (float64, bool) {
	t := strings.TrimSpace(s)
	// in a decimal-comma column "1.234" is a thousands grouping
	if f, ok := Number(t); ok && !(comma && strings.Contains(t, ".")) {
		return f, true
	}
	core, neg := numericCore(t)
	if core == "" || !strings.ContainsAny(core, "0123456789") {
		return 0, false
	}
	if comma {
		core = strings.ReplaceAll(core, ".", "")
		core = strings.ReplaceAll(core, ",", ".")
	} else {
		core = strings.ReplaceAll(core, ",", "")
	}
	f, err := strconv.ParseFloat(core, 64)
	if err != nil {
		return 0, false
	}
	if neg {
		f = -f
	}
	if strings.HasSuffix(t, "%") {
		f /= 100
	}
	return f, true
}

// recodeNumber reads other notations of numbers; strict refuses values
// with letters before the number (a code such as "A1", not a currency).
func recodeNumber(cells []string, strict bool) *Recoding {
	r := &Recoding{Mode: "number", Values: make([]string, len(cells))}
	comma := decimalComma(cells)
	for i, c := range cells {
		if IsMissing(c) {
			continue
		}
		if strict && hasWordPrefix(c) {
			r.Failed++
			continue
		}
		if f, ok := LooseNumber(c, comma); ok {
			r.Values[i] = CellValue(f)
		} else {
			r.Failed++
		}
	}
	return r
}

// hasWordPrefix reports letters before the first digit, other than
// currency codes written in capitals followed by a space ("USD 3").
func hasWordPrefix(s string) bool {
	s = strings.TrimSpace(s)
	lead := reLead.FindString(strings.TrimLeft(s, "+-−"))
	letters := strings.IndexFunc(lead, func(r rune) bool { return 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' }) >= 0
	if !letters {
		return false
	}
	code := strings.TrimSpace(lead)
	return !(len(code) == 3 && strings.ToUpper(code) == code && strings.HasSuffix(lead, " "))
}

func recodeBool(cells []string) *Recoding {
	r := &Recoding{Mode: "bool", Values: make([]string, len(cells))}
	counts := map[string]int{}
	for i, c := range cells {
		if IsMissing(c) {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(c))
		v, ok := boolValues[k]
		if !ok {
			r.Failed++
			continue
		}
		r.Values[i] = CellValue(v)
		counts[k]++
	}
	for k, n := range counts {
		r.Mapping = append(r.Mapping, Code{Value: k, Code: boolValues[k], Count: n})
	}
	sort.Slice(r.Mapping, func(i, j int) bool {
		a, b := r.Mapping[i], r.Mapping[j]
		if a.Code != b.Code {
			return a.Code > b.Code
		}
		return a.Value < b.Value
	})
	return r
}

// recodeCodes numbers the distinct values 1… in sorted order (numbers
// numerically, then text case-insensitively).
func recodeCodes(cells []string) *Recoding {
	r := &Recoding{Mode: "codes", Values: make([]string, len(cells))}
	counts := map[string]int{}
	for _, c := range cells {
		if !IsMissing(c) {
			counts[strings.TrimSpace(c)]++
		}
	}
	vals := make([]string, 0, len(counts))
	for v := range counts {
		vals = append(vals, v)
	}
	sort.Slice(vals, func(i, j int) bool { return lessCell(vals[i], vals[j]) })
	code := map[string]float64{}
	for i, v := range vals {
		code[v] = float64(i + 1)
		r.Mapping = append(r.Mapping, Code{Value: v, Code: float64(i + 1), Count: counts[v]})
	}
	for i, c := range cells {
		if !IsMissing(c) {
			r.Values[i] = CellValue(code[strings.TrimSpace(c)])
		}
	}
	return r
}

func recodeMap(cells []string, m map[string]float64) *Recoding {
	r := &Recoding{Mode: "map", Values: make([]string, len(cells))}
	counts := map[string]int{}
	for i, c := range cells {
		if IsMissing(c) {
			continue
		}
		k := strings.TrimSpace(c)
		v, ok := m[k]
		if !ok {
			r.Failed++
			continue
		}
		r.Values[i] = CellValue(v)
		counts[k]++
	}
	for k, v := range m {
		r.Mapping = append(r.Mapping, Code{Value: k, Code: v, Count: counts[k]})
	}
	sort.Slice(r.Mapping, func(i, j int) bool { return r.Mapping[i].Code < r.Mapping[j].Code })
	return r
}

// Compare orders cells: numbers numerically and before text, text
// case-insensitively (then exactly); surrounding blanks are ignored.
func Compare(a, b string) int {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	fa, oka := Number(a)
	fb, okb := Number(b)
	switch {
	case oka && okb:
		switch {
		case fa < fb:
			return -1
		case fa > fb:
			return 1
		}
		return 0
	case oka:
		return -1
	case okb:
		return 1
	}
	if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

func lessCell(a, b string) bool { return Compare(a, b) < 0 }

// ParseMapping parses "a=1 b=2 'two words'=3".
func ParseMapping(args []string) (map[string]float64, error) {
	m := map[string]float64{}
	for _, a := range args {
		i := strings.LastIndexByte(a, '=')
		if i <= 0 {
			return nil, fmt.Errorf("expected VALUE=CODE, got %q", a)
		}
		f, ok := Number(a[i+1:])
		if !ok {
			return nil, fmt.Errorf("code of %q is not a number", a[:i])
		}
		m[a[:i]] = f
	}
	return m, nil
}
