// Package api: property-index HTTP surface.
//
// These routes expose the GraphStorage.PropertyIndex lifecycle that was
// previously reachable only from in-process Go callers (and durably from
// WAL replay via OpCreatePropertyIndex / OpDropPropertyIndex). Closing
// this gap unblocks consumers like Ulysses that want to create indexes
// on hot lookup keys (e.g., character_id, document_id) without having to
// fork the sidecar.
//
// Scope note (NEEDS OWNER REVIEW): property indexes are process-global
// today — see pkg/storage/index_operations.go:80 docstring. Tenant
// isolation is enforced at lookup time (via
// FindNodesByPropertyIndexedForTenant's post-filter), NOT at index
// creation time. Two tenants asking for an index on "title" share one
// underlying index. The routes are therefore registered with
// requireAdmin (server.go) rather than requireAuth+withTenant — wiring
// them under the per-tenant chain would imply isolation the storage
// layer can't currently honor. If the storage layer is later upgraded
// to per-tenant indexes (the deferred item in the docstring above), the
// auth chain here should be re-evaluated.
package api

import (
	"net/http"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// PropertyIndexRequest is the body for POST /property-indexes.
//
// label is intentionally NOT a field on this request: the underlying
// PropertyIndex struct keys on (property_key) alone, so accepting a
// label here would silently ignore it and lie to the consumer. If the
// storage layer grows label-scoped indexes, add the field then.
type PropertyIndexRequest struct {
	PropertyKey string `json:"property_key"`
	// ValueType is one of "string", "int", "float", "bool", "timestamp",
	// "bytes". Default is "string" when omitted. Mismatch between the
	// declared type and the actual property value at insert time will
	// drop that row from the index (see PropertyIndex.Insert).
	ValueType string `json:"value_type,omitempty"`
}

// PropertyIndexResponse is a single index in API responses.
type PropertyIndexResponse struct {
	PropertyKey string `json:"property_key"`
	// ValueType is omitted unless the caller specified it on create —
	// the storage layer does not currently round-trip the declared type
	// once an index is installed. Empty on GET/LIST.
	ValueType string `json:"value_type,omitempty"`
	// Stats are populated on GET /property-indexes/{key} and on
	// the LIST endpoint. Zero values when no nodes have been
	// inserted yet.
	UniqueValues   int     `json:"unique_values"`
	TotalNodes     int     `json:"total_nodes"`
	AvgNodesPerKey float64 `json:"avg_nodes_per_key"`
}

// PropertyIndexListResponse is the body of GET /property-indexes.
type PropertyIndexListResponse struct {
	Indexes []PropertyIndexResponse `json:"indexes"`
	Count   int                     `json:"count"`
}

// parsePropertyIndexValueType maps the request string to a storage.ValueType.
// Defaults to TypeString when empty. Returns false for unknown values so
// the caller can return 400 with the offending input.
func parsePropertyIndexValueType(s string) (storage.ValueType, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "string":
		return storage.TypeString, true
	case "int", "integer":
		return storage.TypeInt, true
	case "float", "double":
		return storage.TypeFloat, true
	case "bool", "boolean":
		return storage.TypeBool, true
	case "timestamp", "time":
		return storage.TypeTimestamp, true
	case "bytes":
		return storage.TypeBytes, true
	default:
		return 0, false
	}
}

// handlePropertyIndexes routes /property-indexes (collection).
func (s *Server) handlePropertyIndexes(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Get(func() { s.listPropertyIndexes(w, r) }).
		Post(func() { s.createPropertyIndex(w, r) }).
		NotAllowed()
}

// handlePropertyIndex routes /property-indexes/{key} (single resource).
func (s *Server) handlePropertyIndex(w http.ResponseWriter, r *http.Request) {
	s.NewMethodRouter(w, r).
		Get(func() { s.getPropertyIndex(w, r) }).
		Delete(func() { s.deletePropertyIndex(w, r) }).
		NotAllowed()
}

// listPropertyIndexes returns every property-index installed on the
// underlying storage. Indexes are process-global — see the package
// docstring above for why this is not tenant-scoped.
func (s *Server) listPropertyIndexes(w http.ResponseWriter, r *http.Request) {
	keys := s.graph.ListPropertyIndexes()
	stats := s.graph.GetIndexStatistics()

	out := make([]PropertyIndexResponse, 0, len(keys))
	for _, key := range keys {
		stat := stats[key]
		out = append(out, PropertyIndexResponse{
			PropertyKey:    key,
			UniqueValues:   stat.UniqueValues,
			TotalNodes:     stat.TotalNodes,
			AvgNodesPerKey: stat.AvgNodesPerKey,
		})
	}

	s.respondJSON(w, http.StatusOK, PropertyIndexListResponse{
		Indexes: out,
		Count:   len(out),
	})
}

