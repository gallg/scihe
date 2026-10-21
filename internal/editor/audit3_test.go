package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestTransformColumnOrder(t *testing.T) {
	e, tv, _ := openEditTable(t, "a,b,c\n10,1,100\n", "o.csv")
	typeKeys(e, ":transform log10 c a<CR>")
	if h := strings.Join(tv.d.header, ","); h != "a,a_log10,b,c,c_log10" {
		t.Fatalf("header %s", h)
	}
	if got := viewColumn(tv, tv.columnByName("c_log10")); got != "2" {
		t.Fatalf("c_log10 %s", got)
	}
	// a column name containing _ is not mistaken for the placeholder
	e2, _, _ := openEditTable(t, "price_eur\n100\n", "p.csv")
	typeKeys(e2, ":transform log(price_eur)<CR>")
	if !strings.Contains(e2.msg, "use _") {
		t.Fatalf("msg %q", e2.msg)
	}
}

func TestAutoreloadSafety(t *testing.T) {
	md := filepath.Join(t.TempDir(), "s.md")
	os.WriteFile(md, []byte("one\ntwo\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+md+"<CR>:set ar<CR>")
	b := e.win.buf
	// in insert mode the reload waits
	typeKeys(e, "GA")
	touchWrite(t, md, "x\n")
	checkNow(e)
	if b.LineString(0) != "one" {
		t.Fatal("reloaded during insert mode")
	}
	typeKeys(e, "<Esc>")
	checkNow(e)
	if b.Text() != "x" {
		t.Fatalf("not reloaded after insert: %q", b.Text())
	}
	// the reload can be undone
	typeKeys(e, "u")
	if b.Text() != "one\ntwo" {
		t.Fatalf("undo of reload: %q", b.Text())
	}
	// :set autoreload in a table view applies to its file
	csv := filepath.Join(t.TempDir(), "t.csv")
	os.WriteFile(csv, []byte("a\n1\n"), 0o644)
	typeKeys(e, ":e "+csv+"<CR>")
	src := e.win.buf
	typeKeys(e, ":table<CR>:set ar<CR>")
	if !e.options(src).Autoreload {
		t.Fatal("table view: option not on the source")
	}
}

func TestWriteAtomicEdges(t *testing.T) {
	dir := t.TempDir()
	// a dangling symlink is written through
	link := filepath.Join(dir, "link")
	os.Symlink("target.txt", link)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+link+"<CR>inew<Esc>:w<CR>")
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("dangling symlink replaced")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "target.txt")); string(data) != "new\n" {
		t.Fatalf("target %q (msg %q)", data, e.msg)
	}
	// new files follow the umask
	old := syscall.Umask(0o077)
	syscall.Umask(old)
	if st, _ := os.Stat(filepath.Join(dir, "target.txt")); st.Mode().Perm() != 0o666&^umaskFor(t) {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	// a writable file in a read-only directory is still saved
	ro := filepath.Join(dir, "ro")
	os.Mkdir(ro, 0o755)
	f := filepath.Join(ro, "f.txt")
	os.WriteFile(f, []byte("a\n"), 0o666)
	os.Chmod(ro, 0o555)
	defer os.Chmod(ro, 0o755)
	typeKeys(e, ":e "+f+"<CR>ccb<Esc>:w<CR>")
	if data, _ := os.ReadFile(f); string(data) != "b\n" {
		t.Fatalf("read-only dir: %q (msg %q)", data, e.msg)
	}
}

func umaskFor(t *testing.T) os.FileMode {
	m := syscall.Umask(0)
	syscall.Umask(m)
	return os.FileMode(m)
}

func TestLogBuffersAndRefusals(t *testing.T) {
	e := newTestEditor(t, "a\nb")
	out := e.scratch("*log*", 4)
	e.appendText(out, "line\n")
	if out.Modified() {
		t.Fatal("log buffer looks modified")
	}
	// E21 is reported for keys replayed by a macro
	e.win.buf.ReadOnly = false
	typeKeys(e, "qq:t0<CR>q")
	e.win.buf.ReadOnly = true
	e.msg = ""
	typeKeys(e, "@q")
	if !strings.Contains(e.msg, "read-only") {
		t.Fatalf("macro: msg %q", e.msg)
	}
	// held-back bytes are flushed when a job ends
	path := filepath.Join(t.TempDir(), "x.sh")
	os.WriteFile(path, []byte("printf 'ok\\303'\n"), 0o644)
	typeKeys(e, ":e "+path+"<CR>:run<CR>")
	run := e.scratch("*run*", 4)
	waitFor(t, e, "run end", func() bool { return strings.Contains(run.Text(), "[done]") })
	if !strings.Contains(run.Text(), "ok\ufffd") && !strings.Contains(run.Text(), "ok\303") {
		t.Fatalf("tail lost: %q", run.Text())
	}
}

func TestMoveSymlinkIntoItsTarget(t *testing.T) {
	dir := t.TempDir()
	d := filepath.Join(dir, "d")
	os.Mkdir(d, 0o755)
	lnk := filepath.Join(dir, "lnk")
	os.Symlink(d, lnk)
	if err := transferPath(lnk, filepath.Join(d, "lnk"), false); err != nil {
		t.Fatalf("symlink into its target: %v", err)
	}
	if st, err := os.Lstat(filepath.Join(d, "lnk")); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("not moved as a link")
	}
}

func TestGitAddRefreshesStatus(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := gitRepo(t)
	f := filepath.Join(dir, "a.txt")
	os.WriteFile(f, []byte("x\n"), 0o644)
	e := newTestEditor(t, "")
	e.cwd = dir
	typeKeys(e, ":git<CR>")
	sb := e.win.buf
	typeKeys(e, ":e "+f+"<CR>:git add<CR>")
	if !strings.Contains(sb.Text(), "Staged changes (1)") || strings.Contains(e.msg, "--") {
		t.Fatalf("status not refreshed / msg %q:\n%s", e.msg, sb.Text())
	}
}
