// Package mutation runs `mutate`: it plans mutants with the core package,
// checks that the unmutated tests pass, gathers line coverage with the
// coverage package, and runs `go test` once per mutant. Mutants never touch
// the source tree: each one is written to a slopguard-owned temp directory
// and handed to the compiler with `go test -overlay`.
package mutation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/JeevanThandi/slopguard-go/core"
	"github.com/JeevanThandi/slopguard-go/internal/procgroup"
)

// Runner runs the module's tests once for mutate: the plain baseline or one
// mutant. GoTestRunner is the real implementation; tests inject fakes.
type Runner interface {
	// Run runs the tests. It returns an error only when the tests could not
	// be launched (runner_unavailable) or ctx was cancelled (ctx.Err()).
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}

// RunRequest describes one test run.
type RunRequest struct {
	// ProjectRoot is the module root go test runs in, with symlinks resolved.
	ProjectRoot string
	// Packages is the go test package pattern, for example "./...".
	Packages string
	// OverlayPath is the -overlay config file; "" runs without -overlay.
	OverlayPath string
	// Timeout stops the run after this long; zero means no limit.
	Timeout time.Duration
	// Progress receives the raw test output under --verbose.
	Progress *core.ProgressReporter
}

// RunResult reports how one test run ended.
type RunResult struct {
	// BuildFailed is true when go test printed a `FAIL\t<package> [build
	// failed]` or `[setup failed]` summary line: the code did not compile.
	BuildFailed bool
	Duration    time.Duration
	// ExitCode is the go test exit code; 0 means every test passed.
	ExitCode int
	// Output is the last 8 KiB of the combined stdout and stderr.
	Output string
	// TimedOut is true when the run reached Timeout and was killed.
	TimedOut bool
}

// GoTestRunner runs `go test -overlay=<json> -vet=off -failfast <packages>`
// in the module root. It does not pass -count=1: the test cache is keyed on
// the compiled test binary, so packages a mutant does not affect come back
// "(cached)". Each run gets its own process group, and a timeout kills the
// whole group, test binaries included.
type GoTestRunner struct {
	// GoBinary is the go command to invoke; defaults to "go" on PATH.
	GoBinary string
}

// Run runs go test once.
func (r *GoTestRunner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	goBin := r.GoBinary
	if goBin == "" {
		goBin = "go"
	}
	args := []string{"test"}
	if req.OverlayPath != "" {
		args = append(args, "-overlay="+req.OverlayPath)
	}
	args = append(args, "-vet=off", "-failfast", req.Packages)

	runCtx, cancel := ctx, context.CancelFunc(func() {})
	if req.Timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, req.Timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, goBin, args...)
	procgroup.Prepare(cmd)
	cmd.Dir = req.ProjectRoot
	// The same environment handling as the coverage runner, plus PWD: the go
	// command takes its working directory from PWD and resolves -overlay
	// paths against it, so PWD must spell the root the way the overlay does.
	cmd.Env = append(os.Environ(), "GOFLAGS=", "CI=1", "PWD="+req.ProjectRoot)
	sink := &outputSink{progress: req.Progress}
	cmd.Stdout = sink
	cmd.Stderr = sink

	started := time.Now()
	err := cmd.Run()
	sink.flush()
	result := RunResult{BuildFailed: sink.buildFailed, Duration: time.Since(started), Output: sink.tail.String()}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		result.TimedOut = true
		return result, nil
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil, errors.Is(err, exec.ErrWaitDelay):
		// ErrWaitDelay: go test passed, but a leftover child held its output
		// open past procgroup.WaitDelay.
		return result, nil
	case errors.As(err, &exitErr):
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return result, core.RunnerUnavailable(fmt.Sprintf("could not launch '%s': %v", goBin, err))
}

// outputLimit is how many bytes of test output a run keeps for messages.
const outputLimit = 8 * 1024

// buildFailureMarkers end the per-package summary line go test prints for a
// package that did not compile ([build failed]) or could not be loaded
// ([setup failed]): `FAIL\t<package> [build failed]`, at column 0.
var buildFailureMarkers = [][]byte{[]byte("[build failed]"), []byte("[setup failed]")}

// maxSummaryLine bounds the unterminated line the sink keeps between writes.
// A go test summary line is short, so a longer line cannot be one; the sink
// drops it.
const maxSummaryLine = 4 * 1024

// outputSink keeps the last outputLimit bytes of the output, streams the
// output through under --verbose, and checks every complete line for a build
// failure summary. It matches whole lines, not substrings: a failing test
// that logs fixture text containing a marker prints it indented, and that
// must stay a kill. os/exec never calls Write concurrently when stdout and
// stderr share a sink.
type outputSink struct {
	progress    *core.ProgressReporter
	tail        bytes.Buffer
	line        []byte // the current line, until its newline arrives
	overlong    bool   // the current line passed maxSummaryLine and is dropped
	buildFailed bool
}

func (s *outputSink) Write(p []byte) (int, error) {
	s.progress.Raw(p)
	for rest := p; len(rest) > 0; {
		newline := bytes.IndexByte(rest, '\n')
		if newline < 0 {
			s.extendLine(rest)
			break
		}
		s.extendLine(rest[:newline])
		s.endLine()
		rest = rest[newline+1:]
	}
	s.tail.Write(p)
	if excess := s.tail.Len() - outputLimit; excess > 0 {
		s.tail.Next(excess)
	}
	return len(p), nil
}

// flush checks the last line when the output ended without a newline. Call
// it once the command has exited.
func (s *outputSink) flush() { s.endLine() }

func (s *outputSink) extendLine(chunk []byte) {
	if s.overlong {
		return
	}
	if len(s.line)+len(chunk) > maxSummaryLine {
		s.line, s.overlong = s.line[:0], true
		return
	}
	s.line = append(s.line, chunk...)
}

func (s *outputSink) endLine() {
	if !s.overlong && isBuildFailureSummary(s.line) {
		s.buildFailed = true
	}
	s.line, s.overlong = s.line[:0], false
}

// isBuildFailureSummary reports whether line is go test's summary for a
// package that did not build: it starts with FAIL and ends with a marker.
func isBuildFailureSummary(line []byte) bool {
	line = bytes.TrimSuffix(line, []byte("\r"))
	if !bytes.HasPrefix(line, []byte("FAIL")) {
		return false
	}
	for _, marker := range buildFailureMarkers {
		if bytes.HasSuffix(line, marker) {
			return true
		}
	}
	return false
}
