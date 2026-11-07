// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/vfs/vfstest/faults.go at b9bc9d2, lines 182-194, function FaultFS.Open.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (TEST-SUPPORT(SAFE)): record sets a bool field.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

func (f *FaultFS) Open(name string, flag int, perm os.FileMode) (vfs.File, error) {
	f.mu.Lock()
	shouldFail := f.record(f.countOp() || fire(&f.openMode))
	f.mu.Unlock()
	if shouldFail {
		return nil, fmt.Errorf("open %s: %w", name, ErrInjected)
	}
	inner, err := f.base.Open(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &faultFile{File: inner, fs: f}, nil
}
