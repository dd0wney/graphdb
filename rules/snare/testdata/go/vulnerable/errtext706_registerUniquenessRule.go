// Snare benchmark fixture, set 3, rule graphdb-error-text-in-response.
// Source: pkg/api/handlers_admin_uniqueness.go at 8c9f42e^, lines 47-103, function Server.registerUniquenessRule.
// Source: pkg/api/server_helpers.go at 8c9f42e^, lines 124-131, function Server.respondError.
// Label: vulnerable.
// Reason: Before PR #706 the handler sent err.Error() of a RegisterUniquenessRule failure to the client. The text of a failed rules.json write carries the data-directory path.
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
		status := http.StatusBadRequest
		if errors.Is(err, storage.ErrUniquenessRuleKeyTaken) {
			status = http.StatusConflict
		}
		// ruleid: graphdb-error-text-in-response
		s.respondError(w, status, err.Error())
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

func (s *Server) respondError(w http.ResponseWriter, status int, message string) {
	response := ErrorResponse{
		Error:   http.StatusText(status),
		Message: message,
		Code:    status,
	}
	s.respondJSON(w, status, response)
}
