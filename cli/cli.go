// Package cli wires slopguard-go's command-line surface: the default `analyze`
// command, the `mutate` command and a `version` command. It is a thin shim
// over the core, coverage and mutation packages so the wiring can be
// exercised in-process by tests.
package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/JeevanThandi/slopguard-go/core"
)

const usage = `slopguard-go ` + core.Version + ` — CRAP (Change Risk Anti-Patterns) guardrail for Go.

slopguard-go finds complex, undertested code by combining cyclomatic and
cognitive complexity (parsed via go/ast) with line coverage gathered from the
module's own test suite (go test). Coverage is an artifact of the analysis, not
an input.

  Formula:  wCRAP(m) = (cyc × cog) × (1 − cov/100)³ + sqrt(cyc × cog)
  Default crappy threshold: 30.

Usage:
  slopguard-go [analyze] [flags]
  slopguard-go mutate [flags]
  slopguard-go version

Commands:
  analyze   Walk a directory of Go sources, drive go test for coverage, emit a
            wCRAP report (text or JSON). This is the default command.
  mutate    Make small changes to the source (mutants), run go test against
            each one, and report the mutants the tests do not catch. The
            source tree is never written: mutants go through go test -overlay.
  version   Print version metadata as JSON.

Run 'slopguard-go analyze --help' or 'slopguard-go mutate --help' for the flags.
`

// Run parses args (excluding the program name) and executes the selected
// command. It returns the process exit code: 0 success, 1 error, 2 when
// --fail-over is exceeded or the mutation score is below --fail-under, and
// 128 + the signal number when a signal interrupts mutate (130 for SIGINT,
// 143 for SIGTERM). stdout carries the report; stderr carries progress and
// errors.
func Run(args []string, stdout, stderr io.Writer) int {
	// analyze is the default; everything that isn't a recognised subcommand is
	// treated as analyze flags so a bare `slopguard-go --path ./pkg` (and
	// zero-config `slopguard-go`) just works.
	rest := args
	if len(args) > 0 {
		switch args[0] {
		case "version":
			return runVersion(stdout)
		case "-h", "--help", "help":
			fmt.Fprint(stdout, usage)
			return 0
		case "--version":
			fmt.Fprintln(stdout, core.Version)
			return 0
		case "analyze":
			rest = args[1:]
		case "mutate":
			return runMutate(args[1:], stdout, stderr)
		}
	}
	return runAnalyze(rest, stdout, stderr)
}

func runVersion(stdout io.Writer) int {
	payload := map[string]string{"name": core.ToolName, "version": core.Version}
	out, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Fprintln(stdout, string(out))
	return 0
}
