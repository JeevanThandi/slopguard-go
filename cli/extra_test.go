package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDispatch(t *testing.T) {
	for _, arg := range []string{"--help", "-h", "help"} {
		out, _, code := run(t, arg)
		if code != 0 || !strings.Contains(out, "Usage:") {
			t.Errorf("%q: code=%d out=%q", arg, code, out)
		}
	}
	out, _, code := run(t, "--version")
	if code != 0 || !strings.Contains(out, "0.2.0") {
		t.Errorf("--version: code=%d out=%q", code, out)
	}
}

func TestUnknownCommandTreatedAsAnalyzeFlags(t *testing.T) {
	// A bare unknown token is treated as analyze flags; an unknown flag errors.
	_, stderr, code := run(t, "--bogus-flag")
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if stderr == "" {
		t.Error("expected a flag-parse error on stderr")
	}
}

func TestExplicitAnalyzeSubcommand(t *testing.T) {
	dir := tempGoFile(t)
	_, _, code := run(t, "analyze", "--path", dir, "--no-coverage", "--quiet")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
}

func TestIncludeExcludeAndNoDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "keep.go"), []byte("package x\nfunc Keep(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.go"), []byte("package x\nfunc Skip(){}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Repeatable --include exercises stringSlice.Set; --no-default-excludes too.
	out, _, code := run(t, "analyze", "--path", dir, "--no-coverage", "--quiet",
		"--include", "**/keep.go", "--no-default-excludes", "--exclude", "**/nothing.go")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out, "Keep") || strings.Contains(out, "Skip") {
		t.Errorf("include filter not applied:\n%s", out)
	}
}

func TestCoverageFileFlagThroughCLI(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/c\n\ngo 1.23\n")
	mustWriteFile(t, filepath.Join(root, "calc.go"), "package c\n\nfunc Add(a, b int) int {\n\tif a > b {\n\t\treturn a\n\t}\n\treturn b\n}\n")
	profile := filepath.Join(t.TempDir(), "cover.out")
	mustWriteFile(t, profile, "mode: set\nexample.com/c/calc.go:3.30,8.2 3 1\n")

	out, _, code := run(t, "analyze", "--path", root, "--coverage-file", profile, "--quiet", "--json")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out, `"coverage": 100`) {
		t.Errorf("expected 100%% coverage joined from the profile:\n%s", out)
	}
}

func TestProjectDirFlagParsed(t *testing.T) {
	dir := tempGoFile(t)
	// --project-dir is accepted; with --no-coverage it's just recorded, not used.
	_, _, code := run(t, "analyze", "--path", dir, "--no-coverage", "--quiet", "--project-dir", dir, "--verbose")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
}

func TestInvalidFailOver(t *testing.T) {
	dir := tempGoFile(t)
	_, _, code := run(t, "analyze", "--path", dir, "--no-coverage", "--fail-over", "notanumber")
	if code != 1 {
		t.Errorf("code = %d, want 1", code)
	}
}

func TestExpandTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := expandTilde("~/sub"); got != filepath.Join(home, "sub") {
		t.Errorf("expandTilde(~/sub) = %q", got)
	}
	if got := expandTilde("~"); got != home {
		t.Errorf("expandTilde(~) = %q, want %q", got, home)
	}
	if got := expandTilde("/abs/path"); got != "/abs/path" {
		t.Errorf("expandTilde absolute changed: %q", got)
	}
}

func TestStringSliceStringer(t *testing.T) {
	var s stringSlice
	s.Set("a")
	s.Set("b")
	if s.String() != "a,b" {
		t.Errorf("String() = %q, want a,b", s.String())
	}
}

func TestVerboseProgressGoesToStderr(t *testing.T) {
	dir := tempGoFile(t)
	// --verbose without --quiet exercises the verbose reporter branch; progress
	// markers must land on stderr, not stdout.
	stdout, stderr, code := run(t, "analyze", "--path", dir, "--no-coverage", "--verbose")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(stderr, "slopguard:") {
		t.Errorf("expected progress markers on stderr: %q", stderr)
	}
	if strings.Contains(stdout, "slopguard: walking") {
		t.Error("progress must not leak into stdout")
	}
}

func TestFailOverWithNoMethods(t *testing.T) {
	// An empty directory yields zero methods, so --fail-over has nothing to
	// gate on and the run succeeds.
	dir := t.TempDir()
	_, _, code := run(t, "analyze", "--path", dir, "--no-coverage", "--quiet", "--fail-over", "1")
	if code != 0 {
		t.Errorf("code = %d, want 0 (no methods to gate)", code)
	}
}

func TestTextErrorPath(t *testing.T) {
	// Without --json, errors render as a single text line, not an envelope.
	_, stderr, code := run(t, "analyze", "--path", "/no/such/dir/xyz", "--no-coverage")
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "slopguard-go: [file_not_found]") {
		t.Errorf("expected a text error line, got %q", stderr)
	}
	if strings.Contains(stderr, `"error"`) {
		t.Error("non-json error path must not emit a JSON envelope")
	}
}

func TestAutoCoverageThroughCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in -short mode")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not on PATH")
	}
	// A real module with a test — exercises the default (auto) coverage branch
	// end-to-end through the CLI.
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "go.mod"), "module example.com/cliauto\n\ngo 1.23\n")
	mustWriteFile(t, filepath.Join(root, "calc.go"), "package cliauto\n\nfunc Triple(n int) int { return n * 3 }\n")
	mustWriteFile(t, filepath.Join(root, "calc_test.go"), "package cliauto\n\nimport \"testing\"\n\nfunc TestTriple(t *testing.T) {\n\tif Triple(2) != 6 {\n\t\tt.Fail()\n\t}\n}\n")

	out, _, code := run(t, "analyze", "--path", root, "--quiet", "--json")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if !strings.Contains(out, `"coverageAvailable": true`) || !strings.Contains(out, `"coverage": 100`) {
		t.Errorf("expected real auto coverage in output:\n%s", out)
	}
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
