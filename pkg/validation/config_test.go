package validation

import (
	"errors"
	"testing"
	"time"
)

func TestConfigValidator_Required(t *testing.T) {
	cv := NewConfigValidator("TestConfig")
	cv.Required("Name", "")

	if !cv.HasErrors() {
		t.Error("Expected error for empty required field")
	}

	cv2 := NewConfigValidator("TestConfig")
	cv2.Required("Name", "value")

	if cv2.HasErrors() {
		t.Error("Expected no error for non-empty required field")
	}
}

func TestConfigValidator_RequiredInt(t *testing.T) {
	cv := NewConfigValidator("TestConfig")
	cv.RequiredInt("Port", 0)

	if !cv.HasErrors() {
		t.Error("Expected error for zero required int")
	}

	cv2 := NewConfigValidator("TestConfig")
	cv2.RequiredInt("Port", 8080)

	if cv2.HasErrors() {
		t.Error("Expected no error for non-zero required int")
	}
}

func TestConfigValidator_MinInt(t *testing.T) {
	cv := NewConfigValidator("TestConfig")
	cv.MinInt("Workers", 0, 1)

	if !cv.HasErrors() {
		t.Error("Expected error for value below minimum")
	}

	cv2 := NewConfigValidator("TestConfig")
	cv2.MinInt("Workers", 5, 1)

	if cv2.HasErrors() {
		t.Error("Expected no error for value at or above minimum")
	}
}

func TestConfigValidator_MaxInt(t *testing.T) {
	cv := NewConfigValidator("TestConfig")
	cv.MaxInt("Connections", 100, 50)

	if !cv.HasErrors() {
		t.Error("Expected error for value above maximum")
	}

	cv2 := NewConfigValidator("TestConfig")
	cv2.MaxInt("Connections", 25, 50)

	if cv2.HasErrors() {
		t.Error("Expected no error for value at or below maximum")
	}
}

func TestConfigValidator_RangeInt(t *testing.T) {
	tests := []struct {
		name      string
		value     int
		min       int
		max       int
		expectErr bool
	}{
		{"below range", 0, 1, 10, true},
		{"above range", 15, 1, 10, true},
		{"at min", 1, 1, 10, false},
		{"at max", 10, 1, 10, false},
		{"in range", 5, 1, 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cv := NewConfigValidator("TestConfig")
			cv.RangeInt("Value", tt.value, tt.min, tt.max)

			if tt.expectErr && !cv.HasErrors() {
				t.Error("Expected error")
			}
			if !tt.expectErr && cv.HasErrors() {
				t.Errorf("Unexpected error: %v", cv.Error())
			}
		})
	}
}

func TestConfigValidator_MinDuration(t *testing.T) {
	cv := NewConfigValidator("TestConfig")
	cv.MinDuration("Timeout", 500*time.Millisecond, 1*time.Second)

	if !cv.HasErrors() {
		t.Error("Expected error for duration below minimum")
	}

	cv2 := NewConfigValidator("TestConfig")
	cv2.MinDuration("Timeout", 2*time.Second, 1*time.Second)

	if cv2.HasErrors() {
		t.Error("Expected no error for duration at or above minimum")
	}
}

func TestConfigValidator_MaxDuration(t *testing.T) {
	cv := NewConfigValidator("TestConfig")
	cv.MaxDuration("Timeout", 10*time.Minute, 5*time.Minute)

	if !cv.HasErrors() {
		t.Error("Expected error for duration above maximum")
	}

	cv2 := NewConfigValidator("TestConfig")
	cv2.MaxDuration("Timeout", 3*time.Minute, 5*time.Minute)

	if cv2.HasErrors() {
		t.Error("Expected no error for duration at or below maximum")
	}
}

func TestConfigValidator_Positive(t *testing.T) {
	cv := NewConfigValidator("TestConfig")
	cv.Positive("Count", 0)

	if !cv.HasErrors() {
		t.Error("Expected error for zero value")
	}

	cv2 := NewConfigValidator("TestConfig")
	cv2.Positive("Count", -5)

	if !cv2.HasErrors() {
		t.Error("Expected error for negative value")
	}

	cv3 := NewConfigValidator("TestConfig")
	cv3.Positive("Count", 5)

	if cv3.HasErrors() {
		t.Error("Expected no error for positive value")
	}
}

func TestConfigValidator_NonNegative(t *testing.T) {
	cv := NewConfigValidator("TestConfig")
	cv.NonNegative("Count", -1)

	if !cv.HasErrors() {
		t.Error("Expected error for negative value")
	}

	cv2 := NewConfigValidator("TestConfig")
	cv2.NonNegative("Count", 0)

	if cv2.HasErrors() {
		t.Error("Expected no error for zero value")
	}
}

func TestConfigValidator_OneOf(t *testing.T) {
	allowed := []string{"debug", "info", "warn", "error"}

	cv := NewConfigValidator("TestConfig")
	cv.OneOf("LogLevel", "trace", allowed)

	if !cv.HasErrors() {
		t.Error("Expected error for value not in allowed list")
	}

	cv2 := NewConfigValidator("TestConfig")
	cv2.OneOf("LogLevel", "info", allowed)

	if cv2.HasErrors() {
		t.Error("Expected no error for allowed value")
	}
}

func TestConfigValidator_Custom(t *testing.T) {
	cv := NewConfigValidator("TestConfig")
	cv.Custom("CustomField", func() error {
		return errors.New("custom validation failed")
	})

	if !cv.HasErrors() {
		t.Error("Expected error from custom validation")
	}

	cv2 := NewConfigValidator("TestConfig")
	cv2.Custom("CustomField", func() error {
		return nil
	})

	if cv2.HasErrors() {
		t.Error("Expected no error from passing custom validation")
	}
}

