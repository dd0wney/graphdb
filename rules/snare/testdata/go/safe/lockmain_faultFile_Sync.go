// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/vfs/vfstest/faults.go at b9bc9d2, lines 229-238, function faultFile.Sync.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (TEST-SUPPORT(SAFE)): counter, countOp and a switch.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

func (ff *faultFile) Sync() error {
	ff.fs.mu.Lock()
	ff.fs.syncs++
	shouldFail := ff.fs.record(ff.fs.countOp() || fire(&ff.fs.syncMode))
	ff.fs.mu.Unlock()
	if shouldFail {
		return fmt.Errorf("sync %s: %w", ff.Name(), ErrInjected)
	}
	return ff.File.Sync()
}
