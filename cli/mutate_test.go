package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// mutateSource writes x.go, which yields four mutants (4:5 remove_not, 5:12
// boundary and negate_conditional, 7:9 boolean_literal), into a directory
// with no go.mod, and returns the directory.
func mutateSource(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "x.go"), "package x\n\nfunc F(a, b int, ok bool) bool {\n\tif !ok {\n\t\treturn a < b\n\t}\n\treturn false\n}\n")
	return dir
}

// mutateModule writes a module whose calc.go yields three mutants and returns
// its root.
func mutateModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/calc\n\ngo 1.23\n")
	mustWriteFile(t, filepath.Join(root, "calc.go"), "package calc\n\nfunc Max(a, b int) int {\n\tif a > b {\n\t\treturn a\n\t}\n\treturn b\n}\n\nfunc Double(n int) int { return n * 2 }\n")
	return root
}

func TestMutateDryRunText(t *testing.T) {
	dir := mutateSource(t)
	stdout, stderr, code := run(t, "mutate", "--path", dir, "--dry-run")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	for _, want := range []string{"slopguard-go 0.2.0 — mutation report (schema 1)", "runner:    (not run)", "Mutants (4, not run)", "x.go:4:5  remove_not  `!` → ``  F"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "slopguard: generated 4 mutant(s) in 1 file(s)") {
		t.Errorf("progress should go to stderr: %q", stderr)
	}
	if strings.Contains(stdout, "slopguard:") {
		t.Error("progress must not leak into stdout")
	}
}

func TestMutateDryRunJSON(t *testing.T) {
	dir := mutateSource(t)
	stdout, stderr, code := run(t, "mutate", "--path", dir, "--dry-run", "--json", "--quiet", "--operators", "remove_not")
	if code != 0 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	summary := report["summary"].(map[string]any)
	if report["reportType"] != "mutation" || report["runner"] != nil || summary["pending"] != float64(1) || summary["mutationScore"] != nil {
		t.Errorf("report = %v", report)
	}
	if ops := report["operators"].([]any); len(ops) != 1 || ops[0] != "remove_not" {
		t.Errorf("operators = %v", ops)
	}
}

func TestUsageListsMutate(t *testing.T) {
	stdout, _, code := run(t, "--help")
	if code != 0 || !strings.Contains(stdout, "slopguard-go mutate [flags]") || !strings.Contains(stdout, "  mutate    ") {
		t.Errorf("top-level help should list mutate:\n%s", stdout)
	}
	_, stderr, _ := run(t, "mutate", "--help")
	for _, want := range []string{"go test -overlay", "slopguard-ignore-mutant(boundary)", "-fail-under score", "-operators ids", "-timeout seconds"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("mutate help missing %q:\n%s", want, stderr)
		}
	}
}

func TestMutateRejectsAnalyzeOnlyFlags(t *testing.T) {
	dir := mutateSource(t)
	for _, flag := range [][]string{{"--threshold", "5"}, {"-t", "5"}, {"--fail-over", "3"}, {"--coverage-file", "c.out"}} {
		args := append([]string{"mutate", "--path", dir, "--dry-run"}, flag...)
		_, stderr, code := run(t, args...)
		if code != 1 || !strings.Contains(stderr, "flag provided but not defined") {
			t.Errorf("%v: exit = %d, stderr = %q", flag, code, stderr)
		}
	}
}

