// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/vfs/vfstest/faults.go at b9bc9d2, lines 240-252, function faultFile.Close.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (TEST-SUPPORT(SAFE)): counter, countOp and a switch.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

func (ff *faultFile) Close() error {
	ff.fs.mu.Lock()
	shouldFail := ff.fs.record(ff.fs.countOp() || fire(&ff.fs.closeMode))
	ff.fs.mu.Unlock()
	if shouldFail {
		// The descriptor is still released. A fault driver that leaked one
		// would make every test using it leak, and the leak would be blamed on
		// the code under test.
		_ = ff.File.Close()
		return fmt.Errorf("close %s: %w", ff.Name(), ErrInjected)
	}
	return ff.File.Close()
}
