# Usage

## Modes

| Mode | Enter with | Purpose |
|------|------------|---------|
| Normal | `<Esc>` | Move and run commands |
| Insert | `i` `a` `I` `A` `o` `O` | Type text |
| Replace | `R` | Overwrite text |
| Visual | `v` `V` `<C-v>` | Select characters, lines or a block |

In visual mode, an operator acts on the selection.

## Motions and operators

An operator acts from the cursor to the end of a motion: `dw` deletes a
word. Counts work everywhere: `3j`, `2dw`.

| Operator | Description |
|----------|-------------|
| `d` | Delete |
| `c` | Change |
| `y` | Yank (copy) |
| `>` `<` | Indent, outdent |
| `gu` `gU` `g~` | Lowercase, uppercase, toggle case |
| `gc` | Comment |
| `gq` | Reflow |

Text objects:

| Object | Selects |
|--------|---------|
| `iw` `aw` | Word |
| `i(` `a(` | Brackets |
| `i"` `a"` | Quotes |
| `ip` `ap` | Paragraph |
| `ie` `ae` | Entire buffer |
| `ic` `ac` | Notebook cell |
| `if` `af` | Lisp form |

Other normal-mode keys:

| Key | Description |
|-----|-------------|
| `.` | Repeat the last change |
| `p` `P` | Paste after, before |
| `u` `<C-r>` | Undo, redo |
| `q` `@` | Record, replay a macro |
| `m` `'` | Set, jump to a mark |
| `<C-a>` `<C-x>` | Increment, decrement a number |
| `x` `s` `r` `~` | Delete, change, replace a character, toggle case |
| `ZZ` `ZQ` | Save and quit, quit |

## Registers

| Register | Contents |
|----------|----------|
| `"a`–`"z` | Named registers (uppercase appends) |
| `"0`–`"9` | Yank history |
| `"_` | Black hole |
| `"+` | System clipboard |

Results of some commands are also yanked. For example, a statistic
from the data viewer can be pasted with `p`.

## Search

| Key | Description |
|-----|-------------|
| `/` `?` | Search forward, backward |
| `n` `N` | Next, previous match |
| `*` | Search the word under the cursor |
| `:s/pat/repl/` | Substitute in a range (`:%s` for the whole buffer) |

Patterns are Go regular expressions, see
[Differences from vim](vim-differences.md).

## Pickers

| Key | Picks |
|-----|-------|
| `Space Space`, `Space f f` | Files |
| `Space ,`, `Space b b` | Buffers |
| `Space /` | Project grep |
| `Space f r` | Recent files |
| `Space :` | Command history |
| `Space h c` | Commands |
| `Space t t` | Themes |

## Which-key

Press `Space` and wait, or wait after a prefix such as `g`, `z` or
`<C-w>`. A popup lists the keys you can press next. The delay is set by
`whichkey_delay_ms` in the [configuration](configuration.md).

## Windows

| Key | Description |
|-----|-------------|
| `:split`, `:vsplit` | Split the window |
| `<C-w><Space>` | Next layout |
| `<C-w>=` | Equalize sizes |
| `<C-w>` + `<C-h>` `<C-j>` `<C-k>` `<C-l>` or arrows | Move the window border; then `h` `j` `k` `l` keep resizing |
| `Space w r` | Resize mode |

## Files

| Opening | Shows |
|---------|-------|
| A directory | The [file explorer](file-explorer.md) |
| A `.csv` or `.tsv` file | The [data viewer](data-viewer.md) |
| An `.ipynb` file | A [notebook](notebooks.md) |

`Space f y` (or `:e`) reloads the current file from disk; `Space f Y`
(or `:e!`) also discards your changes. Changes made by other programs
are detected, see [Auto-reload](configuration.md#auto-reload).

## Read-only buffers

Help, messages, statistics, command output, REPL logs, git diffs, the
buffer list and the markdown preview can't be edited. Press `q` to
close them.