// createPropertyIndex installs a new property-key index on storage.
// 201 on success, 409 if an index already exists for the key, 400 on
// validation errors. Pattern mirrors createVectorIndex.
func (s *Server) createPropertyIndex(w http.ResponseWriter, r *http.Request) {
	var req PropertyIndexRequest
	if s.NewRequestDecoder(w, r).DecodeJSON(&req).RespondError() {
		return
	}

	req.PropertyKey = strings.TrimSpace(req.PropertyKey)
	if req.PropertyKey == "" {
		s.respondError(w, http.StatusBadRequest, "property_key is required")
		return
	}

	valueType, ok := parsePropertyIndexValueType(req.ValueType)
	if !ok {
		s.respondError(w, http.StatusBadRequest,
			"unsupported value_type: "+req.ValueType+
				" (expected one of: string, int, float, bool, timestamp, bytes)")
		return
	}

	if s.graph.HasPropertyIndex(req.PropertyKey) {
		// Match vector-index conflict semantics: 409 with a clear
		// "already exists" message rather than 200-with-existing-config.
		// Idempotent retries from the consumer side can branch on this.
		s.respondError(w, http.StatusConflict,
			"Property index already exists for key: "+req.PropertyKey)
		return
	}

	if err := s.graph.CreatePropertyIndex(req.PropertyKey, valueType); err != nil {
		// Race: another caller created it between HasPropertyIndex and
		// CreatePropertyIndex. The storage layer's "already exists"
		// error path is the same surface — translate to 409 so callers
		// see consistent semantics.
		if strings.Contains(err.Error(), "already exists") {
			s.respondError(w, http.StatusConflict,
				"Property index already exists for key: "+req.PropertyKey)
			return
		}
		if isBackendUnsupportedError(err) {
			s.respondError(w, http.StatusNotImplemented,
				"Property index creation not supported by current storage backend")
			return
		}
		s.respondError(w, http.StatusInternalServerError,
			sanitizeError(err, "create property index"))
		return
	}

	// Pull initial stats so the create response shape matches GET.
	stat := s.graph.GetIndexStatistics()[req.PropertyKey]
	s.respondJSON(w, http.StatusCreated, PropertyIndexResponse{
		PropertyKey:    req.PropertyKey,
		ValueType:      req.ValueType,
		UniqueValues:   stat.UniqueValues,
		TotalNodes:     stat.TotalNodes,
		AvgNodesPerKey: stat.AvgNodesPerKey,
	})
}

// getPropertyIndex returns metadata + stats for a single index. 404 if
// the key is unknown.
func (s *Server) getPropertyIndex(w http.ResponseWriter, r *http.Request) {
	propertyKey, ok := s.NewPathExtractor(w, r).ExtractString("/property-indexes/")
	if !ok {
		return
	}

	if !s.graph.HasPropertyIndex(propertyKey) {
		s.respondError(w, http.StatusNotFound,
			"Property index not found: "+propertyKey)
		return
	}

	stat := s.graph.GetIndexStatistics()[propertyKey]
	s.respondJSON(w, http.StatusOK, PropertyIndexResponse{
		PropertyKey:    propertyKey,
		UniqueValues:   stat.UniqueValues,
		TotalNodes:     stat.TotalNodes,
		AvgNodesPerKey: stat.AvgNodesPerKey,
	})
}

// deletePropertyIndex drops the index. 204 on success, 404 if missing.
func (s *Server) deletePropertyIndex(w http.ResponseWriter, r *http.Request) {
	propertyKey, ok := s.NewPathExtractor(w, r).ExtractString("/property-indexes/")
	if !ok {
		return
	}

	if !s.graph.HasPropertyIndex(propertyKey) {
		s.respondError(w, http.StatusNotFound,
			"Property index not found: "+propertyKey)
		return
	}

	if err := s.graph.DropPropertyIndex(propertyKey); err != nil {
		// Same race window as create: a concurrent drop won. Surface
		// the underlying not-found state as 404, anything else as 500.
		if strings.Contains(err.Error(), "does not exist") {
			s.respondError(w, http.StatusNotFound,
				"Property index not found: "+propertyKey)
			return
		}
		// Catch storage-backend stubs (BTreeGraphStorage returns its
		// internal errBTreeBackendUnsupported sentinel — unexported, so
		// match by message) so callers see a real "not implemented"
		// instead of an opaque 500.
		if isBackendUnsupportedError(err) {
			s.respondError(w, http.StatusNotImplemented,
				"Property index drop not supported by current storage backend")
			return
		}
		s.respondError(w, http.StatusInternalServerError,
			sanitizeError(err, "delete property index"))
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// isBackendUnsupportedError reports whether err is the BTreeGraphStorage
// "unsupported" sentinel. That error is unexported in pkg/storage so we
// match by message — fragile but the surface is contained to this file
// and the message is asserted in storage tests.
func isBackendUnsupportedError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "btree backend") &&
		strings.Contains(msg, "unsupported")
}
