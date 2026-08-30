package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/validation"
)

// sanitizeError converts an internal error to a user-safe message.
// Internal details like file paths and stack traces are logged but not exposed.
func sanitizeError(err error, operation string) string {
	if err == nil {
		return ""
	}

	// Log the full error for debugging
	log.Printf("ERROR [%s]: %v", operation, err)

	// Return generic message to client
	return fmt.Sprintf("%s failed", operation)
}

// noteIncompleteEnumeration marks a PAGINATED response as incomplete and
// reports whether it could.
//
// It returns false when err is not an incomplete enumeration, which means
// something else failed and there is no partial page worth serving. The caller
// then refuses. It returns true after setting the header, and the caller
// serves the page it already holds.
//
// WHY A PAGE IS SERVED HERE AND A WHOLE-GRAPH ENUMERATION IS NOT.
// ADR 0003 chose a partial result at the storage layer so that one damaged
// record could not become a total outage for a tenant. Refusing here would put
// that outage back one level up: every page of the tenant's list would fail,
// intact ones included, and the caller would never receive a cursor to step
// past the gap. A page is already a fragment by construction, and the caller
// holds a cursor for the rest, so a short page plus an explicit count is a
// truthful answer. A whole-graph enumeration has neither property, and
// respondIncompleteEnumeration below still refuses for those.
//
// The header carries a COUNT and never an ID. storage.SkippedRecordCount is
// the only accessor storage exposes, so the IDs cannot reach a response even
// by mistake. They go to the log, with the byte offset, exactly as getNode
// does for the single-record case.
func (s *Server) noteIncompleteEnumeration(w http.ResponseWriter, operation string, err error) bool {
	skipped, ok := storage.SkippedRecordCount(err)
	if !ok {
		return false
	}
	log.Printf("ERROR [%s]: %d record(s) would not decode for the requesting tenant, "+
		"serving the partial page: %v", operation, skipped, err)
	w.Header().Set(IncompleteEnumerationHeader, strconv.Itoa(skipped))
	return true
}

// respondIncompleteEnumeration refuses a WHOLE-GRAPH enumeration that skipped a
// record it could not decode (ADR 0003).
//
// Paginated endpoints do not use this. They serve the partial page through
// noteIncompleteEnumeration above. This one is for the enumerations that have
// no meaningful fragment: an LSA build, a search-index build, and the
// index-count that follows one. An index built from a short corpus answers "no
// match" for every document the scan skipped, and it keeps answering that until
// somebody rebuilds it, so a partial answer is worse than a refusal.
//
// WHY THIS IS SAFE TO TELL THE CALLER, and why it is NOT the leak that PR #513
// exists to prevent:
//
// Every enumeration endpoint is scoped to getTenantFromContext(r), and the ID
// list it walks comes from the caller's own membership run
// (membershipNodeIDsForTenantLocked and its by-label variant). No
// caller-supplied ID names a resource on these paths, so a stranger asking for
// its own list can never reach another tenant's damaged record: it gets its
// own list, unchanged, whether or not some other tenant's snapshot section
// rotted. There is no second principal to compare against here, which is what
// makes this different from GET /nodes/{id}.
//
// Ownership is established in the storage layer, not in the handler — the same
// property PR #526 relies on for the single-record case. The record's tenant
// string lives inside the record that would not decode, so the handler could
// not establish ownership even if it tried; the membership run establishes it
// before any record is touched.
//
// 500, not 503, for the reason getNode gives: 503 tells the caller to retry,
// and damaged bytes on disk do not repair themselves.
//
// The response body is a fixed sentence. The raw error names record IDs and a
// byte offset into the snapshot file, which belong in the operator's log and
// not in a response body.
func (s *Server) respondIncompleteEnumeration(w http.ResponseWriter, operation string, err error) {
	log.Printf("ERROR [%s]: the enumeration is incomplete for the requesting tenant: %v", operation, err)
	s.respondError(w, http.StatusInternalServerError, fmt.Sprintf(
		"%s could not read every stored record, so the result would be incomplete. "+
			"The data on disk is damaged; restore the affected records from a backup. "+
			"Retrying will not help.", operation))
}

// clientError carries a user-safe message in Error() while preserving the
// original error chain via Unwrap(). Use when the returned error will be
// serialized into an HTTP response body (so callers cannot leak internals)
// but downstream code may still need errors.Is / errors.As.
type clientError struct {
	operation string
	inner     error
}

