package core

// CoverageProvider abstracts coverage data so this package does not depend on
// the coverage subsystem (which spawns `go test`). The coverage package
// supplies a concrete implementation via its CoverageIndex.
//
// The boolean return reports whether coverage is known: methodCoverage returns
// (pct, true) when at least one executable line of the method was tracked, and
// (0, false) when the file or line range is unknown so the caller can fall
// back to file-level coverage or 0%.
type CoverageProvider interface {
	// MethodCoverage returns line coverage in [0, 100] for the line span, or
	// ok=false when unknown.
	MethodCoverage(absolutePath string, line, endLine int) (pct float64, ok bool)
	// FileCoverage returns whole-file line coverage in [0, 100], or ok=false
	// when unknown.
	FileCoverage(absolutePath string) (pct float64, ok bool)
}
