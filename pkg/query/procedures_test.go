package query

import (
	"context"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
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

// TestNewProcedures_RegistryWired pins that the four Phase-A procedures
// land in procedureRegistry. Guards against future refactors clearing
// entries; mirrors the algo.shortestPath registration test.
func TestNewProcedures_RegistryWired(t *testing.T) {
	for _, name := range []string{
		"algo.kHop",
		"algo.nodeSimilarity",
		"algo.linkPrediction",
		"algo.pageRank",
	} {
		if _, ok := procedureRegistry[name]; !ok {
			t.Errorf("%s not in procedureRegistry", name)
		}
	}
}

// TestKHopProcedure_HappyPath: A→B→C chain, kHop from A with MaxHops=2
// reaches both B and C. Exercises the procedure body's full path
// including KHopNeighboursForTenant's tenant scoping.
func TestKHopProcedure_HappyPath(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const tenantID = "tenant-A"
	a, _ := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	b, _ := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	c, _ := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	if _, err := gs.CreateEdgeWithTenant(tenantID, a.ID, b.ID, "E", nil, 1.0); err != nil {
		t.Fatalf("edge a→b: %v", err)
	}
	if _, err := gs.CreateEdgeWithTenant(tenantID, b.ID, c.ID, "E", nil, 1.0); err != nil {
		t.Fatalf("edge b→c: %v", err)
	}

	results, err := kHopProcedure(context.Background(), gs, tenantID, []any{a.ID, int64(2)})
	if err != nil {
		t.Fatalf("procedure: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result row, got %d", len(results))
	}
	total, ok := results[0]["totalReachable"].(int)
	if !ok {
		t.Fatalf("totalReachable type: got %T", results[0]["totalReachable"])
	}
	if total != 2 {
		t.Errorf("totalReachable = %d, want 2 (b and c)", total)
	}
}

// TestKHopProcedure_ArgValidation: each row should produce a non-nil
// error from kHopProcedure.
func TestKHopProcedure_ArgValidation(t *testing.T) {
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
		{"sourceID wrong type", []any{"abc", int64(2)}},
		{"maxHops wrong type", []any{uint64(1), "abc"}},
		{"direction wrong type", []any{uint64(1), int64(2), 42}},
		{"direction unknown value", []any{uint64(1), int64(2), "diagonal"}},
		{"edgeTypes wrong type", []any{uint64(1), int64(2), "out", "not-a-list"}},
		{"edgeTypes list with non-string", []any{uint64(1), int64(2), "out", []any{"E", 5}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := kHopProcedure(context.Background(), gs, "tenant-A", tt.args); err == nil {
				t.Errorf("expected error for args=%v, got nil", tt.args)
			}
		})
	}
}

// TestNodeSimilarityProcedure_HappyPath: two source nodes sharing one
// common neighbor — Jaccard = 1/1 = 1.0 (each has only that one
// neighbor; intersection / union = 1 / 1).
func TestNodeSimilarityProcedure_HappyPath(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const tenantID = "tenant-A"
	a, _ := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	b, _ := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	shared, _ := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	if _, err := gs.CreateEdgeWithTenant(tenantID, a.ID, shared.ID, "E", nil, 1.0); err != nil {
		t.Fatalf("edge a→shared: %v", err)
	}
	if _, err := gs.CreateEdgeWithTenant(tenantID, b.ID, shared.ID, "E", nil, 1.0); err != nil {
		t.Fatalf("edge b→shared: %v", err)
	}

	results, err := nodeSimilarityProcedure(context.Background(), gs, tenantID, []any{a.ID, b.ID})
	if err != nil {
		t.Fatalf("procedure: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result row, got %d", len(results))
	}
	score, ok := results[0]["score"].(float64)
	if !ok {
		t.Fatalf("score type: got %T", results[0]["score"])
	}
	if score != 1.0 {
		t.Errorf("jaccard score = %f, want 1.0 (single shared neighbor, no others)", score)
	}
}

// TestNodeSimilarityProcedure_ArgValidation.
func TestNodeSimilarityProcedure_ArgValidation(t *testing.T) {
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
		{"nodeA wrong type", []any{"abc", uint64(2)}},
		{"metric wrong type", []any{uint64(1), uint64(2), 42}},
		{"metric unknown", []any{uint64(1), uint64(2), "dotProduct"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := nodeSimilarityProcedure(context.Background(), gs, "tenant-A", tt.args); err == nil {
				t.Errorf("expected error for args=%v, got nil", tt.args)
			}
		})
	}
}

