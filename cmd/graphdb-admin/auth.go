package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dd0wney/graphdb/pkg/auth"
)

// mintToken signs a JWT offline using secret — the same secret the server is
// configured with (JWT_SECRET). Pure helper so it's unit-testable without the
// flag-parsing / os.Exit shell around it (#226).
func mintToken(secret, userID, username, role, tenant string, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", fmt.Errorf("JWT_SECRET is required (use the same secret the server is configured with)")
	}
	mgr, err := auth.NewJWTManager(secret, ttl, auth.DefaultRefreshTokenDuration)
	if err != nil {
		return "", err
	}
	return mgr.GenerateTokenWithTenant(userID, username, role, tenant)
}

// login posts credentials to a running server's POST /auth/login and returns
// the access + refresh tokens.
func login(serverURL, username, password string) (accessToken, refreshToken string, err error) {
	body := map[string]string{"username": username, "password": password}
	respBody, err := makeAPIRequest(http.MethodPost, serverURL+"/auth/login", &SecurityConfig{ServerURL: serverURL}, body)
	if err != nil {
		return "", "", err
	}
	var resp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", "", fmt.Errorf("could not parse login response: %w", err)
	}
	return resp.AccessToken, resp.RefreshToken, nil
}

// handleLoginCommand logs in against a running server and prints the access
// token, so integrators don't reverse-engineer the auth flow each time (#226).
func handleLoginCommand(args []string) {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	serverURL := fs.String("server-url", getEnvOrDefault("GRAPHDB_SERVER_URL", "http://localhost:8080"), "GraphDB server URL")
	username := fs.String("username", os.Getenv("GRAPHDB_USERNAME"), "Username")
	password := fs.String("password", os.Getenv("GRAPHDB_PASSWORD"), "Password (prefer the GRAPHDB_PASSWORD env var over the flag)")
	showRefresh := fs.Bool("show-refresh", false, "Also print the refresh token (to stderr)")
	_ = fs.Parse(args)

	if *username == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "Error: --username and --password (or GRAPHDB_USERNAME/GRAPHDB_PASSWORD) are required")
		os.Exit(1)
	}

	accessToken, refreshToken, err := login(*serverURL, *username, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: login failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(accessToken)
	if *showRefresh {
		fmt.Fprintf(os.Stderr, "refresh_token: %s\n", refreshToken)
	}
}

// handleMintTokenCommand mints a JWT offline from JWT_SECRET for scripting/ops
// without a login round-trip (#226). JWT_SECRET is never persisted server-side,
// so the caller must supply it via the environment.
func handleMintTokenCommand(args []string) {
	fs := flag.NewFlagSet("mint-token", flag.ExitOnError)
	username := fs.String("username", "", "Username to embed in the token (required)")
	userID := fs.String("user-id", "", "User ID to embed (defaults to --username)")
	// Default to the least-privileged role (security audit M-6). Minting
	// admin tokens must be a conscious choice — runbooks that copy the
	// example without --role should not silently produce admin credentials.
	role := fs.String("role", auth.RoleViewer, "Role: admin, editor, or viewer")
	tenant := fs.String("tenant", "", "Tenant ID (empty = default tenant)")
	ttl := fs.Duration("ttl", auth.DefaultTokenDuration, "Token lifetime (e.g. 15m, 24h)")
	_ = fs.Parse(args)

	if *username == "" {
		fmt.Fprintln(os.Stderr, "Error: --username is required")
		os.Exit(1)
	}
	id := *userID
	if id == "" {
		id = *username
	}

	token, err := mintToken(os.Getenv("JWT_SECRET"), id, *username, *role, *tenant, *ttl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not mint token: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(token)
	fmt.Fprintf(os.Stderr, "Minted %s token for %q (user-id %q, tenant %q), valid %s\n",
		*role, *username, id, tenantLabel(*tenant), *ttl)
	if *role == auth.RoleAdmin {
		fmt.Fprintln(os.Stderr,
			"WARNING: minted an ADMIN token — use --role viewer or --role editor for service accounts and time-limited sessions.")
	}
}

func tenantLabel(t string) string {
	if t == "" {
		return "default"
	}
	return t
}

func printAuthUsage() {
	usage := `Authentication commands

Usage:
  graphdb-admin login [options]        Log in to a running server, print access token
  graphdb-admin mint-token [options]   Mint a JWT offline from JWT_SECRET

login flags:
  --server-url URL   GraphDB server URL (default: http://localhost:8080)
  --username NAME    Username (or GRAPHDB_USERNAME)
  --password PASS    Password (prefer the GRAPHDB_PASSWORD env var)
  --show-refresh     Also print the refresh token (to stderr)

mint-token flags (requires JWT_SECRET in the environment):
  --username NAME    Username to embed (required)
  --user-id ID       User ID to embed (default: --username)
  --role ROLE        admin | editor | viewer (default: viewer)
  --tenant ID        Tenant ID (default: the default tenant)
  --ttl DURATION     Token lifetime, e.g. 15m, 24h (default: 15m)

Examples:
  # Log in and capture a token for subsequent admin calls
  export GRAPHDB_PASSWORD=...; TOKEN=$(graphdb-admin login --username admin)

  # Mint an admin token offline (CI / ops) — admin must be explicit
  export JWT_SECRET=...; graphdb-admin mint-token --username admin --role admin --ttl 24h
`
	fmt.Print(usage)
}
