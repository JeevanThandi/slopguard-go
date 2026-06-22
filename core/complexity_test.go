package core

import "testing"

// analyzeOne parses src and returns the method with the given qualified name.
func analyzeOne(t *testing.T, src, qualified string) MethodMetric {
	t.Helper()
	report, err := AnalyzeSource([]byte(src), "test.go")
	if err != nil {
		t.Fatalf("AnalyzeSource error: %v", err)
	}
	for _, m := range report.Methods {
		if m.QualifiedName == qualified {
			return m
		}
	}
	t.Fatalf("method %q not found; got %d methods", qualified, len(report.Methods))
	return MethodMetric{}
}

func TestSimpleFunctionIsBaseline(t *testing.T) {
	m := analyzeOne(t, `package p
func Add(a, b int) int { return a + b }`, "Add")
	if m.Complexity != 1 || m.CognitiveComplexity != 0 {
		t.Errorf("baseline: cyc=%d cog=%d, want 1/0", m.Complexity, m.CognitiveComplexity)
	}
	if m.Kind != KindFunction {
		t.Errorf("kind = %q, want function", m.Kind)
	}
}

func TestIfElseChain(t *testing.T) {
	// head if (+1cyc, +1cog), else-if (+1cyc, +1cog flat), else (+1cog flat).
	m := analyzeOne(t, `package p
func grade(n int) string {
	if n > 90 {
		return "a"
	} else if n > 80 {
		return "b"
	} else {
		return "c"
	}
}`, "grade")
	if m.Complexity != 3 {
		t.Errorf("cyc = %d, want 3 (head if + else if)", m.Complexity)
	}
	if m.CognitiveComplexity != 3 {
		t.Errorf("cog = %d, want 3 (if 1 + elseif 1 + else 1)", m.CognitiveComplexity)
	}
}

func TestNestingAmplifiesCognitive(t *testing.T) {
	// Outer if at nesting 0 (+1), inner if at nesting 1 (+2) => cog 3, cyc 2.
	m := analyzeOne(t, `package p
func f(a, b bool) {
	if a {
		if b {
			println("x")
		}
	}
}`, "f")
	if m.Complexity != 3 {
		t.Errorf("cyc = %d, want 3 (base + 2 ifs)", m.Complexity)
	}
	if m.CognitiveComplexity != 3 {
		t.Errorf("cog = %d, want 3 (outer 1 + inner 2)", m.CognitiveComplexity)
	}
}

func TestFlatSwitchIsOneCognitiveIncrement(t *testing.T) {
	// 4 non-default cases => cyc 5 (1 base + 4 cases); switch => cog 1 only.
	m := analyzeOne(t, `package p
func classify(n int) string {
	switch n {
	case 1:
		return "a"
	case 2:
		return "b"
	case 3:
		return "c"
	case 4:
		return "d"
	default:
		return "?"
	}
}`, "classify")
	if m.Complexity != 5 {
		t.Errorf("cyc = %d, want 5 (base + 4 cases)", m.Complexity)
	}
	if m.CognitiveComplexity != 1 {
		t.Errorf("cog = %d, want 1 (whole switch once)", m.CognitiveComplexity)
	}
}

func TestLoopsAndRange(t *testing.T) {
	m := analyzeOne(t, `package p
func sum(xs []int) int {
	total := 0
	for i := 0; i < len(xs); i++ {
		total += xs[i]
	}
	for _, x := range xs {
		total += x
	}
	return total
}`, "sum")
	if m.Complexity != 3 {
		t.Errorf("cyc = %d, want 3 (base + for + range)", m.Complexity)
	}
	if m.CognitiveComplexity != 2 {
		t.Errorf("cog = %d, want 2", m.CognitiveComplexity)
	}
}

func TestBooleanRunCollapse(t *testing.T) {
	// a && b && c is one cognitive run but three cyclomatic decisions... wait:
	// && appears twice => cyc +2; one run => cog +1. Plus the if: cyc +1, cog +1.
	m := analyzeOne(t, `package p
func f(a, b, c bool) {
	if a && b && c {
		println("x")
	}
}`, "f")
	if m.Complexity != 4 {
		t.Errorf("cyc = %d, want 4 (base + if + 2 &&)", m.Complexity)
	}
	if m.CognitiveComplexity != 2 {
		t.Errorf("cog = %d, want 2 (if 1 + one && run 1)", m.CognitiveComplexity)
	}
}

