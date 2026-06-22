package core

import (
	"math"
	"strings"
	"testing"
)

// This file closes mutation-testing gaps: each test pins a specific arithmetic,
// boundary, or branch decision that a mutant would otherwise flip undetected.

// byStartLineCoverage returns a coverage percentage keyed by a method's start
// line, so a single report can mix differently-covered methods.
type byStartLineCoverage map[int]float64

func (m byStartLineCoverage) MethodCoverage(_ string, line, _ int) (float64, bool) {
	pct, ok := m[line]
	return pct, ok
}
func (m byStartLineCoverage) FileCoverage(string) (float64, bool) { return 0, false }

// --- aggregator.go: crappy classification is strict (>) and counters increment ---

func TestMethodAtExactThresholdIsNotCrappy(t *testing.T) {
	reports := []FileReport{{
		Path: "x.go",
		Methods: []MethodMetric{
			// cov=100 collapses CRAP to weightedComplexity = sqrt(30×30) = 30,
			// exactly the default threshold.
			MakeMethodMetric(MethodMetric{Name: "edge", QualifiedName: "edge", Kind: KindFunction, File: "x.go", StartLine: 1, EndLine: 5, Complexity: 30, CognitiveComplexity: 30}),
			// A genuinely crappy method so the counter is a positive number.
			MakeMethodMetric(MethodMetric{Name: "bad", QualifiedName: "bad", Kind: KindFunction, File: "x.go", StartLine: 10, EndLine: 20, Complexity: 30, CognitiveComplexity: 30}),
		},
	}}
	rep := Aggregate(AggregateArgs{FileReports: reports, SourceRoot: "/r", Coverage: byStartLineCoverage{1: 100, 10: 0}})
	byName := map[string]MethodCrap{}
	for _, m := range rep.Methods {
		byName[m.Name] = m
	}
	if byName["edge"].Crap != 30 {
		t.Fatalf("edge crap = %v, want exactly 30", byName["edge"].Crap)
	}
	if byName["edge"].IsCrappy {
		t.Error("a method exactly at the threshold must not be crappy (strict >)")
	}
	if !byName["bad"].IsCrappy {
		t.Error("an uncovered complex method must be crappy")
	}
	if rep.Summary.CrappyMethodCount != 1 {
		t.Errorf("crappyMethodCount = %d, want 1", rep.Summary.CrappyMethodCount)
	}
}

func TestCrappyTypeCount(t *testing.T) {
	reports := []FileReport{{
		Path:  "x.go",
		Types: []TypeDecl{{Kind: KindStruct, Name: "Big", File: "x.go", StartLine: 1, EndLine: 2}},
		Methods: []MethodMetric{
			MakeMethodMetric(MethodMetric{Name: "M", QualifiedName: "Big.M", TypeName: "Big", Kind: KindMethod, File: "x.go", StartLine: 3, EndLine: 30, Complexity: 30, CognitiveComplexity: 30}),
		},
	}}
	rep := Aggregate(AggregateArgs{FileReports: reports, SourceRoot: "/r"})
	if rep.Summary.CrappyTypeCount != 1 {
		t.Fatalf("crappyTypeCount = %d, want 1", rep.Summary.CrappyTypeCount)
	}
}

// --- aggregator.go: makeTypeCrap OR-semantics and exact threshold boundaries ---

func mc(crap float64, cyc, cog, cov, line, end int) MethodCrap {
	return MethodCrap{Crap: crap, Complexity: cyc, CognitiveComplexity: cog, Coverage: float64(cov), Line: line, EndLine: end}
}

func TestTypeIsCrappyViaOr(t *testing.T) {
	// aggregated (156) > threshold but max (20) ≤ threshold: the OR must flag it.
	owned := []MethodCrap{mc(20, 4, 4, 0, 1, 2), mc(20, 4, 4, 0, 3, 4), mc(20, 4, 4, 0, 5, 6)}
	got := makeTypeCrap(TypeDecl{Kind: KindStruct, Name: "T", File: "x.go", StartLine: 1}, owned, 30)
	if !got.IsCrappy {
		t.Errorf("type with aggregated=%.1f max=%.1f must be crappy via OR", got.AggregatedCrap, got.MaxCrap)
	}
}

func TestTypeAggregatedAtThresholdNotCrappy(t *testing.T) {
	// weightedTotal = sqrt(30×30) = 30, weightedCov = 100 → aggregated = 30
	// exactly; max = 15. Strict > keeps it clean.
	owned := []MethodCrap{mc(15, 15, 15, 100, 1, 2), mc(15, 15, 15, 100, 3, 4)}
	got := makeTypeCrap(TypeDecl{Kind: KindStruct, Name: "T", File: "x.go", StartLine: 1}, owned, 30)
	if got.AggregatedCrap != 30 {
		t.Fatalf("aggregated = %v, want exactly 30", got.AggregatedCrap)
	}
	if got.IsCrappy {
		t.Error("type aggregated exactly at threshold (max below) must not be crappy")
	}
}

