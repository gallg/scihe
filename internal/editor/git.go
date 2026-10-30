package editor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"scihe/internal/buffer"
	"scihe/internal/keymap"
)

// A deliberately small git integration: a status buffer to stage,
// unstage, discard and commit, plus diff, log, blame and running any
// non-interactive git subcommand. Everything shells out to git.

// gitEnv makes git fail instead of waiting for input the editor cannot
// provide (an editor, a credential prompt, an ssh passphrase).
func gitEnv() []string {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=false", "GIT_SEQUENCE_EDITOR=false",
		"GIT_PAGER=cat", "PAGER=cat", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C")
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	return env
}

// gitTimeout bounds the synchronous git calls made on the main loop.
const gitTimeout = 20 * time.Second

// git runs git in dir and returns stdout; stderr is folded into the error.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	// kill git's helpers too (textconv, credential helpers, ssh): they
	// inherit the pipes and would keep Run waiting past the timeout
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errors.New(msg)
	}
	return out.String(), nil
}

// gitRoot returns the work tree root containing dir.
func gitRoot(dir string) (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", errors.New("git is not installed")
	}
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", dir)
	}
	return strings.TrimSpace(out), nil
}

// bufferDir is the directory git commands for b run in.
func (e *Editor) bufferDir(b *buffer.Buffer) string {
	if d, ok := e.explorer[b.ID]; ok {
		return d
	}
	if gs := e.gitViews[b.ID]; gs != nil {
		return gs.root
	}
	if b.Path != "" {
		return filepath.Dir(b.Path)
	}
	return e.cwd
}

// ---------------------------------------------------------------- branch

// gitHeads caches the branch shown in the status line per directory;
// lookups run in the background so drawing never waits for git.
type gitHeads struct {
	mu sync.Mutex
	m  map[string]gitHead
}

type gitHead struct {
	name    string // "" outside a repository
	at      time.Time
	pending bool
}

const gitHeadTTL = 5 * time.Second

// gitBranch returns the cached branch for dir and refreshes it when stale.
func (e *Editor) gitBranch(dir string) string {
	h := &e.heads
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.m == nil {
		h.m = map[string]gitHead{}
	}
	cur := h.m[dir]
	if !cur.pending && time.Since(cur.at) > gitHeadTTL {
		if _, err := exec.LookPath("git"); err != nil {
			cur.at = time.Now().Add(time.Hour)
			h.m[dir] = cur
			return ""
		}
		cur.pending = true
		h.m[dir] = cur
		e.safeGo(func() {
			name := ""
			if out, err := git(dir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
				name = strings.TrimSpace(out)
			} else if out, err := git(dir, "rev-parse", "--short", "HEAD"); err == nil {
				name = "(" + strings.TrimSpace(out) + ")" // detached
			}
			h.mu.Lock()
			old := h.m[dir]
			h.m[dir] = gitHead{name: name, at: time.Now()}
			h.mu.Unlock()
			if old.name != name {
				e.Post(func() {})
			}
		})
	}
	return cur.name
}

// invalidateGitHeads makes the next draw ask git again (after a commit,
// checkout, …).
func (e *Editor) invalidateGitHeads() {
	e.heads.mu.Lock()
	for k, v := range e.heads.m {
		if !v.pending {
			v.at = time.Time{}
			e.heads.m[k] = v
		}
	}
	e.heads.mu.Unlock()
}

// ---------------------------------------------------------------- status

type gitEntry struct {
	path    string // relative to the root
	code    string // two-letter porcelain code
	section int    // gitStaged, gitUnstaged, gitUntracked
}

const (
	gitStaged = iota
	gitUnstaged
	gitUntracked
	gitConflict
)

var gitSectionTitle = []string{"Staged changes", "Unstaged changes", "Untracked files", "Merge conflicts"}

// gitView is the state of a *git* status buffer.
type gitView struct {
	root    string
	status  bool             // a status buffer (else a log)
	entries map[int]gitEntry // line -> file
}

