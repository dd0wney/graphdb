package tenant

import (
	"context"
)

// contextKey is an unexported type for context keys to prevent collisions
type contextKey struct{}

// tenantKey is the context key for tenant ID
var tenantKey = contextKey{}

// WithTenant returns a new context with the tenant ID set
func WithTenant(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		tenantID = DefaultTenantID
	}
	return context.WithValue(ctx, tenantKey, tenantID)
}

// FromContext extracts the tenant ID from the context.
// Returns the tenant ID and true if found, or empty string and false if not.
func FromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	tenantID, ok := ctx.Value(tenantKey).(string)
	return tenantID, ok
}

// MustFromContext extracts the tenant ID from the context.
// Returns DefaultTenantID if not found (safe for backward compatibility).
func MustFromContext(ctx context.Context) string {
	tenantID, ok := FromContext(ctx)
	if !ok || tenantID == "" {
		return DefaultTenantID
	}
	return tenantID
}

// IsDefaultTenant returns true if the tenant ID is the default tenant
func IsDefaultTenant(tenantID string) bool {
	return tenantID == "" || tenantID == DefaultTenantID
}
