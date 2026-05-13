package intelligence

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// AutoEmbedObserver is the bridge between pkg/storage's NodeObserver
// notifications (R2.1) and the embedder pipeline. On a node-creation
// notification, it checks each registered EmbeddingPolicy; for every
// match it submits an autoEmbedTask to the worker Pool (R2.2) which
// computes an embedding via the configured Embedder (R2.3/R2.4) and
// writes it back to the node as a vector-typed property.
//
// Lifecycle:
//
//   - OnNodeCreated: dispatches one task per matching policy.
//   - OnNodeUpdated: no-op in R2.5a. Activating update-driven embedding
//     requires a re-entry guard (spike §7.2) — writing the embedding
//     back would itself fire OnNodeUpdated and loop. The guard needs
//     ctx-passing storage methods (a separate track) or a sentinel
//     property key (leaks internal state); both are deliberately
//     deferred. Until then, OnNodeUpdated stays a no-op so users who
//     mutate a node's source text must currently delete + recreate
//     (or manually re-embed via /v1/embeddings) to refresh the vector.
//   - OnNodeDeleted: no-op. The node's vector index entries are removed
//     in pkg/storage.RemoveNodeFromVectorIndexes (R1.2's tenant-aware
//     path), which runs as part of DeleteNode before the observer
//     dispatch.
//
// Wiring:
//
// AutoEmbedObserver is registered on a *storage.GraphStorage at startup
// via gs.AddObserver. R2.5b adds the env-driven wiring block in
// pkg/api/server_init.go that constructs the observer and registers it
// when GRAPHDB_AUTO_EMBED_* env vars are configured. Until R2.5b lands,
// callers wire AutoEmbedObserver manually in their own server bootstrap.
//
// Pool ownership:
//
// The Pool is a constructor argument, not constructed internally. Pool
// lifecycle is the caller's responsibility — multiple observers (or
// future intelligence consumers) may share a single pool, and Shutdown
// belongs at the same layer that owns server-lifecycle cleanup.
type AutoEmbedObserver struct {
	writer   nodeWriter
	embedder Embedder
	pool     *Pool
	policies []EmbeddingPolicy
}

// EmbeddingPolicy describes one auto-embed rule: when a node with Label
// is created and has SourceProperty set as a string, compute an embedding
// of that text via the observer's Embedder and write it to
// TargetProperty as a VectorValue.
//
// All three fields are required; constructor validation rejects empty
// values to surface configuration bugs at startup rather than at runtime.
//
// Multiple policies may be registered. A single node creation triggers
// one task per matching policy, dispatched in registration order.
type EmbeddingPolicy struct {
	// Label is the node label this policy applies to (exact match). A
	// node with multiple labels matches if any label equals Label.
	Label string

	// SourceProperty is the property key containing the text to embed.
	// Must be present on the node AND be a string-typed Value; nodes
	// without it or with non-string values are silently skipped.
	SourceProperty string

	// TargetProperty is the property key the resulting embedding is
	// written to as a VectorValue. If the node already has this property
	// set at the time of OnNodeCreated, the observer skips the writeback
	// to preserve the user-provided value.
	TargetProperty string
}

// nodeWriter is the storage subset AutoEmbedObserver needs for writeback.
// *storage.GraphStorage satisfies this via its UpdateNodeForTenant method.
// Defined as a narrow consumer-side interface to keep tests cheap
// (fakeWriter implements one method) and to insulate observer logic from
// the broader storage.Storage surface evolution.
type nodeWriter interface {
	UpdateNodeForTenant(nodeID uint64, properties map[string]storage.Value, tenantID string) error
}

