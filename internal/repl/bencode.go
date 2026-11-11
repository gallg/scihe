package repl

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
)

// Encode writes v (string, int, []any, map[string]any) in bencode.
func Encode(w io.Writer, v any) error {
	var buf bytes.Buffer
	if err := encode(&buf, v); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

func encode(b *bytes.Buffer, v any) error {
	switch t := v.(type) {
	case string:
		fmt.Fprintf(b, "%d:%s", len(t), t)
	case []byte:
		fmt.Fprintf(b, "%d:", len(t))
		b.Write(t)
	case int:
		fmt.Fprintf(b, "i%de", t)
	case int64:
		fmt.Fprintf(b, "i%de", t)
	case []string:
		b.WriteByte('l')
		for _, s := range t {
			encode(b, s)
		}
		b.WriteByte('e')
	case []any:
		b.WriteByte('l')
		for _, x := range t {
			if err := encode(b, x); err != nil {
				return err
			}
		}
		b.WriteByte('e')
	case map[string]any:
		b.WriteByte('d')
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			encode(b, k)
			if err := encode(b, t[k]); err != nil {
				return err
			}
		}
		b.WriteByte('e')
	default:
		return fmt.Errorf("bencode: unsupported type %T", v)
	}
	return nil
}

// bencodeMaxDepth bounds the nesting of lists and dictionaries (nREPL
// messages nest a few levels): a deeper one is an error, not a stack
// exhausted by a hostile server.
const bencodeMaxDepth = 100

// Decode reads one bencoded value.
func Decode(r *bufio.Reader) (any, error) { return decode(r, 0) }

// readNumber reads the digits of a number up to its delimiter, which it
// consumes; at most 20 bytes (an int64 and its sign).
func readNumber(r *bufio.Reader, delim byte) (string, error) {
	var b []byte
	for {
		c, err := r.ReadByte()
		if err != nil {
			return "", err
		}
		if c == delim {
			return string(b), nil
		}
		if len(b) == 20 {
			return "", errors.New("bencode: number too long")
		}
		b = append(b, c)
	}
}

func decode(r *bufio.Reader, depth int) (any, error) {
	if depth > bencodeMaxDepth {
		return nil, errors.New("bencode: nested too deeply")
	}
	c, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	switch {
	case c == 'i':
		s, err := readNumber(r, 'e')
		if err != nil {
			return nil, err
		}
		return strconv.ParseInt(s, 10, 64)
	case c == 'l':
		var out []any
		for {
			p, err := r.Peek(1)
			if err != nil {
				return nil, err
			}
			if p[0] == 'e' {
				r.ReadByte()
				return out, nil
			}
			v, err := decode(r, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
	case c == 'd':
		out := map[string]any{}
		for {
			p, err := r.Peek(1)
			if err != nil {
				return nil, err
			}
			if p[0] == 'e' {
				r.ReadByte()
				return out, nil
			}
			k, err := decode(r, depth+1)
			if err != nil {
				return nil, err
			}
			ks, ok := k.(string)
			if !ok {
				return nil, errors.New("bencode: non-string dict key")
			}
			v, err := decode(r, depth+1)
			if err != nil {
				return nil, err
			}
			out[ks] = v
		}
	case c >= '0' && c <= '9':
		s, err := readNumber(r, ':')
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(string(c) + s)
		if err != nil || n < 0 || n > 64<<20 {
			return nil, fmt.Errorf("bencode: bad string length")
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		return string(buf), nil
	}
	return nil, fmt.Errorf("bencode: unexpected byte %q", c)
}
