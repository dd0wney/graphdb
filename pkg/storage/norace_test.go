//go:build !race

package storage

// isRaceEnabled returns false when the race detector is not enabled
func isRaceEnabled() bool {
	return false
}
