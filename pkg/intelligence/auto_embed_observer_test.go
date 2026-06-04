package intelligence

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"testing"

	"github.com/dd0wney/graphdb/pkg/search"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// fakeWriter is a minimal nodeWriter for unit tests. Records every
// UpdateNodeForTenant call; can be configured to return an error.
type fakeWriter struct {
	mu    sync.Mutex
	calls []writeCall
	err   error
}

type writeCall struct {
	nodeID     uint64
	properties map[string]storage.Value
	tenantID   string
}

func (f *fakeWriter) UpdateNodeForTenant(nodeID uint64, props map[string]storage.Value, tenantID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Defensive copy of the properties map so the test observes the
	// values at call-time, not whatever the caller mutates afterward.
	pcopy := make(map[string]storage.Value, len(props))
	for k, v := range props {
		pcopy[k] = v
	}
	f.calls = append(f.calls, writeCall{nodeID: nodeID, properties: pcopy, tenantID: tenantID})
	return f.err
}

func (f *fakeWriter) snapshot() []writeCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]writeCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// fakeAutoEmbedder is a minimal Embedder for unit tests. Returns a fixed
// vector and tracks call args; can return a configured error.
type fakeAutoEmbedder struct {
	mu    sync.Mutex
	calls []embedCall
	vec   []float32
	err   error
}

type embedCall struct {
	tenantID string
	text     string
}

func (e *fakeAutoEmbedder) Embed(_ context.Context, tenantID string, text string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, embedCall{tenantID: tenantID, text: text})
	if e.err != nil {
		return nil, e.err
	}
	return e.vec, nil
}

func (e *fakeAutoEmbedder) snapshot() []embedCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]embedCall, len(e.calls))
	copy(out, e.calls)
	return out
}

// synchronousPool returns a Pool in synchronous mode so tests can observe
// post-Submit state without polling.
func synchronousPool() *Pool {
	return NewPool(PoolConfig{Synchronous: true})
}

func newDocPolicy() EmbeddingPolicy {
	return EmbeddingPolicy{
		Label:          "Doc",
		SourceProperty: "body",
		TargetProperty: "embedding",
	}
}

// TestAutoEmbedObserver_DispatchesOnCreate pins the happy path: a matching
// node triggers an embed call and a writeback to the target property.
func TestAutoEmbedObserver_DispatchesOnCreate(t *testing.T) {
	writer := &fakeWriter{}
	embedder := &fakeAutoEmbedder{vec: []float32{0.1, 0.2, 0.3, 0.4}}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, err := NewAutoEmbedObserver(writer, embedder, pool, []EmbeddingPolicy{newDocPolicy()})
	if err != nil {
		t.Fatalf("NewAutoEmbedObserver() error = %v", err)
	}

	node := &storage.Node{
		ID:       42,
		TenantID: "acme",
		Labels:   []string{"Doc"},
		Properties: map[string]storage.Value{
			"body": storage.StringValue("the quick brown fox"),
		},
	}

	obs.OnNodeCreated(context.Background(), node)

	embedCalls := embedder.snapshot()
	if len(embedCalls) != 1 {
		t.Fatalf("Embedder.Embed call count = %d, want 1", len(embedCalls))
	}
	if embedCalls[0].tenantID != "acme" {
		t.Errorf("Embed tenantID = %q, want \"acme\"", embedCalls[0].tenantID)
	}
	if embedCalls[0].text != "the quick brown fox" {
		t.Errorf("Embed text = %q, want \"the quick brown fox\"", embedCalls[0].text)
	}

	writes := writer.snapshot()
	if len(writes) != 1 {
		t.Fatalf("writer.UpdateNodeForTenant call count = %d, want 1", len(writes))
	}
	w := writes[0]
	if w.nodeID != 42 {
		t.Errorf("writeback nodeID = %d, want 42", w.nodeID)
	}
	if w.tenantID != "acme" {
		t.Errorf("writeback tenantID = %q, want \"acme\"", w.tenantID)
	}
	if v, ok := w.properties["embedding"]; !ok {
		t.Errorf("writeback missing \"embedding\" property")
	} else if v.Type != storage.TypeVector {
		t.Errorf("writeback embedding.Type = %v, want TypeVector", v.Type)
	}
}

