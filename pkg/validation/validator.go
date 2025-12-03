package validation

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/go-playground/validator/v10"
)

var (
	// validate is a singleton validator instance
	validate *validator.Validate

	// Validation constants
	MaxLabels        = 10
	MaxLabelLength   = 50
	MaxProperties    = 100
	MaxPropertyKey   = 100
	MaxBatchSize     = 1000
	MinBatchSize     = 1

	// Regular expressions
	labelPattern    = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	propKeyPattern  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

func init() {
	validate = validator.New()
}

// NodeRequest represents a request to create or update a node
type NodeRequest struct {
	Labels     []string               `json:"labels" validate:"required,min=1,max=10,dive,max=50"`
	Properties map[string]any `json:"properties" validate:"omitempty,max=100"`
}

// EdgeRequest represents a request to create or update an edge
type EdgeRequest struct {
	FromNodeID uint64                  `json:"fromNodeId" validate:"required,min=1"`
	ToNodeID   uint64                  `json:"toNodeId" validate:"required,min=1"`
	Type       string                  `json:"type" validate:"required,min=1,max=50"`
	Weight     *float64                `json:"weight" validate:"omitempty"`
	Properties map[string]any  `json:"properties" validate:"omitempty,max=100"`
}

// ValidateNodeRequest validates a node creation/update request
func ValidateNodeRequest(req *NodeRequest) error {
	if req == nil {
		return errors.New("node request cannot be nil")
	}

	// Validate using struct tags
	if err := validate.Struct(req); err != nil {
		return formatValidationError(err)
	}

	// Additional label validation
	if len(req.Labels) > MaxLabels {
		return fmt.Errorf("Labels: maximum %d labels allowed, got %d", MaxLabels, len(req.Labels))
	}

	for i, label := range req.Labels {
		if len(label) > MaxLabelLength {
			return fmt.Errorf("Labels: label at index %d exceeds maximum length of %d characters", i, MaxLabelLength)
		}
		if !labelPattern.MatchString(label) {
			return fmt.Errorf("Labels: label '%s' contains invalid characters (only alphanumeric and underscore allowed)", label)
		}
	}

	// Additional properties validation
	if len(req.Properties) > MaxProperties {
		return fmt.Errorf("Properties: maximum %d properties allowed, got %d", MaxProperties, len(req.Properties))
	}

	// Validate property keys
	for key := range req.Properties {
		if err := ValidatePropertyKey(key); err != nil {
			return fmt.Errorf("Properties: %w", err)
		}
	}

	return nil
}

// ValidateEdgeRequest validates an edge creation/update request
func ValidateEdgeRequest(req *EdgeRequest) error {
	if req == nil {
		return errors.New("edge request cannot be nil")
	}

	// Validate using struct tags
	if err := validate.Struct(req); err != nil {
		return formatValidationError(err)
	}

	// Additional type validation
	if len(req.Type) > MaxLabelLength {
		return fmt.Errorf("Type: exceeds maximum length of %d characters", MaxLabelLength)
	}
	if !labelPattern.MatchString(req.Type) {
		return fmt.Errorf("Type: '%s' contains invalid characters (only alphanumeric and underscore allowed)", req.Type)
	}

	// Additional properties validation
	if len(req.Properties) > MaxProperties {
		return fmt.Errorf("Properties: maximum %d properties allowed, got %d", MaxProperties, len(req.Properties))
	}

	// Validate property keys
	for key := range req.Properties {
		if err := ValidatePropertyKey(key); err != nil {
			return fmt.Errorf("Properties: %w", err)
		}
	}

	return nil
}

// ValidateBatchSize validates the size of a batch request
func ValidateBatchSize(size int) error {
	if size < MinBatchSize {
		return fmt.Errorf("batch size must be at least %d, got %d", MinBatchSize, size)
	}
	if size > MaxBatchSize {
		return fmt.Errorf("batch size must not exceed %d, got %d", MaxBatchSize, size)
	}
	return nil
}

// ValidatePropertyKey validates a property key
func ValidatePropertyKey(key string) error {
	if key == "" {
		return errors.New("property key cannot be empty")
	}
	if len(key) > MaxPropertyKey {
		return fmt.Errorf("property key '%s' exceeds maximum length of %d characters", key, MaxPropertyKey)
	}
	if !propKeyPattern.MatchString(key) {
		return fmt.Errorf("property key '%s' is invalid (must start with letter or underscore, followed by alphanumeric or underscore)", key)
	}
	return nil
}

// formatValidationError converts validator errors to a more user-friendly format
func formatValidationError(err error) error {
	if err == nil {
		return nil
	}

	validationErrs, ok := err.(validator.ValidationErrors)
	if !ok {
		return err
	}

	// Return the first validation error in a user-friendly format
	for _, e := range validationErrs {
		field := e.Field()
		tag := e.Tag()
		param := e.Param()

		switch tag {
		case "required":
			return fmt.Errorf("%s: field is required", field)
		case "min":
			return fmt.Errorf("%s: must be at least %s", field, param)
		case "max":
			return fmt.Errorf("%s: must not exceed %s", field, param)
		case "dive":
			// For array elements
			return fmt.Errorf("%s: invalid element in array", field)
		default:
			return fmt.Errorf("%s: validation failed (%s)", field, tag)
		}
	}

	return err
}
