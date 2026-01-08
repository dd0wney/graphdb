// Package oidc provides OpenID Connect authentication support for GraphDB.
package oidc

import "time"

// OIDCDiscovery represents the OIDC discovery document (.well-known/openid-configuration)
type OIDCDiscovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	JWKSUri               string   `json:"jwks_uri"`
	ScopesSupported       []string `json:"scopes_supported"`
	ClaimsSupported       []string `json:"claims_supported"`
}

// TokenResponse represents the response from the token endpoint
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	IDToken      string `json:"id_token"`
	Scope        string `json:"scope,omitempty"`
}

// IDTokenClaims represents standard OIDC ID token claims
type IDTokenClaims struct {
	// Standard claims
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	Audience  any    `json:"aud"` // Can be string or []string
	ExpiresAt int64  `json:"exp"`
	IssuedAt  int64  `json:"iat"`
	AuthTime  int64  `json:"auth_time,omitempty"`
	Nonce     string `json:"nonce,omitempty"`

	// Profile claims
	Name              string `json:"name,omitempty"`
	GivenName         string `json:"given_name,omitempty"`
	FamilyName        string `json:"family_name,omitempty"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Picture           string `json:"picture,omitempty"`

	// Email claims
	Email         string `json:"email,omitempty"`
	EmailVerified bool   `json:"email_verified,omitempty"`

	// Additional claims for role mapping
	Groups []string `json:"groups,omitempty"`
	Roles  []string `json:"roles,omitempty"`

	// Raw claims map for custom claim access
	Raw map[string]any `json:"-"`
}

// GetAudiences returns the audience claim as a slice (handles both string and array)
func (c *IDTokenClaims) GetAudiences() []string {
	switch aud := c.Audience.(type) {
	case string:
		return []string{aud}
	case []any:
		result := make([]string, 0, len(aud))
		for _, a := range aud {
			if s, ok := a.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return aud
	default:
		return nil
	}
}

// JWK represents a JSON Web Key
type JWK struct {
	Kty string `json:"kty"` // Key type (RSA, EC)
	Kid string `json:"kid"` // Key ID
	Use string `json:"use"` // Key use (sig)
	Alg string `json:"alg"` // Algorithm (RS256, RS384, RS512)

	// RSA key components
	N string `json:"n,omitempty"` // RSA modulus
	E string `json:"e,omitempty"` // RSA exponent

	// EC key components (for future support)
	Crv string `json:"crv,omitempty"` // Curve name
	X   string `json:"x,omitempty"`   // X coordinate
	Y   string `json:"y,omitempty"`   // Y coordinate
}

// JWKS represents a JSON Web Key Set
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// CachedJWKS holds JWKS with cache metadata
type CachedJWKS struct {
	JWKS      *JWKS
	FetchedAt time.Time
	ExpiresAt time.Time
}

// RoleMapping defines how to map OIDC claims to GraphDB roles
type RoleMapping struct {
	// ClaimName is the OIDC claim to check (e.g., "groups", "roles", "cognito:groups")
	ClaimName string `json:"claim_name"`

	// ClaimValues are the values that trigger this mapping (OR logic)
	ClaimValues []string `json:"claim_value"`

	// GraphDBRole is the target GraphDB role ("admin", "editor", "viewer")
	GraphDBRole string `json:"graphdb_role"`
}

// StateEntry holds CSRF state with expiration
type StateEntry struct {
	CreatedAt time.Time
	Nonce     string
}