// parseGitStatus parses `git status --porcelain=v1 -z -b` output.
func parseGitStatus(out string) (branch string, entries []gitEntry) {
	recs := strings.Split(out, "\x00")
	for i := 0; i < len(recs); i++ {
		r := recs[i]
		if len(r) < 3 {
			continue
		}
		if strings.HasPrefix(r, "## ") {
			branch = r[3:]
			continue
		}
		x, y, path := r[0], r[1], r[3:]
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			i++ // the original name follows a rename / copy
		}
		code := r[:2]
		switch {
		case code == "??":
			entries = append(entries, gitEntry{path, code, gitUntracked})
		case x == 'U' || y == 'U' || code == "AA" || code == "DD":
			entries = append(entries, gitEntry{path, code, gitConflict})
		default:
			if x != ' ' {
				entries = append(entries, gitEntry{path, code, gitStaged})
			}
			if y != ' ' {
				entries = append(entries, gitEntry{path, code, gitUnstaged})
			}
		}
	}
	return branch, entries
}

func gitCodeLabel(code string, section int) string {
	c := code[0]
	if section == gitUnstaged {
		c = code[1]
	}
	switch {
	case section == gitUntracked:
		return "new"
	case section == gitConflict:
		return "conflict"
	}
	switch c {
	case 'M':
		return "modified"
	case 'A':
		return "added"
	case 'D':
		return "deleted"
	case 'R':
		return "renamed"
	case 'C':
		return "copied"
	case 'T':
		return "typechange"
	}
	return string(c)
}

// openGitStatus shows (and refreshes) the status buffer for the
// repository of the current buffer.
func (e *Editor) openGitStatus() error {
	root, err := gitRoot(e.bufferDir(e.win.buf))
	if err != nil {
		return err
	}
	var b *buffer.Buffer
	for _, x := range e.buffers {
		if gv := e.gitViews[x.ID]; gv != nil && gv.status && gv.root == root {
			b = x
		}
	}
	if b == nil {
		b = buffer.New("*git " + filepath.Base(root) + "*")
		b.Kind = buffer.KindScratch
		e.addBuffer(b)
		e.gitViews[b.ID] = &gitView{root: root, status: true}
		e.localMaps[b.ID] = e.gitStatusKeys()
	}
	gv := e.gitViews[b.ID]
	if err := e.refreshGitStatus(b); err != nil {
		return err
	}
	e.showBuffer(b)
	// start on the first file
	for l := 0; l < b.LineCount(); l++ {
		if _, ok := gv.entries[l]; ok {
			e.setCursor(e.win, Pos{l, 0})
			break
		}
	}
	return nil
}

// refreshGitStatusOf refreshes the status buffers of a repository.
func (e *Editor) refreshGitStatusOf(root string) {
	for _, b := range e.buffers {
		if gv := e.gitViews[b.ID]; gv != nil && gv.root == root && gv.status {
			e.refreshGitStatus(b)
		}
	}
}

// showBuffer focuses a window showing b, or opens b in a split.
func (e *Editor) showBuffer(b *buffer.Buffer) {
	for _, w := range e.windows() {
		if w.buf == b {
			e.focus(w)
			return
		}
	}
	e.split(b, false)
}

func (e *Editor) refreshGitStatus(b *buffer.Buffer) error {
	gv := e.gitViews[b.ID]
	out, err := git(gv.root, "status", "--porcelain=v1", "-z", "-b", "--untracked-files=all")
	if err != nil {
		return err
	}
	branch, entries := parseGitStatus(out)
	lines := []string{"Head:  " + branch, "Repo:  " + gv.root}
	if last, err := git(gv.root, "log", "-1", "--format=%h %s"); err == nil && strings.TrimSpace(last) != "" {
		lines = append(lines, "Last:  "+strings.TrimSpace(last))
	}
	gv.entries = map[int]gitEntry{}
	for sec := range gitSectionTitle {
		var sel []gitEntry
		for _, en := range entries {
			if en.section == sec {
				sel = append(sel, en)
			}
		}
		if len(sel) == 0 {
			continue
		}
		lines = append(lines, "", fmt.Sprintf("%s (%d)", gitSectionTitle[sec], len(sel)))
		for _, en := range sel {
			gv.entries[len(lines)] = en
			lines = append(lines, fmt.Sprintf("  %-10s %s", gitCodeLabel(en.code, sec), en.path))
		}
	}
	if len(entries) == 0 {
		lines = append(lines, "", "Nothing to commit, working tree clean")
	}
	lines = append(lines, "",
		"s stage   u unstage   a stage all   U unstage all   x discard   c commit   <CR> open",
		"d diff    D diff all  l log         p push          F pull      R refresh  q close")
	b.SetText(strings.Join(lines, "\n"))
	e.forWindows(func(w *Window) {
		if w.buf == b {
			e.clampCursor(w)
		}
	})
	e.invalidateGitHeads()
	return nil
}

