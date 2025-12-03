package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/validation"
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

// requestDecoder decodes and validates request bodies.
// It provides a fluent interface for common request handling patterns.
type requestDecoder struct {
	r           *http.Request
	w           http.ResponseWriter
	server      *Server
	err         error
	statusCode  int
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
