package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"scihe/internal/buffer"
	"scihe/internal/config"
	"scihe/internal/keymap"
)

func newTestEditor(t *testing.T, text string) *Editor {
	t.Helper()
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.SetSize(100, 30)
	t.Cleanup(s.Fini)
	cfg := config.Default()
	cfg.Keys = map[string]map[string]string{}
	e := New(s, cfg)
	b := buffer.NewFromString("test.txt", text)
	e.addBuffer(b)
	e.root = &node{win: e.newWindow(b)}
	e.win = e.root.win
	e.draw()
	t.Cleanup(e.shutdown)
	return e
}

// typeKeys feeds vim key notation, drawing after each key like the UI.
func typeKeys(e *Editor, keys string) {
	for _, k := range keymap.Parse(keys, e.leader) {
		e.handleKey(k)
		e.drainAsync()
		e.draw()
	}
}

func curText(e *Editor) string { return e.win.buf.Text() }

type vimCase struct {
	name   string
	text   string
	keys   string
	want   string
	cursor *Pos
}

func p(l, c int) *Pos { return &Pos{l, c} }

func TestVimEditing(t *testing.T) {
	cases := []vimCase{
		{"x deletes char", "abc", "x", "bc", p(0, 0)},
		{"3x", "abcdef", "3x", "def", p(0, 0)},
		{"x at end moves left", "abc", "$x", "ab", p(0, 1)},
		{"X", "abc", "$X", "ac", p(0, 1)},
		{"dw", "one two three", "dw", "two three", p(0, 0)},
		{"dw last word keeps newline", "one two\nnext", "wdw", "one \nnext", p(0, 3)},
		{"2dw", "a b c d", "2dw", "c d", nil},
		{"d2w", "a b c d", "d2w", "c d", nil},
		{"cw changes to word end", "foo bar", "cwbaz<Esc>", "baz bar", p(0, 2)},
		{"cw on space", "foo  bar", "lllcwX<Esc>", "fooXbar", nil},
		{"ce", "foo bar", "ceX<Esc>", "X bar", nil},
		{"de", "foo bar", "de", " bar", nil},
		{"db", "foo bar", "$db", "foo r", nil},
		{"dd", "a\nb\nc", "jdd", "a\nc", p(1, 0)},
		{"dd last line", "a\nb\nc", "Gdd", "a\nb", p(1, 0)},
		{"3dd", "a\nb\nc\nd", "3dd", "d", p(0, 0)},
		{"dj", "a\nb\nc", "dj", "c", nil},
		{"dk", "a\nb\nc", "jjdk", "a", nil},
		{"dG", "a\nb\nc", "jdG", "a", nil},
		{"dgg", "a\nb\nc", "jdgg", "c", nil},
		{"d$", "hello world", "wd$", "hello ", p(0, 5)},
		{"D", "hello world", "wD", "hello ", nil},
		{"C", "hello world", "wCthere<Esc>", "hello there", nil},
		{"d0", "hello world", "wd0", "world", nil},
		{"d^", "  hello world", "wwd^", "  world", nil},
		{"dfx", "abcxdef", "dfx", "def", nil},
		{"dtx", "abcxdef", "dtx", "xdef", nil},
		{"dFx", "abxcdef", "$dFx", "abf", nil},
		{"d; repeats f", "a,b,c,d", "f,d;", "ac,d", nil},
		{"diw", "foo bar baz", "wdiw", "foo  baz", nil},
		{"daw", "foo bar baz", "wdaw", "foo baz", nil},
		{"daw last word", "foo bar", "wdaw", "foo", nil},
		{"ciw", "foo bar", "ciwX<Esc>", "X bar", nil},
		{"di(", "f(a, b) x", "fadi(", "f() x", nil},
		{"da(", "f(a, b) x", "fada(", "f x", nil},
		{"ci( nested", "f(a, g(b)) x", "fbci(Z<Esc>", "f(a, g(Z)) x", nil},
		{"2di(", "f(a, g(b)) x", "fb2di(", "f() x", nil},
		{"di( on paren", "(abc)", "di(", "()", nil},
		{"di{ multiline", "if {\n  a\n  b\n}", "jdi{", "if {\n}", nil},
		{"di\"", `say "hello world" ok`, "fhdi\"", `say "" ok`, nil},
		{"da\"", `say "hello" ok`, "fhda\"", `say ok`, nil},
		{"ci\" before quotes", `x = "abc"`, "ci\"Q<Esc>", `x = "Q"`, nil},
		{"dip", "a\nb\n\nc", "dip", "\nc", nil},
		{"dap", "a\nb\n\nc", "dap", "c", nil},
		{"yyp", "a\nb", "yyp", "a\na\nb", p(1, 0)},
		{"yyP", "a\nb", "jyyP", "a\nb\nb", p(1, 0)},
		{"yiwP", "foo bar", "wyiwP", "foo barbar", nil},
		{"ddp swaps lines", "a\nb", "ddp", "b\na", p(1, 0)},
		{"xp swaps chars", "ab", "xp", "ba", p(0, 1)},
		{"p charwise after", "ab", "ylp", "aab", p(0, 1)},
		{"3p", "a", "yl3p", "aaaa", p(0, 3)},
		{"named register", "foo\nbar", "\"ayyj\"ap", "foo\nbar\nfoo", nil},
		{"append register", "foo\nbar", "\"ayyj\"Ayy\"ap", "foo\nbar\nfoo\nbar", nil},
		{"black hole", "foo\nbar", "yyj\"_ddp", "foo\nfoo", nil},
		{"yank register 0 survives delete", "foo\nbar\nbaz", "yyjdd\"0p", "foo\nbaz\nfoo", nil},
		{"i insert", "bc", "ia<Esc>", "abc", p(0, 0)},
		{"a append", "ac", "ab<Esc>", "abc", p(0, 1)},
		{"A", "ab", "Acd<Esc>", "abcd", p(0, 3)},
		{"I", "  ab", "$Ix<Esc>", "  xab", p(0, 2)},
		{"o", "a\nc", "ob<Esc>", "a\nb\nc", p(1, 0)},
		{"O", "a\nc", "jOb<Esc>", "a\nb\nc", p(1, 0)},
		{"o autoindent", "  a", "ob<Esc>", "  a\n  b", nil},
		{"3ix", "", "3ix<Esc>", "xxx", p(0, 2)},
		{"3oab", "", "3oab<Esc>", "\nab\nab\nab", nil},
		{"insert CR splits", "abcd", "llia<CR><Esc>", "aba\ncd", nil},
		{"insert BS joins", "ab\ncd", "ji<BS><Esc>", "abcd", nil},
		{"insert C-w", "foo bar", "A<C-w><Esc>", "foo ", nil},
		{"insert C-u", "  foo bar", "A<C-u><Esc>", "  ", nil},
		{"r", "abc", "rx", "xbc", p(0, 0)},
		{"3r", "abcd", "3rx", "xxxd", p(0, 2)},
		{"r too far fails", "ab", "3rx", "ab", nil},
		{"R", "abcd", "Rxy<Esc>", "xycd", nil},
		{"R backspace restores", "abcd", "Rxy<BS><BS><Esc>", "abcd", nil},
		{"J", "a\n  b\nc", "J", "a b\nc", p(0, 1)},
		{"3J", "a\nb\nc\nd", "3J", "a b c\nd", nil},
		{"gJ", "a\n  b", "gJ", "a  b", nil},
		{"~", "abc", "~~", "ABc", p(0, 2)},
		{"g~w", "abc def", "g~w", "ABC def", nil},
		{"gUiw", "abc def", "gUiw", "ABC def", nil},
		{"guu", "ABC DEF", "guu", "abc def", nil},
		{"gUU", "abc", "gUU", "ABC", nil},
		{"gUgU", "abc", "gUgU", "ABC", nil},
		{">>", "a", ">>", "    a", nil},
		{"<<", "        a", "<<", "    a", nil},
		{">j", "a\nb\nc", ">j", "    a\n    b\nc", nil},
		{"3>>", "a\nb\nc\nd", "3>>", "    a\n    b\n    c\nd", nil},
		{"S", "  abc", "Sx<Esc>", "  x", nil},
		{"cc", "  abc\nd", "ccx<Esc>", "  x\nd", nil},
		{"s", "abc", "sX<Esc>", "Xbc", nil},
		{"Y", "a\nb", "Yp", "a\na\nb", nil},
		{"u undo", "abc", "xxu", "bc", nil},
		{"uu", "abc", "xxuu", "abc", p(0, 0)},
		{"C-r redo", "abc", "xxuu<C-r>", "bc", nil},
		{"insert is one undo", "", "ihello world<Esc>u", "", nil},
		{"cw undo is one step", "foo bar", "cwbaz<Esc>u", "foo bar", nil},
		{"dot repeat dw", "a b c d", "dw..", "d", nil},
		{"dot repeat with count", "a b c d e", "dw2.", "d e", nil},
		{"dot repeat insert", "x\nx", "Ahi<Esc>j.", "xhi\nxhi", nil},
		{"dot repeat cw", "foo foo", "cwbar<Esc>w.", "bar bar", nil},
		{"dot repeat o", "a", "ob<Esc>.", "a\nb\nb", nil},
		{"dot repeat x count", "abcdef", "2x.", "ef", nil},
		{"C-a", "x = 9", "<C-a>", "x = 10", p(0, 5)},
		{"5C-a", "x = 9", "5<C-a>", "x = 14", nil},
		{"C-x negative", "x = 1", "3<C-x>", "x = -2", nil},
		{"C-a hex", "0x0f", "<C-a>", "0x10", nil},
		{"C-a after word dash", "item-5", "<C-a>", "item-6", nil},
		{"macro", "a\nb\nc", "qqA!<Esc>jq@q@@", "a!\nb!\nc!", nil},
		{"macro with count", "1\n2\n3\n4", "qqA.<Esc>jq3@q", "1.\n2.\n3.\n4.", nil},
		{"visual d", "abcdef", "lvlld", "aef", nil},
		{"visual y P", "abc", "vly$p", "abcab", nil},
		{"visual line d", "a\nb\nc", "Vjd", "c", nil},
		{"visual >", "a\nb", "Vj>", "    a\n    b", nil},
		{"visual c", "abc def", "vecX<Esc>", "X def", nil},
		{"visual U", "abc", "vlU", "ABc", nil},
		{"visual J", "a\nb\nc", "VjjJ", "a b c", nil},
		{"visual iw", "foo bar", "wviwd", "foo ", nil},
		{"visual ip linewise", "a\nb\n\nc", "vipd", "\nc", nil},
		{"visual o swap", "abcdef", "llvlohd", "aef", nil},
		{"visual r", "abc", "vlrx", "xxc", nil},
		{"visual p replaces", "foo bar", "yiwwviwp", "foo foo", nil},
		{"visual block d", "abcd\nefgh\nijkl", "l<C-v>jjld", "ad\neh\nil", nil},
		{"visual block I", "ab\ncd\nef", "<C-v>jjI# <Esc>", "# ab\n# cd\n# ef", nil},
		{"visual block A", "ab\ncd", "<C-v>jA!<Esc>", "a!b\nc!d", nil},
		{"visual block $A", "ab\ncdef", "<C-v>j$A;<Esc>", "ab;\ncdef;", nil},
		{"visual block c", "abc\ndef", "l<C-v>jcX<Esc>", "aXc\ndXf", nil},
		{"visual block y p", "ab\ncd", "<C-v>jy$p", "aba\ncdc", nil},
		{"gv reselect", "abc def", "viw<Esc>wgvd", " def", nil},
		{"gc comment", "a = 1\nb = 2", "gcj", "# a = 1\n# b = 2", nil},
		{"gcc uncomment", "# a = 1", "gcc", "a = 1", nil},
		{"gq format", strings.Repeat("word ", 20), "gqq", "word word word word word word word word word word word word word word word word\nword word word word", nil},
		{"search n", "a x b x c x", "/x<CR>nD", "a x b ", nil},
		{"search backward", "x a x b", "$?x<CR>D", "x a ", nil},
		{"star", "foo bar foo", "*D", "foo bar ", nil},
		{"d/ search", "abc def ghi", "d/gh<CR>", "ghi", nil},
		{"marks", "a\nb\nc", "majjd'a", "", nil},
		{"mark exact", "abc def", "wmb0d`b", "def", nil},
		{"percent", "f(a(b)c)d", "f(d%", "fd", nil},
		{"G with count", "a\nb\nc", "2Gdd", "a\nc", nil},
		{"brace paragraph", "a\nb\n\nc", "d}", "\nc", nil},
		{"dot after visual-free change", "aaa", "rb$.", "bab", nil},
		{"insert jk no mapping types both", "", "ijk<Esc>", "jk", nil},
		{"ciw empty line", "", "ciwx<Esc>", "x", nil},
		{"o on empty buffer", "", "oa<Esc>", "\na", nil},
		{"dd only line", "abc", "dd", "", p(0, 0)},
		{"x on empty line", "", "x", "", nil},
		{"p linewise on empty", "a", "yyjp", "a\na", nil},
		{"ci( empty", "f()", "f(ci(x<Esc>", "f(x)", nil},
		{"ci( outside parens fails", "f()", "ci(x<Esc>", "()", nil},
		{"tab autoindent python", "def f():", "oreturn 1<Esc>", "def f():\n    return 1", nil},
		{"brace dedent", "func f() {", "o}<Esc>", "func f() {\n}", nil},
		{"alt key after insert is esc+key", "", "iab<M-x>", "a", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newTestEditor(t, c.text)
			if strings.HasPrefix(c.name, "tab autoindent python") {
				e.win.buf.Filetype = "python"
			}
			if strings.HasPrefix(c.name, "brace dedent") {
				e.win.buf.Filetype = "go"
				delete(e.opts, e.win.buf.ID)
			}
			typeKeys(e, c.keys)
			if got := curText(e); got != c.want {
				t.Errorf("keys %q on %q:\n got %q\nwant %q", c.keys, c.text, got, c.want)
			}
			if c.cursor != nil && e.win.cur != *c.cursor {
				t.Errorf("cursor %v, want %v", e.win.cur, *c.cursor)
			}
			if e.mode != ModeNormal {
				t.Errorf("mode %v after test", e.mode)
			}
		})
	}
}

