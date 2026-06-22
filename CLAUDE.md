# CLAUDE.md — slopguard-go

Guidance for Claude when working in this repo. Read this first.

## What this is

`slopguard-go` is the **Go port** of slopguard — a CRAP (Change Risk
Anti-Patterns) guardrail. It scores every function/method by **complexity ×
lack-of-coverage** (wCRAP) and emits a text or JSON report you can gate CI on.

**Parity mandate:** `slopguard-go` is one of three sibling ports that must stay
**behaviourally aligned**. The wCRAP formula, the schema-2 JSON shape, the CLI
UX (flags, exit codes, stderr/stdout split), and the error-envelope shape are a
shared contract — don't change them unilaterally here, or you break cross-tool
consumers and drift from the siblings:

- TypeScript (the reference port for intent): https://github.com/JeevanThandi/slopguard-typescript
- Swift: https://github.com/JeevanThandi/SlopGuard-Swift

## Environment gotcha (important)

`go` is **not on PATH** in this environment and Homebrew is absent. A Go SDK is
installed at `~/.local/go-sdk/go`. Prefix every Go command with:

```bash
export PATH="$HOME/.local/go-sdk/go/bin:$PATH" && export GOTOOLCHAIN=local
```

`GOTOOLCHAIN=local` stops the toolchain auto-downloading (go.mod pins `go 1.23`).
The built binary itself shells out to `go test`, so that same PATH must be
exported in any shell that runs the binary against real coverage — otherwise the
spawned `go test` can't launch and `analyze` exits 1 with empty stdout.

## Build / test / run

A `Makefile` wraps the common tasks (still needs the PATH export above):

```bash
make build        # -> ./slopguard-go
make test         # go test -race -coverprofile=coverage.txt ./...
make vet
make fmt-check    # gofmt -l; CI fails on unformatted files
make dogfood      # analyze own ./core, complexity-only, --fail-over 300
```

Run it against itself:

```bash
./slopguard-go analyze --path ./core              # full, with go test coverage
./slopguard-go analyze --path . --no-coverage     # fast, complexity-only
./slopguard-go analyze --path . --json | jq '.methods | sort_by(-.crap)[:10]'
```

## Architecture

Three importable packages + a thin binary. No third-party dependencies —
**standard library only** (`go/ast`, `go/parser`, `encoding/json`, `flag`).

- **`core/`** — pure analysis, no subprocesses. The CRAP formula (`crap.go`),
  the single-pass AST analyzer (`complexity.go`), models + JSON tags
  (`models.go`), aggregator (`aggregator.go`), glob/excludes (`glob.go`,
  `diranalyzer.go`), formatters (`format.go`), errors, progress, version.
- **`coverage/`** — drives `go test`, parses the profile, joins coverage.
  `runner.go` (spawns `go test -coverprofile -covermode=count -coverpkg`),
  `profile.go` (parse), `index.go` (per-line lookup + path resolution),
  `projectroot.go` (find go.mod / module path), `pipeline.go` (orchestrator with
  auto / prebuilt / none modes).
- **`cli/`** — `Run(args, stdout, stderr) int`; `analyze` (default) + `version`.
- **`cmd/slopguard-go/`** — `os.Exit(cli.Run(...))` shim.
- **`sampleapps/todolist/`** — a separate module used as a CI regression
  baseline (10 methods, 0 crappy, ~98% coverage). It has its own `go.mod`, so
  the parent `go test ./...` ignores it, and `**/sampleapps/**` is excluded from
  scans.

## Key invariants — don't break these

- **wCRAP formula** (`core/crap.go`): `CrapScore(comp, cov) = comp²(1−cov/100)³ +
  comp`, fed `comp = sqrt(cyclomatic × cognitive)`. Default threshold 30.
- **Cyclomatic** counting matches `gocyclo` (if/for/range/non-default case/`&&`/`||`,
  base 1). **Cognitive** follows the SonarSource 2023 spec (whole switch/select
  = one increment, nesting-amplified, boolean-run collapse, labelled jumps
  fundamental, closures bump nesting but get no entry, early exits free). The
  `core/complexity*_test.go` files pin exact expected numbers — if you touch the
  analyzer, those tests are the contract.
- **Go-specific: receiver-based type aggregation.** Unlike the TS/Swift ports
  (lexical nesting), Go methods attach to a type by **receiver** and are rolled
  up package-wide in `core/aggregator.go` (`aggregateTypes`, keyed by
  `(dir, typeName)`). The analyzer only records type *declarations*; it does NOT
  populate method membership. This is the single biggest divergence from the
  siblings — preserve it.
- **JSON field order is alphabetical** in the `models.go` struct definitions on
  purpose (diff-stable output mirroring the siblings' sorted-key encoding). Keep
  new fields alphabetical.
- **`typeName` is `*string`** → `null` for free functions, receiver name for
  methods. `generatedAt` uses `2006-01-02T15:04:05.000Z` (UTC, ms).
- **Generated files are skipped** (`// Code generated … DO NOT EDIT.` header) in
  `core/fileanalyzer.go`, on top of the glob excludes in `core/diranalyzer.go`.
- **Coverage is an artifact, never an input.** `auto` mode runs the module's own
  tests; failing tests don't abort (a note is attached), but a build failure
  with only a header-only profile is `test_run_failed`.

## Conventions

- Always `gofmt` before finishing — CI's `fmt-check` fails on unformatted files.
- Errors are `*core.SlopguardError` with a stable `Code`; surface them via
  `core.EnvelopeFor`. Exit codes: 0 ok, 1 error, 2 `--fail-over` exceeded.
- **Test coverage floor is 95%** (CI gate, `coverpkg` across all three
  packages); current ≈97%. The remaining uncovered lines are deliberately
  unreachable defensive guards (`filepath.Abs`/`MkdirTemp` failures, NaN JSON
  marshal). Real `go test` integration tests are guarded by `testing.Short()`
  and an `exec.LookPath("go")` check — run with `-short` to skip them.

## When verifying a change

```bash
export PATH="$HOME/.local/go-sdk/go/bin:$PATH" && export GOTOOLCHAIN=local
gofmt -l core coverage cli cmd sampleapps   # must be empty
go vet ./... && go test -race ./...
./slopguard-go analyze --path ./sampleapps/todolist --json --quiet \
  | jq '{methods:.summary.methodCount, crappy:.summary.crappyMethodCount}'
# expect {"methods":10,"crappy":0} — the regression baseline
```
