# Markdown rendering test

Open this file in scihe and press `Space m r` to toggle the rendered view
(read-only). Press `Space m r` or `q` to go back to the text. With
`Space t w` (wrap) paragraphs and tables fit the window: try resizing it.

Formulas are written in LaTeX between `$…$` (inline) or `$$…$$`
(display); a line opening an environment (`\begin{align}`) starts a
display formula too. Sections 1–6 are formulas, 7–12 tables, quotes,
alerts, rules and task items, 13 scientific text, and 14 a piece of a
manuscript to try it all together.

## 1. Inline formulas

- Pythagoras: $a^2 + b^2 = c^2$
- With blanks inside the dollars: $ E = m c^2 $
- Greek letters: $\alpha, \beta, \gamma, \Delta, \Omega, \varepsilon, \varphi$
- Subscripts: $x_1 + x_2 + \dots + x_n$, $a_{ij}$, $\alpha_{i,j}$
- Superscripts: $e^{-x}$, $x^{n+1}$, $A^T$, $f'(x)$, also $x_i^n$
- Relations: $a \le b$, $a \ge b$, $a \ne b$, $a \approx b$, $a \equiv b$, $x \not\in A$
- Sets: $x \in \mathbb{R}$, $\mathbb{N} \subset \mathbb{Z} \subseteq \mathbb{Q}$, $A \cup B \cap C$, $\emptyset$
- Logic: $\forall x \exists y : \neg P \Rightarrow Q$, $p \land q \lor r$, $A \iff B$
- Arrows: $f: X \to Y$, $x \mapsto x^2$, $a \leftarrow b$
- Calligraphic, blackboard and bold: $\mathcal{L}$, $\mathcal{N}(\mu, \sigma^2)$, $\mathbb{R}^n$, $\mathbf{x}$
- Accents: $\hat{x}$, $\bar{x}$, $\vec{v}$, $\dot{x}$, $\ddot{x}$, $\tilde{y}$
- Text in formulas: $x = 1 \text{ if } y > 0$, $\operatorname{tr}(A)$
- Functions: $\sin^2\theta + \cos^2\theta = 1$, $\log_2 n$, $\exp(x)$
- Unicode typed directly: $x² + α ≤ ∞$

## 2. Display formulas

On one line: $$\sum_{k=1}^{n} k = \frac{n(n+1)}{2}$$

Over several lines:

$$
\int_0^\infty e^{-x^2}\,dx = \frac{\sqrt{\pi}}{2}
$$

Several rows (`\\`), numbered with `\tag`:

$$
\nabla \cdot E = \frac{\rho}{\varepsilon_0} \tag{1} \\ \nabla \times B = \mu_0 J \tag{2}
$$

An environment without dollars, as in Jupyter:

\begin{align}
f(x) &= (x + 1)^2 \\
     &= x^2 + 2x + 1
\end{align}

## 3. Fractions, roots and brackets

