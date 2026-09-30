package core

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Mutation operator ids. Every slopguard port uses the same ids — in
// --operators, in the JSON `operator` field and in ignore markers — so keep
// them in step with the siblings. An id stays valid even where Go has no
// matching construct.
const (
	// OpArithmetic swaps + and -, * and /, turns % into *, and does the same
	// for the compound assignments (+= and -=, *= and /=, %= into *=).
	OpArithmetic = "arithmetic"
	// OpBooleanLiteral swaps true and false.
	OpBooleanLiteral = "boolean_literal"
	// OpBoundary moves a comparison boundary: < and <=, > and >=.
	OpBoundary = "boundary"
	// OpIncrement swaps ++ and --.
	OpIncrement = "increment"
	// OpInvertNegative removes a unary minus: -x becomes x.
	OpInvertNegative = "invert_negative"
	// OpLogical swaps && and ||.
	OpLogical = "logical"
	// OpNegateConditional negates a comparison: == and !=, < into >=, <= into >,
	// > into <=, >= into <.
	OpNegateConditional = "negate_conditional"
	// OpRemoveCall deletes a statement that is only a call.
	OpRemoveCall = "remove_call"
	// OpRemoveNot removes a unary not: !x becomes x.
	OpRemoveNot = "remove_not"
)

// MutationOperators lists every operator id, sorted. It is the default set
// when --operators is not given.
var MutationOperators = []string{
	OpArithmetic,
	OpBooleanLiteral,
	OpBoundary,
	OpIncrement,
	OpInvertNegative,
	OpLogical,
	OpNegateConditional,
	OpRemoveCall,
	OpRemoveNot,
}

// IsMutationOperator reports whether id is a known operator id.
func IsMutationOperator(id string) bool {
	for _, op := range MutationOperators {
		if op == id {
			return true
		}
	}
	return false
}

// ParseOperators resolves --operators values (comma-separated; the flag may
// repeat) into a sorted list without duplicates. No ids at all means every
// operator. An unknown id is an invalid_argument error.
func ParseOperators(values []string) ([]string, error) {
	requested := map[string]bool{}
	var unknown []string
	for _, value := range values {
		for _, id := range strings.Split(value, ",") {
			id = strings.TrimSpace(id)
			switch {
			case id == "":
				continue
			case IsMutationOperator(id):
				requested[id] = true
			default:
				unknown = append(unknown, id)
			}
		}
	}
	if len(unknown) > 0 {
		return nil, InvalidArgument("--operators", fmt.Sprintf("unknown operator(s): %s (expected: %s)",
			strings.Join(unknown, ", "), strings.Join(MutationOperators, ", ")))
	}
	if len(requested) == 0 {
		return append([]string{}, MutationOperators...), nil
	}
	var operators []string
	for _, op := range MutationOperators {
		if requested[op] {
			operators = append(operators, op)
		}
	}
	return operators, nil
}

// MutantStatus is what happened to a mutant.
type MutantStatus string

const (
	// StatusKilled: the tests failed with the mutant in place.
	StatusKilled MutantStatus = "killed"
	// StatusSurvived: the tests still passed, so no test checks this behaviour.
	StatusSurvived MutantStatus = "survived"
	// StatusTimeout: the test run exceeded the timeout; counted as killed.
	StatusTimeout MutantStatus = "timeout"
	// StatusNoCoverage: no test executes the mutated line, so it was not run.
	StatusNoCoverage MutantStatus = "no_coverage"
	// StatusCompileError: the mutant did not compile; excluded from the score.
	StatusCompileError MutantStatus = "compile_error"
	// StatusIgnored: switched off by a slopguard-ignore-mutant marker.
	StatusIgnored MutantStatus = "ignored"
	// StatusPending: generated but not run (--dry-run, or not run yet).
	StatusPending MutantStatus = "pending"
)

// Mutant is one small change to one source file. JSON field order is
// alphabetical, like the CRAP report models.
type Mutant struct {
	// Column is 1-based and counts Unicode code points from the line start.
	Column int `json:"column"`
	// File is the path relative to the source root (forward slashes).
	File string `json:"file"`
	// ID is `<file>:<line>:<column>:<operator>`.
	ID   string `json:"id"`
	Line int    `json:"line"`
	// Method is the qualified name of the enclosing function or method, or
	// nil for code outside any function.
	Method   *string `json:"method"`
	Operator string  `json:"operator"`
	// Original is the exact source text the mutant replaces.
	Original string `json:"original"`
	// Replacement is the exact text written in its place.
	Replacement string       `json:"replacement"`
	Status      MutantStatus `json:"status"`

	// start and end are the byte offsets of Original in the file's source.
	start, end int
}

// MutantID builds the stable mutant id `<file>:<line>:<column>:<operator>`.
func MutantID(file string, line, column int, operator string) string {
	return fmt.Sprintf("%s:%d:%d:%s", file, line, column, operator)
}

// Apply returns a copy of src, the source of the mutant's file, with the
// mutant's change applied.
func (m Mutant) Apply(src []byte) []byte {
	out := make([]byte, 0, len(src)-len(m.Original)+len(m.Replacement))
	out = append(out, src[:m.start]...)
	out = append(out, m.Replacement...)
	return append(out, src[m.end:]...)
}