// TestAutoEmbedObserver_SkipsWhenTargetSet pins that a user-provided
// embedding is preserved — the observer never overwrites a pre-existing
// target property.
func TestAutoEmbedObserver_SkipsWhenTargetSet(t *testing.T) {
	writer := &fakeWriter{}
	embedder := &fakeAutoEmbedder{vec: []float32{1, 2, 3}}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, _ := NewAutoEmbedObserver(writer, embedder, pool, []EmbeddingPolicy{newDocPolicy()})

	userVec := []float32{9, 9, 9}
	node := &storage.Node{
		ID:       1,
		TenantID: "acme",
		Labels:   []string{"Doc"},
		Properties: map[string]storage.Value{
			"body":      storage.StringValue("some text"),
			"embedding": storage.VectorValue(userVec),
		},
	}

	obs.OnNodeCreated(context.Background(), node)

	if got := len(embedder.snapshot()); got != 0 {
		t.Errorf("Embed call count = %d, want 0 (target already set)", got)
	}
	if got := len(writer.snapshot()); got != 0 {
		t.Errorf("writeback call count = %d, want 0", got)
	}
}

// TestAutoEmbedObserver_SkipsWhenLabelMismatch pins that nodes lacking
// the policy's label do not trigger embedding.
func TestAutoEmbedObserver_SkipsWhenLabelMismatch(t *testing.T) {
	writer := &fakeWriter{}
	embedder := &fakeAutoEmbedder{vec: []float32{1, 2}}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, _ := NewAutoEmbedObserver(writer, embedder, pool, []EmbeddingPolicy{newDocPolicy()})

	node := &storage.Node{
		ID:       1,
		TenantID: "acme",
		Labels:   []string{"User"}, // not "Doc"
		Properties: map[string]storage.Value{
			"body": storage.StringValue("text"),
		},
	}

	obs.OnNodeCreated(context.Background(), node)

	if got := len(embedder.snapshot()); got != 0 {
		t.Errorf("Embed call count = %d, want 0 (label mismatch)", got)
	}
	if got := len(writer.snapshot()); got != 0 {
		t.Errorf("writeback call count = %d, want 0", got)
	}
}

// TestAutoEmbedObserver_SkipsWhenSourceMissing pins that label-matching
// nodes without the policy's source property are silently skipped (not
// an error).
func TestAutoEmbedObserver_SkipsWhenSourceMissing(t *testing.T) {
	writer := &fakeWriter{}
	embedder := &fakeAutoEmbedder{vec: []float32{1, 2}}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, _ := NewAutoEmbedObserver(writer, embedder, pool, []EmbeddingPolicy{newDocPolicy()})

	node := &storage.Node{
		ID:         1,
		TenantID:   "acme",
		Labels:     []string{"Doc"},
		Properties: map[string]storage.Value{}, // no "body"
	}

	obs.OnNodeCreated(context.Background(), node)

	if got := len(embedder.snapshot()); got != 0 {
		t.Errorf("Embed call count = %d, want 0 (source missing)", got)
	}
}

// TestAutoEmbedObserver_SkipsWhenSourceNotString pins that a label-matching
// node whose source property is non-string (e.g., a number or vector) is
// silently skipped rather than crashing the task.
func TestAutoEmbedObserver_SkipsWhenSourceNotString(t *testing.T) {
	writer := &fakeWriter{}
	embedder := &fakeAutoEmbedder{vec: []float32{1, 2}}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, _ := NewAutoEmbedObserver(writer, embedder, pool, []EmbeddingPolicy{newDocPolicy()})

	node := &storage.Node{
		ID:       1,
		TenantID: "acme",
		Labels:   []string{"Doc"},
		Properties: map[string]storage.Value{
			"body": storage.IntValue(42), // not a string
		},
	}

	obs.OnNodeCreated(context.Background(), node)

	if got := len(embedder.snapshot()); got != 0 {
		t.Errorf("Embed call count = %d, want 0 (source not string)", got)
	}
}

// TestAutoEmbedObserver_DropsOnEmbedderError pins that an Embedder error
// (e.g., ErrNoIndexForTenant) leads to no writeback and no panic.
func TestAutoEmbedObserver_DropsOnEmbedderError(t *testing.T) {
	writer := &fakeWriter{}
	embedder := &fakeAutoEmbedder{err: ErrNoIndexForTenant{TenantID: "acme"}}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, _ := NewAutoEmbedObserver(writer, embedder, pool, []EmbeddingPolicy{newDocPolicy()})

	node := &storage.Node{
		ID:       1,
		TenantID: "acme",
		Labels:   []string{"Doc"},
		Properties: map[string]storage.Value{
			"body": storage.StringValue("text"),
		},
	}

	obs.OnNodeCreated(context.Background(), node)

	if got := len(embedder.snapshot()); got != 1 {
		t.Errorf("Embed call count = %d, want 1", got)
	}
	if got := len(writer.snapshot()); got != 0 {
		t.Errorf("writeback call count = %d, want 0 (embedder failed)", got)
	}
}

