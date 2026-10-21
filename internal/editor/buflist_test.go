package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bufListSetup opens three files and the buffer list.
func bufListSetup(t *testing.T) (*Editor, string) {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(dir, n), []byte(n+"\n"), 0o644)
	}
	e := newTestEditor(t, "")
	e.cwd = dir
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		typeKeys(e, ":e "+filepath.Join(dir, n)+"<CR>")
	}
	typeKeys(e, "<Space>bl")
	if e.buflists[e.win.buf.ID] == nil {
		t.Fatalf("no buffer list (buf %q)", e.win.buf.Name)
	}
	return e, dir
}

func gotoBuf(t *testing.T, e *Editor, name string) {
	t.Helper()
	bl := e.buflists[e.win.buf.ID]
	for i, id := range bl.ids {
		if b := e.bufferByID(id); b != nil && b.Name == name {
			e.setCursor(e.win, Pos{Line: i + 1, Col: bufListNameCol})
			return
		}
	}
	t.Fatalf("no %q in\n%s", name, e.win.buf.Text())
}

func TestBufListListingAndOpen(t *testing.T) {
	e, _ := bufListSetup(t)
	txt := e.win.buf.Text()
	if !strings.Contains(txt, "name") || !strings.Contains(txt, "a.txt") || strings.Contains(txt, "*buffers*") {
		t.Fatalf("listing:\n%s", txt)
	}
	// the buffer we came from is %, and the cursor starts on it
	if l := e.win.buf.LineString(e.win.cur.Line); !strings.Contains(l, "%") || !strings.Contains(l, "c.txt") {
		t.Fatalf("cursor line %q", l)
	}
	if e.win.cur.Col != bufListNameCol || !strings.HasPrefix(string(e.win.buf.Line(e.win.cur.Line)[bufListNameCol:]), "c.txt") {
		t.Fatalf("name column: %q", string(e.win.buf.Line(e.win.cur.Line)))
	}
	gotoBuf(t, e, "a.txt")
	typeKeys(e, "<CR>")
	if e.win.buf.Name != "a.txt" {
		t.Fatalf("open: %q", e.win.buf.Name)
	}
	// a modified buffer shows +; :ls opens the same list
	typeKeys(e, "ix<Esc>:ls<CR>")
	gotoBuf(t, e, "a.txt")
	if l := e.win.buf.LineString(e.win.cur.Line); !strings.Contains(l, "%+") {
		t.Fatalf("modified flag: %q", l)
	}
	gotoBuf(t, e, "b.txt")
	typeKeys(e, "o")
	if e.win.buf.Name != "b.txt" || len(e.windows()) != 2 {
		t.Fatal("o: split")
	}
}

func TestBufListMarksAndClose(t *testing.T) {
	e, _ := bufListSetup(t)
	bl := e.buflists[e.win.buf.ID]
	gotoBuf(t, e, "a.txt")
	typeKeys(e, "2m")
	if len(bl.marks) != 2 || !strings.Contains(e.win.buf.LineString(0), "[2 marked") {
		t.Fatalf("marks %v / %q", bl.marks, e.win.buf.LineString(0))
	}
	typeKeys(e, "tU")
	if len(bl.marks) != 0 {
		t.Fatal("U")
	}
	// flag a and c, close them with x
	gotoBuf(t, e, "a.txt")
	typeKeys(e, "d")
	gotoBuf(t, e, "c.txt")
	typeKeys(e, "dx")
	if e.findBuffer(filepath.Join(e.cwd, "a.txt")) != nil || e.findBuffer(filepath.Join(e.cwd, "c.txt")) != nil {
		t.Fatalf("x did not close (msg %q)", e.msg)
	}
	if strings.Contains(e.win.buf.Text(), "a.txt") {
		t.Fatal("list not refreshed")
	}
	// a modified buffer asks; n keeps it
	typeKeys(e, ":e "+filepath.Join(e.cwd, "b.txt")+"<CR>ix<Esc>:ls<CR>")
	gotoBuf(t, e, "b.txt")
	typeKeys(e, "Dn<CR>")
	if e.findBuffer(filepath.Join(e.cwd, "b.txt")) == nil || !strings.Contains(e.msg, "nothing closed") {
		t.Fatalf("closed without asking (msg %q)", e.msg)
	}
	typeKeys(e, "Dy<CR>")
	if e.findBuffer(filepath.Join(e.cwd, "b.txt")) != nil {
		t.Fatal("y did not close")
	}
	if data, _ := os.ReadFile(filepath.Join(e.cwd, "b.txt")); string(data) != "b.txt\n" {
		t.Fatal("closing must not touch the file")
	}
}

