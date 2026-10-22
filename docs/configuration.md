# Configuration

Scihe reads `~/.config/scihe/config.toml`. Another file can be given
with `$SCIHE_CONFIG` or `-config FILE`. The file is plain TOML, with no
scripting.

| Command | Description |
|---------|-------------|
| `:config` | Open the config file |
| `:config save` | Save the current theme and transparency |
| `:config path` | Show which file is used |

Unknown keys and invalid values are reported at startup.

## Example

All values below are the defaults.

```toml
theme = "catppuccin-mocha"
transparent = false
leader = "<Space>"
whichkey_delay_ms = 300
mouse = true
python = "python3"
scheme = "chez"
clojure_jack_in = "clojure -Sdeps '{:deps {nrepl/nrepl {:mvn/version \"1.3.0\"}}}' -M -m nrepl.cmdline"
notebook_max_output_lines = 30

[options]
tabstop = 4
shiftwidth = 4
expandtab = true
number = true
relativenumber = false
cursorline = true
scrolloff = 4
ignorecase = true
smartcase = true
hlsearch = true
incsearch = true
autoindent = true
wrap = false
linebreak = true
autoreload = false
```

## Top-level keys

| Key | Description |
|-----|-------------|
| `theme` | Color theme (`Space t t` lists them) |
| `transparent` | Use the terminal's own background |
| `leader` | The leader key |
| `whichkey_delay_ms` | Delay before the key popup appears |
| `mouse` | Enable the mouse |
| `python` | Python interpreter, looked up on `PATH` |
| `scheme` | Scheme REPL program |
| `clojure_jack_in` | Command that starts an nREPL server |
| `notebook_max_output_lines` | Output lines shown per notebook cell |

Choosing a theme (`Space t t`) or toggling transparency (`Space t T`)
updates the file and keeps your comments.

There is no font setting: the terminal draws the text, so the font and
its size are set in your terminal emulator.

## Options

| Option | Description |
|--------|-------------|
| `tabstop` | Width of a tab |
| `shiftwidth` | Width of one indent level |
| `expandtab` | Indent with spaces |
| `number` | Show line numbers |
| `relativenumber` | Show relative line numbers |
| `cursorline` | Highlight the cursor line |
| `scrolloff` | Lines kept visible around the cursor |
| `ignorecase` | Search ignores case |
| `smartcase` | ...unless the pattern has uppercase letters |
| `hlsearch` | Highlight search matches |
| `incsearch` | Show matches while typing the pattern |
| `autoindent` | Keep the indent of the previous line |
| `wrap` | Soft-wrap long lines |
| `linebreak` | When wrapping, break at blanks, not mid-word |
| `autoreload` | Reload files changed on disk |

`:set` changes an option during the session, as in vim. `wrap`,
`linebreak` and `autoreload` are per buffer: `:set wrap!` changes only
the current buffer.

## Per-filetype options

A `[filetype.NAME]` table overrides `tabstop`, `shiftwidth`,
`expandtab`, `wrap`, `linebreak` and `autoreload` for one language.
`:languages` lists the names.

```toml
[filetype.markdown]
wrap = true
```

Built-in filetype defaults:

| Filetype | Defaults |
|----------|----------|
| `go` | `tabstop = 4`, `shiftwidth = 4`, `expandtab = false` |
| `makefile` | `expandtab = false` |
| `clojure`, `scheme`, `yaml`, `markdown` | `tabstop = 2`, `shiftwidth = 2` |

## Auto-reload

Scihe checks the files of open buffers about once a second.

| Situation | What happens |
|-----------|--------------|
| `autoreload` off (default) | A message; reload with `Space f y` |
| `autoreload` on | The buffer reloads |
| Markdown with its preview open | The buffer reloads |
| Unsaved changes | Never reloaded; a warning is shown |
| File deleted | A message; `:w` writes it again |
| Notebooks | Never reloaded |

Each change is reported once. `Space t a` toggles auto-reload for the
current buffer.

## Key remapping

Mappings go in `[keys.normal]`, `[keys.insert]` and `[keys.visual]`.
A value starting with `:` runs a command. Any other value is replayed
as keys.

```toml
[keys.normal]
"<leader>w" = ":w"
"Q" = "@q"

[keys.insert]
"jk" = "<Esc>"
```

Mappings can only control the editor. They cannot run shell commands or
start other programs:

- `:!`, `:r !` and `:w !` are rejected when the file is loaded.
- `:run`, `gx`, `:nbimage` and git commands other than the built-in
  views are refused when the mapping runs.

`clojure_jack_in` must start with `clojure`, `clj`, `lein`, `bb` or
`boot`.

## REPLs

REPLs are configured in `[repl.NAME]` tables, see [REPLs](repls.md).
