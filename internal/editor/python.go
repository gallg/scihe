package editor

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"scihe/internal/buffer"
	"scihe/internal/config"
)

type configOptions = config.Options

// pythonInterpreter resolves the configured python on PATH. Virtual
// environments are not managed by the editor: activate one in the shell
// before starting it and its python comes first on PATH.
func (e *Editor) pythonInterpreter() (string, error) {
	py := e.cfg.Python
	if py == "" {
		py = "python3"
	}
	p, err := exec.LookPath(expandHome(py))
	if err != nil {
		return "", errors.New("no python interpreter found (" + py + ")")
	}
	return p, nil
}

// exRun runs the current file (python, scheme, clojure, go, else shebang/sh).
func exRun(e *Editor, a *exArgs) error {
	if strings.TrimSpace(a.arg) == "stop" {
		return e.stopJobs("*run*")
	}
	if e.mappingDepth > 0 {
		return errMappingProgram
	}
	b := e.win.buf
	if b.Path == "" {
		return errors.New("E32: No file name")
	}
	if e.modified(b) {
		if err := e.writeBuffer(b, "", false); err != nil {
			return err
		}
	}
	args := splitArgs(a.arg) // quotes group words: -c "print(1)"
	var cmd *exec.Cmd
	switch b.Filetype {
	case "python":
		py, err := e.pythonInterpreter()
		if err != nil {
			return err
		}
		cmd = exec.Command(py, append([]string{"-u", b.Path}, args...)...)
	case "scheme":
		cmd = exec.Command(e.cfg.Scheme, append([]string{"--script", b.Path}, args...)...)
	case "clojure":
		cmd = exec.Command("clojure", append([]string{"-M", b.Path}, args...)...)
	case "go":
		cmd = exec.Command("go", append([]string{"run", b.Path}, args...)...)
	case "markdown":
		return errors.New("markdown files can't be run (<leader>mr renders formulas)")
	default:
		if st, err := os.Stat(b.Path); err == nil && st.Mode()&0o111 != 0 {
			cmd = exec.Command(b.Path, args...)
		} else {
			cmd = exec.Command("sh", append([]string{b.Path}, args...)...)
		}
	}
	cmd.Dir = filepath.Dir(b.Path)
	e.runAsync(cmd, "*run*", func(err error) {
		if err != nil {
			e.warnf("%s: %v", b.Name, err)
		} else {
			e.infof("%s finished", b.Name)
		}
	})
	return nil
}

// runAsync runs cmd, streaming its output into a scratch buffer.
func (e *Editor) runAsync(cmd *exec.Cmd, bufName string, done func(error)) {
	out := e.scratch(bufName, buffer.KindRepl)
	out.SetText("$ " + strings.Join(cmd.Args, " ") + "\n")
	out.ReadOnly = true
	e.showLog(out)
	w := &logWriter{e: e, b: out}
	cmd.Stdout, cmd.Stderr = w, w
	if cmd.Env == nil {
		cmd.Env = os.Environ()
	}
	// its own process group: :run stop and quitting kill all of it, and
	// it cannot read the terminal (it would be stopped instead of
	// stealing keystrokes from the editor)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		e.errorf("%v", err)
		return
	}
	e.jobsMu.Lock()
	e.jobs[cmd.Process] = bufName
	e.jobsMu.Unlock()
	e.safeGo(func() {
		err := cmd.Wait()
		e.jobsMu.Lock()
		delete(e.jobs, cmd.Process)
		e.jobsMu.Unlock()
		rest := w.flush() // Wait returns after the output is copied
		e.Post(func() {
			if rest != "" {
				e.appendText(out, rest)
			}
			status := "[done]"
			if err != nil {
				status = "[" + err.Error() + "]"
			}
			e.appendText(out, status+"\n")
			if done != nil {
				done(err)
			}
		})
	})
}

// stopJobs ends the background jobs writing into the named buffer.
func (e *Editor) stopJobs(bufName string) error {
	e.jobsMu.Lock()
	defer e.jobsMu.Unlock()
	n := 0
	for p, name := range e.jobs {
		if name == bufName {
			syscall.Kill(-p.Pid, syscall.SIGTERM)
			n++
		}
	}
	if n == 0 {
		return errors.New("nothing is running")
	}
	e.infof("stopped %d job(s)", n)
	return nil
}
