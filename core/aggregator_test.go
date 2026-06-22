package core

import "testing"

// fakeCoverage returns a fixed percentage for every query.
type fakeCoverage struct{ pct float64 }

func (f fakeCoverage) MethodCoverage(string, int, int) (float64, bool) { return f.pct, true }
func (f fakeCoverage) FileCoverage(string) (float64, bool)             { return f.pct, true }

func TestAggregateAssociatesMethodsAcrossFilesByReceiver(t *testing.T) {
	// Parser is declared in a.go; its methods live in a.go and b.go (same dir).
	reports := []FileReport{
		{
			Path:  "pkg/a.go",
			Types: []TypeDecl{{Kind: KindStruct, Name: "Parser", File: "pkg/a.go", StartLine: 3, EndLine: 3}},
			Methods: []MethodMetric{
				MakeMethodMetric(MethodMetric{Name: "Parse", QualifiedName: "Parser.Parse", TypeName: "Parser", Kind: KindMethod, File: "pkg/a.go", StartLine: 5, EndLine: 12, Complexity: 6, CognitiveComplexity: 6}),
			},
		},
		{
			Path: "pkg/b.go",
			Methods: []MethodMetric{
				MakeMethodMetric(MethodMetric{Name: "reset", QualifiedName: "Parser.reset", TypeName: "Parser", Kind: KindMethod, File: "pkg/b.go", StartLine: 4, EndLine: 6, Complexity: 2, CognitiveComplexity: 1}),
			},
		},
	}

	report := Aggregate(AggregateArgs{FileReports: reports, SourceRoot: "/root", Coverage: nil})

	if report.Summary.MethodCount != 2 {
		t.Fatalf("method count = %d, want 2", report.Summary.MethodCount)
	}
	if len(report.Types) != 1 {
		t.Fatalf("type count = %d, want 1", len(report.Types))
	}
	parser := report.Types[0]
	if parser.MethodCount != 2 {
		t.Errorf("Parser.MethodCount = %d, want 2 (cross-file receiver association)", parser.MethodCount)
	}
	if parser.TotalComplexity != 8 || parser.MaxComplexity != 6 {
		t.Errorf("Parser totals: total=%d max=%d, want 8/6", parser.TotalComplexity, parser.MaxComplexity)
	}
}

func TestAggregateCoverageDrivesCrap(t *testing.T) {
	reports := []FileReport{{
		Path: "x.go",
		Methods: []MethodMetric{
			MakeMethodMetric(MethodMetric{Name: "f", QualifiedName: "f", Kind: KindFunction, File: "x.go", StartLine: 1, EndLine: 10, Complexity: 20, CognitiveComplexity: 20}),
		},
	}}

	uncovered := Aggregate(AggregateArgs{FileReports: reports, SourceRoot: "/r", Coverage: fakeCoverage{0}})
	covered := Aggregate(AggregateArgs{FileReports: reports, SourceRoot: "/r", Coverage: fakeCoverage{100}})

	if uncovered.Methods[0].Crap <= covered.Methods[0].Crap {
		t.Errorf("uncovered crap (%v) should exceed covered crap (%v)", uncovered.Methods[0].Crap, covered.Methods[0].Crap)
	}
	if !uncovered.Methods[0].IsCrappy {
		t.Error("a complex uncovered method should be crappy")
	}
	if covered.Methods[0].IsCrappy {
		t.Error("a fully covered method should collapse below threshold")
	}
	if covered.Summary.WeightedCoverage == nil || *covered.Summary.WeightedCoverage != 100 {
		t.Errorf("weighted coverage = %v, want 100", covered.Summary.WeightedCoverage)
	}
}

func TestAggregateNoCoverageLeavesWeightedNil(t *testing.T) {
	reports := []FileReport{{Path: "x.go", Methods: []MethodMetric{
		MakeMethodMetric(MethodMetric{Name: "f", QualifiedName: "f", Kind: KindFunction, File: "x.go", StartLine: 1, EndLine: 2, Complexity: 1, CognitiveComplexity: 0}),
	}}}
	report := Aggregate(AggregateArgs{FileReports: reports, SourceRoot: "/r", Coverage: nil})
	if report.Summary.WeightedCoverage != nil {
		t.Errorf("weighted coverage should be nil without coverage, got %v", *report.Summary.WeightedCoverage)
	}
	if report.CoverageAvailable {
		t.Error("CoverageAvailable should be false")
	}
}

func TestAggregateNamedTypeWithoutMethodsOmitted(t *testing.T) {
	reports := []FileReport{{
		Path:  "x.go",
		Types: []TypeDecl{{Kind: KindNamed, Name: "Celsius", File: "x.go", StartLine: 1, EndLine: 1}},
	}}
	report := Aggregate(AggregateArgs{FileReports: reports, SourceRoot: "/r"})
	if len(report.Types) != 0 {
		t.Errorf("named type with no methods should be omitted, got %d types", len(report.Types))
	}
}
