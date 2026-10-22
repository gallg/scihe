package buffer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInsertDeleteUndoRedo(t *testing.T) {
	b := NewFromString("t", "hello\nworld")
	b.BeginUndo(Pos{})
	end := b.Insert(Pos{0, 5}, " there\nnew")
	b.EndUndo(end)
	if got := b.Text(); got != "hello there\nnew\nworld" {
		t.Fatalf("insert: %q", got)
	}
	if end != (Pos{1, 3}) {
		t.Fatalf("end pos %v", end)
	}
	b.Delete(Pos{0, 5}, Pos{2, 0})
	if got := b.Text(); got != "helloworld" {
		t.Fatalf("delete: %q", got)
	}
	b.Undo()
	if got := b.Text(); got != "hello there\nnew\nworld" {
		t.Fatalf("undo delete: %q", got)
	}
	b.Undo()
	if got := b.Text(); got != "hello\nworld" {
		t.Fatalf("undo insert: %q", got)
	}
	if b.Modified() {
		t.Fatal("should be unmodified after undoing everything")
	}
	b.Redo()
	b.Redo()
	if got := b.Text(); got != "helloworld" {
		t.Fatalf("redo: %q", got)
	}
}

func TestSliceOffsets(t *testing.T) {
	b := NewFromString("t", "ab\ncd\nef")
	if s := b.Slice(Pos{0, 1}, Pos{2, 1}); s != "b\ncd\ne" {
		t.Fatalf("slice %q", s)
	}
	for off := 0; off <= 8; off++ {
		if got := b.Offset(b.PosAt(off)); got != off {
			t.Fatalf("offset roundtrip %d -> %d", off, got)
		}
	}
}

func TestMarksShift(t *testing.T) {
	b := NewFromString("t", "a\nb\nc\nd")
	b.Marks['a'] = Pos{3, 0}
	b.Insert(Pos{0, 0}, "x\ny\n")
	if b.Marks['a'].Line != 5 {
		t.Fatalf("mark after insert: %v", b.Marks['a'])
	}
	b.Delete(Pos{0, 0}, Pos{4, 0})
	if b.Marks['a'].Line != 1 {
		t.Fatalf("mark after delete: %v", b.Marks['a'])
	}
	// deleting the marked line removes the mark
	b.Marks['b'] = Pos{1, 0}
	b.Delete(Pos{0, 1}, Pos{1, 1})
	if _, ok := b.Marks['b']; ok {
		t.Fatalf("mark on deleted text survived: %v", b.Marks['b'])
	}
	// a mark after the deletion on the same line keeps its text position
	b2 := NewFromString("t", "abcdef")
	b2.Marks['c'] = Pos{0, 4}
	b2.Delete(Pos{0, 1}, Pos{0, 3})
	if b2.Marks['c'] != (Pos{0, 2}) {
		t.Fatalf("same-line mark: %v", b2.Marks['c'])
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"lf.txt":    "one\ntwo\n",
		"crlf.txt":  "one\r\ntwo\r\n",
		"noeol.txt": "one\ntwo",
		"empty.txt": "",
		"blank.txt": "\n",
		"mixed.txt": "one\r\ntwo\nthree\r\n",
	}
	for name, content := range cases {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(content), 0o600)
		b, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Save(""); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != content {
			t.Errorf("%s: got %q want %q", name, got, content)
		}
		if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
			t.Errorf("%s: permissions not preserved: %v", name, st.Mode())
		}
	}
	os.WriteFile(filepath.Join(dir, "bin"), []byte{0xff, 0xfe, 0x00}, 0o644)
	if _, err := Load(filepath.Join(dir, "bin")); err == nil {
		t.Fatal("expected error for binary file")
	}
}

// a new (or emptied) buffer writes an empty file, like vim
func TestSaveEmptyBuffer(t *testing.T) {
	p := filepath.Join(t.TempDir(), "new.txt")
	b, err := Load(p) // does not exist yet
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Save(""); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); string(got) != "" {
		t.Fatalf("empty buffer wrote %q", got)
	}
	b.Insert(Pos{}, "x")
	b.Save("")
	if got, _ := os.ReadFile(p); string(got) != "x\n" {
		t.Fatalf("typed text wrote %q", got)
	}
}
