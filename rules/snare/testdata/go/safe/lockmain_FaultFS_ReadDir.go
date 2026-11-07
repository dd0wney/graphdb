// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/vfs/vfstest/faults.go at b9bc9d2, lines 201-209, function FaultFS.ReadDir.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (TEST-SUPPORT(SAFE)): record sets a bool field.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

func (f *FaultFS) ReadDir(name string) ([]os.DirEntry, error) {
	f.mu.Lock()
	shouldFail := f.record(f.countOp())
	f.mu.Unlock()
	if shouldFail {
		return nil, fmt.Errorf("readdir %s: %w", name, ErrInjected)
	}
	return f.base.ReadDir(name)
}
