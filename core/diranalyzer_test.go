package core

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAnalyzeTreeRespectsExcludes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "main.go", "package m\nfunc A() {}\n")
	writeFile(t, root, "main_test.go", "package m\nfunc TestA() {}\n")
	writeFile(t, root, "vendor/dep/dep.go", "package dep\nfunc B() {}\n")
	writeFile(t, root, "api.pb.go", "package m\nfunc Gen() {}\n")
	writeFile(t, root, "sub/util.go", "package sub\nfunc C() {}\n")

	reports, err := AnalyzeTree(root, DefaultAnalysisOptions())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range reports {
		got[r.Path] = true
	}
	if !got["main.go"] || !got["sub/util.go"] {
		t.Errorf("expected main.go and sub/util.go, got %v", got)
	}
	for _, excluded := range []string{"main_test.go", "vendor/dep/dep.go", "api.pb.go"} {
		if got[excluded] {
			t.Errorf("%s should have been excluded", excluded)
		}
	}
}

func TestAnalyzeTreeSingleFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "only.go", "package m\nfunc A() {}\n")
	reports, err := AnalyzeTree(filepath.Join(root, "only.go"), DefaultAnalysisOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].Path != "only.go" {
		t.Errorf("single-file analysis wrong: %+v", reports)
	}
}

func TestAnalyzeTreeMissingPath(t *testing.T) {
	_, err := AnalyzeTree("/no/such/path/xyz", DefaultAnalysisOptions())
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	var se *SlopguardError
	if !asSlopguardError(err, &se) || se.Code != ErrFileNotFound {
		t.Errorf("expected file_not_found, got %v", err)
	}
}
