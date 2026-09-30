package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/JeevanThandi/slopguard-go/core"
	"github.com/JeevanThandi/slopguard-go/mutation"
)

const mutateHelp = `Mutate Go source one small change at a time, run go test against each
mutant, and report the mutants the tests do not catch.

A mutant is one small change: > becomes >=, + becomes -, && becomes ||, true
becomes false, a ! or a unary minus is removed, or a call statement is
deleted. A mutant is killed when a test fails and survives when every test
still passes. mutate never writes to the source tree: each mutant reaches the
compiler through go test -overlay.

Statuses: killed, survived, timeout (counted as killed), no_coverage (no test
runs the line), compile_error (excluded from the score), ignored.
Operators: arithmetic, boolean_literal, boundary, increment, invert_negative,
logical, negate_conditional, remove_call, remove_not.
Ignore marker, on the mutated line or on a comment line above it:
  // slopguard-ignore-mutant(boundary): equal values assign the same max

Examples:
  slopguard-go mutate --path ./internal/auth
  slopguard-go mutate --path ./internal/auth/token.go        # one file
  slopguard-go mutate --path . --operators boundary,negate_conditional
  slopguard-go mutate --path . --dry-run                     # list mutants, run nothing
  slopguard-go mutate --path . --json | jq '.mutants[] | select(.status == "survived")'
  slopguard-go mutate --path . --fail-under 80               # fail CI below an 80% score

Flags:
`

// optionalString is a string flag that records whether it was passed, so
// `--timeout ""` is rejected rather than treated as unset.
type optionalString struct {
	value string
	set   bool
}

func (o *optionalString) String() string { return o.value }
func (o *optionalString) Set(v string) error {
	o.value, o.set = v, true
	return nil
}

// runMutate implements `slopguard-go mutate`. It returns the exit code: 0 ok,
// 1 error, 2 when the mutation score is below --fail-under, and 128 + the
// signal number when a signal stops the run.
func runMutate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mutate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, mutateHelp)
		fs.PrintDefaults()
	}

	var (
		path       = fs.String("path", ".", "Directory of Go sources, or a single .go file, to mutate.")
		projectDir = fs.String("project-dir", "", "Module root that go test runs in. Defaults to the nearest go.mod above --path.")
		packages   = fs.String("packages", "./...", "Go package pattern for the test runs (and -coverpkg for the coverage run).")
		noCoverage = fs.Bool("no-coverage", false, "Skip the coverage run and test every mutant, including mutants on lines no test executes.")
		dryRun     = fs.Bool("dry-run", false, "List the mutants without running any tests.")
		jsonOut    = fs.Bool("json", false, "Emit JSON to stdout (default is pretty text).")
		noDefaults = fs.Bool("no-default-excludes", false, "Skip built-in excludes (vendor, *_test.go, generated code, testdata).")
		verbose    = fs.Bool("verbose", false, "Stream go test output to stderr.")
		quiet      = fs.Bool("quiet", false, "Suppress all progress chatter on stderr.")
	)
	var include, exclude, operators stringSlice
	var timeout, failUnder optionalString
	fs.Var(&include, "include", "Only mutate files matching this `glob`. Repeatable.")
	fs.Var(&exclude, "exclude", "Extra `glob` of files/dirs to exclude (combined with defaults). Repeatable.")
	fs.Var(&operators, "operators", "Comma-separated mutation operator `ids` to apply. Repeatable. Defaults to all.")
	fs.Var(&timeout, "timeout", fmt.Sprintf("Per-mutant test timeout in `seconds`. Defaults to 3 × the baseline run time + %d.", mutation.TimeoutGraceSeconds))
	fs.Var(&failUnder, "fail-under", "Exit with code 2 if the mutation `score` (0-100) is below this value. Useful in CI.")
	// Short aliases.
	fs.StringVar(path, "p", ".", "Alias for --path.")
	fs.BoolVar(verbose, "v", false, "Alias for --verbose.")

	if err := fs.Parse(args); err != nil {
		// flag already printed the error/usage.
		return 1
	}

	emitErr := func(err error) {
		env := core.EnvelopeFor(err)
		if *jsonOut {
			fmt.Fprintln(stderr, core.ErrorJSON(env))
		} else {
			fmt.Fprintln(stderr, core.ErrorTextLine(env))
		}
	}

	// The flag package stops at the first positional argument and leaves every
	// flag after it unparsed. Running anyway would drop those flags silently,
	// so `mutate ./pkg --dry-run` would start a full run over '.'.
	if fs.NArg() > 0 {
		emitErr(core.InvalidArgument(fs.Arg(0), "unexpected positional argument; pass the path with --path"))
		return 1
	}

	operatorIDs, err := core.ParseOperators(operators)
	if err != nil {
		emitErr(err)
		return 1
	}
	timeoutSeconds, err := parseTimeout(timeout)
	if err != nil {
		emitErr(err)
		return 1
	}
	threshold, err := parseFailUnder(failUnder)
	if err != nil {
		emitErr(err)
		return 1
	}

	options := core.AnalysisOptions{IncludeGlobs: include}
	if *noDefaults {
		options.ExcludeGlobs = append([]string{}, exclude...)
	} else {
		options.ExcludeGlobs = append(append([]string{}, core.DefaultExcludeGlobs...), exclude...)
	}
	var projectRoot string
	if *projectDir != "" {
		projectRoot, _ = filepath.Abs(expandTilde(*projectDir))
	}
	sourcePath, _ := filepath.Abs(expandTilde(*path))
	progress := resolveProgress(stderr, *verbose, *quiet)

	ctx, caught, stop := interruptContext()
	defer stop()
	report, err := mutation.Run(mutation.Args{
		SourcePath:     sourcePath,
		Options:        options,
		Operators:      operatorIDs,
		ProjectDir:     projectRoot,
		Packages:       *packages,
		NoCoverage:     *noCoverage,
		TimeoutSeconds: timeoutSeconds,
		DryRun:         *dryRun,
		Progress:       progress,
		Context:        ctx,
	})
	if sig := caught(); sig != nil {
		progress.Phase("interrupted")
		return signalExitCode(sig)
	}
	if err != nil {
		emitErr(err)
		return 1
	}

	if *jsonOut {
		out, jerr := core.JSONMutationReport(report)
		if jerr != nil {
			emitErr(jerr)
			return 1
		}
		fmt.Fprintln(stdout, out)
	} else {
		fmt.Fprint(stdout, core.PrettyMutationReport(report))
	}

	score := report.Summary.MutationScore
	if failUnder.set && !*dryRun && score != nil && *score < threshold {
		fmt.Fprintf(stderr, "%s: mutation score %.2f%% is below --fail-under %.2f\n", core.ToolName, *score, threshold)
		return 2
	}
	return 0
}

