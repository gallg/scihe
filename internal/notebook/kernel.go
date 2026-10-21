package notebook

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	_ "embed"
)

//go:embed driver.py
var driverSource string

// Event is a message from the kernel.
type Event struct {
	ID             int            `json:"id"`
	Type           string         `json:"type"` // ready, stream, execute_result, display_data, error, done, exit
	Name           string         `json:"name"`
	Text           string         `json:"text"`
	Data           map[string]any `json:"data"`
	ExecutionCount int            `json:"execution_count"`
	Status         string         `json:"status"`
	Ename          string         `json:"ename"`
	Evalue         string         `json:"evalue"`
	Traceback      []string       `json:"traceback"`
	Python         string         `json:"python"`
}

// Output converts an output event into an nbformat output object.
func (e Event) Output() map[string]any {
	switch e.Type {
	case "stream":
		return map[string]any{"output_type": "stream", "name": e.Name, "text": e.Text}
	case "execute_result":
		return map[string]any{"output_type": "execute_result", "execution_count": e.ExecutionCount, "data": normalizeData(e.Data), "metadata": map[string]any{}}
	case "display_data":
		return map[string]any{"output_type": "display_data", "data": normalizeData(e.Data), "metadata": map[string]any{}}
	case "error":
		tb := make([]any, len(e.Traceback))
		for i, s := range e.Traceback {
			tb[i] = s
		}
		return map[string]any{"output_type": "error", "ename": e.Ename, "evalue": e.Evalue, "traceback": tb}
	}
	return nil
}

// normalizeData splits text mime values into line lists like Jupyter.
func normalizeData(d map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range d {
		if s, ok := v.(string); ok && k != "image/png" && k != "image/jpeg" {
			out[k] = SplitLines(s)
		} else {
			out[k] = v
		}
	}
	return out
}

// Kernel is a running Python driver process.
type Kernel struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	mu     sync.Mutex
	nextID int
	cur    int
	busy   bool
	wmu    sync.Mutex // serializes writes to stdin
	closed bool
	onEv   func(Event)
	wg     sync.WaitGroup
}

// Start launches python running the driver in dir with env. onEvent is
// called from background goroutines for every event.
func Start(python, dir string, env []string, onEvent func(Event)) (*Kernel, error) {
	if python == "" {
		return nil, errors.New("no python interpreter")
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(python, "-u", "-c", driverSource)
	cmd.Dir = dir
	cmd.Env = append(env, "MPLBACKEND=Agg", "PYTHONUNBUFFERED=1", "PYTHONIOENCODING=utf-8")
	cmd.ExtraFiles = []*os.File{pw}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		pr.Close()
		pw.Close()
		return nil, err
	}
	// plain pipes (not StdoutPipe) so cmd.Wait does not depend on the
	// readers: a background child holding fd 1/2 must not hide an exit
	outR, outW, err := os.Pipe()
	if err != nil {
		pr.Close()
		pw.Close()
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		pr.Close()
		pw.Close()
		outR.Close()
		outW.Close()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	if err := cmd.Start(); err != nil {
		for _, f := range []*os.File{pr, pw, outR, outW, errR, errW} {
			f.Close()
		}
		return nil, err
	}
	pw.Close()
	outW.Close()
	errW.Close()
	k := &Kernel{cmd: cmd, stdin: stdin, onEv: onEvent}
	k.wg.Add(1)
	go k.readProto(pr)
	go k.readRaw(outR, "stdout")
	go k.readRaw(errR, "stderr")
	go func() {
		err := cmd.Wait()
		// deliver protocol lines written before the exit
		done := make(chan struct{})
		go func() { k.wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Second):
			pr.Close()
		}
		time.AfterFunc(time.Second, func() { outR.Close(); errR.Close() })
		k.mu.Lock()
		k.closed = true
		k.busy = false
		k.mu.Unlock()
		msg := "kernel exited"
		if err != nil {
			msg = fmt.Sprintf("kernel exited: %v", err)
		}
		onEvent(Event{Type: "exit", Text: msg})
	}()
	return k, nil
}

func (k *Kernel) readProto(r io.ReadCloser) {
	defer k.wg.Done()
	defer r.Close()
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 256<<20)
	for sc.Scan() {
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue
		}
		if ev.Type == "done" {
			k.mu.Lock()
			if ev.ID == k.cur {
				k.busy = false
			}
			k.mu.Unlock()
		}
		k.onEv(ev)
	}
}

// readRaw forwards output written directly to fd 1/2 (e.g. by subprocesses).
func (k *Kernel) readRaw(r io.Reader, name string) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			k.mu.Lock()
			id := k.cur
			k.mu.Unlock()
			k.onEv(Event{ID: id, Type: "stream", Name: name, Text: string(buf[:n])})
		}
		if err != nil {
			return
		}
	}
}

// Execute sends code for execution and returns the request id.
func (k *Kernel) Execute(code string) (int, error) { return k.ExecuteWith(code, nil) }

// ExecuteWith sends code; before (when set) runs with the request id
// before the request is written, so the caller can route events that
// arrive at once. The write happens outside k.mu: a driver busy with an
// earlier request (not reading its input) blocks only this call, never
// the output readers or Close.
func (k *Kernel) ExecuteWith(code string, before func(id int)) (int, error) {
	k.mu.Lock()
	if k.closed {
		k.mu.Unlock()
		return 0, errors.New("kernel is not running")
	}
	k.nextID++
	id := k.nextID
	k.cur = id
	k.busy = true
	k.mu.Unlock()
	if before != nil {
		before(id)
	}
	msg, _ := json.Marshal(map[string]any{"id": id, "code": code})
	k.wmu.Lock()
	_, err := k.stdin.Write(append(msg, '\n'))
	k.wmu.Unlock()
	if err != nil {
		return 0, err
	}
	return id, nil
}

// Busy reports whether a request is running.
func (k *Kernel) Busy() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.busy
}

// Alive reports whether the process is still running.
func (k *Kernel) Alive() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return !k.closed
}

// Interrupt sends SIGINT to the running code.
func (k *Kernel) Interrupt() {
	k.mu.Lock()
	busy, closed := k.busy, k.closed
	k.mu.Unlock()
	if busy && !closed && k.cmd.Process != nil {
		k.cmd.Process.Signal(os.Interrupt)
	}
}

// Close stops the kernel process (and its process group).
func (k *Kernel) Close() {
	k.mu.Lock()
	closed := k.closed
	k.mu.Unlock()
	if closed {
		return
	}
	k.stdin.Close()
	if p := k.cmd.Process; p != nil {
		syscall.Kill(-p.Pid, syscall.SIGKILL)
	}
}
