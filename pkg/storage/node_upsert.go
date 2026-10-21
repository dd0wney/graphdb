package storage

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/dd0wney/graphdb/pkg/tenantid"
	"github.com/dd0wney/graphdb/pkg/wal"
)

// UpsertNodeForTenant finds the node of tenantID that has keyLabel and whose
// keyProperty equals properties[keyProperty]. If none exists it creates a node
// with labels and properties (created == true). If one exists it merges
// properties into it (created == false): a request property overrides the
// stored one, and a stored property the request omits stays. A caller removes
// a property through PatchNodeForTenant, not through an upsert.
//
// Why it exists: create, then look up on a conflict, then update is three
// round trips with a race between the last two. Here the find and the write
// run under one gs.mu hold, so concurrent upserts of one key make one node and
// exactly one call reports created == true.
//
// The call refuses, and writes nothing, in these cases:
//   - More than one node holds the key: *UpsertAmbiguousError. The scan counts
//     every match, because a silent pick would update a node the caller did
//     not mean.
//   - The matched node lacks a label of the request: *UpsertLabelsDifferError.
//     Storage has no label mutation, so accepting the request would drop the
//     label without a word. A request whose labels are a subset of the stored
//     labels is accepted.
//   - A registered uniqueness rule covers labels with a key other than
//     (keyLabel, keyProperty): *UpsertKeyNotRegisteredError. Every covering
//     rule must be the key, because the create branch checks only the key.
//     Without the refusal, an upsert keyed on Host.hostname could give a
//     second node an ip value that the Host.ip rule says only one node may hold.
//   - The merge would give the matched node a value that breaks a registered
//     rule on one of the node's stored labels, for a label the request did not
//     name: *UniqueConstraintError.
//
// A match where every request property is already stored with an equal value
// writes no WAL entry and sends no observer notification, so repeated
// idempotent ingestion does not grow the WAL.
//
// With no registered rule for labels, the upsert protects keyProperty against
// other upserts, but not against plain creates: CreateNodeWithTenant does not
// consult the key, so a plain create can still make a second node with the
// same value. Register a rule (RegisterUniquenessRule) to close that gap.
//
// A WAL wait failure wraps ErrWALWriteFailed and is returned together with the
// node and the correct created value, because the change is applied in memory.
// The returned node is the caller's own copy.
func (gs *GraphStorage) UpsertNodeForTenant(
	tenantID string,
	labels []string,
	properties map[string]Value,
	keyLabel, keyProperty string,
) (*Node, bool, error) {
	if keyLabel == "" || keyProperty == "" {
		return nil, false, fmt.Errorf("keyLabel and keyProperty are required")
	}
	if !containsString(labels, keyLabel) {
		return nil, false, fmt.Errorf("keyLabel %q must be present in labels", keyLabel)
	}
	if _, ok := properties[keyProperty]; !ok {
		return nil, false, fmt.Errorf("property %q is required for the upsert key", keyProperty)
	}

	// The registry is read here, before gs.mu is taken, and never while gs.mu
	// is held: rulesMu and gs.mu are not ordered anywhere else, so nesting
	// them would invite a deadlock. A rule registered after this read is not
	// seen, the same window CreateNodeWithUniquenessRulesForTenant has.
	rules := gs.UniquenessRules()
	if !upsertKeyAllowed(rules, labels, keyLabel, keyProperty) {
		return nil, false, &UpsertKeyNotRegisteredError{KeyLabel: keyLabel, KeyProperty: keyProperty}
	}

	res, err := gs.upsertNodeUnderLock(tenantID, labels, properties, keyLabel, keyProperty, rules)
	if err != nil {
		return nil, false, err
	}
	return res.finish(gs)
}

