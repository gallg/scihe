# File explorer

`Space f e`, `:Explore [DIR]` or opening a directory shows a dired-style
listing. Directories come first and `..` is at the top. The header line
shows the marked entries and the file clipboard.

## Keys

| Key | Description |
|-----|-------------|
| `<CR>`, `l` | Open the file or directory |
| `o` | Open in a vertical split |
| `-`, `h`, `^` | Parent directory |
| `m` | Mark (counts work: `3m`) |
| `u`, `U` | Unmark, unmark all |
| `t` | Toggle marks |
| `d` | Flag for deletion |
| `x` | Delete the flagged entries |
| `D` | Delete the marked entries |
| `R` | Rename or move |
| `C` | Copy |
| `y`, `X` | Copy, cut to the file clipboard |
| `p` | Paste into this directory |
| `w` | Yank the paths as text |
| `+` | New directory |
| `%` | New file |
| `.` | Show or hide dotfiles |
| `g` | Refresh |
| `?` | Show these keys |

Without marks, `D`, `R` and `C` act on the entry under the cursor.

## Safety

- Deleting asks first. Directories are deleted with their contents.
- Existing files are never overwritten.
- Buffers follow renamed files.
- Buffers of deleted files are closed, unless they have unsaved
  changes.
- No external program is run.

## Buffer list

`Space b l` (or `:ls`) lists the open buffers in the same way. `%` marks
the buffer you came from, `+` a modified buffer, `RO` a read-only one.

| Key | Description |
|-----|-------------|
| `<CR>` | Switch to the buffer |
| `o` | Open in a vertical split |
| `m`, `u`, `U`, `t` | Mark, unmark, unmark all, toggle |
| `d` then `x` | Close the flagged buffers |
| `D` | Close the marked buffers |
| `R` | Rename the buffer's file |
| `C` | Copy the buffer's content to a new file |
| `S` | Save |
| `y`, `X` | Copy, cut the files to the file clipboard |
| `p` | Open the file clipboard's files |
| `g` | Refresh |
| `?` | Show these keys |

Closing a buffer with unsaved changes asks first. `Space b b` is the
quick fuzzy switcher.