// TestAutoEmbedObserver_OnNodeUpdatedIsNoOp pins that update notifications
// do not trigger embedding in R2.5a (deferred until re-entry guard ships).
func TestAutoEmbedObserver_OnNodeUpdatedIsNoOp(t *testing.T) {
	writer := &fakeWriter{}
	embedder := &fakeAutoEmbedder{vec: []float32{1, 2}}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, _ := NewAutoEmbedObserver(writer, embedder, pool, []EmbeddingPolicy{newDocPolicy()})

	node := &storage.Node{
		ID:       1,
		TenantID: "acme",
		Labels:   []string{"Doc"},
		Properties: map[string]storage.Value{
			"body": storage.StringValue("updated text"),
		},
	}
	oldNode := &storage.Node{
		ID:       1,
		TenantID: "acme",
		Labels:   []string{"Doc"},
		Properties: map[string]storage.Value{
			"body": storage.StringValue("original text"),
		},
	}

	obs.OnNodeUpdated(context.Background(), node, oldNode)

	if got := len(embedder.snapshot()); got != 0 {
		t.Errorf("Embed call count = %d, want 0 (OnNodeUpdated is deferred)", got)
	}
}

// TestAutoEmbedObserver_OnNodeDeletedIsNoOp pins that delete notifications
// do not call the embedder or writer.
func TestAutoEmbedObserver_OnNodeDeletedIsNoOp(t *testing.T) {
	writer := &fakeWriter{}
	embedder := &fakeAutoEmbedder{vec: []float32{1, 2}}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, _ := NewAutoEmbedObserver(writer, embedder, pool, []EmbeddingPolicy{newDocPolicy()})

	obs.OnNodeDeleted(context.Background(), 1, "acme")

	if got := len(embedder.snapshot()); got != 0 {
		t.Errorf("Embed call count = %d, want 0", got)
	}
	if got := len(writer.snapshot()); got != 0 {
		t.Errorf("writeback call count = %d, want 0", got)
	}
}

// TestAutoEmbedObserver_MultiplePoliciesFire pins that a node matching
// multiple policies triggers one task per policy. The second policy
// writes to a different target property so both writebacks are distinct.
func TestAutoEmbedObserver_MultiplePoliciesFire(t *testing.T) {
	writer := &fakeWriter{}
	embedder := &fakeAutoEmbedder{vec: []float32{1, 2}}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	policies := []EmbeddingPolicy{
		{Label: "Doc", SourceProperty: "body", TargetProperty: "body_embedding"},
		{Label: "Doc", SourceProperty: "title", TargetProperty: "title_embedding"},
	}
	obs, _ := NewAutoEmbedObserver(writer, embedder, pool, policies)

	node := &storage.Node{
		ID:       1,
		TenantID: "acme",
		Labels:   []string{"Doc"},
		Properties: map[string]storage.Value{
			"body":  storage.StringValue("body text"),
			"title": storage.StringValue("title text"),
		},
	}

	obs.OnNodeCreated(context.Background(), node)

	if got := len(embedder.snapshot()); got != 2 {
		t.Errorf("Embed call count = %d, want 2 (one per matching policy)", got)
	}
	writes := writer.snapshot()
	if len(writes) != 2 {
		t.Fatalf("writeback call count = %d, want 2", len(writes))
	}
	got := map[string]bool{}
	for _, w := range writes {
		for k := range w.properties {
			got[k] = true
		}
	}
	if !got["body_embedding"] || !got["title_embedding"] {
		t.Errorf("writebacks wrote properties %v, want both body_embedding+title_embedding", got)
	}
}

