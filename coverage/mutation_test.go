package coverage

import (
	"bytes"
	"math"
	"path/filepath"
	"testing"

	"github.com/JeevanThandi/slopguard-go/core"
)

// This file closes mutation-testing gaps in the coverage package.

// --- index.go: span boundary lines are inclusive; shared-suffix loop bounds ---

func TestMethodCoverageBoundaryLinesIncluded(t *testing.T) {
	// Method spans lines 3-6; boundaries 3 and 6 are uncovered, interior 4,5
	// covered. Excluding either boundary would change the percentage.
	profile := "mode: count\n" +
		"example.com/m/b.go:3.1,3.5 1 0\n" + // line 3 uncovered
		"example.com/m/b.go:4.1,5.5 1 7\n" + // lines 4,5 covered
		"example.com/m/b.go:6.1,6.5 1 0\n" // line 6 uncovered
	p, _ := ParseProfile(profile)
	idx := NewCoverageIndex(p, "example.com/m", "/proj")
	pct, ok := idx.MethodCoverage(filepath.Join("/proj", "b.go"), 3, 6)
	if !ok {
		t.Fatal("expected coverage")
	}
	if math.Abs(pct-50) > 1e-9 { // 2 of 4 lines covered
		t.Errorf("coverage = %v, want 50 (boundaries 3 and 6 must be included)", pct)
	}
}

func TestSharedSuffixLenFullMatch(t *testing.T) {
	// Identical strings: the shared suffix is the whole string, requiring the
	// loop to compare index 0 (>=, not >) and to stop cleanly at the start
	// (&&, not ||, which would read past the start).
	if got := sharedSuffixLen("calc.go", "calc.go"); got != len("calc.go") {
		t.Errorf("sharedSuffixLen(identical) = %d, want %d", got, len("calc.go"))
	}
	if got := sharedSuffixLen("x", "x"); got != 1 {
		t.Errorf("sharedSuffixLen(single char) = %d, want 1", got)
	}
	// Different lengths where the shorter is a full suffix of the longer: the
	// loop must stop when the shorter string is exhausted (the `bi >= 0` guard),
	// not run its index negative.
	if got := sharedSuffixLen("xcalc.go", "calc.go"); got != len("calc.go") {
		t.Errorf("sharedSuffixLen(suffix) = %d, want %d", got, len("calc.go"))
	}
}

// --- pipeline.go: plural, nil-progress handling, packages default, ephemeral ---

func TestPluralSingularAndPlural(t *testing.T) {
	if got := plural(1, "file", "files"); got != "1 file" {
		t.Errorf("plural(1) = %q, want '1 file'", got)
	}
	if got := plural(2, "file", "files"); got != "2 files" {
		t.Errorf("plural(2) = %q, want '2 files'", got)
	}
	if got := plural(0, "file", "files"); got != "0 files" {
		t.Errorf("plural(0) = %q, want '0 files'", got)
	}
}

func TestPipelineKeepsProvidedProgress(t *testing.T) {
	// A non-nil reporter must be used as-is — the nil-check only substitutes a
	// silent reporter for a nil one.
	root := setupModule(t)
	var buf bytes.Buffer
	if _, err := Run(PipelineArgs{
		SourcePath: root,
		Coverage:   CoverageSource{Mode: CoverageNone},
		Options:    core.DefaultAnalysisOptions(),
		Progress:   core.NewReporter(&buf, core.Normal),
	}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("provided progress reporter should have received phase output")
	}
}

type recordingRunner struct{ gotPackages string }

func (r *recordingRunner) RunTests(_, _, packages string, _ *core.ProgressReporter) (TestOutcome, error) {
	r.gotPackages = packages
	return TestOutcome{TestsPassed: true}, nil
}

func TestPipelineDefaultsPackagesPattern(t *testing.T) {
	// With no Packages set, auto mode must default to "./...".
	root := setupModule(t)
	rr := &recordingRunner{}
	if _, err := Run(PipelineArgs{
		SourcePath: root,
		Coverage:   CoverageSource{Mode: CoverageAuto}, // Packages empty
		Options:    core.DefaultAnalysisOptions(),
		Runner:     rr,
	}); err != nil {
		t.Fatal(err)
	}
	if rr.gotPackages != "./..." {
		t.Errorf("packages = %q, want './...'", rr.gotPackages)
	}
}

func TestPipelineAutoCoverageDataPathIsEphemeral(t *testing.T) {
	// In auto mode the profile is generated into a temp dir and cleaned up, so
	// CoverageDataPath must stay nil.
	root := setupModule(t)
	profile := "mode: count\nexample.com/mod/calc.go:3.30,8.2 3 4\n"
	report, err := Run(PipelineArgs{
		SourcePath: root,
		Coverage:   CoverageSource{Mode: CoverageAuto},
		Options:    core.DefaultAnalysisOptions(),
		Runner:     fakeRunner{profileBody: profile, pass: true, produce: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CoverageDataPath != nil {
		t.Errorf("auto-mode CoverageDataPath = %v, want nil (ephemeral)", *report.CoverageDataPath)
	}
}

// --- runner.go: tail keeps the last `limit` bytes exactly ---

func TestTailWriterKeepsExactTail(t *testing.T) {
	var buf bytes.Buffer
	w := &tailWriter{buf: &buf, limit: 4, progress: core.SilentReporter()}
	w.Write([]byte("abcdefgh"))
	if got := buf.String(); got != "efgh" {
		t.Errorf("tail = %q, want the last 4 bytes 'efgh'", got)
	}
}

// --- projectroot.go: a nonexistent path resolves cleanly, never panics ---

func TestDiscoverProjectRootNonexistentPathNoPanic(t *testing.T) {
	root, ok := DiscoverProjectRoot(filepath.Join(t.TempDir(), "does", "not", "exist"))
	if ok || root != "" {
		t.Errorf("nonexistent path: root=%q ok=%v, want '',false", root, ok)
	}
}

// --- runner.go (integration): a compiling no-statement package still "passes" ---

func TestRealRunnerHeaderOnlyProfileStillPasses(t *testing.T) {
	requireGo(t)
	// A package with no statements and no tests compiles and passes, but the
	// coverage profile is header-only → produced=false. The outcome must still
	// report TestsPassed=true with an empty ProfilePath.
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/empty\n\ngo 1.23\n")
	mustWrite(t, filepath.Join(root, "doc.go"), "package empty\n")
	outcome, err := (&TestRunner{}).RunTests(root, t.TempDir(), "./...", core.SilentReporter())
	if err != nil {
		t.Fatalf("RunTests error: %v", err)
	}
	if !outcome.TestsPassed {
		t.Error("a compiling no-statement package should report TestsPassed=true")
	}
	if outcome.ProfilePath != "" {
		t.Errorf("expected no profile path for a header-only profile, got %q", outcome.ProfilePath)
	}
}
