package mutation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/JeevanThandi/slopguard-go/core"
	"github.com/JeevanThandi/slopguard-go/coverage"
)

// calcSource yields three mutants: 4:7 boundary (> → >=), 4:7
// negate_conditional (> → <=) and 10:35 arithmetic (* → /).
const calcSource = `package calc

func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func Double(n int) int { return n * 2 }
`

// calcProfile covers lines 3-7 of calc.go and leaves line 10 unexecuted.
const calcProfile = `mode: count
example.com/calc/calc.go:3.24,4.11 1 5
example.com/calc/calc.go:4.11,6.3 1 3
example.com/calc/calc.go:7.2,7.10 1 2
example.com/calc/calc.go:10.24,10.40 1 0
`

// writeModule creates a module with calc.go and returns its root.
func writeModule(t *testing.T, source string) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/calc\n\ngo 1.23\n")
	mustWrite(t, filepath.Join(root, "calc.go"), source)
	return root
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func realPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// recordedRun is one call to fakeRunner: the request, plus the overlay's
// Replace map and the backing file content as they were at call time.
type recordedRun struct {
	req     RunRequest
	replace map[string]string
	content string
}

// fakeRunner answers the plain baseline with baseline and each mutant run
// with mutant(content), where content is the mutated file. hook, when set,
// runs first and may override the answer.
type fakeRunner struct {
	calls    []recordedRun
	baseline RunResult
	mutant   func(content string) RunResult
	hook     func(ctx context.Context, run recordedRun) (RunResult, error, bool)
}

func (f *fakeRunner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	run := recordedRun{req: req}
	var config struct{ Replace map[string]string }
	if data, err := os.ReadFile(req.OverlayPath); err == nil {
		json.Unmarshal(data, &config)
	}
	run.replace = config.Replace
	for _, backing := range config.Replace {
		data, _ := os.ReadFile(backing)
		run.content = string(data)
	}
	f.calls = append(f.calls, run)
	if f.hook != nil {
		if result, err, ok := f.hook(ctx, run); ok {
			return result, err
		}
	}
	if len(run.replace) == 0 {
		return f.baseline, nil
	}
	if f.mutant == nil {
		return RunResult{ExitCode: 1, Duration: 500 * time.Millisecond}, nil
	}
	return f.mutant(run.content), nil
}

// fakeCoverage stands in for the analyze coverage runner.
type fakeCoverage struct {
	profile  string // written as the coverage profile when non-empty
	path     string // reported instead of the written profile when non-empty
	exitCode int
	err      error
	calls    int
	root     string
	packages string
	hook     func()
}

func (f *fakeCoverage) RunTests(projectRoot, coverageDir, packages string, _ *core.ProgressReporter) (coverage.TestOutcome, error) {
	f.calls++
	f.root, f.packages = projectRoot, packages
	if f.hook != nil {
		f.hook()
	}
	if f.err != nil {
		return coverage.TestOutcome{}, f.err
	}
	outcome := coverage.TestOutcome{ExitCode: f.exitCode, TestsPassed: f.exitCode == 0}
	if f.profile != "" {
		outcome.ProfilePath = filepath.Join(coverageDir, "cover.out")
		if err := os.WriteFile(outcome.ProfilePath, []byte(f.profile), 0o644); err != nil {
			return coverage.TestOutcome{}, err
		}
	}
	if f.path != "" {
		outcome.ProfilePath = f.path
	}
	return outcome, nil
}

// byChange answers mutant runs by the mutated text they contain.
func byChange(results map[string]RunResult) func(string) RunResult {
	return func(content string) RunResult {
		for change, result := range results {
			if strings.Contains(content, change) {
				return result
			}
		}
		return RunResult{ExitCode: 1}
	}
}

func statuses(report core.MutationReport) map[string]core.MutantStatus {
	out := map[string]core.MutantStatus{}
	for _, m := range report.Mutants {
		out[m.ID] = m.Status
	}
	return out
}