// UpsertNodeWithUniquenessRulesForTenant selects the upsert key from the
// uniqueness-rules registry (ADR 0001), as CreateNodeWithUniquenessRulesForTenant
// does, and calls UpsertNodeForTenant. REST uses this form so an upsert can
// only run on a key that the registry enforces for plain creates too.
//
// A required rule that is not registered refuses the call with
// *RequiredRuleMissingError. No matching rule gives ErrNoUniquenessRule,
// because an upsert needs a key and a fallback to a plain create would lose the
// find. A matching rule whose property the request omits gives
// ErrUniquenessRulePropertyMissing. More than one matching rule gives
// ErrMultipleUniquenessRules.
func (gs *GraphStorage) UpsertNodeWithUniquenessRulesForTenant(
	tenantID string,
	labels []string,
	properties map[string]Value,
) (*Node, bool, error) {
	missing, matched := gs.planUniquenessEnforcement(labels)
	if missing != nil {
		return nil, false, missing
	}

	switch len(matched) {
	case 0:
		return nil, false, ErrNoUniquenessRule
	case 1:
		rule := matched[0]
		if _, ok := properties[rule.PropertyKey]; !ok {
			return nil, false, fmt.Errorf("%w: label %q requires a %q property", ErrUniquenessRulePropertyMissing, rule.Label, rule.PropertyKey)
		}
		return gs.UpsertNodeForTenant(tenantID, labels, properties, rule.Label, rule.PropertyKey)
	default:
		sort.Slice(matched, func(i, j int) bool { return matched[i].Name < matched[j].Name })
		return nil, false, fmt.Errorf("%w: %q and %q", ErrMultipleUniquenessRules, matched[0].Name, matched[1].Name)
	}
}

// upsertKeyAllowed reports whether (keyLabel, keyProperty) may key an upsert of
// labels. Every rule covering labels must have that key. The create branch
// checks only the key, so a second covering rule would go unchecked there and
// a new node could hold a value that rule says only one node may hold. With
// two distinct covering rules this is never true, the same answer
// CreateNodeWithUniquenessRulesForTenant gives (ErrMultipleUniquenessRules).
func upsertKeyAllowed(rules []UniquenessRule, labels []string, keyLabel, keyProperty string) bool {
	for _, r := range rules {
		if !containsString(labels, r.Label) {
			continue
		}
		if r.Label != keyLabel || r.PropertyKey != keyProperty {
			return false
		}
	}
	return true
}

// nodeUpsert is what the lock section of an upsert hands to the off-lock half.
type nodeUpsert struct {
	// node is the caller's own copy, cloned inside the lock after the write.
	node    *Node
	created bool

	// Create path.
	walPending  *wal.Pending
	vectorPlans []vectorInsertPlan

	// Match path. patch is nil when the match changed nothing (D4).
	patch *nodePatch
}

// finish runs the work a chosen path does after its lock section: for a create
// the vector inserts, the WAL wait and the created-node notification, and for a
// changing match what a patch does (finishNodePatch). A no-change match has
// nothing to do.
func (res nodeUpsert) finish(gs *GraphStorage) (*Node, bool, error) {
	if res.created {
		gs.applyNodeVectorInserts(res.vectorPlans)
		err := gs.waitWALPending(wal.OpCreateNode, res.walPending)
		gs.notifyNodeCreated(context.Background(), res.node)
		return res.node, true, err
	}
	if res.patch == nil {
		return res.node, false, nil
	}
	return res.node, false, gs.finishNodePatch(res.node.ID, *res.patch)
}

// upsertNodeUnderLock is the part of UpsertNodeForTenant that runs under gs.mu:
// the scan, the checks, and the create or the merge. The deferred unlock
// matters: see createNodeUnderLock.
func (gs *GraphStorage) upsertNodeUnderLock(
	tenantID string,
	labels []string,
	properties map[string]Value,
	keyLabel, keyProperty string,
	rules []UniquenessRule,
) (nodeUpsert, error) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	panicPoint("UpsertNodeForTenant")

	if err := gs.checkClosed(); err != nil {
		return nodeUpsert{}, err
	}

	tid := effectiveTenantID(tenantID)
	holders, err := gs.scanKeyHoldersLocked(tid, keyLabel, keyProperty, properties[keyProperty])
	if err != nil {
		return nodeUpsert{}, err
	}

	switch len(holders) {
	case 0:
		node, walPending, plans, err := gs.createNodeLocked(tenantID, labels, properties)
		if err != nil {
			return nodeUpsert{}, err
		}
		return nodeUpsert{node: node, created: true, walPending: walPending, vectorPlans: plans}, nil
	case 1:
		return gs.mergeUpsertLocked(tid, holders[0], labels, properties, rules)
	default:
		ids := make([]uint64, len(holders))
		for i, h := range holders {
			ids[i] = h.ID
		}
		return nodeUpsert{}, &UpsertAmbiguousError{Label: keyLabel, PropertyKey: keyProperty, NodeIDs: ids}
	}
}

