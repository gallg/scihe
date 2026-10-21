package repl

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Result is one piece of evaluation output. The final Result for a
// request has Done set.
type Result struct {
	Out, Err, Value string
	Done            bool
}

// Session is a live REPL connection.
type Session interface {
	Name() string
	// Eval evaluates code. cb runs on a background goroutine.
	Eval(code, ns string, cb func(Result)) error
	Interrupt()
	Close()
	Alive() bool
}

// chezMarker starts every frame the Chez driver prints.
const chezMarker = "\x1dSCIHE "

// ---------------------------------------------------------------- nREPL

// NREPL is an nREPL client (Clojure, Babashka, ...).
type NREPL struct {
	addr    string
	conn    net.Conn
	mu      sync.Mutex
	wmu     sync.Mutex // serializes socket writes
	session string
	nextID  int
	pending map[string]func(map[string]any)
	current string
	closed  bool
	onClose func(error)
}

// FindNREPLPort looks for a .nrepl-port file in dir and its parents.
func FindNREPLPort(dir string) (string, bool) {
	dir, _ = filepath.Abs(dir)
	for {
		data, err := os.ReadFile(filepath.Join(dir, ".nrepl-port"))
		if err == nil {
			p := strings.TrimSpace(string(data))
			if _, err := strconv.Atoi(p); err == nil {
				return "127.0.0.1:" + p, true
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// DialNREPL connects to addr and clones a session.
func DialNREPL(addr string, onClose func(error)) (*NREPL, error) {
	if !strings.Contains(addr, ":") {
		addr = "127.0.0.1:" + addr
	}
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return nil, err
	}
	c := &NREPL{addr: addr, conn: conn, pending: map[string]func(map[string]any){}, onClose: onClose}
	go c.readLoop()
	done := make(chan string, 1)
	c.send(map[string]any{"op": "clone"}, func(m map[string]any) {
		if s, ok := m["new-session"].(string); ok {
			done <- s
		}
	}, false)
	select {
	case s := <-done:
		c.mu.Lock()
		c.session = s
		c.mu.Unlock()
	case <-time.After(5 * time.Second):
		conn.Close()
		return nil, errors.New("nREPL: no reply to clone")
	}
	return c, nil
}

// Name implements Session.
func (c *NREPL) Name() string { return "nREPL " + c.addr }

// send registers cb and writes msg. The write happens outside c.mu (under
// a separate write lock) so the read loop is never blocked by a slow
// socket. If current is true the request becomes the interrupt target.
func (c *NREPL) send(msg map[string]any, cb func(map[string]any), current bool) (string, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return "", errors.New("nREPL connection closed")
	}
	c.nextID++
	id := strconv.Itoa(c.nextID)
	msg["id"] = id
	if c.session != "" {
		msg["session"] = c.session
	}
	c.pending[id] = cb
	if current {
		c.current = id
	}
	c.mu.Unlock()
	c.wmu.Lock()
	err := Encode(c.conn, msg)
	c.wmu.Unlock()
	if err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		if c.current == id {
			c.current = ""
		}
		c.mu.Unlock()
		return "", err
	}
	return id, nil
}

func (c *NREPL) readLoop() {
	r := bufio.NewReader(c.conn)
	var err error
	for {
		var v any
		v, err = Decode(r)
		if err != nil {
			break
		}
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		c.mu.Lock()
		cb := c.pending[id]
		if isDone(m) {
			delete(c.pending, id)
			if c.current == id {
				c.current = ""
			}
		}
		c.mu.Unlock()
		if cb != nil {
			cb(m)
		}
	}
	c.mu.Lock()
	c.closed = true
	pend := c.pending
	c.pending = map[string]func(map[string]any){}
	c.mu.Unlock()
	for _, cb := range pend {
		cb(map[string]any{"err": "connection closed\n", "status": []any{"done"}})
	}
	if c.onClose != nil {
		c.onClose(err)
	}
}

func isDone(m map[string]any) bool {
	st, _ := m["status"].([]any)
	for _, s := range st {
		if s == "done" {
			return true
		}
	}
	return false
}

// Eval implements Session.
func (c *NREPL) Eval(code, ns string, cb func(Result)) error {
	msg := map[string]any{"op": "eval", "code": code}
	if ns != "" {
		msg["ns"] = ns
	}
	_, err := c.send(msg, func(m map[string]any) {
		var r Result
		r.Out, _ = m["out"].(string)
		r.Err, _ = m["err"].(string)
		r.Value, _ = m["value"].(string)
		if ex, ok := m["ex"].(string); ok && r.Err == "" {
			r.Err = ex + "\n"
		}
		r.Done = isDone(m)
		if r.Out != "" || r.Err != "" || r.Value != "" || r.Done {
			cb(r)
		}
	}, true)
	return err
}

// Interrupt implements Session.
func (c *NREPL) Interrupt() {
	c.mu.Lock()
	cur := c.current
	c.mu.Unlock()
	if cur != "" {
		c.send(map[string]any{"op": "interrupt", "interrupt-id": cur}, func(map[string]any) {}, false)
	}
}

// Close implements Session.
func (c *NREPL) Close() {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if !closed {
		c.send(map[string]any{"op": "close"}, func(map[string]any) {}, false)
	}
	c.conn.Close()
}

// Alive implements Session.
func (c *NREPL) Alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed
}

