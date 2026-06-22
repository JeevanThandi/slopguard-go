package core

import (
	"encoding/json"
	"strings"
	"testing"
)

func sampleReport() CrapReport {
	reports := []FileReport{{
		Path: "x.go",
		Methods: []MethodMetric{
			MakeMethodMetric(MethodMetric{Name: "big", QualifiedName: "big", Kind: KindFunction, File: "x.go", StartLine: 3, EndLine: 20, Complexity: 12, CognitiveComplexity: 10}),
		},
	}}
	return Aggregate(AggregateArgs{FileReports: reports, SourceRoot: "/r", Coverage: fakeCoverage{0}})
}

func TestPrettyReportContainsSummaryAndMarker(t *testing.T) {
	out := PrettyReport(sampleReport(), 20)
	for _, want := range []string{"slopguard-go", "schema 2", "Summary", "Top methods by wCRAP", "x.go:3", "big"} {
		if !strings.Contains(out, want) {
			t.Errorf("pretty output missing %q\n%s", want, out)
		}
	}
	if !strings.Contains(out, "!") {
		t.Error("a crappy method should be marked with !")
	}
}

func TestJSONReportIsValidAndStable(t *testing.T) {
	out, err := JSONReport(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decoded["schemaVersion"] != "2" || decoded["tool"] != "slopguard-go" {
		t.Errorf("schema/tool wrong: %v / %v", decoded["schemaVersion"], decoded["tool"])
	}
	methods, ok := decoded["methods"].([]any)
	if !ok || len(methods) != 1 {
		t.Fatalf("methods array wrong: %v", decoded["methods"])
	}
	m := methods[0].(map[string]any)
	for _, key := range []string{"id", "crap", "complexity", "cognitiveComplexity", "weightedComplexity", "coverage", "isCrappy"} {
		if _, present := m[key]; !present {
			t.Errorf("method JSON missing key %q", key)
		}
	}
}

func TestErrorRendering(t *testing.T) {
	env := EnvelopeFor(FileNotFound("/tmp/x"))
	if env.Code != "file_not_found" {
		t.Errorf("code = %q", env.Code)
	}
	if !strings.Contains(ErrorTextLine(env), "[file_not_found]") {
		t.Error("text line should embed the code")
	}
	if !strings.Contains(ErrorJSON(env), `"file_not_found"`) {
		t.Error("error JSON should embed the code")
	}
}
