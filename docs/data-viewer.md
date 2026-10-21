# Data viewer

The data viewer shows a delimited text file as an aligned table. It
opens for `.csv`, `.tsv`, `.tab` and `.psv` files, or any buffer with
`:table` (`Space d t`). Spreadsheet formats (xls, xlsx, ods) are not
supported.

| Command | Description |
|---------|-------------|
| `:table` | Toggle the table view, guessing the delimiter |
| `:table ;`, `:table tab` | Use a given delimiter |
| `:table noheader` | The file has no header row |
| `q` | Back to the text, on the same record |

Edits made to the text show up the next time the table is shown.

## Moving

| Key | Description |
|-----|-------------|
| `h` `l`, `b` `w`, `<Tab>` | Previous, next column |
| `0` `$` | First, last column |
| `<CR>` | Show the full cell value |

## Selecting

| Key | Description |
|-----|-------------|
| `r` | Select the row |
| `c` | Select the column |
| `a` | Select everything |
| `h` `j` `k` `l` | Grow or shrink the selection |
| `v`, `<C-v>` | Select cells |
| `V` | Select rows |

## Marking

| Key | Description |
|-----|-------------|
| `m` | Mark the cell (visual mode: the selection) |
| `*` | Mark every cell with the cursor's value |
| `M` | Invert the marks in the column |
| `U` | Clear all marks |
| `:mark [COL] EXPR` | Mark cells by a condition, e.g. `:mark units > 30` |

## Sorting and filtering

These change only the view, never the file.

| Key | Description |
|-----|-------------|
| `s`, `S` | Sort by the column, ascending, descending |
| `gs`, `gS` | Add the column as the next sort level |
| `o` | Back to the file order |
| `f` | Filter the column |
| `F` | Clear the filters |
| `:sort apply` | Write the shown order into the file |

Missing values sort last. The header shows the sort levels (▲1 ▼2 …).

`:where [COL] EXPR` (`Space d w`) filters rows:

| Filter | Keeps rows where the cell |
|--------|---------------------------|
| `>10`, `<=2.5` | Compares as a number |
| `=north`, `!=x` | Equals, differs from the text |
| `~regex`, `!~regex` | Matches, doesn't match |
| `empty`, `!empty` | Is empty, is not empty |
| other text | Contains the text |

`:where` with no argument clears the filter.

## What a command works on

Commands use the first of these that exists:

1. the visual selection;
2. the marked cells;
3. a line range;
4. the cursor column.

## Statistics

| Command | Key | Description |
|---------|-----|-------------|
| `:stats [COLS\|*]` | `Space d s`, `=` | Describe the columns |
| `:hist [BINS]` | `Space d h` | Histogram (text columns: value counts) |
| `:freq [COL]` | `Space d f` | Frequency table with % and cumulative % |
| `:corr [METHOD] [COLS\|*]` | `Space d r` | Correlation matrix |
| `:cov [COLS\|*]` | | Covariance matrix |
| `:groupby KEYS [FN COL …]` | `Space d g` | Group and aggregate |
| `:quantile P [COLS]` | | Quantile, P as `0.9` or `90` |

`:stats` shows count, missing, sum, mean, standard error, 95%
confidence interval, std, coefficient of variation, min, quartiles,
max, range, IQR, skewness, kurtosis, unique values and the most
common value.

`:corr` takes `pearson` (default), `spearman` or `kendall`. Stars mark
p < 0.05 (`*`), 0.01 (`**`) and 0.001 (`***`).

`:groupby region sum units mean price` gives one row per region. The
result opens as a new table; `:w FILE` saves it.

### Single statistics

Each gives one value per column. With `!` it gives one value per row.
The result is yanked, so `p` pastes it.

| Command | Key | Description |
|---------|-----|-------------|
| `:sum` | `Space d u` | Sum |
| `:mean` | `Space d m` | Mean |
| `:median` | `Space d M` | Median |
| `:min`, `:max` | `Space d n`, `Space d x` | Minimum, maximum |
| `:count` | `Space d c` | Number of values |
| `:std` | `Space d d` | Standard deviation |
| `:var` | `Space d v` | Variance |
| `:range`, `:iqr` | | Range, interquartile range |
| `:sem` | | Standard error of the mean |
| `:cv` | | Coefficient of variation |
| `:skew`, `:kurt` | | Skewness, excess kurtosis |

