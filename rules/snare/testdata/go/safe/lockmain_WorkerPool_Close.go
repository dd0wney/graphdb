// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/parallel/worker_pool.go at b9bc9d2, lines 88-98, function WorkerPool.Close.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (SAFE): close behind sync.Once on a channel the constructor makes.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// Close shuts down the worker pool
func (wp *WorkerPool) Close() {
	wp.once.Do(func() {
		// Acquire write lock before closing
		wp.mu.Lock()
		wp.closed = true
		close(wp.taskQueue)
		wp.mu.Unlock()
	})
	wp.wg.Wait()
}
