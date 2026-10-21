package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveThroughSymlinkAndSameFile(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	os.WriteFile(real, []byte("old\n"), 0o640)
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+link+"<CR>ccnew<Esc>:w<CR>")
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink replaced by the save")
	}
	if data, _ := os.ReadFile(real); string(data) != "new\n" {
		t.Fatalf("target %q", data)
	}
	if st, _ := os.Stat(real); st.Mode().Perm() != 0o640 {
		t.Fatalf("mode %v", st.Mode().Perm())
	}
	// the same file through another name is the same buffer
	if e.findBuffer(real) != e.win.buf {
		t.Fatal("findBuffer does not see through the symlink")
	}
	// :w into a missing directory fails instead of creating it
	typeKeys(e, ":w "+filepath.Join(dir, "nope", "x.txt")+"<CR>")
	if !strings.Contains(e.msg, "does not exist") {
		t.Fatalf("msg %q", e.msg)
	}
}

func TestTransferGuards(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "d", "sub"), 0o755)
	e := newTestEditor(t, "")
	// an unsaved buffer on new.txt: moving a.txt there is refused
	typeKeys(e, ":e "+filepath.Join(dir, "new.txt")+"<CR>iunsaved<Esc>")
	typeKeys(e, ":e "+dir+"<CR>")
	gotoName(t, e, "a.txt")
	typeKeys(e, "R<C-u>new.txt<CR>")
	if !strings.Contains(e.msg, "is open in buffer") || !exists(filepath.Join(dir, "a.txt")) {
		t.Fatalf("move onto an open buffer: %q", e.msg)
	}
	// a directory cannot be copied into itself through a symlink
	if err := os.Symlink(filepath.Join(dir, "d", "sub"), filepath.Join(dir, "l")); err != nil {
		t.Skip(err)
	}
	if err := transferPath(filepath.Join(dir, "d"), filepath.Join(dir, "l", "d"), true); err == nil || !strings.Contains(err.Error(), "inside itself") {
		t.Fatalf("copy into itself via symlink: %v", err)
	}
}

func TestLogWriterKeepsUTF8(t *testing.T) {
	e := newTestEditor(t, "")
	b := e.scratch("*log*", 4)
	w := &logWriter{e: e, b: b}
	euro := []byte("x€y")
	w.Write(euro[:2]) // x + first byte of €
	w.Write(euro[2:])
	e.drainAsync()
	if got := b.Text(); got != "x€y" {
		t.Fatalf("log %q", got)
	}
	for i := 0; i < 50; i++ {
		w.Write([]byte("line\n"))
	}
	e.drainAsync()
	if _, ok := b.Undo(); ok {
		t.Fatal("log buffer keeps an undo history")
	}
}

func TestBufListCopyNamelessKeepsBuffer(t *testing.T) {
	dir := t.TempDir()
	e := newTestEditor(t, "")
	e.cwd = dir
	typeKeys(e, ":enew<CR>inotes<Esc>:ls<CR>")
	gotoBuf(t, e, "[No Name]")
	typeKeys(e, "C<C-u>"+filepath.Join(dir, "copy.txt")+"<CR>")
	if data, _ := os.ReadFile(filepath.Join(dir, "copy.txt")); string(data) != "notes\n" {
		t.Fatalf("copy %q (msg %q)", data, e.msg)
	}
	for _, b := range e.buffers {
		if b.Name == "[No Name]" && b.Path == "" && b.Text() == "notes" {
			return
		}
	}
	t.Fatal("the nameless buffer was renamed by the copy")
}

func TestRunStop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.sh")
	os.WriteFile(path, []byte("sleep 30\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>:run<CR>")
	time.Sleep(100 * time.Millisecond)
	typeKeys(e, ":run stop<CR>")
	if !strings.Contains(e.msg, "stopped 1") {
		t.Fatalf("msg %q", e.msg)
	}
	out := e.scratch("*run*", 4)
	waitFor(t, e, "job end", func() bool {
		return strings.Contains(out.Text(), "signal") || strings.Contains(out.Text(), "[") && !strings.HasSuffix(out.Text(), "sleep 30\n")
	})
	typeKeys(e, ":run stop<CR>")
	if !strings.Contains(e.msg, "nothing is running") {
		t.Fatalf("msg %q", e.msg)
	}
}

func TestGitRelSafety(t *testing.T) {
	if got := gitRel("/repo", "/repo/-x.txt"); got != "./-x.txt" {
		t.Fatalf("option-like name: %q", got)
	}
	dir := gitRepo(t) // resolved path
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(dir, alias); err != nil {
		t.Skip(err)
	}
	if got := gitRel(dir, filepath.Join(alias, "f.txt")); got != "f.txt" {
		t.Fatalf("symlinked path: %q", got)
	}
}
