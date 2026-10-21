package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/validation"
)

// onConflictMessage is the 400 text for an on_conflict value the routes do not
// accept. It names the accepted values so a client need not read the docs.
const onConflictMessage = `on_conflict must be "error" or "merge"`

// noRuleForUpsertMessage is the 400 text when no uniqueness rule covers the
// labels of an upsert. It names no rule, because none matched.
const noRuleForUpsertMessage = "no uniqueness rule covers these labels; on_conflict=merge needs one"

// wantsMerge reads the on_conflict query parameter of POST /nodes and POST
// /nodes/batch. Absent and "error" keep the create behaviour, "merge" asks for
// an upsert. Any other value gives ok == false, and the caller sends a 400.
func wantsMerge(r *http.Request) (merge, ok bool) {
	switch r.URL.Query().Get("on_conflict") {
	case "", "error":
		return false, true
	case "merge":
		return true, true
	default:
		return false, false
	}
}

// upsertNode handles POST /nodes?on_conflict=merge (spec section 5). The key
// is the registered uniqueness rule for the labels, never a caller choice, so
// a plain create of the same key cannot make a second node.
func (s *Server) upsertNode(w http.ResponseWriter, r *http.Request) {
	var req NodeRequest
	decoder := s.NewRequestDecoder(w, r)
	decoder.DecodeJSON(&req).ValidateNode(&req)
	if decoder.RespondError() {
		return
	}

	converter := newPropertyConverter()
	props := converter.ConvertAndSanitize(req.Properties, s.convertToValue)

	node, created, err := s.graph.UpsertNodeWithUniquenessRulesForTenant(getTenantFromContext(r), req.Labels, props)
	if err != nil {
		if errors.Is(err, storage.ErrWALWriteFailed) {
			// The change applied in memory, and storage returns the node and
			// the right created value with this error.
			s.respondUpsertWALWriteFailed(r.Context(), w, err, node, created)
			return
		}
		s.respondUpsertError(w, err)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	s.respondJSON(w, status, NodeUpsertResponse{NodeResponse: *s.nodeToResponse(r.Context(), node), Created: created})
}

// respondUpsertError maps a refused upsert to its response. The refusals that
// the create shares go through respondNodeWriteRefusal, so the two routes
// cannot drift apart. Nothing here names a uniqueness rule (ADR 0001, R4).
func (s *Server) respondUpsertError(w http.ResponseWriter, err error) {
	if s.respondNodeWriteRefusal(w, err) {
		return
	}
	var ambiguous *storage.UpsertAmbiguousError
	if errors.As(err, &ambiguous) {
		s.respondJSON(w, http.StatusConflict, UpsertAmbiguousResponse{
			ErrorResponse: ErrorResponse{
				Error:   http.StatusText(http.StatusConflict),
				Message: err.Error(),
				Code:    http.StatusConflict,
			},
			ConflictingNodeIDs: ambiguous.NodeIDs,
		})
		return
	}
	var differ *storage.UpsertLabelsDifferError
	if errors.As(err, &differ) {
		s.respondJSON(w, http.StatusConflict, UpsertLabelsDifferResponse{
			ErrorResponse: ErrorResponse{
				Error:   http.StatusText(http.StatusConflict),
				Message: err.Error(),
				Code:    http.StatusConflict,
			},
			ConflictingNodeID: differ.NodeID,
			MissingLabels:     differ.Missing,
		})
		return
	}
	if errors.Is(err, storage.ErrNoUniquenessRule) {
		s.respondError(w, http.StatusBadRequest, noRuleForUpsertMessage)
		return
	}
	// ErrMultipleUniquenessRules lands here: its text names the rules, so only
	// the generic sentence leaves the server.
	s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "create node"))
}

// respondUpsertWALWriteFailed answers an upsert whose WAL append failed with
// the NodeNotDurableResponse fields plus "created".
func (s *Server) respondUpsertWALWriteFailed(ctx context.Context, w http.ResponseWriter, err error, node *storage.Node, created bool) {
	logWALWriteFailed(err)
	resp := NodeUpsertNotDurableResponse{
		NodeNotDurableResponse: NodeNotDurableResponse{notDurableFields: newNotDurableFields()},
		Created:                created,
	}
	if node != nil {
		resp.NodeResponse = *s.nodeToResponse(ctx, node)
	}
	s.respondJSON(w, http.StatusAccepted, resp)
}

// handleBatchNodeUpsert handles POST /nodes/batch?on_conflict=merge. Each item
// is one upsert, in request order, and the batch is not atomic: a refused item
// is an error entry and the others still apply (CC7, CC20).
func (s *Server) handleBatchNodeUpsert(w http.ResponseWriter, r *http.Request, req BatchNodeRequest) {
	start := time.Now()
	tenantID := getTenantFromContext(r)
	nodes := make([]*NodeUpsertResponse, 0, len(req.Nodes))
	converter := newPropertyConverter()
	var errs []BatchItemError
	created, matched := 0, 0

	for i, nodeReq := range req.Nodes {
		validationReq := validation.NodeRequest{Labels: nodeReq.Labels, Properties: nodeReq.Properties}
		if err := validation.ValidateNodeRequest(&validationReq); err != nil {
			errs = append(errs, BatchItemError{Index: i, Error: err.Error()})
			continue
		}

		props := converter.ConvertAndSanitize(nodeReq.Properties, s.convertToValue)
		node, wasCreated, err := s.graph.UpsertNodeWithUniquenessRulesForTenant(tenantID, nodeReq.Labels, props)
		if err != nil {
			errs = append(errs, BatchItemError{Index: i, Error: batchUpsertItemError(err)})
			continue
		}

		if wasCreated {
			created++
		} else {
			matched++
		}
		nodes = append(nodes, &NodeUpsertResponse{NodeResponse: *s.nodeToResponse(r.Context(), node), Created: wasCreated})
	}

	s.respondJSON(w, http.StatusCreated, BatchNodeUpsertResponse{
		Nodes:   nodes,
		Created: created,
		Matched: matched,
		Time:    time.Since(start).String(),
		Failed:  len(errs),
		Errors:  errs,
	})
}

// batchUpsertItemError is the text a refused batch upsert item reports. A WAL
// failure gets the fixed sentence, because the wrapped disk error must not
// leave the server (the write applied in memory, and the item still counts as
// failed, as in the batch create). Every other error follows
// batchCreateItemError.
func batchUpsertItemError(err error) string {
	if errors.Is(err, storage.ErrWALWriteFailed) {
		logWALWriteFailed(err)
		return walWriteFailedMessage
	}
	return batchCreateItemError(err)
}
