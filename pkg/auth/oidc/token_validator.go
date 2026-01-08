package oidc

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"strings"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/auth"
)

var (
	ErrTokenMalformed    = errors.New("token is malformed")
	ErrTokenExpired      = errors.New("token has expired")
	ErrInvalidSignature  = errors.New("invalid token signature")
	ErrInvalidIssuer     = errors.New("invalid token issuer")
	ErrInvalidAudience   = errors.New("invalid token audience")
	ErrMissingSubject    = errors.New("token missing subject claim")
	ErrUnsupportedAlg    = errors.New("unsupported signing algorithm")
)

// OIDCTokenValidator validates OIDC ID tokens using RS256/384/512
type OIDCTokenValidator struct {
	config          *Config
	discoveryClient *DiscoveryClient
	jwksClient      *JWKSClient
	roleMapper      *RoleMapper
}

// NewOIDCTokenValidator creates a new OIDC token validator
func NewOIDCTokenValidator(config *Config) *OIDCTokenValidator {
	return &OIDCTokenValidator{
		config:          config,
		discoveryClient: NewDiscoveryClient(),
		jwksClient:      NewJWKSClient(config.JWKSCacheTTL),
		roleMapper:      NewRoleMapper(config.RoleMappings, config.DefaultRole),
	}
}

// NewOIDCTokenValidatorWithClients creates a validator with custom clients (for testing)
func NewOIDCTokenValidatorWithClients(
	config *Config,
	discoveryClient *DiscoveryClient,
	jwksClient *JWKSClient,
) *OIDCTokenValidator {
	return &OIDCTokenValidator{
		config:          config,
		discoveryClient: discoveryClient,
		jwksClient:      jwksClient,
		roleMapper:      NewRoleMapper(config.RoleMappings, config.DefaultRole),
	}
}

// ValidateToken validates an OIDC ID token and returns GraphDB claims.
// Implements the auth.TokenValidator interface.
func (v *OIDCTokenValidator) ValidateToken(ctx context.Context, tokenString string) (*auth.Claims, error) {
	// Parse token structure (header.payload.signature)
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, ErrTokenMalformed
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid header encoding", ErrTokenMalformed)
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid payload encoding", ErrTokenMalformed)
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid signature encoding", ErrTokenMalformed)
	}

	// Parse header to get algorithm and key ID
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("%w: invalid header JSON", ErrTokenMalformed)
	}

	// Validate algorithm is supported RS* algorithm
	if !isSupportedAlgorithm(header.Alg) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedAlg, header.Alg)
	}

	// Parse claims
	var claims IDTokenClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, fmt.Errorf("%w: invalid payload JSON", ErrTokenMalformed)
	}

	// Store raw claims for role mapping
	var rawClaims map[string]any
	if err := json.Unmarshal(payloadJSON, &rawClaims); err != nil {
		return nil, fmt.Errorf("%w: invalid payload JSON", ErrTokenMalformed)
	}
	claims.Raw = rawClaims

	// Validate issuer
	if claims.Issuer != v.config.Issuer && claims.Issuer != v.config.Issuer+"/" {
		return nil, fmt.Errorf("%w: expected %s, got %s", ErrInvalidIssuer, v.config.Issuer, claims.Issuer)
	}

	// Validate audience
	if !v.validateAudience(&claims) {
		return nil, ErrInvalidAudience
	}

	// Validate expiration
	if time.Now().Unix() > claims.ExpiresAt {
		return nil, ErrTokenExpired
	}

	// Validate signature
	if err := v.validateSignature(ctx, parts[0]+"."+parts[1], signature, header.Alg, header.Kid); err != nil {
		return nil, err
	}

	// Validate subject exists
	if claims.Subject == "" {
		return nil, ErrMissingSubject
	}

	// Map to GraphDB claims
	graphDBRole := v.roleMapper.MapRole(&claims)

	return &auth.Claims{
		UserID:    claims.Subject,
		Username:  v.extractUsername(&claims),
		Role:      graphDBRole,
		ExpiresAt: time.Unix(claims.ExpiresAt, 0),
		IssuedAt:  time.Unix(claims.IssuedAt, 0),
	}, nil
}

// Name returns the validator name for logging/debugging.
// Implements the auth.TokenValidator interface.
func (v *OIDCTokenValidator) Name() string {
	return "oidc-rs256"
}

// validateAudience checks if the token audience matches any allowed audience
func (v *OIDCTokenValidator) validateAudience(claims *IDTokenClaims) bool {
	allowedAudiences := v.config.GetAllowedAudiences()
	tokenAudiences := claims.GetAudiences()

	for _, allowed := range allowedAudiences {
		for _, tokenAud := range tokenAudiences {
			if allowed == tokenAud {
				return true
			}
		}
	}
	return false
}

// validateSignature verifies the token signature using the JWKS
func (v *OIDCTokenValidator) validateSignature(ctx context.Context, signingInput string, signature []byte, alg, kid string) error {
	// Get discovery document to find JWKS URL
	discovery, err := v.discoveryClient.GetDiscovery(ctx, v.config.Issuer)
	if err != nil {
		return fmt.Errorf("failed to fetch discovery document: %w", err)
	}

	// Get the public key
	key, err := v.jwksClient.GetKey(ctx, discovery.JWKSUri, kid)
	if err != nil {
		return fmt.Errorf("failed to get signing key: %w", err)
	}

	// Verify signature based on algorithm
	var hashFunc hash.Hash
	var cryptoHash crypto.Hash

	switch alg {
	case "RS256":
		hashFunc = sha256.New()
		cryptoHash = crypto.SHA256
	case "RS384":
		hashFunc = sha512.New384()
		cryptoHash = crypto.SHA384
	case "RS512":
		hashFunc = sha512.New()
		cryptoHash = crypto.SHA512
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedAlg, alg)
	}

	hashFunc.Write([]byte(signingInput))
	hashed := hashFunc.Sum(nil)

	if err := rsa.VerifyPKCS1v15(key, cryptoHash, hashed, signature); err != nil {
		return ErrInvalidSignature
	}

	return nil
}

// extractUsername determines the best username from OIDC claims
func (v *OIDCTokenValidator) extractUsername(claims *IDTokenClaims) string {
	// Priority: preferred_username > email > name > subject
	if claims.PreferredUsername != "" {
		return claims.PreferredUsername
	}
	if claims.Email != "" {
		return claims.Email
	}
	if claims.Name != "" {
		return claims.Name
	}
	return claims.Subject
}

// isSupportedAlgorithm checks if the algorithm is a supported RS* algorithm
func isSupportedAlgorithm(alg string) bool {
	switch alg {
	case "RS256", "RS384", "RS512":
		return true
	default:
		return false
	}
}

// GetOIDCClaims extracts and returns the full OIDC claims from a token
// (useful for user provisioning)
func (v *OIDCTokenValidator) GetOIDCClaims(ctx context.Context, tokenString string) (*IDTokenClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, ErrTokenMalformed
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid payload encoding", ErrTokenMalformed)
	}

	var claims IDTokenClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, fmt.Errorf("%w: invalid payload JSON", ErrTokenMalformed)
	}

	// Store raw claims
	var rawClaims map[string]any
	if err := json.Unmarshal(payloadJSON, &rawClaims); err != nil {
		return nil, fmt.Errorf("%w: invalid payload JSON", ErrTokenMalformed)
	}
	claims.Raw = rawClaims

	return &claims, nil
}