func baseArgs(root string, runner Runner, cov coverage.Runner) Args {
	return Args{
		SourcePath:     root,
		Options:        core.DefaultAnalysisOptions(),
		Runner:         runner,
		CoverageRunner: cov,
	}
}

func TestRunDryRunRunsNothing(t *testing.T) {
	root := writeModule(t, calcSource)
	runner, cov := &fakeRunner{}, &fakeCoverage{}
	args := baseArgs(root, runner, cov)
	args.DryRun = true
	report, err := Run(args)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 0 || cov.calls != 0 {
		t.Fatalf("a dry run must not run tests (runner %d, coverage %d)", len(runner.calls), cov.calls)
	}
	if report.ProjectRoot != nil || report.Runner != nil || report.TimeoutSeconds != nil || report.CoverageAvailable {
		t.Error("a dry run reports no project, runner, timeout or coverage")
	}
	if report.Summary.Pending != 3 || report.Summary.MutationScore != nil || report.Summary.FileCount != 1 {
		t.Errorf("summary = %+v", report.Summary)
	}
	if !reflect.DeepEqual(report.Operators, core.MutationOperators) {
		t.Errorf("operators = %v", report.Operators)
	}
}

func TestRunWithNothingToRunSkipsTheBaseline(t *testing.T) {
	for name, source := range map[string]string{
		"no mutants":  "package calc\n\nfunc F() {}\n",
		"all ignored": "package calc\n\n// slopguard-ignore-mutant\nfunc Double(n int) int { return n * 2 }\n",
	} {
		// No go.mod either: nothing to run means no project root is needed.
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "calc.go"), source)
		runner := &fakeRunner{}
		report, err := Run(baseArgs(dir, runner, &fakeCoverage{}))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if len(runner.calls) != 0 || report.ProjectRoot != nil || report.Runner != nil || report.TimeoutSeconds != nil {
			t.Errorf("%s: nothing should run, got %d calls and %+v", name, len(runner.calls), report)
		}
	}
}

func TestRunClassifiesEveryMutant(t *testing.T) {
	root := writeModule(t, calcSource)
	runner := &fakeRunner{
		baseline: RunResult{Duration: 1200 * time.Millisecond},
		mutant: byChange(map[string]RunResult{
			"a >= b": {ExitCode: 0, Duration: 400 * time.Millisecond},
			"a <= b": {ExitCode: 1, Duration: 500 * time.Millisecond},
			"n / 2":  {TimedOut: true, ExitCode: -1, Duration: 34 * time.Second},
		}),
	}
	args := baseArgs(root, runner, &fakeCoverage{})
	args.NoCoverage = true
	report, err := Run(args)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]core.MutantStatus{
		"calc.go:4:7:boundary":           core.StatusSurvived,
		"calc.go:4:7:negate_conditional": core.StatusKilled,
		"calc.go:10:35:arithmetic":       core.StatusTimeout,
	}
	if got := statuses(report); !reflect.DeepEqual(got, want) {
		t.Errorf("statuses = %v, want %v", got, want)
	}
	// ceil(1.2 × 3) + 30 = 34.
	if report.TimeoutSeconds == nil || *report.TimeoutSeconds != 34 {
		t.Errorf("timeoutSeconds = %v, want 34", report.TimeoutSeconds)
	}
	if report.Runner == nil || *report.Runner != "go test" || report.ProjectRoot == nil || *report.ProjectRoot != root {
		t.Errorf("runner/projectRoot = %v/%v", report.Runner, report.ProjectRoot)
	}
	if report.CoverageAvailable || len(report.Notes) != 0 {
		t.Errorf("coverage = %v, notes = %v", report.CoverageAvailable, report.Notes)
	}
	if s := report.Summary; s.MutationScore == nil || *s.MutationScore != float64(2)/float64(3)*100 {
		t.Errorf("score = %v", s.MutationScore)
	}
	if len(runner.calls) != 4 {
		t.Fatalf("calls = %d, want the baseline and 3 mutants", len(runner.calls))
	}
	for i, call := range runner.calls {
		wantTimeout := 34 * time.Second
		if i == 0 {
			wantTimeout = 0 // the plain baseline has no timeout
		}
		if call.req.Timeout != wantTimeout || call.req.Packages != "./..." || call.req.ProjectRoot != realPath(t, root) {
			t.Errorf("call %d request = %+v", i, call.req)
		}
	}
}

