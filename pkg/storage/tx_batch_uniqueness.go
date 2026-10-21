package storage

import (
	"fmt"

	"github.com/dd0wney/graphdb/pkg/tenantid"
)

// checkCreateAgainstRulesLocked refuses a create of a node with labels and
// properties in tenant tid that the uniqueness rules in reg forbid: the rule
// selection is CreateNodeWithUniquenessRulesForTenant's (createRule), and the
// held-key scan is the unique create's. The Batch create calls it once per
// operation, before anything is written. Caller holds gs.mu.
func (gs *GraphStorage) checkCreateAgainstRulesLocked(reg uniquenessRegistry, tid tenantid.TenantID, labels []string, properties map[string]Value) error {
	rule, err := reg.createRule(labels, properties)
	if err != nil || rule == nil {
		return err
	}
	holders, err := gs.scanKeyHoldersLocked(tid, rule.Label, rule.PropertyKey, properties[rule.PropertyKey])
	if err != nil {
		return err
	}
	if len(holders) > 0 {
		return &UniqueConstraintError{
			Label:             rule.Label,
			PropertyKey:       rule.PropertyKey,
			ConflictingNodeID: holders[0].ID,
			TenantID:          tid.String(),
		}
	}
	return nil
}

// txKeyClaim is one value a commit gives one node under one rule: a buffered
// create's key, or an update that changes a stored node's key.
type txKeyClaim struct {
	rule   UniquenessRule
	val    Value
	nodeID uint64
}

// validateUniquenessLocked refuses the commit, before any mutation, when the
// state it would leave has two holders of one key value under a registered
// rule. The check is on that end state and not on each buffered write in turn,
// because a transaction is one write: a swap of two keys, or a create of a
// value an update in the same transaction gives up, leaves one holder each and
// commits. Creates also get the rest of the create-time selection (createRule):
// a missing required rule, a missing key property and two covering rules each
// refuse, as they do for a direct create.
//
// Only values the commit writes are checked. A duplicate the store already
// held before the rule was registered is not this commit's to refuse, the same
// scope checkSetAgainstRulesLocked has. Caller holds gs.mu.
func (tx *Transaction) validateUniquenessLocked(reg uniquenessRegistry) error {
	claims, err := tx.keyClaimsLocked(reg)
	if err != nil {
		return err
	}
	tid := effectiveTenantID(tx.tenantID)
	for i, c := range claims {
		for j, o := range claims {
			if i != j && sameRuleKey(o.rule, c.rule) && valuesEqual(o.val, c.val) {
				return fmt.Errorf("commit: node %d: %w", c.nodeID, uniqueConflict(c, o.nodeID, tid))
			}
		}
		holders, err := tx.gs.scanKeyHoldersLocked(tid, c.rule.Label, c.rule.PropertyKey, c.val)
		if err != nil {
			return fmt.Errorf("commit: node %d: %w", c.nodeID, err)
		}
		for _, h := range holders {
			if h.ID == c.nodeID || tx.movesOffValue(h.ID, c) {
				continue
			}
			return fmt.Errorf("commit: node %d: %w", c.nodeID, uniqueConflict(c, h.ID, tid))
		}
	}
	return nil
}

// keyClaimsLocked lists every key value the commit writes, in ascending node-ID
// order so the refusal a caller sees does not depend on map order. An update
// of a node created in this transaction is already merged into the buffered
// create (tx.UpdateNode), so it is checked as a create. validateLocked has
// already resolved every update target to this tenant.
func (tx *Transaction) keyClaimsLocked(reg uniquenessRegistry) ([]txKeyClaim, error) {
	var claims []txKeyClaim
	for _, id := range sortedTxIDs(tx.createdNodes) {
		n := tx.createdNodes[id]
		rule, err := reg.createRule(n.Labels, n.Properties)
		if err != nil {
			return nil, fmt.Errorf("commit: node %d: %w", id, err)
		}
		if rule != nil {
			claims = append(claims, txKeyClaim{rule: *rule, val: n.Properties[rule.PropertyKey], nodeID: id})
		}
	}
	if len(reg.rules) == 0 {
		return claims, nil
	}
	for _, id := range sortedTxIDs(tx.updatedNodes) {
		props := tx.updatedNodes[id]
		existing, err := tx.gs.getNodeRefForTenant(id, tx.tenantID)
		if err != nil {
			return nil, fmt.Errorf("commit: update target %d: %w", id, err)
		}
		for _, r := range reg.rules {
			if !containsString(existing.Labels, r.Label) {
				continue
			}
			v, isSet := props[r.PropertyKey]
			if !isSet {
				continue
			}
			if old, has := existing.Properties[r.PropertyKey]; has && valuesEqual(old, v) {
				continue
			}
			claims = append(claims, txKeyClaim{rule: r, val: v, nodeID: id})
		}
	}
	return claims, nil
}

// movesOffValue reports whether this commit updates the stored holder h to a
// different value of c's key, so h no longer holds c.val once it commits.
func (tx *Transaction) movesOffValue(h uint64, c txKeyClaim) bool {
	v, isSet := tx.updatedNodes[h][c.rule.PropertyKey]
	return isSet && !valuesEqual(v, c.val)
}

// sameRuleKey compares what a rule scans, (Label, PropertyKey), not its name.
func sameRuleKey(a, b UniquenessRule) bool {
	return a.Label == b.Label && a.PropertyKey == b.PropertyKey
}

func uniqueConflict(c txKeyClaim, conflicting uint64, tid tenantid.TenantID) *UniqueConstraintError {
	return &UniqueConstraintError{
		Label:             c.rule.Label,
		PropertyKey:       c.rule.PropertyKey,
		ConflictingNodeID: conflicting,
		TenantID:          tid.String(),
	}
}
