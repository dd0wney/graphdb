package storage

import (
	"errors"
	"math"
	"testing"
)

// #328: non-finite edge weights (±Inf/NaN) can't be WAL/snapshot-marshaled and
// were silently dropped (fire-and-forget WAL write) — the edge existed in memory
// but vanished on crash. These pin that every edge create/update path now
// REJECTS a non-finite weight up front (ErrInvalidEdgeWeight) with no partial
// state, instead of accepting a non-durable edge.

func nonFiniteWeights() []struct {
	name string
	w    float64
} {
	return []struct {
		name string
		w    float64
	}{
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
		{"NaN", math.NaN()},
	}
}

func TestCreateEdge_NonFiniteWeight_Rejected(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()
	const tenant = "acme"
	a, _ := gs.CreateNodeWithTenant(tenant, []string{"N"}, nil)
	b, _ := gs.CreateNodeWithTenant(tenant, []string{"N"}, nil)

	for _, tc := range nonFiniteWeights() {
		t.Run(tc.name, func(t *testing.T) {
			_, err := gs.CreateEdgeWithTenant(tenant, a.ID, b.ID, "T", nil, tc.w)
			if !errors.Is(err, ErrInvalidEdgeWeight) {
				t.Fatalf("CreateEdgeWithTenant(%s) err = %v, want ErrInvalidEdgeWeight", tc.name, err)
			}
		})
	}
	// No partial state: no edge was created by any rejected call.
	if n := gs.CountEdgesForTenant(tenant); n != 0 {
		t.Errorf("CountEdgesForTenant = %d after rejected creates, want 0 (partial apply)", n)
	}
	// A finite weight still works.
	if _, err := gs.CreateEdgeWithTenant(tenant, a.ID, b.ID, "T", nil, 1.5); err != nil {
		t.Fatalf("finite-weight create failed: %v", err)
	}
	if n := gs.CountEdgesForTenant(tenant); n != 1 {
		t.Errorf("CountEdgesForTenant = %d, want 1", n)
	}
}

func TestUpdateEdge_NonFiniteWeight_Rejected(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()
	const tenant = "acme"
	a, _ := gs.CreateNodeWithTenant(tenant, []string{"N"}, nil)
	b, _ := gs.CreateNodeWithTenant(tenant, []string{"N"}, nil)
	e, err := gs.CreateEdgeWithTenant(tenant, a.ID, b.ID, "T", nil, 1.0)
	if err != nil {
		t.Fatalf("create edge: %v", err)
	}

	for _, tc := range nonFiniteWeights() {
		t.Run(tc.name, func(t *testing.T) {
			w := tc.w
			err := gs.UpdateEdgeForTenant(e.ID, nil, &w, tenant)
			if !errors.Is(err, ErrInvalidEdgeWeight) {
				t.Fatalf("UpdateEdgeForTenant(%s) err = %v, want ErrInvalidEdgeWeight", tc.name, err)
			}
		})
	}
	// Weight unchanged after rejected updates.
	got, err := gs.GetEdgeForTenant(e.ID, tenant)
	if err != nil {
		t.Fatalf("get edge: %v", err)
	}
	if got.Weight != 1.0 {
		t.Errorf("weight = %v after rejected updates, want 1.0 unchanged", got.Weight)
	}
}

func TestUpsertEdge_NonFiniteWeight_Rejected(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()
	const tenant = "acme"
	a, _ := gs.CreateNodeWithTenant(tenant, []string{"N"}, nil)
	b, _ := gs.CreateNodeWithTenant(tenant, []string{"N"}, nil)

	if _, _, err := gs.UpsertEdgeWithTenant(tenant, a.ID, b.ID, "T", nil, math.Inf(1)); !errors.Is(err, ErrInvalidEdgeWeight) {
		t.Fatalf("UpsertEdgeWithTenant(+Inf) err = %v, want ErrInvalidEdgeWeight", err)
	}
	if n := gs.CountEdgesForTenant(tenant); n != 0 {
		t.Errorf("CountEdgesForTenant = %d after rejected upsert, want 0", n)
	}
}

func TestBatchCreateEdge_NonFiniteWeight_Rejected(t *testing.T) {
	gs, err := NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("NewGraphStorage: %v", err)
	}
	defer func() { _ = gs.Close() }()
	const tenant = "acme"
	a, _ := gs.CreateNodeWithTenant(tenant, []string{"N"}, nil)
	b, _ := gs.CreateNodeWithTenant(tenant, []string{"N"}, nil)

	batch := gs.BeginBatch()
	if _, err := batch.AddEdge(a.ID, b.ID, "T", nil, math.Inf(-1)); err != nil {
		t.Fatalf("AddEdge enqueue: %v", err)
	}
	if err := batch.Commit(); !errors.Is(err, ErrInvalidEdgeWeight) {
		t.Fatalf("batch Commit err = %v, want ErrInvalidEdgeWeight", err)
	}
	if n := gs.CountEdgesForTenant(tenant); n != 0 {
		t.Errorf("CountEdgesForTenant = %d after rejected batch, want 0", n)
	}
}