- Fractions: $\frac{1}{2}$, $\frac{a + b}{c}$, $\frac12$, $\dfrac{a}{b}$, nested $ \frac{\frac{a}{b}}{c} $
- Roots: $\sqrt{2}$, $\sqrt{x^2 + y^2}$, $\sqrt[3]{8}$, $\sqrt[n]{x}$
- Brackets that grow: $\left( \frac{a}{b} \right)$, $(\frac{1}{2})^2$, $\left[ \frac{x}{2} \right]$, $\left\{ a \right.$
- Brackets that stay on the line: $p(x_i \mid \theta)$, $(X^T X)^{-1}$
- Absolute value, norm, floor, ceiling: $\lvert x \rvert$, $\lVert v \rVert$, $\lfloor x \rfloor$, $\lceil x \rceil$

## 4. Sums, integrals and limits

$$
\lim_{x \to 0} \frac{\sin x}{x} = 1
$$

$$
\prod_{i=1}^{n} p(x_i \mid \theta) = \exp \left( \sum_{i=1}^{n} \log p(x_i \mid \theta) \right)
$$

$$
\iint_{D} f(x, y)\,dx\,dy
$$

Scripts are set compactly: the exponent of the normal density stays on
one row.

$$
f(x) = \frac{1}{\sigma \sqrt{2\pi}} e^{-\frac{(x - \mu)^2}{2\sigma^2}}
$$

## 5. Matrices and cases

$$
A = \begin{pmatrix} a & b \\ c & d \end{pmatrix}, \quad \det A = ad - bc
$$

$$
\lvert x \rvert = \begin{cases} x & \text{if } x \ge 0 \\ -x & \text{if } x < 0 \end{cases}
$$

$$
\binom{n}{k} = \frac{n!}{k!\,(n - k)!}
$$

## 6. Must stay as written (not formulas)

- Prices: it costs $5 and $10 in total.
- Price then formula: $5, then $x$ (only the second part is a formula).
- Lopsided blanks: $ x$ and $y $
- Escaped dollar: the price is \$20.
- Inline code: `$x^2$` stays as code.

```python
# code blocks are never rendered
cost = "$x^2$"
```

~~~
$$ \alpha $$ inside a ~~~ fence stays too
~~~

## 7. Tables

Column alignment from the delimiter row (left, right, center):

| Model    | Accuracy | Learning rate |
|:---------|---------:|:-------------:|
| baseline |     0.81 |   $10^{-3}$   |
| ours     | **0.93** | $\frac{1}{2}$ |
| tuned    |    0.951 | $\eta_0$      |

Without outer pipes, unaligned in the source:

Symbol | Meaning
--- | ---
$\mu$ | mean
$\sigma^2$ | variance
$\rho_{XY}$ | correlation

A pipe in a cell is written `\|`:

| Expression | Value |
|------------|-------|
| a \| b     | or    |
| $\lvert x \rvert$ | absolute value |

Missing cells are left empty, extra cells are dropped:

| a | b | c |
|---|---|---|
| 1 |
| 1 | 2 | 3 | 4 |

Wide characters keep the columns aligned:

| City  | Name      |
|-------|-----------|
| Tokyo | 東京      |
| Paris | Paris     |

A list item ends a table:

| Step | Done |
|------|------|
| 1    | yes  |
- not a row

## 8. Not tables (stay as written)

No delimiter row:

| a | b |
| c | d |

Cell counts differ:

| a | b |
|---|

A heading with a pipe:

# a | b
|---|---|

Indented code:

    | a |
    |---|

## 9. Quotes

> A plain quote with a formula: $E = mc^2$.
> It keeps going on this line.
>
> > A nested quote.
> > > And one more level.

> A display formula inside a quote:
> $$
> \sum_{k=1}^{n} k = \frac{n(n+1)}{2}
> $$

## 10. Alerts and callouts

> [!NOTE]
> Useful information that users should know.

> [!TIP]
> Helpful advice for doing things better.

> [!IMPORTANT]
> Key information users need to know.

> [!WARNING]
> Urgent info that needs immediate attention: memory grows with $n^2$.

> [!CAUTION]
> Advises about risks or negative outcomes.

Obsidian callouts, lowercase, with a title and a fold marker:

> [!tip]- Read this first
> The title replaces the type name.

> [!example]
> Unknown types get a generic marker.

Only the first line of a quote opens an alert:

> Some text
> [!NOTE] stays as written here.

## 11. Horizontal rules

Three dashes:

---

Three stars, spaced:

* * *

Underscores:

___

Not rules:

Setext heading (the dashes underline this line)
---

Dashes indented with a tab are code, not a rule:

	---

## 12. Task lists

- [ ] write the introduction
- [x] collect the data
  - [ ] nested, open
  * [X] nested, done (capital X)
1. [ ] numbered, open
2. [x] numbered, done
- [ ]

> - [x] a task inside a quote, with $x^2$

A plain list stays as written:

- item
- another item
1. first
2. second

```markdown
- [ ] inside a code block nothing is rendered
| a |
|---|
> [!NOTE]
---
```

## 13. Scientific text

Unicode scripts are raised in the view: H₂O, E = mc², the 1<sup>st</sup>
and 2<sub>nd</sub> terms, a footnote.[^1] Type them with `Space m ^` and
`Space m _` (on the character, the number or a selection), and Greek
letters or symbols with `Space m g`, or `Ctrl-k` in insert mode.
`Space m f` puts `$…$` around a word, `Space m F` opens a `$$` block.

| Quantity | Formula | Notes |
|----------|---------|-------|
| Kinetic energy | $E = \frac{1}{2} m v^2$ | a long note that wraps inside its cell when the window is narrow |
| Water | H₂O | |

[^1]: Footnotes show their number raised too.

## 14. A manuscript

### 2.3 Statistical model

We model the response $y_i$ of subject $i = 1, \dots, n$ as a linear
function of $p$ covariates $x_i \in \mathbb{R}^p$ with Gaussian noise of
unknown variance $\sigma^2$:

$$
y_i = x_i^T \beta + \varepsilon_i, \quad \varepsilon_i \sim \mathcal{N}(0, \sigma^2)
$$

Under this model the log-likelihood of the parameters given the data
$D = \{ (x_i, y_i) \}$ is

$$
\ell(\beta, \sigma^2) = -\frac{n}{2} \log(2\pi\sigma^2) - \frac{1}{2\sigma^2} \sum_{i=1}^{n} (y_i - x_i^T \beta)^2
$$

and, writing $X$ for the $n \times p$ design matrix, its maximum is
reached at the ordinary least squares estimator[^ols]

$$
\hat{\beta} = (X^T X)^{-1} X^T y, \quad \hat{\sigma}^2 = \frac{1}{n - p} \lVert y - X \hat{\beta} \rVert^2
$$

The estimator is unbiased, $E[\hat{\beta}] = \beta$, with covariance
$\sigma^2 (X^T X)^{-1}$; confidence intervals at level $1 - \alpha$ follow
from $t_{n-p}$, the Student distribution with $n - p$ degrees of
freedom. Model selection used the Akaike criterion,
$\mathrm{AIC} = 2k - 2 \log \hat{L}$, where $k$ is the number of parameters and
$\hat{L}$ the maximised likelihood.

> [!NOTE]
> All covariates were centred and scaled to unit variance before
> fitting, so that $\bar{x} = 0$ and $s_x = 1$ for every column of $X$.

**Table 2.** Estimates for the three nested models ($n = 248$).

| Model | Covariates | $\hat{\sigma}^2$ | $R^2$ | AIC |
|:------|:-----------|--------------:|------:|----:|
| M₁ | age | 4.12 | 0.31 | 1043.2 |
| M₂ | age, dose | 3.27 | 0.45 | 987.6 |
| M₃ | age, dose, age × dose | 3.25 | 0.46 | 989.1 |

The interaction in M₃ does not improve the fit ($\Delta \mathrm{AIC} = 1.5$), so
M₂ was retained. Its residuals $r_i = y_i - x_i^T \hat{\beta}$ showed no
departure from normality (Shapiro–Wilk $W = 0.993$, $p = 0.31$); see
![Figure 3: residuals of M₂ against fitted values](figures/residuals.png)

[^ols]: Equivalently, $\hat{\beta}$ minimises $\sum_i (y_i - x_i^T \beta)^2$.
