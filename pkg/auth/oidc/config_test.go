package oidc

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfigFromEnv(t *testing.T) {
	// Save original env and restore after test
	originalEnv := map[string]string{
		"OIDC_ENABLED":           os.Getenv("OIDC_ENABLED"),
		"OIDC_ISSUER":            os.Getenv("OIDC_ISSUER"),
		"OIDC_CLIENT_ID":         os.Getenv("OIDC_CLIENT_ID"),
		"OIDC_CLIENT_SECRET":     os.Getenv("OIDC_CLIENT_SECRET"),
		"OIDC_REDIRECT_URI":      os.Getenv("OIDC_REDIRECT_URI"),
		"OIDC_SCOPES":            os.Getenv("OIDC_SCOPES"),
		"OIDC_DEFAULT_ROLE":      os.Getenv("OIDC_DEFAULT_ROLE"),
		"OIDC_JWKS_CACHE_TTL":    os.Getenv("OIDC_JWKS_CACHE_TTL"),
		"OIDC_ALLOWED_AUDIENCES": os.Getenv("OIDC_ALLOWED_AUDIENCES"),
		"OIDC_ROLE_MAPPINGS":     os.Getenv("OIDC_ROLE_MAPPINGS"),
	}
	defer func() {
		for k, v := range originalEnv {
			if v == "" {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, v)
			}
		}
	}()

	tests := []struct {
		name        string
		envVars     map[string]string
		wantEnabled bool
		wantErr     bool
		errContains string
	}{
		{
			name: "OIDC disabled returns valid config",
			envVars: map[string]string{
				"OIDC_ENABLED": "false",
			},
			wantEnabled: false,
			wantErr:     false,
		},
		{
			name: "OIDC enabled with valid config",
			envVars: map[string]string{
				"OIDC_ENABLED":       "true",
				"OIDC_ISSUER":        "https://accounts.google.com",
				"OIDC_CLIENT_ID":     "test-client-id",
				"OIDC_CLIENT_SECRET": "test-client-secret",
			},
			wantEnabled: true,
			wantErr:     false,
		},
		{
			name: "OIDC enabled without issuer fails",
			envVars: map[string]string{
				"OIDC_ENABLED":   "true",
				"OIDC_CLIENT_ID": "test-client-id",
			},
			wantEnabled: true,
			wantErr:     true,
			errContains: "OIDC_ISSUER is required",
		},
		{
			name: "OIDC enabled without client_id fails",
			envVars: map[string]string{
				"OIDC_ENABLED": "true",
				"OIDC_ISSUER":  "https://accounts.google.com",
			},
			wantEnabled: true,
			wantErr:     true,
			errContains: "OIDC_CLIENT_ID is required",
		},
		{
			name: "Invalid default role fails",
			envVars: map[string]string{
				"OIDC_ENABLED":      "true",
				"OIDC_ISSUER":       "https://accounts.google.com",
				"OIDC_CLIENT_ID":    "test-client-id",
				"OIDC_DEFAULT_ROLE": "superadmin",
			},
			wantEnabled: true,
			wantErr:     true,
			errContains: "invalid OIDC_DEFAULT_ROLE",
		},
		{
			name: "Valid role mapping",
			envVars: map[string]string{
				"OIDC_ENABLED":       "true",
				"OIDC_ISSUER":        "https://accounts.google.com",
				"OIDC_CLIENT_ID":     "test-client-id",
				"OIDC_ROLE_MAPPINGS": `[{"claim_name":"groups","claim_value":["admins"],"graphdb_role":"admin"}]`,
			},
			wantEnabled: true,
			wantErr:     false,
		},
		{
			name: "Invalid role mapping JSON",
			envVars: map[string]string{
				"OIDC_ENABLED":       "true",
				"OIDC_ISSUER":        "https://accounts.google.com",
				"OIDC_CLIENT_ID":     "test-client-id",
				"OIDC_ROLE_MAPPINGS": `invalid json`,
			},
			wantEnabled: true,
			wantErr:     true,
			errContains: "invalid OIDC_ROLE_MAPPINGS JSON",
		},
		{
			name: "Role mapping with invalid graphdb_role",
			envVars: map[string]string{
				"OIDC_ENABLED":       "true",
				"OIDC_ISSUER":        "https://accounts.google.com",
				"OIDC_CLIENT_ID":     "test-client-id",
				"OIDC_ROLE_MAPPINGS": `[{"claim_name":"groups","claim_value":["admins"],"graphdb_role":"superadmin"}]`,
			},
			wantEnabled: true,
			wantErr:     true,
			errContains: "invalid graphdb_role",
		},
		{
			name: "Custom scopes parsed correctly",
			envVars: map[string]string{
				"OIDC_ENABLED":   "true",
				"OIDC_ISSUER":    "https://accounts.google.com",
				"OIDC_CLIENT_ID": "test-client-id",
				"OIDC_SCOPES":    "openid, profile, email, groups",
			},
			wantEnabled: true,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all OIDC env vars
			for k := range originalEnv {
				os.Unsetenv(k)
			}

			// Set test env vars
			for k, v := range tt.envVars {
				os.Setenv(k, v)
			}

			config, err := LoadConfigFromEnv()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if config.Enabled != tt.wantEnabled {
				t.Errorf("Expected Enabled=%v, got %v", tt.wantEnabled, config.Enabled)
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		wantErr     bool
		errContains string
	}{
		{
			name: "Valid config",
			config: Config{
				Issuer:       "https://accounts.google.com",
				ClientID:     "test-client-id",
				DefaultRole:  "viewer",
				JWKSCacheTTL: time.Hour,
			},
			wantErr: false,
		},
		{
			name: "Missing issuer",
			config: Config{
				ClientID:     "test-client-id",
				DefaultRole:  "viewer",
				JWKSCacheTTL: time.Hour,
			},
			wantErr:     true,
			errContains: "OIDC_ISSUER is required",
		},
		{
			name: "Missing client_id",
			config: Config{
				Issuer:       "https://accounts.google.com",
				DefaultRole:  "viewer",
				JWKSCacheTTL: time.Hour,
			},
			wantErr:     true,
			errContains: "OIDC_CLIENT_ID is required",
		},
		{
			name: "Invalid default role",
			config: Config{
				Issuer:       "https://accounts.google.com",
				ClientID:     "test-client-id",
				DefaultRole:  "superadmin",
				JWKSCacheTTL: time.Hour,
			},
			wantErr:     true,
			errContains: "invalid OIDC_DEFAULT_ROLE",
		},
		{
			name: "JWKS cache TTL too short",
			config: Config{
				Issuer:       "https://accounts.google.com",
				ClientID:     "test-client-id",
				DefaultRole:  "viewer",
				JWKSCacheTTL: 30 * time.Second,
			},
			wantErr:     true,
			errContains: "OIDC_JWKS_CACHE_TTL must be at least 1 minute",
		},
		{
			name: "Role mapping missing claim_name",
			config: Config{
				Issuer:       "https://accounts.google.com",
				ClientID:     "test-client-id",
				DefaultRole:  "viewer",
				JWKSCacheTTL: time.Hour,
				RoleMappings: []RoleMapping{
					{ClaimValues: []string{"admins"}, GraphDBRole: "admin"},
				},
			},
			wantErr:     true,
			errContains: "claim_name is required",
		},
		{
			name: "Role mapping empty claim_values",
			config: Config{
				Issuer:       "https://accounts.google.com",
				ClientID:     "test-client-id",
				DefaultRole:  "viewer",
				JWKSCacheTTL: time.Hour,
				RoleMappings: []RoleMapping{
					{ClaimName: "groups", ClaimValues: []string{}, GraphDBRole: "admin"},
				},
			},
			wantErr:     true,
			errContains: "claim_value must have at least one value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errContains)
					return
				}
				if tt.errContains != "" && !containsString(err.Error(), tt.errContains) {
					t.Errorf("Expected error containing %q, got %q", tt.errContains, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestConfig_GetAllowedAudiences(t *testing.T) {
	config := Config{
		ClientID:         "my-client-id",
		AllowedAudiences: []string{"extra-audience-1", "extra-audience-2"},
	}

	audiences := config.GetAllowedAudiences()

	if len(audiences) != 3 {
		t.Errorf("Expected 3 audiences, got %d", len(audiences))
	}

	if audiences[0] != "my-client-id" {
		t.Errorf("First audience should be ClientID, got %s", audiences[0])
	}
}

func TestDefaultScopes(t *testing.T) {
	scopes := DefaultScopes()

	expected := []string{"openid", "profile", "email"}
	if len(scopes) != len(expected) {
		t.Errorf("Expected %d scopes, got %d", len(expected), len(scopes))
	}

	for i, scope := range expected {
		if scopes[i] != scope {
			t.Errorf("Expected scope %q at index %d, got %q", scope, i, scopes[i])
		}
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
