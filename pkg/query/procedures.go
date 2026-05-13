// Package query — procedure registry plumbing.
//
// C3.1 lands the registry SKELETON (this file) alongside CallOperator
// (physical_plan.go). At this point the registry is intentionally empty:
// CallOperator dispatches through procedureRegistry but every name resolves
// to "unknown procedure" until C6 registers the actual procedure bodies.
//
// C6 is gated on Decision 6 in `docs/NEXT_STEPS_2026-05-13.md` (S1↔algorithms
// storage-type wiring for algo.shortestPath). Once Decision 6 resolves, C6
// adds entries to procedureRegistry via either direct map assignment in this
// package or external RegisterProcedure calls from other packages.
//
// Why split C3.1 (this PR) and C6 (procedure bodies): the registry's TYPE
// surface (Procedure signature, RegisterProcedure exported func) is needed
// for CallOperator to compile, and CallOperator is needed for C4.1's q.Call
// planner block to compile. Bundling the procedure bodies would force C6's
// Decision 6 resolution before CallOperator can land. Splitting keeps the
// type-surface unblocked and isolates the policy-laden decision to C6.
package query

import (
	"context"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Procedure is the function signature for a Cypher procedure callable via
// `CALL procedure_name(args) YIELD items`. The storage.Storage interface
// argument is intentionally the S1-narrowed type; concrete-backend access
// (e.g. *storage.GraphStorage) is a per-procedure decision (see Decision 6).
type Procedure func(ctx context.Context, graph storage.Storage, tenantID string, args []any) ([]map[string]any, error)

// procedureRegistry is the dispatch table CallOperator looks up by procedure
// name. C3.1 ships it empty; C6 populates it.
var procedureRegistry = map[string]Procedure{}

// RegisterProcedure adds a procedure to the registry. Exported so external
// packages can register procedures (e.g. enterprise plugin loader, test
// fixtures) without requiring direct map access. Last-writer-wins on name
// collisions; no locking because registration is expected to happen at
// init/setup time, not concurrently with query execution.
func RegisterProcedure(name string, proc Procedure) {
	procedureRegistry[name] = proc
}
