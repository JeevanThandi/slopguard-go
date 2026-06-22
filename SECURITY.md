# Security Policy

## Reporting a vulnerability

Email **jeevanthandi@googlemail.com** with details and reproduction steps. Please do not open a public issue for security-sensitive reports. You'll get an acknowledgement within a few days.

## Threat model

slopguard-go is a read-only static analyzer with one controlled subprocess. Concretely:

* **No network, no telemetry.** slopguard-go never makes network requests and collects no usage data.
* **No source mutation.** It reads `.go` files and never writes to them. Coverage profiles are written only to a temporary directory the tool owns, and that directory is removed when the run finishes.
* **One subprocess.** In the default (`auto`) coverage mode the only process slopguard-go spawns is `go test`, run in your module root with `-coverprofile`/`-coverpkg`. Your tests run as they always do — slopguard-go does not inject code or override your test configuration. Use `--no-coverage` to spawn nothing, or `--coverage-file` to join a profile you already produced.
* **Running tests executes your code.** Because gathering coverage means running your test suite, `auto` mode executes whatever your tests execute. This is the same trust boundary as running `go test` yourself. In untrusted checkouts, prefer `--no-coverage` (complexity-only) or review the suite first.

## Supply chain

* **Zero third-party dependencies.** slopguard-go is built entirely on the Go standard library. There is no transitive dependency surface to audit beyond the Go toolchain itself.
* Releases are tagged from CI; the module is `go install`-able directly from source.

## Supported versions

slopguard-go is alpha (v0.1.x). Security fixes land on the latest minor release.
