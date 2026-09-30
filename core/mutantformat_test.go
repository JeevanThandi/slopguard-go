package core

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func strPtr(s string) *string { return &s }

func sampleMutants() []Mutant {
	method := strPtr("Store.Toggle")
	mutants := []Mutant{
		{File: "store.go", Line: 16, Column: 25, Operator: OpNegateConditional, Original: "==", Replacement: "!=", Method: method, Status: StatusKilled},
		{File: "store.go", Line: 18, Column: 3, Operator: OpRemoveCall, Original: "s.notify(\n\t\tx)", Replacement: "", Method: method, Status: StatusSurvived},
		{File: "store.go", Line: 20, Column: 7, Operator: OpBoundary, Original: "<", Replacement: "<=", Status: StatusSurvived},
		{File: "filter.go", Line: 12, Column: 3, Operator: OpRemoveCall, Original: "apply(x)", Replacement: "", Method: strPtr("applyFilter"), Status: StatusNoCoverage},
		{File: "loop.go", Line: 4, Column: 9, Operator: OpIncrement, Original: "++", Replacement: "--", Method: strPtr("spin"), Status: StatusTimeout},
		{File: "loop.go", Line: 5, Column: 2, Operator: OpArithmetic, Original: "+", Replacement: "-", Status: StatusCompileError},
		{File: "loop.go", Line: 6, Column: 2, Operator: OpBoundary, Original: ">", Replacement: ">=", Status: StatusIgnored},
	}
	for i := range mutants {
		m := &mutants[i]
		m.ID = MutantID(m.File, m.Line, m.Column, m.Operator)
	}
	return mutants
}

func sampleMutationReport() MutationReport {
	root, runner, timeout := "/abs/project", "go test", 34.0
	return NewMutationReport(MutationReportArgs{
		CoverageAvailable: true,
		FileCount:         3,
		GeneratedAt:       time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC),
		Mutants:           sampleMutants(),
		Notes:             []string{"1 mutant(s) did not compile and are excluded from the score."},
		Operators:         MutationOperators,
		ProjectRoot:       &root,
		Runner:            &runner,
		SourceRoot:        "/abs/project/src",
		TimeoutSeconds:    &timeout,
	})
}