func (e *Editor) gitStatusKeys() *keymap.Map[*Cmd] {
	m := keymap.NewMap[*Cmd]()
	bind := func(k, label string, f func(c *ctx, gv *gitView, en *gitEntry)) {
		m.Bind(keymap.Parse(k, e.leader), label, action(func(c *ctx) {
			gv := e.gitViews[c.b.ID]
			if gv == nil {
				return
			}
			var en *gitEntry
			if x, ok := gv.entries[c.w.cur.Line]; ok {
				en = &x
			}
			f(c, gv, en)
		}))
	}
	run := func(c *ctx, gv *gitView, args ...string) {
		if _, err := git(gv.root, args...); err != nil {
			e.errorf("git %s: %v", args[0], err)
		}
		if err := e.refreshGitStatus(c.b); err != nil {
			e.errorf("%v", err)
		}
	}
	bind("s", "stage file", func(c *ctx, gv *gitView, en *gitEntry) {
		if en != nil {
			run(c, gv, "add", "--", literal(en.path))
		}
	})
	bind("u", "unstage file", func(c *ctx, gv *gitView, en *gitEntry) {
		if en == nil {
			return
		}
		if _, err := git(gv.root, "rev-parse", "-q", "--verify", "HEAD"); err != nil {
			run(c, gv, "rm", "--cached", "-q", "--", literal(en.path)) // no commit yet
			return
		}
		run(c, gv, "restore", "--staged", "--", literal(en.path))
	})
	bind("a", "stage all", func(c *ctx, gv *gitView, en *gitEntry) { run(c, gv, "add", "-A") })
	bind("U", "unstage all", func(c *ctx, gv *gitView, en *gitEntry) {
		staged := false
		for _, x := range gv.entries {
			if x.section == gitConflict {
				// restoring the index would mark the conflicts resolved
				e.warnf("resolve the merge conflicts first (u unstages one file)")
				return
			}
			staged = staged || x.section == gitStaged
		}
		if !staged {
			e.infof("nothing staged")
			return
		}
		if _, err := git(gv.root, "rev-parse", "-q", "--verify", "HEAD"); err != nil {
			run(c, gv, "rm", "--cached", "-r", "-q", "--", ".") // no commit yet
			return
		}
		run(c, gv, "restore", "--staged", "--", ".")
	})
	bind("x", "discard changes", func(c *ctx, gv *gitView, en *gitEntry) {
		if en == nil {
			return
		}
		if en.section != gitUnstaged {
			e.warnf("only unstaged changes of tracked files can be discarded")
			return
		}
		e.prompt("Discard changes to "+en.path+"? (y/N) ", "", func(s string, ok bool) {
			if confirmed(s, ok) {
				run(c, gv, "restore", "--", literal(en.path))
				e.reloadUnmodified(filepath.Join(gv.root, en.path))
			}
		})
	})
	bind("<CR>", "open file", func(c *ctx, gv *gitView, en *gitEntry) {
		if en != nil {
			e.openPath(c.w, filepath.Join(gv.root, en.path))
		}
	})
	bind("d", "diff file", func(c *ctx, gv *gitView, en *gitEntry) {
		if en == nil {
			return
		}
		switch en.section {
		case gitStaged:
			e.gitShowDiff(gv.root, "diff: "+en.path+" (staged)", "diff", "--cached", "--", literal(en.path))
		case gitUntracked:
			e.gitShowDiff(gv.root, "diff: "+en.path, "diff", "--no-index", "--", os.DevNull, en.path)
		default:
			e.gitShowDiff(gv.root, "diff: "+en.path, "diff", "--", literal(en.path))
		}
	})
	bind("D", "diff all", func(c *ctx, gv *gitView, en *gitEntry) {
		e.gitShowDiff(gv.root, "diff (all)", "diff", "HEAD")
	})
	bind("c", "commit", func(c *ctx, gv *gitView, en *gitEntry) { e.gitCommitBuffer(gv.root) })
	bind("l", "log", func(c *ctx, gv *gitView, en *gitEntry) { e.gitLog(gv.root, "") })
	bind("p", "push", func(c *ctx, gv *gitView, en *gitEntry) { e.gitAsync(gv.root, "push") })
	bind("F", "pull", func(c *ctx, gv *gitView, en *gitEntry) { e.gitAsync(gv.root, "pull", "--ff-only") })
	bind("R", "refresh", func(c *ctx, gv *gitView, en *gitEntry) {
		if err := e.refreshGitStatus(c.b); err != nil {
			e.errorf("%v", err)
		}
	})
	bind("q", "close", func(c *ctx, gv *gitView, en *gitEntry) { e.closeSpecial(c.w, c.b) })
	return m
}