// NewAutoEmbedObserver constructs an AutoEmbedObserver with the given
// dependencies and policies.
//
// Validation:
//   - writer, embedder, pool must be non-nil.
//   - policies may be empty (the observer becomes a no-op).
//   - Each policy must have non-empty Label, SourceProperty, TargetProperty.
//   - SourceProperty != TargetProperty within a single policy (would
//     overwrite the input).
//
// Returns an error on any validation failure; this surfaces configuration
// bugs at startup rather than silently at runtime.
func NewAutoEmbedObserver(writer nodeWriter, embedder Embedder, pool *Pool, policies []EmbeddingPolicy) (*AutoEmbedObserver, error) {
	if writer == nil {
		return nil, fmt.Errorf("auto-embed observer: writer must not be nil")
	}
	if embedder == nil {
		return nil, fmt.Errorf("auto-embed observer: embedder must not be nil")
	}
	if pool == nil {
		return nil, fmt.Errorf("auto-embed observer: pool must not be nil")
	}
	for i, p := range policies {
		if p.Label == "" {
			return nil, fmt.Errorf("auto-embed observer: policies[%d].Label must not be empty", i)
		}
		if p.SourceProperty == "" {
			return nil, fmt.Errorf("auto-embed observer: policies[%d].SourceProperty must not be empty", i)
		}
		if p.TargetProperty == "" {
			return nil, fmt.Errorf("auto-embed observer: policies[%d].TargetProperty must not be empty", i)
		}
		if p.SourceProperty == p.TargetProperty {
			return nil, fmt.Errorf("auto-embed observer: policies[%d]: SourceProperty and TargetProperty must differ (both %q)", i, p.SourceProperty)
		}
	}
	return &AutoEmbedObserver{
		writer:   writer,
		embedder: embedder,
		pool:     pool,
		policies: policies,
	}, nil
}

// OnNodeCreated dispatches one autoEmbedTask per matching policy. The
// node is a clone snapshot supplied by pkg/storage's notify path (R2.1
// guarantees this), safe to retain inside the task closure.
func (o *AutoEmbedObserver) OnNodeCreated(ctx context.Context, node *storage.Node) {
	for _, policy := range o.policies {
		if !nodeMatchesPolicy(node, policy) {
			continue
		}
		task := &autoEmbedTask{
			writer:   o.writer,
			embedder: o.embedder,
			node:     node,
			policy:   policy,
		}
		o.pool.Submit(ctx, task)
	}
}

// OnNodeUpdated is intentionally a no-op in R2.5a.
//
// Activating update-driven re-embedding requires a re-entry guard (S11
// spike §7.2): the observer's own writeback would fire OnNodeUpdated and
// loop. The two guard options (ctx-value sentinel, internal property
// sentinel) each carry separate-track cost: ctx-passing storage methods
// don't exist yet; sentinel properties leak internal state. Until that's
// resolved, users who mutate a node's source text must delete+recreate
// (or call /v1/embeddings manually) to refresh the vector.
//
// TODO(R2.x): when update-driven embedding activates, add the re-entry
// guard before this method does anything observable.
func (o *AutoEmbedObserver) OnNodeUpdated(_ context.Context, _ *storage.Node, _ *storage.Node) {
}

// OnNodeDeleted is a no-op: vector index entries are removed by
// pkg/storage.RemoveNodeFromVectorIndexes (R1.2) before the observer
// dispatch runs. The observer has nothing to clean up.
func (o *AutoEmbedObserver) OnNodeDeleted(_ context.Context, _ uint64, _ string) {
}

// nodeMatchesPolicy reports whether node carries policy.Label among its
// labels. Multi-label nodes match any of their labels equal to the
// policy's Label (exact match, not glob/regex).
func nodeMatchesPolicy(node *storage.Node, policy EmbeddingPolicy) bool {
	for _, l := range node.Labels {
		if l == policy.Label {
			return true
		}
	}
	return false
}

// autoEmbedTask is the unit of async work dispatched to Pool for a single
// (node, policy) pair. Implements intelligence.Task.
type autoEmbedTask struct {
	writer   nodeWriter
	embedder Embedder
	node     *storage.Node
	policy   EmbeddingPolicy
}

