package vectorstore

import (
	"math"
	"testing"
)

func TestVectorLiteralRejectsZeroAndNonFiniteVectors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		vector []float32
	}{
		{"empty", nil},
		{"zero", []float32{0, 0}},
		{"not-a-number", []float32{float32(math.NaN())}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := vectorLiteral(tc.vector); err == nil {
				t.Errorf("vectorLiteral(%v) = %q, want error", tc.vector, got)
			}
		})
	}
	if got, err := vectorLiteral([]float32{1, -0.5}); err != nil || got != "[1,-0.5]" {
		t.Errorf("vectorLiteral([1,-0.5]) = %q, %v, want [1,-0.5], nil", got, err)
	}
}
