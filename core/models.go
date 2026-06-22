package core

import (
	"fmt"
	"math"
)

// MethodKind describes what kind of declaration a metric represents. Go has no
// constructors/accessors as language constructs, so the set is small: free
// functions and methods (functions with a receiver). Anonymous function
// literals do not get their own entry — their branches fold into the
// enclosing method with a cognitive nesting bump.
type MethodKind string

const (
	KindFunction MethodKind = "function"
	KindMethod   MethodKind = "method"
)

// TypeKind describes what kind of named type owns a set of methods. In Go,
// methods attach to a type by receiver rather than by lexical nesting, so a
// type's members are gathered package-wide during aggregation.
type TypeKind string

const (
	KindStruct    TypeKind = "struct"
	KindInterface TypeKind = "interface"
	KindNamed     TypeKind = "type" // defined non-struct/non-interface type (e.g. `type Celsius float64`)
)

// MethodMetric is the pure-syntactic analysis output for one function or
// method. No coverage is attached here — coverage is joined later by the
// aggregator so this type stays useful in no-coverage modes.
type MethodMetric struct {
	// Name is the leaf identifier as written in source, e.g. "Parse".
	Name string
	// QualifiedName includes the receiver type for methods, e.g.
	// "Parser.Parse"; for free functions it equals Name.
	QualifiedName string
	// TypeName is the receiver type name for methods, or "" for free functions.
	TypeName string
	Kind     MethodKind
	// File is the path relative to the analysis root (forward-slash).
	File      string
	StartLine int
	EndLine   int
	// Complexity is cyclomatic complexity (McCabe): branching decisions +1
	// from a base of 1.
	Complexity int
	// CognitiveComplexity is per the SonarSource 2023 spec: nesting-amplified,
	// flat dispatch charged once, early exits free.
	CognitiveComplexity int
	// WeightedComplexity is sqrt(Complexity × CognitiveComplexity), the value
	// fed into the CRAP formula since schema 2.
	WeightedComplexity float64
}

// MakeMethodMetric finalises a metric by computing its weighted complexity.
func MakeMethodMetric(m MethodMetric) MethodMetric {
	cyc := math.Max(0, float64(m.Complexity))
	cog := math.Max(0, float64(m.CognitiveComplexity))
	m.WeightedComplexity = math.Sqrt(cyc * cog)
	return m
}

// MethodID is the stable cross-tool identifier for a method.
// Format: relative/path.go#Qualified.Name@startLine.
func MethodID(file, qualifiedName string, startLine int) string {
	return fmt.Sprintf("%s#%s@%d", file, qualifiedName, startLine)
}

// TypeDecl is a lightweight record of a struct/interface/named-type
// declaration. Unlike the TypeScript/Swift ports — where methods are lexically
// nested inside their type — Go methods attach by receiver, so the analyzer
// records only the declaration site here and the aggregator gathers the
// owning methods package-wide.
type TypeDecl struct {
	Kind      TypeKind
	Name      string
	File      string
	StartLine int
	EndLine   int
}

// TypeID is the stable identifier for a type declaration.
func TypeID(file, name string, startLine int) string {
	return fmt.Sprintf("%s#%s@%d", file, name, startLine)
}

// FileReport is the pure-syntactic result for a single source file.
type FileReport struct {
	Path    string
	Methods []MethodMetric
	Types   []TypeDecl
}

// MethodCrap is a single method's entry in the final report. JSON field order
// is alphabetical to keep --json output diff-friendly in CI and aligned with
// the TypeScript/Swift ports' stable-sorted encoding.
type MethodCrap struct {
	CognitiveComplexity int        `json:"cognitiveComplexity"`
	Complexity          int        `json:"complexity"`
	Coverage            float64    `json:"coverage"`
	Crap                float64    `json:"crap"`
	EndLine             int        `json:"endLine"`
	File                string     `json:"file"`
	ID                  string     `json:"id"`
	IsCrappy            bool       `json:"isCrappy"`
	Kind                MethodKind `json:"kind"`
	Line                int        `json:"line"`
	Name                string     `json:"name"`
	QualifiedName       string     `json:"qualifiedName"`
	TypeName            *string    `json:"typeName"`
	WeightedComplexity  float64    `json:"weightedComplexity"`
}

// TypeCrap is a type-level aggregation entry in the final report.
type TypeCrap struct {
	AggregatedCrap           float64  `json:"aggregatedCrap"`
	File                     string   `json:"file"`
	ID                       string   `json:"id"`
	IsCrappy                 bool     `json:"isCrappy"`
	Kind                     TypeKind `json:"kind"`
	Line                     int      `json:"line"`
	MaxCognitiveComplexity   int      `json:"maxCognitiveComplexity"`
	MaxComplexity            int      `json:"maxComplexity"`
	MaxCrap                  float64  `json:"maxCrap"`
	MethodCount              int      `json:"methodCount"`
	Name                     string   `json:"name"`
	SumCrap                  float64  `json:"sumCrap"`
	TotalCognitiveComplexity int      `json:"totalCognitiveComplexity"`
	TotalComplexity          int      `json:"totalComplexity"`
	WeightedCoverage         float64  `json:"weightedCoverage"`
	WeightedTotalComplexity  float64  `json:"weightedTotalComplexity"`
}

// ReportSummary holds the rollup statistics across the whole analysis.
type ReportSummary struct {
	AverageCognitiveComplexity float64 `json:"averageCognitiveComplexity"`
	AverageComplexity          float64 `json:"averageComplexity"`
	AverageCrap                float64 `json:"averageCrap"`
	AverageWeightedComplexity  float64 `json:"averageWeightedComplexity"`
	CrappyMethodCount          int     `json:"crappyMethodCount"`
	CrappyTypeCount            int     `json:"crappyTypeCount"`
	FileCount                  int     `json:"fileCount"`
	MaxCrap                    float64 `json:"maxCrap"`
	MethodCount                int     `json:"methodCount"`
	TypeCount                  int     `json:"typeCount"`
	// WeightedCoverage is nil when coverage was unavailable (--no-coverage).
	WeightedCoverage *float64 `json:"weightedCoverage"`
}

// CrapReport is the top-level payload emitted by --json. Stable and versioned
// (SchemaVersion "2", shared with slopguard-swift and slopguard-typescript).
type CrapReport struct {
	CoverageAvailable bool `json:"coverageAvailable"`
	// CoverageDataPath is the on-disk coverage profile the report was joined
	// against, or nil when coverage was generated ephemerally / unavailable.
	CoverageDataPath *string       `json:"coverageDataPath"`
	GeneratedAt      string        `json:"generatedAt"`
	Methods          []MethodCrap  `json:"methods"`
	Notes            []string      `json:"notes"`
	SchemaVersion    string        `json:"schemaVersion"`
	SourceRoot       string        `json:"sourceRoot"`
	Summary          ReportSummary `json:"summary"`
	Threshold        float64       `json:"threshold"`
	Tool             string        `json:"tool"`
	ToolVersion      string        `json:"toolVersion"`
	Types            []TypeCrap    `json:"types"`
}
