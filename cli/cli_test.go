package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Run(args, &out, &errb)
	return out.String(), errb.String(), code
}

func TestVersionCommand(t *testing.T) {
	stdout, _, code := run(t, "version")
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("version output not JSON: %v", err)
	}
	if payload["name"] != "slopguard-go" || payload["version"] == "" {
		t.Errorf("version payload = %v", payload)
	}
}

func tempGoFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := `package x

func tangled(a, b, c bool) int {
	if a {
		if b {
			if c {
				return 1
			}
		}
	}
	return 0
}
`
	if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAnalyzeNoCoverageText(t *testing.T) {
	dir := tempGoFile(t)
	stdout, _, code := run(t, "analyze", "--path", dir, "--no-coverage")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "tangled") || !strings.Contains(stdout, "Summary") {
		t.Errorf("unexpected output:\n%s", stdout)
	}
}

func TestAnalyzeJSON(t *testing.T) {
	dir := tempGoFile(t)
	stdout, _, code := run(t, "analyze", "--path", dir, "--no-coverage", "--json")
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, stdout)
	}
	if report["tool"] != "slopguard-go" {
		t.Errorf("tool = %v", report["tool"])
	}
}

func TestAnalyzeFailOverExitsTwo(t *testing.T) {
	dir := tempGoFile(t)
	// tangled is complex and uncovered, so its CRAP is well above 5.
	_, stderr, code := run(t, "analyze", "--path", dir, "--no-coverage", "--fail-over", "5")
	if code != 2 {
		t.Fatalf("exit = %d, want 2\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, "fail-over") {
		t.Errorf("stderr should explain fail-over: %s", stderr)
	}
}

func TestAnalyzeFailOverPasses(t *testing.T) {
	dir := tempGoFile(t)
	_, _, code := run(t, "analyze", "--path", dir, "--no-coverage", "--fail-over", "10000")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}

func TestInvalidThreshold(t *testing.T) {
	_, _, code := run(t, "analyze", "--threshold", "notanumber")
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
}

func TestErrorAsJSON(t *testing.T) {
	stdout, stderr, code := run(t, "analyze", "--path", "/no/such/dir/xyz", "--no-coverage", "--json")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	_ = stdout
	if !strings.Contains(stderr, `"error"`) || !strings.Contains(stderr, "file_not_found") {
		t.Errorf("expected JSON error envelope on stderr: %s", stderr)
	}
}