func TestRunNotesCompileErrorsAndEverySurvived(t *testing.T) {
	root := writeModule(t, calcSource)
	runner := &fakeRunner{mutant: byChange(map[string]RunResult{
		"a >= b": {ExitCode: 0},
		"a <= b": {ExitCode: 0},
		"n / 2":  {ExitCode: 1, BuildFailed: true},
	})}
	args := baseArgs(root, runner, nil)
	args.NoCoverage = true
	report, err := Run(args)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{core.NoteEverySurvived, "1 mutant(s) did not compile and are excluded from the score."}
	if !reflect.DeepEqual(report.Notes, want) {
		t.Errorf("notes = %q, want %q", report.Notes, want)
	}
	if statuses(report)["calc.go:10:35:arithmetic"] != core.StatusCompileError {
		t.Errorf("statuses = %v", statuses(report))
	}
}

func TestRunOverlayCarriesTheMutantAndLeavesTheSourceAlone(t *testing.T) {
	root := writeModule(t, calcSource)
	sourceFile := filepath.Join(root, "calc.go")
	before, _ := os.Stat(sourceFile)
	runner := &fakeRunner{}
	args := baseArgs(root, runner, nil)
	args.NoCoverage = true
	args.Operators = []string{core.OpBoundary}
	if _, err := Run(args); err != nil {
		t.Fatal(err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("calls = %d, want the baseline and one mutant", len(runner.calls))
	}
	if baseline := runner.calls[0]; baseline.replace == nil || len(baseline.replace) != 0 {
		t.Errorf("the baseline overlay should be an empty Replace map, got %v", baseline.replace)
	}
	mutantRun := runner.calls[1]
	key := filepath.Join(realPath(t, root), "calc.go")
	if _, ok := mutantRun.replace[key]; !ok || len(mutantRun.replace) != 1 {
		t.Errorf("overlay = %v, want one entry for %s", mutantRun.replace, key)
	}
	if want := strings.Replace(calcSource, "a > b", "a >= b", 1); mutantRun.content != want {
		t.Errorf("mutated file =\n%s\nwant\n%s", mutantRun.content, want)
	}
	after, _ := os.Stat(sourceFile)
	data, _ := os.ReadFile(sourceFile)
	if string(data) != calcSource || !after.ModTime().Equal(before.ModTime()) {
		t.Error("mutate must never write to the source tree")
	}
	if _, err := os.Stat(filepath.Dir(mutantRun.req.OverlayPath)); !os.IsNotExist(err) {
		t.Errorf("the temp dir should be removed after the run: %v", err)
	}
}

func TestRunThroughASymlinkedSourcePath(t *testing.T) {
	root := writeModule(t, calcSource)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	runner := &fakeRunner{}
	args := baseArgs(link, runner, nil)
	args.NoCoverage = true
	args.Operators = []string{core.OpBoundary}
	report, err := Run(args)
	if err != nil {
		t.Fatal(err)
	}
	// go test runs in the real directory and the overlay key uses the same
	// spelling; the report keeps the path the user gave.
	real := realPath(t, root)
	mutantRun := runner.calls[1]
	if mutantRun.req.ProjectRoot != real {
		t.Errorf("go test ran in %q, want %q", mutantRun.req.ProjectRoot, real)
	}
	if _, ok := mutantRun.replace[filepath.Join(real, "calc.go")]; !ok {
		t.Errorf("overlay key should use the real path, got %v", mutantRun.replace)
	}
	if *report.ProjectRoot != link || report.SourceRoot != link {
		t.Errorf("projectRoot/sourceRoot = %q/%q, want %q", *report.ProjectRoot, report.SourceRoot, link)
	}
}

func TestRunBaselineFailure(t *testing.T) {
	for output, want := range map[string]string{
		"\n--- FAIL: TestMax\nFAIL\n": "(exit 1). Fix the failing tests first: --- FAIL: TestMax\nFAIL",
		"   ":                         "(exit 1). Fix the failing tests first: no output captured",
	} {
		root := writeModule(t, calcSource)
		runner := &fakeRunner{baseline: RunResult{ExitCode: 1, Output: output}}
		_, err := Run(baseArgs(root, runner, &fakeCoverage{}))
		var se *core.SlopguardError
		if !errors.As(err, &se) || se.Code != core.ErrBaselineFailed || !strings.HasSuffix(se.Message, want) {
			t.Errorf("err = %v, want baseline_failed ending %q", err, want)
		}
		if len(runner.calls) != 1 {
			t.Errorf("no mutant may run after a failed baseline, got %d calls", len(runner.calls))
		}
	}
}

func TestRunClassifiesUnexecutedLinesAsNoCoverage(t *testing.T) {
	root := writeModule(t, calcSource)
	runner := &fakeRunner{}
	cov := &fakeCoverage{profile: calcProfile}
	args := baseArgs(root, runner, cov)
	args.Packages = "./calc/..."
	report, err := Run(args)
	if err != nil {
		t.Fatal(err)
	}
	if !report.CoverageAvailable || len(report.Notes) != 0 {
		t.Errorf("coverage = %v, notes = %v", report.CoverageAvailable, report.Notes)
	}
	if got := statuses(report)["calc.go:10:35:arithmetic"]; got != core.StatusNoCoverage {
		t.Errorf("line 10 is never executed; status = %s", got)
	}
	if len(runner.calls) != 3 {
		t.Errorf("calls = %d, want the baseline and the two covered mutants", len(runner.calls))
	}
	if cov.root != root || cov.packages != "./calc/..." {
		t.Errorf("coverage ran in %q for %q", cov.root, cov.packages)
	}
}

func TestRunRunsMutantsOnLinesCoverageDoesNotKnow(t *testing.T) {
	root := writeModule(t, calcSource)
	runner := &fakeRunner{}
	profile := strings.Replace(calcProfile, "example.com/calc/calc.go:10.24,10.40 1 0\n", "", 1)
	report, err := Run(baseArgs(root, runner, &fakeCoverage{profile: profile}))
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.NoCoverage != 0 || len(runner.calls) != 4 {
		t.Errorf("unknown coverage must run the mutant: %+v, %d calls", report.Summary, len(runner.calls))
	}
}

func TestRunKeepsCoverageFromAFailingCoverageRun(t *testing.T) {
	root := writeModule(t, calcSource)
	report, err := Run(baseArgs(root, &fakeRunner{}, &fakeCoverage{profile: calcProfile, exitCode: 1}))
	if err != nil {
		t.Fatalf("a failing coverage run is never fatal: %v", err)
	}
	if !report.CoverageAvailable || report.Summary.NoCoverage != 1 {
		t.Errorf("the coverage data should still be used: %+v", report.Summary)
	}
	if want := []string{core.CoverageRunExitNote(1)}; !reflect.DeepEqual(report.Notes, want) {
		t.Errorf("notes = %q, want %q", report.Notes, want)
	}
}

func TestRunWithoutUsableCoverageRunsEveryMutant(t *testing.T) {
	cases := map[string]*fakeCoverage{
		"runner error":      {err: core.TestRunFailed(1, "build failed")},
		"no profile":        {exitCode: 1},
		"header-only":       {profile: "mode: count\n"},
		"malformed profile": {profile: "mode: count\nnot a profile line\n"},
		"missing profile":   {path: "/no/such/cover.out"},
	}
	for name, cov := range cases {
		root := writeModule(t, calcSource)
		runner := &fakeRunner{}
		report, err := Run(baseArgs(root, runner, cov))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if report.CoverageAvailable || len(runner.calls) != 4 {
			t.Errorf("%s: coverage = %v, calls = %d", name, report.CoverageAvailable, len(runner.calls))
		}
		if want := []string{core.NoteNoCoverageData}; !reflect.DeepEqual(report.Notes, want) {
			t.Errorf("%s: notes = %q, want %q", name, report.Notes, want)
		}
	}
}

func TestRunNoCoverageSkipsTheCoverageRun(t *testing.T) {
	root := writeModule(t, calcSource)
	cov := &fakeCoverage{profile: calcProfile}
	args := baseArgs(root, &fakeRunner{}, cov)
	args.NoCoverage = true
	report, err := Run(args)
	if err != nil {
		t.Fatal(err)
	}
	if cov.calls != 0 || report.CoverageAvailable || report.Summary.NoCoverage != 0 || len(report.Notes) != 0 {
		t.Errorf("--no-coverage: calls=%d report=%+v", cov.calls, report)
	}
}

func TestRunUsesAGivenTimeout(t *testing.T) {
	root := writeModule(t, calcSource)
	runner := &fakeRunner{baseline: RunResult{Duration: time.Hour}}
	args := baseArgs(root, runner, nil)
	args.NoCoverage = true
	args.TimeoutSeconds = 2.5
	report, err := Run(args)
	if err != nil {
		t.Fatal(err)
	}
	if *report.TimeoutSeconds != 2.5 || runner.calls[1].req.Timeout != 2500*time.Millisecond {
		t.Errorf("timeout = %v / %v", *report.TimeoutSeconds, runner.calls[1].req.Timeout)
	}
}

func TestRunStopsWhenInterrupted(t *testing.T) {
	cancelled := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	t.Run("before the baseline", func(t *testing.T) {
		runner := &fakeRunner{}
		args := baseArgs(writeModule(t, calcSource), runner, nil)
		args.Context = cancelled()
		if _, err := Run(args); !errors.Is(err, context.Canceled) || len(runner.calls) != 0 {
			t.Errorf("err = %v, calls = %d", err, len(runner.calls))
		}
	})
	t.Run("during the baseline", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		runner := &fakeRunner{hook: func(ctx context.Context, _ recordedRun) (RunResult, error, bool) {
			cancel()
			return RunResult{}, ctx.Err(), true
		}}
		args := baseArgs(writeModule(t, calcSource), runner, nil)
		args.Context = ctx
		if _, err := Run(args); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
		if _, err := os.Stat(filepath.Dir(runner.calls[0].req.OverlayPath)); !os.IsNotExist(err) {
			t.Error("the temp dir should be removed after an interrupt")
		}
	})
	t.Run("during the coverage run", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		args := baseArgs(writeModule(t, calcSource), &fakeRunner{}, &fakeCoverage{profile: calcProfile, hook: cancel})
		args.Context = ctx
		if _, err := Run(args); !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("between mutants", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		runner := &fakeRunner{hook: func(_ context.Context, run recordedRun) (RunResult, error, bool) {
			if len(run.replace) > 0 {
				cancel() // the first mutant finishes; the second must not start
			}
			return RunResult{ExitCode: 1}, nil, len(run.replace) > 0
		}}
		args := baseArgs(writeModule(t, calcSource), runner, nil)
		args.Context = ctx
		args.NoCoverage = true
		if _, err := Run(args); !errors.Is(err, context.Canceled) || len(runner.calls) != 2 {
			t.Errorf("err = %v, calls = %d", err, len(runner.calls))
		}
	})
}

