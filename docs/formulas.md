# Formulas

Formulas written in LaTeX, in markdown files (`$…$`, `$$…$$`, `\begin{…}`
environments) and in notebook outputs (sympy, `IPython.display.Math`,
`Latex`, `Markdown`), are typeset in the rendered view (`Space m r`) and in
the `Out [n]` boxes. The syntax is described under
[Markdown in the keymap](keymap.md#markdown-space-m); `examples/formulas.md`
has an example of everything.

They are shown in one of two ways:

- **Typeset**, as LaTeX does it: TeX's layout rules with a real math font,
  drawn as images in the text. Scripts and limits are smaller, fractions,
  roots and growing brackets are drawn, a line is as tall as its formula.
  This needs a terminal that shows images (below).
- **As text** everywhere else: laid out in two dimensions with Unicode
  characters, scripts a row above or below.

## Terminals

| Terminal | Typeset formulas | How |
|----------|------------------|-----|
| Ghostty | yes, also inside tmux | kitty graphics protocol, Unicode placeholders |
| kitty | yes, also inside tmux | kitty graphics protocol, Unicode placeholders |
| Konsole | yes, outside tmux only | kitty graphics protocol, images placed on their cells |
| others | no: text | |

The terminal is recognised from its environment variables: `TERM_PROGRAM=ghostty`,
`TERM=xterm-ghostty` or `GHOSTTY_RESOURCES_DIR` (Ghostty),
`TERM=xterm-kitty` or `KITTY_WINDOW_ID` (kitty), `KONSOLE_VERSION`
(Konsole). Over SSH usually only `TERM` reaches the remote machine, so
Ghostty and kitty are recognised there when their `TERM` is kept, and
Konsole is not.

Ghostty and kitty also need 24-bit colour (`COLORTERM=truecolor`, which
both set): each image is identified by the colour of its cells.

`math_images = false` in `config.toml` keeps formulas as text in every
terminal ([Configuration](configuration.md)).

## tmux

Images reach the terminal through tmux's passthrough. Nothing needs to be
set up:

- When `allow-passthrough` is off, scihe turns it on **for its own pane
  only** while it runs, and sets it back as it was when it exits (also
  after a crash). To have it on everywhere, put
  `set -g allow-passthrough on` in `tmux.conf`. The option exists from
  tmux 3.3 on.
- tmux must send RGB colours to the terminal (`RGB` among
  `tmux display -p '#{client_termfeatures}'`). tmux reads it from the
  terminal's description (it does for Ghostty); otherwise add `set -as terminal-features ",xterm-ghostty:RGB"`
  (with your terminal's `TERM`).
- Konsole places images at a position on the screen, which tmux can't
  follow: inside tmux, formulas stay text there.

When scihe can't use tmux's passthrough or RGB colours it says so in the
message line, and formulas stay text.

Several scihe in the panes of one tmux window keep their images apart.
If the terminal lost the images (a tmux session attached from another
terminal window), `Ctrl-l` sends them again. After `Ctrl-z` they are sent
again by themselves.

## Size and spacing

- `math_scale` (default `1.2`, from `0.5` to `3`) is the size of formulas,
  times the size of the text. Scripts never get smaller than 0.6 of it.
- An inline formula fits in its line: it is made smaller, down to the size
  of the text (or `math_scale`, when smaller), so that it does. Only a
  formula taller than that, such as a `\dfrac` or a matrix, makes its line
  taller. A display formula takes the rows it needs, at `math_scale`.
- Unicode super- and subscripts (`H₂O`, `mc²`), `<sup>`, `<sub>` and
  footnote marks in the text are set the same way, on their line.
- Rows of different formulas whose ink would meet over the boundary
  between them (consecutive lines, list items) are set off by a blank
  row, as TeX's `\lineskip`: a formula taller than a line of text, such
  as a fraction, a root with an inner formula or a sum with limits,
  fills the cells it takes. Formulas that leave as much room around
  them as text does, like a letter with a superscript or a subscript,
  are not set apart. The bars of tables and quotes go on through the
  blank row. In notebooks a blank row sets off consecutive formulas.
- The size of a cell in pixels is asked from the terminal; when it is
  smaller than 24 pixels high the images are drawn at twice the size, for
  Retina displays.

## When a formula is shown as text

Even in a terminal that shows images, a formula is shown as text when:

- the font has no character for it (Chinese in `\text{…}`, for example);
- it is wider than the window: it is laid out as text then, and with
  `wrap` written on one line when that is still too wide.

## Copying

The rendered view is read-only. Yanking from it (`yy`, `Space y`) copies a
typeset formula as text on one line (`E = mc²`, `(a + b)/2`). `:nboutput`
shows a cell's outputs as text in a buffer.

## The font

The math font is built in: **Scihe Math**, Latin Modern Math (the OpenType
version of TeX's Computer Modern) with its font names changed. It is not
under scihe's MIT License but under the GUST Font License; its authors,
what was changed and where to get the original are in
[`internal/editor/fonts/README-Scihe-Math.md`](../internal/editor/fonts/README-Scihe-Math.md),
its files in the
[manifest](../internal/editor/fonts/MANIFEST-Scihe-Math.md).
