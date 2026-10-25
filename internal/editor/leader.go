package editor

import (
	"scihe/internal/keymap"
)

// setupLeader installs the <leader> bindings shown by which-key.
func (e *Editor) setupLeader() {
	L := e.leader
	group := func(keys, name string) {
		e.normalMap.SetGroup(keymap.Parse(keys, L), name)
		e.visualMap.SetGroup(keymap.Parse(keys, L), name)
	}
	n := func(keys, label string, f func(c *ctx)) {
		e.normalMap.Bind(keymap.Parse(keys, L), label, action(f))
	}
	v := func(keys, label string, f func(c *ctx)) {
		e.visualMap.Bind(keymap.Parse(keys, L), label, action(f))
	}
	ex := func(keys, label, cmd string) {
		e.normalMap.Bind(keymap.Parse(keys, L), label, &Cmd{Kind: KindAction, Action: func(c *ctx) { c.e.execEx(cmd) }})
	}
	e.normalMap.SetGroup([]Key{L}, "leader")
	e.visualMap.SetGroup([]Key{L}, "leader")

	n("<leader><leader>", "find file", func(c *ctx) { c.e.pickFile() })
	n("<leader>/", "grep project", func(c *ctx) { c.e.pickGrep("") })
	n("<leader>,", "switch buffer", func(c *ctx) { c.e.pickBuffer() })
	n("<leader>:", "command history", func(c *ctx) { c.e.pickHistory() })
	n("<leader>?", "all key bindings", func(c *ctx) { c.e.execEx("help") })

	group("<leader>f", "file")
	n("<leader>ff", "find file", func(c *ctx) { c.e.pickFile() })
	n("<leader>fr", "recent / open files", func(c *ctx) { c.e.pickBuffer() })
	ex("<leader>fs", "save", "w")
	ex("<leader>fS", "save all", "wa")
	n("<leader>fe", "file explorer", func(c *ctx) { c.e.exploreHere() })
	n("<leader>fn", "new file", func(c *ctx) {
		c.e.prompt("New file: ", "", func(s string, ok bool) {
			if ok && s != "" {
				c.e.execEx("e " + s)
			}
		})
	})
	n("<leader>fR", "rename (save as)", func(c *ctx) {
		c.e.prompt("Save as: ", c.b.Path, func(s string, ok bool) {
			if ok && s != "" {
				c.e.execEx("saveas " + s)
			}
		})
	})
	ex("<leader>fy", "reload from disk", "e")
	ex("<leader>fY", "reload (discard changes)", "e!")

	group("<leader>b", "buffer")
	n("<leader>bb", "switch buffer", func(c *ctx) { c.e.pickBuffer() })
	ex("<leader>bn", "next buffer", "bn")
	ex("<leader>bp", "previous buffer", "bp")
	ex("<leader>bd", "delete buffer", "bd")
	ex("<leader>bD", "delete buffer (force)", "bd!")
	ex("<leader>ba", "delete all buffers", "bdall")
	ex("<leader>bA", "delete all buffers (force)", "bdall!")
	ex("<leader>bl", "buffer list (dired)", "ls")
	n("<leader>bs", "scratch buffer", func(c *ctx) { c.e.execEx("enew") })

	group("<leader>w", "window")
	ex("<leader>ws", "split", "split")
	ex("<leader>wv", "vertical split", "vsplit")
	ex("<leader>wc", "close", "close")
	ex("<leader>wo", "only", "only")
	for _, d := range []struct {
		keys   []string
		dx, dy int
		label  string
	}{
		{[]string{"h", "<Left>"}, -1, 0, "go left"},
		{[]string{"j", "<Down>"}, 0, 1, "go down"},
		{[]string{"k", "<Up>"}, 0, -1, "go up"},
		{[]string{"l", "<Right>"}, 1, 0, "go right"},
	} {
		d := d
		for _, k := range d.keys {
			n("<leader>w"+k, d.label, func(c *ctx) { c.e.focus(c.e.neighbor(c.w, d.dx, d.dy)) })
		}
	}
	n("<leader>ww", "next window", func(c *ctx) { c.e.cycleWindow(1) })
	n("<leader>w=", "equalize", func(c *ctx) { c.e.equalizeAll() })
	ex("<leader>w<Space>", "next layout", "layout next")
	n("<leader>wr", "resize mode (h/j/k/l)", func(c *ctx) { c.e.resizeStep(true, 0) })

	group("<leader>s", "search")
	n("<leader>ss", "search lines in buffer", func(c *ctx) { c.e.pickLines() })
	n("<leader>sg", "grep project", func(c *ctx) { c.e.pickGrep("") })
	n("<leader>sw", "grep word under cursor", func(c *ctx) {
		c.e.pickGrep(wordAt(c.b.Line(c.w.cur.Line), c.w.cur.Col))
	})
	n("<leader>sr", "replace word under cursor", func(c *ctx) {
		w := wordAt(c.b.Line(c.w.cur.Line), c.w.cur.Col)
		c.e.startCmdline(':', `%s/\<`+w+`\>/`)
	})
	ex("<leader>sh", "clear highlight", "nohlsearch")
	n("<leader>sm", "marks", func(c *ctx) { c.e.execEx("marks") })
	n("<leader>sR", "registers", func(c *ctx) { c.e.execEx("registers") })

	group("<leader>p", "python")
	ex("<leader>pr", "run file", "run")
	n("<leader>pR", "run file with args", func(c *ctx) {
		c.e.prompt("run args: ", "", func(s string, ok bool) {
			if ok {
				c.e.execEx("run " + s)
			}
		})
	})
	ex("<leader>pk", "restart kernel / REPL", "repl restart")

	group("<leader>r", "repl / eval")
	n("<leader>rr", "eval form / line", func(c *ctx) { c.e.evalCurrent(false) })
	n("<leader>rt", "eval top-level form / cell", func(c *ctx) { c.e.evalCurrent(true) })
	n("<leader>rb", "eval buffer", func(c *ctx) { c.e.evalBuffer() })
	n("<leader>rc", "start / connect REPL", func(c *ctx) { c.e.execEx("repl start") })
	n("<leader>rj", "jack-in (start nREPL)", func(c *ctx) { c.e.execEx("repl jack-in") })
	n("<leader>rq", "quit REPL", func(c *ctx) { c.e.execEx("repl quit") })
	n("<leader>ri", "interrupt", func(c *ctx) { c.e.execEx("repl interrupt") })
	n("<leader>rl", "show REPL log", func(c *ctx) { c.e.execEx("repl log") })
	n("<leader>rx", "clear inline results", func(c *ctx) { delete(c.e.annots, c.b.ID) })
	n("<leader>rd", "doc for symbol", func(c *ctx) { c.e.docLookup() })
	// "<leader>re{motion}" evaluates a motion / text object
	e.normalMap.Bind(keymap.Parse("<leader>re", L), "eval {motion}", operator(OpEval))
	v("<leader>rr", "eval selection", func(c *ctx) { c.e.evalSelection(c) })
	v("<leader>re", "eval selection", func(c *ctx) { c.e.evalSelection(c) })

	group("<leader>n", "notebook")
	ex("<leader>nr", "run cell", "nbrun")
	n("<leader>nn", "run cell and go to next", func(c *ctx) { c.e.nbRunAndNext() })
	ex("<leader>na", "run all cells", "nbrunall")
	n("<leader>nA", "run cells above", func(c *ctx) { c.e.nbRunRange(false) })
	n("<leader>nB", "run cell and below", func(c *ctx) { c.e.nbRunRange(true) })
	n("<leader>no", "new cell below", func(c *ctx) { c.e.nbInsertCell(true, "code") })
	n("<leader>nO", "new cell above", func(c *ctx) { c.e.nbInsertCell(false, "code") })
	n("<leader>nm", "toggle markdown/code", func(c *ctx) { c.e.nbToggleType() })
	n("<leader>nt", "set cell type…", func(c *ctx) { c.e.startCmdline(':', "nbtype ") })
	n("<leader>nd", "delete cell", func(c *ctx) { c.e.nbDeleteCell() })
	ex("<leader>nc", "clear cell output", "nbclear")
	ex("<leader>nC", "clear all outputs", "nbclear!")
	ex("<leader>ni", "interrupt kernel", "nbinterrupt")
	ex("<leader>nk", "restart kernel", "nbrestart")
	ex("<leader>nv", "view full output", "nboutput")
	ex("<leader>nI", "open image outputs", "nbimage")
	n("<leader>nj", "move cell down", func(c *ctx) { c.e.nbMoveCell(true) })
	n("<leader>nK", "move cell up", func(c *ctx) { c.e.nbMoveCell(false) })
	n("<leader>nx", "toggle output folding", func(c *ctx) { c.e.nbToggleFold() })
	n("<leader>ne", "export to .py", func(c *ctx) { c.e.execEx("nbexport") })

	group("<leader>g", "git")
	ex("<leader>gg", "status", "git")
	ex("<leader>gd", "diff current file", "git diff")
	ex("<leader>gD", "diff all", "git diff HEAD")
	ex("<leader>gl", "log", "git log")
	ex("<leader>gL", "log of current file", "git log %")
	ex("<leader>gb", "blame", "git blame")
	ex("<leader>ga", "stage current file", "git add")
	ex("<leader>gc", "commit", "git commit")
	ex("<leader>gp", "push", "git push")
	ex("<leader>gP", "pull (fast-forward)", "git pull --ff-only")

	group("<leader>d", "data / table")
	ex("<leader>dt", "toggle table view", "table")
	for _, k := range []struct{ key, label, cmd string }{
		{"s", "statistics (describe)", "stats"},
		{"h", "histogram", "hist"},
		{"u", "sum", "sum"},
		{"m", "mean", "mean"},
		{"M", "median", "median"},
		{"n", "min", "min"},
		{"x", "max", "max"},
		{"c", "count", "count"},
		{"d", "std (standard deviation)", "std"},
		{"v", "var (variance)", "var"},
	} {
		k := k
		n("<leader>d"+k.key, k.label, func(c *ctx) { c.e.execEx(k.cmd) })
		v("<leader>d"+k.key, k.label, func(c *ctx) { c.e.execEx(k.cmd) })
	}
	n("<leader>dw", "filter rows (:where)", func(c *ctx) { c.e.startCmdline(':', "where ") })
	n("<leader>dr", "correlation matrix", func(c *ctx) { c.e.execEx("corr") })
	v("<leader>dr", "correlation matrix", func(c *ctx) { c.e.execEx("corr") })
	n("<leader>df", "frequency table", func(c *ctx) { c.e.execEx("freq") })
	n("<leader>dg", "group by (:groupby)", func(c *ctx) { c.e.startCmdline(':', "groupby ") })
	n("<leader>dC", "computed column (:compute)", func(c *ctx) { c.e.startCmdline(':', "compute ") })
	n("<leader>dT", "transform column (:transform)", func(c *ctx) { c.e.startCmdline(':', "transform ") })

	group("<leader>m", "markdown")
	ex("<leader>mp", "toggle preview", "preview")

	group("<leader>t", "toggle")
	ex("<leader>tn", "line numbers", "set number!")
	ex("<leader>tr", "relative numbers", "set relativenumber!")
	ex("<leader>tc", "cursor line", "set cursorline!")
	ex("<leader>th", "search highlight", "set hlsearch!")
	ex("<leader>ti", "ignore case", "set ignorecase!")
	ex("<leader>te", "expand tab", "set expandtab!")
	ex("<leader>tw", "line wrap (this buffer)", "set wrap!")
	ex("<leader>ta", "auto-reload (this buffer)", "set autoreload!")
	ex("<leader>tp", "colored brackets (this buffer)", "set colorbrackets!")
	n("<leader>tt", "theme", func(c *ctx) { c.e.pickTheme() })
	ex("<leader>tT", "transparent background", "set transparent!")
	n("<leader>tl", "language", func(c *ctx) { c.e.pickFiletype() })

	group("<leader>h", "help")
	ex("<leader>hk", "key bindings", "help")
	ex("<leader>hm", "messages", "messages")
	ex("<leader>hl", "languages", "languages")
	n("<leader>hc", "commands", func(c *ctx) { c.e.pickCommand() })

	group("<leader>q", "quit")
	ex("<leader>qq", "quit all", "qa")
	ex("<leader>qQ", "quit all (discard changes)", "qa!")
	ex("<leader>qw", "write all and quit", "wqa")

	// q closes read-only special buffers (help, logs, output, preview, …);
	// a buffer's own keys (table view, git status) come first
	e.readOnlyMap = keymap.NewMap[*Cmd]()
	if _, mapped := e.cfg.Keys["normal"]["q"]; !mapped {
		e.readOnlyMap.Bind(keymap.Parse("q", L), "close", action(func(c *ctx) { c.e.closeReadOnly(c.w, c.b) }))
	}

	// visual gc/gq/> etc. come from operators; add a few visual leader actions
	v("<leader>sg", "grep selection", func(c *ctx) {
		r := c.e.visualRange()
		c.e.exitVisual()
		c.e.pickGrep(c.e.rangeText(c.b, r).Text)
	})
}