func TestPrettyMutationReportLayout(t *testing.T) {
	want := `slopguard-go 0.2.0 — mutation report (schema 1)
source:    /abs/project/src
project:   /abs/project
runner:    go test
timeout:   34s per mutant

Notes
  • 1 mutant(s) did not compile and are excluded from the score.

Summary
  files:          3
  mutants:        7
  killed:         1
  timed out:      1
  survived:       2
  no coverage:    1
  compile errors: 1
  ignored:        1
  score:          40.00%

Survived (2) — tests still pass with these changes
  store.go:18:3  remove_call  ` + "`s.notify( x)` → ``" + `  Store.Toggle
  store.go:20:7  boundary  ` + "`<` → `<=`" + `

No coverage (1) — no test runs these lines
  filter.go:12:3  remove_call  ` + "`apply(x)` → ``" + `  applyFilter

Timed out (1) — counted as killed
  loop.go:4:9  increment  ` + "`++` → `--`" + `  spin
`
	if got := PrettyMutationReport(sampleMutationReport()); got != want {
		t.Errorf("text report mismatch.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestPrettyMutationReportDryRun(t *testing.T) {
	report := NewMutationReport(MutationReportArgs{
		FileCount:  1,
		Mutants:    []Mutant{{File: "a.go", Line: 1, Column: 2, Operator: OpLogical, Original: "&&", Replacement: "||", Status: StatusPending}},
		SourceRoot: "/src",
	})
	got := PrettyMutationReport(report)
	for _, want := range []string{
		"project:   (not run)\n", "runner:    (not run)\n", "timeout:   (not run)\n",
		"  pending:        1\n  score:          n/a\n",
		"\nMutants (1, not run)\n  a.go:1:2  logical  `&&` → `||`\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("dry-run report missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Notes") {
		t.Error("the Notes block is omitted when there are no notes")
	}
}

func TestPrettyMutationReportOmitsPendingRowAndEmptySections(t *testing.T) {
	report := NewMutationReport(MutationReportArgs{Mutants: []Mutant{{Status: StatusKilled}}})
	got := PrettyMutationReport(report)
	for _, absent := range []string{"pending:", "Survived", "No coverage", "Timed out", "Mutants ("} {
		if strings.Contains(got, absent) {
			t.Errorf("report should not contain %q:\n%s", absent, got)
		}
	}
	if !strings.HasSuffix(got, "  score:          100.00%\n") {
		t.Errorf("report should end with the score row:\n%s", got)
	}
}

func TestPrettyMutationReportFractionalTimeout(t *testing.T) {
	timeout := 2.5
	got := PrettyMutationReport(NewMutationReport(MutationReportArgs{TimeoutSeconds: &timeout}))
	if !strings.Contains(got, "timeout:   2.5s per mutant\n") {
		t.Errorf("timeout line wrong:\n%s", got)
	}
}

func TestMutantSnippet(t *testing.T) {
	cases := map[string]string{
		"":                            "",
		"a <= b":                      "a <= b",
		"f(\n\t\ta,\n\t\tb)":          "f( a, b)",
		"  x  ":                       " x ",
		strings.Repeat("a", 40):       strings.Repeat("a", 40),
		strings.Repeat("a", 41):       strings.Repeat("a", 39) + "…",
		strings.Repeat("é", 45):       strings.Repeat("é", 39) + "…",
		"x" + strings.Repeat(" ", 50): "x ",
	}
	for in, want := range cases {
		if got := mutantSnippet(in); got != want {
			t.Errorf("mutantSnippet(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatSeconds(t *testing.T) {
	for in, want := range map[float64]string{34: "34", 2.5: "2.5", 0.25: "0.25", 1e6: "1000000"} {
		if got := FormatSeconds(in); got != want {
			t.Errorf("FormatSeconds(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestJSONMutationReportShape(t *testing.T) {
	out, err := JSONMutationReport(sampleMutationReport())
	if err != nil {
		t.Fatal(err)
	}
	// Operators stay readable: no HTML escaping of <, >, &.
	if !strings.Contains(out, `"original": "<"`) || strings.Contains(out, `\u003c`) {
		t.Errorf("JSON should not escape operators:\n%s", out)
	}
	if strings.HasSuffix(out, "\n") {
		t.Error("JSON has no trailing newline; the CLI adds one")
	}
	// Keys are alphabetical at every level.
	assertKeyOrder(t, out, `"coverageAvailable"`, `"generatedAt"`, `"mutants"`, `"notes"`, `"operators"`,
		`"projectRoot"`, `"reportType"`, `"runner"`, `"schemaVersion"`, `"sourceRoot"`, `"summary"`,
		`"timeoutSeconds"`, `"tool"`, `"toolVersion"`)
	firstMutant := out[strings.Index(out, `"mutants"`):strings.Index(out, `"notes"`)]
	assertKeyOrder(t, firstMutant, `"column"`, `"file"`, `"id"`, `"line"`, `"method"`, `"operator"`,
		`"original"`, `"replacement"`, `"status"`)
	summary := out[strings.Index(out, `"summary"`):strings.Index(out, `"timeoutSeconds"`)]
	assertKeyOrder(t, summary, `"compileErrors"`, `"fileCount"`, `"ignored"`, `"killed"`, `"mutantCount"`,
		`"mutationScore"`, `"noCoverage"`, `"pending"`, `"survived"`, `"timedOut"`)

	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decoded["reportType"] != "mutation" || decoded["schemaVersion"] != "1" || decoded["runner"] != "go test" {
		t.Errorf("metadata wrong: %v", decoded)
	}
	// Whole floats encode without ".0".
	if !strings.Contains(out, `"timeoutSeconds": 34,`) || !strings.Contains(out, `"mutationScore": 40,`) {
		t.Errorf("whole floats should encode as integers:\n%s", out)
	}
	mutants := decoded["mutants"].([]any)
	if first := mutants[0].(map[string]any); first["id"] != "store.go:16:25:negate_conditional" || first["status"] != "killed" {
		t.Errorf("first mutant = %v", first)
	}
	if mutants[2].(map[string]any)["method"] != nil {
		t.Error("a mutant outside any method encodes method as null")
	}
}

func TestJSONMutationReportNulls(t *testing.T) {
	out, err := JSONMutationReport(NewMutationReport(MutationReportArgs{SourceRoot: "/src"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"projectRoot": null`, `"runner": null`, `"timeoutSeconds": null`,
		`"mutationScore": null`, `"mutants": []`, `"notes": []`, `"coverageAvailable": false`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSON missing %s:\n%s", want, out)
		}
	}
}

func TestJSONMutationReportNaNErrors(t *testing.T) {
	nan := math.NaN()
	if _, err := JSONMutationReport(MutationReport{TimeoutSeconds: &nan}); err == nil {
		t.Error("encoding/json cannot encode NaN; expected an error")
	}
}

func assertKeyOrder(t *testing.T, text string, keys ...string) {
	t.Helper()
	last := -1
	for _, key := range keys {
		at := strings.Index(text, key)
		if at < 0 {
			t.Errorf("key %s missing", key)
			continue
		}
		if at < last {
			t.Errorf("key %s is out of alphabetical order", key)
		}
		last = at
	}
}
