package oidc

// RoleMapper maps OIDC claims to GraphDB roles
type RoleMapper struct {
	mappings    []RoleMapping
	defaultRole string
}

// NewRoleMapper creates a new role mapper with the given mappings
func NewRoleMapper(mappings []RoleMapping, defaultRole string) *RoleMapper {
	if defaultRole == "" {
		defaultRole = DefaultRole
	}
	return &RoleMapper{
		mappings:    mappings,
		defaultRole: defaultRole,
	}
}

// MapRole determines the GraphDB role based on OIDC claims.
// Mappings are evaluated in order; first match wins.
// Higher-privilege mappings should be listed first (admin before editor before viewer).
func (m *RoleMapper) MapRole(claims *IDTokenClaims) string {
	// Check each mapping in order
	for _, mapping := range m.mappings {
		if m.matchesMapping(&mapping, claims) {
			return mapping.GraphDBRole
		}
	}

	return m.defaultRole
}

// matchesMapping checks if the claims match a specific role mapping
func (m *RoleMapper) matchesMapping(mapping *RoleMapping, claims *IDTokenClaims) bool {
	claimValue := m.getClaimValue(mapping.ClaimName, claims)
	if claimValue == nil {
		return false
	}

	// Handle different claim value types
	switch v := claimValue.(type) {
	case string:
		// Single string value
		for _, expected := range mapping.ClaimValues {
			if v == expected {
				return true
			}
		}
	case []string:
		// Array of strings (e.g., groups)
		for _, actual := range v {
			for _, expected := range mapping.ClaimValues {
				if actual == expected {
					return true
				}
			}
		}
	case []any:
		// Array of interface{} (from JSON)
		for _, actual := range v {
			if str, ok := actual.(string); ok {
				for _, expected := range mapping.ClaimValues {
					if str == expected {
						return true
					}
				}
			}
		}
	}

	return false
}

// getClaimValue retrieves a claim value by name from the claims
func (m *RoleMapper) getClaimValue(claimName string, claims *IDTokenClaims) any {
	// Check built-in claim fields first
	switch claimName {
	case "groups":
		if len(claims.Groups) > 0 {
			return claims.Groups
		}
	case "roles":
		if len(claims.Roles) > 0 {
			return claims.Roles
		}
	case "email":
		if claims.Email != "" {
			return claims.Email
		}
	case "email_verified":
		return claims.EmailVerified
	case "sub":
		return claims.Subject
	case "iss":
		return claims.Issuer
	case "preferred_username":
		return claims.PreferredUsername
	case "name":
		return claims.Name
	}

	// Fall back to raw claims for custom claims (e.g., "cognito:groups")
	if claims.Raw != nil {
		if val, exists := claims.Raw[claimName]; exists {
			return val
		}
	}

	return nil
}

// RolePriority returns the priority of a role (higher = more privileged)
func RolePriority(role string) int {
	switch role {
	case "admin":
		return 3
	case "editor":
		return 2
	case "viewer":
		return 1
	default:
		return 0
	}
}

// HigherPrivilegeRole returns the role with higher privilege
func HigherPrivilegeRole(a, b string) string {
	if RolePriority(a) >= RolePriority(b) {
		return a
	}
	return b
}