func TestMotions(t *testing.T) {
	cases := []struct {
		name, text, keys string
		want             Pos
	}{
		{"w", "foo bar", "w", Pos{0, 4}},
		{"w punct", "foo.bar", "w", Pos{0, 3}},
		{"W", "foo.bar baz", "W", Pos{0, 8}},
		{"w across lines", "foo\nbar", "w", Pos{1, 0}},
		{"w empty line stop", "foo\n\nbar", "w", Pos{1, 0}},
		{"b", "foo bar", "$b", Pos{0, 4}},
		{"b across lines", "foo\nbar", "jb", Pos{0, 0}},
		{"e", "foo bar", "e", Pos{0, 2}},
		{"ee", "foo bar", "ee", Pos{0, 6}},
		{"ge", "foo bar", "$ge", Pos{0, 2}},
		{"0", "  foo", "$0", Pos{0, 0}},
		{"^", "  foo", "$^", Pos{0, 2}},
		{"$", "foo", "$", Pos{0, 2}},
		{"j keeps column", "abcd\nx\nabcd", "$jj", Pos{2, 3}},
		{"j column want", "abcd\nab\nabcd", "lllj" + "j", Pos{2, 3}},
		{"gg G", "a\nb\nc", "Ggg", Pos{0, 0}},
		{"5G", "a\nb\nc\nd\ne\nf", "5G", Pos{4, 0}},
		{"f", "abcabc", "fc", Pos{0, 2}},
		{"2f", "abcabc", "2fc", Pos{0, 5}},
		{"t", "abcabc", "tc", Pos{0, 1}},
		{"; after t", "abcabc", "tc;", Pos{0, 4}},
		{",", "abcabca", "$Fa,", Pos{0, 6}},
		{", no match stays", "abcabc", "$Fa,", Pos{0, 3}},
		{"%", "(a[b]c)", "%", Pos{0, 6}},
		{"% back", "(a[b]c)", "$%", Pos{0, 0}},
		{"%  finds next bracket", "x (a)", "%", Pos{0, 4}},
		{"}", "a\nb\n\nc", "}", Pos{2, 0}},
		{"{", "a\n\nb\nc", "G{", Pos{1, 0}},
		{"]c cells", "# %%\na\n# %%\nb", "]c", Pos{3, 0}},
		{"[c cells", "# %%\na\n# %%\nb", "G[c", Pos{1, 0}},
		{"]] sections", "(a)\n x\n(b)", "]]", Pos{2, 0}},
		{"search wraps", "x\na\nx", "G/x<CR>", Pos{0, 0}},
		{"n count", "a x x x", "/x<CR>2n", Pos{0, 6}},
		{"H M L", "1\n2\n3\n4\n5", "L", Pos{4, 0}},
		{"jump back", "a\nb\nc\nd", "G<C-o>", Pos{0, 0}},
		{"tab expansion column", "\tab", "$", Pos{0, 2}},
		{"<C-d>", strings.Repeat("x\n", 100), "<C-d>", Pos{14, 0}},
		{":N", "a\nb\nc", ":3<CR>", Pos{2, 0}},
		{"backtick mark", "abc\ndef", "lmaj`a", Pos{0, 1}},
		{"_ count", "a\n b\nc", "2_", Pos{1, 1}},
		{"- line up", "a\n b", "j-", Pos{0, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newTestEditor(t, c.text)
			typeKeys(e, c.keys)
			if e.win.cur != c.want {
				t.Errorf("keys %q: cursor %v want %v", c.keys, e.win.cur, c.want)
			}
		})
	}
}