// closeSpecial closes the window of a special buffer, or switches it to
// another buffer when it is the last window.
func (e *Editor) closeSpecial(w *Window, b *buffer.Buffer) {
	if !e.closeWindow(w) {
		if w.alt != nil && e.bufferAlive(w.alt) {
			e.setBuffer(w, w.alt)
		} else {
			e.deleteBuffer(b, true)
		}
	}
}

// closeReadOnly closes a read-only special buffer shown in w: a
// rendered markdown view gives way to its source, other buffers lose
// their window (or give way to the alternate buffer). Logs are kept
// alive for the REPL or job still writing into them.
func (e *Editor) closeReadOnly(w *Window, b *buffer.Buffer) {
	if r := e.renders[b.ID]; r != nil {
		e.leaveRender(r)
		return
	}
	if e.closeWindow(w) {
		return
	}
	if w.alt != nil && w.alt != b && e.bufferAlive(w.alt) {
		e.setBuffer(w, w.alt)
		return
	}
	if b.Kind != buffer.KindRepl {
		e.deleteBuffer(b, true)
		return
	}
	for _, x := range e.listedBuffers() {
		if x != b && x.Kind != buffer.KindPreview && x.Kind != buffer.KindRepl {
			e.setBuffer(w, x)
			return
		}
	}
	nb := buffer.New("[No Name]")
	e.addBuffer(nb)
	e.setBuffer(w, nb)
}

// openPath shows the file in w (names with spaces need no escaping).
func (e *Editor) openPath(w *Window, path string) {
	b, err := e.loadBuffer(path)
	if err != nil {
		e.errorf("%v", err)
		return
	}
	e.setBuffer(w, b)
}

// literal makes git take a file name as it is, not as a pattern
// ("[id].tsx" would also match "i.tsx"). With GIT_LITERAL_PATHSPECS set
// git already does, and would take the magic for part of the name.
func literal(path string) string {
	if v := os.Getenv("GIT_LITERAL_PATHSPECS"); v != "" && v != "0" {
		return path
	}
	return ":(literal)" + path
}

// gitRel is path relative to the work tree root. git reports the root
// with symlinks resolved (/home → /var/home), so the path is resolved
// too; a name starting with "-" gets a "./" so git cannot take it for an
// option.
func gitRel(root, path string) string {
	rel, err := filepath.Rel(root, realPath(path))
	if err != nil || strings.HasPrefix(rel, "..") {
		rel, _ = filepath.Rel(root, path)
	}
	if strings.HasPrefix(rel, "-") {
		rel = "." + string(filepath.Separator) + rel
	}
	return rel
}

