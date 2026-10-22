# Commands

Commands are typed after `:`. Many take a range first: `%` (whole
buffer), `.` (current line), `$` (last line), `'a` (a mark), `/pat/`,
or offsets like `.,+5`. A `!` after a command forces it.

## Files

| Command | Description |
|---------|-------------|
| `:w [FILE]` | Write |
| `:wq`, `:x` | Write and close |
| `:wa` | Write all |
| `:wqall`, `:xall` | Write all and quit |
| `:up[date]` | Write if modified |
| `:sav[eas] FILE` | Write under a new name |
| `:e[dit] FILE` | Edit a file |
| `:e` | Reload the current file |
| `:e!` | Reload, discarding changes |
| `:ene[w]` | New empty buffer |
| `:fin[d]` | Find a file |
| `:Explore [DIR]` | File explorer |

## Windows and buffers

| Command | Description |
|---------|-------------|
| `:q` | Close the window |
| `:qa` | Quit all |
| `:clo[se]` | Close the window |
| `:on[ly]` | Close the other windows |
| `:sp[lit]`, `:vs[plit]` | Split |
| `:new`, `:vne[w]` | Split with an empty buffer |
| `:lay[out] [next\|prev\|NAME]` | Change the window layout |
| `:b NAME` | Go to a buffer |
| `:bn`, `:bp` | Next, previous buffer |
| `:bd[elete]` | Delete the buffer |
| `:bda[ll]` | Delete all buffers (keeps modified ones) |
| `:bda[ll]!` | Delete all buffers, discarding changes |
| `:ls` | [Buffer list](file-explorer.md#buffer-list) |

## Editing

| Command | Description |
|---------|-------------|
| `:[range]s/pat/repl/[flags]` | Substitute (`&`, `&&` repeat) |
| `:[range]g/pat/ CMD` | Run CMD on matching lines |
| `:[range]v/pat/ CMD` | Run CMD on lines that don't match |
| `:[range]d`, `:y`, `:pu[t]` | Delete, yank, put lines |
| `:[range]m ADDR` | Move lines |
| `:[range]t ADDR`, `:co ADDR` | Copy lines |
| `:[range]j` | Join lines |
| `:[range]>`, `:<` | Indent, outdent |
| `:[range]sor[t]` | Sort lines |
| `:[range]norm[al] KEYS` | Run normal-mode keys on each line |
| `:r FILE` | Insert a file |
| `:r !CMD` | Insert the output of a shell command |
| `:[range]!CMD` | Filter lines through a shell command |
| `:u[ndo]`, `:red[o]` | Undo, redo |

## Information

| Command | Description |
|---------|-------------|
| `:help` | Every key and command |
| `:reg[isters]` | Registers |
| `:ma[rks]` | Marks |
| `:mes[sages]` | Message history |
| `:languages` | Supported languages |
| `:noh[lsearch]` | Clear the search highlight |

## Settings

| Command | Description |
|---------|-------------|
| `:set OPT`, `:set OPT!` | Set, toggle an option |
| `:setlo[cal] OPT` | Set an option for this buffer |
| `:setf[iletype] FT` | Set the language |
| `:theme NAME`, `:colo[rscheme] NAME` | Set the theme |
| `:conf[ig] [edit\|save\|path]` | Open, update, locate the config file |
| `:pwd`, `:cd DIR` | Show, change the directory |

See [Configuration](configuration.md).

## Running code

| Command | Description |
|---------|-------------|
| `:run [ARGS]` | Save and run the current file |
| `:run stop` | Stop the running program |
| `:repl start [NAME]` | Start or connect a REPL |
| `:repl restart` | Restart the REPL |
| `:repl connect [ADDR]` | Connect to a running nREPL |
| `:repl jack-in` | Start an nREPL server and connect |
| `:repl interrupt` | Interrupt |
| `:repl log` | Show the log |
| `:repl quit` | Quit the REPL |
| `:[range]eval [CODE]` | Evaluate lines or CODE |

`:run` picks the interpreter from the filetype:

| Filetype | Runs |
|----------|------|
| Python | `python3` |
| Scheme | `chez --script` |
| Clojure | `clojure -M` |
| Go | `go run` |
| Other | The file as a script |

Output goes to the `*run*` buffer. Quotes group words in ARGS:
`:run --name "Ada Lovelace"` passes two arguments. See [REPLs](repls.md).

## Notebooks

| Command | Description |
|---------|-------------|
| `:nbrun` | Run the cell |
| `:nbrunall` | Run all cells |
| `:nbclear [!]` | Clear the cell's outputs (`!`: all) |
| `:nbtype TYPE` | Set the cell type: `code`, `python`, `markdown`, `raw` |
| `:nboutput` | Show the full output |
| `:nbimage` | Open image outputs |
| `:nbinterrupt` | Interrupt the kernel |
| `:nbrestart` | Restart the kernel |
| `:nbexport` | Export as a percent-format `.py` |

See [Notebooks](notebooks.md).

## Data viewer

| Command | Description |
|---------|-------------|
| `:table [DELIM] [noheader]` | Toggle the table view |
| `:where [COL] EXPR` | Filter rows |
| `:stats [COLS]` | Describe columns |
| `:corr`, `:cov` | Correlation, covariance matrix |
| `:groupby KEYS [FN COL …]` | Group and aggregate |
| `:freq [COL]` | Frequency table |
| `:compute[!] NAME = EXPR` | Add a computed column |
| `:transform[!] KIND [COLS]` | Transform columns |
| `:recode[!] [MODE]` | Make cells numeric |
| `:mark [COL] EXPR` | Mark cells by a condition |
| `:column rename\|delete\|add` | Edit columns |

The single statistics (`:sum`, `:mean`, …) and the expression language
are in [Data viewer](data-viewer.md).

## Markdown

| Command | Description |
|---------|-------------|
| `:prev[iew]`, `:MarkdownPreview` | Toggle the live preview |

## Git

| Command | Description |
|---------|-------------|
| `:git` | Status |
| `:git diff` | Diff |
| `:git log [%\|FILE]` | Log |
| `:git blame` | Blame |
| `:git commit` | Commit |
| `:git add [FILE]` | Stage |
| `:git ARGS` | Run any other git command |

See [Git](git.md).
