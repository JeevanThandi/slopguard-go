package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReceiverTypeNameVariants(t *testing.T) {
	src := `package p
type Box[T any] struct{ v T }
func (b Box[T]) Get() T { return b.v }
func (b *Box[T]) Set(v T) { b.v = v }
type Plain struct{}
func (p *Plain) Do() {}
`
	report, err := AnalyzeSource([]byte(src), "g.go")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range report.Methods {
		got[m.Name] = m.TypeName
	}
	for name, want := range map[string]string{"Get": "Box", "Set": "Box", "Do": "Plain"} {
		if got[name] != want {
			t.Errorf("%s receiver = %q, want %q (got %v)", name, got[name], want, got)
		}
	}
}

func TestNamedTypeWithMethodIsEmitted(t *testing.T) {
	src := `package p
type Celsius float64
func (c Celsius) Freezing() bool { return c <= 0 }
`
	report, err := AnalyzeSource([]byte(src), "g.go")
	if err != nil {
		t.Fatal(err)
	}
	out := Aggregate(AggregateArgs{FileReports: []FileReport{report}, SourceRoot: "/r"})
	if len(out.Types) != 1 || out.Types[0].Kind != KindNamed || out.Types[0].MethodCount != 1 {
		t.Errorf("named type with method: %+v", out.Types)
	}
}

func TestTopLevelCodeHasNoMethodFrame(t *testing.T) {
	// A package-level var with a closure: no FuncDecl, so no method entry, and
	// the closure body's branches must not panic without a current method.
	src := `package p
var handler = func(a, b bool) bool { return a && b }
`
	report, err := AnalyzeSource([]byte(src), "g.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Methods) != 0 {
		t.Errorf("expected no methods for a package-level closure, got %d", len(report.Methods))
	}
}

func TestInterfaceMethodsAreNotCountedAsMethods(t *testing.T) {
	src := `package p
type Reader interface {
	Read(p []byte) (int, error)
	Close() error
}
`
	report, err := AnalyzeSource([]byte(src), "g.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Methods) != 0 {
		t.Errorf("interface signatures must not be methods, got %d", len(report.Methods))
	}
	if len(report.Types) != 1 || report.Types[0].Kind != KindInterface {
		t.Errorf("expected one interface type, got %+v", report.Types)
	}
}

func TestAnalyzeFileUnreadable(t *testing.T) {
	_, err := AnalyzeFile("/no/such/file.go", "file.go")
	var se *SlopguardError
	if !asSlopguardError(err, &se) || se.Code != ErrUnreadableFile {
		t.Errorf("expected unreadable_file, got %v", err)
	}
}

func TestAnalyzeSourceParseError(t *testing.T) {
	_, err := AnalyzeSource([]byte("package p\nfunc ( bad syntax"), "bad.go")
	var se *SlopguardError
	if !asSlopguardError(err, &se) || se.Code != ErrParseFailed {
		t.Errorf("expected parse_failed, got %v", err)
	}
}

func TestHeadBytesTruncatesLargeInput(t *testing.T) {
	big := make([]byte, 5000)
	for i := range big {
		big[i] = 'a'
	}
	if got := len(headBytes(big)); got != 2048 {
		t.Errorf("headBytes large = %d, want 2048", got)
	}
	small := []byte("short")
	if len(headBytes(small)) != len(small) {
		t.Error("headBytes should pass small input through")
	}
}

func TestAggregatorHelpers(t *testing.T) {
	if maxInt(3, 7) != 7 || maxInt(7, 3) != 7 {
		t.Error("maxInt")
	}
	if got := absolutize("/root", "/already/abs.go"); got != "/already/abs.go" {
		t.Errorf("absolutize abs = %q", got)
	}
	if got := absolutize("/root", "rel.go"); got != filepath.Join("/root", "rel.go") {
		t.Errorf("absolutize rel = %q", got)
	}
	if weightedCoverage(nil) != 0 {
		t.Error("weightedCoverage(nil) should be 0")
	}
	if coverageFor(nil, "/x", 1, 2) != 0 {
		t.Error("coverageFor(nil provider) should be 0")
	}
}

