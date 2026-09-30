package mutation

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/JeevanThandi/slopguard-go/core"
	"github.com/JeevanThandi/slopguard-go/coverage"
)

// RunnerName is the runner a mutation report names.
const RunnerName = "go test"

// TimeoutGraceSeconds is added to three times the plain baseline's run time
// to form the default per-mutant timeout. Every Go mutant run includes a
// compile, so the grace is larger than for the TypeScript and Python ports.
const TimeoutGraceSeconds = 30

// Args bundles the inputs to Run.
type Args struct {
	// SourcePath is the directory or single Go file to mutate.
	SourcePath string
	Options    core.AnalysisOptions
	// Operators lists the operator ids to apply; empty means every operator.
	// Unknown ids are an invalid_argument error.
	Operators []string
	// ProjectDir overrides module-root discovery (the nearest go.mod above
	// SourcePath).
	ProjectDir string
	// Packages is the go test package pattern; "" means "./...".
	Packages string
	// NoCoverage skips the coverage baseline, so no mutant is classified
	// no_coverage and every mutant runs.
	NoCoverage bool
	// TimeoutSeconds is the per-mutant timeout. Zero computes it from the
	// plain baseline: ceil(baseline × 3) + TimeoutGraceSeconds.
	TimeoutSeconds float64
	// DryRun lists the mutants and runs no test.
	DryRun   bool
	Progress *core.ProgressReporter
	// Context stops the run when it is done; the CLI cancels it on SIGINT,
	// SIGTERM and SIGHUP. Run then kills the running go test process group
	// and returns the context's error.
	Context context.Context
	// Runner and CoverageRunner are injectable for tests; nil runs go test.
	Runner         Runner
	CoverageRunner coverage.Runner
	// GeneratedAt is stamped on the report; the zero value means time.Now().
	GeneratedAt time.Time
}

// Run executes `mutate`: plan the mutants, run the plain baseline and the
// coverage baseline, run go test once per mutant, and build the report.
// The source tree is never written: every mutant reaches the compiler through
// `go test -overlay`, from a temp directory that Run removes before it
// returns.
func Run(args Args) (core.MutationReport, error) {
	ctx := args.Context
	if ctx == nil {
		ctx = context.Background()
	}
	progress := args.Progress
	if progress == nil {
		progress = core.SilentReporter()
	}
	operators, err := core.ParseOperators(args.Operators)
	if err != nil {
		return core.MutationReport{}, err
	}
	sourcePath, _ := filepath.Abs(args.SourcePath)

	progress.Phase("walking " + sourcePath)
	plan, err := core.PlanMutants(sourcePath, args.Options, operators)
	if err != nil {
		return core.MutationReport{}, err
	}
	progress.Phase(fmt.Sprintf("generated %d mutant(s) in %d file(s)", len(plan.Mutants), len(plan.Files)))
	if err := ctx.Err(); err != nil {
		return core.MutationReport{}, err
	}

	reportArgs := core.MutationReportArgs{
		FileCount:   len(plan.Files),
		GeneratedAt: args.GeneratedAt,
		Mutants:     plan.Mutants,
		Operators:   operators,
		SourceRoot:  plan.SourceRoot,
	}
	if args.DryRun || !anyToRun(plan.Mutants) {
		return core.NewMutationReport(reportArgs), nil
	}

	s, err := newSession(ctx, args, progress, sourcePath)
	if err != nil {
		return core.MutationReport{}, err
	}
	defer s.close()
	if err := s.execute(plan, &reportArgs); err != nil {
		return core.MutationReport{}, err
	}
	report := core.NewMutationReport(reportArgs)
	sum := report.Summary
	progress.Phase(fmt.Sprintf("done — %d killed, %d timeout, %d survived, %d no_coverage, %d compile_error, %d ignored",
		sum.Killed, sum.TimedOut, sum.Survived, sum.NoCoverage, sum.CompileErrors, sum.Ignored))
	return report, nil
}

// anyToRun reports whether at least one mutant is not ignored.
func anyToRun(mutants []core.Mutant) bool {
	for _, m := range mutants {
		if m.Status != core.StatusIgnored {
			return true
		}
	}
	return false
}

// session is one mutate run against one module: where go test runs, the
// runners, and the slopguard-owned temp directory for overlays.
type session struct {
	ctx      context.Context
	progress *core.ProgressReporter
	runner   Runner
	coverage coverage.Runner
	packages string
	// projectRoot is the module root as reported; realRoot is the same
	// directory with symlinks resolved, where go test actually runs.
	projectRoot string
	realRoot    string
	noCoverage  bool
	timeout     float64
	tempDir     string
	overlayPath string
	// overlayKeys caches, per reported file path, the path under which the
	// go command sees the file.
	overlayKeys map[string]string
}

