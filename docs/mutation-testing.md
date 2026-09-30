# Mutation testing — slopguard-go

This records a mutation-testing pass over the three importable packages
(`core`, `coverage`, `cli`). Mutation testing seeds small source changes
("mutants") — flipping `>` to `>=`, `+` to `-`, `&&` to `||`, `true` to
`false`, `i++` to `i--` — and runs the test suite against each. A mutant the
tests catch is *killed*; one that slips through is a *survivor* and marks a gap
in the tests.

## Repeat it with `slopguard-go mutate`

This pass used a manual harness, described below. The `mutate` command now
does the same job, and the README section "Mutation testing" documents it. To
repeat this pass, mutate one package at a time and scope the test runs to that
package, as the harness did:

```bash
slopguard-go mutate --path ./core --packages ./core/...
slopguard-go mutate --path ./coverage --packages ./coverage/...
slopguard-go mutate --path ./cli --packages ./cli/...
```

`mutate` differs from the harness in four ways:

- It has more operators: besides the binary-operator, increment and
  boolean-literal mutants, it removes call statements, `!` and unary minus.
- A mutant on a line that no test executes is `no_coverage` and does not run.
- The timeout is `ceil(3 × baseline seconds) + 30` instead of `-timeout 12s`,
  and a timeout counts as killed.
- An equivalent mutant can be switched off in the source with the
  `slopguard-ignore-mutant` marker, for example
  `if s > agg.Max { // slopguard-ignore-mutant(boundary): equal values assign the same max`.
  The inventory below lists the candidates.

For one file, `slopguard-go mutate --path ./core/crap.go --packages ./core/...`
reports 11 mutants: 10 killed and 1 survived (score 90.91%). The survivor is
`core/crap.go:53` `s > agg.Max` → `>=`, the equivalent mutant listed below.

## How it was run

A standard-library-only AST harness (`go/parser` + `go/printer`) enumerates
binary-operator, increment/decrement, and boolean-literal mutants per source
file, applies one at a time, and runs `go test` for the affected package
(`go test -timeout 12s` so an infinite-loop mutant self-aborts as a kill rather
than hanging). String-concatenation `+` and similar non-compiling swaps are
reported as build-errors and excluded from the score.

## Result

| Metric | Before | After |
|---|---:|---:|
| Viable mutants | 268 | 268 |
| Killed | 193 | 241 |
| Survived | 75 | 27 |
| **Mutation score** | **72.0%** | **~89.9%** |

All 27 remaining survivors are **equivalent mutants** — the mutation does not
change observable behaviour, so no test can distinguish it. They are catalogued
below so future contributors don't mistake them for missing coverage.

## Equivalent-mutant inventory (cannot be killed)

### Assign-equal-value is a no-op (`max`/aggregate tracking)
- `core/aggregator.go:189` `m.Complexity > maxComplexity` → `>=`
- `core/aggregator.go:192` `m.CognitiveComplexity > maxCognitive` → `>=`
- `core/aggregator.go:282` `maxInt`: `a > b` → `>=`
- `core/crap.go:53` `s > agg.Max` → `>=`

  When the values are equal, the guarded assignment writes the same number, so
  the output is identical. `core/crap.go:53` compares `float64` scores and has
  one exception: with a negative-zero input, the `>=` version stores -0
  instead of 0. `CrapScore` never returns -0, so no score that slopguard-go
  computes tells the two versions apart.

### Stable-sort comparator strictness
- `core/aggregator.go:86`, `core/aggregator.go:87`, `core/format.go:97`
  `a.Crap > b.Crap` → `>=`
- `core/diranalyzer.go:73` `reports[i].Path < reports[j].Path` → `<=`

  A `>=`/`<=` comparator returns true for equal keys, which violates the
  strict-weak-ordering contract; with the distinct keys these sorts ever see,
  and `SliceStable`, the emitted order is unchanged.

### Slice/repeat at the exact boundary is the identity
- `core/fileanalyzer.go:55` `len(src) > limit` → `>=` (`src[:2048]` when
  `len(src)==2048` equals `src`)
- `core/format.go:98` `len(ranked) > topN` → `>=` (`ranked[:topN]` when
  `len==topN` is the whole slice)
- `core/format.go:131`, `core/format.go:138` `len(s) >= width` → `>`
  (`strings.Repeat(" ", 0)` when `len(s)==width`)
- `coverage/runner.go:158` `buf.Len() > limit` → `>=` (excess `= 0` →
  `buf.Next(0)` is a no-op)

### Unreachable / dead-in-practice guards
- `core/complexity.go:188` `amount <= 0` → `< 0` — `bumpCognitive` is only ever
  called with `≥ 1`; on `0` both branches add nothing.
- `core/complexity.go:200` `len(n.Recv.List) > 0` → `>=` — `n.Recv != nil`
  already implies at least one receiver field.
- `core/complexity.go:225` `len(a.methodStack) > 0` → `>=` — the stack always
  holds the method being finished.
- `coverage/projectroot.go:20` `return "", false` → `true` — only reached if
  `filepath.Abs` fails (i.e. `os.Getwd` fails), which does not happen in tests.

### Loop bound that a `break` reaches first
- `coverage/projectroot.go:25` `i < 64` → `<=` and `i++` → `--` — the walk-up
  loop returns/breaks at the filesystem root before the 64-iteration backstop is
  ever decisive.

### Buffer-size tuning protected by the `bufio` capacity floor
- `coverage/profile.go:43` (×3) and `coverage/runner.go:128` (×2)
  `scanner.Buffer(make([]byte, 0, 64*1024), …)` `*` → `/`

  `bufio.Scanner` uses `max(maxArg, cap(buf))`, so the 64 KiB initial capacity
  remains the effective floor; only a single profile line larger than 64 KiB
  (a pathological filename) could tell the variants apart.

### Behaviourally inert flag / parse bookkeeping
- `coverage/profile.go:51` `first = false` → `true` — re-entering the
  mode-line check on later lines is harmless; data lines never carry a `mode:`
  prefix.
- `cli/analyze.go:60` `verbose = fs.Bool("verbose", false, …)` → `true` — the
  `-v` alias (`fs.BoolVar(verbose, "v", false, …)` on line 69) re-sets the same
  variable's default afterward, so flipping line 60 alone changes nothing.

### Non-deterministic by construction
- `coverage/index.go:125` `overlap > bestOverlap` → `>=` — the strict-vs-loose
  choice only matters on a tie between two equal-suffix candidates, and the
  candidate slice is built from a map iteration (random order), so there is no
  stable contract to assert against.