// methodOnlyCoverage knows method-level but never file-level, exercising the
// file-fallback branch in coverageFor.
type methodOnlyCoverage struct{}

func (methodOnlyCoverage) MethodCoverage(string, int, int) (float64, bool) { return 0, false }
func (methodOnlyCoverage) FileCoverage(string) (float64, bool)             { return 42, true }

func TestCoverageForFallsBackToFile(t *testing.T) {
	if got := coverageFor(methodOnlyCoverage{}, "/x", 1, 2); got != 42 {
		t.Errorf("coverageFor file fallback = %v, want 42", got)
	}
}

func TestRelativize(t *testing.T) {
	if got := relativize("/r/a/b.go", "/r"); got != "a/b.go" {
		t.Errorf("relativize = %q", got)
	}
	if got := relativize("/r", "/r"); got != "r" {
		t.Errorf("relativize self = %q, want basename", got)
	}
}

func TestShouldAnalyzeIncludeGlobs(t *testing.T) {
	opts := AnalysisOptions{IncludeGlobs: []string{"**/keep.go"}}
	if !shouldAnalyze("dir/keep.go", opts) {
		t.Error("keep.go should be included")
	}
	if shouldAnalyze("dir/other.go", opts) {
		t.Error("other.go should be excluded by include filter")
	}
	if shouldAnalyze("dir/file.txt", opts) {
		t.Error("non-go file should be rejected")
	}
}

func TestEnumerateUnreadableDir(t *testing.T) {
	// A path that exists as a file but is walked as a dir triggers ReadDir error.
	root := t.TempDir()
	writeFile(t, root, "ok.go", "package p\nfunc A(){}\n")
	// Remove read permission on a subdir to force an unreadable error.
	sub := filepath.Join(root, "locked")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, sub, "x.go", "package p\nfunc B(){}\n")
	if err := os.Chmod(sub, 0o000); err != nil {
		t.Skip("cannot chmod in this environment")
	}
	defer os.Chmod(sub, 0o755)
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses permission checks")
	}
	_, err := AnalyzeTree(root, DefaultAnalysisOptions())
	if err == nil {
		t.Error("expected an unreadable-dir error")
	}
}

func TestGlobUnclosedBracket(t *testing.T) {
	re := GlobToRegexp("file[abc")
	if !re.MatchString("file[abc") {
		t.Error("unclosed bracket should be treated literally")
	}
}

func TestFormatHeaderVariants(t *testing.T) {
	path := "/tmp/cover.out"
	withPath := CrapReport{ToolVersion: Version, SchemaVersion: "2", CoverageDataPath: &path, Threshold: 30}
	if !strings.Contains(header(withPath), "/tmp/cover.out") {
		t.Error("header should show coverage data path")
	}
	ephemeral := CrapReport{ToolVersion: Version, SchemaVersion: "2", CoverageAvailable: true, Threshold: 30}
	if !strings.Contains(header(ephemeral), "go test") {
		t.Error("header should note ephemeral coverage")
	}
	none := CrapReport{ToolVersion: Version, SchemaVersion: "2", Threshold: 30}
	if !strings.Contains(header(none), "none") {
		t.Error("header should note absent coverage")
	}
}

func TestPrettyReportEmptyAndNotesAndUnavailable(t *testing.T) {
	report := CrapReport{
		ToolVersion: Version, SchemaVersion: "2", Threshold: 30,
		Notes:   []string{"a note"},
		Summary: ReportSummary{WeightedCoverage: nil},
		Methods: []MethodCrap{},
	}
	out := PrettyReport(report, 20)
	if !strings.Contains(out, "a note") {
		t.Error("notes section missing")
	}
	if !strings.Contains(out, "No methods analyzed") {
		t.Error("empty methods message missing")
	}
	if !strings.Contains(out, "unavailable") {
		t.Error("coverage-unavailable line missing")
	}
}

func TestPadEnd(t *testing.T) {
	if got := padEnd("ab", 5); got != "ab   " {
		t.Errorf("padEnd = %q", got)
	}
	if got := padEnd("abcdef", 3); got != "abcdef" {
		t.Errorf("padEnd over-width = %q", got)
	}
}
