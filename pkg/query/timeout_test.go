package query

import (
	"testing"
	"time"
)

func TestValidateTimeout_Basic(t *testing.T) {
	config := TimeoutConfig{
		Min:     1 * time.Second,
		Max:     1 * time.Minute,
		Default: 30 * time.Second,
	}

	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"zero returns default", 0, 30 * time.Second},
		{"negative returns default", -1 * time.Second, 30 * time.Second},
		{"below min returns default", 500 * time.Millisecond, 30 * time.Second},
		{"above max returns max", 2 * time.Minute, 1 * time.Minute},
		{"valid value unchanged", 45 * time.Second, 45 * time.Second},
		{"at min boundary", 1 * time.Second, 1 * time.Second},
		{"at max boundary", 1 * time.Minute, 1 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateTimeout(tt.input, config)
			if result != tt.expected {
				t.Errorf("ValidateTimeout(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidateTimeout_NoMin(t *testing.T) {
	config := TimeoutConfig{
		Min:     0, // No minimum
		Max:     1 * time.Minute,
		Default: 30 * time.Second,
	}

	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"zero still returns default", 0, 30 * time.Second},
		{"small value allowed", 100 * time.Millisecond, 100 * time.Millisecond},
		{"above max still capped", 2 * time.Minute, 1 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateTimeout(tt.input, config)
			if result != tt.expected {
				t.Errorf("ValidateTimeout(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidateTimeout_NoMax(t *testing.T) {
	config := TimeoutConfig{
		Min:     1 * time.Second,
		Max:     0, // No maximum
		Default: 30 * time.Second,
	}

	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"zero returns default", 0, 30 * time.Second},
		{"below min returns default", 500 * time.Millisecond, 30 * time.Second},
		{"very large value allowed", 24 * time.Hour, 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateTimeout(tt.input, config)
			if result != tt.expected {
				t.Errorf("ValidateTimeout(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidateQueryTimeout(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"zero returns default", 0, DefaultQueryTimeout},
		{"negative returns default", -1 * time.Second, DefaultQueryTimeout},
		{"above max returns max", 10 * time.Minute, MaxQueryTimeout},
		{"valid value unchanged", 1 * time.Minute, 1 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateQueryTimeout(tt.input)
			if result != tt.expected {
				t.Errorf("ValidateQueryTimeout(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidateTaskTimeout(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{"zero returns default", 0, DefaultTaskTimeout},
		{"below min returns default", 500 * time.Millisecond, DefaultTaskTimeout},
		{"at min boundary", MinTaskTimeout, MinTaskTimeout},
		{"valid value unchanged", 2 * time.Minute, 2 * time.Minute},
		// Task timeout has no max, so large values are allowed
		{"large value allowed", 1 * time.Hour, 1 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateTaskTimeout(tt.input)
			if result != tt.expected {
				t.Errorf("ValidateTaskTimeout(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