// SortMutants orders mutants by file (byte-wise), line, column and operator id.
func SortMutants(mutants []Mutant) {
	sort.SliceStable(mutants, func(i, j int) bool {
		a, b := mutants[i], mutants[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return a.Operator < b.Operator
	})
}

// MutationSummary counts mutants by status. The counts always sum to
// MutantCount. JSON field order is alphabetical.
type MutationSummary struct {
	CompileErrors int `json:"compileErrors"`
	// FileCount is the number of source files scanned, including files that
	// yielded no mutants.
	FileCount   int `json:"fileCount"`
	Ignored     int `json:"ignored"`
	Killed      int `json:"killed"`
	MutantCount int `json:"mutantCount"`
	// MutationScore is (killed + timedOut) / (killed + timedOut + survived +
	// noCoverage) × 100, unrounded; nil when that denominator is 0.
	MutationScore *float64 `json:"mutationScore"`
	NoCoverage    int      `json:"noCoverage"`
	Pending       int      `json:"pending"`
	Survived      int      `json:"survived"`
	TimedOut      int      `json:"timedOut"`
}

// SummarizeMutants counts statuses and computes the mutation score.
func SummarizeMutants(mutants []Mutant, fileCount int) MutationSummary {
	s := MutationSummary{FileCount: fileCount, MutantCount: len(mutants)}
	for _, m := range mutants {
		switch m.Status {
		case StatusKilled:
			s.Killed++
		case StatusSurvived:
			s.Survived++
		case StatusTimeout:
			s.TimedOut++
		case StatusNoCoverage:
			s.NoCoverage++
		case StatusCompileError:
			s.CompileErrors++
		case StatusIgnored:
			s.Ignored++
		default:
			s.Pending++
		}
	}
	detected := s.Killed + s.TimedOut
	scored := detected + s.Survived + s.NoCoverage
	if scored > 0 {
		score := float64(detected) / float64(scored) * 100
		s.MutationScore = &score
	}
	return s
}

// Standard notes on a mutation report. The wording is shared with every port.
const (
	// NoteEverySurvived: at least one mutant ran and none was killed.
	NoteEverySurvived = "Every tested mutant survived. Check that the tests import the source under --path."
	// NoteNoCoverageData: coverage was requested but the coverage run gave no
	// usable data.
	NoteNoCoverageData = "The baseline test run produced no coverage data, so every mutant was run."
)

// CoverageRunExitNote is the note for a coverage run that exited non-zero
// but still produced coverage data, which mutate used.
func CoverageRunExitNote(exitCode int) string {
	return fmt.Sprintf("The coverage run exited with code %d; its coverage data was still used.", exitCode)
}

// MutationResultNotes returns the standard notes that follow from the
// mutant statuses: every tested mutant survived, and mutants that did not
// compile.
func MutationResultNotes(s MutationSummary) []string {
	var notes []string
	if s.Survived > 0 && s.Killed+s.TimedOut == 0 {
		notes = append(notes, NoteEverySurvived)
	}
	if s.CompileErrors > 0 {
		notes = append(notes, fmt.Sprintf("%d mutant(s) did not compile and are excluded from the score.", s.CompileErrors))
	}
	return notes
}

// MutationReport is the top-level payload of `mutate --json`. It is versioned
// apart from the CRAP report (MutationSchemaVersion; ReportType "mutation")
// and shared with every slopguard port. JSON field order is alphabetical.
type MutationReport struct {
	// CoverageAvailable reports whether line coverage classified no_coverage
	// mutants.
	CoverageAvailable bool     `json:"coverageAvailable"`
	GeneratedAt       string   `json:"generatedAt"`
	Mutants           []Mutant `json:"mutants"`
	Notes             []string `json:"notes"`
	// Operators lists the enabled operator ids, sorted.
	Operators []string `json:"operators"`
	// ProjectRoot is where go test ran; nil when no test ran.
	ProjectRoot *string `json:"projectRoot"`
	ReportType  string  `json:"reportType"`
	// Runner is "go test"; nil when no test ran.
	Runner        *string         `json:"runner"`
	SchemaVersion string          `json:"schemaVersion"`
	SourceRoot    string          `json:"sourceRoot"`
	Summary       MutationSummary `json:"summary"`
	// TimeoutSeconds is the per-mutant timeout; nil when no test ran.
	TimeoutSeconds *float64 `json:"timeoutSeconds"`
	Tool           string   `json:"tool"`
	ToolVersion    string   `json:"toolVersion"`
}

// MutationReportArgs bundles the inputs to NewMutationReport.
type MutationReportArgs struct {
	CoverageAvailable bool
	FileCount         int
	// GeneratedAt is stamped on the report; the zero value means time.Now().
	GeneratedAt    time.Time
	Mutants        []Mutant
	Notes          []string
	Operators      []string
	ProjectRoot    *string
	Runner         *string
	SourceRoot     string
	TimeoutSeconds *float64
}

// NewMutationReport assembles a report: it computes the summary and stamps the
// tool, version and schema metadata. Nil slices become empty JSON arrays.
func NewMutationReport(args MutationReportArgs) MutationReport {
	generatedAt := args.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	mutants := args.Mutants
	if mutants == nil {
		mutants = []Mutant{}
	}
	return MutationReport{
		CoverageAvailable: args.CoverageAvailable,
		GeneratedAt:       generatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		Mutants:           mutants,
		Notes:             append([]string{}, args.Notes...),
		Operators:         append([]string{}, args.Operators...),
		ProjectRoot:       args.ProjectRoot,
		ReportType:        "mutation",
		Runner:            args.Runner,
		SchemaVersion:     MutationSchemaVersion,
		SourceRoot:        args.SourceRoot,
		Summary:           SummarizeMutants(mutants, args.FileCount),
		TimeoutSeconds:    args.TimeoutSeconds,
		Tool:              ToolName,
		ToolVersion:       Version,
	}
}
