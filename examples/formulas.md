# Markdown rendering test

Open this file in scihe and press `Space m r` to toggle the rendered view
(read-only). Press `Space m r` or `q` to go back to the text.

Sections 1–6 are formulas; sections 7–12 are tables, quotes, alerts,
rules and task items. Every line of the file stays one line of the view.

## 1. Inline formulas

- Pythagoras: $a^2 + b^2 = c^2$
- With blanks inside the dollars: $ E = mc^2 $
- Greek letters: $\alpha, \beta, \gamma, \Delta, \Omega, \varepsilon, \varphi$
- Subscripts: $x_1 + x_2 + \dots + x_n$, $a_{ij}$, $\alpha_{i,j}$
- Superscripts: $e^{-x}$, $x^{n+1}$, $A^T$, $f'(x)$
- Fractions: $\frac{1}{2}$, $\frac{a+b}{c}$, $\frac12$
- Roots: $\sqrt{2}$, $\sqrt{x^2 + y^2}$, $\sqrt[3]{8}$, $\sqrt[n]{x}$
- Relations: $a \le b$, $a \ge b$, $a \ne b$, $a \approx b$, $a \equiv b$
- Sets: $x \in \mathbb{R}$, $\mathbb{N} \subset \mathbb{Z} \subseteq \mathbb{Q}$, $A \cup B \cap C$, $\emptyset$
- Logic: $\forall x \exists y : \neg P \Rightarrow Q$, $p \land q \lor r$, $A \iff B$
- Arrows: $f: X \to Y$, $x \mapsto x^2$, $a \leftarrow b$, $\lim_{x \to \infty}$
- Calligraphic: $\mathcal{L}$, $\mathcal{N}(\mu, \sigma^2)$
- Accents: $\hat{x}$, $\bar{x}$, $\vec{v}$, $\dot{x}$, $\ddot{x}$, $\tilde{y}$
- Text in formulas: $x = 1 \text{ if } y > 0$
- Functions: $\sin^2\theta + \cos^2\theta = 1$, $\log_2 n$, $\exp(x)$, $\max_i x_i$
- Parenthesis form: \(a \cdot b = |a| |b| \cos\theta\)

## 2. Display formulas

On one line: $$\sum_{k=1}^{n} k = \frac{n(n+1)}{2}$$

Over several lines:

$$
\int_0^\infty e^{-x^2}\,dx = \frac{\sqrt{\pi}}{2}
$$

With brackets:

\[
\nabla \cdot \mathbf{E} = \frac{\rho}{\varepsilon_0}
\]

## 3. Math environments (Jupyter style, no dollars)

\begin{equation}
\hat{\beta} = (X^T X)^{-1} X^T y
\end{equation}

\begin{align*}
\mu &= \frac{1}{n} \sum_{i=1}^{n} x_i \\
\sigma^2 &= \frac{1}{n-1} \sum_{i=1}^{n} (x_i - \mu)^2
\end{align*}

\begin{gather}
P(A \mid B) = \frac{P(B \mid A) \, P(A)}{P(B)}
\end{gather}

## 4. Statistics examples

- Normal density: $f(x) = \frac{1}{\sigma\sqrt{2\pi}} e^{-\frac{(x-\mu)^2}{2\sigma^2}}$
- Expected value: $E[X] = \sum_x x \, P(X = x)$
- Correlation: $\rho_{XY} = \frac{\mathrm{cov}(X, Y)}{\sigma_X \sigma_Y}$
- Likelihood: $\mathcal{L}(\theta) = \prod_{i=1}^{n} p(x_i \mid \theta)$
- Gradient step: $\theta \leftarrow \theta - \eta \nabla_\theta J(\theta)$

## 5. Must stay as written (not formulas)

- Prices: it costs $5 and $10 in total.
- Price then formula: $5, then $x$ (only the second part is a formula).
- Lopsided blanks: $ x$ and $y $
- Escaped dollar: the price is \$20.
- Inline code: `$x^2$` stays as code.
- Other environments: \begin{itemize} is left alone.

```python
# code blocks are never rendered
cost = "$x^2$"
```

~~~
$$ \alpha $$ inside a ~~~ fence stays too
~~~

## 6. Known limits (one line, no 2D layout)

- Nested fractions: $\frac{\frac{a}{b}}{c}$
- Superscript inside a superscript: $e^{x^2}$
- Matrices: $\begin{pmatrix} a & b \\ c & d \end{pmatrix}$
- Unknown commands are kept: $\foo{x} + \operatorname{argmax}_x f(x)$

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

A pipe in a cell is written `\|`, also in a formula:

| Expression | Value |
|------------|-------|
| a \| b     | or    |
| $\|x\|$    | norm  |

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