func (e *clientError) Error() string { return e.operation + " failed" }
func (e *clientError) Unwrap() error { return e.inner }

// wrapForClient logs the inner error with operation context and returns a
// *clientError. Replaces the deprecated `fmt.Errorf("%s", sanitizeError(...))`
// pattern, which threw away the wrap chain.
func wrapForClient(err error, operation string) error {
	if err == nil {
		return nil
	}
	log.Printf("ERROR [%s]: %v", operation, err)
	return &clientError{operation: operation, inner: err}
}

// requestDecoder decodes and validates request bodies.
// It provides a fluent interface for common request handling patterns.
type requestDecoder struct {
	r          *http.Request
	w          http.ResponseWriter
	server     *Server
	err        error
	statusCode int
}

// NewRequestDecoder creates a new request decoder for the given request.
func (s *Server) NewRequestDecoder(w http.ResponseWriter, r *http.Request) *requestDecoder {
	return &requestDecoder{
		r:      r,
		w:      w,
		server: s,
	}
}

// DecodeJSON decodes the request body into the provided struct.
// Returns the decoder for chaining. Check HasError() after calling.
func (rd *requestDecoder) DecodeJSON(v any) *requestDecoder {
	if rd.err != nil {
		return rd
	}
	if err := json.NewDecoder(rd.r.Body).Decode(v); err != nil {
		rd.err = fmt.Errorf("invalid request body: %w", err)
		rd.statusCode = http.StatusBadRequest
	}
	return rd
}

// ValidateNode validates a node request.
// Returns the decoder for chaining.
func (rd *requestDecoder) ValidateNode(req *NodeRequest) *requestDecoder {
	if rd.err != nil {
		return rd
	}
	validationReq := validation.NodeRequest{
		Labels:     req.Labels,
		Properties: req.Properties,
	}
	if err := validation.ValidateNodeRequest(&validationReq); err != nil {
		rd.err = err
		rd.statusCode = http.StatusBadRequest
	}
	return rd
}

// ValidateEdge validates an edge request.
// Returns the decoder for chaining.
func (rd *requestDecoder) ValidateEdge(req *EdgeRequest) *requestDecoder {
	if rd.err != nil {
		return rd
	}
	validationReq := validation.EdgeRequest{
		FromNodeID: req.FromNodeID,
		ToNodeID:   req.ToNodeID,
		Type:       req.Type,
		Properties: req.Properties,
	}
	if err := validation.ValidateEdgeRequest(&validationReq); err != nil {
		rd.err = err
		rd.statusCode = http.StatusBadRequest
	}
	return rd
}

// HasError returns true if any error occurred during decoding/validation.
func (rd *requestDecoder) HasError() bool {
	return rd.err != nil
}

// Error returns the error if any occurred.
func (rd *requestDecoder) Error() error {
	return rd.err
}

// RespondError sends the error response and returns true if there was an error.
// Returns false if no error occurred.
func (rd *requestDecoder) RespondError() bool {
	if rd.err == nil {
		return false
	}
	rd.server.respondError(rd.w, rd.statusCode, rd.err.Error())
	return true
}

// pathIDExtractor extracts IDs from URL paths.
type pathIDExtractor struct {
	w      http.ResponseWriter
	server *Server
	path   string
}

// NewPathExtractor creates a new path extractor.
func (s *Server) NewPathExtractor(w http.ResponseWriter, r *http.Request) *pathIDExtractor {
	return &pathIDExtractor{
		w:      w,
		server: s,
		path:   r.URL.Path,
	}
}

// ExtractUint64 extracts a uint64 ID from the path after the given prefix.
// Returns the ID and true on success, or 0 and false on error (error response sent).
func (pe *pathIDExtractor) ExtractUint64(prefix string) (uint64, bool) {
	if !strings.HasPrefix(pe.path, prefix) {
		pe.server.respondError(pe.w, http.StatusBadRequest, "Invalid path")
		return 0, false
	}
	idStr := pe.path[len(prefix):]
	// Remove trailing slash if present
	idStr = strings.TrimSuffix(idStr, "/")

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		pe.server.respondError(pe.w, http.StatusBadRequest, "Invalid ID format")
		return 0, false
	}
	return id, true
}

