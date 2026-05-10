package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func (s *Server) nodeToResponse(node *storage.Node) *NodeResponse {
	props := make(map[string]any)
	for k, v := range node.Properties {
		props[k] = valueToInterface(v)
	}

	return &NodeResponse{
		ID:         node.ID,
		Labels:     node.Labels,
		Properties: props,
	}
}

func (s *Server) edgeToResponse(edge *storage.Edge) *EdgeResponse {
	props := make(map[string]any)
	for k, v := range edge.Properties {
		props[k] = valueToInterface(v)
	}

	return &EdgeResponse{
		ID:         edge.ID,
		FromNodeID: edge.FromNodeID,
		ToNodeID:   edge.ToNodeID,
		Type:       edge.Type,
		Properties: props,
		Weight:     edge.Weight,
	}
}

func (s *Server) convertToValue(v any) storage.Value {
	switch val := v.(type) {
	case string:
		return storage.StringValue(val)
	case float64:
		// JSON numbers are always float64
		if val == float64(int64(val)) {
			return storage.IntValue(int64(val))
		}
		return storage.FloatValue(val)
	case bool:
		return storage.BoolValue(val)
	default:
		return storage.StringValue(fmt.Sprintf("%v", v))
	}
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
func valueToInterface(v storage.Value) any {
	switch v.Type {
	case storage.TypeString:
		s, err := v.AsString()
		if err != nil {
			return v.Data
		}
		return s
	case storage.TypeInt:
		i, err := v.AsInt()
		if err != nil {
			return v.Data
		}
		return i
	case storage.TypeFloat:
		f, err := v.AsFloat()
		if err != nil {
			return v.Data
		}
		return f
	case storage.TypeBool:
		b, err := v.AsBool()
		if err != nil {
			return v.Data
		}
		return b
	case storage.TypeTimestamp:
		t, err := v.AsTimestamp()
		if err != nil {
			return v.Data
		}
		return t.UTC().Format(time.RFC3339)
	case storage.TypeBytes:
		// base64 is the right JSON encoding for raw bytes
		return v.Data
	case storage.TypeVector:
		vec, err := v.AsVector()
		if err != nil {
			return v.Data
		}
		return vec
	case storage.TypeStringArray:
		arr, err := v.AsStringArray()
		if err != nil {
			return v.Data
		}
		return arr
	case storage.TypeIntArray:
		arr, err := v.AsIntArray()
		if err != nil {
			return v.Data
		}
		return arr
	case storage.TypeFloatArray:
		arr, err := v.AsFloatArray()
		if err != nil {
			return v.Data
		}
		return arr
	case storage.TypeBoolArray:
		arr, err := v.AsBoolArray()
		if err != nil {
			return v.Data
		}
		return arr
	default:
		return v.Data
	}
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