func newSession(ctx context.Context, args Args, progress *core.ProgressReporter, sourcePath string) (*session, error) {
	root, _ := filepath.Abs(args.ProjectDir)
	if args.ProjectDir == "" {
		discovered, ok := coverage.DiscoverProjectRoot(sourcePath)
		if !ok {
			return nil, core.MutateProjectRootNotFound(sourcePath)
		}
		root = discovered
	}
	// PWD must be absolute for the go command to use it, so realRoot is too.
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, core.FileNotFound(root)
	}
	packages := args.Packages
	if packages == "" {
		packages = "./..."
	}
	tempDir, err := os.MkdirTemp("", "slopguard-mutate-")
	if err != nil {
		return nil, core.Unsupported("could not create temp dir: " + err.Error())
	}
	s := &session{
		ctx:         ctx,
		progress:    progress,
		runner:      args.Runner,
		coverage:    args.CoverageRunner,
		packages:    packages,
		projectRoot: root,
		realRoot:    realRoot,
		noCoverage:  args.NoCoverage,
		timeout:     args.TimeoutSeconds,
		tempDir:     tempDir,
		overlayPath: filepath.Join(tempDir, "overlay.json"),
		overlayKeys: map[string]string{},
	}
	if s.runner == nil {
		s.runner = &GoTestRunner{}
	}
	if s.coverage == nil {
		s.coverage = &coverage.TestRunner{Context: ctx}
	}
	return s, nil
}

// close removes the temp directory with every overlay and mutant file.
func (s *session) close() { os.RemoveAll(s.tempDir) }

// execute runs both baselines and every mutant, and fills in the run fields
// of the report.
func (s *session) execute(plan core.MutantPlan, report *core.MutationReportArgs) error {
	baseline, err := s.plainBaseline()
	if err != nil {
		return err
	}
	timeoutSeconds := s.timeout
	if timeoutSeconds <= 0 {
		timeoutSeconds = math.Ceil(baseline.Seconds()*3) + TimeoutGraceSeconds
	}
	s.progress.Phase(fmt.Sprintf("baseline passed in %.1fs; timeout is %ss per mutant",
		baseline.Seconds(), core.FormatSeconds(timeoutSeconds)))

	var index *coverage.CoverageIndex
	var notes []string
	if !s.noCoverage {
		if index, notes, err = s.coverageBaseline(); err != nil {
			return err
		}
	}
	files := map[string]core.PlannedFile{}
	for _, f := range plan.Files {
		files[f.Path] = f
	}
	timeout := secondsToDuration(timeoutSeconds)
	width := len(strconv.Itoa(len(plan.Mutants)))
	for i := range plan.Mutants {
		m := &plan.Mutants[i]
		elapsed, err := s.classify(m, files[m.File], index, timeout)
		if err != nil {
			return err
		}
		timing := ""
		if elapsed != nil {
			timing = fmt.Sprintf(" (%.1fs)", elapsed.Seconds())
		}
		s.progress.Phase(fmt.Sprintf("[%*d/%d] %-13s %s:%d:%d %s%s",
			width, i+1, len(plan.Mutants), m.Status, m.File, m.Line, m.Column, m.Operator, timing))
	}

	projectRoot, runner := s.projectRoot, RunnerName
	report.CoverageAvailable = index != nil
	report.Mutants = plan.Mutants
	report.ProjectRoot = &projectRoot
	report.Runner = &runner
	report.TimeoutSeconds = &timeoutSeconds
	summary := core.SummarizeMutants(plan.Mutants, len(plan.Files))
	report.Notes = append(notes, core.MutationResultNotes(summary)...)
	return nil
}

