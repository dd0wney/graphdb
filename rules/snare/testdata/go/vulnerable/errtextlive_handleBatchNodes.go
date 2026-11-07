// Snare benchmark fixture, set 3, rule graphdb-error-text-in-response.
// Source: pkg/api/handlers_nodes.go at b9bc9d2, lines 227-250, function batchCreateItemError.
// Source: pkg/api/handlers_nodes.go at b9bc9d2, lines 403-482, function Server.handleBatchNodes.
// Label: vulnerable.
// Reason: At b9bc9d2, POST batch nodes reports batchCreateItemError(err) per failed item. A WAL wait error wraps ErrWALWriteFailed and the disk error. batchCreateItemError falls through to err.Error(), so the disk error text reaches the client.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// batchCreateItemError is the text a refused batch item reports. It follows
// createNode's mapping where storage's own text would name a rule (ADR 0001,
// R4): a missing required rule names only the caller's label, and a write
// that matches several rules gets a generic message. The upsert errors
// (ErrUpsertAmbiguous, ErrUpsertLabelsDiffer, ErrUpsertKeyNotRegistered) carry
// node IDs, labels and the caller's own key, never a rule, so their text stays.
// ErrNoUniquenessRule gets the single route's sentence, which says what to do.
// Every other error keeps the text the batch has always reported.
func batchCreateItemError(err error) string {
	if errors.Is(err, storage.ErrNoUniquenessRule) {
		return noRuleForUpsertMessage
	}
	var missing *storage.RequiredRuleMissingError
	if errors.As(err, &missing) {
		return fmt.Sprintf(
			"a required uniqueness rule for label %q is not registered; contact the administrator",
			missing.Label,
		)
	}
	if errors.Is(err, storage.ErrMultipleUniquenessRules) {
		return sanitizeError(err, "create node")
	}
	// ruleid: graphdb-error-text-in-response
	return err.Error()
}

func (s *Server) handleBatchNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.respondError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	merge, ok := wantsMerge(r)
	if !ok {
		s.respondError(w, http.StatusBadRequest, onConflictMessage)
		return
	}

	var req BatchNodeRequest
	decoder := s.NewRequestDecoder(w, r)
	decoder.DecodeJSON(&req)
	if decoder.RespondError() {
		return
	}

	// Validate batch size
	if err := validation.ValidateBatchSize(len(req.Nodes)); err != nil {
		s.respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	if merge {
		s.handleBatchNodeUpsert(w, r, req)
		return
	}

	start := time.Now()
	tenantID := getTenantFromContext(r)
	nodes := make([]*NodeResponse, 0, len(req.Nodes))
	converter := newPropertyConverter()

	// #455: track skipped items so callers can tell "dropped" from
	// "never sent" instead of a bare Created count. Index refers to the
	// item's position in req.Nodes (the request), not the response —
	// dropped items never make it into `nodes`, so a response-relative
	// index would be meaningless. This is purely additive: the existing
	// Nodes/Created/Time fields and partial-success behaviour (CC7,
	// consumer_contract_jailgraph_test.go) are untouched.
	var errs []BatchItemError

	for i, nodeReq := range req.Nodes {
		// Validate each node request
		validationReq := validation.NodeRequest{
			Labels:     nodeReq.Labels,
			Properties: nodeReq.Properties,
		}
		if err := validation.ValidateNodeRequest(&validationReq); err != nil {
			errs = append(errs, BatchItemError{Index: i, Error: err.Error()})
			continue // Skip invalid nodes
		}

		// Convert and sanitize properties
		props := converter.ConvertAndSanitize(nodeReq.Properties, s.convertToValue)

		// Audit A6a: scoped create. Through the uniqueness registry, as
		// createNode does: a batch item is a write like any other, and
		// CreateNodeWithTenant here stored a duplicate for every repeated key.
		node, err := s.graph.CreateNodeWithUniquenessRulesForTenant(tenantID, nodeReq.Labels, props)
		if err != nil {
			errs = append(errs, BatchItemError{Index: i, Error: batchCreateItemError(err)})
			continue
		}

		nodes = append(nodes, s.nodeToResponse(r.Context(), node))
	}

	response := BatchNodeResponse{
		Nodes:   nodes,
		Created: len(nodes),
		Time:    time.Since(start).String(),
		Failed:  len(errs),
		Errors:  errs,
	}

	s.respondJSON(w, http.StatusCreated, response)
}
