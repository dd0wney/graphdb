package storage

// lockPanicHook, when a test sets it, is called by panicPoint inside each
// critical section that runs non-trivial code under a lock. A test makes it
// panic to prove the section releases its lock on a panic: the Cypher executor
// and net/http recover panics, so a lock left held hangs every later call,
// Close included, while the process keeps running.
//
// Tests only. Production never sets it, and panicPoint is then one nil check.
// It is package state rather than a GraphStorage field because some sections
// hold the lock of a VectorIndex or an EdgeStore, which has no GraphStorage. A
// test that sets it must therefore not run in parallel.
var lockPanicHook func(site string)

// panicPoint marks a place inside a critical section where a panic must not
// leave the lock held.
func panicPoint(site string) {
	if lockPanicHook != nil {
		lockPanicHook(site)
	}
}