func TestRunErrors(t *testing.T) {
	code := func(err error) core.ErrorCode {
		var se *core.SlopguardError
		if errors.As(err, &se) {
			return se.Code
		}
		return ""
	}
	module := writeModule(t, calcSource)
	noModule := t.TempDir()
	mustWrite(t, filepath.Join(noModule, "calc.go"), calcSource)
	cases := []struct {
		name string
		args Args
		want core.ErrorCode
	}{
		{"unknown operator", Args{SourcePath: module, Operators: []string{"flip"}}, core.ErrInvalidArgument},
		{"missing source", Args{SourcePath: "/no/such/source"}, core.ErrFileNotFound},
		{"no go.mod", Args{SourcePath: noModule, Runner: &fakeRunner{}}, core.ErrProjectRootMissing},
		{"missing project dir", Args{SourcePath: module, ProjectDir: "/no/such/project", Runner: &fakeRunner{}}, core.ErrFileNotFound},
		{"runner unavailable", Args{SourcePath: module, NoCoverage: true, Runner: &fakeRunner{
			hook: func(context.Context, recordedRun) (RunResult, error, bool) {
				return RunResult{}, core.RunnerUnavailable("could not launch 'go'"), true
			},
		}}, core.ErrRunnerUnavailable},
		{"runner unavailable for a mutant", Args{SourcePath: module, NoCoverage: true, Runner: &fakeRunner{
			hook: func(_ context.Context, run recordedRun) (RunResult, error, bool) {
				return RunResult{}, core.RunnerUnavailable("gone"), len(run.replace) > 0
			},
		}}, core.ErrRunnerUnavailable},
		{"mutant cannot be written", Args{SourcePath: module, NoCoverage: true, Runner: &fakeRunner{
			hook: func(_ context.Context, run recordedRun) (RunResult, error, bool) {
				// Block the temp dir's mutant directory with a plain file.
				os.WriteFile(filepath.Join(filepath.Dir(run.req.OverlayPath), "mutant"), nil, 0o644)
				return RunResult{}, nil, true
			},
		}}, core.ErrUnsupported},
	}
	for _, c := range cases {
		if _, err := Run(c.args); code(err) != c.want {
			t.Errorf("%s: err = %v, want %s", c.name, err, c.want)
		}
	}
	t.Setenv("TMPDIR", filepath.Join(noModule, "missing"))
	if _, err := Run(Args{SourcePath: module, Runner: &fakeRunner{}}); code(err) != core.ErrUnsupported {
		t.Errorf("an unusable temp dir: err = %v, want unsupported", err)
	}
}

