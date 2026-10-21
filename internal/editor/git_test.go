package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
	} {
		if out, err := git(dir, args...); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	// resolve symlinks (macOS /var -> /private/var) like rev-parse does
	dir, _ = filepath.EvalSymlinks(dir)
	return dir
}

func TestParseGitStatus(t *testing.T) {
	out := "## main...origin/main [ahead 1]\x00M  staged.go\x00 M work.go\x00MM both.go\x00R  new.go\x00old.go\x00 R moved.go\x00orig.go\x00?? new file.txt\x00UU c.go\x00"
	br, ents := parseGitStatus(out)
	if br != "main...origin/main [ahead 1]" {
		t.Fatalf("branch %q", br)
	}
	got := []string{}
	for _, en := range ents {
		got = append(got, en.path+":"+gitSectionTitle[en.section][:3])
	}
	want := "staged.go:Sta work.go:Uns both.go:Sta both.go:Uns new.go:Sta moved.go:Uns new file.txt:Unt c.go:Mer"
	if strings.Join(got, " ") != want {
		t.Fatalf("entries %q", strings.Join(got, " "))
	}
}

func TestCommitHashIn(t *testing.T) {
	for line, want := range map[string]string{
		"* 1a2b3c4 2026-10-20 Me (HEAD -> main)  msg": "1a2b3c4",
		"| * deadbee 2026-10-20 Me  x":                "deadbee",
		"|/":                                          "",
		"* not-a-hash here":                           "",
	} {
		if got := commitHashIn(line); got != want {
			t.Errorf("%q: got %q want %q", line, got, want)
		}
	}
}

func TestGitStatusStageCommit(t *testing.T) {
	dir := gitRepo(t)
	f := filepath.Join(dir, "a.txt")
	os.WriteFile(f, []byte("one\n"), 0o644)
	e := newTestEditor(t, "")
	e.cwd = dir
	typeKeys(e, ":e "+f+"<CR>:git<CR>")
	sb := e.win.buf
	if !strings.Contains(sb.Text(), "Untracked files (1)") || !strings.Contains(sb.Text(), "a.txt") {
		t.Fatalf("status:\n%s", sb.Text())
	}
	if e.gitViews[sb.ID].entries[e.win.cur.Line].path != "a.txt" {
		t.Fatalf("cursor not on the file: line %d", e.win.cur.Line)
	}
	typeKeys(e, "s")
	if !strings.Contains(sb.Text(), "Staged changes (1)") {
		t.Fatalf("after stage:\n%s", sb.Text())
	}
	// unstage on an unborn branch, then stage again
	typeKeys(e, "gg")
	for l := 0; l < sb.LineCount(); l++ {
		if _, ok := e.gitViews[sb.ID].entries[l]; ok {
			e.win.cur = Pos{l, 0}
		}
	}
	typeKeys(e, "u")
	if !strings.Contains(sb.Text(), "Untracked files (1)") {
		t.Fatalf("after unstage:\n%s", sb.Text())
	}
	typeKeys(e, "a")
	typeKeys(e, "c")
	cb := e.win.buf
	if !strings.HasSuffix(cb.Path, "COMMIT_EDITMSG") || !strings.Contains(cb.Text(), "a.txt") {
		t.Fatalf("commit buffer %q:\n%s", cb.Path, cb.Text())
	}
	typeKeys(e, "ZZ")
	if !strings.Contains(e.msg, "empty commit message") {
		t.Fatalf("empty message accepted: %q", e.msg)
	}
	typeKeys(e, "ggiFirst commit<Esc>ZZ")
	waitFor(t, e, "commit", func() bool { return !e.bufferAlive(cb) })
	log, err := git(dir, "log", "--format=%s")
	if err != nil || strings.TrimSpace(log) != "First commit" {
		t.Fatalf("log %q %v (msg %q)", log, err, e.msg)
	}
	if !strings.Contains(sb.Text(), "working tree clean") {
		t.Fatalf("status not refreshed:\n%s", sb.Text())
	}
	// modify, diff, discard
	os.WriteFile(f, []byte("two\n"), 0o644)
	typeKeys(e, "R")
	for l := 0; l < sb.LineCount(); l++ {
		if en, ok := e.gitViews[sb.ID].entries[l]; ok && en.section == gitUnstaged {
			e.win.cur = Pos{l, 0}
		}
	}
	typeKeys(e, "d")
	if db := e.win.buf; db.Filetype != "diff" || !strings.Contains(db.Text(), "+two") {
		t.Fatalf("diff:\n%s", db.Text())
	}
	typeKeys(e, ":git<CR>x")
	typeKeys(e, "y<CR>")
	if data, _ := os.ReadFile(f); string(data) != "one\n" {
		t.Fatalf("discard: %q", data)
	}
	// the open buffer follows the file restored on disk
	if fb := e.findBuffer(f); fb == nil || fb.Text() != "one" {
		t.Fatalf("buffer not reloaded: %q", fb.Text())
	}
	// log view and showing a commit
	typeKeys(e, ":git log<CR>")
	if !strings.Contains(e.win.buf.Text(), "First commit") {
		t.Fatalf("log:\n%s", e.win.buf.Text())
	}
	typeKeys(e, "<CR>")
	if !strings.Contains(e.win.buf.Text(), "+one") {
		t.Fatalf("show:\n%s", e.win.buf.Text())
	}
	// status line shows the branch once the background lookup is done
	fw := e.win
	for _, w := range e.windows() {
		if w.buf.Path == f {
			fw = w
		}
	}
	e.focus(fw)
	waitFor(t, e, "branch", func() bool { return e.gitBranch(dir) == "main" })
}

