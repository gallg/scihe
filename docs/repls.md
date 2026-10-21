# REPLs

Scihe sends code to a running REPL and shows the result after the code:
`=> value` or `!! error`. There is one session per language, so all
`.py` buffers share the same Python session.

## Evaluating

| Key | Evaluates |
|-----|-----------|
| `Space r r` | The form (Lisp) or line (Python) at the cursor; the selection in visual mode |
| `Space r t` | The top-level form, `# %%` cell or Python block |
| `Space r b` | The whole buffer |
| `Space r e` + motion | A motion or text object, e.g. `Space r e ip` |
| `:eval [CODE]` | A range of lines, or CODE |

| Key | Description |
|-----|-------------|
| `Space r l` | Show the log of code and results |
| `Space r x` | Clear the inline results |
| `Space r i` | Interrupt |
| `Space r q` | Quit the REPL |
| `:repl restart` | Start a fresh REPL |

## Languages

| Name | Files | Runs |
|------|-------|------|
| `scheme` | `.scm` `.ss` `.sls` `.sps` `.sld` | Chez Scheme |
| `clojure` | `.clj` `.cljs` `.cljc` `.bb` `.edn` | An nREPL connection |
| `python` | `.py` | The configured `python` |
| `racket` | `.rkt` | `racket -i` |
| `common lisp` | `.lisp` `.cl` | `sbcl --noinform` |

## Clojure

| Command | Description |
|---------|-------------|
| `:repl start` | Connect using the project's `.nrepl-port` file |
| `:repl connect [HOST:]PORT` | Connect to a running server |
| `:repl jack-in` (`Space r j`) | Start a server and connect |

The project is found by `deps.edn`, `project.clj`, `bb.edn` or
`shadow-cljs.edn`. Jack-in runs `clojure_jack_in` from the
[configuration](configuration.md), or `lein repl :headless` in a
`project.clj` project. The namespace comes from the buffer's `(ns …)`
form.

## Configuring REPLs

Each language is a `[repl.NAME]` table. The built-in languages need no
configuration. A table changes only the keys it sets; a new name adds a
language.

```toml
[repl.guile]
filetypes = ["scheme"]
command = "guile -q"
reader = "scheme"
kind = "subprocess"
prompt_regex = '^(scheme@\(guile-user\)> )+'
```

| Key | Description |
|-----|-------------|
| `filetypes` | Filetypes it serves (`:languages` lists them) |
| `command` | Program and arguments, run without a shell |
| `reader` | How code is split: `scheme`, `clojure` or `python` |
| `kind` | Connection type, see below |
| `prompt_regex` | Prompt text to remove from the output |
| `sentinel` | Code that prints the end marker (`{id}` is replaced) |

| Kind | Description |
|------|-------------|
| `subprocess` | Any REPL program, through a pipe |
| `chez` | Chez Scheme (the `scheme` entry) |
| `nrepl` | nREPL (the `clojure` entry) |
| `kernel` | Python (the `python` entry) |

With `subprocess`, Scihe sends a short *sentinel* expression after each
evaluation. It prints a marker that shows where the output ends. The
last output line is the inline result. Errors are mixed with the output,
and interrupting sends `SIGINT`.

If the Racket or SBCL prompts aren't removed, adjust `command` or
`prompt_regex`.
