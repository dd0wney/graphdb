// Snare benchmark fixture, set 1, rule graphdb-lock-call-without-defer.
// Source: pkg/query/stream_types.go at b9bc9d2, lines 102-111, function ResultStream.Close.
// Label: safe.
// Reason: A human labelled the finding on this function as not a hazard (SAFE): close behind sync.Once on a channel the constructor makes.
// Copied from graphdb (MIT licence, see LICENSE at the repository root).
package fixture

// Close closes the result stream
func (rs *ResultStream) Close() {
	rs.once.Do(func() {
		rs.cancel()
		rs.sendMu.Lock()
		rs.closed = true
		close(rs.ch)
		rs.sendMu.Unlock()
	})
}
