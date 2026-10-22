package notebook

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
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
	Pgid           int            `json:"pgid"` // "child": the running `!command` (0: none)
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

	// output written straight to fd 1/2 (child processes) is batched:
	// an unbuffered child would otherwise send one event per line
	rawMu    sync.Mutex
	raw      []rawChunk
	rawSize  int
	rawTimer *time.Timer

	// the driver ends each request with a sync marker on fd 1 and 2;
	// "done" waits for both so direct output is not delivered after it
	syncMu sync.Mutex
	synced map[string]int // stream -> last request id synced
	syncCh chan struct{}

	child int // process group of the running `!command` (k.mu)
}

type rawChunk struct {
	id   int
	name string
	text []byte
}

// rawDelay is how long direct output may wait to be batched;
// rawMax flushes a batch early.
const (
	rawDelay = 15 * time.Millisecond
	rawMax   = 64 << 10
)

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
	k := &Kernel{cmd: cmd, stdin: stdin, onEv: onEvent,
		synced: map[string]int{}, syncCh: make(chan struct{}, 1)}
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
		k.flushRaw()
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
		if ev.Type == "child" {
			k.mu.Lock()
			k.child = ev.Pgid
			k.mu.Unlock()
			continue
		}
		if ev.Type == "done" {
			k.waitSync(ev.ID)
		}
		// direct output read so far was written before this event
		k.flushRaw()
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

// syncTag starts the driver's end-of-request marker on fd 1 and 2:
// "\x1dSCIHE-SYNC <id>\x1d".
const syncTag = "\x1dSCIHE-SYNC "

// readRaw forwards output written directly to fd 1/2 (e.g. by subprocesses).
func (k *Kernel) readRaw(r io.Reader, name string) {
	buf := make([]byte, 32<<10)
	var carry []byte // an incomplete marker from the previous read
	for {
		n, err := r.Read(buf)
		if n > 0 {
			k.mu.Lock()
			id := k.cur
			k.mu.Unlock()
			data := append(carry, buf[:n]...)
			carry = nil
			for {
				i := bytes.Index(data, []byte(syncTag))
				if i < 0 {
					break
				}
				j := bytes.IndexByte(data[i+len(syncTag):], 0x1d)
				if j < 0 {
					break
				}
				k.addRaw(id, name, data[:i])
				done, _ := strconv.Atoi(string(data[i+len(syncTag) : i+len(syncTag)+j]))
				data = data[i+len(syncTag)+j+1:]
				k.markSynced(name, done)
			}
			keep := partialMarker(data)
			k.addRaw(id, name, data[:len(data)-keep])
			carry = append([]byte(nil), data[len(data)-keep:]...)
		}
		if err != nil {
			k.mu.Lock()
			id := k.cur
			k.mu.Unlock()
			k.addRaw(id, name, carry)
			k.flushRaw()
			k.markSynced(name, int(^uint(0)>>1)) // no more output: never wait
			return
		}
	}
}

// partialMarker is the length of the end of data that may be the start
// of a sync marker continued by the next read.
func partialMarker(data []byte) int {
	tag := []byte(syncTag)
	if i := bytes.LastIndex(data, tag); i >= 0 && len(data)-i <= len(syncTag)+20 {
		return len(data) - i // the id and the closing byte are still to come
	}
	for n := min(len(tag)-1, len(data)); n > 0; n-- {
		if bytes.HasPrefix(tag, data[len(data)-n:]) {
			return n
		}
	}
	return 0
}

func (k *Kernel) markSynced(name string, id int) {
	k.syncMu.Lock()
	if id > k.synced[name] {
		k.synced[name] = id
	}
	k.syncMu.Unlock()
	select {
	case k.syncCh <- struct{}{}:
	default:
	}
}

// waitSync waits (briefly) until fd 1 and 2 are synced up to request id.
func (k *Kernel) waitSync(id int) {
	deadline := time.After(500 * time.Millisecond)
	for {
		k.syncMu.Lock()
		ok := k.synced["stdout"] >= id && k.synced["stderr"] >= id
		k.syncMu.Unlock()
		if ok {
			return
		}
		select {
		case <-k.syncCh:
		case <-deadline:
			return
		}
	}
}

// addRaw queues direct output; it is sent after rawDelay, when the batch
// reaches rawMax, or before the next protocol event (print output,
// results, "done"), so the order of outputs is kept.
func (k *Kernel) addRaw(id int, name string, p []byte) {
	if len(p) == 0 {
		return
	}
	k.rawMu.Lock()
	if n := len(k.raw); n > 0 && k.raw[n-1].id == id && k.raw[n-1].name == name {
		k.raw[n-1].text = append(k.raw[n-1].text, p...)
	} else {
		k.raw = append(k.raw, rawChunk{id: id, name: name, text: append([]byte(nil), p...)})
	}
	k.rawSize += len(p)
	full := k.rawSize >= rawMax
	if !full && k.rawTimer == nil {
		k.rawTimer = time.AfterFunc(rawDelay, k.flushRaw)
	}
	k.rawMu.Unlock()
	if full {
		k.flushRaw()
	}
}

// flushRaw sends the queued direct output, in order (the lock is held
// while sending so concurrent flushes cannot reorder chunks).
func (k *Kernel) flushRaw() {
	k.rawMu.Lock()
	defer k.rawMu.Unlock()
	if k.rawTimer != nil {
		k.rawTimer.Stop()
		k.rawTimer = nil
	}
	chunks := k.raw
	k.raw, k.rawSize = nil, 0
	for _, c := range chunks {
		k.onEv(Event{ID: c.id, Type: "stream", Name: c.name, Text: string(c.text)})
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
	// a running `!command` has its own process group
	k.mu.Lock()
	child := k.child
	k.mu.Unlock()
	if child > 0 {
		syscall.Kill(-child, syscall.SIGKILL)
	}
}
