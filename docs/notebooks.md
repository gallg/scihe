# Notebooks

Scihe opens and edits Jupyter `.ipynb` files. Jupyter doesn't need to be
installed: cells run with the configured `python` through a small
built-in driver. Only Python notebooks are supported.

## The view

- Cells are drawn as boxes: `In [n]` for code, `Markdown`, `Raw`.
- Code cells show their output in an `Out [n]` box below.
- Figures are saved in the notebook and shown as a placeholder.
  `:nbimage` opens them.
- While you type in a cell, its raw text is shown, with markers like
  `# %% [code]`.

## Keys and commands

| Key | Command | Description |
|-----|---------|-------------|
| `Space n r` | `:nbrun` | Run the cell |
| `Space n n` | | Run the cell and go to the next |
| `Space n a` | `:nbrunall` | Run all cells |
| `]c`, `[c` | | Next, previous cell |
| `ic`, `ac` | | Cell text objects |
| `Space n o`, `Space n O` | | New cell below, above |
| `Space n d` | | Delete the cell |
| `Space n m` | | Toggle markdown / code |
| `Space n t` | `:nbtype TYPE` | Set the type: `code`, `python`, `markdown`, `raw` |
| `Space n c`, `Space n C` | `:nbclear`, `:nbclear!` | Clear the outputs of the cell, of all cells |
| `Space n v` | `:nboutput` | Show the full output |
| `Space n I` | `:nbimage` | Open image outputs |
| `Space n i` | `:nbinterrupt` | Interrupt the kernel |
| `Space n k` | `:nbrestart` | Restart the kernel |
| `Space n e` | `:nbexport` | Export as a percent-format `.py` |

A cell that stops being code loses its outputs. The full list of
notebook keys is in the [keymap](keymap.md#notebook-space-n).

## Notebooks and `.py` files

A `.py` file with `# %%` markers works like a notebook: `Space r t`
runs the current cell. All `.py` buffers share one Python session, see
[REPLs](repls.md). Each notebook has its own kernel.
