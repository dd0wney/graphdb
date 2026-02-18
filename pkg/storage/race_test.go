//go:build race

package storage

// isRaceEnabled returns true when the race detector is enabled
func isRaceEnabled() bool {
	return true
}
