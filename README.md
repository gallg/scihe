<h1><img src="icon.png" alt="" height="64" align="middle"> Scihe</h1>

A small, self-contained modal editor for the terminal, made for working
with data and code without leaving the shell.

## Why

Most editors become projects: plugin managers, configuration frameworks,
language servers to install. Scihe goes the other way: inspired by
MicroEmacs' "one binary, no plugins" idea, it is a single static binary
that already knows how to edit (vim motions), read and explore data (CSV/TSV viewer with
statistics), run code interactively (REPLs, Jupyter notebooks) and talk to git. 

Start it on a remote machine over SSH and everything is there, no setup. And it
stays out of the way: no built-in terminal, no virtual environments to
manage: just use your own terminal multiplexer.

## Features

- **Editing** — vim normal/insert/visual modes, counts, operators, text
  objects, registers, macros, marks, undo/redo, splits, mouse
- **Fuzzy finders** — files, buffers, project grep, themes, commands
- **Syntax highlighting** — ~300 languages, ~70 themes (chroma)
- **Which-key popups** — press `Space` (or wait after any prefix) to see
  the available keys
- **REPLs** — Scheme, Clojure (nREPL), Python and any other REPL you
  configure; evaluate forms, cells or lines in the buffer with inline `=> result`
- **Notebooks** — open, edit and run `.ipynb` files, no Jupyter install needed.
- **Data viewer** — CSV/TSV as an aligned table: sort, filter, edit,
  mark cells, compute statistics or transform your data with built-in or custom functions.
- **Markdown preview** — live markdown preview with soft line wrap.
- **Git** — built-in, simple to use git support.
- **File explorer** — dired-style: mark, copy, paste, move, rename, delete, create.
- **Auto-reload** — files changed on disk are detected as you work;
  reload on demand, or enable `autoreload` to re-read unmodified
  buffers automatically

For more information please see the [Documentation](docs/index.md).
What changed in each version is in the [Changelog](docs/changelog.md).

Press `Space` or run `:help` inside the editor for every key and command.
The full documentation lives in the [docs folder](docs/index.md).
Configuration lives in `~/.config/scihe/config.toml` on Linux and
`~/Library/Application Support/scihe/config.toml` on macOS (declarative
only, no scripting — see `:config`).

## Install

Requires [Go](https://go.dev) ≥ 1.27:

```sh
git clone <repository-url> && cd scihe
go build -o scihe ./cmd/scihe
```

Then run it:

```sh
./scihe [+LINE] [file ...]   # files, directories, notebooks, CSV/TSV
```

## Requirements and dependencies

Works on Linux and macOS. No configuration or extra packages are required: the
editor is one binary and everything is implemented in Go.

Optional tools, used when present:

| Tool | Used for |
|------|----------|
| `python3` | notebooks, Python evaluation (`python` key sets which one) |
| `git` | the git interface |
| `chez` | Scheme REPL (the `scheme` key sets the binary) |
| `clojure` / `lein` | Clojure nREPL jack-in |
| `wl-copy` / `xclip` / `xsel` | system clipboard |
| `xdg-open` (`open` on macOS) | opening images and URLs |


## License

Scihe is released under the [MIT License](LICENSE).