func TestTypeMaxAtThresholdNotCrappy(t *testing.T) {
	// One uncovered method whose CRAP is exactly 30 (the max) with a low
	// aggregated score isolates the `max > threshold` boundary.
	owned := []MethodCrap{mc(30, 5, 5, 0, 1, 2), mc(0, 1, 0, 100, 3, 4)}
	got := makeTypeCrap(TypeDecl{Kind: KindStruct, Name: "T", File: "x.go", StartLine: 1}, owned, 30)
	if got.MaxCrap != 30 {
		t.Fatalf("max crap = %v, want 30", got.MaxCrap)
	}
	if got.AggregatedCrap >= 30 {
		t.Fatalf("aggregated = %v, want < 30 to isolate the max boundary", got.AggregatedCrap)
	}
	if got.IsCrappy {
		t.Error("type whose max equals the threshold exactly must not be crappy (strict >)")
	}
}

func TestTypeWeightedTotalIsGeometricMean(t *testing.T) {
	// totalCyc = 5, totalCog = 4 → weightedTotal = sqrt(20), not sqrt(5/4).
	owned := []MethodCrap{mc(0, 3, 0, 100, 1, 2), mc(0, 2, 4, 100, 3, 4)}
	got := makeTypeCrap(TypeDecl{Kind: KindStruct, Name: "T", File: "x.go", StartLine: 1}, owned, 30)
	if math.Abs(got.WeightedTotalComplexity-math.Sqrt(20)) > 1e-9 {
		t.Errorf("weightedTotal = %v, want sqrt(20)", got.WeightedTotalComplexity)
	}
}

// --- aggregator.go: weightedCoverage line-weighting ---

func TestWeightedCoverageSingleMethod(t *testing.T) {
	if got := weightedCoverage([]MethodCrap{mc(0, 1, 1, 80, 1, 4)}); got != 80 {
		t.Errorf("weightedCoverage(one 80%% method) = %v, want 80", got)
	}
}

func TestWeightedCoverageLineWeighted(t *testing.T) {
	// 100%-covered method over 10 lines + 0%-covered over 2 lines → 1000/12.
	got := weightedCoverage([]MethodCrap{mc(0, 1, 1, 100, 1, 10), mc(0, 1, 1, 0, 20, 21)})
	if math.Abs(got-1000.0/12.0) > 1e-9 {
		t.Errorf("weightedCoverage = %v, want %v", got, 1000.0/12.0)
	}
}

func TestWeightedCoverageSummaryLineWeighted(t *testing.T) {
	reports := []FileReport{{
		Path: "x.go",
		Methods: []MethodMetric{
			MakeMethodMetric(MethodMetric{Name: "big", QualifiedName: "big", Kind: KindFunction, File: "x.go", StartLine: 1, EndLine: 10, Complexity: 1, CognitiveComplexity: 1}),
			MakeMethodMetric(MethodMetric{Name: "small", QualifiedName: "small", Kind: KindFunction, File: "x.go", StartLine: 100, EndLine: 101, Complexity: 1, CognitiveComplexity: 1}),
		},
	}}
	rep := Aggregate(AggregateArgs{FileReports: reports, SourceRoot: "/r", Coverage: byStartLineCoverage{1: 100, 100: 0}})
	if rep.Summary.WeightedCoverage == nil {
		t.Fatal("weighted coverage should be present")
	}
	if want := 1000.0 / 12.0; math.Abs(*rep.Summary.WeightedCoverage-want) > 1e-9 {
		t.Errorf("summary weighted coverage = %v, want %v", *rep.Summary.WeightedCoverage, want)
	}
}

func TestWeightedCoverageZeroExecutableNoNaN(t *testing.T) {
	// EndLine before StartLine yields zero executable lines; with coverage
	// available the summary must report 0, not NaN.
	reports := []FileReport{{
		Path: "x.go",
		Methods: []MethodMetric{
			MakeMethodMetric(MethodMetric{Name: "f", QualifiedName: "f", Kind: KindFunction, File: "x.go", StartLine: 5, EndLine: 1, Complexity: 1, CognitiveComplexity: 0}),
		},
	}}
	rep := Aggregate(AggregateArgs{FileReports: reports, SourceRoot: "/r", Coverage: fakeCoverage{50}})
	if rep.Summary.WeightedCoverage == nil {
		t.Fatal("weighted coverage should be present")
	}
	if math.IsNaN(*rep.Summary.WeightedCoverage) || *rep.Summary.WeightedCoverage != 0 {
		t.Errorf("weighted coverage = %v, want 0 (no NaN)", *rep.Summary.WeightedCoverage)
	}
}

