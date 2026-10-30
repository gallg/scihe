# Git

`:git` (`Space g g`) opens the status of the current file's repository.
The status line always shows the current branch.

## Status keys

| Key | Description |
|-----|-------------|
| `s` | Stage |
| `u` | Unstage |
| `a` | Stage all |
| `U` | Unstage all (not during a merge with conflicts) |
| `x` | Discard changes |
| `c` | Commit |
| `<CR>` | Open the file |
| `d` | Diff |
| `D` | Diff of all files |
| `l` | Log |
| `p` | Push |
| `F` | Pull (fast-forward) |
| `R` | Refresh |
| `q` | Close |

## Commands

| Command | Description |
|---------|-------------|
| `:git diff` | Diff with per-file statistics |
| `:git log` | Commit history |
| `:git log %` | History of the current file |
| `:git blame` | Commit, author and date of each line |
| `:git commit` | Commit message buffer: `ZZ` commits, `ZQ` aborts |
| `:git add [FILE]` | Stage |
| `:git ARGS` | Any other git command, e.g. `:git switch -c topic` |

Other commands run in the background. Their output goes to
`*git-output*`.

## Notes

- Git never waits for input. Commands that need a password or an editor
  fail; use your shell for them.
- After a pull, checkout or discard, unmodified buffers reload.
- `:w` warns if the file changed since it was read.
