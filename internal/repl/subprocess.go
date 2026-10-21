package repl

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Subprocess drives any REPL program on a pipe (Racket, SBCL, …). After
// each evaluation it sends a sentinel expression that prints an end
// marker ("\x1dSCIHE <id> done"); everything the program prints until the
// marker belongs to that request, and its last non-empty line is the
// value. Standard error is merged with standard output, so errors are not
// told apart from output: this is the price of a protocol-free REPL.
type Subprocess struct {
	name     string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	sentinel string
	prompt   *regexp.Regexp
	nonce    string // random per session: output cannot fake an end marker
	writes   chan string
	done     chan struct{}
	once     sync.Once

	mu      sync.Mutex
	nextID  int
	queue   []*subReq // in evaluation order
	closed  bool
	onClose func(error)
}

type subReq struct {
	id   int
	cb   func(Result)
	last string // last non-empty output line
}

const subMarker = chezMarker // "\x1dSCIHE ": the same marker as the Chez driver

// StartSubprocess launches argv in dir. sentinel must print the end
// marker with {id} replaced by the request id; prompt (optional) is
// stripped from the start of output lines.
func StartSubprocess(name string, argv []string, dir, sentinel string, prompt *regexp.Regexp, onClose func(error)) (*Subprocess, error) {
	if len(argv) == 0 {
		return nil, errors.New("no command")
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return nil, fmt.Errorf("%s not found on PATH", argv[0])
	}
	cmd := exec.Command(path, argv[1:]...)
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
	s := &Subprocess{name: name, cmd: cmd, stdin: stdin, sentinel: sentinel, prompt: prompt,
		nonce:  strconv.FormatInt(rand.Int64N(1<<62), 36),
		writes: make(chan string, 256), done: make(chan struct{}), onClose: onClose}
	go s.readLoop(stdout)
	go s.writeLoop()
	return s, nil
}

// Name implements Session.
func (s *Subprocess) Name() string { return s.name }

// Alive implements Session.
func (s *Subprocess) Alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed
}

// Eval implements Session (ns is ignored).
func (s *Subprocess) Eval(code, ns string, cb func(Result)) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("%s is not running", s.name)
	}
	s.nextID++
	id := s.nextID
	s.queue = append(s.queue, &subReq{id: id, cb: cb})
	s.mu.Unlock()
	// the blank line ends an indented block (Python) before the sentinel
	msg := strings.TrimRight(code, "\n") + "\n\n" + strings.ReplaceAll(s.sentinel, "{id}", s.nonce+"-"+strconv.Itoa(id)) + "\n"
	select {
	case s.writes <- msg:
		return nil
	default:
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, q := range s.queue {
			if q.id == id {
				s.queue = append(s.queue[:i], s.queue[i+1:]...)
				return fmt.Errorf("%s is not reading its input (too many queued evaluations)", s.name)
			}
		}
		return nil // the process ended meanwhile: the request was completed with its error
	}
}

func (s *Subprocess) writeLoop() {
	for {
		select {
		case msg := <-s.writes:
			if _, err := io.WriteString(s.stdin, msg); err != nil {
				return
			}
		case <-s.done:
			return
		}
	}
}

// readLoop delivers output to the oldest open request and completes it
// at its end marker.
func (s *Subprocess) readLoop(r io.Reader) {
	br := bufio.NewReader(r)
	var err error
	for {
		var line string
		line, err = br.ReadString('\n')
		if line != "" {
			s.handleLine(line)
		}
		if err != nil {
			break
		}
	}
	if werr := s.cmd.Wait(); werr != nil { // reap the process
		err = werr
	}
	s.mu.Lock()
	s.closed = true
	pending := s.queue
	s.queue = nil
	s.mu.Unlock()
	for _, q := range pending {
		q.cb(Result{Err: s.name + " exited\n", Done: true})
	}
	if s.onClose != nil {
		s.onClose(err)
	}
}

func (s *Subprocess) handleLine(line string) {
	if s.prompt != nil {
		line = s.prompt.ReplaceAllString(line, "")
	}
	text, marker, hasMarker := strings.Cut(line, subMarker)
	s.mu.Lock()
	var head *subReq
	if len(s.queue) > 0 {
		head = s.queue[0]
	}
	if hasMarker {
		f := strings.Fields(marker)
		nonce, num, _ := strings.Cut(f0(f), "-")
		if len(f) >= 2 && f[1] == "done" && nonce == s.nonce {
			if id, err := strconv.Atoi(num); err == nil {
				// complete every request up to this id (a lost marker
				// must not leave the queue stuck)
				var finished []*subReq
				for len(s.queue) > 0 && s.queue[0].id <= id {
					finished = append(finished, s.queue[0])
					s.queue = s.queue[1:]
				}
				s.mu.Unlock()
				if head != nil && strings.TrimSpace(text) != "" {
					head.last = strings.TrimSpace(text)
					head.cb(Result{Out: text + "\n"})
				}
				for _, q := range finished {
					q.cb(Result{Value: q.last, Done: true})
				}
				return
			}
		}
	}
	if head != nil && strings.TrimSpace(line) != "" {
		head.last = strings.TrimSpace(strings.TrimRight(line, "\n"))
	}
	s.mu.Unlock()
	if head != nil && line != "" {
		head.cb(Result{Out: line})
	}
}

func f0(f []string) string {
	if len(f) == 0 {
		return ""
	}
	return f[0]
}

// Interrupt implements Session: SIGINT to the process group.
func (s *Subprocess) Interrupt() {
	if p := s.cmd.Process; p != nil && s.Alive() {
		syscall.Kill(-p.Pid, syscall.SIGINT)
	}
}

// Close implements Session.
func (s *Subprocess) Close() {
	s.once.Do(func() { close(s.done) })
	s.stdin.Close()
	if p := s.cmd.Process; p != nil {
		syscall.Kill(-p.Pid, syscall.SIGKILL)
	}
}
