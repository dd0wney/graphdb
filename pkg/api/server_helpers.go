package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"path/filepath"

	"github.com/dd0wney/graphdb/pkg/masking"
	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// nodeToResponse converts a *storage.Node to its JSON response shape.
// Takes ctx for tenant-scoped masking policy lookup (F3): the request
// context carries the resolved tenantID via withTenant, and this
// helper applies the tenant's masking policy (if any) to property
// values before returning.
//
// ctx may be a non-HTTP context (background, test) — in that case
// there's no tenant resolvable from context, so no masking is applied
// (equivalent to pre-F3 behaviour).
func (s *Server) nodeToResponse(ctx context.Context, node *storage.Node) *NodeResponse {
	props := make(map[string]any, len(node.Properties))
	for k, v := range node.Properties {
		props[k] = valueToInterface(v)
	}
	props = s.applyMaskingPolicy(ctx, props)

	return &NodeResponse{
		ID:         node.ID,
		Labels:     node.Labels,
		Properties: props,
	}
}

// edgeToResponse mirrors nodeToResponse for edges. Same ctx contract.
func (s *Server) edgeToResponse(ctx context.Context, edge *storage.Edge) *EdgeResponse {
	props := make(map[string]any, len(edge.Properties))
	for k, v := range edge.Properties {
		props[k] = valueToInterface(v)
	}
	props = s.applyMaskingPolicy(ctx, props)

	return &EdgeResponse{
		ID:         edge.ID,
		FromNodeID: edge.FromNodeID,
		ToNodeID:   edge.ToNodeID,
		Type:       edge.Type,
		Properties: props,
		Weight:     edge.Weight,
	}
}

// applyMaskingPolicy is the per-tenant masking hook. Resolves the
// tenant from ctx, looks up the tenant's masking policy (if any),
// and returns a copy of props with the policy applied. If no policy
// is set for the tenant — the common case — returns props unchanged.
//
// Defense-in-depth: returns props unchanged on any internal error.
// The F3 design doc §6 calls out that read paths must never
// fail-closed on masking errors — better to ship unmasked output
// and surface the gap via audit logs than to break customer reads.
func (s *Server) applyMaskingPolicy(ctx context.Context, props map[string]any) map[string]any {
	if s == nil || s.maskingPolicyStore == nil || s.masker == nil || ctx == nil {
		return props
	}
	tenantID, ok := tenant.FromContext(ctx)
	if !ok || tenantID == "" {
		return props
	}
	policy, err := s.maskingPolicyStore.Get(tenantID)
	if err != nil {
		if errors.Is(err, masking.ErrPolicyNotFound) {
			return props
		}
		// Other errors are unexpected from an in-memory store; pass
		// through rather than fail the response.
		return props
	}
	return policy.Apply(props, s.masker)
}

// convertToValue is the thin method wrapper around
// storage.ValueFromJSON, kept here so existing call sites that pass
// `s.convertToValue` as a method value to `pc.ConvertAndSanitize`
// don't need to change. The real conversion logic lives in
// pkg/storage and is shared with the GraphQL resolver path —
// previously the two diverged and caused silent-failure shape #7
// (2026-05-14).
func (s *Server) convertToValue(v any) storage.Value {
	return storage.ValueFromJSON(v)
}

// valueToInterface decodes a typed storage.Value into a JSON-serializable
// Go value, dispatching on Type. Inverse of convertToValue (which goes
// JSON -> Value on the way in).
//
// Without this, every REST `/nodes` and `/edges` GET emits properties as
// base64-encoded strings — encoding/json marshals []byte that way, and
// Value.Data is the *binary* encoding of typed values. See planning-doc
// H4.1.
//
// On a decode error (storage corruption / malformed Data), falls back to
// returning the raw bytes — preserves current base64-output behaviour
// rather than corrupting the response shape with a sentinel string.
//
// The conversion logic lives in storage.ValueToJSON so the REST and GraphQL
// paths share one converter (#224).
func valueToInterface(v storage.Value) any {
	return storage.ValueToJSON(v)
}

func (s *Server) respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding JSON response: %v", err)
	}
}

func (s *Server) respondError(w http.ResponseWriter, status int, message string) {
	response := ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
		Code:    status,
	}
	s.respondJSON(w, status, response)
}

// walWriteFailedMessage is the fixed sentence every WriteNotDurableResponse
// carries. It never varies with the wrapped disk error (see
// respondWALWriteFailed) so a caller can match on it without depending on
// storage's own error wording.
const walWriteFailedMessage = "The write applied in memory and is not durable yet. Do not retry: a retry applies the change a second time. A snapshot or a clean shutdown makes it durable."