func TestExCommands(t *testing.T) {
	cases := []struct {
		name, text, keys, want string
	}{
		{"s", "foo foo", ":s/foo/bar/<CR>", "bar foo"},
		{"s g", "foo foo", ":s/foo/bar/g<CR>", "bar bar"},
		{"%s", "a\na", ":%s/a/b/<CR>", "b\nb"},
		{"s groups", "john smith", `:s/(\w+) (\w+)/\2, \1/<CR>`, "smith, john"},
		{"s ampersand", "abc", ":s/b/[&]/<CR>", "a[b]c"},
		{"s case conv", "hello", `:s/\w+/\u&/<CR>`, "Hello"},
		{"s newline", "a,b", `:s/,/\r/<CR>`, "a\nb"},
		{"s other delim", "a/b", ":s#/#-#<CR>", "a-b"},
		{"s range", "x\nx\nx", ":2,3s/x/y/<CR>", "x\ny\ny"},
		{"s visual", "x\nx\nx", "Vj:s/x/y/<CR>", "y\ny\nx"},
		{"s word boundary", "cat category", `:s/\<lt>cat\>/dog/g<CR>`, "dog category"},
		{"s empty pattern reuses search", "abc", "/b<CR>:s//X/<CR>", "aXc"},
		{"&& repeat", "aa\naa", ":s/a/b/<CR>j:&&<CR>", "ba\nba"},
		{"g delete", "a1\nb\na2\nc", ":g/a/d<CR>", "b\nc"},
		{"g consecutive delete", "a\na\nb\na", ":g/a/d<CR>", "b"},
		{"v delete", "a1\nb\na2\nc", ":v/a/d<CR>", "a1\na2"},
		{"g! delete", "a1\nb", ":g!/a/d<CR>", "a1"},
		{"g normal", "a\nb", ":g/./normal A;<CR>", "a;\nb;"},
		{"normal range", "a\nb\nc", ":%norm Ax<CR>", "ax\nbx\ncx"},
		{"d range", "a\nb\nc\nd", ":2,3d<CR>", "a\nd"},
		{"d count", "a\nb\nc\nd", ":d 2<CR>", "c\nd"},
		{"y and put", "a\nb", ":1y<CR>:2put<CR>", "a\nb\na"},
		{"m", "a\nb\nc", ":1m$<CR>", "b\nc\na"},
		{"m 0", "a\nb\nc", ":3m0<CR>", "c\na\nb"},
		{"t copy", "a\nb", ":1t.<CR>", "a\na\nb"},
		{"copy to end", "a\nb", ":1co$<CR>", "a\nb\na"},
		{"j", "a\nb\nc", ":1,3j<CR>", "a b c"},
		{">", "a", ":><CR>", "    a"},
		{">> twice", "a", ":>><CR>", "        a"},
		{"sort", "c\na\nb", ":sort<CR>", "a\nb\nc"},
		{"sort!", "c\na\nb", ":sort!<CR>", "c\nb\na"},
		{"sort n", "10\n9\n100", ":sort n<CR>", "9\n10\n100"},
		{"sort u", "b\na\nb", ":sort u<CR>", "a\nb"},
		{"filter", "c\na\nb", ":%!sort<CR>", "a\nb\nc"},
		{"read cmd", "a", ":r !echo hi<CR>", "a\nhi"},
		{"range offsets", "a\nb\nc\nd", ":.+1,$-1d<CR>", "a\nd"},
		{"pattern range", "a\nb\nc\nd", ":/b/,/c/d<CR>", "a\nd"},
		{"mark range", "a\nb\nc\nd", "jmajmb:'a,'bd<CR>", "a\nd"},
		{"undo ex", "abc", ":s/a/x/<CR>:undo<CR>", "abc"},
		{"ex is one undo step", "a\na", ":%s/a/b/<CR>u", "a\na"},
		{"set et sw", "a", ":set sw=2<CR>>>", "  a"},
		{"set noet", "a", ":set noet<CR>>>", "\ta"},
		{"count colon", "a\nb\nc\nd", "2:d<CR>", "c\nd"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newTestEditor(t, c.text)
			typeKeys(e, c.keys)
			if got := curText(e); got != c.want {
				t.Errorf("keys %q:\n got %q\nwant %q (msg %q)", c.keys, got, c.want, e.msg)
			}
		})
	}
}