## Computed columns

`:compute NAME = EXPR` (`Space d C`) adds a column computed for every
row. `:compute!` replaces an existing column.

```
:compute revenue = units * price
:compute z = (price - mean(price)) / std(price)
```

## Transforming columns

`:transform KIND [COLS]` (`Space d T`) adds a `NAME_KIND` column next to
each source column. `:transform!` changes the column in place.

| Kind | Result |
|------|--------|
| `z` | `(x - mean) / std` |
| `minmax` | `(x - min) / (max - min)` |
| `pct` | `x / sum * 100` |
| `log` `log10` `log2` `sqrt` `square` `exp` `abs` | The function |
| `rank` `cumsum` `diff` | Rank, running sum, difference to the previous row |

Instead of a kind you can write an expression, where `_` is the
column: `:transform _ / 1000 price`.

## Expressions

Used by `:compute` and `:transform`.

| Element | Examples |
|---------|----------|
| Numbers | `3`, `2.5`, `1e-3`, `pi`, `e` |
| Text | `"north"`, `'north'` |
| Columns | `price`, `` `unit price` ``, `[unit price]`, `$3` |
| Arithmetic | `+ - * / %`, `^` or `**` for powers |
| Comparisons | `< <= > >= = == !=` (give 1 or 0) |
| Logic | `and or not`, `&& \|\| !` |

Functions of each row:

| Function | Description |
|----------|-------------|
| `abs` `sqrt` `square` `exp` | Basic math |
| `log` `ln` `log10` `log2` | Logarithms |
| `floor` `ceil` `round(x[, digits])` | Rounding |
| `pow(x, y)` | x to the power y |
| `min(a, b, …)`, `max(a, b, …)` | Smallest, largest argument |
| `if(cond, a, b)` | a if cond is true, else b |
| `isna(x)` | 1 if x is missing |
| `coalesce(a, b, …)` | The first value that isn't missing |

Aggregates, one value over all rows:

| Function | Description |
|----------|-------------|
| `mean` `median` `sum` `count` | Basic aggregates |
| `min(x)`, `max(x)` | With one argument |
| `std` `var` `sem` | Spread |
| `range` `iqr` `quantile(x, p)` | Ranges and quantiles |
| `skew` `kurt` | Shape |
| `n()` | Number of rows |

Window functions, in row order:

| Function | Description |
|----------|-------------|
| `rank(x)` | Rank |
| `cumsum(x)` | Running sum |
| `diff(x)` | Difference to the previous row |
| `lag(x[, k])`, `lead(x[, k])` | Value k rows before, after |

A missing or non-numeric value gives a missing result. So do division
by zero and invalid math such as `log(-1)`.

## Recoding

`:recode` makes cells numeric and adds a `NAME_num` or `NAME_code`
column. `:recode!` changes the column in place. Missing values stay
missing.

| Mode | Converts |
|------|----------|
| `auto` (default) | Picks one of the modes below |
| `number` | Decimal commas, thousands separators, `12%` (0.12), `€3.50`, `3.5 kg` |
| `bool` | yes/no, true/false, on/off to 1/0 |
| `codes` | Categories to 1, 2, … in sorted order (mapping shown in `*recode*`) |
| `map V=C …` | Your own mapping |

## Edit mode

Press `e` (or `:table edit`) to edit the table, like Emacs wdired.

| Key | Description |
|-----|-------------|
| `i`, `<CR>` | Edit the cell (on the header: rename the column) |
| `x` | Clear the cell or selection |
| `dd`, `d` (visual) | Delete rows |
| `o`, `O` | Add a row below, above |
| `u`, `<C-r>` | Undo, redo |
| `:column rename NAME` | Rename the column |
| `:column delete` | Delete the column |
| `:column add [NAME]` | Add a column |

Changes are highlighted and only staged:

- `:w` or `ZZ` asks before writing them to the file.
- `ZQ`, `q`, `:table` or leaving edit mode asks before discarding them.

Unchanged records keep their exact text. If the text changes while
edits are staged, applying them is refused.
