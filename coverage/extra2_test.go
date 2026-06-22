package coverage

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JeevanThandi/slopguard-go/core"
)

func TestMalformedLineError(t *testing.T) {
	e := &malformedLine{line: "bad data"}
	if !strings.Contains(e.Error(), "bad data") {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestParseProfileSkipsBlankLines(t *testing.T) {
	p, err := ParseProfile("mode: set\n\nexample.com/m/f.go:1.1,2.2 1 1\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 1 {
		t.Errorf("blank lines should be skipped, got %d files", len(p.Files))
	}
}

func TestMethodCoverageNoExecutableInRange(t *testing.T) {
	p, _ := ParseProfile("mode: set\nexample.com/m/f.go:10.1,12.2 1 1\n")
	idx := NewCoverageIndex(p, "example.com/m", "/proj")
	abs := filepath.Join("/proj", "f.go")
	// The method spans lines 100-110; the only block covers 10-12 → no overlap.
	if _, ok := idx.MethodCoverage(abs, 100, 110); ok {
		t.Error("a range with no executable lines should return ok=false")
	}
}

func TestModulePathNoModuleDirective(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "// a comment\ngo 1.23\n")
	if got := ModulePath(root); got != "" {
		t.Errorf("ModulePath without a module line = %q, want empty", got)
	}
}

func TestRunRejectsBadSourcePath(t *testing.T) {
	_, err := Run(PipelineArgs{
		SourcePath: "/no/such/source/path/xyz",
		Coverage:   CoverageSource{Mode: CoverageNone},
		Options:    core.DefaultAnalysisOptions(),
	})
	if err == nil {
		t.Fatal("expected an error for a missing source path")
	}
}

// --- Real `go test` integration paths (skipped in -short / without go) ---

func requireGo(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping subprocess test in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
}

func TestRealRunnerNoTestsStillInstruments(t *testing.T) {
	requireGo(t)
	root := setupModule(t) // has calc.go but no _test.go
	outcome, err := (&TestRunner{}).RunTests(root, t.TempDir(), "./...", core.SilentReporter())
	if err != nil {
		t.Fatalf("RunTests error: %v", err)
	}
	if !outcome.TestsPassed {
		t.Error("a suite with no tests should still 'pass'")
	}
	// With -coverpkg the package is instrumented even with no tests, so a
	// profile of all-zero blocks is produced — yielding an honest 0% report.
	if outcome.ProfilePath == "" {
		t.Fatal("expected an instrumented (all-zero) profile")
	}
	p, err := ParseProfile(readFileT(t, outcome.ProfilePath))
	if err != nil {
		t.Fatal(err)
	}
	idx := NewCoverageIndex(p, "example.com/mod", root)
	if pct, ok := idx.MethodCoverage(filepath.Join(root, "calc.go"), 3, 8); ok && pct != 0 {
		t.Errorf("no-tests coverage = %v, want 0", pct)
	}
}

func TestRealRunnerFailingTestStillEmitsProfile(t *testing.T) {
	requireGo(t)
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/failing\n\ngo 1.23\n")
	mustWrite(t, filepath.Join(root, "calc.go"), "package failing\n\nfunc Add(a, b int) int { return a + b }\n")
	mustWrite(t, filepath.Join(root, "calc_test.go"), "package failing\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\t_ = Add(1, 2)\n\tt.Fail()\n}\n")

	outcome, err := (&TestRunner{}).RunTests(root, t.TempDir(), "./...", core.SilentReporter())
	if err != nil {
		t.Fatalf("a failing test that still compiles should not error: %v", err)
	}
	if outcome.TestsPassed {
		t.Error("expected TestsPassed=false for a failing test")
	}
	if outcome.ProfilePath == "" {
		t.Error("expected a profile even though the test failed")
	}
}

func TestRealPipelineAutoWithNilRunner(t *testing.T) {
	requireGo(t)
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/realauto\n\ngo 1.23\n")
	mustWrite(t, filepath.Join(root, "calc.go"), "package realauto\n\nfunc Double(n int) int { return n * 2 }\n")
	mustWrite(t, filepath.Join(root, "calc_test.go"), "package realauto\n\nimport \"testing\"\n\nfunc TestDouble(t *testing.T) {\n\tif Double(2) != 4 {\n\t\tt.Fail()\n\t}\n}\n")

	// Nil Runner exercises the default-construction path against the real toolchain.
	report, err := Run(PipelineArgs{
		SourcePath: root,
		Coverage:   CoverageSource{Mode: CoverageAuto},
		Options:    core.DefaultAnalysisOptions(),
	})
	if err != nil {
		t.Fatalf("real auto pipeline: %v", err)
	}
	if !report.CoverageAvailable {
		t.Error("coverage should be available from the real run")
	}
	if len(report.Methods) != 1 || report.Methods[0].Coverage != 100 {
		t.Errorf("expected Double at 100%% coverage, got %+v", report.Methods)
	}
}
