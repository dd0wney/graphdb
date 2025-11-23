package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/audit"
	"github.com/dd0wney/cluso-graphdb/pkg/encryption"
)

// handleSecurityKeyRotate rotates the encryption key
func (s *Server) handleSecurityKeyRotate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Check if encryption is enabled
	if s.keyManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Encryption not enabled")
		return
	}

	// Type assert to access KeyManager methods
	km, ok := s.keyManager.(*encryption.KeyManager)
	if !ok {
		s.respondError(w, http.StatusInternalServerError, "Invalid key manager")
		return
	}

	// Rotate the key
	newVersion, err := km.RotateKey()
	if err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to rotate key: %v", err))
		return
	}

	response := map[string]interface{}{
		"message":     "Key rotated successfully",
		"new_version": newVersion,
		"timestamp":   time.Now().Format(time.RFC3339),
	}

	s.respondJSON(w, http.StatusOK, response)
}

// handleSecurityAuditLogs retrieves audit logs with optional filtering
func (s *Server) handleSecurityAuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Parse query parameters for filtering
	query := r.URL.Query()

	filter := &audit.Filter{}

	if userID := query.Get("user_id"); userID != "" {
		filter.UserID = userID
	}

	if username := query.Get("username"); username != "" {
		filter.Username = username
	}

	if action := query.Get("action"); action != "" {
		filter.Action = audit.Action(action)
	}

	if resourceType := query.Get("resource_type"); resourceType != "" {
		filter.ResourceType = audit.ResourceType(resourceType)
	}

	if status := query.Get("status"); status != "" {
		filter.Status = audit.Status(status)
	}

	// Parse time filters
	if startTime := query.Get("start_time"); startTime != "" {
		if t, err := time.Parse(time.RFC3339, startTime); err == nil {
			filter.StartTime = &t
		}
	}

	if endTime := query.Get("end_time"); endTime != "" {
		if t, err := time.Parse(time.RFC3339, endTime); err == nil {
			filter.EndTime = &t
		}
	}

	// Get events
	events := s.auditLogger.GetEvents(filter)

	// Parse limit
	limit := 100 // default
	if limitStr := query.Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	// Limit results
	if len(events) > limit {
		events = events[:limit]
	}

	response := map[string]interface{}{
		"events": events,
		"count":  len(events),
		"total":  s.auditLogger.GetEventCount(),
	}

	s.respondJSON(w, http.StatusOK, response)
}

// handleSecurityAuditExport exports audit logs in JSON format
func (s *Server) handleSecurityAuditExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Get all events
	events := s.auditLogger.GetEvents(nil)

	// Set headers for file download
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=audit-logs-%s.json", time.Now().Format("2006-01-02")))

	// Encode as JSON
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(events); err != nil {
		s.respondError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to export logs: %v", err))
		return
	}
}

// handleSecurityKeyInfo retrieves information about encryption keys
func (s *Server) handleSecurityKeyInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Check if encryption is enabled
	if s.keyManager == nil {
		s.respondError(w, http.StatusServiceUnavailable, "Encryption not enabled")
		return
	}

	// Type assert to access KeyManager methods
	km, ok := s.keyManager.(*encryption.KeyManager)
	if !ok {
		s.respondError(w, http.StatusInternalServerError, "Invalid key manager")
		return
	}

	// Get statistics
	stats := km.GetStatistics()

	// Get key metadata
	keys := km.ListKeys()

	response := map[string]interface{}{
		"statistics": stats,
		"keys":       keys,
	}

	s.respondJSON(w, http.StatusOK, response)
}

// handleSecurityHealth checks the health of security components
func (s *Server) handleSecurityHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	health := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"status":    "healthy",
		"components": map[string]interface{}{
			"encryption": map[string]interface{}{
				"enabled": s.encryptionEngine != nil,
			},
			"tls": map[string]interface{}{
				"enabled": s.tlsConfig != nil && s.tlsConfig.Enabled,
			},
			"audit": map[string]interface{}{
				"enabled":      true,
				"event_count":  s.auditLogger.GetEventCount(),
			},
			"authentication": map[string]interface{}{
				"jwt_enabled":    true,
				"apikey_enabled": true,
			},
		},
	}

	// Add key manager stats if available
	if s.keyManager != nil {
		if km, ok := s.keyManager.(*encryption.KeyManager); ok {
			stats := km.GetStatistics()
			health["components"].(map[string]interface{})["encryption"].(map[string]interface{})["key_stats"] = stats
		}
	}

	s.respondJSON(w, http.StatusOK, health)
}
