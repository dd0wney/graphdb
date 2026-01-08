package oidc

import (
	"testing"
)

func TestRoleMapper_MapRole(t *testing.T) {
	tests := []struct {
		name         string
		mappings     []RoleMapping
		defaultRole  string
		claims       *IDTokenClaims
		expectedRole string
	}{
		{
			name:         "No mappings, returns default role",
			mappings:     nil,
			defaultRole:  "viewer",
			claims:       &IDTokenClaims{Subject: "user1"},
			expectedRole: "viewer",
		},
		{
			name: "Groups claim matches admin",
			mappings: []RoleMapping{
				{ClaimName: "groups", ClaimValues: []string{"admins"}, GraphDBRole: "admin"},
				{ClaimName: "groups", ClaimValues: []string{"editors"}, GraphDBRole: "editor"},
			},
			defaultRole:  "viewer",
			claims:       &IDTokenClaims{Subject: "user1", Groups: []string{"admins", "developers"}},
			expectedRole: "admin",
		},
		{
			name: "Groups claim matches editor",
			mappings: []RoleMapping{
				{ClaimName: "groups", ClaimValues: []string{"admins"}, GraphDBRole: "admin"},
				{ClaimName: "groups", ClaimValues: []string{"editors"}, GraphDBRole: "editor"},
			},
			defaultRole:  "viewer",
			claims:       &IDTokenClaims{Subject: "user1", Groups: []string{"editors", "developers"}},
			expectedRole: "editor",
		},
		{
			name: "No match returns default",
			mappings: []RoleMapping{
				{ClaimName: "groups", ClaimValues: []string{"admins"}, GraphDBRole: "admin"},
			},
			defaultRole:  "viewer",
			claims:       &IDTokenClaims{Subject: "user1", Groups: []string{"developers"}},
			expectedRole: "viewer",
		},
		{
			name: "First matching mapping wins",
			mappings: []RoleMapping{
				{ClaimName: "groups", ClaimValues: []string{"superusers"}, GraphDBRole: "admin"},
				{ClaimName: "groups", ClaimValues: []string{"superusers"}, GraphDBRole: "viewer"}, // Same group, lower role
			},
			defaultRole:  "viewer",
			claims:       &IDTokenClaims{Subject: "user1", Groups: []string{"superusers"}},
			expectedRole: "admin", // First match wins
		},
		{
			name: "Roles claim mapping",
			mappings: []RoleMapping{
				{ClaimName: "roles", ClaimValues: []string{"db:admin"}, GraphDBRole: "admin"},
			},
			defaultRole:  "viewer",
			claims:       &IDTokenClaims{Subject: "user1", Roles: []string{"db:admin", "db:user"}},
			expectedRole: "admin",
		},
		{
			name: "Email claim mapping",
			mappings: []RoleMapping{
				{ClaimName: "email", ClaimValues: []string{"admin@example.com"}, GraphDBRole: "admin"},
			},
			defaultRole:  "viewer",
			claims:       &IDTokenClaims{Subject: "user1", Email: "admin@example.com"},
			expectedRole: "admin",
		},
		{
			name: "Custom claim in Raw (Cognito groups)",
			mappings: []RoleMapping{
				{ClaimName: "cognito:groups", ClaimValues: []string{"GraphDB-Admins"}, GraphDBRole: "admin"},
			},
			defaultRole: "viewer",
			claims: &IDTokenClaims{
				Subject: "user1",
				Raw: map[string]any{
					"cognito:groups": []any{"GraphDB-Admins", "Users"},
				},
			},
			expectedRole: "admin",
		},
		{
			name: "Multiple claim values (OR logic)",
			mappings: []RoleMapping{
				{ClaimName: "groups", ClaimValues: []string{"admins", "superusers", "root"}, GraphDBRole: "admin"},
			},
			defaultRole:  "viewer",
			claims:       &IDTokenClaims{Subject: "user1", Groups: []string{"superusers"}},
			expectedRole: "admin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapper := NewRoleMapper(tt.mappings, tt.defaultRole)
			role := mapper.MapRole(tt.claims)

			if role != tt.expectedRole {
				t.Errorf("Expected role %q, got %q", tt.expectedRole, role)
			}
		})
	}
}

func TestRolePriority(t *testing.T) {
	tests := []struct {
		role     string
		expected int
	}{
		{"admin", 3},
		{"editor", 2},
		{"viewer", 1},
		{"unknown", 0},
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			priority := RolePriority(tt.role)
			if priority != tt.expected {
				t.Errorf("Expected priority %d for role %q, got %d", tt.expected, tt.role, priority)
			}
		})
	}
}

func TestHigherPrivilegeRole(t *testing.T) {
	tests := []struct {
		a, b     string
		expected string
	}{
		{"admin", "viewer", "admin"},
		{"viewer", "admin", "admin"},
		{"editor", "viewer", "editor"},
		{"editor", "admin", "admin"},
		{"viewer", "viewer", "viewer"},
		{"admin", "admin", "admin"},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			result := HigherPrivilegeRole(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestRoleMapper_GetClaimValue(t *testing.T) {
	mapper := NewRoleMapper(nil, "viewer")

	claims := &IDTokenClaims{
		Subject:           "user123",
		Issuer:            "https://idp.example.com",
		Email:             "user@example.com",
		EmailVerified:     true,
		PreferredUsername: "testuser",
		Name:              "Test User",
		Groups:            []string{"group1", "group2"},
		Roles:             []string{"role1"},
		Raw: map[string]any{
			"custom_claim": "custom_value",
		},
	}

	tests := []struct {
		claimName string
		expected  any
	}{
		{"sub", "user123"},
		{"iss", "https://idp.example.com"},
		{"email", "user@example.com"},
		{"email_verified", true},
		{"preferred_username", "testuser"},
		{"name", "Test User"},
		{"groups", []string{"group1", "group2"}},
		{"roles", []string{"role1"}},
		{"custom_claim", "custom_value"},
		{"nonexistent", nil},
	}

	for _, tt := range tests {
		t.Run(tt.claimName, func(t *testing.T) {
			value := mapper.getClaimValue(tt.claimName, claims)

			if tt.expected == nil {
				if value != nil {
					t.Errorf("Expected nil, got %v", value)
				}
				return
			}

			// For slices, compare string representations
			switch expected := tt.expected.(type) {
			case []string:
				actual, ok := value.([]string)
				if !ok {
					t.Errorf("Expected []string, got %T", value)
					return
				}
				if len(actual) != len(expected) {
					t.Errorf("Expected %v, got %v", expected, actual)
				}
			default:
				if value != tt.expected {
					t.Errorf("Expected %v, got %v", tt.expected, value)
				}
			}
		})
	}
}
