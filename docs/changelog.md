# Changelog

All notable changes to Scihe. Versions follow `MAJOR.MINOR.PATCH`;
`scihe -version` prints the installed one.

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
