# CLAUDE.md — slopguard-go

Guidance for Claude when working in this repo. Read this first.

## What this is

`slopguard-go` is the **Go port** of slopguard — a CRAP (Change Risk
Anti-Patterns) guardrail. It scores every function/method by **complexity ×
lack-of-coverage** (wCRAP) and emits a text or JSON report you can gate CI on.
Its `mutate` command is a mutation tester: it changes the code one small step
at a time, runs `go test` against each change, and reports the changes the
tests do not catch.

**Parity mandate:** `slopguard-go` is one of five sibling ports that must stay
**behaviourally aligned**. The wCRAP formula, the schema-2 JSON shape, the CLI
UX (flags, exit codes, stderr/stdout split), and the error-envelope shape are a
shared contract — don't change them unilaterally here, or you break cross-tool
consumers and drift from the siblings:

- TypeScript (the reference port for intent): https://github.com/JeevanThandi/slopguard-typescript
- Swift: https://github.com/JeevanThandi/SlopGuard-Swift
- Kotlin: https://github.com/JeevanThandi/slopguard-kotlin
- Python: https://github.com/JeevanThandi/slopguard-python

The `mutate` command follows a second shared contract, implemented by five
ports (TypeScript, Go, Python, Kotlin, Swift): the same flags, operator ids,
statuses, JSON report (`reportType: "mutation"`, schema 1), text layout,
progress lines, note wording, exit codes and error codes. The TypeScript port
is the reference (`src/mutation/`, `src/core/mutation/`,
`src/core/formatting/mutationReportFormatter.ts`). Change any of these only
together with the siblings.

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
make mutate-sample  # mutate sampleapps/todolist, --fail-under 100
```

Run it against itself:

```bash
./slopguard-go analyze --path ./core              # full, with go test coverage
./slopguard-go analyze --path . --no-coverage     # fast, complexity-only
./slopguard-go analyze --path . --json | jq '.methods | sort_by(-.crap)[:10]'
./slopguard-go mutate --path ./core/crap.go --packages ./core/...   # one file, core tests only
./slopguard-go mutate --path ./core --dry-run                       # list mutants, run nothing
```

## Architecture

Four importable packages, one internal helper and a thin binary. No
third-party dependencies — **standard library only** (`go/ast`, `go/parser`,
`encoding/json`, `flag`, `os/exec`).

- **`core/`** — pure analysis, no subprocesses. The CRAP formula (`crap.go`),
  the single-pass AST analyzer (`complexity.go`), models + JSON tags
  (`models.go`), aggregator (`aggregator.go`), glob/excludes (`glob.go`,
  `diranalyzer.go`), formatters (`format.go`), errors, progress, version.
  Mutation testing, still pure: `mutant.go` (operator ids, statuses, `Mutant`,
  `MutationReport`, summary + score, standard notes), `mutantgen.go` (the AST
  mutant generator, ignore markers, enclosing-method lookup via the complexity
  analyzer, `PlanMutants` over the same directory walk as `AnalyzeTree`),
  `mutantformat.go` (text + JSON report).
- **`coverage/`** — drives `go test`, parses the profile, joins coverage.
  `runner.go` (spawns `go test -coverprofile -covermode=count -coverpkg`; an
  optional `Context` kills its process group),
  `profile.go` (parse), `index.go` (per-line lookup + path resolution),
  `projectroot.go` (find go.mod / module path), `pipeline.go` (orchestrator with
  auto / prebuilt / none modes).
- **`mutation/`** — runs `mutate`. `runner.go` (`GoTestRunner`: `go test
  -overlay=<json> -vet=off -failfast <packages>`, bounded output tail,
  `[build failed]` / `[setup failed]` detection, timeout), `pipeline.go`
  (`Run`: plan → plain baseline → coverage baseline via the coverage package →
  one run per mutant → report; overlay files in a slopguard-owned temp dir).
- **`internal/procgroup/`** — starts a command in its own process group and
  kills the whole group when its context is done (`Setpgid` under the `unix`
  build tag; other platforms kill the direct child only).
- **`cli/`** — `Run(args, stdout, stderr) int`; `analyze` (default), `mutate`
  (`mutate.go`, including SIGINT/SIGTERM/SIGHUP handling) + `version`.
- **`cmd/slopguard-go/`** — `os.Exit(cli.Run(...))` shim.
- **`sampleapps/todolist/`** — a separate module used as a CI regression
  baseline (10 methods, 0 crappy, ~98% coverage; mutation: 17 mutants, 15
  killed, 1 timeout, 1 ignored, 0 survived). It has its own `go.mod`, so the
  parent `go test ./...` ignores it, and `**/sampleapps/**` is excluded from
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

### `mutate` invariants

- **`mutate` never writes to the source tree.** Each mutant goes to a backing
  file in a slopguard-owned temp dir and reaches the compiler through
  `go test -overlay`; `Run` removes the temp dir on every exit path. Go needs
  no workspace guard, lock or journal (the in-place ports do).
- **Overlay keys must match the go command's path spelling**, or go silently
  ignores the overlay and every mutant "survives". go test runs in the
  symlink-resolved module root with `PWD` set to it, and each key is that root
  joined with the file's path inside it (`pathWithin` compares directories by
  `os.SameFile`, so a symlinked `/tmp` or a different letter case still
  matches). `mutation/integration_test.go` runs through a symlink to guard this.
- **The plain baseline alone decides pass/fail.** It is the exact mutant
  command with an empty overlay and no timeout; non-zero → `baseline_failed`.
  Its wall time sets the timeout: `ceil(3 × seconds) + 30`. The coverage
  baseline is never fatal: a non-zero exit with data keeps the data (note),
  no usable data runs every mutant (note).
- **`no_coverage`** = the coverage index reports exactly 0% for `[line, line]`;
  unknown lines run. Go coverage is per statement block, so a `case` line
  counts as executed only when its body ran.
- **Classification:** timeout → `timeout`; exit 0 → `survived`; a summary
  line that starts with `FAIL` and ends with `[build failed]` or
  `[setup failed]` → `compile_error`; other non-zero → `killed`. A launch
  failure is `runner_unavailable`. The sink matches whole lines, not
  substrings: a failing test that logs fixture text containing a marker
  prints it indented, and it stays `killed`.
- **Process groups:** every `go test` that mutate starts runs in its own
  process group; a timeout or a signal kills the whole group, so a hanging
  test binary never outlives the run. Signals end the run with 128 + signal
  number (130 SIGINT, 143 SIGTERM, 129 SIGHUP). `analyze` keeps the old
  behaviour (no process group, no context).
- **No positional arguments.** The `flag` package stops at the first one and
  leaves every later flag unparsed, so `mutate ./pkg --dry-run` would start a
  full run over `.`. `runMutate` rejects any positional argument with
  `invalid_argument`. `analyze` has the same parser behaviour and still
  ignores them (pre-existing).
- **Generator rules** (pinned by `core/mutantgen_test.go`): only syntax nodes
  mutate; `true`/`false` are `*ast.Ident`, so declared names, selectors and
  labels spelled `true`/`false` are skipped; `+`/`+=` with a stringy operand
  (recursively through parentheses) is skipped; `remove_call` only takes
  expression statements directly in a block or clause body and skips
  `fmt.Print*`, `log.*`, `t.Log*`, `b.Log*`; `remove_not` and
  `invert_negative` replace the token with one space instead of nothing when
  both neighbouring bytes are identifier bytes (`return!x` must not become
  `returnx`); positions come from raw byte offsets (`//line` directives are
  ignored) and columns count code points.
- **Ignore marker:** plain text search per line; the bare marker ignores every
  operator, `(ids)` only those (unknown ids dropped, an unclosed list runs to
  the end of the line); on a comment-only line it applies to the next line.
  Go refinement: a line starting with `*` counts as a comment only when the `*`
  is followed by a space, `/` or the line end, because `*p = x` is code.
- **Report shape:** `Mutant`, `MutationSummary` and `MutationReport` fields
  are alphabetical, like `models.go`. The JSON encoder does not escape `<`,
  `>` and `&`. Text layout, note wording and progress lines mirror the
  TypeScript reference character for character.

## Conventions

- Always `gofmt` before finishing — CI's `fmt-check` fails on unformatted files.
- Errors are `*core.SlopguardError` with a stable `Code`; surface them via
  `core.EnvelopeFor`. Exit codes: 0 ok, 1 error, 2 `--fail-over` exceeded or
  mutation score below `--fail-under`, 130/143/129 when a signal stops
  `mutate`.
- **Test coverage floor is 95%** (CI gate, `coverpkg` across `core`,
  `coverage`, `cli`, `mutation` and `internal`); current ≈98.9%. Most of the
  remaining uncovered lines are defensive guards (`filepath.Abs`/`MkdirTemp`/
  `filepath.Rel` failures, the `totalLines == 0` guard in `weightedCoverage`,
  NaN JSON marshal). A few reachable branches have no test: the hidden-file
  and symlink skips and the `AnalyzeFile` error in `core/diranalyzer.go`, the
  types sort comparator and the no-data fallback of `coverageFor` in
  `core/aggregator.go`, the scanner error in `coverage/profile.go`, and the
  runner-error return (`test_run_failed`) in `coverage/pipeline.go`. Real
  `go test` integration tests are guarded by `testing.Short()` and an
  `exec.LookPath("go")` check — run with `-short` to skip them.

## When verifying a change

```bash
export PATH="$HOME/.local/go-sdk/go/bin:$PATH" && export GOTOOLCHAIN=local
gofmt -l core coverage cli cmd mutation internal sampleapps   # must be empty
go vet ./... && go test -race ./...
./slopguard-go analyze --path ./sampleapps/todolist --json --quiet \
  | jq '{methods:.summary.methodCount, crappy:.summary.crappyMethodCount}'
# expect {"methods":10,"crappy":0} — the regression baseline
./slopguard-go mutate --path ./sampleapps/todolist --json --quiet \
  | jq '{mutants:.summary.mutantCount, killed:.summary.killed, survived:.summary.survived, score:.summary.mutationScore}'
# expect {"mutants":17,"killed":15,"survived":0,"score":100} — the mutation baseline (~40 s:
# one mutant, id-- in Store.All, loops forever and hits the timeout)
```

`go vet` includes the stdversion check: it flags standard-library APIs newer
than the `go 1.23` in go.mod (for example `t.Context`), which CI's Go 1.23
job could not build.
