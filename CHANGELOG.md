# Changelog

All notable changes to slopguard-go are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/), and the project adheres to
[Semantic Versioning](https://semver.org/).

## [0.1.0] — Unreleased

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
