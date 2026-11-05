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
| `internal/editor` | Modes, keys, commands, pickers, git, table and markdown views, typeset formulas |
| `internal/buffer` | Text buffer and file I/O |
| `internal/keymap` | Key parsing and the which-key tree |
| `internal/syntax` | Syntax highlighting and themes |
| `internal/config` | Loading and checking `config.toml` |
| `internal/repl` | Form extraction and REPL sessions |
| `internal/notebook` | `.ipynb` files, kernel and `driver.py` |
| `internal/table` | Table parsing, statistics, expressions |
| `internal/clipboard` | System clipboard |

## Formulas

Formulas are parsed once (`mdformula.go`, which also lays them out as
text) and typeset from the same tree when the terminal shows images:

| File | Contents |
|------|----------|
| `mathfont.go` | The built-in font and its OpenType `MATH` table (constants, larger variants, glyph assemblies) |
| `mathtype.go` | TeX's layout rules in ems, and drawing into a bitmap |
| `termgfx.go` | The kitty graphics protocol: detection, tmux, sending images, placeholders and direct placement, the cache of formulas |

`golang.org/x/image` reads and draws the font. The font is
`internal/editor/fonts/scihe-math.otf`, Latin Modern Math with its names
changed for its license; how it was made is in
[`README-Scihe-Math.md`](../internal/editor/fonts/README-Scihe-Math.md).
Keep those files, the manifest and the license together, and update them
if the font changes.

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
