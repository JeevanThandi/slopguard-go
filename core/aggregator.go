package core

import (
	"math"
	"path"
	"path/filepath"
	"sort"
	"time"
)

// schemaTwoNote is attached to every report so downstream consumers know the
// crap-derived fields are driven by the weighted blend, not raw cyclomatic.
const schemaTwoNote = "Score is wCRAP (weighted CRAP) since schema 2: complexity input is " +
	"weightedComplexity = sqrt(cyclomatic × cognitive), not raw cyclomatic. " +
	"Both raw metrics ship under `complexity` (cyclomatic, McCabe) and " +
	"`cognitiveComplexity` (SonarSource 2023); the score itself is reported " +
	"under the existing `crap` field for schema continuity. Recursion " +
	"increment is deferred (known undercount vs Sonar parity)."

// AggregateArgs bundles the inputs to Aggregate.
type AggregateArgs struct {
	FileReports []FileReport
	// SourceRoot resolves each FileReport.Path (relative) to an absolute path
	// so the coverage provider can match it.
	SourceRoot string
	// CoverageDataPath is recorded on the report; not used for analysis.
	CoverageDataPath *string
	Threshold        float64
	// Coverage is nil for no-coverage mode — every method reads 0%.
	Coverage CoverageProvider
	Notes    []string
	// GeneratedAt is stamped on the report; zero value means time.Now().
	GeneratedAt time.Time
}

// Aggregate joins per-file complexity output with optional coverage data to
// produce the final CrapReport. Unlike the TypeScript/Swift ports, Go methods
// attach to types by receiver, so types collect their owning methods
// package-wide (by matching receiver name within the same directory) rather
// than by lexical nesting.
func Aggregate(args AggregateArgs) CrapReport {
	threshold := args.Threshold
	if threshold == 0 {
		threshold = DefaultCrapThreshold
	}
	rootAbs, _ := filepath.Abs(args.SourceRoot)
	coverageAvailable := args.Coverage != nil

	var methods []MethodCrap
	var totalComplexity, totalCognitive int
	var totalWeighted, totalCovered, totalExecutable float64

	for _, fr := range args.FileReports {
		absFile := absolutize(rootAbs, fr.Path)
		for _, m := range fr.Methods {
			cov := coverageFor(args.Coverage, absFile, m.StartLine, m.EndLine)
			crap := CrapScore(m.WeightedComplexity, cov)
			executable := float64(maxInt(0, m.EndLine-m.StartLine+1))

			methods = append(methods, MethodCrap{
				ID:                  MethodID(m.File, m.QualifiedName, m.StartLine),
				File:                m.File,
				Line:                m.StartLine,
				EndLine:             m.EndLine,
				TypeName:            typeNamePtr(m.TypeName),
				Name:                m.Name,
				QualifiedName:       m.QualifiedName,
				Kind:                m.Kind,
				Complexity:          m.Complexity,
				CognitiveComplexity: m.CognitiveComplexity,
				WeightedComplexity:  m.WeightedComplexity,
				Coverage:            cov,
				Crap:                crap,
				IsCrappy:            crap > threshold,
			})
			totalComplexity += m.Complexity
			totalCognitive += m.CognitiveComplexity
			totalWeighted += m.WeightedComplexity
			totalCovered += executable * cov / 100
			totalExecutable += executable
		}
	}

	types := aggregateTypes(args.FileReports, methods, threshold)

	sort.SliceStable(methods, func(i, j int) bool { return methods[i].Crap > methods[j].Crap })
	sort.SliceStable(types, func(i, j int) bool { return types[i].AggregatedCrap > types[j].AggregatedCrap })

	crappyMethods := 0
	for _, m := range methods {
		if m.IsCrappy {
			crappyMethods++
		}
	}
	crappyTypes := 0
	for _, t := range types {
		if t.IsCrappy {
			crappyTypes++
		}
	}

	count := len(methods)
	var avgCrap, maxCrap float64
	for _, m := range methods {
		avgCrap += m.Crap
	}
	if count > 0 {
		avgCrap /= float64(count)
		maxCrap = methods[0].Crap
	}

	var weightedCoverage *float64
	if coverageAvailable {
		var pct float64
		if totalExecutable > 0 {
			pct = totalCovered / totalExecutable * 100
		}
		weightedCoverage = &pct
	}

	generatedAt := args.GeneratedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}

	return CrapReport{
		SchemaVersion:     SchemaVersion,
		Tool:              ToolName,
		ToolVersion:       Version,
		GeneratedAt:       generatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		SourceRoot:        rootAbs,
		CoverageDataPath:  args.CoverageDataPath,
		Threshold:         threshold,
		CoverageAvailable: coverageAvailable,
		Notes:             append([]string{schemaTwoNote}, args.Notes...),
		Summary: ReportSummary{
			FileCount:                  len(args.FileReports),
			TypeCount:                  len(types),
			MethodCount:                count,
			CrappyMethodCount:          crappyMethods,
			CrappyTypeCount:            crappyTypes,
			AverageCrap:                avgCrap,
			MaxCrap:                    maxCrap,
			AverageComplexity:          avgFloat(totalComplexity, count),
			AverageCognitiveComplexity: avgFloat(totalCognitive, count),
			AverageWeightedComplexity:  avgFloatF(totalWeighted, count),
			WeightedCoverage:           weightedCoverage,
		},
		Methods: nonNilMethods(methods),
		Types:   nonNilTypes(types),
	}
}

