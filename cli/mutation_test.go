package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This file closes mutation-testing gaps in the CLI wiring.

func TestFailOverBoundaryNotExceeded(t *testing.T) {
	// A trivial function has weightedComplexity 0 → CRAP 0. With --fail-over 0,
	// the worst CRAP (0) does NOT exceed 0, so exit 0 (strict >).
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "x.go"), "package x\n\nfunc F() {}\n")
	_, _, code := run(t, "analyze", "--path", dir, "--no-coverage", "--fail-over", "0")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (CRAP 0 does not exceed fail-over 0)", code)
	}
}

func TestDefaultExcludesApplyWithoutFlag(t *testing.T) {
	// Without --no-default-excludes, a *_test.go file is excluded by default.
	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, "keep.go"), "package x\n\nfunc Keep() {}\n")
	mustWriteFile(t, filepath.Join(dir, "helper_test.go"), "package x\n\nfunc Helper() {}\n")
	out, _, code := run(t, "analyze", "--path", dir, "--no-coverage", "--quiet")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(out, "Keep") {
		t.Errorf("expected Keep in output:\n%s", out)
	}
	if strings.Contains(out, "Helper") {
		t.Errorf("default excludes should drop *_test.go, but Helper appeared:\n%s", out)
	}
}

func TestZeroArgsRunsWithoutPanic(t *testing.T) {
	// A bare invocation (no args) must dispatch to analyze on "." without
	// indexing an empty args slice.
	dir := t.TempDir() // no go.mod above → auto coverage fails fast, no panic
	mustWriteFile(t, filepath.Join(dir, "x.go"), "package x\n\nfunc F() {}\n")
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if code := Run(nil, io.Discard, io.Discard); code != 1 {
		t.Errorf("zero-arg run code = %d, want 1 (project_root_not_found)", code)
	}
}

func TestNonVerboseAutoCoverageHidesSubprocessOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/cliquiet\n\ngo 1.23\n")
	mustWriteFile(t, filepath.Join(root, "calc.go"), "package cliquiet\n\nfunc Triple(n int) int { return n * 3 }\n")
	mustWriteFile(t, filepath.Join(root, "calc_test.go"), "package cliquiet\n\nimport \"testing\"\n\nfunc TestTriple(t *testing.T) {\n\tif Triple(2) != 6 {\n\t\tt.Fail()\n\t}\n}\n")

	// No --verbose and no --quiet: phase markers appear, but raw `go test`
	// chatter ("... of statements") must NOT, since the default reporter is
	// Normal. Guards the --verbose flag default against a true flip.
	_, stderr, code := run(t, "analyze", "--path", root, "--json")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stderr, "slopguard:") {
		t.Errorf("expected phase markers on stderr: %q", stderr)
	}
	if strings.Contains(stderr, "of statements") {
		t.Errorf("non-verbose run leaked raw go-test output to stderr:\n%s", stderr)
	}
}