// parseTimeout validates --timeout: a positive number of seconds. Zero means
// the flag was not passed and the timeout is computed from the baseline.
func parseTimeout(flagValue optionalString) (float64, error) {
	if !flagValue.set {
		return 0, nil
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(flagValue.value), 64)
	if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
		return 0, core.InvalidArgument("--timeout", "not a positive number: "+flagValue.value)
	}
	return seconds, nil
}

// parseFailUnder validates --fail-under: any finite number.
func parseFailUnder(flagValue optionalString) (float64, error) {
	if !flagValue.set {
		return 0, nil
	}
	score, err := strconv.ParseFloat(strings.TrimSpace(flagValue.value), 64)
	if err != nil || math.IsNaN(score) || math.IsInf(score, 0) {
		return 0, core.InvalidArgument("--fail-under", "not a number: "+flagValue.value)
	}
	return score, nil
}

// notifySignals subscribes c to the signals that interrupt mutate and returns
// the unsubscribe function. Tests replace it to deliver a signal without
// sending one to the test process.
var notifySignals = func(c chan<- os.Signal) (stop func()) {
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	return func() { signal.Stop(c) }
}

// interruptContext returns a context that is cancelled on SIGINT, SIGTERM or
// SIGHUP, a function that reports the signal received (nil if none), and a
// function that stops listening. Cancelling the context makes mutation.Run
// kill the running go test process group and return.
func interruptContext() (ctx context.Context, caught func() os.Signal, stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 1)
	unsubscribe := notifySignals(signals)
	var mu sync.Mutex
	var received os.Signal
	done := make(chan struct{})
	go func() {
		select {
		case sig := <-signals:
			mu.Lock()
			received = sig
			mu.Unlock()
			cancel()
		case <-done:
		}
	}()
	caught = func() os.Signal {
		mu.Lock()
		defer mu.Unlock()
		return received
	}
	stop = func() {
		close(done)
		unsubscribe()
		cancel()
	}
	return ctx, caught, stop
}

// signalExitCode is the conventional exit code for a process stopped by sig:
// 128 + the signal number (130 for SIGINT, 143 for SIGTERM, 129 for SIGHUP).
func signalExitCode(sig os.Signal) int {
	if s, ok := sig.(syscall.Signal); ok {
		return 128 + int(s)
	}
	return 130
}