// aggregateTypes builds one TypeCrap per declared struct/interface (and per
// named type that owns at least one method) by gathering the methods whose
// receiver type matches the declaration within the same package directory.
func aggregateTypes(fileReports []FileReport, methods []MethodCrap, threshold float64) []TypeCrap {
	// Index methods by (package dir, receiver type name).
	byOwner := map[string][]MethodCrap{}
	for _, m := range methods {
		if m.TypeName == nil {
			continue
		}
		key := ownerKey(path.Dir(m.File), *m.TypeName)
		byOwner[key] = append(byOwner[key], m)
	}

	var types []TypeCrap
	for _, fr := range fileReports {
		for _, decl := range fr.Types {
			owned := byOwner[ownerKey(path.Dir(decl.File), decl.Name)]
			// Emit struct/interface declarations always (they are first-class
			// API surface); emit other named types only when they carry methods.
			if len(owned) == 0 && decl.Kind == KindNamed {
				continue
			}
			types = append(types, makeTypeCrap(decl, owned, threshold))
		}
	}
	return types
}

func makeTypeCrap(decl TypeDecl, owned []MethodCrap, threshold float64) TypeCrap {
	var totalComplexity, maxComplexity, totalCognitive, maxCognitive int
	scores := make([]float64, 0, len(owned))
	for _, m := range owned {
		totalComplexity += m.Complexity
		totalCognitive += m.CognitiveComplexity
		if m.Complexity > maxComplexity {
			maxComplexity = m.Complexity
		}
		if m.CognitiveComplexity > maxCognitive {
			maxCognitive = m.CognitiveComplexity
		}
		scores = append(scores, m.Crap)
	}
	agg := AggregateCrap(scores)
	weightedCov := weightedCoverage(owned)
	weightedTotal := math.Sqrt(float64(totalComplexity) * float64(totalCognitive))
	aggregated := CrapScore(weightedTotal, weightedCov)

	return TypeCrap{
		ID:                       TypeID(decl.File, decl.Name, decl.StartLine),
		File:                     decl.File,
		Line:                     decl.StartLine,
		Kind:                     decl.Kind,
		Name:                     decl.Name,
		MethodCount:              len(owned),
		TotalComplexity:          totalComplexity,
		MaxComplexity:            maxComplexity,
		TotalCognitiveComplexity: totalCognitive,
		MaxCognitiveComplexity:   maxCognitive,
		WeightedTotalComplexity:  weightedTotal,
		WeightedCoverage:         weightedCov,
		SumCrap:                  agg.Sum,
		MaxCrap:                  agg.Max,
		AggregatedCrap:           aggregated,
		IsCrappy:                 aggregated > threshold || agg.Max > threshold,
	}
}

func ownerKey(dir, name string) string { return dir + "\x00" + name }

func coverageFor(provider CoverageProvider, absFile string, line, endLine int) float64 {
	if provider == nil {
		return 0
	}
	if pct, ok := provider.MethodCoverage(absFile, line, endLine); ok {
		return pct
	}
	if pct, ok := provider.FileCoverage(absFile); ok {
		return pct
	}
	return 0
}

func weightedCoverage(methods []MethodCrap) float64 {
	if len(methods) == 0 {
		return 0
	}
	var totalLines, weighted float64
	for _, m := range methods {
		lines := float64(maxInt(1, m.EndLine-m.Line+1))
		totalLines += lines
		weighted += m.Coverage * lines
	}
	if totalLines == 0 {
		return 0
	}
	return weighted / totalLines
}

func absolutize(rootAbs, rel string) string {
	if filepath.IsAbs(rel) {
		return rel
	}
	return filepath.Join(rootAbs, rel)
}

func typeNamePtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func avgFloat(total, count int) float64 {
	if count == 0 {
		return 0
	}
	return float64(total) / float64(count)
}

func avgFloatF(total float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// nonNilMethods/nonNilTypes guarantee JSON encodes [] rather than null for an
// empty analysis — keeps the schema stable for consumers.
func nonNilMethods(m []MethodCrap) []MethodCrap {
	if m == nil {
		return []MethodCrap{}
	}
	return m
}

func nonNilTypes(t []TypeCrap) []TypeCrap {
	if t == nil {
		return []TypeCrap{}
	}
	return t
}