// newNotDurableFields builds the five status fields every not-durable
// response body carries (see notDurableFields in types.go). Factored out
// so the three respond*WALWriteFailed helpers below build it identically.
func newNotDurableFields() notDurableFields {
	return notDurableFields{
		Applied: true,
		Durable: false,
		Retry:   false,
		Error:   "WAL write failed",
		Message: walWriteFailedMessage,
	}
}

// logWALWriteFailed logs a WAL append failure with the same logger the
// handlers use for 500s. The wrapped disk error never reaches the response
// body — same precedent as ErrRecordUnreadable (see getNode in
// handlers_nodes.go) — only the fixed sentence does, so this is the only
// place err's detail goes.
func logWALWriteFailed(err error) {
	log.Printf("ERROR [wal write]: change applied in memory, WAL append failed: %v", err)
}

// respondWALWriteFailed answers a write whose in-memory change applied but
// whose WAL append failed (storage.ErrWALWriteFailed wraps the disk cause)
// with a WriteNotDurableResponse: id (0 for deleteAllNodes/
// handleDeleteTenant's bulk deletes and for the vector-index handlers,
// which have no single per-write entity id) plus the five not-durable
// fields. Status 202 Accepted, not 201 and not a 5xx: a 2xx stops all
// three first-party clients from retrying, and a 5xx would still make the
// Go client retry on an idempotent method (clients/go/transport.go retries
// a 5xx only on GET, HEAD, PUT, DELETE, and OPTIONS — POST and PATCH are
// excluded — and PUT/DELETE are what most callers of this helper use: the
// node/edge update and delete routes, deleteAllNodes, and
// handleDeleteTenant), so the 202 decision here keeps its reason. A 4xx
// would say the request itself was wrong, which it was not. 202 rather
// than 201 lets a caller that checks the exact status code tell the two
// apart.
//
// createNode and createEdge use respondNodeWALWriteFailed /
// respondEdgeWALWriteFailed instead, which answer with the full entity.
func (s *Server) respondWALWriteFailed(w http.ResponseWriter, err error, id uint64) {
	logWALWriteFailed(err)
	s.respondJSON(w, http.StatusAccepted, WriteNotDurableResponse{
		ID:               id,
		notDurableFields: newNotDurableFields(),
	})
}

// respondNodeWALWriteFailed answers a createNode whose WAL append failed
// with a NodeNotDurableResponse: the full node (via nodeToResponse, so
// tenant masking still applies) plus the five not-durable fields — a
// SUPERSET of the 201 NodeResponse body, so a client that treats any 2xx as
// success and reads only the node fields still builds a correct node. node
// is expected non-nil (CreateNodeWithUniquenessRulesForTenant's two routes,
// CreateNodeWithTenant and CreateNodeWithUniquePropertyForTenant, both
// return the node alongside a WAL error); the nil guard is defensive.
func (s *Server) respondNodeWALWriteFailed(ctx context.Context, w http.ResponseWriter, err error, node *storage.Node) {
	logWALWriteFailed(err)
	resp := NodeNotDurableResponse{notDurableFields: newNotDurableFields()}
	if node != nil {
		resp.NodeResponse = *s.nodeToResponse(ctx, node)
	}
	s.respondJSON(w, http.StatusAccepted, resp)
}

// respondEdgeWALWriteFailed mirrors respondNodeWALWriteFailed for
// createEdge. edge is expected non-nil (CreateEdgeWithTenant returns the
// edge alongside a WAL error); the nil guard is defensive.
func (s *Server) respondEdgeWALWriteFailed(ctx context.Context, w http.ResponseWriter, err error, edge *storage.Edge) {
	logWALWriteFailed(err)
	resp := EdgeNotDurableResponse{notDurableFields: newNotDurableFields()}
	if edge != nil {
		resp.EdgeResponse = *s.edgeToResponse(ctx, edge)
	}
	s.respondJSON(w, http.StatusAccepted, resp)
}

// SaveAuthData persists users and API keys to disk
func (s *Server) SaveAuthData() error {
	if s.dataDir == "" {
		return nil // No data directory configured
	}

	authDataDir := filepath.Join(s.dataDir, "auth")

	// Save users
	if err := s.userStore.SaveUsers(authDataDir); err != nil {
		log.Printf("⚠️  Warning: Failed to save users: %v", err)
		return err
	}

	// Save API keys
	if err := s.apiKeyStore.SaveAPIKeys(authDataDir); err != nil {
		log.Printf("⚠️  Warning: Failed to save API keys: %v", err)
		return err
	}

	return nil
}

// GetDataDir returns the server's data directory
func (s *Server) GetDataDir() string {
	return s.dataDir
}