// ---------------------------------------------------------------- Chez

// The driver evaluates each request inside a guarded, interruptible
// context and reports output, values and errors as framed lines:
//
//	\x1dSCIHE <id> <kind> "<scheme string literal>"
const chezDriver = `
(keyboard-interrupt-handler (lambda () (void)))
(define scihe-console (current-output-port))
(define (scihe-frame id kind str)
  (put-string scihe-console (string (integer->char 29)))
  (put-string scihe-console "SCIHE ")
  (write id scihe-console)
  (put-string scihe-console " ")
  (put-string scihe-console kind)
  (put-string scihe-console " ")
  (write str scihe-console)
  (newline scihe-console)
  (flush-output-port scihe-console))
(define (scihe-show v) (call-with-string-output-port (lambda (p) (write v p))))
(define (scihe-cond e) (call-with-string-output-port (lambda (p) (display-condition e p))))
(define (scihe-eval id code)
  (let ([out (make-custom-textual-output-port "scihe-out"
               (lambda (s start n) (scihe-frame id "out" (substring s start (+ start n))) n)
               #f #f #f)]
        [ip (open-string-input-port code)])
    (call/cc
      (lambda (k)
        (parameterize ([keyboard-interrupt-handler
                         (lambda () (flush-output-port out) (scihe-frame id "err" "interrupted") (k #f))]
                       [current-output-port out])
          (guard (e [#t (flush-output-port out) (scihe-frame id "err" (scihe-cond e))])
            (let loop ()
              (let ([x (read ip)])
                (unless (eof-object? x)
                  (call-with-values
                    (lambda () (eval x (interaction-environment)))
                    (lambda vals
                      (flush-output-port out)
                      (for-each (lambda (v) (unless (eq? v (void)) (scihe-frame id "value" (scihe-show v)))) vals)))
                  (loop))))))))
    (flush-output-port out)
    (scihe-frame id "done" "")))
(scihe-frame 0 "ready" (scheme-version))
(let scihe-loop ()
  (let ([req (read)])
    (unless (eof-object? req)
      (scihe-eval (car req) (cadr req))
      (scihe-loop))))
`

// Chez is a Chez Scheme subprocess session.
type Chez struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	writes  chan string   // requests, written in order by writeLoop
	done    chan struct{} // closed by Close
	once    sync.Once
	mu      sync.Mutex
	nextID  int
	pending map[int]func(Result)
	closed  bool
	version string
	onClose func(error)
}

// StartChez launches bin (e.g. "chez" or "scheme") in dir.
func StartChez(bin, dir string, onClose func(error)) (*Chez, error) {
	if bin == "" {
		bin = "chez"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		if p2, err2 := exec.LookPath("scheme"); err2 == nil {
			path = p2
		} else {
			return nil, fmt.Errorf("chez scheme not found (%s)", bin)
		}
	}
	cmd := exec.Command(path, "-q")
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Chez{cmd: cmd, stdin: stdin, pending: map[int]func(Result){}, onClose: onClose,
		writes: make(chan string, 256), done: make(chan struct{})}
	ready := make(chan string, 1)
	go c.readLoop(stdout, ready)
	if _, err := io.WriteString(stdin, chezDriver); err != nil {
		c.Close()
		return nil, err
	}
	go c.writeLoop()
	select {
	case v := <-ready:
		c.version = v
	case <-time.After(10 * time.Second):
		c.Close()
		return nil, errors.New("chez scheme did not start")
	}
	return c, nil
}

// Name implements Session.
func (c *Chez) Name() string {
	if _, v, ok := strings.Cut(c.version, "Version "); ok {
		return "chez " + v
	}
	return "chez"
}