// mergeUpsertLocked applies the D3, D6 and D4 checks and then the merge to the
// one node that holds the key. Caller holds gs.mu.Lock. existing is a read-only
// reference and may live in the mmap base, so the write goes through
// materializeNode.
func (gs *GraphStorage) mergeUpsertLocked(
	tid tenantid.TenantID,
	existing *Node,
	labels []string,
	properties map[string]Value,
	rules []UniquenessRule,
) (nodeUpsert, error) {
	var missing []string
	for _, l := range labels {
		if !containsString(existing.Labels, l) && !containsString(missing, l) {
			missing = append(missing, l)
		}
	}
	if len(missing) > 0 {
		return nodeUpsert{}, &UpsertLabelsDifferError{NodeID: existing.ID, Missing: missing}
	}

	if err := gs.checkMergeAgainstRulesLocked(tid, existing, properties, rules); err != nil {
		return nodeUpsert{}, err
	}

	if !propertiesDiffer(existing.Properties, properties) {
		return nodeUpsert{node: existing.Clone()}, nil
	}

	node, err := gs.materializeNode(existing.ID)
	if err != nil {
		return nodeUpsert{}, err
	}
	p, err := gs.applyNodePatchLocked(node, properties, nil)
	if err != nil {
		return nodeUpsert{}, err
	}
	// The caller's copy is taken here, after the merge and inside the lock.
	// p.newNode exists only when observers are registered, and the observers
	// own it.
	return nodeUpsert{node: node.Clone(), patch: &p}, nil
}

// checkMergeAgainstRulesLocked refuses a merge that would give the matched
// node a value another node already holds under a registered rule (D6). The
// rules checked are those whose Label is on the stored node and whose
// PropertyKey the request sets to a value different from the stored one. The
// scan is the unique create's, except that the matched node is not a holder of
// its own value. Caller holds gs.mu.
func (gs *GraphStorage) checkMergeAgainstRulesLocked(
	tid tenantid.TenantID,
	existing *Node,
	properties map[string]Value,
	rules []UniquenessRule,
) error {
	for _, r := range rules {
		if !containsString(existing.Labels, r.Label) {
			continue
		}
		newVal, set := properties[r.PropertyKey]
		if !set {
			continue
		}
		if old, has := existing.Properties[r.PropertyKey]; has && valuesEqual(old, newVal) {
			continue
		}
		holders, err := gs.scanKeyHoldersLocked(tid, r.Label, r.PropertyKey, newVal)
		if err != nil {
			return err
		}
		for _, h := range holders {
			if h.ID == existing.ID {
				continue
			}
			return &UniqueConstraintError{
				Label:             r.Label,
				PropertyKey:       r.PropertyKey,
				ConflictingNodeID: h.ID,
				TenantID:          tid.String(),
			}
		}
	}
	return nil
}

// scanKeyHoldersLocked returns every node of the tenant in the label bucket
// whose key property equals val. It does not stop at the first, because the
// upsert must tell one match from several. An unreadable record aborts with
// ErrRecordUnreadable wrapped, never "no match": the store cannot say whether
// that record holds val. Caller holds gs.mu.
func (gs *GraphStorage) scanKeyHoldersLocked(tid tenantid.TenantID, label, key string, val Value) ([]*Node, error) {
	var holders []*Node
	for _, id := range gs.membershipNodeIDsByLabelLocked(tid, label) {
		n, err := gs.resolveNodeRefLocked(id)
		if err != nil {
			if errors.Is(err, ErrNodeNotFound) {
				// Absent, for example a tombstone the membership run has not
				// dropped yet. Not a holder.
				continue
			}
			return nil, fmt.Errorf("uniqueness scan for %s.%s: node %d: %w", label, key, id, err)
		}
		if v, has := n.Properties[key]; has && valuesEqual(v, val) {
			holders = append(holders, n)
		}
	}
	return holders, nil
}

// propertiesDiffer reports whether applying set to stored would change any
// value, which decides whether an upsert match writes at all (D4).
func propertiesDiffer(stored, set map[string]Value) bool {
	for k, v := range set {
		if old, has := stored[k]; !has || !valuesEqual(old, v) {
			return true
		}
	}
	return false
}
