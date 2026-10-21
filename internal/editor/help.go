package editor

import (
	"fmt"
	"sort"
	"strings"

	"scihe/internal/keymap"
)

// exHelp lists every key binding and ex command in a scratch buffer.
func exHelp(e *Editor, a *exArgs) error {
	var sb strings.Builder
	sb.WriteString("scihe — key bindings and commands\n")
	sb.WriteString("=====================================\n\n")
	sb.WriteString("Leader key: " + keymap.Format([]keymap.Key{e.leader}) + "   (press it and wait for the which-key popup)\n")
	sb.WriteString("Config file: " + configPathHint() + "\n\n")
	section := func(title string, m *keymap.Map[*Cmd]) {
		sb.WriteString(title + "\n" + strings.Repeat("-", len(title)) + "\n")
		var lines []string
		var walk func(n *keymap.Node[*Cmd], prefix []keymap.Key)
		walk = func(n *keymap.Node[*Cmd], prefix []keymap.Key) {
			if n.Binding != nil && len(prefix) > 0 {
				lines = append(lines, fmt.Sprintf("  %-16s %s", keymap.Format(prefix), n.Binding.Label))
			}
			for k, c := range n.Children {
				walk(c, append(append([]keymap.Key(nil), prefix...), k))
			}
		}
		walk(m.Root, nil)
		sort.Strings(lines)
		sb.WriteString(strings.Join(lines, "\n"))
		sb.WriteString("\n\n")
	}
	section("Normal mode", e.normalMap)
	section("Visual mode", e.visualMap)
	section("Operator-pending (motions and text objects)", e.opMap)
	sb.WriteString("Insert mode\n-----------\n")
	sb.WriteString("  <Esc>/<C-c>      normal mode         <C-w>  delete word      <C-u>  delete to line start\n")
	sb.WriteString("  <C-r>{reg}       insert register     <C-o>  one normal cmd   <C-n>/<C-p>  keyword completion\n")
	sb.WriteString("  <C-t>/<C-d>      indent / dedent     <Tab>  indent           <BS>   delete (soft tab aware)\n\n")
	sb.WriteString("Ex commands\n-----------\n")
	seen := map[string]bool{}
	for _, c := range exCommands {
		if seen[c.name] {
			continue
		}
		seen[c.name] = true
		abbr := c.name
		if c.min < len(c.name) {
			abbr = c.name[:c.min] + "[" + c.name[c.min:] + "]"
		}
		sb.WriteString(fmt.Sprintf("  :%-18s %s\n", abbr, c.desc))
	}
	sb.WriteString("\nRanges: N  .  $  %  'x  /pat/  ?pat?  with +N/-N offsets, e.g. :.,+3d  :'<,'>s/a/b/g\n")
	sb.WriteString(expressionHelp)
	sb.WriteString("Patterns use Go regexp syntax; \\< \\> are word boundaries; \\c / \\C force case.\n")
	e.showOutput("*help*", sb.String())
	return nil
}

func configPathHint() string {
	if configPath == "" {
		return "~/.config/scihe/config.toml (none in use: settings are not saved)"
	}
	return configPath
}

// configPath is the config file in use, set by main through
// SetConfigPath; "" (tests) means interactive settings are not saved.
var configPath string

// SetConfigPath records the config file location for :help, :config and
// for saving the theme / transparency chosen interactively.
func SetConfigPath(p string) { configPath = p }

// expressionHelp documents the formulas of :compute and :transform.
const expressionHelp = `
Expressions (:compute NAME = EXPR, :transform EXPR)
---------------------------------------------------
  values      numbers, "text", pi, e; columns by name, ` + "`name with spaces`" + `, [name], $3;
              _ is the column being transformed (:transform)
  operators   + - * / %   ^ or ** (power)   < <= > >= == != (1 or 0)   and or not
  row         abs sqrt square exp log ln log10 log2 floor ceil round(x[, d]) pow(x, y)
              min(a, b, …) max(a, b, …) if(c, a, b) isna(x) coalesce(a, b, …)
  aggregates  mean median sum count std var sem range iqr skew kurt quantile(x, p) n(),
              min(x) max(x) with one argument: one value over the rows, any
              expression inside, e.g. :compute dev = price - mean(price)
  window      rank(x) cumsum(x) diff(x) lag(x[, k]) lead(x[, k]), in row order
  rows        :compute uses all rows (shown order first); :transform the
              selected rows in the shown order
  shortcuts   :transform z | minmax | pct | log | log10 | log2 | sqrt | square |
              exp | abs | rank | cumsum | diff  (z = (_ - mean(_)) / std(_))
  Missing or non-numeric operands, division by zero and math errors give
  missing values.
`