// reloadUnmodified re-reads an open, unmodified buffer after git changed
// the file on disk.
func (e *Editor) reloadUnmodified(path string) {
	if b := e.findBuffer(path); b != nil && !e.modified(b) {
		if err := e.reload(b); err != nil {
			e.errorf("%v", err)
		}
	}
}

// reloadAllUnmodified refreshes every unmodified file buffer (after a
// pull, checkout, …).
func (e *Editor) reloadAllUnmodified() {
	for _, b := range e.buffers {
		if b.Kind == buffer.KindFile && b.Path != "" && !e.modified(b) {
			if st, err := os.Stat(b.Path); err == nil && st.ModTime().After(e.modTimes[b.ID]) {
				e.reload(b)
			}
		}
	}
}

// ---------------------------------------------------------------- views

// gitReadOnly shows git output in a read-only scratch buffer.
func (e *Editor) gitReadOnly(name, text, filetype string) *buffer.Buffer {
	b := e.scratch("*"+name+"*", buffer.KindScratch)
	b.SetText(strings.TrimSuffix(text, "\n"))
	b.ReadOnly = true
	b.Data["git"] = true
	b.Filetype = filetype
	e.showBuffer(b)
	e.setCursor(e.win, Pos{})
	e.win.top = 0
	return b
}

func (e *Editor) gitShowDiff(root, title string, args ...string) {
	out, err := git(root, args...)
	// `git diff --no-index` exits 1 when the files differ
	if err != nil && out == "" {
		e.errorf("git diff: %v", err)
		return
	}
	if strings.TrimSpace(out) == "" {
		e.infof("no differences")
		return
	}
	e.gitReadOnly(title, out, "diff")
}

// gitLog shows the history (of one file when path is set); <CR> on a
// line shows that commit.
func (e *Editor) gitLog(root, path string, extra ...string) {
	args := []string{"log", "--graph", "--decorate", "--date=short", "--format=%h %ad %an%d  %s", "-n", "500"}
	args = append(args, extra...)
	title := "git log"
	if len(extra) > 0 {
		title += " " + strings.Join(extra, " ")
	}
	if path != "" {
		args = append(args, "--follow", "--", literal(path))
		title += ": " + filepath.Base(path)
	}
	out, err := git(root, args...)
	if err != nil {
		e.errorf("git log: %v", err)
		return
	}
	if strings.TrimSpace(out) == "" {
		e.infof("no commits yet")
		return
	}
	b := e.gitReadOnly(title, out, "")
	e.gitViews[b.ID] = &gitView{root: root}
	m := keymap.NewMap[*Cmd]()
	show := func(c *ctx) {
		if h := commitHashIn(c.b.LineString(c.w.cur.Line)); h != "" {
			e.gitShowDiff(root, "commit "+h, "show", "--stat", "--patch", h)
		}
	}
	m.Bind(keymap.Parse("<CR>", e.leader), "show commit", action(show))
	m.Bind(keymap.Parse("q", e.leader), "close", action(func(c *ctx) { e.closeSpecial(c.w, c.b) }))
	e.localMaps[b.ID] = m
}

// commitHashIn finds the abbreviated hash in a `log --graph` line.
func commitHashIn(line string) string {
	for _, f := range strings.Fields(line) {
		if len(f) >= 7 && len(f) <= 40 && strings.Trim(f, "0123456789abcdef") == "" {
			return f
		}
		if strings.Trim(f, "*|/\\_") != "" {
			return "" // past the graph without finding a hash
		}
	}
	return ""
}

// gitBlame shows `git blame` for the current file, cursor on the same line.
func (e *Editor) gitBlame() error {
	b := e.win.buf
	if b.Path == "" || b.Kind != buffer.KindFile {
		return errors.New("blame needs a file buffer")
	}
	root, err := gitRoot(filepath.Dir(b.Path))
	if err != nil {
		return err
	}
	rel := gitRel(root, b.Path)
	out, err := git(root, "blame", "--date=short", "--", rel)
	if err != nil {
		return err
	}
	line := e.win.cur.Line
	nb := e.gitReadOnly("blame: "+rel, out, "")
	e.setCursor(e.win, nb.Clamp(Pos{line, 0}))
	return nil
}

