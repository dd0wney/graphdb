// Package api: node-label mutation HTTP surface.
//
// Closes the third "storage supports it, HTTP doesn't" gap surfaced by
// the Ulysses consumer audit (after property-index lifecycle and the
// general unique_property field). Before this file, a node's labels
// were write-once: PUT /nodes/{id} only accepted properties, and the
// Cypher dialect's SetClause AST (pkg/query/ast.go) doesn't model
// `SET n:Label` syntax. The only "workaround" was delete-and-recreate,
// which loses the i64 node ID and breaks every IdMapper reference and
// edge pointing at the node.
//
// Concrete blocked use case the routes here unblock: Ulysses' embedding
// store writes new TextEmbedding nodes with a secondary per-entity-type
// label (CharacterEmbedding, DocumentEmbedding, …) starting at a
// recent commit. Existing embeddings written before that commit have
// only TextEmbedding. Backfilling the secondary label is now a
// POST /nodes/{id}/labels per node — no rewrites, no broken refs.
//
// Shape: dedicated REST endpoints (Shape A in the original brief),
// chosen over a Cypher SET extension (Shape B) because:
//   - smaller diff (no parser / AST surgery)
//   - directly addresses the consumer's backfill need without forcing
//     them to issue Cypher
//   - Shape B can land later if and when Cypher callers need it; the
//     two shapes aren't mutually exclusive
//
// Auth: requireAuth + withTenant, mirroring /nodes — a tenant's
// principals are the only ones who should be able to mutate that
// tenant's nodes' labels. Cross-tenant attempts return 404 (unified
// existence-leak guard, same rationale as GetNodeForTenant).
package api

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/validation"
)

// AddNodeLabelsRequest is the body for POST /nodes/{id}/labels.
//
// labels is a list (not a single value) so the common backfill case —
// adding several secondary labels to a freshly-imported node — fits in
// one request. The labels are treated as a set: re-sending one already
// present on the node is a no-op (200 with that label absent from
// `added` in the response).
type AddNodeLabelsRequest struct {
	Labels []string `json:"labels"`
}

// AddNodeLabelsResponse reports which labels were newly added vs.
// already present, plus the node's full post-mutation label set so the
// caller can reconcile without an extra GET.
//
// Idempotency contract: a POST that adds zero new labels returns 200
// with empty `added` (NOT 409). Labels are a set, not a sequence.
type AddNodeLabelsResponse struct {
	NodeID     uint64   `json:"node_id"`
	Added      []string `json:"added"`
	Labels     []string `json:"labels"`
	AlreadyHad []string `json:"already_had,omitempty"`
}

// handleNodeLabels routes /nodes/{id}/labels (collection).
//
// Wired with requireAuth + withTenant in server.go. Path extraction
// happens inside the POST handler since /nodes/{id}/labels and
// /nodes/{id}/labels/{label} share a registration prefix.
func (s *Server) handleNodeLabels(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Post(func() { s.addNodeLabels(w, r) }).
		NotAllowed()
}

// handleNodeLabel routes /nodes/{id}/labels/{label} (single resource).
// Only DELETE is meaningful here — the collection POST handles add.
func (s *Server) handleNodeLabel(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Delete(func() { s.deleteNodeLabel(w, r) }).
		NotAllowed()
}

