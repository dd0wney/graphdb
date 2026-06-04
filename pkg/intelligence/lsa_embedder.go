package intelligence

import (
	"context"
	"fmt"

	"github.com/dd0wney/graphdb/pkg/search"
)

// LSAEmbedder implements Embedder by routing each Embed call to the
// caller's tenant's LSA index via search.TenantLSAIndexes. This is the
// canonical first-party Embedder implementation (R2.4 / S11 spike §6).
//
// LSA is deterministic given a fixed corpus + seed (see pkg/search/lsa.go's
// "Seed=42 is a feature" framing), which makes LSAEmbedder a good baseline
// for embedding-space consistency between the read path (existing
// /v1/embeddings and /hybrid-search handlers) and the new write-path
// auto-embedder. Enterprise plugins providing zero-config or larger-scale
// embeddings (ONNX-bundled, hosted-API) implement the same Embedder
// interface and replace LSAEmbedder at startup time via the same
// AddObserver / wire-up surface R2.5 will introduce.
//
// Operational note: LSAEmbedder.Embed returns ErrNoIndexForTenant when
// the tenant has not had an LSA index built yet. This is NOT a transient
// error — retrying without administrative action (POST
// /hybrid-search/lsa-index) returns the same result. The R2.5
// AutoEmbedObserver will log + meter + drop the embedding task in that
// case rather than retrying.
type LSAEmbedder struct {
	indexes *search.TenantLSAIndexes
}

// NewLSAEmbedder constructs an Embedder backed by the given per-tenant
// LSA registry. The registry MUST be non-nil; pass an empty
// search.NewTenantLSAIndexes() if no tenants have indexes yet (Embed will
// return ErrNoIndexForTenant for every call until tenants are registered
// via TenantLSAIndexes.Set).
func NewLSAEmbedder(indexes *search.TenantLSAIndexes) *LSAEmbedder {
	return &LSAEmbedder{indexes: indexes}
}

// Embed projects text into tenantID's LSA latent space.
//
// Returns ErrNoIndexForTenant when no LSA index has been built for
// tenantID. Returns an error (from search.LSAIndex.FoldQuery) when the
// query is out-of-vocabulary for the tenant's index or projects to the
// zero vector. Honors ctx.Err() — if the caller cancels before Embed
// starts the projection, returns the ctx error without touching the
// index.
//
// On success, the returned []float32 has length equal to the tenant's
// LSAIndex.Dimensions() (200 in production via DefaultLSAConfig, smaller
// in tests). It is L2-normalized — FoldQuery guarantees this for any
// successful return.
func (e *LSAEmbedder) Embed(ctx context.Context, tenantID string, text string) ([]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	idx := e.indexes.Get(tenantID)
	if idx == nil {
		return nil, ErrNoIndexForTenant{TenantID: tenantID}
	}
	vec, _, err := idx.FoldQuery(text)
	if err != nil {
		return nil, fmt.Errorf("lsa embed: %w", err)
	}
	return vec, nil
}
