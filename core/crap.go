package core

import "math"

// The CRAP (Change Risk Anti-Patterns) formula.
//
//	CRAP(m) = comp(m)² × (1 − cov(m)/100)³ + comp(m)
//
// Where:
//   - comp is whatever complexity weighting the caller chooses to feed in.
//     Since schema 2, slopguard feeds weightedComplexity =
//     sqrt(cyclomatic × cognitive) so the score reflects both raw branching
//     (cyclomatic) and human-perceived difficulty (cognitive). The formula
//     itself is metric-agnostic — call sites that want classic
//     cyclomatic-driven CRAP can still pass the raw cyclomatic value.
//   - cov is the line coverage percentage in [0, 100].
//
// Interpretation:
//   - Fully covered code (cov = 100) collapses to comp — complexity alone.
//   - Untested code (cov = 0) penalises quadratically: comp² + comp.
//   - The cubed coverage factor sharply rewards even partial test coverage.

// DefaultCrapThreshold is the default threshold above which a method/type is
// considered "crappy", matching the original CRAP paper.
const DefaultCrapThreshold = 30.0

// CrapScore computes the CRAP score for a single unit of code. The complexity
// weighting is clamped to ≥ 0 and coverage is clamped to [0, 100]. The result
// is always ≥ 0.
func CrapScore(complexity, coveragePercent float64) float64 {
	comp := math.Max(0, complexity)
	cov := math.Max(0, math.Min(100, coveragePercent))
	covFactor := 1 - cov/100
	return comp*comp*(covFactor*covFactor*covFactor) + comp
}

// CrapAggregate captures three useful views over a collection of per-method
// CRAP scores:
//   - Sum: total burden of the type — comparable across types.
//   - Max: worst single method — drives the "biggest fire" metric.
//   - MethodCount: how many methods contributed.
type CrapAggregate struct {
	Sum         float64
	Max         float64
	MethodCount int
}

// AggregateCrap reduces a slice of method CRAP values into a CrapAggregate.
func AggregateCrap(scores []float64) CrapAggregate {
	var agg CrapAggregate
	for _, s := range scores {
		agg.Sum += s
		if s > agg.Max {
			agg.Max = s
		}
		agg.MethodCount++
	}
	return agg
}
