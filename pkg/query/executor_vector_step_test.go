package query

import (
	"context"
	"fmt"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestVectorSearchStep_ThresholdFiltering(t *testing.T) {
	// Simulate HNSW returning 3 results with varying distances
	// Cosine distance: 0 = identical, 2 = opposite
	searchResults := []VectorSearchResult{
		{NodeID: 1, Distance: 0.1},  // similarity = 0.9
		{NodeID: 2, Distance: 0.5},  // similarity = 0.5
		{NodeID: 3, Distance: 0.05}, // similarity = 0.95
	}

	nodes := map[uint64]*storage.Node{
		1: {ID: 1, Labels: []string{"Concept"}, Properties: map[string]storage.Value{
			"name": storage.StringValue("Node A"),
		}},
		2: {ID: 2, Labels: []string{"Concept"}, Properties: map[string]storage.Value{
			"name": storage.StringValue("Node B"),
		}},
		3: {ID: 3, Labels: []string{"Concept"}, Properties: map[string]storage.Value{
			"name": storage.StringValue("Node C"),
		}},
	}

	step := &VectorSearchStep{
		variable:     "c",
		propertyName: "embedding",
		threshold:    0.8,
		labels:       []string{"Concept"},
		queryVectorParamName: "query_embedding",
		searchFn: func(prop string, q []float32, k, ef int) ([]VectorSearchResult, error) {
			return searchResults, nil
		},
		similarityFn: func(a, b []float32) (float64, error) {
			return 0, nil // not used in HNSW path
		},
		getNodeFn: func(nodeID uint64) (any, error) {
			if node, ok := nodes[nodeID]; ok {
				return node, nil
			}
			return nil, fmt.Errorf("node not found: %d", nodeID)
		},
		k:              100,
		ef:             50,
		distanceMetric: "cosine",
	}

	queryVec := []float32{1.0, 0.0, 0.0}
	execCtx := &ExecutionContext{
		context: context.Background(),
		results: []*BindingSet{
			{bindings: map[string]any{"$query_embedding": queryVec}},
		},
	}

	if err := step.Execute(execCtx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only nodes 1 (0.9) and 3 (0.95) should pass threshold 0.8
	if len(execCtx.results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(execCtx.results))
	}

	// Verify bindings contain the correct nodes
	for _, binding := range execCtx.results {
		node, ok := binding.bindings["c"].(*storage.Node)
		if !ok {
			t.Fatalf("expected *storage.Node, got %T", binding.bindings["c"])
		}
		if node.ID != 1 && node.ID != 3 {
			t.Errorf("unexpected node ID: %d", node.ID)
		}
	}
}

func TestVectorSearchStep_LabelFiltering(t *testing.T) {
	searchResults := []VectorSearchResult{
		{NodeID: 1, Distance: 0.1},
		{NodeID: 2, Distance: 0.1},
	}

	nodes := map[uint64]*storage.Node{
		1: {ID: 1, Labels: []string{"Concept"}, Properties: map[string]storage.Value{}},
		2: {ID: 2, Labels: []string{"Other"}, Properties: map[string]storage.Value{}},
	}

	step := &VectorSearchStep{
		variable:     "c",
		propertyName: "embedding",
		threshold:    0.5,
		labels:       []string{"Concept"},
		queryVectorParamName: "query_embedding",
		searchFn: func(prop string, q []float32, k, ef int) ([]VectorSearchResult, error) {
			return searchResults, nil
		},
		getNodeFn: func(nodeID uint64) (any, error) {
			if node, ok := nodes[nodeID]; ok {
				return node, nil
			}
			return nil, fmt.Errorf("node not found: %d", nodeID)
		},
		k:              100,
		ef:             50,
		distanceMetric: "cosine",
	}

	queryVec := []float32{1.0, 0.0}
	execCtx := &ExecutionContext{
		context: context.Background(),
		results: []*BindingSet{
			{bindings: map[string]any{"$query_embedding": queryVec}},
		},
	}

	if err := step.Execute(execCtx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only node 1 has label "Concept"
	if len(execCtx.results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(execCtx.results))
	}

	node := execCtx.results[0].bindings["c"].(*storage.Node)
	if node.ID != 1 {
		t.Errorf("expected node ID 1, got %d", node.ID)
	}
}

func TestVectorSearchStep_EmptyResults(t *testing.T) {
	step := &VectorSearchStep{
		variable:     "c",
		propertyName: "embedding",
		threshold:    0.99,
		queryVectorParamName: "q",
		searchFn: func(prop string, q []float32, k, ef int) ([]VectorSearchResult, error) {
			return []VectorSearchResult{}, nil
		},
		k:              100,
		ef:             50,
		distanceMetric: "cosine",
	}

	queryVec := []float32{1.0}
	execCtx := &ExecutionContext{
		context: context.Background(),
		results: []*BindingSet{
			{bindings: map[string]any{"$q": queryVec}},
		},
	}

	if err := step.Execute(execCtx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(execCtx.results) != 0 {
		t.Errorf("expected 0 results, got %d", len(execCtx.results))
	}
}

func TestVectorSearchStep_CarriesForwardBindings(t *testing.T) {
	searchResults := []VectorSearchResult{
		{NodeID: 1, Distance: 0.1},
	}

	nodes := map[uint64]*storage.Node{
		1: {ID: 1, Labels: []string{"Concept"}, Properties: map[string]storage.Value{}},
	}

	step := &VectorSearchStep{
		variable:     "c",
		propertyName: "embedding",
		threshold:    0.5,
		queryVectorParamName: "q",
		searchFn: func(prop string, q []float32, k, ef int) ([]VectorSearchResult, error) {
			return searchResults, nil
		},
		getNodeFn: func(nodeID uint64) (any, error) {
			if node, ok := nodes[nodeID]; ok {
				return node, nil
			}
			return nil, fmt.Errorf("node not found: %d", nodeID)
		},
		k:              100,
		ef:             50,
		distanceMetric: "cosine",
	}

	queryVec := []float32{1.0, 0.0}
	execCtx := &ExecutionContext{
		context: context.Background(),
		results: []*BindingSet{
			{bindings: map[string]any{
				"$q":           queryVec,
				"$other_param": "hello",
			}},
		},
	}

	if err := step.Execute(execCtx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(execCtx.results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(execCtx.results))
	}

	// Verify existing bindings are carried forward
	binding := execCtx.results[0]
	if binding.bindings["$other_param"] != "hello" {
		t.Errorf("expected $other_param to be carried forward")
	}
	if binding.bindings["$q"] == nil {
		t.Errorf("expected $q to be carried forward")
	}
	if _, ok := binding.bindings["c"].(*storage.Node); !ok {
		t.Errorf("expected 'c' to be bound to *storage.Node")
	}
}