func TestRunProgressLines(t *testing.T) {
	root := writeModule(t, "package calc\n\nfunc Max(a, b int) bool {\n\treturn a > b // slopguard-ignore-mutant(boundary)\n}\n")
	var buf bytes.Buffer
	runner := &fakeRunner{
		baseline: RunResult{Duration: 2100 * time.Millisecond},
		mutant:   func(string) RunResult { return RunResult{ExitCode: 1, Duration: 900 * time.Millisecond} },
	}
	args := baseArgs(root, runner, &fakeCoverage{err: core.TestRunFailed(1, "x")})
	args.Progress = core.NewReporter(&buf, core.Normal)
	if _, err := Run(args); err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"slopguard: walking " + root,
		"slopguard: generated 2 mutant(s) in 1 file(s)",
		"slopguard: running baseline tests (go test) in " + root,
		"slopguard: baseline passed in 2.1s; timeout is 37s per mutant",
		"slopguard: [1/2] ignored       calc.go:4:11 boundary",
		"slopguard: [2/2] killed        calc.go:4:11 negate_conditional (0.9s)",
		"slopguard: done — 1 killed, 0 timeout, 0 survived, 0 no_coverage, 0 compile_error, 1 ignored",
	}, "\n") + "\n"
	if got := buf.String(); got != want {
		t.Errorf("progress =\n%s\nwant\n%s", got, want)
	}
}

