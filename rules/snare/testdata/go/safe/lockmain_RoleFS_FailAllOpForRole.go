// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/vfs/vfstest/role.go at b9bc9d2, lines 232-247, function RoleFS.FailAllOpForRole.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (TEST-SUPPORT(SAFE)): resetLocked assigns fields and makes maps.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// FailAllOpForRole fails every operation of one KIND for one role, and lets
// that role's other operations through.
//
// This is what a partly broken disk looks like from inside: writes land and
// removals do not, or the reverse. It is also the only way to reach an error
// path that runs after a successful operation of a different kind — a cleanup
// that cannot remove anything, for one, where failing the whole role would
// stop the work before it ever got to the cleanup.
func (r *RoleFS) FailAllOpForRole(role Role, op Op) {
	r.mu.Lock()
	r.alwaysRole, r.alwaysOp, r.alwaysOpSet = role, op, true
	r.target, r.nth = "", 0
	r.hasTargetKey = false
	r.resetLocked()
	r.mu.Unlock()
}