func TestConfigValidator_When(t *testing.T) {
	// Condition true - validation should run
	cv := NewConfigValidator("TestConfig")
	cv.When(true, func(v *ConfigValidator) {
		v.Positive("Count", -1)
	})

	if !cv.HasErrors() {
		t.Error("Expected error when condition is true")
	}

	// Condition false - validation should not run
	cv2 := NewConfigValidator("TestConfig")
	cv2.When(false, func(v *ConfigValidator) {
		v.Positive("Count", -1)
	})

	if cv2.HasErrors() {
		t.Error("Expected no error when condition is false")
	}
}

func TestConfigValidator_Chaining(t *testing.T) {
	cv := NewConfigValidator("ServerConfig")
	cv.Required("Host", "localhost").
		RangeInt("Port", 8080, 1, 65535).
		MinDuration("Timeout", 5*time.Second, 1*time.Second).
		Positive("Workers", 4)

	if cv.HasErrors() {
		t.Errorf("Expected no errors for valid config, got: %v", cv.Error())
	}
}

func TestConfigValidator_MultipleErrors(t *testing.T) {
	cv := NewConfigValidator("TestConfig")
	cv.Required("Name", "").
		Positive("Count", -1).
		MinDuration("Timeout", 0, 1*time.Second)

	if len(cv.Errors()) != 3 {
		t.Errorf("Expected 3 errors, got %d", len(cv.Errors()))
	}
}

func TestConfigValidator_Validate(t *testing.T) {
	cv := NewConfigValidator("TestConfig")
	cv.Required("Name", "")

	err := cv.Validate()
	if err == nil {
		t.Error("Expected error from Validate()")
	}

	cv2 := NewConfigValidator("TestConfig")
	cv2.Required("Name", "valid")

	err2 := cv2.Validate()
	if err2 != nil {
		t.Errorf("Expected no error from Validate(), got: %v", err2)
	}
}

func TestDefaultOr(t *testing.T) {
	if DefaultOr("", "default") != "default" {
		t.Error("Expected default for empty string")
	}
	if DefaultOr("value", "default") != "value" {
		t.Error("Expected value for non-empty string")
	}
}

func TestDefaultOrInt(t *testing.T) {
	if DefaultOrInt(0, 10) != 10 {
		t.Error("Expected default for zero")
	}
	if DefaultOrInt(-5, 10) != 10 {
		t.Error("Expected default for negative")
	}
	if DefaultOrInt(5, 10) != 5 {
		t.Error("Expected value for positive")
	}
}

func TestDefaultOrDuration(t *testing.T) {
	if DefaultOrDuration(0, 5*time.Second) != 5*time.Second {
		t.Error("Expected default for zero duration")
	}
	if DefaultOrDuration(-1*time.Second, 5*time.Second) != 5*time.Second {
		t.Error("Expected default for negative duration")
	}
	if DefaultOrDuration(10*time.Second, 5*time.Second) != 10*time.Second {
		t.Error("Expected value for positive duration")
	}
}

func TestClampInt(t *testing.T) {
	tests := []struct {
		value, min, max, expected int
	}{
		{5, 1, 10, 5},   // in range
		{0, 1, 10, 1},   // below min
		{15, 1, 10, 10}, // above max
		{1, 1, 10, 1},   // at min
		{10, 1, 10, 10}, // at max
	}

	for _, tt := range tests {
		result := ClampInt(tt.value, tt.min, tt.max)
		if result != tt.expected {
			t.Errorf("ClampInt(%d, %d, %d) = %d, want %d", tt.value, tt.min, tt.max, result, tt.expected)
		}
	}
}

func TestClampDuration(t *testing.T) {
	tests := []struct {
		value, min, max, expected time.Duration
	}{
		{5 * time.Second, 1 * time.Second, 10 * time.Second, 5 * time.Second},
		{500 * time.Millisecond, 1 * time.Second, 10 * time.Second, 1 * time.Second},
		{15 * time.Second, 1 * time.Second, 10 * time.Second, 10 * time.Second},
	}

	for _, tt := range tests {
		result := ClampDuration(tt.value, tt.min, tt.max)
		if result != tt.expected {
			t.Errorf("ClampDuration(%v, %v, %v) = %v, want %v", tt.value, tt.min, tt.max, result, tt.expected)
		}
	}
}

// Example of a validatable config struct
type ExampleConfig struct {
	Host    string
	Port    int
	Timeout time.Duration
}

func (c *ExampleConfig) Validate() error {
	return NewConfigValidator("ExampleConfig").
		Required("Host", c.Host).
		RangeInt("Port", c.Port, 1, 65535).
		MinDuration("Timeout", c.Timeout, 1*time.Second).
		Validate()
}

func TestValidateConfig(t *testing.T) {
	validConfig := &ExampleConfig{
		Host:    "localhost",
		Port:    8080,
		Timeout: 30 * time.Second,
	}

	if err := ValidateConfig(validConfig); err != nil {
		t.Errorf("Expected valid config, got error: %v", err)
	}

	invalidConfig := &ExampleConfig{
		Host:    "",
		Port:    0,
		Timeout: 0,
	}

	if err := ValidateConfig(invalidConfig); err == nil {
		t.Error("Expected error for invalid config")
	}
}

func TestValidateConfig_Nil(t *testing.T) {
	err := ValidateConfig(nil)
	if err == nil {
		t.Error("Expected error for nil config")
	}
}