// addNodeLabels handles POST /nodes/{id}/labels.
//
// Body: `{"labels": ["LabelA", "LabelB"]}`. Validation matches the
// node-create rules (alphanumeric + underscore, max length 50,
// non-empty) so the same set of labels valid on create is valid here.
// Returns 200 + AddNodeLabelsResponse on success, 400 on validation
// failure, 404 on missing-or-cross-tenant node.
func (s *Server) addNodeLabels(w http.ResponseWriter, r *http.Request) {
	nodeID, ok := extractNodeIDFromLabelsPath(s, w, r, "/labels")
	if !ok {
		return
	}

	var req AddNodeLabelsRequest
	if s.NewRequestDecoder(w, r).DecodeJSON(&req).RespondError() {
		return
	}

	if len(req.Labels) == 0 {
		s.respondError(w, http.StatusBadRequest, "labels: at least one label is required")
		return
	}

	// Reuse the existing label-format validator so the rules here track
	// the rules on POST /nodes. Don't roll a parallel regex.
	if err := validateLabelList(req.Labels); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	tenantID := getTenantFromContext(r)
	added, err := s.graph.AddNodeLabelsForTenant(nodeID, tenantID, req.Labels)
	if err != nil {
		if errors.Is(err, storage.ErrNodeNotFound) {
			// Cross-tenant and genuinely-missing both surface as 404 —
			// no existence-leak side channel, same as GetNodeForTenant.
			s.respondError(w, http.StatusNotFound, "Node not found")
			return
		}
		s.respondError(w, http.StatusInternalServerError,
			sanitizeError(err, "add node labels"))
		return
	}

	// Re-fetch so the response can report the full post-mutation label
	// set. The extra read is cheap (single shard hit) and saves the
	// caller a follow-up GET they would otherwise need to reconcile.
	node, err := s.graph.GetNodeForTenant(nodeID, tenantID)
	if err != nil {
		// Add succeeded but read-back failed — return what we know.
		s.respondJSON(w, http.StatusOK, AddNodeLabelsResponse{
			NodeID: nodeID,
			Added:  added,
			Labels: req.Labels, // best effort
		})
		return
	}

	// Invalidate the tenant's cached GraphQL schema if (and only if) a
	// previously-unknown label entered the tenant's label set. The
	// GraphQL schema generator (pkg/graphql/schema.go) materializes one
	// type per label visible to the tenant; adding a brand-new label
	// changes the schema, so the next /graphql request needs a rebuilt
	// handler. Mirrors handleSchemaRegenerate's single-line invalidate.
	// Idempotent no-ops (`added` is empty) skip the invalidation since
	// the tenant's label set is unchanged.
	if len(added) > 0 {
		invalidateTenantSchemaCache(s, tenantID)
	}

	alreadyHad := labelsAlreadyHad(req.Labels, added)
	s.respondJSON(w, http.StatusOK, AddNodeLabelsResponse{
		NodeID:     nodeID,
		Added:      added,
		Labels:     node.Labels,
		AlreadyHad: alreadyHad,
	})
}

// deleteNodeLabel handles DELETE /nodes/{id}/labels/{label}.
//
// 204 on success, 404 on missing-or-cross-tenant node OR on
// label-not-present-on-node (consumer asked to remove something that
// isn't there — that's a meaningful 404). 400 if the label name is
// empty or invalid, or if removing it would leave the node with zero
// labels (matches the validator's min=1 invariant on create).
func (s *Server) deleteNodeLabel(w http.ResponseWriter, r *http.Request) {
	nodeID, label, ok := extractNodeIDAndLabelFromPath(s, w, r)
	if !ok {
		return
	}

	if err := validateLabelList([]string{label}); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	tenantID := getTenantFromContext(r)
	err := s.graph.RemoveNodeLabelForTenant(nodeID, tenantID, label)
	switch {
	case err == nil:
		// If the removed label was the LAST node in the tenant carrying
		// it, that label is now gone from the tenant's set — the
		// GraphQL schema for that tenant should be rebuilt so the
		// (now-empty) type disappears. We can't cheaply check
		// "was this the last node with the label" without re-locking,
		// so invalidate on every successful remove. The cost is one
		// schema rebuild on the next /graphql request; same shape as
		// handleSchemaRegenerate.
		invalidateTenantSchemaCache(s, tenantID)
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, storage.ErrNodeNotFound):
		s.respondError(w, http.StatusNotFound, "Node not found")
	case errors.Is(err, storage.ErrLabelNotPresent):
		// Distinct 404 message so the consumer can branch on the body —
		// the status itself stays generic to avoid an existence-leak
		// side channel via differing status codes.
		s.respondError(w, http.StatusNotFound, "Label not present on node")
	case errors.Is(err, storage.ErrLabelLastLabel):
		s.respondError(w, http.StatusBadRequest,
			"Cannot remove the node's only label; nodes must carry at least one label")
	default:
		s.respondError(w, http.StatusInternalServerError,
			sanitizeError(err, "remove node label"))
	}
}

// validateLabelList runs the same label-format checks the node-create
// validator does (alphanumeric + underscore, max length 50, non-empty,
// per-call max count). Borrows the rule set from
// validation.ValidateNodeRequest so a label that's legal on create is
// legal here and vice-versa — the validator owns the canonical rules.
//
// One call covers the whole slice (not per-element) so the per-request
// MaxLabels cap applies symmetrically with the create path. Without
// this, a caller could bypass MaxLabels by patching them on after
// creation.
func validateLabelList(labels []string) error {
	if len(labels) == 0 {
		return errLabelsRequired
	}
	req := validation.NodeRequest{
		Labels:     labels,
		Properties: nil,
	}
	return validation.ValidateNodeRequest(&req)
}

