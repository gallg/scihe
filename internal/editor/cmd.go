package editor

import (
	"scihe/internal/buffer"
	"scihe/internal/keymap"
)

// CmdKind classifies keymap entries.
type CmdKind int

const (
	KindMotion CmdKind = iota
	KindOperator
	KindAction
	KindTextObject
)

// MotionType decides how an operator uses a motion's range.
type MotionType int

const (
	Exclusive MotionType = iota
	Inclusive
	Linewise
)

// Operator identifiers.
type Operator int

const (
	OpNone Operator = iota
	OpDelete
	OpChange
	OpYank
	OpIndent
	OpDedent
	OpLower
	OpUpper
	OpToggleCase
	OpComment
	OpFormat
	OpEval // send range to the REPL / kernel
)

// ctx carries the parsed state of one command.
type ctx struct {
	e        *Editor
	w        *Window
	b        *buffer.Buffer
	count    int // effective count (>= 1)
	hasCount bool
	reg      rune
	char     rune // argument for f/t/r/m/q/@/'/`
	op       Operator
	keys     []keymap.Key
}

// MotionFunc computes a motion target.
type MotionFunc func(c *ctx) (target buffer.Pos, mt MotionType, ok bool)

// TextObjFunc computes a range [start, end) for a text object.
type TextObjFunc func(c *ctx) (start, end buffer.Pos, mt MotionType, ok bool)

// ActionFunc executes a command.
type ActionFunc func(c *ctx)

// Cmd is a keymap entry.
type Cmd struct {
	Kind      CmdKind
	Motion    MotionFunc
	TextObj   TextObjFunc
	Action    ActionFunc
	Op        Operator
	NeedsChar bool
	Change    bool // repeatable with "."
	Jump      bool // motion adds to the jump list
	KeepWant  bool // vertical motion: keep desired column
	EOL       bool // motion sets the desired column to end of line ($)
	NoUndo    bool // action manages undo itself (u, <C-r>)
	// Mapping replays keys or runs an ex command (from config).
	Mapping string
}

func motion(f MotionFunc) *Cmd { return &Cmd{Kind: KindMotion, Motion: f} }
func jumpMotion(f MotionFunc) *Cmd {
	return &Cmd{Kind: KindMotion, Motion: f, Jump: true}
}
func vmotion(f MotionFunc) *Cmd { return &Cmd{Kind: KindMotion, Motion: f, KeepWant: true} }
func charMotion(f MotionFunc) *Cmd {
	return &Cmd{Kind: KindMotion, Motion: f, NeedsChar: true}
}
func action(f ActionFunc) *Cmd { return &Cmd{Kind: KindAction, Action: f} }
func change(f ActionFunc) *Cmd { return &Cmd{Kind: KindAction, Action: f, Change: true} }
func charChange(f ActionFunc) *Cmd {
	return &Cmd{Kind: KindAction, Action: f, Change: true, NeedsChar: true}
}
func charAction(f ActionFunc) *Cmd { return &Cmd{Kind: KindAction, Action: f, NeedsChar: true} }
func operator(op Operator) *Cmd {
	return &Cmd{Kind: KindOperator, Op: op, Change: op != OpYank && op != OpEval}
}
func textObj(f TextObjFunc) *Cmd { return &Cmd{Kind: KindTextObject, TextObj: f} }
func noUndo(f ActionFunc) *Cmd   { return &Cmd{Kind: KindAction, Action: f, NoUndo: true} }
