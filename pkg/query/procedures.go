// Package query — procedure registry + procedure bodies for Cypher CALL ... YIELD.
//
// History:
//   - C3.1 (PR #175): registry SKELETON (Procedure type, empty map,
//     RegisterProcedure exported func) so CallOperator could compile.
//   - C6 (this file): registers the first real procedure, algo.shortestPath,
//     by wiring to pkg/algorithms.ShortestPathForTenant. Per Decision 6 = B
//     (NEXT_STEPS_2026-05-13.md, resolved 2026-05-13), the algorithm takes
//     storage.Storage (interface) so the procedure passes graph through
//     without type assertion.
//
// Future procedures:
//   - gnn.messagePass — skipped; pkg/gnn doesn't exist on OSS (Subset 🟢
//     audit note carries forward).
//   - llm.generate — dropped; pkg/intelligence doesn't exist on OSS
//     (Decision 4 retired 2026-05-13 by package absence).
//   - Backend-specific procedures may register via RegisterProcedure at init.
package query

import (
	"context"
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/algorithms"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Procedure is the function signature for a Cypher procedure callable via
// `CALL procedure_name(args) YIELD items`. The storage.Storage interface
// argument is intentionally the S1-narrowed type; algorithms exposed as
// procedures take the interface (per Decision 6 = B).
type Procedure func(ctx context.Context, graph storage.Storage, tenantID string, args []any) ([]map[string]any, error)

// procedureRegistry is the dispatch table CallOperator looks up by procedure
// name. Last-writer-wins on collisions; not concurrent-safe by design
// (registration is init-time, not runtime).
var procedureRegistry = map[string]Procedure{
	"algo.shortestPath": shortestPathProcedure,
}

// RegisterProcedure adds a procedure to the registry. Exported so external
// packages can register procedures (e.g. enterprise plugin loader, test
// fixtures) without requiring direct map access.
func RegisterProcedure(name string, proc Procedure) {
	procedureRegistry[name] = proc
}

// shortestPathProcedure exposes algorithms.ShortestPathForTenant as the
// CALL algo.shortestPath(startID, endID) YIELD path procedure.
//
// Returns a single result row: [{"path": []uint64}]. If no path exists
// the inner path slice is nil (algorithm returns (nil, nil) for "no path").
func shortestPathProcedure(_ context.Context, graph storage.Storage, tenantID string, args []any) ([]map[string]any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("algo.shortestPath requires 2 arguments (start, end node IDs); got %d", len(args))
	}

	startID, ok := coerceToUint64(args[0])
	if !ok {
		return nil, fmt.Errorf("algo.shortestPath: start argument must be a numeric node ID; got %T", args[0])
	}
	endID, ok := coerceToUint64(args[1])
	if !ok {
		return nil, fmt.Errorf("algo.shortestPath: end argument must be a numeric node ID; got %T", args[1])
	}

	path, err := algorithms.ShortestPathForTenant(graph, startID, endID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("algo.shortestPath: %w", err)
	}

	return []map[string]any{{"path": path}}, nil
}

// coerceToUint64 accepts the numeric types Cypher's parser can produce for
// node-ID arguments (int64 from integer literals, uint64 from raw bindings,
// float64 from arithmetic). Returns (0, false) for anything that isn't a
// non-negative number.
func coerceToUint64(v any) (uint64, bool) {
	switch n := v.(type) {
	case uint64:
		return n, true
	case int64:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case float64:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	default:
		return 0, false
	}
}
