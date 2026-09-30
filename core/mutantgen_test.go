package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// genMutants generates the mutants of src (reported as x.go) and fails the
// test on error.
func genMutants(t *testing.T, src string) []Mutant {
	t.Helper()
	mutants, err := GenerateMutants([]byte(src), "x.go")
	if err != nil {
		t.Fatalf("GenerateMutants: %v", err)
	}
	return mutants
}

// summaries renders mutants as "line:column operator original→replacement"
// for compact comparisons.
func summaries(mutants []Mutant) []string {
	out := []string{}
	for _, m := range mutants {
		out = append(out, strings.Join([]string{
			itoa(m.Line) + ":" + itoa(m.Column), m.Operator, m.Original + "→" + m.Replacement,
		}, " "))
	}
	return out
}

func itoa(n int) string { return strconv.Itoa(n) }

func ofOperator(mutants []Mutant, operator string) []Mutant {
	var out []Mutant
	for _, m := range mutants {
		if m.Operator == operator {
			out = append(out, m)
		}
	}
	return out
}

func assertSummaries(t *testing.T, got []Mutant, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if s := summaries(got); !reflect.DeepEqual(s, want) {
		t.Errorf("mutants =\n  %s\nwant\n  %s", strings.Join(s, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestArithmeticBinaryOperators(t *testing.T) {
	src := "package x\n\nvar v = a + b - c*d/e%f\n"
	assertSummaries(t, ofOperator(genMutants(t, src), OpArithmetic),
		"3:11 arithmetic +→-",
		"3:15 arithmetic -→+",
		"3:18 arithmetic *→/",
		"3:20 arithmetic /→*",
		"3:22 arithmetic %→*",
	)
}

func TestArithmeticCompoundAssignments(t *testing.T) {
	src := "package x\n\nfunc f(n int) {\n\tn += 1\n\tn -= 1\n\tn *= 2\n\tn /= 2\n\tn %= 2\n}\n"
	assertSummaries(t, ofOperator(genMutants(t, src), OpArithmetic),
		"4:4 arithmetic +=→-=",
		"5:4 arithmetic -=→+=",
		"6:4 arithmetic *=→/=",
		"7:4 arithmetic /=→*=",
		"8:4 arithmetic %=→*=",
	)
}

func TestArithmeticSkipsStringConcatenation(t *testing.T) {
	src := `package x

func f(a, b, c string, n int) {
	_ = "a" + b
	_ = a + "b"
	_ = "a" + b + c
	_ = (a + "b") + c
	_ = ` + "`raw`" + ` + a
	a += "suffix"
	a += ("x" + b)
	_ = 'r' + n
	_ = a + b + "c"
}
`
	// Only 'r' + n (a rune is a number) and the inner a + b of a + b + "c"
	// remain: the stringy rule looks at operands, recursively, not parents.
	assertSummaries(t, ofOperator(genMutants(t, src), OpArithmetic),
		"11:10 arithmetic +→-",
		"12:8 arithmetic +→-",
	)
}

func TestArithmeticIgnoresOtherOperators(t *testing.T) {
	src := "package x\n\nvar v = a&b | c<<1 ^ d>>2 &^ e\n"
	if got := genMutants(t, src); len(got) != 0 {
		t.Errorf("bitwise operators are not mutated, got %v", summaries(got))
	}
}

func TestBooleanLiteralValues(t *testing.T) {
	src := `package x

var v = true

func f(m map[bool]int) bool {
	x := false
	m[true] = 1
	_ = map[bool]int{true: 1}
	g(true, x)
	return false
}
`
	assertSummaries(t, ofOperator(genMutants(t, src), OpBooleanLiteral),
		"3:9 boolean_literal true→false",
		"6:7 boolean_literal false→true",
		"7:4 boolean_literal true→false",
		"8:19 boolean_literal true→false",
		"9:4 boolean_literal true→false",
		"10:9 boolean_literal false→true",
	)
}

func TestBooleanLiteralSkipsNamesThatSpellTrueOrFalse(t *testing.T) {
	// In Go, true and false are identifiers, so they can be declared (legal
	// but perverse). Only value uses of the predeclared constants mutate.
	src := `package x

import true "fmt"

type false int

var true = 1

type T struct{ true bool }

func true(false int) {}

func f(t T, xs []int) {
	_ = t.true
	true, x := 1, 2
	for true, false := range xs {
	}
true:
	for {
		break true
	}
}
`
	if got := ofOperator(genMutants(t, src), OpBooleanLiteral); len(got) != 0 {
		t.Errorf("declared names must not mutate, got %v", summaries(got))
	}
}

func TestBoundaryAndNegateConditional(t *testing.T) {
	src := "package x\n\nvar v = a < b || a <= b || a > b || a >= b || a == b || a != b\n"
	assertSummaries(t, genMutants(t, src),
		"3:11 boundary <→<=",
		"3:11 negate_conditional <→>=",
		"3:15 logical ||→&&",
		"3:20 boundary <=→<",
		"3:20 negate_conditional <=→>",
		"3:25 logical ||→&&",
		"3:30 boundary >→>=",
		"3:30 negate_conditional >→<=",
		"3:34 logical ||→&&",
		"3:39 boundary >=→>",
		"3:39 negate_conditional >=→<",
		"3:44 logical ||→&&",
		"3:49 negate_conditional ==→!=",
		"3:54 logical ||→&&",
		"3:59 negate_conditional !=→==",
	)
}

func TestLogicalOperators(t *testing.T) {
	src := "package x\n\nvar v = a && b || c\n"
	assertSummaries(t, genMutants(t, src),
		"3:11 logical &&→||",
		"3:16 logical ||→&&",
	)
}

func TestIncrementStatements(t *testing.T) {
	src := "package x\n\nfunc f(i int) {\n\ti++\n\ti--\n}\n"
	assertSummaries(t, genMutants(t, src),
		"4:3 increment ++→--",
		"5:3 increment --→++",
	)
}

func TestInvertNegativeAndRemoveNot(t *testing.T) {
	src := "package x\n\nvar v = -a - -(b) + f(!c, !!d, a != b)\n"
	got := genMutants(t, src)
	assertSummaries(t, ofOperator(got, OpInvertNegative),
		"3:9 invert_negative -→",
		"3:14 invert_negative -→",
	)
	assertSummaries(t, ofOperator(got, OpRemoveNot),
		"3:23 remove_not !→",
		"3:27 remove_not !→",
		"3:28 remove_not !→",
	)
	// The binary minus is arithmetic, never invert_negative; != is not remove_not.
	assertSummaries(t, ofOperator(got, OpArithmetic), "3:12 arithmetic -→+", "3:19 arithmetic +→-")
}

func TestTokenRemovalKeepsIdentifiersApart(t *testing.T) {
	// Not gofmt'd on purpose: removing the token between `return` and the
	// operand must leave a space, or the mutant reads `returnx`.
	src := `package x

func f(x bool) bool { return!x }

func g(n int) int { return-n }

func h() int { return-1 }

func k(é bool) bool { return!é }

func m(y bool) (x bool) {
	x = !y
	return x - -1 > 0 && (!y)
}
`
	var got []string
	applied := map[int]string{}
	for _, mutant := range genMutants(t, src) {
		if mutant.Operator != OpRemoveNot && mutant.Operator != OpInvertNegative {
			continue
		}
		got = append(got, itoa(mutant.Line)+" "+mutant.Operator+" "+strconv.Quote(mutant.Replacement))
		lines := strings.Split(string(mutant.Apply([]byte(src))), "\n")
		applied[mutant.Line] = lines[mutant.Line-1]
	}
	want := []string{
		`3 remove_not " "`,
		`5 invert_negative " "`,
		`7 invert_negative " "`,
		`9 remove_not " "`,
		// Only one neighbour is an identifier character: plain removal.
		`12 remove_not ""`,
		`13 invert_negative ""`,
		`13 remove_not ""`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("replacements =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
	for line, text := range map[int]string{
		3:  "func f(x bool) bool { return x }",
		5:  "func g(n int) int { return n }",
		7:  "func h() int { return 1 }",
		9:  "func k(é bool) bool { return é }",
		12: "\tx = y",
	} {
		if applied[line] != text {
			t.Errorf("line %d after removal = %q, want %q", line, applied[line], text)
		}
	}
}

func TestIsIdentifierByte(t *testing.T) {
	for _, b := range []byte("azAZ09_\xc3\xa9") {
		if !isIdentifierByte(b) {
			t.Errorf("isIdentifierByte(%q) = false", b)
		}
	}
	for _, b := range []byte(" \t\n(){}[]!-+*/=<>&|,;:.\"'`") {
		if isIdentifierByte(b) {
			t.Errorf("isIdentifierByte(%q) = true", b)
		}
	}
}

func TestRemoveCallStatements(t *testing.T) {
	src := `package x

func f(ch chan int) {
	g()
	(g())
	switch {
	case true:
		g()
	}
	select {
	case <-ch:
		g()
	}
	func() {
		g()
	}()
	h(1,
		2)
}
`
	assertSummaries(t, ofOperator(genMutants(t, src), OpRemoveCall),
		"4:2 remove_call g()→",
		"5:2 remove_call (g())→",
		"8:3 remove_call g()→",
		"12:3 remove_call g()→",
		"14:2 remove_call func() {\n\t\tg()\n\t}()→",
		"15:3 remove_call g()→",
		"17:2 remove_call h(1,\n\t\t2)→",
	)
}

func TestRemoveCallSkipsLoggingAndNonListStatements(t *testing.T) {
	src := `package x

func f(t *testing.T, b *testing.B, logger Logger) {
	fmt.Println("x")
	fmt.Printf("x")
	fmt.Print("x")
	log.Printf("x")
	log.Default.Println("x")
	t.Log("x")
	t.Logf("x")
	b.Log("x")
	defer g()
	go g()
	if g(); ok {
	}
	for g(); ok; g() {
	}
L:
	g()
	fmt.Fprintf(w, "x")
	logger.Printf("x")
	t.Errorf("x")
	fmt.Sprint("x")
	s.log.Printf("x")
	newClient().Close()
}
`
	// The last six calls change observable output or state, so they stay.
	// Only a chain rooted at the identifier log counts as logging.
	assertSummaries(t, ofOperator(genMutants(t, src), OpRemoveCall),
		"20:2 remove_call fmt.Fprintf(w, \"x\")→",
		"21:2 remove_call logger.Printf(\"x\")→",
		"22:2 remove_call t.Errorf(\"x\")→",
		"23:2 remove_call fmt.Sprint(\"x\")→",
		"24:2 remove_call s.log.Printf(\"x\")→",
		"25:2 remove_call newClient().Close()→",
	)
}

func TestColumnsCountCodePoints(t *testing.T) {
	// "日本語" is 9 bytes but 3 code points; é is 2 bytes but 1 code point.
	src := "package x\n\nfunc f(a, b int) {\n\t_, _ = \"日本語\", a < b // é\n\t_ = \"é\"; _ = a + b\n}\n"
	got := genMutants(t, src)
	assertSummaries(t, got,
		"4:18 boundary <→<=",
		"4:18 negate_conditional <→>=",
		"5:17 arithmetic +→-",
	)
	if got[0].ID != "x.go:4:18:boundary" {
		t.Errorf("id = %q, want x.go:4:18:boundary", got[0].ID)
	}
}

func TestPositionsIgnoreLineDirectivesAndCRLF(t *testing.T) {
	src := "package x\r\n\r\n//line other.go:100\r\nfunc f(a, b int) bool {\r\n\treturn a < b\r\n}\r\n"
	got := genMutants(t, src)
	if len(got) != 2 || got[0].Line != 5 || got[0].Column != 11 || got[0].Original != "<" {
		t.Fatalf("mutants = %v, want the physical position 5:11", summaries(got))
	}
}

func TestMutantsCarryFileIDMethodAndStatus(t *testing.T) {
	src := `package x

var top = a + b

type T struct{}

func (t *T) M(a, b int) bool {
	f := func() bool { return a < b }
	return f()
}

func Free(n int) { n++ }
`
	mutants, err := GenerateMutants([]byte(src), "pkg/x.go")
	if err != nil {
		t.Fatal(err)
	}
	methods := map[string]string{}
	for _, m := range mutants {
		if m.File != "pkg/x.go" || !strings.HasPrefix(m.ID, "pkg/x.go:") || m.Status != StatusPending {
			t.Errorf("mutant %+v: wrong file, id or status", m)
		}
		name := "<nil>"
		if m.Method != nil {
			name = *m.Method
		}
		methods[m.ID] = name
	}
	// Package-level code has no method; a closure belongs to its enclosing
	// method; `return f()` is not a call statement, so it has no mutant.
	want := map[string]string{
		"pkg/x.go:3:13:arithmetic":         "<nil>",
		"pkg/x.go:8:30:boundary":           "T.M",
		"pkg/x.go:8:30:negate_conditional": "T.M",
		"pkg/x.go:12:21:increment":         "Free",
	}
	if !reflect.DeepEqual(methods, want) {
		t.Errorf("methods = %v, want %v", methods, want)
	}
}

func TestEnclosingMethodPrefersInnermostThenLaterStart(t *testing.T) {
	methods := []MethodMetric{
		{QualifiedName: "outer", StartLine: 1, EndLine: 20},
		{QualifiedName: "inner", StartLine: 5, EndLine: 10},
		{QualifiedName: "early", StartLine: 12, EndLine: 14},
		{QualifiedName: "late", StartLine: 14, EndLine: 16},
		{QualifiedName: "same", StartLine: 14, EndLine: 16},
	}
	cases := map[int]string{3: "outer", 7: "inner", 13: "early", 14: "late", 16: "late", 21: "<nil>"}
	for line, want := range cases {
		got := "<nil>"
		if name := enclosingMethod(methods, line); name != nil {
			got = *name
		}
		if got != want {
			t.Errorf("line %d: method = %q, want %q", line, got, want)
		}
	}
}

func TestIgnoreMarkerForms(t *testing.T) {
	src := `package x

func f(a, b int, p *bool) bool {
	_ = a < b && a > b // slopguard-ignore-mutant
	_ = a < b && a > b // slopguard-ignore-mutant(boundary, logical)
	// slopguard-ignore-mutant(negate_conditional)
	_ = a < b && a > b
	_ = a < b // slopguard-ignore-mutant(bogus)
	/* slopguard-ignore-mutant(boundary) */
	_ = a < b
	// slopguard-ignore-mutant(boundary)
	_ = a < b && a > b // slopguard-ignore-mutant(logical
	_ = "slopguard-ignore-mutant" == ""
	*p = a >= b // slopguard-ignore-mutant(boundary)
	return a < b
}
`
	var ignored []string
	for _, m := range genMutants(t, src) {
		if m.Status == StatusIgnored {
			ignored = append(ignored, itoa(m.Line)+" "+m.Operator+" "+m.Original)
		}
	}
	want := []string{
		// Bare marker: every mutant on the line.
		"4 boundary <", "4 negate_conditional <", "4 logical &&", "4 boundary >", "4 negate_conditional >",
		// A list: only those ids.
		"5 boundary <", "5 logical &&", "5 boundary >",
		// A comment-only line applies to the next line.
		"7 negate_conditional <", "7 negate_conditional >",
		// Unknown ids are dropped, so (bogus) ignores nothing on line 8.
		// /* ... */ on its own line applies to line 10.
		"10 boundary <",
		// Comment-only (boundary) for line 12 combines with the unclosed
		// (logical list on line 12, which runs to the end of the line.
		"12 boundary <", "12 logical &&", "12 boundary >",
		// The search is plain text, so a marker inside a string still counts.
		"13 negate_conditional ==",
		// A line that starts with a pointer dereference is code, not a comment.
		"14 boundary >=",
	}
	if !reflect.DeepEqual(ignored, want) {
		t.Errorf("ignored =\n  %s\nwant\n  %s", strings.Join(ignored, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestIgnoreMarkerStarCommentLines(t *testing.T) {
	cases := []struct {
		text        string
		commentOnly bool
	}{
		{"\t// slopguard-ignore-mutant", true},
		{"/* slopguard-ignore-mutant */", true},
		{" * slopguard-ignore-mutant", true},
		{"\t*\tslopguard-ignore-mutant", true},
		{" *", true},
		{" */ slopguard-ignore-mutant", true},
		{"\t*p = a < b // slopguard-ignore-mutant", false},
		{"\t**pp = 1 // slopguard-ignore-mutant", false},
		{"\t_ = a < b // slopguard-ignore-mutant", false},
	}
	for _, c := range cases {
		if got := isCommentOnly(c.text); got != c.commentOnly {
			t.Errorf("isCommentOnly(%q) = %v, want %v", c.text, got, c.commentOnly)
		}
	}
	// A marker on a " * ..." continuation line applies to the next line.
	src := "package x\n\n/*\n * slopguard-ignore-mutant\n */ var v = a < b\nvar w = a < b\n"
	for _, m := range genMutants(t, src) {
		if (m.Status == StatusIgnored) != (m.Line == 5) {
			t.Errorf("mutant %s: status %s", m.ID, m.Status)
		}
	}
}

func TestGenerateMutantsSkipsGeneratedFiles(t *testing.T) {
	src := "// Code generated by stringer. DO NOT EDIT.\n\npackage x\n\nvar v = a < b\n"
	if got := genMutants(t, src); len(got) != 0 {
		t.Errorf("generated files yield no mutants, got %v", summaries(got))
	}
}

func TestGenerateMutantsParseError(t *testing.T) {
	_, err := GenerateMutants([]byte("package x\nfunc {"), "bad.go")
	var se *SlopguardError
	if !asSlopguardError(err, &se) || se.Code != ErrParseFailed {
		t.Fatalf("expected parse_failed, got %v", err)
	}
}

func TestGenerateMutantsIsSortedAndApplies(t *testing.T) {
	src := "package x\n\nfunc f(a, b int) bool {\n\tg()\n\treturn !(a <= b)\n}\n"
	got := genMutants(t, src)
	assertSummaries(t, got,
		"4:2 remove_call g()→",
		"5:9 remove_not !→",
		"5:13 boundary <=→<",
		"5:13 negate_conditional <=→>",
	)
	applied := map[string]string{}
	for _, m := range got {
		applied[m.Operator] = string(m.Apply([]byte(src)))
	}
	if !strings.Contains(applied[OpRemoveCall], "{\n\t\n\treturn") {
		t.Errorf("remove_call result:\n%s", applied[OpRemoveCall])
	}
	if !strings.Contains(applied[OpRemoveNot], "return (a <= b)") {
		t.Errorf("remove_not result:\n%s", applied[OpRemoveNot])
	}
	if !strings.Contains(applied[OpNegateConditional], "return !(a > b)") {
		t.Errorf("negate_conditional result:\n%s", applied[OpNegateConditional])
	}
}

func TestPlanMutantsWalksLikeAnalyze(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "b.go", "package m\n\nfunc B(a, b int) bool { return a < b }\n")
	writeFile(t, root, "a.go", "package m\n\nfunc A(ok bool) bool { return !ok }\n")
	writeFile(t, root, "empty.go", "package m\n")
	writeFile(t, root, "a_test.go", "package m\n\nfunc helper() bool { return true }\n")
	writeFile(t, root, "vendor/dep/dep.go", "package dep\n\nvar v = 1 + 2\n")
	writeFile(t, root, "sub/c.go", "package sub\n\nfunc C(i int) { i++ }\n")

	plan, err := PlanMutants(root, DefaultAnalysisOptions(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.SourceRoot != root {
		t.Errorf("SourceRoot = %q, want %q", plan.SourceRoot, root)
	}
	var paths []string
	for _, f := range plan.Files {
		paths = append(paths, f.Path)
		if f.AbsPath != filepath.Join(root, filepath.FromSlash(f.Path)) || len(f.Source) == 0 {
			t.Errorf("file %+v: wrong AbsPath or empty Source", f)
		}
	}
	if want := []string{"a.go", "b.go", "empty.go", "sub/c.go"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("files = %v, want %v", paths, want)
	}
	var ids []string
	for _, m := range plan.Mutants {
		ids = append(ids, m.ID)
	}
	want := []string{"a.go:3:31:remove_not", "b.go:3:34:boundary", "b.go:3:34:negate_conditional", "sub/c.go:3:18:increment"}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}

	filtered, err := PlanMutants(root, AnalysisOptions{ExcludeGlobs: DefaultExcludeGlobs, IncludeGlobs: []string{"b.go"}}, []string{OpBoundary})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Files) != 1 || len(filtered.Mutants) != 1 || filtered.Mutants[0].ID != "b.go:3:34:boundary" {
		t.Errorf("include + operator filter: files=%d mutants=%v", len(filtered.Files), summaries(filtered.Mutants))
	}
}

func TestPlanMutantsSingleFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "only.go", "package m\n\nvar v = a < b\n")
	path := filepath.Join(root, "only.go")
	plan, err := PlanMutants(path, DefaultAnalysisOptions(), []string{OpBoundary})
	if err != nil {
		t.Fatal(err)
	}
	// Like analyze: the reported path is the base name, the source root the file.
	if plan.SourceRoot != path || len(plan.Mutants) != 1 || plan.Mutants[0].ID != "only.go:3:11:boundary" {
		t.Errorf("single file plan = %q %v", plan.SourceRoot, summaries(plan.Mutants))
	}
}

func TestPlanMutantsErrors(t *testing.T) {
	if _, err := PlanMutants("/no/such/path/xyz", DefaultAnalysisOptions(), nil); err == nil {
		t.Error("a missing path should fail with file_not_found")
	}
	root := t.TempDir()
	writeFile(t, root, "bad.go", "package m\nfunc {")
	var se *SlopguardError
	if _, err := PlanMutants(root, DefaultAnalysisOptions(), nil); !asSlopguardError(err, &se) || se.Code != ErrParseFailed {
		t.Errorf("expected parse_failed, got %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read unreadable files")
	}
	unreadable := t.TempDir()
	writeFile(t, unreadable, "locked.go", "package m\n")
	if err := os.Chmod(filepath.Join(unreadable, "locked.go"), 0); err != nil {
		t.Fatal(err)
	}
	if _, err := PlanMutants(unreadable, DefaultAnalysisOptions(), nil); !asSlopguardError(err, &se) || se.Code != ErrUnreadableFile {
		t.Errorf("expected unreadable_file, got %v", err)
	}
}
