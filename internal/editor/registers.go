package editor

import (
	"strings"

	"scihe/internal/clipboard"
)

// RegType tells how register text is put back.
type RegType int

const (
	RegChar RegType = iota
	RegLine
	RegBlock
)

// Register is the content of one register.
type Register struct {
	Text string // for blockwise: lines joined by \n
	Type RegType
}

// Registers holds all named and special registers.
type Registers struct {
	m         map[rune]Register
	clipFall  Register // used when no system clipboard tool exists
	lastClipE error
	// noDeleteReg: deletes without a named register store nothing
	// (delete_to_register = false in the config)
	noDeleteReg bool
}

func newRegisters() *Registers {
	return &Registers{m: map[rune]Register{}}
}

func validRegister(r rune) bool {
	return r == '"' || r == '-' || r == '_' || r == '+' || r == '*' || r == '/' || r == ':' || r == '.' || r == '%' ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// Get returns register r.
func (rs *Registers) Get(r rune) (Register, bool) {
	switch r {
	case 0:
		r = '"'
	case '+', '*':
		if s, err := clipboard.Paste(); err == nil {
			t := RegChar
			if strings.HasSuffix(s, "\n") {
				t = RegLine
			}
			return Register{Text: s, Type: t}, true
		}
		return rs.clipFall, rs.clipFall.Text != ""
	}
	if r >= 'A' && r <= 'Z' {
		r = r - 'A' + 'a'
	}
	reg, ok := rs.m[r]
	return reg, ok
}

// SetSpecial sets read-only registers (/ : . %).
func (rs *Registers) SetSpecial(r rune, text string) {
	rs.m[r] = Register{Text: text}
}

// Yank stores yanked text into reg (or unnamed) and "0".
func (rs *Registers) Yank(r rune, reg Register) {
	if r == '_' {
		return
	}
	if rs.store(r, reg) {
		rs.linkUnnamed(r)
		return
	}
	rs.m['"'] = reg
	rs.m['0'] = reg
}

// Delete stores deleted text: small deletes go to "-", others shift "1".."9".
func (rs *Registers) Delete(r rune, reg Register) {
	if r == '_' || r == 0 && rs.noDeleteReg {
		return
	}
	if rs.store(r, reg) {
		rs.linkUnnamed(r)
		return
	}
	rs.m['"'] = reg
	if reg.Type == RegLine || strings.Contains(reg.Text, "\n") {
		for i := '9'; i > '1'; i-- {
			if v, ok := rs.m[i-1]; ok {
				rs.m[i] = v
			}
		}
		rs.m['1'] = reg
	} else {
		rs.m['-'] = reg
	}
}

// linkUnnamed makes the unnamed register mirror a named one after a write.
func (rs *Registers) linkUnnamed(r rune) {
	if r != '+' && r != '*' {
		rs.m['"'] = rs.m[lower(r)]
	}
}

func lower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r - 'A' + 'a'
	}
	return r
}

// store handles explicitly named registers; returns true if handled.
func (rs *Registers) store(r rune, reg Register) bool {
	switch {
	case r == 0 || r == '"':
		return false
	case r == '+' || r == '*':
		rs.clipFall = reg
		rs.lastClipE = clipboard.Copy(reg.Text)
		rs.m['"'] = reg
		return true
	case r >= 'A' && r <= 'Z':
		lr := lower(r)
		old := rs.m[lr]
		if old.Type == RegLine || reg.Type == RegLine {
			t := strings.TrimSuffix(old.Text, "\n")
			if t != "" {
				t += "\n"
			}
			rs.m[lr] = Register{Text: t + strings.TrimSuffix(reg.Text, "\n") + "\n", Type: RegLine}
		} else {
			rs.m[lr] = Register{Text: old.Text + reg.Text, Type: old.Type}
		}
		return true
	case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		rs.m[r] = reg
		return true
	}
	return false
}

// Names lists non-empty registers for :registers.
func (rs *Registers) Names() []rune {
	order := `"0123456789abcdefghijklmnopqrstuvwxyz-.:/%`
	var out []rune
	for _, r := range order {
		if v, ok := rs.m[r]; ok && v.Text != "" {
			out = append(out, r)
		}
	}
	return out
}
