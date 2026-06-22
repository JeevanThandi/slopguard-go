package core

import (
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strings"
)

// generatedHeader matches the conventional "// Code generated ... DO NOT EDIT."
// marker the Go ecosystem stamps on machine-written files. Such files are
// branchy codegen noise that swamps real signal, so the analyzer skips them
// (mirroring the TypeScript port skipping *.generated.* and minified bundles).
var generatedHeader = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)

// IsGoSource reports whether a path is an analyzable Go source file.
func IsGoSource(path string) bool {
	return strings.HasSuffix(path, ".go")
}

// AnalyzeFile reads and analyzes a single Go source file. reportedPath is the
// path recorded on the result (typically relative to the analysis root);
// absPath is read from disk.
func AnalyzeFile(absPath, reportedPath string) (FileReport, error) {
	src, err := os.ReadFile(absPath)
	if err != nil {
		return FileReport{}, UnreadableFile(absPath, err)
	}
	return AnalyzeSource(src, reportedPath)
}

// AnalyzeSource analyzes Go source already held in memory. Generated files
// (per the standard header) yield an empty report so they don't pollute the
// numbers.
func AnalyzeSource(src []byte, reportedPath string) (FileReport, error) {
	if generatedHeader.Match(headBytes(src)) {
		return FileReport{Path: reportedPath}, nil
	}
	fset := token.NewFileSet()
	// SkipObjectResolution keeps parsing fast; the analyzer relies only on the
	// syntax tree, never on resolved identifiers.
	f, err := parser.ParseFile(fset, reportedPath, src, parser.SkipObjectResolution)
	if err != nil {
		return FileReport{}, ParseFailed(reportedPath, err)
	}
	methods, types := analyze(fset, reportedPath, f)
	return FileReport{Path: reportedPath, Methods: methods, Types: types}, nil
}

// headBytes returns the first chunk of src, enough to hold a generated header
// that conventionally sits in the first few lines.
func headBytes(src []byte) []byte {
	const limit = 2048
	if len(src) > limit {
		return src[:limit]
	}
	return src
}