// ---------------------------------------------------------------- commit

// gitCommitBuffer opens .git/COMMIT_EDITMSG for the message; ZZ (or
// :git commit while it is open) commits, ZQ aborts.
func (e *Editor) gitCommitBuffer(root string) {
	staged, err := git(root, "diff", "--cached", "--name-status")
	if err != nil {
		e.errorf("git: %v", err)
		return
	}
	if strings.TrimSpace(staged) == "" {
		e.warnf("nothing staged to commit (stage files with s / a in the status buffer)")
		return
	}
	gitDir, err := git(root, "rev-parse", "--absolute-git-dir")
	if err != nil {
		e.errorf("git: %v", err)
		return
	}
	path := filepath.Join(strings.TrimSpace(gitDir), "COMMIT_EDITMSG")
	if old := e.findBuffer(path); old != nil {
		e.showBuffer(old)
		return
	}
	var sb strings.Builder
	sb.WriteString("\n# Write the commit message above. Lines starting with '#' are ignored.\n")
	sb.WriteString("# ZZ commits, ZQ aborts.\n#\n# Changes to be committed:\n")
	for _, l := range strings.Split(strings.TrimSpace(staged), "\n") {
		sb.WriteString("#   " + strings.ReplaceAll(l, "\t", "  ") + "\n")
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		e.errorf("%v", err)
		return
	}
	b, err := e.loadBuffer(path)
	if err != nil {
		e.errorf("%v", err)
		return
	}
	e.gitCommits[b.ID] = root
	m := keymap.NewMap[*Cmd]()
	m.Bind(keymap.Parse("ZZ", e.leader), "commit", action(func(c *ctx) {
		if err := e.gitCommit(c.b); err != nil {
			e.errorf("%v", err)
		}
	}))
	m.Bind(keymap.Parse("ZQ", e.leader), "abort commit", action(func(c *ctx) {
		delete(e.gitCommits, c.b.ID)
		b := c.b
		e.closeSpecial(c.w, b)
		if e.bufferAlive(b) {
			e.deleteBuffer(b, true)
		}
		e.infof("commit aborted")
	}))
	e.localMaps[b.ID] = m
	e.showBuffer(b)
	e.setCursor(e.win, Pos{})
}

// gitCommit commits with the message in the commit buffer b.
func (e *Editor) gitCommit(b *buffer.Buffer) error {
	root, ok := e.gitCommits[b.ID]
	if !ok {
		return errors.New("not a commit message buffer")
	}
	var msg []string
	for l := 0; l < b.LineCount(); l++ {
		if s := b.LineString(l); !strings.HasPrefix(s, "#") {
			msg = append(msg, s)
		}
	}
	if strings.TrimSpace(strings.Join(msg, "\n")) == "" {
		return errors.New("empty commit message (ZQ aborts)")
	}
	if err := e.writeBuffer(b, "", true); err != nil {
		return err
	}
	// hooks may be slow: commit in the background
	path := b.Path
	e.infof("committing …")
	e.safeGo(func() {
		out, err := git(root, "-c", "core.commentChar=#", "commit", "--cleanup=strip", "-F", path)
		e.Post(func() { e.gitCommitDone(b, root, out, err) })
	})
	return nil
}

func (e *Editor) gitCommitDone(b *buffer.Buffer, root, out string, err error) {
	if err != nil {
		e.errorf("git commit: %v", err)
		return
	}
	delete(e.gitCommits, b.ID)
	for _, w := range e.windows() {
		if w.buf == b {
			e.closeSpecial(w, b)
		}
	}
	if e.bufferAlive(b) {
		e.deleteBuffer(b, true)
	}
	first, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	e.infof("%s", first)
	e.refreshGitStatusOf(root)
	e.invalidateGitHeads()
}

// ---------------------------------------------------------------- :git