func TestMixedBooleanRunsCountTransitions(t *testing.T) {
	// a && b || c: && run and || run => two cognitive runs; cyc += 2.
	m := analyzeOne(t, `package p
func f(a, b, c bool) bool {
	return a && b || c
}`, "f")
	if m.Complexity != 3 {
		t.Errorf("cyc = %d, want 3 (base + && + ||)", m.Complexity)
	}
	if m.CognitiveComplexity != 2 {
		t.Errorf("cog = %d, want 2 (&& run + || run)", m.CognitiveComplexity)
	}
}

func TestClosureBumpsNestingNoEntry(t *testing.T) {
	// The closure body's if is at nesting 1 => cog 2; the closure gets no entry.
	report, err := AnalyzeSource([]byte(`package p
func outer(xs []int) {
	run(func() {
		if len(xs) > 0 {
			println("x")
		}
	})
}`), "test.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Methods) != 1 {
		t.Fatalf("got %d methods, want 1 (closure must not get its own entry)", len(report.Methods))
	}
	m := report.Methods[0]
	if m.CognitiveComplexity != 2 {
		t.Errorf("cog = %d, want 2 (if inside closure at nesting 1)", m.CognitiveComplexity)
	}
}

func TestLabeledBreakIsFundamental(t *testing.T) {
	m := analyzeOne(t, `package p
func f(grid [][]int) {
outer:
	for _, row := range grid {
		for _, v := range row {
			if v < 0 {
				break outer
			}
		}
	}
}`, "f")
	// for(+1) + for(+2) + if(+3) + labeled break(+1) = 7 cognitive.
	if m.CognitiveComplexity != 7 {
		t.Errorf("cog = %d, want 7", m.CognitiveComplexity)
	}
}

func TestMethodReceiverAndQualifiedName(t *testing.T) {
	report, err := AnalyzeSource([]byte(`package p
type Parser struct{}
func (p *Parser) Parse() error { return nil }
func (p Parser) reset() {}`), "test.go")
	if err != nil {
		t.Fatal(err)
	}
	var sawParse, sawReset bool
	for _, m := range report.Methods {
		switch m.QualifiedName {
		case "Parser.Parse":
			sawParse = true
			if m.Kind != KindMethod || m.TypeName != "Parser" {
				t.Errorf("Parse: kind=%q typeName=%q", m.Kind, m.TypeName)
			}
		case "Parser.reset":
			sawReset = true
		}
	}
	if !sawParse || !sawReset {
		t.Errorf("missing methods: Parse=%v reset=%v", sawParse, sawReset)
	}
	// One struct type declaration recorded.
	if len(report.Types) != 1 || report.Types[0].Kind != KindStruct {
		t.Errorf("types = %+v, want one struct", report.Types)
	}
}

func TestGeneratedFileSkipped(t *testing.T) {
	report, err := AnalyzeSource([]byte(`// Code generated by protoc. DO NOT EDIT.
package p
func huge(a, b bool) { if a && b { println("x") } }`), "test.pb.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Methods) != 0 {
		t.Errorf("generated file should yield no methods, got %d", len(report.Methods))
	}
}

func TestTypeSwitchAndSelect(t *testing.T) {
	m := analyzeOne(t, `package p
func f(x any, ch chan int) {
	switch x.(type) {
	case int:
		println("int")
	case string:
		println("string")
	}
	select {
	case <-ch:
		println("recv")
	default:
		println("none")
	}
}`, "f")
	// type switch: cog +1, cyc +2 (two cases). select: cog +1, cyc +1 (one comm case).
	if m.Complexity != 4 {
		t.Errorf("cyc = %d, want 4", m.Complexity)
	}
	if m.CognitiveComplexity != 2 {
		t.Errorf("cog = %d, want 2", m.CognitiveComplexity)
	}
}
