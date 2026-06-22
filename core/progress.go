package core

import (
	"fmt"
	"io"
)

// Verbosity controls how much progress chatter a ProgressReporter emits.
type Verbosity int

const (
	// Silent swallows everything. Default for library callers.
	Silent Verbosity = iota
	// Normal emits phase markers only.
	Normal
	// Verbose additionally streams raw subprocess output.
	Verbose
)

// ProgressReporter is the sink for human-readable progress chatter from
// long-running operations (directory walks, test runs, coverage parsing). It
// always writes to a side channel (stderr in the CLI) so it can't pollute the
// main result stream — `--json` consumers piping into jq stay clean.
type ProgressReporter struct {
	verbosity Verbosity
	out       io.Writer // phase markers; nil for silent
}

// SilentReporter discards all progress. Use for library calls.
func SilentReporter() *ProgressReporter {
	return &ProgressReporter{verbosity: Silent}
}

// NewReporter wires a reporter to w (typically os.Stderr) at the given
// verbosity.
func NewReporter(w io.Writer, verbosity Verbosity) *ProgressReporter {
	return &ProgressReporter{verbosity: verbosity, out: w}
}

// Phase emits a phase marker, prefixed with "slopguard: " so it's
// distinguishable from go-test chatter when both share stderr.
func (p *ProgressReporter) Phase(message string) {
	if p == nil || p.out == nil || p.verbosity == Silent {
		return
	}
	fmt.Fprintf(p.out, "slopguard: %s\n", message)
}

// Raw passes subprocess bytes through verbatim. Only the verbose reporter
// writes; every other reporter discards.
func (p *ProgressReporter) Raw(chunk []byte) {
	if p == nil || p.out == nil || p.verbosity != Verbose {
		return
	}
	p.out.Write(chunk)
}

// IsVerbose reports whether raw subprocess output is being streamed.
func (p *ProgressReporter) IsVerbose() bool {
	return p != nil && p.verbosity == Verbose
}