func TestMutateRejectsPositionalArguments(t *testing.T) {
	dir := mutateSource(t)
	// The flag package stops at x.go and never parses the --dry-run after it.
	stdout, stderr, code := run(t, "mutate", "--path", dir, "x.go", "--dry-run")
	want := "slopguard-go: [invalid_argument] Invalid argument 'x.go': unexpected positional argument; pass the path with --path\n"
	if code != 1 || stdout != "" || stderr != want {
		t.Errorf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	// --json came before the argument, so the error is the JSON envelope.
	_, stderr, code = run(t, "mutate", "--json", "--path", dir, "--dry-run", "extra")
	var envelope struct {
		Error struct{ Code, Message string }
	}
	if err := json.Unmarshal([]byte(stderr), &envelope); err != nil || code != 1 ||
		envelope.Error.Code != "invalid_argument" || !strings.HasPrefix(envelope.Error.Message, "Invalid argument 'extra': ") {
		t.Errorf("json: exit = %d, stderr = %q", code, stderr)
	}
}

func TestMutateValidatesFlagValues(t *testing.T) {
	dir := mutateSource(t)
	cases := []struct {
		args    []string
		message string
	}{
		{[]string{"--operators", "boundary,flip"}, "unknown operator(s): flip"},
		{[]string{"--timeout", "0"}, "Invalid argument '--timeout': not a positive number: 0"},
		{[]string{"--timeout", "-2"}, "not a positive number: -2"},
		{[]string{"--timeout", "soon"}, "not a positive number: soon"},
		{[]string{"--timeout", ""}, "not a positive number: "},
		{[]string{"--timeout", "NaN"}, "not a positive number: NaN"},
		{[]string{"--timeout", "Inf"}, "not a positive number: Inf"},
		{[]string{"--fail-under", "high"}, "Invalid argument '--fail-under': not a number: high"},
		{[]string{"--fail-under", "NaN"}, "not a number: NaN"},
	}
	for _, c := range cases {
		args := append([]string{"mutate", "--path", dir, "--dry-run", "--json"}, c.args...)
		_, stderr, code := run(t, args...)
		var envelope struct {
			Error struct{ Code, Message string }
		}
		if err := json.Unmarshal([]byte(stderr), &envelope); err != nil {
			t.Errorf("%v: stderr is not a JSON envelope: %q", c.args, stderr)
			continue
		}
		if code != 1 || envelope.Error.Code != "invalid_argument" || !strings.Contains(envelope.Error.Message, c.message) {
			t.Errorf("%v: exit = %d, envelope = %+v", c.args, code, envelope.Error)
		}
	}
	// Without --json the same error is one text line.
	_, stderr, code := run(t, "mutate", "--path", dir, "--timeout", "0")
	if code != 1 || !strings.HasPrefix(stderr, "slopguard-go: [invalid_argument] ") {
		t.Errorf("text error: exit = %d, stderr = %q", code, stderr)
	}
}

func TestMutateFailUnderIsIgnoredInADryRun(t *testing.T) {
	dir := mutateSource(t)
	if _, stderr, code := run(t, "mutate", "--path", dir, "--dry-run", "--fail-under", "90"); code != 0 {
		t.Errorf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestMutateNullScoreNeverFails(t *testing.T) {
	// Every mutant is ignored, so nothing runs and the score is n/a.
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "x.go"), "package x\n\nfunc F(a, b int) bool { return a < b } // slopguard-ignore-mutant\n")
	stdout, stderr, code := run(t, "mutate", "--path", dir, "--fail-under", "50")
	if code != 0 || !strings.Contains(stdout, "score:          n/a") {
		t.Errorf("exit = %d, stderr = %q, stdout:\n%s", code, stderr, stdout)
	}
}

func TestMutateErrors(t *testing.T) {
	_, stderr, code := run(t, "mutate", "--path", "/no/such/dir/xyz")
	if code != 1 || !strings.Contains(stderr, "slopguard-go: [file_not_found]") {
		t.Errorf("missing path: exit = %d, stderr = %q", code, stderr)
	}
	// A real run needs a module root; the flags below all parse.
	dir := mutateSource(t)
	_, stderr, code = run(t, "mutate", "-p", dir, "-v", "--quiet", "--packages", "./...", "--no-coverage",
		"--include", "**/*.go", "--exclude", "**/gen/**", "--no-default-excludes", "--operators", "remove_not", "--operators", "boundary",
		"--timeout", "5")
	if code != 1 || !strings.Contains(stderr, "[project_root_not_found]") || strings.Contains(stderr, "--no-coverage") {
		t.Errorf("no go.mod: exit = %d, stderr = %q", code, stderr)
	}
	_, stderr, code = run(t, "mutate", "--path", dir, "--project-dir", "~/no/such/project/xyz", "--json")
	if code != 1 || !strings.Contains(stderr, `"file_not_found"`) {
		t.Errorf("missing project dir: exit = %d, stderr = %q", code, stderr)
	}
}

type otherSignal struct{}

func (otherSignal) String() string { return "other" }
func (otherSignal) Signal()        {}

func TestSignalExitCode(t *testing.T) {
	cases := map[os.Signal]int{os.Interrupt: 130, syscall.SIGTERM: 143, syscall.SIGHUP: 129, otherSignal{}: 130}
	for sig, want := range cases {
		if got := signalExitCode(sig); got != want {
			t.Errorf("signalExitCode(%v) = %d, want %d", sig, got, want)
		}
	}
}

// fakeSignals replaces notifySignals for one test and returns a function
// that delivers a signal as if the process had received it.
func fakeSignals(t *testing.T) (send func(os.Signal)) {
	t.Helper()
	channels := make(chan chan<- os.Signal, 1)
	original := notifySignals
	notifySignals = func(c chan<- os.Signal) func() {
		channels <- c
		return func() {}
	}
	t.Cleanup(func() { notifySignals = original })
	return func(sig os.Signal) { (<-channels) <- sig }
}

func TestInterruptContextCancelsOnASignal(t *testing.T) {
	send := fakeSignals(t)
	ctx, caught, stop := interruptContext()
	defer stop()
	if caught() != nil {
		t.Fatal("no signal yet")
	}
	send(syscall.SIGTERM)
	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the context should be cancelled by the signal")
	}
	if caught() != syscall.SIGTERM {
		t.Errorf("caught = %v, want SIGTERM", caught())
	}
}

func TestInterruptContextStopsCleanly(t *testing.T) {
	_, caught, stop := interruptContext() // the real signal.Notify
	stop()
	if caught() != nil {
		t.Error("no signal was sent")
	}
	ctx, _, stop2 := interruptContext()
	stop2()
	if ctx.Err() != context.Canceled {
		t.Error("stop should release the context")
	}
}

// TestMutateSampleAppEndToEnd runs the built-in `mutate` against the sample
// app with the real go test. CI pins the same numbers on the binary.
func TestMutateSampleAppEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	stdout, stderr, code := run(t, "mutate", "--path", filepath.Join("..", "sampleapps", "todolist"), "--json", "--quiet", "--fail-under", "100")
	if code != 0 {
		t.Fatalf("exit = %d, stderr: %s", code, stderr)
	}
	var report struct {
		CoverageAvailable bool
		Summary           struct {
			MutantCount, Killed, TimedOut, Survived, NoCoverage, CompileErrors, Ignored int
			MutationScore                                                               *float64
		}
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	s := report.Summary
	if !report.CoverageAvailable || s.MutantCount != 17 || s.Killed+s.TimedOut != 16 || s.Survived != 0 ||
		s.NoCoverage != 0 || s.CompileErrors != 0 || s.Ignored != 1 || s.MutationScore == nil || *s.MutationScore != 100 {
		t.Errorf("sample app mutation baseline drifted: %+v (score %v)", s, s.MutationScore)
	}
}