// ExtractString extracts a string segment from the path after the given
// prefix, trimming any trailing slash. Returns the segment and true on
// success. If the path does not have the prefix or the segment is empty
// after trimming, sends a 400 response and returns ("", false).
//
// Use this for string identifiers (property names, tenant slugs, API key
// names) where ExtractUint64 doesn't fit.
func (pe *pathIDExtractor) ExtractString(prefix string) (string, bool) {
	if !strings.HasPrefix(pe.path, prefix) {
		pe.server.respondError(pe.w, http.StatusBadRequest, "Invalid path")
		return "", false
	}
	segment := strings.TrimSuffix(pe.path[len(prefix):], "/")
	if segment == "" {
		pe.server.respondError(pe.w, http.StatusBadRequest, "Path segment is required")
		return "", false
	}
	return segment, true
}

// ExtractParts trims the prefix from the path and returns the remaining
// segments split by "/". Returns the segments and true on success. If
// the prefix doesn't match, sends 400 and returns (nil, false). A
// trailing slash is trimmed before splitting (so "a/b/" yields ["a", "b"]).
//
// Use this for multi-segment subpaths like "/api/v1/tenants/{id}/usage"
// where the caller needs to inspect the trailing action segment.
func (pe *pathIDExtractor) ExtractParts(prefix string) ([]string, bool) {
	if !strings.HasPrefix(pe.path, prefix) {
		pe.server.respondError(pe.w, http.StatusBadRequest, "Invalid path")
		return nil, false
	}
	rest := strings.TrimSuffix(pe.path[len(prefix):], "/")
	if rest == "" {
		pe.server.respondError(pe.w, http.StatusBadRequest, "Path segment is required")
		return nil, false
	}
	return strings.Split(rest, "/"), true
}

// propertyConverter converts and sanitizes properties.
type propertyConverter struct{}

// newPropertyConverter creates a new property converter.
func newPropertyConverter() *propertyConverter {
	return &propertyConverter{}
}

// ConvertAndSanitize sanitizes the input properties and converts them to storage.Value format.
func (pc *propertyConverter) ConvertAndSanitize(props map[string]any, converter func(any) storage.Value) map[string]storage.Value {
	sanitized := storage.SanitizePropertyMap(props)
	result := make(map[string]storage.Value)
	for k, v := range sanitized {
		result[k] = converter(v)
	}
	return result
}

// methodRouter routes requests based on HTTP method.
// Provides a cleaner alternative to switch statements for method routing.
type methodRouter struct {
	w       http.ResponseWriter
	r       *http.Request
	server  *Server
	handled bool
}

// NewMethodRouter creates a new method router.
func (s *Server) NewMethodRouter(w http.ResponseWriter, r *http.Request) *methodRouter {
	return &methodRouter{
		w:      w,
		r:      r,
		server: s,
	}
}

// Get handles GET requests with the provided handler.
func (mr *methodRouter) Get(handler func()) *methodRouter {
	if !mr.handled && mr.r.Method == http.MethodGet {
		handler()
		mr.handled = true
	}
	return mr
}

// Head handles HEAD requests with the provided handler. Per RFC 9110 §9.3.2
// HEAD must return the same headers as GET would; the body is suppressed.
// Handlers registered here typically use the same primitive(s) as the
// matching Get handler but skip body serialization — useful for cheap
// counts (X-Total-Count header) and existence checks.
func (mr *methodRouter) Head(handler func()) *methodRouter {
	if !mr.handled && mr.r.Method == http.MethodHead {
		handler()
		mr.handled = true
	}
	return mr
}

// Post handles POST requests with the provided handler.
func (mr *methodRouter) Post(handler func()) *methodRouter {
	if !mr.handled && mr.r.Method == http.MethodPost {
		handler()
		mr.handled = true
	}
	return mr
}

// Put handles PUT requests with the provided handler.
func (mr *methodRouter) Put(handler func()) *methodRouter {
	if !mr.handled && mr.r.Method == http.MethodPut {
		handler()
		mr.handled = true
	}
	return mr
}

// Delete handles DELETE requests with the provided handler.
func (mr *methodRouter) Delete(handler func()) *methodRouter {
	if !mr.handled && mr.r.Method == http.MethodDelete {
		handler()
		mr.handled = true
	}
	return mr
}

// NotAllowed sends a 405 response if no method matched.
func (mr *methodRouter) NotAllowed() {
	if !mr.handled {
		mr.server.respondError(mr.w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}
