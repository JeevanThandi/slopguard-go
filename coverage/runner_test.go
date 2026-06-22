package coverage

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JeevanThandi/slopguard-go/core"
)

// TestRealRunnerProducesProfile drives the actual `go test` toolchain against a
// throwaway module. Skipped when `go` is not on PATH or in -short mode.
func TestRealRunnerProducesProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}

	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/tested\n\ngo 1.23\n")
	mustWrite(t, filepath.Join(root, "calc.go"), `package tested

func Double(n int) int { return n * 2 }
`)
	mustWrite(t, filepath.Join(root, "calc_test.go"), `package tested

import "testing"

func TestDouble(t *testing.T) {
	if Double(3) != 6 {
		t.Fail()
	}
}
`)

	coverDir := t.TempDir()
	runner := &TestRunner{}
	outcome, err := runner.RunTests(root, coverDir, "./...", core.SilentReporter())
	if err != nil {
		t.Fatalf("RunTests error: %v", err)
	}
	if !outcome.TestsPassed {
		t.Error("tests should have passed")
	}
	if outcome.ProfilePath == "" {
		t.Fatal("expected a coverage profile path")
	}
	if _, err := os.Stat(outcome.ProfilePath); err != nil {
		t.Errorf("profile not written: %v", err)
	}

	profile, err := ParseProfile(readFileT(t, outcome.ProfilePath))
	if err != nil {
		t.Fatalf("parse produced profile: %v", err)
	}
	idx := NewCoverageIndex(profile, "example.com/tested", root)
	if idx.FileCount() == 0 {
		t.Error("expected at least one file in the coverage index")
	}
}

func TestRealRunnerBuildFailureErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/broken\n\ngo 1.23\n")
	mustWrite(t, filepath.Join(root, "broken.go"), "package broken\nthis is not go\n")

	runner := &TestRunner{}
	_, err := runner.RunTests(root, t.TempDir(), "./...", core.SilentReporter())
	if err == nil {
		t.Fatal("expected a build failure error")
	}
	var se *core.SlopguardError
	if !asCoreErr(err, &se) || se.Code != core.ErrTestRunFailed {
		t.Errorf("expected test_run_failed, got %v", err)
	}
	// The captured output tail must carry the real compiler output (naming the
	// offending file), not the "no output captured" fallback.
	if !strings.Contains(se.Message, "broken") {
		t.Errorf("error should include captured build output naming broken.go, got: %q", se.Message)
	}
}

func readFileT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func asCoreErr(err error, target **core.SlopguardError) bool {
	if se, ok := err.(*core.SlopguardError); ok {
		*target = se
		return true
	}
	return false
}
