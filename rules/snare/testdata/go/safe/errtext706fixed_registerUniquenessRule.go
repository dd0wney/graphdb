// Snare benchmark fixture, set 3, rule graphdb-error-text-in-response.
// Source: pkg/api/handlers_admin_uniqueness.go at 8c9f42e, lines 47-100, function Server.registerUniquenessRule.
// Source: pkg/api/handlers_admin_uniqueness.go at 8c9f42e, lines 143-160, function registerRuleErrorResponse.
// Source: pkg/api/server_helpers.go at 8c9f42e, lines 124-131, function Server.respondError.
// Source: pkg/api/handler_helper.go at 8c9f42e, lines 17-29, function sanitizeError.
// Label: safe.
// Reason: After PR #706 the handler maps a storage error through registerRuleErrorResponse. Only the three sentinel errors keep their text. Any other error goes through sanitizeError.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// registerUniquenessRule handles POST /admin/uniqueness-rules: upsert by
// name (storage.RegisterUniquenessRule is idempotent), 200 with the stored
// rule. Validated with pkg/validation before it reaches storage, so a
// malformed body is a 400 naming the field it failed on, ahead of
// storage's own (differently worded) validation.
func (s *Server) registerUniquenessRule(w http.ResponseWriter, r *http.Request) {
	claims := s.requireAdminClaims(w, r)
	if claims == nil {
		return
	}

	var req storage.UniquenessRule
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := validateUniquenessRuleBody(req); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.graph.RegisterUniquenessRule(req); err != nil {
		status, message := registerRuleErrorResponse(err)
		s.respondError(w, status, message)
		return
	}

	// R5: audit event on register, following handlers_tenant.go's
	// handleCreateTenant pattern.
	s.logAuditEvent(&audit.Event{
		UserID:       claims.UserID,
		Username:     claims.Username,
		Action:       audit.ActionCreate,
		ResourceType: "uniqueness_rule",
		ResourceID:   req.Name,
		Status:       audit.StatusSuccess,
		IPAddress:    getIPAddress(r),
		UserAgent:    r.UserAgent(),
	})

	// Fix round 1 (Minor): respond with what the registry actually holds,
	// not the decoded request body. RegisterUniquenessRule just succeeded
	// under req.Name, so this is always found; the fallback to req only
	// guards an impossible-in-practice race with a concurrent remove of
	// the same name between the two calls above.
	response := req
	for _, stored := range s.graph.UniquenessRules() {
		if stored.Name == req.Name {
			response = stored
			break
		}
	}
	s.respondJSON(w, http.StatusOK, response)
}

// registerRuleErrorResponse maps a RegisterUniquenessRule error to the
// status and message the client receives.
//
// Storage marks the caller's faults with sentinels: ErrUniquenessRuleKeyTaken,
// ErrInvalidUniquenessRule and ErrUniquenessRuleLimit. Any other error (a
// failed write of rules.json, for one) is the server's, and its text carries
// the data-directory path, so it must not reach the client verbatim;
// sanitizeError logs it and returns a generic message.
func registerRuleErrorResponse(err error) (int, string) {
	switch {
	case errors.Is(err, storage.ErrUniquenessRuleKeyTaken):
		return http.StatusConflict, err.Error()
	case errors.Is(err, storage.ErrInvalidUniquenessRule), errors.Is(err, storage.ErrUniquenessRuleLimit):
		return http.StatusBadRequest, err.Error()
	default:
		return http.StatusInternalServerError, sanitizeError(err, "register uniqueness rule")
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