// TestLinkPredictionProcedure_HappyPath: two source nodes sharing one
// common neighbor — Common Neighbours score = 1.
func TestLinkPredictionProcedure_HappyPath(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const tenantID = "tenant-A"
	a, _ := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	b, _ := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	shared, _ := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	if _, err := gs.CreateEdgeWithTenant(tenantID, a.ID, shared.ID, "E", nil, 1.0); err != nil {
		t.Fatalf("edge a→shared: %v", err)
	}
	if _, err := gs.CreateEdgeWithTenant(tenantID, b.ID, shared.ID, "E", nil, 1.0); err != nil {
		t.Fatalf("edge b→shared: %v", err)
	}

	results, err := linkPredictionProcedure(context.Background(), gs, tenantID, []any{a.ID, b.ID})
	if err != nil {
		t.Fatalf("procedure: %v", err)
	}
	score, ok := results[0]["score"].(float64)
	if !ok {
		t.Fatalf("score type: got %T", results[0]["score"])
	}
	if score != 1.0 {
		t.Errorf("commonNeighbours score = %f, want 1.0 (one shared neighbor)", score)
	}
}

// TestLinkPredictionProcedure_ArgValidation.
func TestLinkPredictionProcedure_ArgValidation(t *testing.T) {
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
		{"fromID wrong type", []any{"abc", uint64(2)}},
		{"method wrong type", []any{uint64(1), uint64(2), 42}},
		{"method unknown", []any{uint64(1), uint64(2), "katz"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := linkPredictionProcedure(context.Background(), gs, "tenant-A", tt.args); err == nil {
				t.Errorf("expected error for args=%v, got nil", tt.args)
			}
		})
	}
}

// TestPageRankProcedure_HappyPath: 3-node chain a→b→c. PageRank should
// return scores for all three nodes summing to ~1.0 (normalized).
func TestPageRankProcedure_HappyPath(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const tenantID = "tenant-A"
	a, _ := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	b, _ := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	c, _ := gs.CreateNodeWithTenant(tenantID, []string{"N"}, nil)
	if _, err := gs.CreateEdgeWithTenant(tenantID, a.ID, b.ID, "E", nil, 1.0); err != nil {
		t.Fatalf("edge a→b: %v", err)
	}
	if _, err := gs.CreateEdgeWithTenant(tenantID, b.ID, c.ID, "E", nil, 1.0); err != nil {
		t.Fatalf("edge b→c: %v", err)
	}

	results, err := pageRankProcedure(context.Background(), gs, tenantID, nil)
	if err != nil {
		t.Fatalf("procedure: %v", err)
	}
	scores, ok := results[0]["scores"].(map[uint64]float64)
	if !ok {
		t.Fatalf("scores type: got %T", results[0]["scores"])
	}
	if len(scores) != 3 {
		t.Errorf("score count = %d, want 3", len(scores))
	}
	total := 0.0
	for _, s := range scores {
		total += s
	}
	if total < 0.999 || total > 1.001 {
		t.Errorf("score sum = %f, want ~1.0 (normalized)", total)
	}
}

// TestPageRankProcedure_ArgValidation.
func TestPageRankProcedure_ArgValidation(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	tests := []struct {
		name string
		args []any
	}{
		{"damping wrong type", []any{"high"}},
		{"maxIterations wrong type", []any{0.85, "many"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := pageRankProcedure(context.Background(), gs, "tenant-A", tt.args); err == nil {
				t.Errorf("expected error for args=%v, got nil", tt.args)
			}
		})
	}
}

// TestCoerceToInt pins the integer-arg coercion contract.
func TestCoerceToInt(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		want   int
		wantOK bool
	}{
		{"int", 42, 42, true},
		{"int64", int64(42), 42, true},
		{"uint64", uint64(42), 42, true},
		{"float64 truncates", 42.9, 42, true},
		{"int negative passes (algorithm enforces bounds)", -1, -1, true},
		{"string", "abc", 0, false},
		{"nil", nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := coerceToInt(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("got = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestCoerceToFloat64 pins the real-number coercion contract.
func TestCoerceToFloat64(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		want   float64
		wantOK bool
	}{
		{"float64", 0.85, 0.85, true},
		{"int promotes", 1, 1.0, true},
		{"int64 promotes", int64(2), 2.0, true},
		{"uint64 promotes", uint64(3), 3.0, true},
		{"string", "high", 0, false},
		{"nil", nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := coerceToFloat64(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("got = %f, want %f", got, tt.want)
			}
		})
	}
}

// TestCoerceToStringSlice pins the string-list coercion contract,
// including the []any case that parser_expressions.parseListLiteral
// emits.
func TestCoerceToStringSlice(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		want   []string
		wantOK bool
	}{
		{"nil", nil, nil, true},
		{"empty []any", []any{}, []string{}, true},
		{"[]any of strings", []any{"A", "B"}, []string{"A", "B"}, true},
		{"[]string passthrough", []string{"X", "Y"}, []string{"X", "Y"}, true},
		{"[]any with non-string", []any{"A", 42}, nil, false},
		{"non-list", "scalar", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := coerceToStringSlice(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if len(got) != len(tt.want) {
				t.Errorf("len = %d, want %d", len(got), len(tt.want))
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
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