// gitAsync runs a (possibly slow, e.g. network) git command in the
// background, streaming its output into *git-output*.
func (e *Editor) gitAsync(root string, args ...string) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	cmd.Env = gitEnv()
	e.runAsync(cmd, "*git-output*", func(err error) {
		if err != nil {
			e.errorf("git %s failed (see *git-output*)", args[0])
		} else {
			e.infof("git %s done", args[0])
		}
		e.reloadAllUnmodified()
		e.invalidateGitHeads()
		e.refreshGitStatusOf(root)
	})
}

// exGit: no argument opens the status buffer; status/diff/log/blame/
// commit/add have built-in views; anything else runs as `git <args>`.
func exGit(e *Editor, a *exArgs) error {
	args := splitArgs(a.arg)
	if len(args) == 0 {
		return e.openGitStatus()
	}
	b := e.win.buf
	root, err := gitRoot(e.bufferDir(b))
	if err != nil {
		return err
	}
	rel := ""
	if b.Path != "" && b.Kind == buffer.KindFile {
		rel = gitRel(root, b.Path)
	}
	for i, x := range args {
		if x == "%" && rel != "" {
			args[i] = rel
		}
	}
	if e.mappingDepth > 0 && !gitMappingSafe(args) {
		return errors.New("config mappings can only run :git status, diff, log, blame, commit and add with file names")
	}
	switch args[0] {
	case "status", "st":
		return e.openGitStatus()
	case "diff":
		if len(args) == 1 {
			if rel == "" {
				e.gitShowDiff(root, "diff (all)", "diff", "HEAD")
			} else {
				e.gitShowDiff(root, "diff: "+rel, "diff", "HEAD", "--", literal(rel))
			}
			return nil
		}
		e.gitShowDiff(root, "diff "+strings.Join(args[1:], " "), args...)
		return nil
	case "log":
		switch {
		case len(args) == 1:
			e.gitLog(root, "")
		case len(args) == 2 && fileExists(filepath.Join(root, args[1])):
			e.gitLog(root, args[1])
		default:
			e.gitLog(root, "", args[1:]...)
		}
		return nil
	case "blame":
		return e.gitBlame()
	case "commit":
		if _, ok := e.gitCommits[b.ID]; ok {
			return e.gitCommit(b)
		}
		if len(args) == 1 {
			e.gitCommitBuffer(root)
			return nil
		}
	case "add", "stage":
		if len(args) == 1 {
			if rel == "" {
				return errors.New("usage: :git add [file]")
			}
			if e.modified(b) {
				if err := e.writeBuffer(b, "", false); err != nil {
					return err
				}
			}
			args = append(args, "--", literal(rel))
		}
		args[0] = "add"
		if _, err := git(root, args...); err != nil {
			return err
		}
		e.refreshGitStatusOf(root)
		staged := args[1:]
		if len(staged) > 0 && staged[0] == "--" {
			staged = staged[1:]
		}
		for i, s := range staged {
			staged[i] = strings.TrimPrefix(s, ":(literal)")
		}
		e.infof("staged %s", strings.Join(staged, " "))
		return nil
	}
	e.gitAsync(root, args...)
	return nil
}

// gitMappingSafe allows a config mapping only the built-in views, with
// plain arguments: git options (-c alias.x=!cmd, --upload-pack=…, …) can
// run arbitrary programs.
func gitMappingSafe(args []string) bool {
	switch args[0] {
	case "status", "st", "diff", "log", "blame", "commit", "add", "stage":
	default:
		return false
	}
	for _, a := range args[1:] {
		if strings.HasPrefix(a, "-") && a != "--" {
			return false
		}
	}
	return true
}

// splitArgs splits a command line on spaces, honouring '…' and "…".
func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	quote := rune(0)
	has := false
	for _, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote == 0 && (r == '\'' || r == '"'):
			quote, has = r, true
		case quote == 0 && (r == ' ' || r == '\t'):
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	if quote != 0 {
		// an unclosed quote is an apostrophe (`it's`), not a quote
		return strings.Fields(s)
	}
	return out
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