func TestFileCommands(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.py")
	os.WriteFile(path, []byte("x = 1\n"), 0o644)
	e := newTestEditor(t, "")
	typeKeys(e, ":e "+path+"<CR>")
	if e.win.buf.Path != path || e.win.buf.Filetype != "python" {
		t.Fatalf("edit: %q ft=%q", e.win.buf.Path, e.win.buf.Filetype)
	}
	typeKeys(e, "Ay = 2<Esc>:w<CR>")
	data, _ := os.ReadFile(path)
	if string(data) != "x = 1y = 2\n" {
		t.Fatalf("written %q", data)
	}
	if e.modified(e.win.buf) {
		t.Fatal("still modified after :w")
	}
	// :q with modified buffer refuses
	typeKeys(e, "x:q<CR>")
	if e.quit {
		t.Fatal("quit with unsaved changes")
	}
	if !strings.Contains(e.msg, "E37") {
		t.Fatalf("expected E37, got %q", e.msg)
	}
	// external change detection
	os.WriteFile(path, []byte("changed\n"), 0o644)
	future := e.modTimes[e.win.buf.ID].Add(-10e9)
	e.modTimes[e.win.buf.ID] = future
	typeKeys(e, ":w<CR>")
	if !strings.Contains(e.msg, "changed since reading") {
		t.Fatalf("expected overwrite warning, got %q", e.msg)
	}
	typeKeys(e, ":w!<CR>")
	typeKeys(e, ":q<CR>")
	if !e.quit {
		t.Fatalf("did not quit: %q", e.msg)
	}
}

