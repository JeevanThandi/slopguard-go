package coverage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverProjectRootWalksUp(t *testing.T) {
	root := setupModule(t)
	deep := filepath.Join(root, "a", "b")
	mustMkdir(t, deep)

	found, ok := DiscoverProjectRoot(deep)
	if !ok {
		t.Fatal("expected to find go.mod")
	}
	if found != root {
		t.Errorf("found %q, want %q", found, root)
	}
}

func TestModulePath(t *testing.T) {
	root := setupModule(t)
	if got := ModulePath(root); got != "example.com/mod" {
		t.Errorf("ModulePath = %q, want example.com/mod", got)
	}
}

func TestDiscoverProjectRootMissing(t *testing.T) {
	if _, ok := DiscoverProjectRoot(t.TempDir()); ok {
		t.Error("expected no go.mod in a bare temp dir")
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}
