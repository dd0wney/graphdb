// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/vfs/vfstest/role.go at b9bc9d2, lines 162-172, function RoleFS.FailAtKey.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (TEST-SUPPORT(SAFE)): resetLocked assigns fields and makes maps.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// FailAtKey arms a fault on one structural failure point. This is how a case
// found by the sweep is pinned as a regression test that no longer depends on
// the ordinal the sweep happened to walk.
func (r *RoleFS) FailAtKey(k Key) {
	r.mu.Lock()
	r.targetKey, r.hasTargetKey = k, true
	r.target, r.nth = "", 0
	r.alwaysRole = ""
	r.resetLocked()
	r.mu.Unlock()
}
