# Changelog

All notable changes to slopguard-go are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project adheres to
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.2.0] — 2026-09-30

### Added

- A `mutate` command: a mutation tester. It makes one small change to the
  source at a time (a mutant), runs `go test` against each mutant, and reports
  the mutants the tests do not catch. Each entry names the file, line, column,
  original text, replacement text and enclosing function. The command follows
  the `mutate` contract shared with the TypeScript, Python, Kotlin and Swift
  ports: the same flags, operator ids, statuses, JSON shape, exit codes and
  error codes.
- Nine operators: `arithmetic`, `boolean_literal`, `boundary`, `increment`,
  `invert_negative`, `logical`, `negate_conditional`, `remove_call`,
  `remove_not`.
- Mutants never touch the source tree. Each one is written to a
  slopguard-owned temporary directory and reaches the compiler through
  `go test -overlay`.
- A plain baseline run, which must pass, and a coverage baseline run. Mutants
  on lines that no test executes get `no_coverage` and do not run.
- A per-mutant timeout (`ceil(3 × baseline) + 30` seconds, or `--timeout`)
  that kills the whole `go test` process group. SIGINT, SIGTERM and SIGHUP end
  the run with exit code 130, 143 or 129.
- The flags `--path`, `--operators`, `--dry-run`, `--fail-under`,
  `--timeout`, `--no-coverage`, `--packages`, `--project-dir`, `--include`,
  `--exclude`, `--no-default-excludes`, `--json`, `--verbose` and `--quiet`.
- The `slopguard-ignore-mutant` comment marker for equivalent mutants.
- A JSON report with `reportType: "mutation"` and `schemaVersion: "1"`.
- The error codes `baseline_failed` and `runner_unavailable`. The shared
  catalogue also lists `mutation_in_progress` and `restore_failed`, which the
  Go port never returns.
- Library API: `mutation.Run`, `core.PlanMutants`, `core.GenerateMutants`,
  `core.ParseOperators`, `core.PrettyMutationReport`,
  `core.JSONMutationReport` and the report types.
- `coverage.TestRunner.Context`, which kills the coverage run's process group
  when the context is done, and `coverage.TestOutcome.ExitCode`.
- CI pins the mutation baseline of `sampleapps/todolist`: 17 mutants, 15
  killed, 0 survived and a score of 100. The same run also reports 1 timed-out
  mutant and 1 ignored mutant. `sampleapps/README.md` explains these numbers.
  The `make mutate-sample` target runs `mutate` on the sample app with
  `--fail-under 100`.

### Changed

- `sampleapps/todolist/store.go` carries an ignore marker on its one
  equivalent mutant (`id < s.nextID` → `<=` in `Store.All`). The code is
  unchanged.
- `docs/mutation-testing.md` points at the new command.
- The README documents `mutate` in a new "Mutation testing" section.
- The threat model in `SECURITY.md` covers `mutate`: the `go test` runs it
  starts and the mutant copies it writes to a temporary directory.

## [0.1.0] — 2026-06-22

Initial alpha release. The Go sibling of slopguard-swift and
slopguard-typescript — same wCRAP formula, same schema-2 JSON, same CLI UX.

### Added

- **wCRAP analyzer** over `go/ast`: cyclomatic complexity (McCabe, comparable
  to `gocyclo`) and cognitive complexity (SonarSource 2023 spec) computed in a
  single pass, blended as `sqrt(cyc × cog)`.
- **Receiver-based type aggregation** — methods attach to their type by
  receiver and roll up package-wide across files, the Go-idiomatic analog of
  the lexical nesting used in the TypeScript/Swift ports.
- **Coverage pipeline** driving the module's own `go test -coverprofile`, with
  `auto`, `--coverage-file` (prebuilt profile), and `--no-coverage` modes.
- **CLI**: `analyze` (default) and `version`, with `--path`, `--threshold`,
  `--project-dir`, `--packages`, `--include`/`--exclude`,
  `--no-default-excludes`, `--json`, `--fail-over`, `--verbose`, `--quiet`.
- Stable, versioned JSON report (`schemaVersion: "2"`) and a human-readable
  text report ranking the top methods by wCRAP.
- Default excludes for `vendor/`, `testdata/`, `*_test.go`, and generated code
  (including files with the `// Code generated … DO NOT EDIT.` header).
- `sampleapps/todolist` reference fixture used as a CI regression baseline.

### Posture

- Zero third-party dependencies (Go standard library only).
- No network, no telemetry, no source mutation.

[Unreleased]: https://github.com/JeevanThandi/slopguard-go/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/JeevanThandi/slopguard-go/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/JeevanThandi/slopguard-go/releases/tag/v0.1.0
