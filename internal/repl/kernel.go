package repl

import (
	"fmt"
	"strings"
	"sync"

	"scihe/internal/notebook"
)

// KernelSession makes the built-in Python kernel (the one notebooks use)
// a Session, so .py buffers are evaluated like the Lisp languages.
// Kernel events map onto Results: stream → Out, execute_result →
// Value, error → Err ("Name: message" first, then the traceback).
// Evaluations sent before the kernel is ready wait for it.
type KernelSession struct {
	k       *notebook.Kernel
	onImage func(notebook.Event)
	onClose func(error)
	jobs    chan kernelJob // written to the kernel in order by writeLoop
	done    chan struct{}
	once    sync.Once

	mu      sync.Mutex
	ready   bool
	version string
	waiting []kernelJob // evaluations sent before "ready"
	pending map[int]func(Result)
	closed  bool
	waitErr strings.Builder // output before ready (startup errors)
}

type kernelJob struct {
	code string
	cb   func(Result)
}

// StartKernel launches python with the kernel driver in dir. onImage
// receives image outputs (figures); onClose runs when the kernel exits.
func StartKernel(python, dir string, env []string, onImage func(notebook.Event), onClose func(error)) (*KernelSession, error) {
	s := &KernelSession{onImage: onImage, onClose: onClose, pending: map[int]func(Result){},
		jobs: make(chan kernelJob, 256), done: make(chan struct{})}
	k, err := notebook.Start(python, dir, env, s.event)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.k = k
	s.mu.Unlock()
	go s.writeLoop()
	return s, nil
}

// writeLoop hands jobs to the kernel off the caller's goroutine (the
// UI): a kernel still running an earlier request reads no input.
func (s *KernelSession) writeLoop() {
	for {
		select {
		case j := <-s.jobs:
			if _, err := s.k.ExecuteWith(j.code, func(id int) {
				s.mu.Lock()
				s.pending[id] = j.cb
				s.mu.Unlock()
			}); err != nil {
				j.cb(Result{Err: err.Error() + "\n", Done: true})
			}
		case <-s.done:
			return
		}
	}
}

func (s *KernelSession) event(ev notebook.Event) {
	s.mu.Lock()
	switch ev.Type {
	case "ready":
		s.ready, s.version = true, ev.Python
		waiting := s.waiting
		s.waiting = nil
		s.mu.Unlock()
		for _, j := range waiting {
			s.submit(j)
		}
		return
	case "exit":
		s.closed = true
		pending := s.pending
		s.pending = map[int]func(Result){}
		waiting := s.waiting
		s.waiting = nil
		msg := ev.Text
		if tail := strings.TrimSpace(s.waitErr.String()); tail != "" && !s.ready {
			msg += ": " + tail
		}
		s.mu.Unlock()
		for _, cb := range pending {
			cb(Result{Err: msg + "\n", Done: true})
		}
		for _, j := range waiting {
			j.cb(Result{Err: msg + "\n", Done: true})
		}
		s.once.Do(func() { close(s.done) })
		if s.onClose != nil {
			s.onClose(nil)
		}
		return
	}
	if !s.ready && ev.Type == "stream" && s.waitErr.Len() < 8192 {
		s.waitErr.WriteString(ev.Text)
	}
	cb := s.pending[ev.ID]
	if ev.Type == "done" {
		delete(s.pending, ev.ID)
	}
	s.mu.Unlock()
	if cb == nil {
		return
	}
	switch ev.Type {
	case "stream":
		cb(Result{Out: ev.Text})
	case "execute_result":
		if t, ok := ev.Data["text/plain"].(string); ok {
			cb(Result{Value: t})
		}
	case "display_data":
		if _, ok := ev.Data["image/png"]; ok {
			cb(Result{Out: "[image/png output — :nbimage opens it; use a notebook to keep figures]\n"})
			if s.onImage != nil {
				s.onImage(ev)
			}
		} else if t, ok := ev.Data["text/plain"].(string); ok {
			cb(Result{Out: t + "\n"})
		}
	case "error":
		cb(Result{Err: fmt.Sprintf("%s: %s\n%s\n", ev.Ename, ev.Evalue, strings.Join(ev.Traceback, "\n"))})
	case "done":
		cb(Result{Done: true})
	}
}

// submit queues a job for writeLoop without blocking.
func (s *KernelSession) submit(j kernelJob) {
	select {
	case s.jobs <- j:
	default:
		j.cb(Result{Err: "python kernel is not reading its input (too many queued evaluations)\n", Done: true})
	}
}

// Name implements Session.
func (s *KernelSession) Name() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.version != "" {
		return "python " + s.version
	}
	return "python (starting)"
}

// Alive implements Session.
func (s *KernelSession) Alive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.k != nil && s.k.Alive()
}

// Eval implements Session (ns is ignored); it never blocks.
func (s *KernelSession) Eval(code, ns string, cb func(Result)) error {
	j := kernelJob{code, cb}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("python kernel is not running")
	}
	if !s.ready {
		s.waiting = append(s.waiting, j)
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	s.submit(j)
	return nil
}

// Interrupt implements Session.
func (s *KernelSession) Interrupt() {
	s.mu.Lock()
	k := s.k
	s.mu.Unlock()
	if k != nil {
		k.Interrupt()
	}
}

// Close implements Session; evaluations still waiting get an error.
func (s *KernelSession) Close() {
	s.mu.Lock()
	k := s.k
	s.closed = true
	waiting := s.waiting
	s.waiting = nil
	s.mu.Unlock()
	for _, j := range waiting {
		j.cb(Result{Err: "python kernel closed\n", Done: true})
	}
	s.once.Do(func() { close(s.done) })
	if k != nil {
		k.Close()
	}
}