// labelsAlreadyHad returns the labels in `requested` that are NOT in
// `added` — i.e., the labels that were no-ops because the node already
// carried them. Used to populate AddNodeLabelsResponse.AlreadyHad.
//
// O(n*m) but n and m are both bounded by validation.MaxLabels=10, so
// a hash-set would just add allocation overhead. If MaxLabels grows
// substantially this is the place to revisit.
func labelsAlreadyHad(requested, added []string) []string {
	if len(added) == len(requested) {
		return nil
	}
	addedSet := make(map[string]struct{}, len(added))
	for _, l := range added {
		addedSet[l] = struct{}{}
	}
	out := make([]string, 0, len(requested)-len(added))
	for _, l := range requested {
		if _, ok := addedSet[l]; !ok {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Sentinel error for the "no labels in the request" case — surfaced
// before validateLabelList delegates to the validator. The validator's
// own "field is required" message names `Labels` which leaks the Go
// field name into the response body; this sentinel carries a wire-
// friendly phrasing instead.
var errLabelsRequired = errLabelValidation("labels: at least one label is required")

// errLabelValidation is a string-as-error so callers see only the
// user-safe message via .Error() in the HTTP response body.
type errLabelValidation string

func (e errLabelValidation) Error() string { return string(e) }

// extractNodeIDFromLabelsPath parses /nodes/{id}/{suffix} into the
// uint64 node ID. suffix is the literal segment that must follow the
// ID (e.g. "/labels" for POST /nodes/123/labels). Sends a 400 and
// returns ok=false if the path doesn't match the expected shape.
//
// Lives here rather than on pathIDExtractor because the existing
// ExtractUint64 assumes the prefix ends immediately before the ID —
// /nodes/{id}/labels has a fixed trailing segment that ExtractUint64
// would silently treat as part of the ID and fail to parse.
func extractNodeIDFromLabelsPath(s *Server, w http.ResponseWriter, r *http.Request, suffix string) (uint64, bool) {
	parts, ok := s.NewPathExtractor(w, r).ExtractParts("/nodes/")
	if !ok {
		return 0, false
	}
	// Expected shape: ["{id}", "labels"] for POST and similar for
	// future siblings. Anything else (missing segment, extra segments,
	// or wrong suffix) is a 400.
	if len(parts) != 2 || "/"+parts[1] != suffix {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return 0, false
	}
	id, ok := parseUint64(parts[0])
	if !ok {
		s.respondError(w, http.StatusBadRequest, "Invalid ID format")
		return 0, false
	}
	return id, true
}

// extractNodeIDAndLabelFromPath parses /nodes/{id}/labels/{label} into
// the (uint64 node ID, string label) pair. The label segment is
// URL-decoded by the net/http path handling; this function does not
// re-decode. Sends a 400 and returns ok=false on shape mismatch.
func extractNodeIDAndLabelFromPath(s *Server, w http.ResponseWriter, r *http.Request) (uint64, string, bool) {
	parts, ok := s.NewPathExtractor(w, r).ExtractParts("/nodes/")
	if !ok {
		return 0, "", false
	}
	// Expected shape: ["{id}", "labels", "{label}"]
	if len(parts) != 3 || parts[1] != "labels" {
		s.respondError(w, http.StatusBadRequest, "Invalid path")
		return 0, "", false
	}
	id, ok := parseUint64(parts[0])
	if !ok {
		s.respondError(w, http.StatusBadRequest, "Invalid ID format")
		return 0, "", false
	}
	label := parts[2]
	if label == "" {
		s.respondError(w, http.StatusBadRequest, "Label is required")
		return 0, "", false
	}
	return id, label, true
}

// parseUint64 is a thin strconv wrapper that mirrors ExtractUint64's
// internal behaviour but without the embedded response — callers above
// emit the 400 themselves so they can surface a more specific
// "Invalid ID format" alongside the path-shape error.
func parseUint64(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// invalidateTenantSchemaCache drops the cached GraphQL handler for the
// given tenant so the next /graphql request lazy-rebuilds against the
// current label set. Same single-call surface that
// handleSchemaRegenerate (server_handlers.go) uses. Wrapped in a
// helper so the label-mutation handlers don't reach directly into
// server internals — keeps the coupling discoverable when the cache
// shape changes (e.g., if it grows a versioning or stampede-guard
// layer the helper is the single edit point).
func invalidateTenantSchemaCache(s *Server, tenantID string) {
	s.graphqlHandlers.Delete(tenantID)
}
