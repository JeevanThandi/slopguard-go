package coverage

import (
	"path/filepath"
	"testing"
)

const sampleProfile = `mode: count
example.com/mod/calc.go:3.20,6.2 2 5
example.com/mod/calc.go:8.10,10.3 1 0
example.com/mod/sub/util.go:1.1,2.2 1 3
`

func TestParseProfile(t *testing.T) {
	p, err := ParseProfile(sampleProfile)
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != "count" {
		t.Errorf("mode = %q, want count", p.Mode)
	}
	blocks := p.Files["example.com/mod/calc.go"]
	if len(blocks) != 2 {
		t.Fatalf("calc.go blocks = %d, want 2", len(blocks))
	}
	if blocks[0].StartLine != 3 || blocks[0].EndLine != 6 || blocks[0].Count != 5 {
		t.Errorf("block[0] = %+v", blocks[0])
	}
}

func TestParseProfileMalformed(t *testing.T) {
	if _, err := ParseProfile("mode: set\nthis is not valid\n"); err == nil {
		t.Fatal("expected error on malformed line")
	}
}

func TestCoverageIndexResolvesAndScores(t *testing.T) {
	p, _ := ParseProfile(sampleProfile)
	idx := NewCoverageIndex(p, "example.com/mod", "/project")

	if idx.FileCount() != 2 {
		t.Fatalf("file count = %d, want 2", idx.FileCount())
	}

	calc := filepath.Join("/project", "calc.go")
	// Lines 3-6 covered (5>0), lines 8-10 uncovered (0). Method spanning 3-6:
	pct, ok := idx.MethodCoverage(calc, 3, 6)
	if !ok || pct != 100 {
		t.Errorf("MethodCoverage(3,6) = %v,%v want 100,true", pct, ok)
	}
	// Method spanning the uncovered block:
	pct, ok = idx.MethodCoverage(calc, 8, 10)
	if !ok || pct != 0 {
		t.Errorf("MethodCoverage(8,10) = %v,%v want 0,true", pct, ok)
	}
	// Whole file: 4 covered lines (3,4,5,6) of 7 (3-6, 8-10) => ~57%.
	fpct, ok := idx.FileCoverage(calc)
	if !ok || fpct < 50 || fpct > 60 {
		t.Errorf("FileCoverage = %v,%v want ~57", fpct, ok)
	}
}

func TestCoverageIndexBasenameFallback(t *testing.T) {
	p, _ := ParseProfile(sampleProfile)
	idx := NewCoverageIndex(p, "example.com/mod", "/project")
	// Query a different absolute root; basename + suffix fallback should match.
	other := "/some/where/else/calc.go"
	if _, ok := idx.MethodCoverage(other, 3, 6); !ok {
		t.Error("expected basename fallback to resolve calc.go")
	}
}

func TestUnknownFileReturnsNotOk(t *testing.T) {
	p, _ := ParseProfile(sampleProfile)
	idx := NewCoverageIndex(p, "example.com/mod", "/project")
	if _, ok := idx.MethodCoverage("/nope/missing.go", 1, 5); ok {
		t.Error("unknown file should return ok=false")
	}
}
