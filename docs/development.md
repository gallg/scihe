# Development

Scihe is written in Go. The entry point is `cmd/scihe/main.go`; all
other code is in `internal/`.

## Build and test

```sh
go build -o scihe ./cmd/scihe
go test ./...
go test -race ./...
```

Some tests use `python3` and `chez` when they are installed.

## Packages

| Package | Contents |
|---------|----------|
| `internal/editor` | Modes, keys, commands, pickers, git and table views |
| `internal/buffer` | Text buffer and file I/O |
| `internal/keymap` | Key parsing and the which-key tree |
| `internal/syntax` | Syntax highlighting and themes |
| `internal/config` | Loading and checking `config.toml` |
| `internal/repl` | Form extraction and REPL sessions |
| `internal/notebook` | `.ipynb` files, kernel and `driver.py` |
| `internal/table` | Table parsing, statistics, expressions |
| `internal/markdown` | Markdown preview |
| `internal/clipboard` | System clipboard |

## Integration tests

Integration tests use a `.venv` at the repository root when it exists:

```sh
uv venv .venv
uv pip install --python .venv/bin/python jupyter nbformat matplotlib pandas
```

It is used to check saved notebooks with `nbformat` and to test
matplotlib figures.

## REPL notes

The default prompts for Racket and SBCL (`prompt_regex`) have not been
tested against those programs.
