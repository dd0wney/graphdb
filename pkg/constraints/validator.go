package constraints

import (
	"time"
)

// ValidationResult contains the results of validating a graph against constraints
type ValidationResult struct {
	Valid      bool        // True if no violations found
	Violations []Violation // List of all violations
	CheckedAt  time.Time   // When validation was performed
}

// GetViolationsBySeverity returns violations filtered by severity level
func (vr *ValidationResult) GetViolationsBySeverity(severity Severity) []Violation {
	filtered := make([]Violation, 0)
	for _, v := range vr.Violations {
		if v.Severity == severity {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// GetViolationsByType returns violations filtered by type
func (vr *ValidationResult) GetViolationsByType(violationType ViolationType) []Violation {
	filtered := make([]Violation, 0)
	for _, v := range vr.Violations {
		if v.Type == violationType {
			filtered = append(filtered, v)
		}
	}
	return filtered
}

// Validator manages a set of constraints and validates graphs against them
type Validator struct {
	constraints []Constraint
}

// NewValidator creates a new empty validator
func NewValidator() *Validator {
	return &Validator{
		constraints: make([]Constraint, 0),
	}
}

// AddConstraint adds a constraint to the validator
func (v *Validator) AddConstraint(constraint Constraint) {
	v.constraints = append(v.constraints, constraint)
}

// AddConstraints adds multiple constraints to the validator
func (v *Validator) AddConstraints(constraints []Constraint) {
	v.constraints = append(v.constraints, constraints...)
}

// Validate runs all constraints against the graph and returns the results
func (v *Validator) Validate(graph GraphReader) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:      true,
		Violations: make([]Violation, 0),
		CheckedAt:  time.Now(),
	}

	// Run each constraint
	for _, constraint := range v.constraints {
		violations, err := constraint.Validate(graph)
		if err != nil {
			return nil, err
		}

		if len(violations) > 0 {
			result.Valid = false
			result.Violations = append(result.Violations, violations...)
		}
	}

	return result, nil
}

// GetConstraints returns all constraints in the validator
func (v *Validator) GetConstraints() []Constraint {
	return v.constraints
}

// ClearConstraints removes all constraints from the validator
func (v *Validator) ClearConstraints() {
	v.constraints = make([]Constraint, 0)
}
