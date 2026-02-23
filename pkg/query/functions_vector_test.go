package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestExtractValue_TypeVector(t *testing.T) {
	embedding := []float32{0.1, 0.2, 0.3, 0.4}
	node := &storage.Node{
		ID:     1,
		Labels: []string{"Concept"},
		Properties: map[string]storage.Value{
			"name":      storage.StringValue("Quantum Mechanics"),
			"embedding": storage.VectorValue(embedding),
		},
	}

	context := map[string]any{
		"c": node,
	}

	tests := []struct {
		name     string
		expr     Expression
		wantNil  bool
		wantLen  int
		wantVals []float32
	}{
		{
			name:     "extract vector property from node",
			expr:     &PropertyExpression{Variable: "c", Property: "embedding"},
			wantLen:  4,
			wantVals: embedding,
		},
		{
			name:    "extract missing property returns nil",
			expr:    &PropertyExpression{Variable: "c", Property: "nonexistent"},
			wantNil: true,
		},
		{
			name:    "extract from unbound variable returns nil",
			expr:    &PropertyExpression{Variable: "x", Property: "embedding"},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractValue(tt.expr, context)

			if tt.wantNil {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
				return
			}

			vec, ok := result.([]float32)
			if !ok {
				t.Fatalf("expected []float32, got %T", result)
			}

			if len(vec) != tt.wantLen {
				t.Errorf("expected length %d, got %d", tt.wantLen, len(vec))
			}

			for i, v := range tt.wantVals {
				if vec[i] != v {
					t.Errorf("vec[%d] = %f, want %f", i, vec[i], v)
				}
			}
		})
	}
}

func TestAggregationExtractValue_TypeVector(t *testing.T) {
	embedding := []float32{1.0, 2.0, 3.0}
	val := storage.VectorValue(embedding)

	computer := &AggregationComputer{}
	result := computer.ExtractValue(val)

	vec, ok := result.([]float32)
	if !ok {
		t.Fatalf("expected []float32, got %T", result)
	}

	if len(vec) != 3 {
		t.Errorf("expected length 3, got %d", len(vec))
	}

	for i, v := range embedding {
		if vec[i] != v {
			t.Errorf("vec[%d] = %f, want %f", i, vec[i], v)
		}
	}
}
