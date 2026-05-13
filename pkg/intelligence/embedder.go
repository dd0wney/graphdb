package intelligence

import (
	"context"
	"fmt"
)

// Embedder computes a dense vector embedding for a text input, scoped to a
// specific tenant.
//
// Tenant scoping is the caller's responsibility — the embedder may use the
// tenantID to select a per-tenant model, corpus, or configuration. The
// canonical implementation in this package (R2.4: LSAEmbedder) routes by
// tenantID to a per-tenant LSA index registered via pkg/search; enterprise
// plugins may implement different backends (ONNX-bundled, hosted-API) that
// satisfy the same interface (Decision 3, tier-based: open-core resolution
// in NEXT_STEPS_2026-05-14.md).
//
// Contract:
//
//   - Embed MUST return a non-nil error if it cannot produce a real
//     embedding. It MUST NEVER return a mock or placeholder vector
//     alongside a nil error — silent fakery is the failure mode this
//     interface exists to structurally prevent. See S11 spike §1.1 for
//     the archive's `mockEmbedding` pattern this design rejects.
//
//   - The returned slice length is consistent with the embedding space
//     the caller has configured (e.g., the HNSW index dimensions). A
//     dimension mismatch is a configuration error and should surface as
//     a typed error from Embed, not be papered over.
//
//   - Implementations must be safe for concurrent calls — the worker
//     pool in worker.go dispatches multiple Tasks in parallel, each of
//     which may call Embed concurrently.
//
//   - Implementations should respect ctx.Done() for cancellation; the
//     worker pool propagates Shutdown cancellation through Task.Execute's
//     ctx, and embedding backends with expensive operations (network
//     calls, large matrix multiplies) should bail when ctx is cancelled.
type Embedder interface {
	Embed(ctx context.Context, tenantID string, text string) ([]float32, error)
}

// ErrNoIndexForTenant is returned by Embedder implementations when the
// target tenant has no configured backend state to embed against (e.g.,
// no LSA index built, no per-tenant model loaded).
//
// This is NOT a transient error — retrying the same Embed call without
// administrative action will return the same result. Callers (typically
// the AutoEmbedObserver in R2.5) should log + meter + drop the task
// rather than retry.
//
// The error message is intentionally generic to fit any Embedder backend.
// Implementations that want backend-specific guidance (e.g., LSAEmbedder
// pointing the operator at `POST /hybrid-search/lsa-index`) should log
// that separately or wrap this error with %w + their own context. The
// errors.Is chain still resolves to ErrNoIndexForTenant for callers that
// branch on this condition.
type ErrNoIndexForTenant struct {
	TenantID string
}

// Error implements the error interface.
func (e ErrNoIndexForTenant) Error() string {
	return fmt.Sprintf("embedder: no index configured for tenant %q", e.TenantID)
}