func (c *Chez) readLoop(r io.Reader, ready chan string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Text()
		// console output without a trailing newline can precede a frame
		if i := strings.Index(line, chezMarker); i > 0 {
			c.dispatch(c.running(), "out", line[:i], ready)
			line = line[i:]
		}
		if strings.HasPrefix(line, chezMarker) {
			parts := strings.SplitN(line[len(chezMarker):], " ", 3)
			if len(parts) == 3 {
				id, _ := strconv.Atoi(parts[0])
				payload, _ := ParseSchemeString(parts[2])
				c.dispatch(id, parts[1], payload, ready)
				continue
			}
		}
		// raw console output (e.g. from code writing to the console port)
		c.dispatch(c.running(), "out", line+"\n", ready)
	}
	if sc.Err() != nil {
		// e.g. a single output line over the scanner limit: the process
		// would block writing forever, so stop it
		c.Close()
	}
	c.mu.Lock()
	c.closed = true
	pend := c.pending
	c.pending = map[int]func(Result){}
	c.mu.Unlock()
	for _, cb := range pend {
		cb(Result{Err: "scheme process exited\n", Done: true})
	}
	err := c.cmd.Wait()
	if c.onClose != nil {
		c.onClose(err)
	}
}

// running is the request being evaluated: the oldest one still open
// (requests run in order); unframed console output belongs to it.
func (c *Chez) running() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur := 0
	for id := range c.pending {
		if cur == 0 || id < cur {
			cur = id
		}
	}
	return cur
}

func (c *Chez) dispatch(id int, kind, payload string, ready chan string) {
	if kind == "ready" {
		select {
		case ready <- payload:
		default:
		}
		return
	}
	c.mu.Lock()
	cb := c.pending[id]
	if kind == "done" {
		delete(c.pending, id)
	}
	c.mu.Unlock()
	if cb == nil {
		return
	}
	switch kind {
	case "out":
		cb(Result{Out: payload})
	case "err":
		cb(Result{Err: payload + "\n"})
	case "value":
		cb(Result{Value: payload})
	case "done":
		cb(Result{Done: true})
	}
}

// Eval implements Session. ns is ignored.
// Eval queues a request; the pipe is written by writeLoop, never under
// c.mu and never on the caller's goroutine (the UI): a process that stops
// reading its input cannot block the editor or the output reader.
func (c *Chez) Eval(code, ns string, cb func(Result)) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("scheme process is not running")
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = cb
	c.mu.Unlock()
	req := fmt.Sprintf("(%d %s)\n", id, SchemeString(code))
	select {
	case c.writes <- req:
		return nil
	default:
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return errors.New("scheme is not reading its input (too many queued evaluations)")
	}
}

func (c *Chez) writeLoop() {
	for {
		select {
		case req := <-c.writes:
			if _, err := io.WriteString(c.stdin, req); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

// Interrupt implements Session.
func (c *Chez) Interrupt() {
	cur := c.running()
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if cur != 0 && !closed && c.cmd.Process != nil {
		c.cmd.Process.Signal(os.Interrupt)
	}
}

// Close implements Session.
func (c *Chez) Close() {
	c.once.Do(func() { close(c.done) })
	c.stdin.Close()
	if p := c.cmd.Process; p != nil {
		syscall.Kill(-p.Pid, syscall.SIGKILL)
	}
}

// Alive implements Session.
func (c *Chez) Alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed
}

// SchemeString encodes s as a Scheme string literal.
func SchemeString(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&sb, `\x%x;`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// ParseSchemeString decodes a Scheme string literal as written by `write`.
func ParseSchemeString(lit string) (string, error) {
	if len(lit) < 2 || lit[0] != '"' || lit[len(lit)-1] != '"' {
		return lit, errors.New("not a string literal")
	}
	rs := []rune(lit[1 : len(lit)-1])
	var sb strings.Builder
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if r != '\\' || i+1 >= len(rs) {
			sb.WriteRune(r)
			continue
		}
		i++
		switch rs[i] {
		case 'n':
			sb.WriteByte('\n')
		case 't':
			sb.WriteByte('\t')
		case 'r':
			sb.WriteByte('\r')
		case 'a':
			sb.WriteByte('\a')
		case 'b':
			sb.WriteByte('\b')
		case 'v':
			sb.WriteByte('\v')
		case 'f':
			sb.WriteByte('\f')
		case '0':
			sb.WriteByte(0)
		case 'x':
			j := i + 1
			for j < len(rs) && rs[j] != ';' {
				j++
			}
			if n, err := strconv.ParseInt(string(rs[i+1:j]), 16, 32); err == nil && j < len(rs) {
				sb.WriteRune(rune(n))
				i = j
			} else {
				sb.WriteRune('x')
			}
		default:
			sb.WriteRune(rs[i])
		}
	}
	return sb.String(), nil
}
