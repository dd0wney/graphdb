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
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "key rotation"))
		return
	}

	response := map[string]any{
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

	// Get events from in-memory logger (for API queries)
	// Note: inMemoryAuditLogger always keeps recent events for API access
	events := s.inMemoryAuditLogger.GetEvents(filter)

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

	response := map[string]any{
		"events": events,
		"count":  len(events),
		"total":  s.inMemoryAuditLogger.GetEventCount(),
	}

	// Add persistent audit info if enabled
	if s.persistentAudit != nil {
		stats := s.persistentAudit.GetStatistics()
		response["persistent_audit"] = map[string]any{
			"enabled":          true,
			"total_persisted":  stats.TotalEvents,
			"total_files":      stats.TotalFiles,
			"total_size_bytes": stats.TotalSize,
			"current_file":     stats.CurrentFile,
		}
	}

	s.respondJSON(w, http.StatusOK, response)
}

// handleSecurityAuditExport exports audit logs in JSON format
func (s *Server) handleSecurityAuditExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	// Get all events from in-memory logger
	events := s.inMemoryAuditLogger.GetEvents(nil)

	// Set headers for file download
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=audit-logs-%s.json", time.Now().Format("2006-01-02")))

	// Encode as JSON
	// Note: json.NewEncoder does not return an error - it always succeeds
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(events); err != nil {
		s.respondError(w, http.StatusInternalServerError, sanitizeError(err, "export logs"))
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

	response := map[string]any{
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

	health := map[string]any{
		"timestamp": time.Now().Format(time.RFC3339),
		"status":    "healthy",
		"components": map[string]any{
			"encryption": map[string]any{
				"enabled": s.encryptionEngine != nil,
			},
			"tls": map[string]any{
				"enabled": s.tlsConfig != nil && s.tlsConfig.Enabled,
			},
			"audit": map[string]any{
				"enabled":      true,
				"event_count":  s.auditLogger.GetEventCount(),
			},
			"authentication": map[string]any{
				"jwt_enabled":    true,
				"apikey_enabled": true,
			},
		},
	}

	// Add key manager stats if available
	if s.keyManager != nil {
		if km, ok := s.keyManager.(*encryption.KeyManager); ok {
			stats := km.GetStatistics()
			health["components"].(map[string]any)["encryption"].(map[string]any)["key_stats"] = stats
		}
	}

	s.respondJSON(w, http.StatusOK, health)
}
