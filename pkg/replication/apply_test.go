package replication

import (
	"sync"
	"testing"
)

// Audit A8 #3 (2026-05-09): tests the WriteReceiver apply path's
// fail-closed behavior on empty TenantID, plus the tenant-flow-through
// for populated TenantID.
//
// The load-bearing test is the fail-closed half: the assertion is
// "mock executor recorded zero calls," not "no error returned." A
// future regression where rejection-then-call slips in (e.g., a
// refactor that misorders the guard and the dispatch) must fail this
// test. "didn't error" assertions would pass through that bug.

// recordingExecutor is a test-only WriteExecutor that records every
// call it receives. The recordedCall struct captures enough to
// distinguish create_node vs create_edge invocations and verify the
// tenantID flowed through. Concurrent-safe so the test can run
// without serializing on test order.
type recordedCall struct {
	method   string // "CreateNodeWithTenant" or "CreateEdgeWithTenant"
	tenantID string
	labels   []string
	from     uint64
	to       uint64
	edgeType string
}

type recordingExecutor struct {
	mu    sync.Mutex
	calls []recordedCall
}

func (e *recordingExecutor) CreateNodeWithTenant(tenantID string, labels []string, _ map[string]interface{}) (uint64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, recordedCall{
		method:   "CreateNodeWithTenant",
		tenantID: tenantID,
		labels:   labels,
	})
	return uint64(len(e.calls)), nil // synthetic ID
}

func (e *recordingExecutor) CreateEdgeWithTenant(tenantID string, from, to uint64, edgeType string, _ map[string]interface{}, _ float64) (uint64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, recordedCall{
		method:   "CreateEdgeWithTenant",
		tenantID: tenantID,
		from:     from,
		to:       to,
		edgeType: edgeType,
	})
	return uint64(len(e.calls)), nil
}

func (e *recordingExecutor) recorded() []recordedCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]recordedCall, len(e.calls))
	copy(out, e.calls)
	return out
}

// TestExecuteWrite_FailsClosedOnEmptyTenantID is the security gate.
// An incoming WriteOperation with empty TenantID — whether from a
// buggy/old sender or a malicious payload — must NOT reach the
// executor. The audit precedent is the JWT_SECRET fix in
// pkg/api/server_init.go: silent default-on-empty was the exact
// pattern this audit cycle exists to close.
//
// The assertion is recorded-calls-equals-zero, NOT "no error" — a
// future regression where rejection-then-call slips in must fail
// here. We pin REPLICATION_ALLOW_EMPTY_TENANT to "" via t.Setenv:
// against the current `!= "1"` gate the empty string is equivalent
// to unset (both fail closed). If a future change relaxes the gate
// to `!= ""`, this test would silently regress; keep the gate's
// comparison intentional.
func TestExecuteWrite_FailsClosedOnEmptyTenantID(t *testing.T) {
	tests := []struct {
		name string
		op   WriteOperation
	}{
		{
			name: "create_node refused",
			op: WriteOperation{
				TenantID: "", // the smoking gun
				Type:     "create_node",
				Labels:   []string{"User"},
			},
		},
		{
			name: "create_edge refused",
			op: WriteOperation{
				TenantID:   "",
				Type:       "create_edge",
				FromNodeID: 1,
				ToNodeID:   2,
				EdgeType:   "OWNS",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Ensure the env-var escape hatch is OFF for this test —
			// the *default* behavior must be fail-closed. Setenv with
			// the helper handles cleanup.
			t.Setenv(replicationAllowEmptyTenantEnv, "")

			executor := &recordingExecutor{}
			receiver := &WriteReceiver{executor: executor}

			receiver.executeWrite(&tt.op)

			calls := executor.recorded()
			if len(calls) != 0 {
				t.Errorf("fail-closed gate breached: executor received %d call(s) for empty tenant: %+v", len(calls), calls)
			}
		})
	}
}

