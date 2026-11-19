# REPLs

Scihe sends code to a running REPL and shows the result after the code:
`=> value` or `!! error`, as CIDER does. The value is what the code
returns, not what it prints: `(println "Hello")` shows `=> nil`, and
`Hello` goes to the log. Python's `print("Hello")` shows `=> None`. Code
that returns no value (a Python statement, a Scheme `(void)`) shows
`=> ✓`. There is one session per language, so all `.py` buffers share
the same Python session.

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
| `Space r l` | Show the log of code, output and errors |
| `Space r x` | Clear the inline results |
| `Space r i` | Interrupt |
| `Space r q` | Quit the REPL |
| `Space r k` | Restart the REPL or kernel |
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
| `init` | Code sent once at start that frames values and errors (`{nonce}` is replaced) |

| Kind | Description |
|------|-------------|
| `subprocess` | Any REPL program, through a pipe |
| `chez` | Chez Scheme (the `scheme` entry) |
| `nrepl` | nREPL (the `clojure` entry) |
| `kernel` | Python (the `python` entry) |

With `subprocess`, Scihe sends a short *sentinel* expression after each
evaluation. It prints a marker that shows where the output ends.
Interrupting sends `SIGINT`.

Without `init`, the last output line is the inline result, as the REPL
prints values and output alike, and errors are mixed with the output.
With `init`, the REPL prints each value and error on a line of its own
(`\x1d` is the character with code 29):

```text
\x1dSCIHE {nonce} value "<value as printed, in a Scheme string literal>"
\x1dSCIHE {nonce} err "<message>"
```

The `racket` and `common lisp` entries come with an `init`, used while
their `command` is the default one. Racket prints values with
`current-print` and errors with `error-display-handler`, and shows no
prompt. SBCL runs with `--noprint` and evaluates each form through
`scihe-run`: an error is reported instead of entering the debugger. For
restarts and the debugger, run SBCL in a terminal.

If a REPL's prompts aren't removed, adjust `command` or `prompt_regex`.