func TestSummaryAveragesAreComputed(t *testing.T) {
	reports := []FileReport{{
		Path: "x.go",
		Methods: []MethodMetric{
			MakeMethodMetric(MethodMetric{Name: "a", QualifiedName: "a", Kind: KindFunction, File: "x.go", StartLine: 1, EndLine: 2, Complexity: 4, CognitiveComplexity: 2}),
			MakeMethodMetric(MethodMetric{Name: "b", QualifiedName: "b", Kind: KindFunction, File: "x.go", StartLine: 3, EndLine: 4, Complexity: 2, CognitiveComplexity: 6}),
		},
	}}
	rep := Aggregate(AggregateArgs{FileReports: reports, SourceRoot: "/r"})
	if rep.Summary.AverageComplexity != 3 { // (4+2)/2
		t.Errorf("avg complexity = %v, want 3", rep.Summary.AverageComplexity)
	}
	if rep.Summary.AverageCognitiveComplexity != 4 { // (2+6)/2
		t.Errorf("avg cognitive = %v, want 4", rep.Summary.AverageCognitiveComplexity)
	}
	if want := (math.Sqrt(8) + math.Sqrt(12)) / 2; math.Abs(rep.Summary.AverageWeightedComplexity-want) > 1e-9 {
		t.Errorf("avg weighted = %v, want %v", rep.Summary.AverageWeightedComplexity, want)
	}
}

// --- complexity.go: nesting amplification and select cyclomatic ---

func TestNestedForAmplifiesCognitive(t *testing.T) {
	m := analyzeOne(t, `package p
func f(a bool, n int) {
	if a {
		for i := 0; i < n; i++ {
			println(i)
		}
	}
}`, "f")
	if m.CognitiveComplexity != 3 { // if(+1) + for at nesting 1 (+2)
		t.Errorf("cog = %d, want 3 (nested for amplified)", m.CognitiveComplexity)
	}
}

func TestNestedSwitchAmplifiesCognitive(t *testing.T) {
	m := analyzeOne(t, `package p
func f(a bool, n int) {
	if a {
		switch n {
		case 1:
			println(1)
		case 2:
			println(2)
		}
	}
}`, "f")
	if m.CognitiveComplexity != 3 { // if(+1) + switch at nesting 1 (+2)
		t.Errorf("cog = %d, want 3 (nested switch amplified)", m.CognitiveComplexity)
	}
}

func TestNestedTypeSwitchAmplifiesCognitive(t *testing.T) {
	m := analyzeOne(t, `package p
func f(a bool, x any) {
	if a {
		switch x.(type) {
		case int:
			println(1)
		}
	}
}`, "f")
	if m.CognitiveComplexity != 3 { // if(+1) + type switch at nesting 1 (+2)
		t.Errorf("cog = %d, want 3 (nested type switch amplified)", m.CognitiveComplexity)
	}
}

func TestNestedSelectAmplifiesCognitive(t *testing.T) {
	m := analyzeOne(t, `package p
func f(a bool, ch chan int) {
	if a {
		select {
		case <-ch:
			println(1)
		}
	}
}`, "f")
	if m.CognitiveComplexity != 3 { // if(+1) + select at nesting 1 (+2)
		t.Errorf("cog = %d, want 3 (nested select amplified)", m.CognitiveComplexity)
	}
}

func TestSelectCommCaseCountsCyclomatic(t *testing.T) {
	m := analyzeOne(t, `package p
func f(ch chan int) {
	select {
	case <-ch:
		println(1)
	}
}`, "f")
	if m.Complexity != 2 { // base 1 + one non-default comm case
		t.Errorf("cyc = %d, want 2 (select comm case)", m.Complexity)
	}
}

// --- format.go: padding width and empty-notes section ---

func TestPadStartExactWidth(t *testing.T) {
	if got := padStart("ab", 5); got != "   ab" {
		t.Errorf("padStart = %q, want %q", got, "   ab")
	}
	if got := padStart("abcdef", 3); got != "abcdef" {
		t.Errorf("padStart over-width = %q", got)
	}
}

func TestPrettyReportOmitsEmptyNotes(t *testing.T) {
	report := CrapReport{ToolVersion: Version, SchemaVersion: "2", Threshold: 30, Notes: nil, Methods: []MethodCrap{}}
	if out := PrettyReport(report, 20); strings.Contains(out, "Notes") {
		t.Errorf("empty notes must omit the Notes section:\n%s", out)
	}
}

// --- glob.go: '?' advance and single-character class ---

func TestGlobQuestionMark(t *testing.T) {
	re := GlobToRegexp("a?c")
	if !re.MatchString("abc") || !re.MatchString("axc") {
		t.Error("? should match exactly one character")
	}
	if re.MatchString("ac") || re.MatchString("abbc") {
		t.Error("? must match exactly one char, not zero or two")
	}
}

func TestGlobSingleCharClass(t *testing.T) {
	re := GlobToRegexp("[a]bc")
	if !re.MatchString("abc") {
		t.Error("single-char class [a] should match 'a'")
	}
	if re.MatchString("[a]bc") {
		t.Error("[a] should be a class, not a literal")
	}
}

// --- errors.go: unwrap-to-nil resolves to internal_error without panicking ---

type nilUnwrapErr struct{}

func (nilUnwrapErr) Error() string { return "nil-unwrap" }
func (nilUnwrapErr) Unwrap() error { return nil }

func TestEnvelopeForErrorUnwrappingToNil(t *testing.T) {
	env := EnvelopeFor(nilUnwrapErr{})
	if env.Code != string(ErrInternal) {
		t.Errorf("code = %q, want internal_error", env.Code)
	}
	if env.Message != "nil-unwrap" {
		t.Errorf("message = %q", env.Message)
	}
}