func TestNewSessionDefaultsToGoTest(t *testing.T) {
	root := writeModule(t, calcSource)
	ctx := context.Background()
	s, err := newSession(ctx, Args{}, core.SilentReporter(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if _, ok := s.runner.(*GoTestRunner); !ok {
		t.Errorf("runner = %T, want *GoTestRunner", s.runner)
	}
	if cov, ok := s.coverage.(*coverage.TestRunner); !ok || cov.Context != ctx {
		t.Errorf("coverage runner = %#v, want a *coverage.TestRunner bound to the run's context", s.coverage)
	}
	if s.packages != "./..." || s.projectRoot != root || s.realRoot != realPath(t, root) {
		t.Errorf("session = %+v", s)
	}
}

func TestSessionTempFileErrors(t *testing.T) {
	root := writeModule(t, calcSource)
	s, err := newSession(context.Background(), Args{}, core.SilentReporter(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	file := core.PlannedFile{AbsPath: filepath.Join(root, "calc.go"), Path: "calc.go", Source: []byte(calcSource)}
	key := s.overlayKey(file)
	code := func(err error) core.ErrorCode {
		var se *core.SlopguardError
		if errors.As(err, &se) {
			return se.Code
		}
		return ""
	}

	// The backing path is taken by a directory: the mutant cannot be written.
	if err := os.MkdirAll(filepath.Join(s.tempDir, "mutant", "calc.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.writeOverlay(key, file.Source); code(err) != core.ErrUnsupported {
		t.Errorf("blocked backing file: err = %v", err)
	}
	// The temp dir is gone: the overlay config cannot be written.
	os.RemoveAll(s.tempDir)
	if _, err := s.plainBaseline(); code(err) != core.ErrUnsupported {
		t.Errorf("missing temp dir: err = %v", err)
	}
	// The temp dir is a file: neither subdirectory can be created.
	mustWrite(t, s.tempDir, "not a directory")
	if err := s.writeOverlay(key, file.Source); code(err) != core.ErrUnsupported {
		t.Errorf("temp dir is a file (mutant): err = %v", err)
	}
	if _, _, err := s.coverageBaseline(); code(err) != core.ErrUnsupported {
		t.Errorf("temp dir is a file (coverage): err = %v", err)
	}
}

func TestOverlayKeyResolvesTheModuleSpelling(t *testing.T) {
	base := realPath(t, t.TempDir())
	root := filepath.Join(base, "module")
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	s := &session{realRoot: root, overlayKeys: map[string]string{}}
	inside := core.PlannedFile{AbsPath: filepath.Join(link, "pkg", "a.go"), Path: "pkg/a.go"}
	if got, want := s.overlayKey(inside), filepath.Join(root, "pkg", "a.go"); got != want {
		t.Errorf("overlayKey(via symlink) = %q, want %q", got, want)
	}
	// Cached per reported path.
	s.overlayKeys["pkg/a.go"] = "cached"
	if got := s.overlayKey(inside); got != "cached" {
		t.Errorf("overlayKey should be cached, got %q", got)
	}
	outside := core.PlannedFile{AbsPath: filepath.Join(base, "elsewhere", "b.go"), Path: "b.go"}
	if got, want := s.overlayKey(outside), filepath.Join(base, "elsewhere", "b.go"); got != want {
		t.Errorf("overlayKey(outside) = %q, want %q", got, want)
	}
}

func TestPathWithin(t *testing.T) {
	base := realPath(t, t.TempDir())
	root := filepath.Join(base, "root")
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		root, dir, rel string
		ok             bool
	}{
		{root, root, "", true},
		{root, deep, filepath.Join("a", "b"), true},
		{root, base, "", false},
		{filepath.Join(base, "missing"), deep, "", false},
	}
	for _, c := range cases {
		rel, ok := pathWithin(c.root, c.dir)
		if rel != c.rel || ok != c.ok {
			t.Errorf("pathWithin(%q, %q) = %q, %v; want %q, %v", c.root, c.dir, rel, ok, c.rel, c.ok)
		}
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(root, link); err == nil {
		// Two spellings of the same directory match by identity.
		if rel, ok := pathWithin(link, deep); !ok || rel != filepath.Join("a", "b") {
			t.Errorf("pathWithin(symlink) = %q, %v", rel, ok)
		}
	}
}
