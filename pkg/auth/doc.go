// Package auth handles authentication for graphdb: JWT tokens and API keys.
//
// [JWTManager] (from [NewJWTManager]) issues and validates JWTs whose [Claims]
// carry the user ID, username, role (admin/editor/viewer), and optional tenant.
// [UserStore] (from [NewUserStore]) manages users and password verification,
// [APIKeyStore] manages hashed API keys, and the HTTP handlers cover
// login/refresh and user management. Token validation can compose JWT with OIDC
// when configured.
//
// The signing secret comes from the JWT_SECRET environment variable (minimum 32
// characters) and is never persisted — the same secret must be supplied wherever
// tokens are minted (e.g. the graphdb-admin mint-token command).
package auth
