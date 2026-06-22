package coverage

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/JeevanThandi/slopguard-go/core"
)

func TestRunTestsMissingGoBinary(t *testing.T) {
	root := setupModule(t)
	runner := &TestRunner{GoBinary: "/no/such/go-binary-xyz"}
	_, err := runner.RunTests(root, t.TempDir(), "./...", core.SilentReporter())
	if err == nil {
		t.Fatal("expected an error when the go binary can't be launched")
	}
	var se *core.SlopguardError
	if !asCoreErr(err, &se) || se.Code != core.ErrUnsupported {
		t.Errorf("expected unsupported, got %v", err)
	}
}

func TestPipelineNoteWhenTestsFail(t *testing.T) {
	root := setupModule(t)
	profile := "mode: count\nexample.com/mod/calc.go:3.30,8.2 3 1\n"
	report, err := Run(PipelineArgs{
		SourcePath: root,
		Coverage:   CoverageSource{Mode: CoverageAuto},
		Options:    core.DefaultAnalysisOptions(),
		Runner:     fakeRunner{profileBody: profile, pass: false, produce: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasNote(report.Notes, "Some tests failed") {
		t.Errorf("expected a failing-tests note, got %v", report.Notes)
	}
}

func TestPipelineNoteWhenNoProfileProduced(t *testing.T) {
	root := setupModule(t)
	report, err := Run(PipelineArgs{
		SourcePath: root,
		Coverage:   CoverageSource{Mode: CoverageAuto},
		Options:    core.DefaultAnalysisOptions(),
		Runner:     fakeRunner{produce: false, pass: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasNote(report.Notes, "no coverage data was produced") {
		t.Errorf("expected a no-coverage note, got %v", report.Notes)
	}
	if report.CoverageAvailable {
		t.Error("coverage should be unavailable when no profile was produced")
	}
}

func TestPipelineNoteWhenProfileHasNoFiles(t *testing.T) {
	root := setupModule(t)
	// A header-only prebuilt profile parses to zero files.
	profilePath := filepath.Join(t.TempDir(), "empty.out")
	mustWrite(t, profilePath, "mode: set\n")
	report, err := Run(PipelineArgs{
		SourcePath: root,
		Coverage:   CoverageSource{Mode: CoveragePrebuilt, ProfileFile: profilePath},
		Options:    core.DefaultAnalysisOptions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasNote(report.Notes, "no per-file coverage data") {
		t.Errorf("expected an empty-coverage note, got %v", report.Notes)
	}
}

func TestPipelinePrebuiltUnreadableErrors(t *testing.T) {
	root := setupModule(t)
	_, err := Run(PipelineArgs{
		SourcePath: root,
		Coverage:   CoverageSource{Mode: CoveragePrebuilt, ProfileFile: "/no/such/profile.out"},
		Options:    core.DefaultAnalysisOptions(),
	})
	if err == nil {
		t.Fatal("expected an unreadable-profile error")
	}
}

func hasNote(notes []string, substr string) bool {
	for _, n := range notes {
		if strings.Contains(n, substr) {
			return true
		}
	}
	return false
}
