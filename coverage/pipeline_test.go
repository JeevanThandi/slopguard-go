package coverage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/JeevanThandi/slopguard-go/core"
)

// fakeRunner writes a profile and reports the configured outcome instead of
// spawning `go test`.
type fakeRunner struct {
	profileBody string
	pass        bool
	produce     bool
}

func (f fakeRunner) RunTests(projectRoot, coverageDir, packages string, _ *core.ProgressReporter) (TestOutcome, error) {
	if !f.produce {
		return TestOutcome{TestsPassed: f.pass}, nil
	}
	path := filepath.Join(coverageDir, "cover.out")
	if err := os.WriteFile(path, []byte(f.profileBody), 0o644); err != nil {
		return TestOutcome{}, err
	}
	return TestOutcome{ProfilePath: path, TestsPassed: f.pass}, nil
}

func setupModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/mod\n\ngo 1.23\n")
	mustWrite(t, filepath.Join(root, "calc.go"), `package mod

func Add(a, b int) int {
	if a > b {
		return a
	}
	return b
}
`)
	return root
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPipelineAutoJoinsCoverage(t *testing.T) {
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
	if !report.CoverageAvailable {
		t.Fatal("coverage should be available")
	}
	if len(report.Methods) != 1 || report.Methods[0].Name != "Add" {
		t.Fatalf("methods = %+v", report.Methods)
	}
	if report.Methods[0].Coverage != 100 {
		t.Errorf("coverage = %v, want 100", report.Methods[0].Coverage)
	}
}

func TestPipelineNoCoverage(t *testing.T) {
	root := setupModule(t)
	report, err := Run(PipelineArgs{
		SourcePath: root,
		Coverage:   CoverageSource{Mode: CoverageNone},
		Options:    core.DefaultAnalysisOptions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CoverageAvailable {
		t.Error("CoverageAvailable should be false in no-coverage mode")
	}
	if report.Methods[0].Coverage != 0 {
		t.Errorf("coverage = %v, want 0", report.Methods[0].Coverage)
	}
}

func TestPipelinePrebuilt(t *testing.T) {
	root := setupModule(t)
	profilePath := filepath.Join(t.TempDir(), "cover.out")
	mustWrite(t, profilePath, "mode: set\nexample.com/mod/calc.go:3.30,8.2 3 1\n")

	report, err := Run(PipelineArgs{
		SourcePath: root,
		Coverage:   CoverageSource{Mode: CoveragePrebuilt, ProfileFile: profilePath},
		Options:    core.DefaultAnalysisOptions(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.CoverageDataPath == nil || *report.CoverageDataPath != profilePath {
		t.Errorf("prebuilt coverage path = %v, want %s", report.CoverageDataPath, profilePath)
	}
	if report.Methods[0].Coverage != 100 {
		t.Errorf("coverage = %v, want 100", report.Methods[0].Coverage)
	}
}

func TestPipelineProjectRootNotFound(t *testing.T) {
	// A directory with Go source but no go.mod anywhere above it.
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "x.go"), "package x\nfunc F() {}\n")
	_, err := Run(PipelineArgs{
		SourcePath: dir,
		Coverage:   CoverageSource{Mode: CoverageAuto},
		Options:    core.DefaultAnalysisOptions(),
		Runner:     fakeRunner{},
	})
	if err == nil {
		t.Fatal("expected project_root_not_found error")
	}
}
