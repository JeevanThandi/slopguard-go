package core

import (
	"go/ast"
	"math"
	"strings"
	"testing"
)

func TestReceiverTypeNameDirect(t *testing.T) {
	// Valid Go receivers never use these shapes, but the unwrapper handles them
	// defensively. Selector (qualified), index-list (multi-type-param), and an
	// unsupported expr returning "".
	sel := &ast.SelectorExpr{X: ast.NewIdent("pkg"), Sel: ast.NewIdent("T")}
	if got := receiverTypeName(sel); got != "T" {
		t.Errorf("selector receiver = %q, want T", got)
	}
	idxList := &ast.IndexListExpr{X: ast.NewIdent("Pair"), Indices: []ast.Expr{ast.NewIdent("A"), ast.NewIdent("B")}}
	if got := receiverTypeName(idxList); got != "Pair" {
		t.Errorf("index-list receiver = %q, want Pair", got)
	}
	if got := receiverTypeName(&ast.ArrayType{}); got != "" {
		t.Errorf("unsupported receiver = %q, want empty", got)
	}
}

func TestBumpAndNestingGuards(t *testing.T) {
	a := &analyzer{}
	// No current method: these must be no-ops, not panics.
	a.bumpCyclomatic()
	a.bumpCognitive(5)
	if a.nesting() != 0 {
		t.Error("nesting with no method should be 0")
	}
	// amount <= 0 is ignored even with a method present.
	a.methodStack = append(a.methodStack, &methodFrame{})
	a.bumpCognitive(0)
	a.bumpCognitive(-3)
	if a.curMethod().cognitive != 0 {
		t.Errorf("non-positive cognitive bump should be ignored, got %d", a.curMethod().cognitive)
	}
}

func TestPopEmptyStackIsSafe(t *testing.T) {
	a := &analyzer{}
	a.pop() // must not panic on an empty stack
}

func TestJSONReportNaNErrors(t *testing.T) {
	// encoding/json cannot marshal NaN — exercises JSONReport's error path.
	report := CrapReport{
		Methods: []MethodCrap{{Crap: math.NaN()}},
	}
	if _, err := JSONReport(report); err == nil {
		t.Error("expected JSONReport to error on NaN")
	}
}

func TestTopMethodsTruncatesToTopN(t *testing.T) {
	report := CrapReport{
		ToolVersion: Version, SchemaVersion: "2", Threshold: 30,
		Summary: ReportSummary{CrappyMethodCount: 0},
		Methods: []MethodCrap{
			{Name: "a", QualifiedName: "a", File: "x.go", Line: 1, Crap: 9},
			{Name: "b", QualifiedName: "b", File: "x.go", Line: 2, Crap: 5},
			{Name: "c", QualifiedName: "c", File: "x.go", Line: 3, Crap: 1},
		},
	}
	out := topMethods(report, 1)
	if !strings.Contains(out, "ranked hotspots") {
		t.Error("expected non-crappy header")
	}
	if strings.Contains(out, "x.go:2") || strings.Contains(out, "x.go:3") {
		t.Error("topN=1 should only show the worst method")
	}
}

func TestGlobNegationClass(t *testing.T) {
	re := GlobToRegexp("file[!0-9].go")
	if !re.MatchString("filex.go") {
		t.Error("[!0-9] should match a non-digit")
	}
	if re.MatchString("file5.go") {
		t.Error("[!0-9] should not match a digit")
	}
}