func TestBufListRenameCopySave(t *testing.T) {
	e, dir := bufListSetup(t)
	// rename a saved file: it moves, the buffer follows
	gotoBuf(t, e, "a.txt")
	typeKeys(e, "R<C-u>renamed.txt<CR>")
	if exists(filepath.Join(dir, "a.txt")) || !exists(filepath.Join(dir, "renamed.txt")) || e.findBuffer(filepath.Join(dir, "renamed.txt")) == nil {
		t.Fatalf("rename (msg %q)", e.msg)
	}
	if !strings.Contains(e.win.buf.Text(), "renamed.txt") {
		t.Fatal("list not refreshed after rename")
	}
	// refuse to overwrite
	gotoBuf(t, e, "b.txt")
	typeKeys(e, "R<C-u>c.txt<CR>")
	if !strings.Contains(e.msg, "exists") && !strings.Contains(e.msg, "another buffer") {
		t.Fatalf("overwrite allowed: %q", e.msg)
	}
	// copy includes unsaved changes; the original stays modified
	typeKeys(e, "<CR>ichanged <Esc>:ls<CR>")
	gotoBuf(t, e, "b.txt")
	typeKeys(e, "C<C-u>copy.txt<CR>")
	if data, _ := os.ReadFile(filepath.Join(dir, "copy.txt")); string(data) != "changed b.txt\n" {
		t.Fatalf("copy content %q (msg %q)", data, e.msg)
	}
	if b := e.findBuffer(filepath.Join(dir, "copy.txt")); b == nil {
		t.Fatal("copy not opened")
	}
	if b := e.findBuffer(filepath.Join(dir, "b.txt")); b == nil || !e.modified(b) {
		t.Fatal("original changed by copy")
	}
	// S saves it
	gotoBuf(t, e, "b.txt")
	typeKeys(e, "S")
	if data, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(data) != "changed b.txt\n" || !strings.Contains(e.msg, "saved 1") {
		t.Fatalf("save %q (msg %q)", data, e.msg)
	}
	// a nameless buffer: rename saves it under the name
	typeKeys(e, ":enew<CR>inew text<Esc>:ls<CR>")
	gotoBuf(t, e, "[No Name]")
	typeKeys(e, "R<C-u>"+filepath.Join(dir, "named.txt")+"<CR>")
	if data, _ := os.ReadFile(filepath.Join(dir, "named.txt")); string(data) != "new text\n" {
		t.Fatalf("nameless rename %q (msg %q)", data, e.msg)
	}
	// special buffers cannot be renamed
	gotoBuf(t, e, "named.txt")
	typeKeys(e, ":e "+dir+"<CR>:ls<CR>")
	line := -1
	for l := 1; l < e.win.buf.LineCount(); l++ {
		if strings.Contains(e.win.buf.LineString(l), "directory") {
			line = l
		}
	}
	if line < 0 {
		t.Fatalf("explorer buffer kind:\n%s", e.win.buf.Text())
	}
	e.setCursor(e.win, Pos{Line: line, Col: bufListNameCol})
	typeKeys(e, "R")
	if !strings.Contains(e.msg, "not a file buffer") {
		t.Fatalf("renaming a directory listing: %q", e.msg)
	}
}

func TestBufListFileClipboard(t *testing.T) {
	e, dir := bufListSetup(t)
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	gotoBuf(t, e, "a.txt")
	typeKeys(e, "y")
	if len(e.fileClip.paths) != 1 || e.fileClip.cut {
		t.Fatalf("clipboard %+v", e.fileClip)
	}
	// paste into a directory listing
	typeKeys(e, ":e "+sub+"<CR>p")
	if !exists(filepath.Join(sub, "a.txt")) {
		t.Fatalf("paste in explorer (msg %q)", e.msg)
	}
	// cut, paste moves and the buffer follows
	typeKeys(e, ":ls<CR>")
	gotoBuf(t, e, "b.txt")
	typeKeys(e, "X:e "+sub+"<CR>p")
	if exists(filepath.Join(dir, "b.txt")) || e.findBuffer(filepath.Join(sub, "b.txt")) == nil {
		t.Fatalf("cut-paste (msg %q)", e.msg)
	}
	// p in the buffer list opens the clipboard's files
	os.WriteFile(filepath.Join(dir, "z.txt"), []byte("z\n"), 0o644)
	e.fileClip = fileClipboard{paths: []string{filepath.Join(dir, "z.txt")}}
	typeKeys(e, ":ls<CR>p")
	if e.findBuffer(filepath.Join(dir, "z.txt")) == nil || !strings.Contains(e.win.buf.Text(), "z.txt") {
		t.Fatalf("p opens (msg %q)", e.msg)
	}
	// buffers without a file cannot go in the clipboard
	typeKeys(e, ":enew<CR>:ls<CR>")
	gotoBuf(t, e, "[No Name]")
	typeKeys(e, "y")
	if !strings.Contains(e.msg, "no file buffers") {
		t.Fatalf("msg %q", e.msg)
	}
}
