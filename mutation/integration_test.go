package mutation

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/JeevanThandi/slopguard-go/core"
)

func requireGo(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping subprocess test in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
}

// integrationSource yields one mutant per status the real go test can
// produce quickly: survived, killed, compile_error, no_coverage and ignored.
const integrationSource = `package calc

// Max returns the larger of a and b.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Greet joins a greeting and a name.
func Greet(name string) string {
	greeting := "hello, "
	return greeting + name
}

// Untested has no test.
func Untested(n int) int {
	return n * 3
}

// Double is excluded from mutation.
func Double(n int) int {
	return n + n // slopguard-ignore-mutant
}
`

const integrationTests = `package calc

import "testing"

func TestMax(t *testing.T) {
	if Max(3, 2) != 3 || Max(2, 3) != 3 {
		t.Fatal("Max is wrong")
	}
}

func TestGreet(t *testing.T) {
	if Greet("bob") != "hello, bob" {
		t.Fatal("Greet is wrong")
	}
}

func TestDouble(t *testing.T) { _ = Double(2) }
`

// TestRealRunAgainstGoTest drives the real toolchain end to end. The source
// path is a symlink, so the run also proves that the -overlay keys match the
// paths the go command uses (a mismatch would make every mutant survive).
func TestRealRunAgainstGoTest(t *testing.T) {
	requireGo(t)
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "go.mod"), "module example.com/calc\n\ngo 1.23\n")
	mustWrite(t, filepath.Join(root, "calc.go"), integrationSource)
	mustWrite(t, filepath.Join(root, "calc_test.go"), integrationTests)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		link = root
	}

	report, err := Run(Args{SourcePath: link, Options: core.DefaultAnalysisOptions()})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]core.MutantStatus{
		// Max(3,3) returns 3 either way: an equivalent mutant.
		"calc.go:5:7:boundary":           core.StatusSurvived,
		"calc.go:5:7:negate_conditional": core.StatusKilled,
		// greeting - name does not compile for strings.
		"calc.go:14:18:arithmetic": core.StatusCompileError,
		"calc.go:19:11:arithmetic": core.StatusNoCoverage,
		"calc.go:24:11:arithmetic": core.StatusIgnored,
	}
	if got := statuses(report); !reflect.DeepEqual(got, want) {
		t.Errorf("statuses = %v\nwant %v", got, want)
	}
	if !report.CoverageAvailable {
		t.Error("the coverage baseline should have produced coverage")
	}
	if want := []string{"1 mutant(s) did not compile and are excluded from the score."}; !reflect.DeepEqual(report.Notes, want) {
		t.Errorf("notes = %q", report.Notes)
	}
	data, _ := os.ReadFile(filepath.Join(root, "calc.go"))
	if string(data) != integrationSource {
		t.Error("mutate must never write to the source tree")
	}
}
