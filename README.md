# slopguard-go

[![CI](https://github.com/JeevanThandi/slopguard-go/actions/workflows/ci.yml/badge.svg)](https://github.com/JeevanThandi/slopguard-go/actions/workflows/ci.yml)

> **CRAP (Change Risk Anti-Patterns) guardrail for Go.**

> ⚠️ **Alpha (v0.2.x).** The analyzer is stable and self-tested, but the CLI surface and JSON schema may still change before v1.0.

`slopguard-go` measures **complex, undertested code** in Go modules. It computes a weighted CRAP score combining cyclomatic and cognitive complexity with line coverage, and prints a structured report you can pipe into `jq` or fail CI on. It is the Go sibling of [slopguard-swift](https://github.com/JeevanThandi/SlopGuard-Swift) and [slopguard-typescript](https://github.com/JeevanThandi/slopguard-typescript) — same formula, same schema, same UX.

It also ships `mutate`, a mutation tester that checks whether the tests catch small changes to the code. See [Mutation testing](#mutation-testing).

```
wCRAP(m) = (cyc × cog) × (1 − cov/100)³ + sqrt(cyc × cog)
```

* `cyc` — cyclomatic complexity (McCabe), parsed via the standard library [`go/ast`](https://pkg.go.dev/go/ast). Counts `if`, `for`, `range`, each non-default `case`, and each `&&` / `||` — comparable to `gocyclo`.
* `cog` — cognitive complexity per the [SonarSource 2023 spec](https://www.sonarsource.com/resources/cognitive-complexity/) — penalises nesting, charges a whole `switch`/`select` once, ignores early-exit shapes (plain `return`/`break`/`continue`).
* `wt`  — `sqrt(cyc × cog)`, the geometric blend fed into the formula. A flat 50-case `switch` (cyc=50, cog=1) scores like a small method; a deeply nested 3-branch tangle (cyc=3, cog=12) scores like medium-complex code.
* `cov` — line coverage gathered by slopguard-go itself, by driving the module's own `go test`. Never user-supplied.
* Default crappy threshold: **30** (on wCRAP).

## Install

```bash
go install github.com/JeevanThandi/slopguard-go/cmd/slopguard-go@latest
```

…or build from source:

```bash
git clone https://github.com/JeevanThandi/slopguard-go.git
cd slopguard-go
go build -o slopguard-go ./cmd/slopguard-go
cp slopguard-go /usr/local/bin
```

Requires Go 1.23+.

## Quickstart

```bash
# Zero-config: analyze the current module (runs go test for coverage)
slopguard-go

# Scan a specific directory and print the top crappy methods
slopguard-go analyze --path ./internal --threshold 30

# Scope the test run to specific packages (anything they don't exercise reads 0%)
slopguard-go analyze --path ./internal/auth --packages ./internal/auth/...

# Full JSON for CI / downstream tooling
slopguard-go analyze --path . --json | jq '.methods | sort_by(-.crap)[:10]'

# Fail CI when any method's CRAP exceeds 50
slopguard-go analyze --path . --fail-over 50

# Complexity only (skip the test run — every method shows 0% coverage)
slopguard-go analyze --path ./pkg --no-coverage

# Join coverage CI already produced (a `go test -coverprofile` file)
slopguard-go analyze --path . --coverage-file cover.out
```

Progress markers (`slopguard: running go test with coverage…`) go to **stderr**, so piped stdout stays clean. `--verbose` streams the underlying `go test` output through; `--quiet` silences progress entirely.

## How coverage works

Coverage is an *artifact of the analysis*, not an input — mirroring how slopguard-swift drives `xcodebuild test` and slopguard-typescript drives vitest/jest:

1. **Module discovery.** Walk up from `--path` to the nearest `go.mod` (override with `--project-dir`).
2. **Test run.** Run `go test -coverprofile=<temp>/cover.out -covermode=count -coverpkg=<packages> <packages>` (default `./...`) into a slopguard-owned temp directory. `-coverpkg` instruments every selected package so coverage from cross-package tests counts. Failing tests don't abort — partial coverage is still useful (a note is attached). A build failure with no usable profile aborts with the `go test` output tail.
3. **Join.** Parse the coverage profile into a per-line index, resolve its import-path file names to disk via the module path from `go.mod` (basename + longest-suffix fallback for CI-vs-local path mismatches), join per-method line coverage onto the parsed declarations, then delete the temp dir.

A Go coverage profile is the universal interchange format — anything that runs `go test -coverprofile` produces one — so a profile your CI already generated is supported via `--coverage-file`.

## Subcommands

| Command   | Purpose |
|-----------|---------|
| `analyze` | Walk a directory of Go sources, drive `go test` for coverage, emit a wCRAP report (text or JSON). |
| `mutate`  | Change the source one small step at a time, run `go test` against each change, and report the changes the tests do not catch. See [Mutation testing](#mutation-testing). |
| `version` | Print version metadata as JSON. |

`analyze` is the default subcommand and `--path` defaults to the current directory — a bare `slopguard-go` in your module root just works.

## JSON output

`--json` emits a stable, versioned (`schemaVersion: "2"`, shared with slopguard-swift and slopguard-typescript) report with:

* `summary` — file/type/method counts, average + max wCRAP, weighted coverage.
* `methods[]` — every analyzed function/method with `complexity`, `cognitiveComplexity`, `weightedComplexity`, `coverage`, `crap`, `isCrappy`, and a stable `id`.
* `types[]` — per-struct / interface / method-bearing named-type aggregation: `aggregatedCrap` (formula applied to type totals) and `maxCrap` (worst single-method offender).

Slice with `jq`:

```bash
# Top 10 worst methods
slopguard-go analyze --path . --json | jq '.methods | sort_by(-.crap)[:10]'

# Only crappy types
slopguard-go analyze --path . --json | jq '.types[] | select(.isCrappy)'

# Coverage gaps: high complexity, low coverage
slopguard-go analyze --path . --json \
  | jq '.methods[] | select(.complexity >= 5 and .coverage <= 50)'
```

### Build an agent work queue

```bash
slopguard-go analyze --json --quiet \
  | jq '[.methods[] | select(.isCrappy)] | sort_by(-.crap)
         | map({id, crap, coverage, file, line})'
```

Drop this into `CLAUDE.md` / `AGENTS.md` so your agent gates on slop and refactors the worst offenders first:

> Use `slopguard-go` to analyze this repo and find the method with the highest wCRAP score. Show me its file and line, then add tests or refactor until its score is under 30.

## Why it exists

Test coverage alone says "this code ran in a test"; complexity alone says "this code has many paths." Neither tells you whether the *risky* code is tested. CRAP combines them: a method with 20 branches and 0% coverage scores 420; the same method at 100% coverage scores 20 (just its complexity). The score lights up the code most likely to break under a refactor *and* be the hardest to verify the fix for — exactly the code your coding agents trip over.

## What counts as a method

Top-level **functions** and **methods** (functions with a receiver). Anonymous function literals don't get their own entry — their branches count toward the enclosing function, with a cognitive nesting bump for the closure body, per the Sonar spec.

Go attaches methods to a type by **receiver**, not by lexical nesting, so a type's members are gathered package-wide: `func (p *Parser) Parse()` in `parse.go` and `func (p *Parser) reset()` in `reset.go` both roll up into the `Parser` type entry.

Default excludes keep noise out: `vendor/`, `testdata/`, `*_test.go`, generated code (`*.pb.go`, `*_gen.go`, `mock_*.go`, `*_string.go`, and anything carrying the `// Code generated … DO NOT EDIT.` header), and the `bin/`/`node_modules` build dirs. Analyze excluded code with `--no-default-excludes`.

## Mutation testing

Coverage shows which lines ran during the tests. It does not show whether a test checked the result. `slopguard-go mutate` checks that. It makes one small change to the source at a time, called a *mutant*: `>` becomes `>=`, `+` becomes `-`, `&&` becomes `||`, `true` becomes `false`, a call statement is deleted. Then it runs `go test` against each mutant.

* When a test fails, the mutant is **killed**. The tests check that behaviour.
* When every test still passes, the mutant **survived**. No test checks that behaviour.

Each survivor is reported with its file, line, column, original text, replacement text and enclosing function. A developer or an agent can use that to write the test that kills it. `analyze` finds complex code that the tests do not run. `mutate` finds code that the tests run but do not check.

### Quickstart

```bash
# Mutation-test one package (one go test run per mutant)
slopguard-go mutate --path ./internal/auth

# One file, only the comparison operators
slopguard-go mutate --path ./internal/auth/token.go --operators boundary,negate_conditional

# List the mutants without running a test
slopguard-go mutate --path . --dry-run

# Survivors as JSON, as an agent work queue
slopguard-go mutate --path . --json --quiet \
  | jq '[.mutants[] | select(.status == "survived") | {id, original, replacement, method}]'

# Fail CI when the mutation score is below 80
slopguard-go mutate --path . --fail-under 80
```

Each mutant costs one `go test` run, so start with one package or one file. The Go test cache helps: packages that a mutant does not change come back `(cached)`.

Drop this into `CLAUDE.md` / `AGENTS.md`:

> Run `slopguard-go mutate --path <package> --json --quiet`. For every survived mutant, write a test that fails with the mutant in place and passes without it. Re-run until nothing survives.

### How a run works

1. `mutate` walks `--path` with the same excludes as `analyze` (test files, `vendor/`, generated code), parses each file and lists its mutants. A `--dry-run` stops here. So does a run with no mutant left to run.
2. The plain baseline runs `go test -overlay=<json> -vet=off -failfast <packages>` once in the module root, with an empty overlay and no timeout. It must pass. A failing suite stops the run with `baseline_failed`. Its run time sets the per-mutant timeout: `ceil(3 × baseline seconds) + 30`.
3. The coverage baseline runs the `analyze` coverage command once. `--no-coverage` skips it. A mutant on a line that no test executes gets `no_coverage` and does not run. This run never fails `mutate`: without usable coverage data, every mutant runs and a note says so.
4. Each remaining mutant runs as `go test -overlay=<json> -vet=off -failfast <packages>`, in report order, with the timeout.

### Flags

| Flag | Meaning |
|---|---|
| `-p, --path <path>` | Directory or single `.go` file to mutate. Default `.`. |
| `--include <glob>` | Only mutate files matching the glob. Repeatable. |
| `--exclude <glob>` | Extra glob to skip, added to the default excludes. Repeatable. |
| `--no-default-excludes` | Drop the built-in excludes (the same list as `analyze`). |
| `--operators <ids>` | Comma-separated operator ids to apply. Repeatable. Default: all. |
| `--project-dir <dir>` | Module root that `go test` runs in. Default: the nearest `go.mod` above `--path`. |
| `--packages <pattern>` | `go test` package pattern. Default `./...`. |
| `--no-coverage` | Skip the coverage baseline and run every mutant. |
| `--timeout <seconds>` | Per-mutant timeout. Default: `ceil(3 × baseline seconds) + 30`. |
| `--dry-run` | List the mutants; run no test. |
| `--json` | Emit the JSON report on stdout. |
| `--fail-under <score>` | Exit 2 when the mutation score is below this number. A score of `n/a` never fails. Ignored with `--dry-run`. |
| `-v, --verbose` | Stream the `go test` output to stderr. |
| `--quiet` | Suppress progress on stderr. |

`--threshold`, `--fail-over` and `--coverage-file` belong to `analyze` and are rejected. A positional argument is rejected too: pass the path with `--path`. Exit codes: `0` ok, `1` error, `2` below `--fail-under`, `130` / `143` / `129` stopped by SIGINT / SIGTERM / SIGHUP.

### Operators

The operator ids are the same in every slopguard port.

| id | Change | Notes |
|---|---|---|
| `arithmetic` | `+`↔`-`, `*`↔`/`, `%`→`*`, `+=`↔`-=`, `*=`↔`/=`, `%=`→`*=` | Skips string concatenation: a `+` or `+=` with a string-literal operand, including every `+` in `"a" + b + c`. |
| `boolean_literal` | `true`↔`false` | |
| `boundary` | `<`↔`<=`, `>`↔`>=` | |
| `increment` | `i++`↔`i--` | |
| `invert_negative` | `-x` → `x` | Writes one space instead of nothing when the `-` sits between two identifier characters, as in `return-x`. |
| `logical` | `&&`↔`\|\|` | |
| `negate_conditional` | `==`↔`!=`, `<`→`>=`, `<=`→`>`, `>`→`<=`, `>=`→`<` | |
| `remove_call` | Deletes a statement that is only a call. | Skips `fmt.Print*`, `log.*`, `t.Log*` and `b.Log*`. |
| `remove_not` | `!x` → `x` | Writes one space instead of nothing when the `!` sits between two identifier characters, as in `return!x`. |

A mutant can fail to compile: `a - b` on two strings, or a deleted call that leaves an unused variable. Those get `compile_error` and do not count.

### Statuses

| Status | Meaning | Score |
|---|---|---|
| `killed` | A test failed. | detected |
| `timeout` | The run exceeded the timeout and was killed. | detected |
| `survived` | Every test passed. | missed |
| `no_coverage` | No test executes the line, so the mutant did not run. | missed |
| `compile_error` | The mutant did not compile. | not counted |
| `ignored` | An ignore marker switched the mutant off. | not counted |
| `pending` | Listed by `--dry-run`; not run. | not counted |

`mutationScore = (killed + timeout) / (killed + timeout + survived + no_coverage) × 100`. It is `null` (`n/a` in text) when that denominator is 0.

Go coverage is recorded per block of statements. A `case` line counts as executed only when its body ran. A mutant in a `case` expression can therefore get `no_coverage` although the expression was evaluated.

### Ignore marker

Some mutants cannot be killed because the change has no observable effect (an *equivalent* mutant). Switch them off with a comment:

```go
if m.Complexity > maxComplexity { // slopguard-ignore-mutant(boundary): equal values assign the same max
```

* `slopguard-ignore-mutant` ignores every mutant on its line.
* `slopguard-ignore-mutant(boundary,negate_conditional)` ignores only those operators. Unknown ids are dropped.
* On a line that holds only a comment, the marker applies to the next line.

Ignored mutants never run and do not count in the score.

### JSON output

`--json` emits `reportType: "mutation"`, `schemaVersion: "1"`, shared with the other ports. Keys are alphabetical at every level. An excerpt from `sampleapps/todolist`:

```json
{
  "coverageAvailable": true,
  "mutants": [
    {
      "column": 18,
      "file": "store.go",
      "id": "store.go:55:18:negate_conditional",
      "line": 55,
      "method": "Store.All",
      "operator": "negate_conditional",
      "original": "<",
      "replacement": ">=",
      "status": "killed"
    }
  ],
  "notes": [],
  "projectRoot": "/abs/slopguard-go/sampleapps/todolist",
  "reportType": "mutation",
  "runner": "go test",
  "schemaVersion": "1",
  "summary": {
    "compileErrors": 0,
    "fileCount": 3,
    "ignored": 1,
    "killed": 15,
    "mutantCount": 17,
    "mutationScore": 100,
    "noCoverage": 0,
    "pending": 0,
    "survived": 0,
    "timedOut": 1
  },
  "timeoutSeconds": 34
}
```

`id` is `<file>:<line>:<column>:<operator>`. `column` counts Unicode code points. `method` is `null` for code outside a function. `projectRoot`, `runner` and `timeoutSeconds` are `null` when no test ran.

### Safety

`mutate` never writes to your source tree. It writes each mutant to a temporary directory that slopguard-go owns, and the mutant reaches the compiler through `go test -overlay`. The directory is removed when the run ends, including after an error or an interrupt.

Each `go test` run starts in its own process group. A timeout kills the whole group, so a test binary stuck in an infinite loop stops too. On Windows, only the `go` process itself is killed.

SIGINT, SIGTERM and SIGHUP kill the running `go test` and end `mutate` with exit code 130, 143 or 129.

Running the tests executes your code once per mutant. This is the same trust boundary as `analyze`.

## Posture

* **Zero third-party dependencies** — only the Go standard library (`go/ast`, `go/parser`, `encoding/json`, `flag`).
* **The only subprocess slopguard-go spawns is the module's own `go test`.**
* **No network, no telemetry, no writes to your source tree.** See [`SECURITY.md`](SECURITY.md) for the full threat model.
* **MIT licensed** ([`LICENSE`](LICENSE)).

## Library use

Everything the CLI does is exported:

```go
import (
    "github.com/JeevanThandi/slopguard-go/core"
    "github.com/JeevanThandi/slopguard-go/coverage"
)

report, err := coverage.Run(coverage.PipelineArgs{
    SourcePath: "./internal",
    Coverage:   coverage.CoverageSource{Mode: coverage.CoverageAuto},
    Options:    core.DefaultAnalysisOptions(),
})
```

For complexity-only analysis with no I/O, `core.AnalyzeSource([]byte, "file.go")` returns the per-file metrics directly.

Mutation testing has the same shape:

```go
import "github.com/JeevanThandi/slopguard-go/mutation"

report, err := mutation.Run(mutation.Args{
    SourcePath: "./internal",
    Options:    core.DefaultAnalysisOptions(),
})
fmt.Println(core.PrettyMutationReport(report))
```

`core.GenerateMutants([]byte, "file.go")` lists the mutants of one file with no I/O, and `core.PlanMutants` does the same for a directory tree.