// TestAutoEmbedObserver_ConstructorValidation pins that NewAutoEmbedObserver
// surfaces configuration bugs at startup rather than at runtime.
func TestAutoEmbedObserver_ConstructorValidation(t *testing.T) {
	writer := &fakeWriter{}
	embedder := &fakeAutoEmbedder{}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	cases := []struct {
		name     string
		writer   nodeWriter
		embedder Embedder
		pool     *Pool
		policies []EmbeddingPolicy
		wantSub  string
	}{
		{"nil writer", nil, embedder, pool, nil, "writer"},
		{"nil embedder", writer, nil, pool, nil, "embedder"},
		{"nil pool", writer, embedder, nil, nil, "pool"},
		{"empty label", writer, embedder, pool, []EmbeddingPolicy{{Label: "", SourceProperty: "x", TargetProperty: "y"}}, "Label"},
		{"empty source", writer, embedder, pool, []EmbeddingPolicy{{Label: "Doc", SourceProperty: "", TargetProperty: "y"}}, "SourceProperty"},
		{"empty target", writer, embedder, pool, []EmbeddingPolicy{{Label: "Doc", SourceProperty: "x", TargetProperty: ""}}, "TargetProperty"},
		{"source equals target", writer, embedder, pool, []EmbeddingPolicy{{Label: "Doc", SourceProperty: "x", TargetProperty: "x"}}, "must differ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewAutoEmbedObserver(tc.writer, tc.embedder, tc.pool, tc.policies)
			if err == nil {
				t.Fatal("NewAutoEmbedObserver() err = nil, want non-nil")
			}
			if !contains(err.Error(), tc.wantSub) {
				t.Errorf("err = %q, want it to contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// TestAutoEmbedObserver_ImplementsNodeObserver pins the interface
// contract at compile time.
func TestAutoEmbedObserver_ImplementsNodeObserver(t *testing.T) {
	var _ storage.NodeObserver = (*AutoEmbedObserver)(nil)
}

// TestAutoEmbedObserver_CtxCancelled pins that a pre-cancelled context
// bails the task before any embedder/writer work.
func TestAutoEmbedObserver_CtxCancelled(t *testing.T) {
	writer := &fakeWriter{}
	embedder := &fakeAutoEmbedder{vec: []float32{1, 2}}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, _ := NewAutoEmbedObserver(writer, embedder, pool, []EmbeddingPolicy{newDocPolicy()})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	node := &storage.Node{
		ID:       1,
		TenantID: "acme",
		Labels:   []string{"Doc"},
		Properties: map[string]storage.Value{
			"body": storage.StringValue("text"),
		},
	}

	obs.OnNodeCreated(ctx, node)

	// In synchronous mode the task ran inline; the ctx.Err check at the
	// top should have caused an early return.
	if got := len(embedder.snapshot()); got != 0 {
		t.Errorf("Embed call count = %d, want 0 (ctx cancelled)", got)
	}
}

// ---------- Integration test (real GraphStorage + LSAEmbedder + Pool) ----------

// TestAutoEmbedObserver_Integration ties the observer to a real
// *storage.GraphStorage, a real LSAEmbedder backed by an in-memory LSA
// index, and a real synchronous Pool. End-to-end: create a node with
// matching label + source text → after OnNodeCreated returns, the node's
// target property holds a non-empty []float32 of the LSA index's
// configured dimensionality.
//
// This catches wire-up bugs (wrong tenant routing, sync vs async dispatch
// timing) that the unit tests with fakes would miss.
func TestAutoEmbedObserver_Integration(t *testing.T) {
	tmpDir := t.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{DataDir: tmpDir, BulkImportMode: true})
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig() error = %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	// Build a tiny LSA index for the "acme" tenant.
	registry := search.NewTenantLSAIndexes()
	registry.Set("acme", buildIndex(t, testCorpus()))

	embedder := NewLSAEmbedder(registry)
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, err := NewAutoEmbedObserver(gs, embedder, pool, []EmbeddingPolicy{newDocPolicy()})
	if err != nil {
		t.Fatalf("NewAutoEmbedObserver() error = %v", err)
	}
	gs.AddObserver(obs)

	// Create a node — observer fires, task runs inline (sync pool), writeback completes.
	node, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]storage.Value{
		"body": storage.StringValue("dogs in the garden"),
	})
	if err != nil {
		t.Fatalf("CreateNodeWithTenant() error = %v", err)
	}

	// Read back the node and assert the embedding landed.
	got, err := gs.GetNodeForTenant(node.ID, "acme")
	if err != nil {
		t.Fatalf("GetNodeForTenant() error = %v", err)
	}
	emb, ok := got.Properties["embedding"]
	if !ok {
		t.Fatal("node missing \"embedding\" property after observer dispatch")
	}
	vec, err := emb.AsVector()
	if err != nil {
		t.Fatalf("embedding.AsVector() error = %v", err)
	}
	if len(vec) != testLSAConfig().Dims {
		t.Errorf("len(vec) = %d, want %d", len(vec), testLSAConfig().Dims)
	}
}

// TestAutoEmbedObserver_Integration_RejectsMockShape mirrors the
// spike T2 invariant at the integration level: even when the observer
// dispatches a real LSAEmbedder under a real pool with a real
// GraphStorage, the resulting vector is the LSA's dims (NOT 3).
func TestAutoEmbedObserver_Integration_RejectsMockShape(t *testing.T) {
	tmpDir := t.TempDir()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{DataDir: tmpDir, BulkImportMode: true})
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig() error = %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	registry := search.NewTenantLSAIndexes()
	registry.Set("acme", buildIndex(t, testCorpus()))

	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, _ := NewAutoEmbedObserver(gs, NewLSAEmbedder(registry), pool, []EmbeddingPolicy{newDocPolicy()})
	gs.AddObserver(obs)

	node, _ := gs.CreateNodeWithTenant("acme", []string{"Doc"}, map[string]storage.Value{
		"body": storage.StringValue("morning birds sing"),
	})
	got, _ := gs.GetNodeForTenant(node.ID, "acme")
	emb, _ := got.Properties["embedding"].AsVector()
	if len(emb) == 3 {
		t.Fatal("embedding has length 3 — mockEmbedding pattern detected in the integration path")
	}
}

// TestAutoEmbedObserver_LogsEmbedderError_SanitizesUserText pins the M-1
// sanitization contract at the observer site. When the Embedder returns an
// error whose message contains the user's source text, the log line must
// surface the error *category* but never the raw error string. A regression
// here re-opens the audit M-1 leak at the worker layer.
func TestAutoEmbedObserver_LogsEmbedderError_SanitizesUserText(t *testing.T) {
	writer := &fakeWriter{}
	// Simulate the LSAEmbedder pattern: wrap an err whose message contains the
	// raw user text (FoldQuery formats it with %q). The observer must NOT
	// log this verbatim.
	sensitiveText := "user-secret-correlation-id-12345"
	embedder := &fakeAutoEmbedder{
		err: fmt.Errorf("lsa embed: no vocabulary terms matched in query %q", sensitiveText),
	}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, _ := NewAutoEmbedObserver(writer, embedder, pool, []EmbeddingPolicy{newDocPolicy()})

	node := &storage.Node{
		ID:       7,
		TenantID: "acme",
		Labels:   []string{"Doc"},
		Properties: map[string]storage.Value{
			"body": storage.StringValue(sensitiveText),
		},
	}

	out := captureLog(t, func() {
		obs.OnNodeCreated(context.Background(), node)
	})

	if !contains(out, "embedder failed") {
		t.Errorf("log should record the embedder-failed category; got: %s", out)
	}
	if !contains(out, "embed-failed") {
		t.Errorf("log should record the embed-failed category token; got: %s", out)
	}
	// M-1 contract: the user-controlled text must NOT appear in the log.
	if contains(out, sensitiveText) {
		t.Errorf("M-1 regression: log contains user-controlled source text %q; got: %s",
			sensitiveText, out)
	}
	// Belt and braces: the raw FoldQuery-style fragment also must not appear.
	if contains(out, "no vocabulary terms matched") {
		t.Errorf("M-1 regression: log contains raw embedder error message fragment; got: %s", out)
	}
	// Useful context that SHOULD appear: tenant, node, policy.
	for _, expected := range []string{"tenant=acme", "node=7", "policy=Doc"} {
		if !contains(out, expected) {
			t.Errorf("log missing diagnostic field %q; got: %s", expected, out)
		}
	}
}

// TestAutoEmbedObserver_LogsEmbedderError_NoIndexCategory pins that the
// no-index case surfaces with its dedicated category (so operators can
// distinguish "admin needs to build the index" from generic embed failures).
func TestAutoEmbedObserver_LogsEmbedderError_NoIndexCategory(t *testing.T) {
	embedder := &fakeAutoEmbedder{err: ErrNoIndexForTenant{TenantID: "acme"}}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, _ := NewAutoEmbedObserver(&fakeWriter{}, embedder, pool, []EmbeddingPolicy{newDocPolicy()})
	node := &storage.Node{
		ID: 1, TenantID: "acme", Labels: []string{"Doc"},
		Properties: map[string]storage.Value{"body": storage.StringValue("hello")},
	}

	out := captureLog(t, func() {
		obs.OnNodeCreated(context.Background(), node)
	})

	if !contains(out, "no-index-for-tenant") {
		t.Errorf("log should record the no-index-for-tenant category; got: %s", out)
	}
}

// TestAutoEmbedObserver_LogsWritebackError pins the writeback-error log
// path. The error here originates from storage and does not echo user
// input — safe to log the raw err.Error().
func TestAutoEmbedObserver_LogsWritebackError(t *testing.T) {
	writer := &fakeWriter{err: errors.New("storage: tenant index closed")}
	embedder := &fakeAutoEmbedder{vec: []float32{0.1, 0.2}}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, _ := NewAutoEmbedObserver(writer, embedder, pool, []EmbeddingPolicy{newDocPolicy()})
	node := &storage.Node{
		ID: 99, TenantID: "acme", Labels: []string{"Doc"},
		Properties: map[string]storage.Value{"body": storage.StringValue("hello")},
	}

	out := captureLog(t, func() {
		obs.OnNodeCreated(context.Background(), node)
	})

	if !contains(out, "writeback failed") {
		t.Errorf("log should record writeback-failed; got: %s", out)
	}
	if !contains(out, "tenant index closed") {
		t.Errorf("log should propagate the storage error (no user-text concern here); got: %s", out)
	}
}

// TestAutoEmbedObserver_LogsSourceTypeMismatch pins the source-property
// type-mismatch log path. A node whose source property exists but is not
// a string is a config bug worth surfacing.
func TestAutoEmbedObserver_LogsSourceTypeMismatch(t *testing.T) {
	writer := &fakeWriter{}
	embedder := &fakeAutoEmbedder{vec: []float32{1, 2}}
	pool := synchronousPool()
	defer pool.Shutdown(context.Background())

	obs, _ := NewAutoEmbedObserver(writer, embedder, pool, []EmbeddingPolicy{newDocPolicy()})
	node := &storage.Node{
		ID: 13, TenantID: "acme", Labels: []string{"Doc"},
		// "body" is an int instead of string — typical config error.
		Properties: map[string]storage.Value{"body": storage.IntValue(42)},
	}

	out := captureLog(t, func() {
		obs.OnNodeCreated(context.Background(), node)
	})

	if !contains(out, "not a string-typed value") {
		t.Errorf("log should surface the source-type mismatch; got: %s", out)
	}
	if len(embedder.snapshot()) != 0 {
		t.Errorf("embedder should not have been called for non-string source")
	}
}

// TestAutoEmbedObserver_NoLogOnNormalSkip pins that the *normal* skip
// conditions (target already set, source absent, label mismatch) do NOT
// log — those are not failures, just opt-in misses. A regression here
// would flood operator logs.
func TestAutoEmbedObserver_NoLogOnNormalSkip(t *testing.T) {
	cases := []struct {
		name string
		node *storage.Node
	}{
		{
			name: "target-already-set",
			node: &storage.Node{
				ID: 1, TenantID: "acme", Labels: []string{"Doc"},
				Properties: map[string]storage.Value{
					"body":      storage.StringValue("hi"),
					"embedding": storage.VectorValue([]float32{1, 2, 3}),
				},
			},
		},
		{
			name: "source-property-missing",
			node: &storage.Node{
				ID: 2, TenantID: "acme", Labels: []string{"Doc"},
				Properties: map[string]storage.Value{},
			},
		},
		{
			name: "label-mismatch",
			node: &storage.Node{
				ID: 3, TenantID: "acme", Labels: []string{"NotADoc"},
				Properties: map[string]storage.Value{"body": storage.StringValue("hi")},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obs, _ := NewAutoEmbedObserver(
				&fakeWriter{},
				&fakeAutoEmbedder{vec: []float32{1, 2}},
				synchronousPool(),
				[]EmbeddingPolicy{newDocPolicy()},
			)
			out := captureLog(t, func() {
				obs.OnNodeCreated(context.Background(), tc.node)
			})
			if out != "" {
				t.Errorf("normal-skip path should produce no log output; got: %s", out)
			}
		})
	}
}

// ---------- helpers ----------

// captureLog redirects the default logger's output to a buffer for the
// duration of fn, returning whatever was logged. log.Flags is cleared so
// captured strings don't carry the test process's timestamp prefix.
//
// The default logger is process-global, so tests using captureLog must
// not run with t.Parallel() — concurrent log capture would interleave.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	oldOutput := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(oldOutput)
		log.SetFlags(oldFlags)
	}()
	fn()
	return buf.String()
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
