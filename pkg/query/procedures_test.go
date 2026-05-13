package query

import (
	"context"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestShortestPathProcedure_HappyPath constructs a 3-node chain (a→b→c),
// invokes the procedure body directly with start=a, end=c, and asserts
// the returned path is [a, b, c]. This closes the C6 acceptance bar:
// "procedure registry has only real implementations; mock fallbacks
// return errors instead of silently lying" — by demonstrating that the
// registered algo.shortestPath produces real path data, not a stub.
func TestShortestPathProcedure_HappyPath(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const tenantID = "tenant-A"

	a, err := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	if err != nil {
		t.Fatalf("create b: %v", err)
	}
	c, err := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	if err != nil {
		t.Fatalf("create c: %v", err)
	}

	if _, err := gs.CreateEdgeWithTenant(tenantID, a.ID, b.ID, "E", nil, 1.0); err != nil {
		t.Fatalf("edge a→b: %v", err)
	}
	if _, err := gs.CreateEdgeWithTenant(tenantID, b.ID, c.ID, "E", nil, 1.0); err != nil {
		t.Fatalf("edge b→c: %v", err)
	}

	results, err := shortestPathProcedure(context.Background(), gs, tenantID, []any{a.ID, c.ID})
	if err != nil {
		t.Fatalf("procedure: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result row, got %d", len(results))
	}

	path, ok := results[0]["path"].([]uint64)
	if !ok {
		t.Fatalf("expected path to be []uint64, got %T", results[0]["path"])
	}

	want := []uint64{a.ID, b.ID, c.ID}
	if len(path) != len(want) {
		t.Fatalf("path length = %d, want %d (path=%v)", len(path), len(want), path)
	}
	for i := range want {
		if path[i] != want[i] {
			t.Errorf("path[%d] = %d, want %d", i, path[i], want[i])
		}
	}
}

// TestShortestPathProcedure_RegistryWired confirms the registry literal
// at procedures.go contains the algo.shortestPath entry — guards against
// future refactors accidentally clearing the registration. Tested via the
// public registry lookup CallOperator would use.
func TestShortestPathProcedure_RegistryWired(t *testing.T) {
	if _, ok := procedureRegistry["algo.shortestPath"]; !ok {
		t.Fatal("algo.shortestPath not in procedureRegistry; C6 registration regressed")
	}
}

// TestShortestPathProcedure_ArgValidation exercises error paths so future
// signature changes surface as test failures rather than silent runtime
// errors at the Cypher boundary.
func TestShortestPathProcedure_ArgValidation(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	tests := []struct {
		name string
		args []any
	}{
		{"no args", []any{}},
		{"single arg", []any{uint64(1)}},
		{"wrong type — string", []any{"abc", uint64(2)}},
		{"wrong type — negative int", []any{int64(-1), uint64(2)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := shortestPathProcedure(context.Background(), gs, "tenant-A", tt.args)
			if err == nil {
				t.Errorf("expected error for args=%v, got nil", tt.args)
			}
		})
	}
}

// TestCoerceToUint64 pins the type-coercion contract — the parser
// produces int64 from integer literals, but raw bindings can hold uint64
// or float64. All three plus int should coerce; negatives and non-numerics
// should fail.
func TestCoerceToUint64(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		want   uint64
		wantOK bool
	}{
		{"uint64", uint64(42), 42, true},
		{"int64 positive", int64(42), 42, true},
		{"int64 zero", int64(0), 0, true},
		{"int64 negative", int64(-1), 0, false},
		{"int positive", 42, 42, true},
		{"int negative", -1, 0, false},
		{"float64 positive", 42.0, 42, true},
		{"float64 negative", -1.0, 0, false},
		{"string", "abc", 0, false},
		{"nil", nil, 0, false},
		{"bool", true, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := coerceToUint64(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("got = %d, want %d", got, tt.want)
			}
		})
	}
}
