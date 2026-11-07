// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/vfs/vfstest/role.go at b9bc9d2, lines 213-230, function RoleFS.FailAllForRole.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (TEST-SUPPORT(SAFE)): resetLocked assigns fields and makes maps.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// FailAllForRole fails every operation of one role, from now on.
//
// FailNthOpForRole and FailAtKey each name a single operation, so they model a
// transient error: the caller retries and the store recovers. A disk that is
// full, or mounted read-only, does not behave that way.
//
// The distinction is not academic. A durability promise — "this call returning
// nil means the data is on the disk" — is kept, not broken, when a transient
// error is retried into success. It is only observable at all when the failure
// persists, so testing it needs this.
func (r *RoleFS) FailAllForRole(role Role) {
	r.mu.Lock()
	r.alwaysRole, r.alwaysOpSet = role, false
	r.target, r.nth = "", 0
	r.hasTargetKey = false
	r.resetLocked()
	r.mu.Unlock()
}
