// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4^, lines 117-204, function GraphStorage.CreateNodeWithUniquePropertyForTenant.
// Label: vulnerable.
// Reason: The annotated line takes the lock through a mutex Lock call, then the section calls a function and releases the lock with an explicit unlock, so a panic in the call leaves the lock held.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// CreateNodeWithUniquePropertyForTenant creates a node only if no other
// node in the same tenant already has the same value for
// (uniqueLabel, uniquePropertyKey). The check + create runs under a
// single gs.mu.Lock acquisition, so two concurrent calls cannot both
// observe "no conflict" and both create. On conflict, returns a
// *UniqueConstraintError (errors.Is matches ErrUniqueConstraintViolation).
//
// uniqueLabel must be present in labels and the new properties must
// contain uniquePropertyKey. The constraint is enforced for the named
// label only, mirroring the per-label scope in pkg/constraints —
// nodes with other labels can hold the same property value.
//
// Introduced for B-lite (docs/COORD_DEPLOY_SPIKE_2026-05-10.md §5.2 /
// §10 PR 1) so the GraphQL :Claim resolver can enforce one active claim
// per for_task. Generic enough to reuse if other coord types need
// uniqueness; a fully general HTTP/GraphQL constraint API (B-full) is
// still deferred per the spike.
func (gs *GraphStorage) CreateNodeWithUniquePropertyForTenant(
	tenantID string,
	labels []string,
	properties map[string]Value,
	uniqueLabel string,
	uniquePropertyKey string,
) (*Node, error) {
	if uniqueLabel == "" || uniquePropertyKey == "" {
		return nil, fmt.Errorf("uniqueLabel and uniquePropertyKey are required")
	}

	// Caller-side sanity: the new node must carry the labelled property
	// so the uniqueness rule is meaningful.
	if !containsString(labels, uniqueLabel) {
		return nil, fmt.Errorf("uniqueLabel %q must be present in labels", uniqueLabel)
	}
	newVal, ok := properties[uniquePropertyKey]
	if !ok {
		return nil, fmt.Errorf("property %q is required for uniqueness check", uniquePropertyKey)
	}

	// ruleid: graphdb-lock-call-without-defer
	gs.mu.Lock()

	if err := gs.checkClosed(); err != nil {
		gs.mu.Unlock()
		return nil, err
	}

	tid := effectiveTenantID(tenantID)
	for _, existingID := range gs.membershipNodeIDsByLabelLocked(tid, uniqueLabel) {
		existing, err := gs.resolveNodeRefLocked(existingID)
		if err != nil {
			if errors.Is(err, ErrNodeNotFound) {
				// Legitimately absent (e.g. a tombstone the membership run
				// has not dropped yet) — not a conflict.
				continue
			}
			// An unreadable record must not be read as "no conflict": the
			// store cannot tell whether existingID holds newVal, so it must
			// not answer "no conflict" for it. Abort the whole create and
			// wrap ErrRecordUnreadable so the caller can tell this apart
			// from both success and a real *UniqueConstraintError.
			gs.mu.Unlock()
			return nil, fmt.Errorf("uniqueness scan for %s.%s: node %d: %w", uniqueLabel, uniquePropertyKey, existingID, err)
		}
		if existingVal, has := existing.Properties[uniquePropertyKey]; has && valuesEqual(existingVal, newVal) {
			gs.mu.Unlock()
			return nil, &UniqueConstraintError{
				Label:             uniqueLabel,
				PropertyKey:       uniquePropertyKey,
				ConflictingNodeID: existingID,
				TenantID:          tid.String(),
			}
		}
	}

	node, walPending, vectorPlans, err := gs.createNodeLocked(tenantID, labels, properties)
	gs.mu.Unlock()
	// HNSW insert(s) off-lock (Track P item 3 / H2); see CreateNodeWithTenant.
	gs.applyNodeVectorInserts(vectorPlans)
	// Wait for WAL durability after lock release (group commit, Track P item 1).
	// A wait error wraps ErrWALWriteFailed; see CreateNodeWithTenant.
	if waitErr := gs.waitWALPending(wal.OpCreateNode, walPending); err == nil {
		err = waitErr
	}
	// R2.1: dispatch after lock release. See CreateNodeWithTenant.
	if node != nil {
		gs.notifyNodeCreated(context.Background(), node)
	}
	return node, err
}
