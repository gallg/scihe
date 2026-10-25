# Differences from vim

## Regular expressions

Search and `:s` use Go (RE2) regular expressions.

| Supported | Not supported |
|-----------|---------------|
| `(a\|b)` groups, `\d` and the other RE2 classes | Backtracking, lookaround |
| `\<` `\>` word boundaries | Multi-line matches |
| `\c` `\C` to force the case | `:s///c` (confirm) |

Replacements support `&`, `\0`–`\9`, `\n`, `\r` and `\u` `\l` `\U` `\L`
`\e`.

## Other differences

- **Buffers stay loaded** when their window closes, like `set hidden`.
  `:q` on the last window refuses to quit if a buffer has unsaved
  changes.
- **Soft wrap is per buffer.** `:set wrap!` (`Space t w`) changes only
  the current buffer. Table views and directory listings never wrap.
- **`gj` `gk`** move by screen row, `j` `k` by line.
- **Configuration is TOML only.** There is no vimscript or Lua, and
  mappings can't run shell commands, see
  [Configuration](configuration.md#key-remapping).
- **Files changed on disk** are detected, and can reload automatically,
  see [Auto-reload](configuration.md#auto-reload).
- **Notebooks** need no Jupyter install, see [Notebooks](notebooks.md).
- **`q` closes read-only buffers** (help, output, logs, the preview, the
  buffer list) instead of recording a macro.
- **Deletes can leave the register alone.** With
  `delete_to_register = false`, `d`, `c`, `x`, … no longer fill the
  register `p` pastes, see
  [Configuration](configuration.md#top-level-keys).