func TestSplitsAndBuffers(t *testing.T) {
	e := newTestEditor(t, "one")
	typeKeys(e, ":vsplit<CR>")
	if len(e.windows()) != 2 {
		t.Fatal("vsplit failed")
	}
	typeKeys(e, ":enew<CR>itwo<Esc>")
	typeKeys(e, "<C-w>l")
	if e.win.buf.Text() != "one" {
		t.Fatalf("focus right: %q", e.win.buf.Text())
	}
	typeKeys(e, "<C-w>h")
	if e.win.buf.Text() != "two" {
		t.Fatalf("focus left: %q", e.win.buf.Text())
	}
	typeKeys(e, "<C-w>s<C-w>j")
	if len(e.windows()) != 3 {
		t.Fatal("split failed")
	}
	typeKeys(e, "<C-w>o")
	if len(e.windows()) != 1 {
		t.Fatal("only failed")
	}
	typeKeys(e, ":bn<CR>")
	if e.win.buf.Text() == "two" {
		t.Fatal("bn did not switch")
	}
	typeKeys(e, "<C-^>")
	if e.win.buf.Text() != "two" {
		t.Fatalf("alternate: %q", e.win.buf.Text())
	}
	typeKeys(e, ":bd!<CR>")
	if e.win.buf.Text() != "one" {
		t.Fatalf("bd: %q", e.win.buf.Text())
	}
}

