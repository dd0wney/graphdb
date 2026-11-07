// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/vfs/vfstest/role.go at b9bc9d2, lines 150-160, function RoleFS.FailNthOpForRole.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (TEST-SUPPORT(SAFE)): resetLocked assigns fields and makes maps.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// FailNthOpForRole arms a fault on the N-th operation of one role, counting
// from 1. It resets the counters, so each sweep step starts from the same
// place.
func (r *RoleFS) FailNthOpForRole(role Role, n int) {
	r.mu.Lock()
	r.target, r.nth = role, n
	r.hasTargetKey = false
	r.alwaysRole = ""
	r.resetLocked()
	r.mu.Unlock()
}
