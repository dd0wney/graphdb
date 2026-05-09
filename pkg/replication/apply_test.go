package replication

import (
	"errors"
	"strings"
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
	// returnErr is the error-injection knob for testing
	// ApplyWriteOperation's error-propagation contract. If set,
	// both Create methods record the call AND return this error —
	// the call is recorded so a test can distinguish "executor was
	// invoked and errored" from "executor was never invoked."
	returnErr error
}

func (e *recordingExecutor) CreateNodeWithTenant(tenantID string, labels []string, _ map[string]interface{}) (uint64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, recordedCall{
		method:   "CreateNodeWithTenant",
		tenantID: tenantID,
		labels:   labels,
	})
	if e.returnErr != nil {
		return 0, e.returnErr
	}
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
	if e.returnErr != nil {
		return 0, e.returnErr
	}
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

	if err := ApplyWriteOperation(executor, op); err != nil {
		t.Fatalf("ApplyWriteOperation: %v", err)
	}

	if op.TenantID != "" {
		t.Errorf("ApplyWriteOperation mutated caller op: TenantID=%q (want unchanged empty)", op.TenantID)
	}
	calls := executor.recorded()
	if len(calls) != 1 || calls[0].tenantID != "default" {
		t.Errorf("escape hatch should still apply on the local copy: got calls=%+v", calls)
	}
}

// TestApplyWriteOperation_PropagatesExecutorError pins that errors
// from the underlying executor are returned to the caller (not just
// logged-and-swallowed). The fail-closed gate's refusal is NOT an
// error — refusal is the documented success path. But a real
// executor failure (e.g., storage rejected a malformed op) must
// reach the caller so audit and unit tests can assert on dispatch
// success rather than only on observable state.
//
// Without error propagation, a future audit-row extension where
// (e.g.) create_edge references stale node IDs would fail with a
// confusing "got 0 edges" rather than the actual cause.
func TestApplyWriteOperation_PropagatesExecutorError(t *testing.T) {
	t.Setenv(replicationAllowEmptyTenantEnv, "")

	tests := []struct {
		name string
		op   WriteOperation
	}{
		{
			name: "create_node propagates",
			op: WriteOperation{
				TenantID: "tenant-A",
				Type:     "create_node",
				Labels:   []string{"User"},
			},
		},
		{
			name: "create_edge propagates",
			op: WriteOperation{
				TenantID:   "tenant-A",
				Type:       "create_edge",
				FromNodeID: 1,
				ToNodeID:   2,
				EdgeType:   "OWNS",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &recordingExecutor{returnErr: errors.New("storage rejected")}
			err := ApplyWriteOperation(executor, tt.op)
			if err == nil {
				t.Fatal("ApplyWriteOperation: want error from executor, got nil")
			}
			if !strings.Contains(err.Error(), "storage rejected") {
				t.Errorf("error doesn't wrap underlying: %v", err)
			}
			// And the call WAS attempted (proving the error path is
			// post-dispatch, not a pre-flight refusal).
			if got := executor.recorded(); len(got) != 1 {
				t.Errorf("want 1 recorded call, got %d", len(got))
			}
		})
	}
}

// TestApplyWriteOperation_RefusalReturnsNil pins that fail-closed
// refusal is NOT signaled as an error. Refusal is the gate's
// documented success path — the receive-loop has nothing to do with
// the signal, and surfacing it as an error would force the loop to
// distinguish refusal-vs-failure for no benefit. A future "errors.New
// for refusal" change would silently double-up logging.
func TestApplyWriteOperation_RefusalReturnsNil(t *testing.T) {
	t.Setenv(replicationAllowEmptyTenantEnv, "")

	executor := &recordingExecutor{}
	err := ApplyWriteOperation(executor, WriteOperation{
		TenantID: "", // refused by the gate
		Type:     "create_node",
		Labels:   []string{"User"},
	})
	if err != nil {
		t.Errorf("refusal should return nil (gate's success path), got: %v", err)
	}
	if got := executor.recorded(); len(got) != 0 {
		t.Errorf("refusal must not call executor: got %d call(s)", len(got))
	}
}
