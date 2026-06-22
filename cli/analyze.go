package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/JeevanThandi/slopguard-go/core"
	"github.com/JeevanThandi/slopguard-go/coverage"
)

// stringSlice is a repeatable string flag (e.g. --include a --include b).
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ",") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

const analyzeHelp = `Analyze a directory or file and print a CRAP report.

slopguard-go drives the module's own test suite (go test) with a forced
coverage profile to gather line coverage. Coverage is an artifact of the
analysis, not an input.

Examples:
  slopguard-go                                              # zero-config: analyze .
  slopguard-go analyze --path ./internal --threshold 30
  slopguard-go analyze --path . --json | jq '.methods | sort_by(-.crap)[:10]'
  slopguard-go analyze --path . --fail-over 50             # fail CI when any method's CRAP > 50
  slopguard-go analyze --path ./pkg --no-coverage          # skip the test run (complexity-only)
  slopguard-go analyze --path ./pkg --packages ./pkg/...   # scope the test run
  slopguard-go analyze --path . --coverage-file cover.out  # join a pre-built go test profile

Flags:
`

func runAnalyze(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, analyzeHelp)
		fs.PrintDefaults()
	}

	var (
		path       = fs.String("path", ".", "Directory of Go sources, or a single .go file.")
		threshold  = fs.Float64("threshold", core.DefaultCrapThreshold, "CRAP threshold above which a method/type is crappy.")
		projectDir = fs.String("project-dir", "", "Module root that go test runs in. Defaults to the nearest go.mod above --path.")
		packages   = fs.String("packages", "./...", "Go package pattern for the test run and coverage instrumentation (-coverpkg).")
		noCoverage = fs.Bool("no-coverage", false, "Skip the test run and report complexity only (every method shows 0% coverage).")
		coverFile  = fs.String("coverage-file", "", "Pre-built go test coverage profile to join instead of running tests.")
		jsonOut    = fs.Bool("json", false, "Emit JSON to stdout (default is pretty text).")
		noDefaults = fs.Bool("no-default-excludes", false, "Skip built-in excludes (vendor, *_test.go, generated code, testdata).")
		failOver   = fs.Float64("fail-over", notSet, "Exit with code 2 if any method's CRAP exceeds this value. Useful in CI.")
		verbose    = fs.Bool("verbose", false, "Stream go test output and other subprocess chatter to stderr.")
		quiet      = fs.Bool("quiet", false, "Suppress all progress chatter on stderr.")
	)
	var include, exclude stringSlice
	fs.Var(&include, "include", "Glob of files to include. Repeatable.")
	fs.Var(&exclude, "exclude", "Extra glob of files/dirs to exclude (combined with defaults). Repeatable.")
	// Short aliases.
	fs.StringVar(path, "p", ".", "Alias for --path.")
	fs.Float64Var(threshold, "t", core.DefaultCrapThreshold, "Alias for --threshold.")
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

	// Build analysis options.
	options := core.AnalysisOptions{IncludeGlobs: include}
	if *noDefaults {
		options.ExcludeGlobs = append([]string{}, exclude...)
	} else {
		options.ExcludeGlobs = append(append([]string{}, core.DefaultExcludeGlobs...), exclude...)
	}

	// Build coverage source.
	src := coverage.CoverageSource{Packages: *packages}
	switch {
	case *noCoverage:
		src.Mode = coverage.CoverageNone
	case *coverFile != "":
		src.Mode = coverage.CoveragePrebuilt
		abs, _ := filepath.Abs(expandTilde(*coverFile))
		src.ProfileFile = abs
	default:
		src.Mode = coverage.CoverageAuto
	}
	if *projectDir != "" {
		abs, _ := filepath.Abs(expandTilde(*projectDir))
		src.ProjectDir = abs
	}

	progress := resolveProgress(stderr, *verbose, *quiet)
	sourcePath, _ := filepath.Abs(expandTilde(*path))

	report, err := coverage.Run(coverage.PipelineArgs{
		SourcePath: sourcePath,
		Coverage:   src,
		Threshold:  *threshold,
		Options:    options,
		Progress:   progress,
	})
	if err != nil {
		emitErr(err)
		return 1
	}

	if *jsonOut {
		out, jerr := core.JSONReport(report)
		if jerr != nil {
			emitErr(jerr)
			return 1
		}
		fmt.Fprintln(stdout, out)
	} else {
		fmt.Fprint(stdout, core.PrettyReport(report, 20))
	}

	if *failOver != notSet && len(report.Methods) > 0 {
		worst := report.Methods[0]
		if worst.Crap > *failOver {
			fmt.Fprintf(stderr, "%s: CRAP %.2f exceeds --fail-over %.2f\n", core.ToolName, worst.Crap, *failOver)
			return 2
		}
	}
	return 0
}

// notSet is a sentinel for the optional --fail-over flag (a NaN-free
// out-of-range marker; real CRAP scores are always ≥ 0).
const notSet = -1.0

func resolveProgress(stderr io.Writer, verbose, quiet bool) *core.ProgressReporter {
	if quiet {
		return core.SilentReporter()
	}
	if verbose {
		return core.NewReporter(stderr, core.Verbose)
	}
	return core.NewReporter(stderr, core.Normal)
}

func expandTilde(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}
