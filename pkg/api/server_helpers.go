package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func (s *Server) nodeToResponse(node *storage.Node) *NodeResponse {
	props := make(map[string]any)
	for k, v := range node.Properties {
		props[k] = v.Data
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
		props[k] = v.Data
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