func TestWhichKeyAndLeader(t *testing.T) {
	e := newTestEditor(t, "abc")
	typeKeys(e, "<Space>")
	if !e.whichKey {
		t.Fatal("which-key not shown for leader")
	}
	node := e.whichKeyNode()
	if node == nil || len(node.Entries()) < 5 {
		t.Fatal("which-key entries missing")
	}
	found := false
	for _, en := range node.Entries() {
		if en.Key == "n" && en.Group && en.Label == "+notebook" {
			found = true
		}
	}
	if !found {
		t.Fatalf("notebook group missing: %+v", node.Entries())
	}
	typeKeys(e, "<Esc>")
	if len(e.pending) != 0 || e.whichKey {
		t.Fatal("escape did not cancel")
	}
	// operator-pending which-key lists text objects
	typeKeys(e, "di")
	if n := e.whichKeyNode(); n == nil || len(n.Entries()) < 10 {
		t.Fatal("no op-pending text objects")
	}
	typeKeys(e, "<Esc>")
	// rendering the popup must not panic on a tiny screen
	e.screen.(tcell.SimulationScreen).SetSize(20, 5)
	typeKeys(e, "<Space>")
	e.draw()
	typeKeys(e, "<Esc>")
}

func TestUserMappings(t *testing.T) {
	s := tcell.NewSimulationScreen("UTF-8")
	s.Init()
	s.SetSize(80, 24)
	defer s.Fini()
	cfg := config.Default()
	cfg.Keys = map[string]map[string]string{
		"insert": {"jk": "<Esc>"},
		"normal": {"<leader>x": ":s/a/b/", "Q": "dd"},
	}
	e := New(s, cfg)
	e.Open(nil)
	typeKeys(e, "iaja<Esc>")
	if curText(e) != "aja" {
		t.Fatalf("partial mapping: %q", curText(e))
	}
	typeKeys(e, "Ajk")
	if e.mode != ModeNormal || curText(e) != "aja" {
		t.Fatalf("jk mapping: mode %v text %q", e.mode, curText(e))
	}
	typeKeys(e, "<Space>x")
	if curText(e) != "bja" {
		t.Fatalf("leader mapping: %q", curText(e))
	}
	typeKeys(e, "Q")
	if curText(e) != "" {
		t.Fatalf("Q mapping: %q", curText(e))
	}
}

