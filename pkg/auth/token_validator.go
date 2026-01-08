package auth

import (
	"context"
	"errors"
)

// TokenValidator abstracts token validation to support multiple auth methods (JWT, OIDC, etc.)
type TokenValidator interface {
	// ValidateToken validates a token and returns claims.
	// Returns error if token is invalid, expired, or malformed.
	ValidateToken(ctx context.Context, token string) (*Claims, error)

	// Name returns the validator name for logging/debugging
	Name() string
}

// ErrNoValidatorMatched is returned when no validator can validate the token
var ErrNoValidatorMatched = errors.New("no validator could validate the token")

// CompositeTokenValidator chains multiple validators, trying each in order
type CompositeTokenValidator struct {
	validators []TokenValidator
}

// NewCompositeTokenValidator creates a validator that tries multiple validators in order
func NewCompositeTokenValidator(validators ...TokenValidator) *CompositeTokenValidator {
	return &CompositeTokenValidator{validators: validators}
}

// ValidateToken tries each validator in order until one succeeds
func (c *CompositeTokenValidator) ValidateToken(ctx context.Context, token string) (*Claims, error) {
	if len(c.validators) == 0 {
		return nil, ErrNoValidatorMatched
	}

	var lastErr error
	for _, v := range c.validators {
		claims, err := v.ValidateToken(ctx, token)
		if err == nil {
			return claims, nil
		}
		lastErr = err
	}

	// Return the last error (most specific)
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrNoValidatorMatched
}

// Name returns a composite name of all validators
func (c *CompositeTokenValidator) Name() string {
	return "composite"
}

// AddValidator adds a validator to the chain
func (c *CompositeTokenValidator) AddValidator(v TokenValidator) {
	c.validators = append(c.validators, v)
}
