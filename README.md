# slopguard-go

[![CI](https://github.com/JeevanThandi/slopguard-go/actions/workflows/ci.yml/badge.svg)](https://github.com/JeevanThandi/slopguard-go/actions/workflows/ci.yml)

> **CRAP (Change Risk Anti-Patterns) guardrail for Go.**

> ⚠️ **Alpha (v0.1.x).** The analyzer is stable and self-tested, but the CLI surface and JSON schema may still change before v1.0.

`slopguard-go` measures **complex, undertested code** in Go modules. It computes a weighted CRAP score combining cyclomatic and cognitive complexity with line coverage, and prints a structured report you can pipe into `jq` or fail CI on. It is the Go sibling of [slopguard-swift](https://github.com/JeevanThandi/SlopGuard-Swift) and [slopguard-typescript](https://github.com/JeevanThandi/slopguard-typescript) — same formula, same schema, same UX.

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

## Posture

* **Zero third-party dependencies** — only the Go standard library (`go/ast`, `go/parser`, `encoding/json`, `flag`).
* **The only subprocess slopguard-go spawns is the module's own `go test`.**
* **No network, no telemetry, no source mutation.** See [`SECURITY.md`](SECURITY.md) for the full threat model.
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
