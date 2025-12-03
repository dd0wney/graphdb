package validation

import (
	"errors"
	"fmt"
	"time"
)

// ConfigValidator provides a fluent interface for validating configuration values.
// It collects all validation errors rather than failing on the first one.
type ConfigValidator struct {
	errors []error
	name   string // config struct name for error messages
}

// NewConfigValidator creates a new config validator with the given config name.
func NewConfigValidator(configName string) *ConfigValidator {
	return &ConfigValidator{
		name:   configName,
		errors: make([]error, 0),
	}
}

// Required validates that a string field is not empty.
func (cv *ConfigValidator) Required(field, value string) *ConfigValidator {
	if value == "" {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: required field is empty", cv.name, field))
	}
	return cv
}

// RequiredInt validates that an int field is not zero.
func (cv *ConfigValidator) RequiredInt(field string, value int) *ConfigValidator {
	if value == 0 {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: required field is zero", cv.name, field))
	}
	return cv
}

// RequiredDuration validates that a duration field is not zero.
func (cv *ConfigValidator) RequiredDuration(field string, value time.Duration) *ConfigValidator {
	if value == 0 {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: required duration is zero", cv.name, field))
	}
	return cv
}

// MinInt validates that an int field is at least the minimum value.
func (cv *ConfigValidator) MinInt(field string, value, min int) *ConfigValidator {
	if value < min {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: value %d is below minimum %d", cv.name, field, value, min))
	}
	return cv
}

// MaxInt validates that an int field does not exceed the maximum value.
func (cv *ConfigValidator) MaxInt(field string, value, max int) *ConfigValidator {
	if value > max {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: value %d exceeds maximum %d", cv.name, field, value, max))
	}
	return cv
}

// RangeInt validates that an int field is within the specified range.
func (cv *ConfigValidator) RangeInt(field string, value, min, max int) *ConfigValidator {
	if value < min || value > max {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: value %d is outside range [%d, %d]", cv.name, field, value, min, max))
	}
	return cv
}

// MinDuration validates that a duration is at least the minimum.
func (cv *ConfigValidator) MinDuration(field string, value, min time.Duration) *ConfigValidator {
	if value < min {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: duration %v is below minimum %v", cv.name, field, value, min))
	}
	return cv
}

// MaxDuration validates that a duration does not exceed the maximum.
func (cv *ConfigValidator) MaxDuration(field string, value, max time.Duration) *ConfigValidator {
	if value > max {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: duration %v exceeds maximum %v", cv.name, field, value, max))
	}
	return cv
}

// RangeDuration validates that a duration is within the specified range.
func (cv *ConfigValidator) RangeDuration(field string, value, min, max time.Duration) *ConfigValidator {
	if value < min || value > max {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: duration %v is outside range [%v, %v]", cv.name, field, value, min, max))
	}
	return cv
}

// Positive validates that an int field is positive (> 0).
func (cv *ConfigValidator) Positive(field string, value int) *ConfigValidator {
	if value <= 0 {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: value %d must be positive", cv.name, field, value))
	}
	return cv
}

// NonNegative validates that an int field is non-negative (>= 0).
func (cv *ConfigValidator) NonNegative(field string, value int) *ConfigValidator {
	if value < 0 {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: value %d must be non-negative", cv.name, field, value))
	}
	return cv
}

// PositiveFloat validates that a float field is positive (> 0).
func (cv *ConfigValidator) PositiveFloat(field string, value float64) *ConfigValidator {
	if value <= 0 {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: value %f must be positive", cv.name, field, value))
	}
	return cv
}

// NonNegativeFloat validates that a float field is non-negative (>= 0).
func (cv *ConfigValidator) NonNegativeFloat(field string, value float64) *ConfigValidator {
	if value < 0 {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: value %f must be non-negative", cv.name, field, value))
	}
	return cv
}

// OneOf validates that a string field is one of the allowed values.
func (cv *ConfigValidator) OneOf(field, value string, allowed []string) *ConfigValidator {
	for _, a := range allowed {
		if value == a {
			return cv
		}
	}
	cv.errors = append(cv.errors, fmt.Errorf("%s.%s: value %q must be one of %v", cv.name, field, value, allowed))
	return cv
}

// Custom applies a custom validation function.
func (cv *ConfigValidator) Custom(field string, fn func() error) *ConfigValidator {
	if err := fn(); err != nil {
		cv.errors = append(cv.errors, fmt.Errorf("%s.%s: %w", cv.name, field, err))
	}
	return cv
}

// When conditionally applies validations if the condition is true.
func (cv *ConfigValidator) When(condition bool, validations func(*ConfigValidator)) *ConfigValidator {
	if condition {
		validations(cv)
	}
	return cv
}

// HasErrors returns true if any validation errors occurred.
func (cv *ConfigValidator) HasErrors() bool {
	return len(cv.errors) > 0
}

// Error returns the first validation error, or nil if no errors.
func (cv *ConfigValidator) Error() error {
	if len(cv.errors) == 0 {
		return nil
	}
	return cv.errors[0]
}

// Errors returns all validation errors.
func (cv *ConfigValidator) Errors() []error {
	return cv.errors
}

// Validate returns a combined error if any validations failed.
func (cv *ConfigValidator) Validate() error {
	if len(cv.errors) == 0 {
		return nil
	}
	if len(cv.errors) == 1 {
		return cv.errors[0]
	}
	return fmt.Errorf("%s validation failed with %d errors: %v", cv.name, len(cv.errors), cv.errors[0])
}

// Validatable is an interface for types that can validate themselves.
type Validatable interface {
	Validate() error
}

// ValidateConfig validates any type that implements Validatable.
func ValidateConfig(config Validatable) error {
	if config == nil {
		return errors.New("config cannot be nil")
	}
	return config.Validate()
}

// DefaultOr returns the value if it's non-zero, otherwise returns the default.
func DefaultOr[T comparable](value, defaultValue T) T {
	var zero T
	if value == zero {
		return defaultValue
	}
	return value
}

// DefaultOrInt returns the value if it's positive, otherwise returns the default.
func DefaultOrInt(value, defaultValue int) int {
	if value <= 0 {
		return defaultValue
	}
	return value
}

// DefaultOrDuration returns the value if it's positive, otherwise returns the default.
func DefaultOrDuration(value, defaultValue time.Duration) time.Duration {
	if value <= 0 {
		return defaultValue
	}
	return value
}

// ClampInt clamps a value to the specified range [min, max].
func ClampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ClampDuration clamps a duration to the specified range [min, max].
func ClampDuration(value, min, max time.Duration) time.Duration {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
