package gnn

import (
	"context"
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/stretchr/testify/assert"
)

func TestMessagePass_E2E(t *testing.T) {
	dataDir, _ := os.MkdirTemp("", "gnn-test-*")
	defer os.RemoveAll(dataDir)

	graph, _ := storage.NewGraphStorage(dataDir)
	defer graph.Close()

	tenantID := "default"
	
	// Create nodes with features
	// n1 (v: [1, 0]) -> n2 (v: [0, 1])
	n1, _ := graph.CreateNodeWithTenant(tenantID, []string{"Node"}, map[string]storage.Value{
		"feat": storage.VectorValue([]float32{1.0, 0.0}),
	})
	n2, _ := graph.CreateNodeWithTenant(tenantID, []string{"Node"}, map[string]storage.Value{
		"feat": storage.VectorValue([]float32{0.0, 1.0}),
	})
	graph.CreateEdgeWithTenant(tenantID, n1.ID, n2.ID, "LINK", nil, 1.0)

	// Run MessagePass (Mean aggregation) on n1
	err := MessagePass(context.Background(), graph, tenantID, []uint64{n1.ID}, "feat", "feat_out", 1, AggMean)
	assert.NoError(t, err)

	// Verify n1.feat_out = mean([1,0], [0,1]) = [0.5, 0.5]
	err = graph.WithNodeRefForTenant(n1.ID, tenantID, func(node *storage.Node) error {
		val, ok := node.Properties["feat_out"]
		assert.True(t, ok)
		vec, err := val.AsVector()
		assert.NoError(t, err)
		assert.InDeltaSlice(t, []float32{0.5, 0.5}, vec, 1e-6)
		return nil
	})
	assert.NoError(t, err)
}
