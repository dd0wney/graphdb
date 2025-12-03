package query

import "time"

// TimeoutConfig defines the bounds for timeout validation.
type TimeoutConfig struct {
	Min     time.Duration // Minimum allowed timeout (0 means no minimum)
	Max     time.Duration // Maximum allowed timeout (0 means no maximum)
	Default time.Duration // Default timeout when value is invalid
}

// DefaultQueryTimeoutConfig returns the standard config for query timeouts.
func DefaultQueryTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		Min:     0,
		Max:     MaxQueryTimeout,
		Default: DefaultQueryTimeout,
	}
}

// DefaultTaskTimeoutConfig returns the standard config for task timeouts.
func DefaultTaskTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		Min:     MinTaskTimeout,
		Max:     0, // No maximum for tasks
		Default: DefaultTaskTimeout,
	}
}

// ValidateTimeout validates and normalizes a timeout duration.
// Returns the default if timeout is <= 0.
// Returns min if timeout is less than min (when min > 0).
// Returns max if timeout exceeds max (when max > 0).
func ValidateTimeout(timeout time.Duration, config TimeoutConfig) time.Duration {
	// Use default for zero or negative values
	if timeout <= 0 {
		return config.Default
	}

	// Enforce minimum (if configured)
	if config.Min > 0 && timeout < config.Min {
		return config.Default
	}

	// Enforce maximum (if configured)
	if config.Max > 0 && timeout > config.Max {
		return config.Max
	}

	return timeout
}

// ValidateQueryTimeout is a convenience function for validating query timeouts.
func ValidateQueryTimeout(timeout time.Duration) time.Duration {
	return ValidateTimeout(timeout, DefaultQueryTimeoutConfig())
}

// ValidateTaskTimeout is a convenience function for validating task timeouts.
func ValidateTaskTimeout(timeout time.Duration) time.Duration {
	return ValidateTimeout(timeout, DefaultTaskTimeoutConfig())
}
