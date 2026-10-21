package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func diredSetup(t *testing.T) (*Editor, string) {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "d1", "inner"), 0o755)
	os.WriteFile(filepath.Join(dir, "d1", "inner", "deep.txt"), []byte("deep\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o640)
	os.WriteFile(filepath.Join(dir, ".hidden"), []byte("h\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+dir+"<CR>")
	return e, dir
}

// gotoName moves the cursor to the listing line of name.
func gotoName(t *testing.T, e *Editor, name string) {
	t.Helper()
	d := e.direds[e.win.buf.ID]
	for i, en := range d.entries {
		if en.name == name {
			e.setCursor(e.win, Pos{Line: i + 1, Col: diredNameCol})
			return
		}
	}
	t.Fatalf("no entry %q in\n%s", name, e.win.buf.Text())
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestDiredListing(t *testing.T) {
	e, dir := diredSetup(t)
	txt := e.win.buf.Text()
	// directories first, details like ls -l
	order := []string{"../", "d1/", ".hidden", "a.txt", "b.txt"}
	last := -1
	for _, n := range order {
		i := strings.Index(txt, "  "+n)
		if i < 0 || i < last {
			t.Fatalf("order of %q:\n%s", n, txt)
		}
		last = i
	}
	if !strings.Contains(txt, "-rw-r-----") || !strings.Contains(txt, "drwxr-xr-x") {
		t.Fatalf("modes:\n%s", txt)
	}
	if !strings.HasPrefix(e.win.buf.LineString(0), "  "+dir+"/") {
		t.Fatalf("header %q", e.win.buf.LineString(0))
	}
	typeKeys(e, ".")
	if strings.Contains(e.win.buf.Text(), ".hidden") {
		t.Fatal("dotfile not hidden")
	}
	typeKeys(e, ".")
	if !strings.Contains(e.win.buf.Text(), ".hidden") {
		t.Fatal("dotfile not shown again")
	}
	// the buffer cannot be edited as text
	typeKeys(e, "x")
	if e.win.buf.Modified() {
		t.Fatal("listing modified")
	}
}

func TestDiredMarksAndDelete(t *testing.T) {
	e, dir := diredSetup(t)
	d := e.direds[e.win.buf.ID]
	gotoName(t, e, "a.txt")
	typeKeys(e, "2m") // marks a.txt and b.txt, cursor moves down
	if d.marks["a.txt"] != '*' || d.marks["b.txt"] != '*' {
		t.Fatalf("marks %v", d.marks)
	}
	if !strings.HasPrefix(e.win.buf.LineString(e.win.cur.Line-1), "* ") {
		t.Fatalf("mark not shown: %q", e.win.buf.LineString(e.win.cur.Line-1))
	}
	if !strings.Contains(e.win.buf.LineString(0), "[2 marked, 0 flagged]") {
		t.Fatalf("header %q", e.win.buf.LineString(0))
	}
	typeKeys(e, "t")
	if d.marks["a.txt"] != 0 || d.marks["d1"] != '*' || d.marks[".."] != 0 {
		t.Fatalf("toggle %v", d.marks)
	}
	typeKeys(e, "U")
	if len(d.marks) != 0 {
		t.Fatalf("U %v", d.marks)
	}
	// flag, then x asks; "n" keeps everything
	gotoName(t, e, "d1")
	typeKeys(e, "d")
	gotoName(t, e, "b.txt")
	typeKeys(e, "d")
	typeKeys(e, "xn<CR>")
	if !exists(filepath.Join(dir, "d1")) || !strings.Contains(e.msg, "nothing deleted") {
		t.Fatalf("deleted without y (msg %q)", e.msg)
	}
	// an open file inside the deleted directory loses its buffer
	typeKeys(e, ":e "+filepath.Join(dir, "d1", "inner", "deep.txt")+"<CR><C-^>")
	typeKeys(e, "xy<CR>")
	if exists(filepath.Join(dir, "d1")) || exists(filepath.Join(dir, "b.txt")) || !exists(filepath.Join(dir, "a.txt")) {
		t.Fatalf("x deleted the wrong files (msg %q)", e.msg)
	}
	if e.findBuffer(filepath.Join(dir, "d1", "inner", "deep.txt")) != nil {
		t.Fatal("buffer of a deleted file kept")
	}
	// D deletes the file under the cursor; modified buffers are kept
	typeKeys(e, ":e "+filepath.Join(dir, "a.txt")+"<CR>ix<Esc>:e "+dir+"<CR>")
	gotoName(t, e, "a.txt")
	typeKeys(e, "Dy<CR>")
	if exists(filepath.Join(dir, "a.txt")) {
		t.Fatal("D did not delete")
	}
	if b := e.findBuffer(filepath.Join(dir, "a.txt")); b == nil || !strings.Contains(e.msg+strings.Join(e.messages, " "), "unsaved changes") {
		t.Fatalf("modified buffer dropped or no warning (msg %q)", e.msg)
	}
	// ".." can never be deleted
	gotoName(t, e, "..")
	typeKeys(e, "D")
	if !strings.Contains(e.msg, "nothing to delete") {
		t.Fatalf("msg %q", e.msg)
	}
}

func TestDiredRenameCopy(t *testing.T) {
	e, dir := diredSetup(t)
	typeKeys(e, ":e "+filepath.Join(dir, "a.txt")+"<CR>:e "+dir+"<CR>")
	// rename in place: the open buffer follows
	gotoName(t, e, "a.txt")
	typeKeys(e, "R<C-u>"+filepath.Join(dir, "renamed.txt")+"<CR>")
	if exists(filepath.Join(dir, "a.txt")) || !exists(filepath.Join(dir, "renamed.txt")) {
		t.Fatalf("rename failed (msg %q)", e.msg)
	}
	if b := e.findBuffer(filepath.Join(dir, "renamed.txt")); b == nil || b.Text() != "a" {
		t.Fatal("buffer did not follow the rename")
	}
	if l := e.win.buf.LineString(e.win.cur.Line); !strings.HasSuffix(l, "renamed.txt") {
		t.Fatalf("cursor on %q", l)
	}
	// relative destination, existing target refused
	gotoName(t, e, "b.txt")
	typeKeys(e, "R<C-u>renamed.txt<CR>")
	if !strings.Contains(e.msg, "already exists") || !exists(filepath.Join(dir, "b.txt")) {
		t.Fatalf("overwrite allowed (msg %q)", e.msg)
	}
	// move marked files into a directory
	gotoName(t, e, "b.txt")
	typeKeys(e, "m")
	gotoName(t, e, "renamed.txt")
	typeKeys(e, "mR<C-u>d1<CR>")
	if !exists(filepath.Join(dir, "d1", "b.txt")) || !exists(filepath.Join(dir, "d1", "renamed.txt")) || exists(filepath.Join(dir, "b.txt")) {
		t.Fatalf("move into dir failed (msg %q)", e.msg)
	}
	if b := e.findBuffer(filepath.Join(dir, "d1", "renamed.txt")); b == nil {
		t.Fatal("buffer did not follow the move")
	}
	// copy a directory tree
	gotoName(t, e, "d1")
	typeKeys(e, "C<C-u>copy<CR>")
	if data, err := os.ReadFile(filepath.Join(dir, "copy", "inner", "deep.txt")); err != nil || string(data) != "deep\n" {
		t.Fatalf("copy tree: %v %q (msg %q)", err, data, e.msg)
	}
	if !exists(filepath.Join(dir, "d1", "inner", "deep.txt")) {
		t.Fatal("copy removed the source")
	}
	// a directory cannot go inside itself
	gotoName(t, e, "d1")
	typeKeys(e, "C<C-u>d1/inner/x<CR>")
	if !strings.Contains(e.msg, "inside itself") {
		t.Fatalf("msg %q", e.msg)
	}
	// new directory and new file
	typeKeys(e, "+made/deeper<CR>")
	if !exists(filepath.Join(dir, "made", "deeper")) {
		t.Fatal("mkdir")
	}
	typeKeys(e, "%new.txt<CR>")
	if e.win.buf.Path != filepath.Join(dir, "new.txt") {
		t.Fatalf("new file at %q", e.win.buf.Path)
	}
}

func TestDiredClipboard(t *testing.T) {
	e, dir := diredSetup(t)
	other := filepath.Join(dir, "d1")
	typeKeys(e, ":e "+filepath.Join(dir, "a.txt")+"<CR>:e "+dir+"<CR>")
	// copy two marked files, paste them in another directory
	gotoName(t, e, "a.txt")
	typeKeys(e, "2my")
	if len(e.fileClip.paths) != 2 || e.fileClip.cut || len(e.direds[e.win.buf.ID].marks) != 0 {
		t.Fatalf("clipboard %+v", e.fileClip)
	}
	if !strings.Contains(e.win.buf.LineString(0), "[clipboard: 2 to copy") {
		t.Fatalf("header %q", e.win.buf.LineString(0))
	}
	typeKeys(e, ":e "+other+"<CR>p")
	for _, n := range []string{"a.txt", "b.txt"} {
		if !exists(filepath.Join(other, n)) || !exists(filepath.Join(dir, n)) {
			t.Fatalf("copy-paste of %s (msg %q)", n, e.msg)
		}
	}
	// a copy clipboard stays: pasting again clashes and is refused
	typeKeys(e, "p")
	if !strings.Contains(e.msg, "already exists") {
		t.Fatalf("msg %q", e.msg)
	}
	// cut a file and paste it: it moves, its buffer follows, clipboard empties
	typeKeys(e, ":e "+dir+"<CR>")
	gotoName(t, e, "a.txt")
	typeKeys(e, "X")
	os.Remove(filepath.Join(other, "a.txt"))
	typeKeys(e, ":e "+other+"<CR>p")
	if exists(filepath.Join(dir, "a.txt")) || !exists(filepath.Join(other, "a.txt")) {
		t.Fatalf("cut-paste (msg %q)", e.msg)
	}
	if e.findBuffer(filepath.Join(other, "a.txt")) == nil {
		t.Fatal("buffer did not follow the move")
	}
	if len(e.fileClip.paths) != 0 {
		t.Fatal("cut clipboard not emptied")
	}
	if l := e.win.buf.LineString(e.win.cur.Line); !strings.HasSuffix(l, "a.txt") {
		t.Fatalf("cursor on %q", l)
	}
	typeKeys(e, "p")
	if !strings.Contains(e.msg, "clipboard is empty") {
		t.Fatalf("msg %q", e.msg)
	}
	// w yanks the path as text
	gotoName(t, e, "b.txt")
	typeKeys(e, "w")
	if r, _ := e.regs.Get('"'); r.Text != filepath.Join(other, "b.txt") {
		t.Fatalf("register %q", r.Text)
	}
}
