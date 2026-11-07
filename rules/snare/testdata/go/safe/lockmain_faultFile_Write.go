// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/vfs/vfstest/faults.go at b9bc9d2, lines 217-227, function faultFile.Write.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (TEST-SUPPORT(SAFE)): counter, countOp and a switch.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

func (ff *faultFile) Write(p []byte) (int, error) {
	ff.fs.mu.Lock()
	ff.fs.writes++
	shouldFail := ff.fs.record(ff.fs.countOp() ||
		(ff.fs.writes > ff.fs.writeAfter && fire(&ff.fs.writeMode)))
	ff.fs.mu.Unlock()
	if shouldFail {
		return 0, fmt.Errorf("write %s: %w", ff.Name(), ErrInjected)
	}
	return ff.File.Write(p)
}