// secondsToDuration converts a timeout in seconds, capping values too large
// for a time.Duration (about 292 years) instead of overflowing.
func secondsToDuration(seconds float64) time.Duration {
	if seconds >= time.Duration(math.MaxInt64).Seconds() {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(seconds * float64(time.Second))
}

// plainBaseline runs the exact mutant test command with no mutation (an empty
// overlay) and no timeout. It must pass: a broken command would otherwise
// "kill" every mutant and fake a perfect score. Its wall time sets the
// default timeout.
func (s *session) plainBaseline() (time.Duration, error) {
	s.progress.Phase(fmt.Sprintf("running baseline tests (%s) in %s", RunnerName, s.projectRoot))
	if err := s.writeOverlay("", nil); err != nil {
		return 0, err
	}
	result, err := s.runner.Run(s.ctx, s.request(0))
	if err != nil {
		return 0, err
	}
	if result.ExitCode != 0 {
		output := strings.TrimSpace(result.Output)
		if output == "" {
			output = "no output captured"
		}
		return 0, core.BaselineFailed(result.ExitCode, output)
	}
	return result.Duration, nil
}

// coverageBaseline runs the analyze coverage runner once, to learn which
// lines the tests execute. It never fails the run: the plain baseline alone
// decides pass/fail. A non-zero exit that still produced coverage data keeps
// the data (a note says so); a run without usable data returns a nil index,
// so every mutant runs.
func (s *session) coverageBaseline() (*coverage.CoverageIndex, []string, error) {
	coverageDir := filepath.Join(s.tempDir, "coverage")
	if err := os.MkdirAll(coverageDir, 0o755); err != nil {
		return nil, nil, core.Unsupported("could not create temp dir: " + err.Error())
	}
	// The profile names files by import path, so the coverage run does not
	// need the resolved root; the reported root keeps the progress lines
	// consistent. The index resolves import paths against the real root.
	outcome, err := s.coverage.RunTests(s.projectRoot, coverageDir, s.packages, s.progress)
	if ctxErr := s.ctx.Err(); ctxErr != nil {
		return nil, nil, ctxErr
	}
	noData := []string{core.NoteNoCoverageData}
	if err != nil || outcome.ProfilePath == "" {
		return nil, noData, nil
	}
	text, err := os.ReadFile(outcome.ProfilePath)
	if err != nil {
		return nil, noData, nil
	}
	profile, err := coverage.ParseProfile(string(text))
	if err != nil {
		return nil, noData, nil
	}
	index := coverage.NewCoverageIndex(profile, coverage.ModulePath(s.realRoot), s.realRoot)
	if index.FileCount() == 0 {
		return nil, noData, nil
	}
	var notes []string
	if outcome.ExitCode != 0 {
		notes = append(notes, core.CoverageRunExitNote(outcome.ExitCode))
	}
	return index, notes, nil
}

// classify sets one mutant's status: ignored mutants stay ignored, mutants on
// lines no test executes become no_coverage, and every other mutant is run.
// It returns the run's duration, or nil when nothing ran.
func (s *session) classify(m *core.Mutant, file core.PlannedFile, index *coverage.CoverageIndex, timeout time.Duration) (*time.Duration, error) {
	if m.Status == core.StatusIgnored {
		return nil, nil
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	key := s.overlayKey(file)
	if index != nil {
		if pct, ok := index.MethodCoverage(key, m.Line, m.Line); ok && pct == 0 {
			m.Status = core.StatusNoCoverage
			return nil, nil
		}
	}
	if err := s.writeOverlay(key, m.Apply(file.Source)); err != nil {
		return nil, err
	}
	result, err := s.runner.Run(s.ctx, s.request(timeout))
	if err != nil {
		return nil, err
	}
	m.Status = statusOf(result)
	return &result.Duration, nil
}

// statusOf maps a finished mutant run to a status.
func statusOf(result RunResult) core.MutantStatus {
	switch {
	case result.TimedOut:
		return core.StatusTimeout
	case result.ExitCode == 0:
		return core.StatusSurvived
	case result.BuildFailed:
		return core.StatusCompileError
	default:
		return core.StatusKilled
	}
}

func (s *session) request(timeout time.Duration) RunRequest {
	return RunRequest{
		ProjectRoot: s.realRoot,
		Packages:    s.packages,
		OverlayPath: s.overlayPath,
		Timeout:     timeout,
		Progress:    s.progress,
	}
}

// writeOverlay writes the -overlay config. With a key it writes content to a
// backing file in the temp dir and maps the key to it; with "" it writes an
// empty config (the plain baseline).
func (s *session) writeOverlay(key string, content []byte) error {
	replace := map[string]string{}
	if key != "" {
		dir := filepath.Join(s.tempDir, "mutant")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return core.Unsupported("could not create temp dir: " + err.Error())
		}
		backing := filepath.Join(dir, filepath.Base(key))
		if err := os.WriteFile(backing, content, 0o644); err != nil {
			return core.Unsupported("could not write the mutant: " + err.Error())
		}
		replace[key] = backing
	}
	config, _ := json.Marshal(struct{ Replace map[string]string }{replace})
	if err := os.WriteFile(s.overlayPath, config, 0o644); err != nil {
		return core.Unsupported("could not write the overlay config: " + err.Error())
	}
	return nil
}

// overlayKey returns the path under which the go command sees file: the real
// module root joined with the file's path inside it. go test matches -overlay
// keys against paths built from its working directory, so a key spelled
// differently (a symlinked /tmp instead of /private/tmp, or another letter
// case on a case-insensitive disk) would be ignored without an error, and
// every mutant would survive.
func (s *session) overlayKey(file core.PlannedFile) string {
	if key, ok := s.overlayKeys[file.Path]; ok {
		return key
	}
	dir := filepath.Dir(file.AbsPath)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	key := filepath.Join(dir, filepath.Base(file.AbsPath))
	if rel, ok := pathWithin(s.realRoot, dir); ok {
		key = filepath.Join(s.realRoot, rel, filepath.Base(file.AbsPath))
	}
	s.overlayKeys[file.Path] = key
	return key
}

// pathWithin returns dir relative to root when dir is root or lies below it.
// It walks up from dir comparing file identity (os.SameFile), so two
// spellings of the same directory still match.
func pathWithin(root, dir string) (string, bool) {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return "", false
	}
	var below []string
	for current := dir; ; {
		if info, err := os.Stat(current); err == nil && os.SameFile(rootInfo, info) {
			parts := make([]string, 0, len(below))
			for i := len(below) - 1; i >= 0; i-- {
				parts = append(parts, below[i])
			}
			return filepath.Join(parts...), true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		below = append(below, filepath.Base(current))
		current = parent
	}
}
