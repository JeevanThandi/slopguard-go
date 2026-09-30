package core

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseOperatorsDefaultsToEveryOperator(t *testing.T) {
	for _, values := range [][]string{nil, {}, {""}, {" , ,"}} {
		got, err := ParseOperators(values)
		if err != nil {
			t.Fatalf("ParseOperators(%q) error: %v", values, err)
		}
		if !reflect.DeepEqual(got, MutationOperators) {
			t.Errorf("ParseOperators(%q) = %v, want every operator", values, got)
		}
	}
}

func TestParseOperatorsAccumulatesSortsAndDeduplicates(t *testing.T) {
	got, err := ParseOperators([]string{"logical, boundary", " arithmetic ", "boundary"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{OpArithmetic, OpBoundary, OpLogical}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseOperators = %v, want %v", got, want)
	}
}

func TestParseOperatorsRejectsUnknownIDs(t *testing.T) {
	_, err := ParseOperators([]string{"bogus,boundary", "nope"})
	var se *SlopguardError
	if !asSlopguardError(err, &se) || se.Code != ErrInvalidArgument {
		t.Fatalf("expected invalid_argument, got %v", err)
	}
	for _, want := range []string{"--operators", "unknown operator(s): bogus, nope", "expected: arithmetic, boolean_literal"} {
		if !strings.Contains(se.Message, want) {
			t.Errorf("message %q should contain %q", se.Message, want)
		}
	}
}

func TestMutationOperatorsAreSortedAndKnown(t *testing.T) {
	if len(MutationOperators) != 9 {
		t.Fatalf("expected 9 operators, got %d", len(MutationOperators))
	}
	for i, op := range MutationOperators {
		if !IsMutationOperator(op) {
			t.Errorf("%q should be a known operator", op)
		}
		if i > 0 && MutationOperators[i-1] >= op {
			t.Errorf("operators not sorted at %q", op)
		}
	}
	if IsMutationOperator("Boundary") || IsMutationOperator("") {
		t.Error("ids are exact, lower-case snake_case")
	}
}

func withStatuses(statuses ...MutantStatus) []Mutant {
	mutants := make([]Mutant, len(statuses))
	for i, s := range statuses {
		mutants[i] = Mutant{Status: s}
	}
	return mutants
}

func TestSummarizeMutantsCountsEveryStatusAndScores(t *testing.T) {
	mutants := withStatuses(
		StatusKilled, StatusKilled, StatusKilled, StatusTimeout,
		StatusSurvived, StatusSurvived, StatusNoCoverage, StatusNoCoverage,
		StatusCompileError, StatusIgnored, StatusPending,
	)
	s := SummarizeMutants(mutants, 4)
	want := MutationSummary{
		CompileErrors: 1, FileCount: 4, Ignored: 1, Killed: 3, MutantCount: 11,
		NoCoverage: 2, Pending: 1, Survived: 2, TimedOut: 1,
	}
	got := s
	got.MutationScore = nil
	if got != want {
		t.Errorf("summary = %+v, want %+v", got, want)
	}
	sum := s.Killed + s.TimedOut + s.Survived + s.NoCoverage + s.CompileErrors + s.Ignored + s.Pending
	if sum != s.MutantCount {
		t.Errorf("counts sum to %d, want mutantCount %d", sum, s.MutantCount)
	}
	// (3 killed + 1 timeout) / (4 + 2 survived + 2 no_coverage) = 50%.
	if s.MutationScore == nil || *s.MutationScore != 50 {
		t.Errorf("score = %v, want 50", s.MutationScore)
	}
}

func TestMutationScoreIsUnrounded(t *testing.T) {
	// The spec's order of operations, (detected / scored) × 100, like the
	// TypeScript reference: 33.33333333333333, not 100/3.
	s := SummarizeMutants(withStatuses(StatusKilled, StatusSurvived, StatusSurvived), 1)
	want := float64(1) / float64(3) * 100
	if s.MutationScore == nil || *s.MutationScore != want {
		t.Fatalf("score = %v, want %v", s.MutationScore, want)
	}
}

func TestMutationScoreIsNilWithoutScoredMutants(t *testing.T) {
	for _, mutants := range [][]Mutant{nil, withStatuses(StatusIgnored, StatusCompileError, StatusPending)} {
		if s := SummarizeMutants(mutants, 0); s.MutationScore != nil {
			t.Errorf("score = %v, want nil", *s.MutationScore)
		}
	}
}

func TestMutationResultNotes(t *testing.T) {
	cases := []struct {
		name  string
		s     MutationSummary
		notes []string
	}{
		{"all survived", MutationSummary{Survived: 2}, []string{NoteEverySurvived}},
		{"one timeout", MutationSummary{Survived: 2, TimedOut: 1}, nil},
		{"one killed", MutationSummary{Survived: 2, Killed: 1}, nil},
		{"nothing ran", MutationSummary{NoCoverage: 3}, nil},
		{"compile errors", MutationSummary{Killed: 1, CompileErrors: 2},
			[]string{"2 mutant(s) did not compile and are excluded from the score."}},
		{"both", MutationSummary{Survived: 1, CompileErrors: 1},
			[]string{NoteEverySurvived, "1 mutant(s) did not compile and are excluded from the score."}},
	}
	for _, c := range cases {
		if got := MutationResultNotes(c.s); !reflect.DeepEqual(got, c.notes) {
			t.Errorf("%s: notes = %q, want %q", c.name, got, c.notes)
		}
	}
}

func TestStandardNoteWording(t *testing.T) {
	if NoteEverySurvived != "Every tested mutant survived. Check that the tests import the source under --path." {
		t.Errorf("NoteEverySurvived wording changed: %q", NoteEverySurvived)
	}
	if NoteNoCoverageData != "The baseline test run produced no coverage data, so every mutant was run." {
		t.Errorf("NoteNoCoverageData wording changed: %q", NoteNoCoverageData)
	}
	if got := CoverageRunExitNote(1); got != "The coverage run exited with code 1; its coverage data was still used." {
		t.Errorf("CoverageRunExitNote = %q", got)
	}
}

func TestNewMutationReportStampsMetadata(t *testing.T) {
	at := time.Date(2026, 9, 30, 12, 0, 0, 7_000_000, time.FixedZone("x", 3600))
	report := NewMutationReport(MutationReportArgs{
		FileCount:   2,
		GeneratedAt: at,
		Mutants:     withStatuses(StatusKilled),
		Operators:   []string{OpBoundary},
		SourceRoot:  "/src",
	})
	if report.GeneratedAt != "2026-09-30T11:00:00.007Z" {
		t.Errorf("generatedAt = %q", report.GeneratedAt)
	}
	if report.SchemaVersion != "1" || report.ReportType != "mutation" {
		t.Errorf("schema/reportType = %q/%q", report.SchemaVersion, report.ReportType)
	}
	if report.Tool != ToolName || report.ToolVersion != Version {
		t.Errorf("tool = %q %q", report.Tool, report.ToolVersion)
	}
	if report.Summary.FileCount != 2 || report.Summary.Killed != 1 {
		t.Errorf("summary = %+v", report.Summary)
	}
	if report.Notes == nil || len(report.Notes) != 0 {
		t.Errorf("notes = %#v, want an empty non-nil slice", report.Notes)
	}
	if report.ProjectRoot != nil || report.Runner != nil || report.TimeoutSeconds != nil {
		t.Error("run fields should stay nil when no test ran")
	}
}

func TestNewMutationReportDefaults(t *testing.T) {
	before := time.Now().Add(-time.Second)
	report := NewMutationReport(MutationReportArgs{})
	if report.Mutants == nil || report.Operators == nil || report.Notes == nil {
		t.Error("slices must encode as [] rather than null")
	}
	stamped, err := time.Parse("2006-01-02T15:04:05.000Z", report.GeneratedAt)
	if err != nil || stamped.Before(before.UTC().Truncate(time.Millisecond)) {
		t.Errorf("zero GeneratedAt should stamp now, got %q (%v)", report.GeneratedAt, err)
	}
}

func TestMutantIDAndApply(t *testing.T) {
	if got := MutantID("pkg/x.go", 3, 14, OpBoundary); got != "pkg/x.go:3:14:boundary" {
		t.Errorf("MutantID = %q", got)
	}
	src := []byte("a < b")
	m := Mutant{Original: "<", Replacement: "<=", start: 2, end: 3}
	if got := string(m.Apply(src)); got != "a <= b" {
		t.Errorf("Apply = %q", got)
	}
	if string(src) != "a < b" {
		t.Error("Apply must not modify the source it was given")
	}
	removed := Mutant{Original: "f()", Replacement: "", start: 0, end: 3}
	if got := string(removed.Apply([]byte("f()\n"))); got != "\n" {
		t.Errorf("Apply(remove) = %q", got)
	}
}

func TestSortMutantsOrder(t *testing.T) {
	mutants := []Mutant{
		{File: "a/b.go", Line: 1, Column: 1, Operator: OpLogical},
		{File: "a.go", Line: 2, Column: 1, Operator: OpLogical},
		{File: "a.go", Line: 1, Column: 5, Operator: OpNegateConditional},
		{File: "a.go", Line: 1, Column: 5, Operator: OpBoundary},
		{File: "a.go", Line: 1, Column: 2, Operator: OpRemoveNot},
	}
	SortMutants(mutants)
	var got []string
	for _, m := range mutants {
		got = append(got, MutantID(m.File, m.Line, m.Column, m.Operator))
	}
	// Byte-wise file order puts "a.go" before "a/b.go" ('.' < '/').
	want := []string{
		"a.go:1:2:remove_not", "a.go:1:5:boundary", "a.go:1:5:negate_conditional",
		"a.go:2:1:logical", "a/b.go:1:1:logical",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}