// Execute runs the embed+writeback pipeline for one node/policy pair.
//
// Error handling:
//
// The task is dispatched from OnNodeCreated which has already returned by
// the time Execute runs — there is no caller-side error channel. Real
// errors (source-property type mismatch, embedder backend failure,
// writeback failure) are surfaced via structured logs at the operator's
// log stream. Normal "skip" conditions (target already set, source
// property absent) stay silent — they aren't failures.
//
// The embedder-error log path is M-1 sanitized: the raw error from
// Embedder.Embed may include user text (LSAEmbedder wraps FoldQuery's
// error, which formats the query string into its message via %q). The log
// records only an error *category* — never the raw err.Error() — so
// operator log streams never carry user query content. See
// docs/internals/design/AUDIT_vector_embedding_side_channels_2026-05-15.md
// finding M-1.
//
// Backpressure-drop events (Pool.Submit returning false because the queue
// is full) deliberately do NOT log here — they happen at high frequency
// in normal operation and would dominate the log. The Pool.Dropped()
// counter is the operator interface for that signal (S11 spike §7.5).
func (t *autoEmbedTask) Execute(ctx context.Context) {
	if err := ctx.Err(); err != nil {
		return
	}

	// Preserve user-provided embeddings: if TargetProperty is already set
	// on the node at creation time, the user has explicitly supplied a
	// vector and the auto-embedder must not overwrite it.
	if _, has := t.node.Properties[t.policy.TargetProperty]; has {
		return
	}

	// Read source text. Missing source is a normal "this node doesn't need
	// embedding" case (policy is opt-in by label); skip silently. A
	// present-but-non-string value is a config bug worth surfacing.
	sourceVal, ok := t.node.Properties[t.policy.SourceProperty]
	if !ok {
		return
	}
	text, err := sourceVal.AsString()
	if err != nil {
		log.Printf("auto-embed: tenant=%s node=%d policy=%s: source property %q is not a string-typed value",
			t.node.TenantID, t.node.ID, t.policy.Label, t.policy.SourceProperty)
		return
	}

	vec, err := t.embedder.Embed(ctx, t.node.TenantID, text)
	if err != nil {
		// M-1 sanitization: don't log err.Error() — it may contain the
		// user's query text. Log a category instead.
		log.Printf("auto-embed: tenant=%s node=%d policy=%s: embedder failed (%s)",
			t.node.TenantID, t.node.ID, t.policy.Label, embedErrorCategory(err))
		return
	}

	// Writeback. UpdateNodeForTenant is the tenant-strict path; the node's
	// captured TenantID ensures the writeback lands in the same tenant the
	// node was created in. Writeback errors come from storage (closed
	// storage, concurrent delete) — they don't echo user input, safe to log
	// verbatim.
	update := map[string]storage.Value{
		t.policy.TargetProperty: storage.VectorValue(vec),
	}
	if err := t.writer.UpdateNodeForTenant(t.node.ID, update, t.node.TenantID); err != nil {
		log.Printf("auto-embed: tenant=%s node=%d policy=%s: writeback failed: %v",
			t.node.TenantID, t.node.ID, t.policy.Label, err)
	}
}

// embedErrorCategory returns a short, content-free category string for an
// Embedder error. The result never contains user-controlled text — only
// fixed identifiers — so it is safe to include in operator log streams.
//
// Categories:
//   - "no-index-for-tenant" — ErrNoIndexForTenant (admin must build the
//     index; permanent until acted on).
//   - "embed-failed" — anything else (out-of-vocab, zero-vector projection,
//     backend-specific failures). The Embedder docstring is the audit
//     trail for what's reachable here.
func embedErrorCategory(err error) string {
	var noIndex ErrNoIndexForTenant
	if errors.As(err, &noIndex) {
		return "no-index-for-tenant"
	}
	return "embed-failed"
}