// TestExecuteWrite_PopulatedTenantIDFlowsThrough pins the happy path:
// a WriteOperation with non-empty TenantID reaches the executor with
// the correct tenant. Without this, fail-closed could regress to
// "fail always" without anything noticing.
func TestExecuteWrite_PopulatedTenantIDFlowsThrough(t *testing.T) {
	t.Setenv(replicationAllowEmptyTenantEnv, "")

	executor := &recordingExecutor{}
	receiver := &WriteReceiver{executor: executor}

	receiver.executeWrite(&WriteOperation{
		TenantID: "tenant-A",
		Type:     "create_node",
		Labels:   []string{"User"},
	})
	receiver.executeWrite(&WriteOperation{
		TenantID:   "tenant-B",
		Type:       "create_edge",
		FromNodeID: 10,
		ToNodeID:   20,
		EdgeType:   "OWNS",
	})

	calls := executor.recorded()
	if len(calls) != 2 {
		t.Fatalf("want 2 recorded calls, got %d: %+v", len(calls), calls)
	}
	if calls[0].method != "CreateNodeWithTenant" || calls[0].tenantID != "tenant-A" {
		t.Errorf("call[0]: want CreateNodeWithTenant(tenant-A, ...), got %+v", calls[0])
	}
	if calls[1].method != "CreateEdgeWithTenant" || calls[1].tenantID != "tenant-B" {
		t.Errorf("call[1]: want CreateEdgeWithTenant(tenant-B, ...), got %+v", calls[1])
	}
}

// TestExecuteWrite_EscapeHatchOptsIntoLegacyDefault exercises the
// REPLICATION_ALLOW_EMPTY_TENANT=1 escape hatch from the spike's Q3.
// With the env var set, an empty TenantID is rewritten to "default"
// before dispatch — the old (insecure) behavior, made explicit and
// opt-in for one-shot migration scenarios.
//
// This test exists so a future change to the gate's wording (or its
// removal) is intentional. If the escape hatch is dropped, this test
// is the canary that flags it.
func TestExecuteWrite_EscapeHatchOptsIntoLegacyDefault(t *testing.T) {
	t.Setenv(replicationAllowEmptyTenantEnv, "1")

	executor := &recordingExecutor{}
	receiver := &WriteReceiver{executor: executor}

	receiver.executeWrite(&WriteOperation{
		TenantID: "", // legacy/migration shape
		Type:     "create_node",
		Labels:   []string{"Doc"},
	})

	calls := executor.recorded()
	if len(calls) != 1 {
		t.Fatalf("want 1 recorded call (escape hatch active), got %d", len(calls))
	}
	if calls[0].tenantID != "default" {
		t.Errorf("escape hatch should rewrite empty to %q, got %q", "default", calls[0].tenantID)
	}
}

// TestApplyWriteOperation_DoesNotMutateCallerOp pins the by-value
// contract on ApplyWriteOperation. The escape-hatch path rewrites
// empty TenantID to "default" before dispatch — but only on the
// function's local copy. A future refactor that switches the
// signature to *WriteOperation "for symmetry with executeWrite"
// would silently re-introduce caller-visible mutation; this test is
// the canary.
func TestApplyWriteOperation_DoesNotMutateCallerOp(t *testing.T) {
	t.Setenv(replicationAllowEmptyTenantEnv, "1")

	executor := &recordingExecutor{}
	op := WriteOperation{
		TenantID: "",
		Type:     "create_node",
		Labels:   []string{"Doc"},
	}

	ApplyWriteOperation(executor, op)

	if op.TenantID != "" {
		t.Errorf("ApplyWriteOperation mutated caller op: TenantID=%q (want unchanged empty)", op.TenantID)
	}
	calls := executor.recorded()
	if len(calls) != 1 || calls[0].tenantID != "default" {
		t.Errorf("escape hatch should still apply on the local copy: got calls=%+v", calls)
	}
}
