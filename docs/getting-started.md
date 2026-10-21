# Getting started

## Requirements

- Linux or macOS
- [Go](https://go.dev) 1.27 or newer, to build

Optional tools, used when present:

| Tool | Used for |
|------|----------|
| `python3` | Notebooks and Python evaluation |
| `git` | The git interface |
| `chez` | Scheme REPL |
| `clojure` or `lein` | Clojure nREPL |
| `wl-copy`, `xclip` or `xsel` | System clipboard |
| `xdg-open` (`open` on macOS) | Opening images and URLs |

## Install

Build from a checkout:

```sh
git clone <repository-url> && cd scihe
go build -o scihe ./cmd/scihe
```

## Run

```sh
./scihe [+LINE] [file ...]
```

Arguments can be files, directories, notebooks or CSV/TSV files.
`+LINE` puts the cursor on that line.

| Option | Description |
|--------|-------------|
| `-config FILE` | Use another configuration file |
| `-version` | Print the version and exit |

## First steps

Scihe is modal, like vim. It starts in normal mode, where keys are
commands. Press `i` to type text and `<Esc>` to go back.

| Key | Description |
|-----|-------------|
| `Space` | Show the available keys |
| `Space Space` | Find a file |
| `:w` | Save |
| `:q` | Close the window |
| `:help` | List every key and command |

Scihe has no built-in terminal and does not manage virtual
environments. Use your terminal multiplexer for a shell. To use a
venv, activate it before starting `scihe`.

Next: [Usage](usage.md), [Keymap](keymap.md).