func TestPaste(t *testing.T) {
	e := newTestEditor(t, "ab")
	e.handleEvent(tcell.NewEventPaste(true))
	for _, k := range keymap.Parse("x<CR>  y", "") {
		if k == "<CR>" {
			e.handleEvent(tcell.NewEventKey(tcell.KeyEnter, 0, 0))
			continue
		}
		e.handleEvent(tcell.NewEventKey(tcell.KeyRune, []rune(keyText(k))[0], 0))
	}
	e.handleEvent(tcell.NewEventPaste(false))
	if got := curText(e); got != "ax\n  yb" {
		t.Fatalf("normal-mode paste: %q", got)
	}
	typeKeys(e, "u")
	if curText(e) != "ab" {
		t.Fatalf("paste not one undo step: %q", curText(e))
	}
}

func TestPicker(t *testing.T) {
	if s, _, ok := fuzzyScore("internal/editor/render.go", "rend"); !ok || s <= 0 {
		t.Fatal("fuzzy match failed")
	}
	if _, _, ok := fuzzyScore("abc", "abd"); ok {
		t.Fatal("unexpected match")
	}
	a, _, _ := fuzzyScore("src/render.go", "render")
	b, _, _ := fuzzyScore("r_e_n_d_e_r.txt", "render")
	if a <= b {
		t.Fatalf("contiguous match should score higher: %d vs %d", a, b)
	}
	e := newTestEditor(t, "alpha\nbeta\ngamma")
	typeKeys(e, "<Space>ss")
	if e.picker == nil {
		t.Fatal("picker not open")
	}
	typeKeys(e, "gam<CR>")
	if e.picker != nil || e.win.cur.Line != 2 {
		t.Fatalf("picker select: line %d", e.win.cur.Line)
	}
}

func TestRenderSmoke(t *testing.T) {
	e := newTestEditor(t, "package main\n\nfunc main() {\n\tprintln(\"héllo 世界\")\n}\n\x01ctrl")
	e.win.buf.Filetype = "go"
	typeKeys(e, "jjvj")
	e.draw()
	typeKeys(e, "<Esc>:set rnu<CR>:vsplit<CR>:split<CR>")
	e.draw()
	// cursor is visible on screen
	if e.win.curX < 0 {
		t.Fatal("cursor not placed")
	}
	sim := e.screen.(tcell.SimulationScreen)
	cells, w, _ := sim.GetContents()
	var row strings.Builder
	for x := 0; x < w; x++ {
		row.WriteString(string(cells[x].Runes))
	}
	if !strings.Contains(row.String(), "package") {
		t.Fatalf("first row not drawn: %q", row.String())
	}
}
