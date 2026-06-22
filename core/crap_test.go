package core

import (
	"math"
	"testing"
)

func TestCrapScoreFullCoverageCollapsesToComplexity(t *testing.T) {
	// cov=100 => score is exactly comp.
	if got := CrapScore(12, 100); got != 12 {
		t.Errorf("CrapScore(12,100) = %v, want 12", got)
	}
}

func TestCrapScoreZeroCoverage(t *testing.T) {
	// cov=0 => comp² + comp. comp=20 => 420.
	if got := CrapScore(20, 0); got != 420 {
		t.Errorf("CrapScore(20,0) = %v, want 420", got)
	}
}

func TestCrapScoreCubedCoverageFactor(t *testing.T) {
	// At 50% coverage the risk term is 0.5³ = 0.125 of the untested penalty.
	comp := 10.0
	got := CrapScore(comp, 50)
	want := comp*comp*0.125 + comp
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("CrapScore(10,50) = %v, want %v", got, want)
	}
}

func TestCrapScoreClampsInputs(t *testing.T) {
	if got := CrapScore(-5, 50); got != 0 {
		t.Errorf("negative complexity should clamp to 0, got %v", got)
	}
	if got := CrapScore(10, 150); got != 10 {
		t.Errorf("coverage >100 should clamp to 100, got %v", got)
	}
	if CrapScore(10, -10) != CrapScore(10, 0) {
		t.Error("coverage <0 should clamp to 0")
	}
}

func TestAggregateCrap(t *testing.T) {
	agg := AggregateCrap([]float64{3, 9, 1})
	if agg.Sum != 13 || agg.Max != 9 || agg.MethodCount != 3 {
		t.Errorf("AggregateCrap = %+v", agg)
	}
	empty := AggregateCrap(nil)
	if empty.Sum != 0 || empty.Max != 0 || empty.MethodCount != 0 {
		t.Errorf("empty AggregateCrap = %+v", empty)
	}
}

func TestWeightedComplexityIsGeometricMean(t *testing.T) {
	m := MakeMethodMetric(MethodMetric{Complexity: 50, CognitiveComplexity: 1})
	if math.Abs(m.WeightedComplexity-math.Sqrt(50)) > 1e-9 {
		t.Errorf("weighted = %v, want sqrt(50)", m.WeightedComplexity)
	}
}
