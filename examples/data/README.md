# Test data for the table view

Open a file with `./scihe examples/data/<file>` and press `SPC d t` (or `:table`).

| File | Format | What it exercises |
|------|--------|-------------------|
| `sales.csv` | comma, 240 rows × 9 | dates, text and numeric columns; `NA` and empty cells; quoted cells with commas, `""` quotes and a line break |
| `measurements.tsv` | tab, 500 rows × 7 | numbers only per group: negatives, scientific notation (`5.5e-04`), good for `:hist` and `:stats` |
| `european.csv` | semicolon, 8 rows × 5 | delimiter guessing; decimal commas (`891,1`) are shown as text, not numbers |
| `events.psv` | pipe, 120 rows × 4, **no header** | open with `:table noheader`; columns are named `1`–`4` |
| `edge_cases.csv` | comma, UTF-8 with BOM | accents, CJK, Arabic, emoji; a trailing empty cell; short and long (ragged) rows; padded cells; `1e308` / `-1e308` |

Things to try:

- `sales.csv`:
  - `l` to `units`, then `:sum`, `:mean`, `:median`;
  - `S` sorts descending, with the missing values last;
  - `=` describes the column; `:stats *` describes all columns.
- `sales.csv`:
  - `f` on `region` with `=north`;
  - then `:where revenue > 50`;
  - `:sum revenue` now sums only the filtered rows; `F` clears the filters.
- `sales.csv`: `<C-v>`, select a few rows across `units`…`revenue`, then `SPC d u`. Add `!` for per-row sums (`:'<,'>sum!`).
- `measurements.tsv`:
  - `:hist` on `temp_c`;
  - `:hist 30` for more bins;
  - `:hist` on `group` shows value counts;
  - `:where group = control`, then `:mean temp_c` (compare with the other groups).
- `edge_cases.csv`: columns stay aligned with wide characters; `$` reaches the trailing empty cell; `<CR>` shows a full cell.
- Any file: `q` goes back to the text on the same record; edit it, and `:table` shows the change.
- `european.csv`: `3l` to `Fläche_km2`, then `:recode` adds `Fläche_km2_num` with real numbers (decimal commas). Then `:w` asks before writing it to the file; `ZQ` discards.
- `sales.csv`:
  - `l` to `region`, then `:recode codes` adds `region_code` (1–4) and shows the mapping in `*recode*`;
  - `*` on a cell marks every cell with that value; `:mark units > 30` marks cells by a condition; `:mean` then uses only the marked cells.
- Any file:
  - `c` selects the column, `r` the row, `a` everything; arrows grow or shrink the selection;
  - `e` enters edit mode: `i` edits a cell, `dd` deletes a row, `o` adds one, `u` undoes; `:w` applies after asking.
