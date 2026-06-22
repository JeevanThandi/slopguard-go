package coverage

import (
	"bytes"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JeevanThandi/slopguard-go/core"
)

func TestResolveDiskPathVariants(t *testing.T) {
	cases := []struct {
		name, module, root, want string
	}{
		{"example.com/mod/pkg/f.go", "example.com/mod", "/proj", filepath.Join("/proj", "pkg", "f.go")},
		{"example.com/mod", "example.com/mod", "/proj", "/proj"},
		{"/already/abs.go", "example.com/mod", "/proj", "/already/abs.go"},
		{"other/rel.go", "example.com/mod", "/proj", filepath.Join("/proj", "other", "rel.go")},
		{"bare.go", "", "/proj", filepath.Join("/proj", "bare.go")},
	}
	for _, c := range cases {
		if got := resolveDiskPath(c.name, c.module, c.root); got != c.want {
			t.Errorf("resolveDiskPath(%q,%q,%q) = %q, want %q", c.name, c.module, c.root, got, c.want)
		}
	}
}

func TestSharedSuffixLen(t *testing.T) {
	if got := sharedSuffixLen("/a/b/calc.go", "/x/y/calc.go"); got != len("/calc.go") {
		t.Errorf("sharedSuffixLen = %d, want %d", got, len("/calc.go"))
	}
	if sharedSuffixLen("abc", "xyz") != 0 {
		t.Error("no shared suffix should be 0")
	}
}

func TestLookupPicksLongestSuffix(t *testing.T) {
	// Two files share a basename; the longest path-suffix match wins.
	profile := "mode: set\n" +
		"m/a/dup.go:1.1,2.2 1 1\n" +
		"m/b/dup.go:1.1,2.2 1 0\n"
	p, _ := ParseProfile(profile)
	idx := NewCoverageIndex(p, "m", "/proj")
	// Query resolves to .../b/dup.go — should pick the b candidate (count 0).
	pct, ok := idx.MethodCoverage("/somewhere/b/dup.go", 1, 2)
	if !ok {
		t.Fatal("expected a suffix match")
	}
	if pct != 0 {
		t.Errorf("expected the b/dup.go (uncovered) candidate, got %v", pct)
	}
}

func TestParseProfileNoModeLineTolerated(t *testing.T) {
	p, err := ParseProfile("example.com/m/f.go:1.1,2.2 1 1\n")
	if err != nil {
		t.Fatalf("missing mode line should be tolerated: %v", err)
	}
	if len(p.Files) != 1 {
		t.Errorf("expected one file, got %d", len(p.Files))
	}
}

func TestProfileHasDataMissingFile(t *testing.T) {
	if profileHasData("/no/such/profile.out") {
		t.Error("missing profile should report no data")
	}
}

func TestAsExitErrorNonExit(t *testing.T) {
	var ee *exec.ExitError
	if asExitError(errors.New("plain"), &ee) {
		t.Error("a plain error is not an ExitError")
	}
}

func TestTailWriterBounds(t *testing.T) {
	var buf bytes.Buffer
	w := &tailWriter{buf: &buf, limit: 4, progress: core.SilentReporter()}
	w.Write([]byte("abcdefgh")) // exceeds limit; oldest bytes dropped
	got := buf.String()
	if len(got) > 4 {
		t.Errorf("tail = %q, want <= 4 bytes", got)
	}
	if !strings.HasSuffix("abcdefgh", got) {
		t.Errorf("tail %q should be a suffix of the input", got)
	}
}

func TestLoadCoverageIndexErrors(t *testing.T) {
	if _, err := loadCoverageIndex("/no/such.out", "m", "/proj"); err == nil {
		t.Error("unreadable profile should error")
	}
	bad := filepath.Join(t.TempDir(), "bad.out")
	mustWrite(t, bad, "mode: set\ngarbage line\n")
	if _, err := loadCoverageIndex(bad, "m", "/proj"); err == nil {
		t.Error("malformed profile should error")
	}
}

func TestProjectContextFallsBackWhenNoModule(t *testing.T) {
	dir := t.TempDir() // no go.mod anywhere above
	root, module := projectContext("", dir)
	if module != "" {
		t.Errorf("module = %q, want empty", module)
	}
	if root != dir {
		t.Errorf("root = %q, want %q", root, dir)
	}
}

func TestModulePathMissingGoMod(t *testing.T) {
	if got := ModulePath(t.TempDir()); got != "" {
		t.Errorf("ModulePath without go.mod = %q, want empty", got)
	}
}

func TestDiscoverProjectRootFromFile(t *testing.T) {
	root := setupModule(t)
	found, ok := DiscoverProjectRoot(filepath.Join(root, "calc.go"))
	if !ok || found != root {
		t.Errorf("from file: found=%q ok=%v, want %q", found, ok, root)
	}
}

func TestFileCoverageUnknownFile(t *testing.T) {
	p, _ := ParseProfile(sampleProfile)
	idx := NewCoverageIndex(p, "example.com/mod", "/project")
	if _, ok := idx.FileCoverage("/nope/missing.go"); ok {
		t.Error("unknown file should return ok=false")
	}
}
