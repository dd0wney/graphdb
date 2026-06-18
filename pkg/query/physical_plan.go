// Package query's physical_plan.go declares the Volcano-model physical
// operator interface and 16 concrete operator implementations (C3.0
// extraction).
//
// Status: PARTIAL. C3.0 lifts the operator types + execution logic from
// origin/archive/gemini-bulk-2026-05-13^3 with no consumers. The existing
// Step-based Executor (executor.go + executor_steps.go) keeps serving the
// /v1/cypher endpoint; this file's operators run only when wired by C4
// (planner) + C5 (parser additions) in later PRs.
//
// Deferred:
//   - CallOperator (CALL ... YIELD) — references procedureRegistry, which
//     lives in procedures.go (C6 territory). To preserve the per-PR
//     discipline, it lands alongside C6 rather than dragging C6 forward.
//   - Operator-level unit tests — deferred to C3.1 (mirroring the C1.0 +
//     C1.1 split that surfaced a real navigation bug in the btree archive).
//
// Each operator carries `otel.Tracer("query").Start(...)` spans on Open
// (and on Next where loop hot-paths warrant it) per the audit's S7
// verdict. The acceptance bar of "OTEL spans visible in pkg/telemetry/
// exporter integration test" cannot be met because pkg/telemetry does
// not yet exist in the tree; surface added so a future telemetry
// extraction can wire to it.
//
// Operator families split into sibling files:
//   - physical_ops_scan.go   — NodeScanOperator, IndexSeekOperator, ExpandOperator, OptionalMatchOperator
//   - physical_ops_mutate.go — CreateOperator, SetOperator, DeleteOperator, RemoveOperator, MergeOperator
//   - physical_ops_project.go — FilterOperator, ProjectOperator, UnwindOperator, UnionOperator, AggregateOperator
//   - physical_ops_join.go   — NestedLoopJoinOperator, HashJoinOperator
//   - physical_ops_call.go   — CallOperator
package query

import (
	"github.com/dd0wney/graphdb/pkg/storage"
)

// PhysicalOperator is the interface for physical query operators (Volcano model).
type PhysicalOperator interface {
	// Open initializes the operator and its children.
	Open(ctx *ExecutionContext) error

	// Next returns the next row (BindingSet) from the operator.
	// Returns (nil, nil) when the result set is exhausted.
	Next(ctx *ExecutionContext) (*BindingSet, error)

	// Close releases any resources held by the operator.
	Close(ctx *ExecutionContext) error
}

// compareStorageValue compares two storage.Value objects.
// Kept here (not in a family file) because it is shared by MergeOperator
// (physical_ops_mutate.go) and OptionalMatchOperator (physical_ops_scan.go).
func compareStorageValue(a, b storage.Value) bool {
	if a.Type != b.Type {
		return false
	}
	return string(a.Data) == string(b.Data) // Simplified
}
