# Keymap

The leader key is `Space`. Press it and wait to see the keys below.
`:help` lists every binding.

## Global

| Key | Description |
|-----|-------------|
| `Space Space` | Find file |
| `Space ,` | Switch buffer |
| `Space /` | Grep project |
| `Space :` | Command history |
| `Space ?` | All key bindings |

## Clipboard

`y` and `p` use the editor's own registers. The system clipboard has
its own keys, as in helix:

| Key | Description |
|-----|-------------|
| `Space y` + motion | Yank to the clipboard, e.g. `Space y iw`; `Space y y` yanks the line |
| `Space Y` | Yank the line (with a count, lines) to the clipboard |
| `Space p`, `Space P` | Paste the clipboard after, before the cursor |
| `Space y` (visual) | Yank the selection to the clipboard |
| `Space Y` (visual) | Yank the selected lines to the clipboard, as `Y` does |
| `Space p`, `Space R` (visual) | Replace the selection with the clipboard |
| `Space P` (visual) | Replace the selection, keeping the register `p` pastes |

The clipboard is reached through `wl-copy`/`wl-paste`, `xclip`, `xsel`
or `pbcopy`/`pbpaste`. Without one of them these keys show an error; a
paste that gets no answer within 2 seconds gives up with an error.

## File (`Space f`)

| Key | Description |
|-----|-------------|
| `Space f f` | Find file |
| `Space f r` | Recent and open files |
| `Space f e` | File explorer |
| `Space f n` | New file |
| `Space f s` | Save |
| `Space f S` | Save all |
| `Space f R` | Rename (save as) |
| `Space f y` | Reload from disk |
| `Space f Y` | Reload from disk, discarding changes |

## Buffer (`Space b`)

