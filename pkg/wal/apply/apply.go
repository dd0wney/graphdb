// Package apply provides the structured-write-operation primitive and the
// fail-closed tenant gate that audits its application against storage.
//
// History (2026-05-12): this package was lifted from pkg/replication/ when
// A8.1 (Option B) deleted the standalone replication library. The
// receiver/socket machinery did not survive the deletion — that infrastructure
// was NNG/TCP-shaped fossilization with no `cmd/server`-native rebuild path.
// What did survive is what this package contains: the WriteOperation
// wire-format struct, the WriteExecutor abstraction, and ApplyWriteOperation
// — the fail-closed apply gate that A8 (the parent audit) added to refuse
// empty TenantID. Those primitives are still load-bearing for the
// audit-regression suite and would be re-derived from scratch by any future
// cmd/server-native replication rebuild.
//
// See:
//   - docs/A8_REPLICATION_TENANCY_DESIGN.md §Q1 (TenantID on the wire).
//   - docs/A8_1_SPIKE_2026-05-12.md §6 (Decision B).
//   - pkg/api/audit_regression_test.go A8 row (drives ApplyWriteOperation
//     end-to-end against a real storage.Storage).
package apply

import (
	"fmt"
	"log"
	"os"

	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// WriteOperation is a structured representation of a graph mutation in
// transit between a write-source and a write-applier. JSON-serializable;
// tenant-required on the wire.
//
// Audit A8 (2026-05-09): TenantID is required on every WriteOperation.
// The apply path (ApplyWriteOperation) fails closed when TenantID is empty
// — silently defaulting to the default tenant on the wire was the exact
// silent-default class A8 closes (in-house precedent: the JWT_SECRET
// fail-closed fix in pkg/api/server_init.go). The JSON tag deliberately
// omits `omitempty` so an explicit empty value reaches the receiver
// unmodified, which the apply path can log and refuse — rather than
// appearing identical to a missing field.
type WriteOperation struct {
	TenantID   string                 `json:"tenant_id"`
	Type       string                 `json:"type"` // "create_node", "create_edge"
	Labels     []string               `json:"labels,omitempty"`
	Properties map[string]interface{} `json:"properties,omitempty"`
	FromNodeID uint64                 `json:"from_node_id,omitempty"`
	ToNodeID   uint64                 `json:"to_node_id,omitempty"`
	EdgeType   string                 `json:"edge_type,omitempty"`
	Weight     float64                `json:"weight,omitempty"`
}

// WriteExecutor is the storage-write surface ApplyWriteOperation requires.
// interface{} for properties keeps the abstraction decoupled from
// storage.Value's concrete shape — the audit-regression suite's adapter
// converts at the boundary.
//
// Audit A8 (2026-05-09): methods carry an explicit tenantID. The caller
// (ApplyWriteOperation) refuses empty tenantID by default — see the
// fail-closed gate there. Concrete implementers can assume tenantID is
// non-empty unless the AllowEmptyTenantEnv escape hatch is set (in which
// case the apply path defaults it to "default" before calling these
// methods).
type WriteExecutor interface {
	CreateNodeWithTenant(tenantID string, labels []string, properties map[string]interface{}) (uint64, error)
	CreateEdgeWithTenant(tenantID string, from, to uint64, edgeType string, properties map[string]interface{}, weight float64) (uint64, error)
}

// AllowEmptyTenantEnv opts a deployment out of the default fail-closed
// behavior on empty WriteOperation.TenantID. See doc on ApplyWriteOperation
// — same shape as the JWT_SECRET fail-closed pattern in
// pkg/api/server_init.go.
const AllowEmptyTenantEnv = "REPLICATION_ALLOW_EMPTY_TENANT"

// ApplyWriteOperation applies a single WriteOperation against executor,
// with a fail-closed gate on empty TenantID.
//
// Audit A8 (2026-05-09): empty op.TenantID is refused by default — silent
// default-tenant routing is the exact pattern this audit closes (in-house
// precedent: the JWT_SECRET fail-closed fix in pkg/api/server_init.go:74-77).
//
// The AllowEmptyTenantEnv=1 env var opts back into the legacy behavior
// (default empty to "default") for one-shot migration scenarios — e.g.,
// draining writes from an unmigrated sender. Off by default; document and
// remove once all senders populate TenantID.
//
// op is taken by value so the escape-hatch's TenantID rewrite never mutates
// caller state. Callers can reuse a single op struct across multiple apply
// calls without surprise.
//
// Returns nil on successful dispatch AND on fail-closed refusal — refusal
// is the documented success path of the gate, signaled by the log.Printf
// above. A non-nil return means the executor itself rejected the op (e.g.,
// storage error); callers driving the apply path from tests should treat
// a non-nil return as a fatal signal rather than relying on observable-state
// assertions alone.
func ApplyWriteOperation(executor WriteExecutor, op WriteOperation) error {
	if op.TenantID == "" {
		if os.Getenv(AllowEmptyTenantEnv) != "1" {
			log.Printf("apply: refusing %q with empty tenant_id; "+
				"set %s=1 to opt into legacy default-tenant behavior",
				op.Type, AllowEmptyTenantEnv)
			return nil
		}
		op.TenantID = tenant.DefaultTenantID
	}

	switch op.Type {
	case "create_node":
		if _, err := executor.CreateNodeWithTenant(op.TenantID, op.Labels, op.Properties); err != nil {
			log.Printf("Failed to create node: %v", err)
			return fmt.Errorf("create_node: %w", err)
		}
	case "create_edge":
		if _, err := executor.CreateEdgeWithTenant(op.TenantID, op.FromNodeID, op.ToNodeID, op.EdgeType, op.Properties, op.Weight); err != nil {
			log.Printf("Failed to create edge: %v", err)
			return fmt.Errorf("create_edge: %w", err)
		}
	default:
		log.Printf("Unknown write operation type: %s", op.Type)
	}
	return nil
}
