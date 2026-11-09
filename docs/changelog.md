# Changelog

All notable changes to Scihe. Versions follow `MAJOR.MINOR.PATCH`;
`scihe -version` prints the installed one.

## 0.2.4 — 2026-11-09

### Changed
- Typeset formulas: a blank row sets off two formula rows whose ink
  would meet over the boundary between them (as TeX's `\lineskip`),
  instead of every two rows showing a formula. A formula taller than a
  line of text — a fraction, a root with an inner formula, a sum with
  limits — fills the cells it takes and keeps its spacing; small
  formulas, like a letter with a superscript or a subscript, leave as
  much room around them as text does and are no longer set apart. The
  bars of tables and quotes go on through the blank row as before
  ([Formulas](formulas.md)).

## 0.2.3 — 2026-11-05

### Added
- Formulas are typeset as LaTeX typesets them, in terminals that show
  images (Ghostty and kitty, with the Unicode placeholders of the
  kitty graphics protocol; Konsole, with images placed on their cells):
  TeX's rules with the MATH table of Scihe Math, the font built in:
  Latin Modern Math with its names changed, under the GUST Font License
  (see the README), so scripts, limits and fractions are set in smaller
  sizes instead of whole rows of text, roots and delimiters are drawn
  as tall as they need, and a line is as tall as its formula. In the
  rendered markdown view and in notebook outputs. `math_scale` sets
  their size (1.2 times the text); scripts keep at least 0.6 of it.
  Inline formulas fit in their line, made smaller down to the size of
  the text if need be. Unicode scripts, `<sup>`, `<sub>` and footnote
  marks in the text are set the same way, on their line. Rows of
  different formulas that would touch (consecutive lines, list items,
  table rows) are set off by a blank row; the bars of tables and
  quotes go on through it.
  `math_images = false` keeps the text layout, which other terminals
  use, as do formulas with a character the font lacks.
- Inside tmux the editor turns `allow-passthrough` on for its own pane
  while it runs, and back as it was on exit (also after a crash); it
  needs tmux to pass RGB colours to the terminal. Editors in several
  panes keep their images apart. `Ctrl-l` sends the images again.
- Yanking from the rendered view copies typeset formulas as text on one
  line (`E = mc²`); `:nboutput` shows outputs as text.
- A [Formulas](formulas.md) page: terminals, tmux, size and spacing,
  the font and its license.

### Changed
- Tables in the rendered view set their rows of one line apart with a
  blank row (the bars going on through it), so they are easier to read.
- Notebooks: consecutive LaTeX or markdown outputs are set off by a
  blank row.
- `\dfrac` and `\cfrac` set their fraction in display style, as in
  LaTeX (typeset formulas).

## 0.2.2 — 2026-11-03

### Added
- The rendered markdown view (`Space m r`) is laid out as a page:
  formulas in two dimensions (stacked fractions, raised and lowered
  scripts, limits above and below `∑` and `lim`, roots with their bar,
  brackets and integrals as tall as what they enclose, matrices, cases,
  aligned rows), markers of emphasis, code and links replaced by their
  styles, headings underlined, hanging lists, coloured alert bars,
  images shown as `▣ alt`, HTML comments hidden. Unicode super- and
  subscripts, `<sup>`/`<sub>` and footnote marks are raised and lowered
  in the size of the text. The line numbers are those of the file.
- Formulas are written in LaTeX: `\frac{a}{b}`, `x^{2}`, `\sqrt[3]{x}`,
  `\sum_{k=1}^{n}`, `\left( … \right)`, `\alpha`, `\mathbb{R}`,
  `\text{…}`, `\operatorname{…}`, and the `matrix`, `pmatrix`,
  `bmatrix`, `cases`, `array` and `aligned` environments. A line
  opening an environment (`\begin{equation}`, `\begin{align*}`, …)
  starts a display formula without dollars, up to its `\end`. Scripts
  are set compactly: what is inside a script stays on one row and
  simple scripts in a fraction are Unicode characters, so formulas are
  no taller than they need.
- Notebook outputs in LaTeX or markdown (sympy expressions,
  `IPython.display.Math`, `Latex`, `Markdown`) are drawn in the
  `Out [n]` box as the rendered view draws them: formulas in two
  dimensions, at the left edge, a blank row around those of several
  rows. Other outputs, and outputs over 64 KB, are shown as text;
  markdown cells are left as written.
- `Space m ^` / `Space m _` turn the number, character or selection
  into Unicode super- or subscripts (`mc²`, `H₂O`) and back, with
  `<sup>`/`<sub>` for characters that have no such form. `Space m f`
  puts `$…$` around a word or selection, `Space m F` opens a `$$`
  block. `Space m g`, or `Ctrl-k` in insert mode, inserts a Greek
  letter or symbol picked by name.

- Tables in the rendered view separate their rows with a rule, and
  their header with a double rule, when a row takes several lines (a
  formula or wrapped text in a cell).

### Changed
- With `wrap` on, `j`, `k`, `<Down>` and `<Up>` move by screen row
  (also the arrows in insert mode); with a count they move by line, and
  operators (`dj`) still take whole lines. `gj`/`gk` and the row moves
  keep the column across short rows.
- The rendered view shares the options of its file: `wrap` set in one
  is set in the other. With `wrap`, paragraphs and tables fit the
  window (a table's widest columns give way and their cells wrap) and
  are laid out again when the window is resized; without it, every line
  of the file keeps its row.

### Fixed
- Many lines opening a display formula that is never closed no longer
  make the rendered view slow (each searched the rest of the file).
- C1 control characters (U+0080–U+009F) in a buffer are drawn as `?`
  instead of being sent to the terminal, which could read them as
  escape sequences.

## 0.2.1 — 2026-11-01

### Added
- System clipboard keys, as in helix: `Space y` + motion (`Space y y`
  for the line) and `Space Y` yank to the clipboard, `Space p` /
  `Space P` paste from it; in visual mode `Space y` yanks the selection,
  `Space Y` its lines, and `Space p` / `Space R` replace it (`Space P`
  keeps the register). `y` and `p` keep using the editor's registers;
  `<C-r>+` and `@+` no longer read the clipboard.
- A missing clipboard tool (`wl-copy`, `xclip`, `xsel`) is reported
  when these keys are used.
- The rendered markdown view (`Space m r`) also draws GitHub tables
  (aligned columns with `│`, a `─┼─` rule under the header, formulas in
  cells), quotes (`┃`), alerts and Obsidian callouts (`> [!NOTE]` reads
  `┃ ℹ Note`), horizontal rules and task checkboxes (`☐` / `☑`). Every
  line of the file stays one line of the view; code and YAML front
  matter are left as written. `examples/formulas.md` has examples of
  each.

### Changed
- The `"+` and `"*` registers are gone: use the clipboard keys.
  `:yank +` and `:put +` report an invalid register.
- The python group (`Space p`) is gone, its keys moved to `Space r`:
  `Space r f` runs the file, `Space r F` runs it with arguments,
  `Space r k` restarts the REPL or kernel.
- `Space m r` / `:mdrender` is labelled "rendered view" instead of
  "rendered formulas".
- The rendered markdown view is computed about 3x faster (fewer
  allocations and regexp calls per line).

### Fixed
- Yanking to the clipboard failed silently when no clipboard tool
  could be used: the text only reached an internal copy.
- The which-key popup listed groups with no keys in the current mode
  (visual `Space b`, `f`, `w`, ...) with a blank description. Only keys
  that do something are shown now, and an empty group's key is unbound.
- Pasting from the system clipboard could freeze the editor when the
  clipboard tool hung; it now gives up after 2 seconds with an error.
  A failing tool's own message is shown instead of "exit status 1".
- `:registers` could cut a multi-byte character when shortening long
  contents.

## 0.2.0 — 2026-10-30

### Added
- Git status: `U` unstages all staged files. During a merge with
  conflicts it does nothing (it would mark them resolved); unstage files
  one by one with `u`.
- Markdown formatting under `Space m`: bold, italic, inline code,
  strikethrough, links, headings, task checkboxes, bullet and numbered
  lists, quotes, code blocks (most also on a visual selection), table
  alignment, a table of contents and heading jumps. They are toggles:
  - the emphasis keys act on the whole span the cursor is in (anywhere
    in it, markers included, also `__bold__` and `_italic_`): bold turns
    into italic and back, and the key of the current style removes it;
    a selection that would cross spans is refused;
  - `Space m l` in a link leaves its text, `Space m C` in a code block
    removes its fences.
- `Space m r` (`:mdrender`) shows formulas (`$…$`, `$ … $`, `$$…$$`,
  `\(…\)`, `\[…\]`, `\begin{equation}` and other math environments) as
  Unicode text in a read-only view of the buffer; `Space f y` and
  `:set autoreload` in the view act on the markdown file.
  `examples/formulas.md` is a sample.
- `Space m I` (`:mdimage`) opens the image under the cursor with the
  system viewer. Only image files and `http(s)` links are opened: the
  system opener also runs programs.

### Fixed
- Git: file names with `[`, `*` or `?` (such as `[id].tsx`) were taken
  as patterns by `s`, `u`, `x`, `d`, `:git add`, `:git diff` and
  `:git log FILE`, so staging `[id].tsx` also staged `i.tsx` and
  `d.tsx`. They now act on that file only.

### Removed
- The markdown preview (`:preview`, `:MarkdownPreview`, `Space m p`):
  it showed the same text as the buffer. `:run` in a markdown file no
  longer opens it.

## 0.1.2 — 2026-10-25

### Added
- `delete_to_register = false` in `config.toml`: deletes (`d`, `D`,
  `c`, `x`, visual `p`, `Space n d`, `:d`, …) no longer overwrite the
  text `p` pastes. Naming a register (`"ad`) still stores it. Vim's
  behaviour (`true`) stays the default.
- Colored brackets: `Space t p` (`:set colorbrackets!`) colors brackets
  by nesting depth; a closing bracket without an opening one shows in
  the error color. Brackets in strings and comments are skipped, and
  each notebook cell starts afresh. `colorbrackets = true` in `[options]`
  or `[filetype.NAME]` turns it on by default. Typing stays as fast as
  without it, also in large files.

### Changed
- Notebooks take part in auto-reload: a change on disk is reported, and
  with `autoreload` on an unmodified notebook reloads (after any running
  cells finish). Before, notebook changes on disk went unnoticed.
- `Space t a` (`:set autoreload`) in a buffer that can't reload (git,
  explorer, buffer list, output, logs, the markdown preview) now says
  so instead of silently doing nothing. Turning it off is always
  accepted.
- The matching bracket is highlighted in insert mode too (the bracket
  just typed, or under the cursor, and its pair), and no longer in
  visual mode (it stays while typing a `:` command or a search). It looks at most 3000 lines away, so typing stays fast
  in big files; `%` still searches the whole buffer.

### Fixed
- Visual `p` in a read-only buffer (help, output, logs) changed nothing
  but still replaced the register with the selected text; the register
  is now kept.
- `:nbexport` commented out lines starting with `!` inside triple-quoted
  strings, changing the program; only real shell lines are commented now.
- `:run` arguments with an apostrophe (`:run it's here`) were merged into
  one; an unclosed quote is now read as a plain character.
- `:w FILE` on the buffer list, the file explorer or git status turned
  the listing into a file while it kept working as a listing; it now
  writes a copy of the listing and leaves it as it is.
- nREPL: a server that closed the connection left a background writer
  and the socket open; a server that closed before starting a session
  made the editor wait 5 s; `:repl quit` could cut off the `close`
  request sent to the server.
- The buffer list misaligned its columns once buffer numbers passed 999.
- Reloading a notebook (`Space f y`) kept showing the old outputs instead
  of the ones in the file.
- `:bdall` did not count a markdown preview deleted with its file.

## 0.1.1 — 2026-10-22

### Added
- `Space f Y` (`:e!`): reload from disk, discarding changes.
- `:bdall` / `:bdall!` (`Space b a` / `Space b A`): delete all buffers.
  Without `!`, modified buffers and tables with staged edits are kept.
  Logs that a running job or REPL still writes to are always kept.
- Notebooks: lines starting with `!` run in the shell, like in Jupyter.
  An interrupt stops the command and nothing else the kernel started.
  `:nbexport` writes them as comments.
- `q` closes read-only buffers: help, messages, statistics, command
  output, REPL and `*run*` logs, git diff/log/blame, the buffer list
  and the markdown preview.
- This changelog.

### Changed
- `:run ARGS` honours quotes: `:run -c "print(1)"` passes two arguments.
- Output that child processes of a notebook kernel write directly is
  batched: a program printing 20,000 lines no longer sends one event per
  line. Output written while the cell runs now arrives before the cell
  finishes, in order with `print` output; before, the end of it could be
  lost.
- The example notebook and the notebook docs say plainly that images
  are placeholders and that LaTeX, HTML and math are shown as source.
- The docs explain that the font is set in the terminal, not in Scihe.

### Fixed
- Output buffers (`*help*`, `*messages*`, `*stats*`, `*shell*`, git
  diff/blame, `*run*`, …) could be edited by accident; they are
  read-only now. Writing one to a file (`:w FILE`) makes it an ordinary,
  editable file.
- Key labels longer than 32 characters were cut off in the `Space`
  popup (e.g. `Space f Y`); all labels now fit, and a test keeps them so.
- `:bd` without `!` deleted a table view (or its source) with staged
  edits without asking; it now refuses, like for a modified file.
- `:q` / `:qa` quit with staged table edits; they now refuse (`!`
  overrides).
- Deleting a markdown buffer left its preview window behind, with stale
  text; the preview now goes with it.
- `:g/pat/j` joined all matching lines into one; it now joins them in
  pairs, like vim.
- `:put 0` … `:put 9` put the unnamed register instead of registers 0–9.
- An nREPL server that stopped reading froze the editor on evaluation,
  and `:repl quit` hung too. Requests are now written in the background.
- Saving an empty file wrote a newline into it. A file with mixed line
  endings was saved with all CRLF; its line endings are now kept as
  they are (a lone `\r` shows as `^M`).

## 0.1.0 — 2026-10-21

First release: vim-style modal editing, fuzzy pickers, syntax
highlighting and themes, REPLs, Jupyter notebooks, the CSV/TSV data
viewer, markdown preview, git, the file explorer and auto-reload.
