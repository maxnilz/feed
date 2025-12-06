package ai

import (
	"math"
	"testing"
)

func TestComputeSimilarity(t *testing.T) {
	e := &GeminiEmbedder{}

	tests := []struct {
		name string
		v1   []float32
		v2   []float32
		want float32
	}{
		{
			name: "Identical vectors",
			v1:   []float32{1.0, 0.0},
			v2:   []float32{1.0, 0.0},
			want: 1.0,
		},
		{
			name: "Orthogonal vectors",
			v1:   []float32{1.0, 0.0},
			v2:   []float32{0.0, 1.0},
			want: 0.0,
		},
		{
			name: "Opposite vectors",
			v1:   []float32{1.0, 0.0},
			v2:   []float32{-1.0, 0.0},
			want: -1.0,
		},
		{
			name: "Different magnitudes",
			v1:   []float32{1.0, 0.0},
			v2:   []float32{5.0, 0.0},
			want: 1.0,
		},
		{
			name: "Empty vectors",
			v1:   []float32{},
			v2:   []float32{},
			want: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := e.ComputeSimilarity(tt.v1, tt.v2)
			if math.Abs(float64(got-tt.want)) > 1e-5 {
				t.Errorf("ComputeSimilarity() = %v, want %v", got, tt.want)
			}
		})
	}
}
