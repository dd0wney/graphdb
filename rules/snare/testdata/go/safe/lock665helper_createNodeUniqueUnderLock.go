// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/storage/node_operations.go at b6fb6a4, lines 198-244, function GraphStorage.createNodeUniqueUnderLock.
// Label: safe.
// Reason: A helper that PR #665 added. The helper holds the lock with a deferred unlock.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// createNodeUniqueUnderLock is the part of CreateNodeWithUniquePropertyForTenant
// that runs under gs.mu: the closed check, the uniqueness scan and the create.
// The deferred unlock matters: see createNodeUnderLock.
func (gs *GraphStorage) createNodeUniqueUnderLock(
	tenantID string,
	labels []string,
	properties map[string]Value,
	uniqueLabel string,
	uniquePropertyKey string,
	newVal Value,
) (*Node, *wal.Pending, []vectorInsertPlan, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	panicPoint("CreateNodeWithUniquePropertyForTenant")

	if err := gs.checkClosed(); err != nil {
		return nil, nil, nil, err
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
			return nil, nil, nil, fmt.Errorf("uniqueness scan for %s.%s: node %d: %w", uniqueLabel, uniquePropertyKey, existingID, err)
		}
		if existingVal, has := existing.Properties[uniquePropertyKey]; has && valuesEqual(existingVal, newVal) {
			return nil, nil, nil, &UniqueConstraintError{
				Label:             uniqueLabel,
				PropertyKey:       uniquePropertyKey,
				ConflictingNodeID: existingID,
				TenantID:          tid.String(),
			}
		}
	}

	return gs.createNodeLocked(tenantID, labels, properties)
}