| Key | Description |
|-----|-------------|
| `Space b b` | Switch buffer |
| `Space b n` | Next buffer |
| `Space b p` | Previous buffer |
| `Space b d` | Delete buffer |
| `Space b D` | Delete buffer, discarding changes |
| `Space b a` | Delete all buffers (keeps modified ones) |
| `Space b A` | Delete all buffers, discarding changes |
| `Space b s` | Scratch buffer |
| `Space b l` | [Buffer list](file-explorer.md#buffer-list) |

## Window (`Space w`)

| Key | Description |
|-----|-------------|
| `Space w s` | Split |
| `Space w v` | Vertical split |
| `Space w c` | Close |
| `Space w o` | Close the other windows |
| `Space w w` | Next window |
| `Space w h` `j` `k` `l` | Go to the window on the left, below, above, right |
| `Space w =` | Equalize sizes |
| `Space w Space` | Next layout |
| `Space w r` | Resize mode (`h` `j` `k` `l`) |

## Search (`Space s`)

| Key | Description |
|-----|-------------|
| `Space s s` | Search lines in the buffer |
| `Space s g` | Grep project |
| `Space s w` | Grep the word under the cursor |
| `Space s r` | Replace the word under the cursor |
| `Space s h` | Clear the search highlight |
| `Space s m` | Marks |
| `Space s R` | Registers |

## REPL (`Space r`)

| Key | Description |
|-----|-------------|
| `Space r r` | Evaluate the form or line (visual mode: the selection) |
| `Space r t` | Evaluate the top-level form or `# %%` cell |
| `Space r b` | Evaluate the buffer |
| `Space r e` + motion | Evaluate a motion, e.g. `Space r e ip` |
| `Space r c` | Start or connect a REPL |
| `Space r j` | Start an nREPL server and connect |
| `Space r d` | Documentation for the symbol |
| `Space r i` | Interrupt |
| `Space r l` | Show the REPL log |
| `Space r x` | Clear inline results |
| `Space r q` | Quit the REPL |
| `Space r k` | Restart the REPL or kernel |
| `Space r f` | Run the file (`:run`) |
| `Space r F` | Run the file with arguments |

See [REPLs](repls.md).

## Notebook (`Space n`)

| Key | Description |
|-----|-------------|
| `Space n r` | Run the cell |
| `Space n n` | Run the cell and go to the next |
| `Space n a` | Run all cells |
| `Space n A` | Run the cells above |
| `Space n B` | Run the cell and the cells below |
| `Space n o` | New cell below |
| `Space n O` | New cell above |
| `Space n d` | Delete the cell |
| `Space n j` | Move the cell down |
| `Space n K` | Move the cell up |
| `Space n m` | Toggle markdown / code |
| `Space n t` | Set the cell type |
| `Space n c` | Clear the cell output |
| `Space n C` | Clear all outputs |
| `Space n x` | Fold or unfold the output |
| `Space n v` | View the full output |
| `Space n I` | Open image outputs |
| `Space n i` | Interrupt the kernel |
| `Space n k` | Restart the kernel |
| `Space n e` | Export to `.py` |

See [Notebooks](notebooks.md).

## Data (`Space d`)

These work on the current column, or on the selection in visual mode.

| Key | Description |
|-----|-------------|
| `Space d t` | Toggle the table view |
| `Space d s` | Statistics (describe) |
| `Space d h` | Histogram |
| `Space d u` | Sum |
| `Space d m` | Mean |
| `Space d M` | Median |
| `Space d n` | Min |
| `Space d x` | Max |
| `Space d c` | Count |
| `Space d d` | Standard deviation |
| `Space d v` | Variance |
| `Space d r` | Correlation matrix |
| `Space d f` | Frequency table |
| `Space d g` | Group by |
| `Space d w` | Filter rows |
| `Space d C` | Computed column |
| `Space d T` | Transform column |

See [Data viewer](data-viewer.md).

## Git (`Space g`)

| Key | Description |
|-----|-------------|
| `Space g g` | Status |
| `Space g d` | Diff of the current file |
| `Space g D` | Diff of all files |
| `Space g l` | Log |
| `Space g L` | Log of the current file |
| `Space g b` | Blame |
| `Space g a` | Stage the current file |
| `Space g c` | Commit |
| `Space g p` | Push |
| `Space g P` | Pull (fast-forward) |

See [Git](git.md).

## Markdown (`Space m`)

Keys marked with V also work on a visual selection.

| Key | | Description |
|-----|---|-------------|
| `Space m b` / `i` / `c` / `s` | V | Toggle bold, italic, inline code, strikethrough |
| `Space m l` | V | Link: asks for the URL, the word or selection is the text; in a link, unlink |
| `Space m h` | | Cycle the heading level (none, `#` … `######`) |
| `Space m 1` … `6` | | Set the heading level (again: plain text) |
| `Space m x` | V | Toggle a task checkbox `- [ ]` / `- [x]` |
| `Space m -` | V | Toggle a bullet list |
| `Space m o` | V | Toggle a numbered list (renumbers) |
| `Space m q` | V | Toggle a `>` quote |
| `Space m C` | V | Fence as a code block (asks for the language); in one, remove the fences |
| `Space m t` | | Align the table under the cursor |
| `Space m T` | | Insert or refresh a table of contents |
| `Space m n` / `N` | | Next / previous heading |
| `Space m r` | | Toggle the rendered view (formulas, tables, alerts, …) |
| `Space m I` | | Open the image under the cursor |

The formatting keys work in markdown buffers only.

**Emphasis** (`b`, `i`, `c`, `s`) works on the cursor line:

- Anywhere in an emphasised span, markers included, the key acts on the
  whole span. Bold and italic switch the span's style: on
  `**two words**`, `Space m i` gives `*two words*` and `Space m b` gives
  `two words`; on `*two words*`, `Space m b` gives `**two words**`. On
  `***two words***` each key removes its own style. Inline code and
  strikethrough are removed. In nested spans the innermost one changes.
- Outside a span the key wraps the word under the cursor. With no word
  there it inserts an empty pair (`****`) and starts insert mode between
  them.
- To emphasise several words, select them in visual mode. A selection
  with markers outside a single span (such as all of `**a** and **b**`)
  is refused, so spans never get crossed; select plain text, or put the
  cursor in the span.
- With whole lines selected (`V`) each line is toggled on its own, after
  its list, heading or quote markers (empty items and trailing blanks,
  a hard line break, are kept).
- `__bold__` and `_italic_` are recognised too (not `_` inside a word, as
  in `snake_case`).

**Tables**: `Space m t` pads the cells of the table under the cursor so
its pipes line up; the delimiter row (`:--`, `--:`, `:-:`) sets each
column's alignment. As in GitHub markdown, write a pipe inside a cell
as `\|`, also in code.

**Links**: a URL with blanks or parentheses is written as `<url>`.
`Space m l` in a link (not an image) leaves only its text.

**Rendered view**: `Space m r` shows the buffer rendered, read-only.
It keeps the line of the cursor; `Space m r` (or `q`) goes back to the
editable text. In the view, `Space f y` (`:e`) and `:set autoreload`
act on the markdown file, and the view follows it. Every line of the
file stays one line of the view, so elements get no top or bottom
borders. Code and YAML front matter are left as written.
`examples/formulas.md` has an example of everything below.

- **Formulas** (`$…$` or `$ … $`, `$$…$$`, `\(…\)`, `\[…\]`, and math
  environments such as `\begin{equation}…\end{equation}` or `align`)
  read as Unicode text: `$\alpha_i \le \frac{1}{2}$` reads `αᵢ ≤ 1/2`.
  `$5 and $10` is not a formula. Everything stays on one line: no
  stacked fractions or matrices. `examples/formulas.md` shows what is
  supported.
- **Tables** (GitHub markdown: a header row, then a delimiter row with
  as many cells) are aligned with `│` between the cells and a `─┼─`
  rule under the header, following the column alignment. Formulas in
  cells are rendered; write a pipe in a cell as `\|`, also in a formula
  (`$\|x\|$`). A table ends at a blank line.
- **Quotes** (`> text`) read `┃ text`, one bar per level.
- **Alerts**, the GitHub ones (`> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`,
  `[!WARNING]`, `[!CAUTION]`) and Obsidian callouts (`> [!tip]- Title`),
  get a title line: `> [!WARNING]` reads `┃ ⚠ Warning`.
- **Horizontal rules** (`---`, `***`, `___`) read as a line of `─`.
  `---` under a line of text underlines a heading and is left as is.
- **Task items** read `☐ todo` and `☑ done` (`1. ☑ first` in a
  numbered list).

**Images**: `Space m I` opens a local image (relative to the file) or a
URL from `![alt](path)` or `<img src="…">` with the system viewer, like
`:nbimage` in notebooks. Only image files (`.png`, `.jpg`, `.svg`, …,
also behind a symlink) and `http(s)` links are opened, since the system
opener would also run a program or script.

## Toggles (`Space t`)

| Key | Description |
|-----|-------------|
| `Space t n` | Line numbers |
| `Space t r` | Relative line numbers |
| `Space t c` | Cursor line |
| `Space t h` | Search highlight |
| `Space t i` | Ignore case |
| `Space t e` | Expand tabs |
| `Space t w` | Line wrap (this buffer) |
| `Space t a` | Auto-reload (this buffer) |
| `Space t p` | Colored brackets (this buffer) |
| `Space t t` | Theme |
| `Space t T` | Transparent background |
| `Space t l` | Language |

## Help (`Space h`)

| Key | Description |
|-----|-------------|
| `Space h k` | Key bindings |
| `Space h c` | Commands |
| `Space h m` | Messages |
| `Space h l` | Languages |

## Quit (`Space q`)

| Key | Description |
|-----|-------------|
| `Space q q` | Quit all |
| `Space q Q` | Quit all, discarding changes |
| `Space q w` | Save all and quit |
