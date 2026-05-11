package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/masking"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
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