func TestGitAsyncCommand(t *testing.T) {
	dir := gitRepo(t)
	e := newTestEditor(t, "")
	e.cwd = dir
	typeKeys(e, ":git checkout -q -b feature<CR>")
	out := e.scratch("*git-output*", 4)
	waitFor(t, e, "git output", func() bool { return strings.Contains(out.Text(), "[done]") })
	if br, _ := git(dir, "symbolic-ref", "--short", "HEAD"); strings.TrimSpace(br) != "feature" {
		t.Fatalf("branch %q", br)
	}
}

func TestSplitArgs(t *testing.T) {
	got := splitArgs(`commit -m "fix the thing" --author='A B <a@b>'`)
	want := []string{"commit", "-m", "fix the thing", "--author=A B <a@b>"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%q", got)
	}
}

func TestGitMappingGuard(t *testing.T) {
	dir := gitRepo(t)
	marker := filepath.Join(t.TempDir(), "x")
	e := newTestEditor(t, "")
	e.cwd = dir
	e.cfg.Keys = map[string]map[string]string{"normal": {
		"Q": `:git -c "alias.p=!touch ` + marker + `" p`,
		"X": ":git push --receive-pack=touch",
		"Y": ":git log",
	}}
	e.applyUserKeys()
	typeKeys(e, "Q")
	if !strings.Contains(e.msg, "config mappings") {
		t.Fatalf("msg %q", e.msg)
	}
	typeKeys(e, "X")
	if !strings.Contains(e.msg, "config mappings") {
		t.Fatalf("msg %q", e.msg)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("mapping ran a program through git")
	}
	typeKeys(e, "Y")
	if strings.Contains(e.msg, "config mappings") {
		t.Fatalf("built-in view refused: %q", e.msg)
	}
}

func TestGitStatusPerRepository(t *testing.T) {
	a, b := gitRepo(t), gitRepo(t)
	os.WriteFile(filepath.Join(a, "in-a.txt"), []byte("x\n"), 0o644)
	os.WriteFile(filepath.Join(b, "in-b.txt"), []byte("x\n"), 0o644)
	e := newTestEditor(t, "")
	e.cwd = a
	typeKeys(e, ":git<CR>")
	sa := e.win.buf
	e.cwd = b
	typeKeys(e, ":enew<CR>:git<CR>")
	if e.win.buf == sa || !strings.Contains(e.win.buf.Text(), "in-b.txt") || !strings.Contains(sa.Text(), "in-a.txt") {
		t.Fatalf("status buffers mixed up:\n%s\n---\n%s", sa.Text(), e.win.buf.Text())
	}
}
