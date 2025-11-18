package editions

import (
	"fmt"
	"os"
	"strings"
)

// Edition represents the GraphDB edition (Community or Enterprise)
type Edition int

const (
	// Community is the open-source edition with custom vector search
	Community Edition = iota
	// Enterprise includes Cloudflare integrations (Vectorize, R2, Queues)
	Enterprise
)

// String returns the string representation of the edition
func (e Edition) String() string {
	switch e {
	case Community:
		return "Community"
	case Enterprise:
		return "Enterprise"
	default:
		return "Unknown"
	}
}

// Current holds the currently active edition
var Current Edition = Community

// DetectEdition determines the edition from environment variables,
// config files, or license keys
func DetectEdition() Edition {
	// 1. Check environment variable (highest priority)
	if env := os.Getenv("GRAPHDB_EDITION"); env != "" {
		switch strings.ToLower(env) {
		case "enterprise", "ent":
			return Enterprise
		case "community", "ce":
			return Community
		default:
			fmt.Printf("Warning: Unknown edition '%s', defaulting to Community\n", env)
			return Community
		}
	}

	// 2. Check for license key file (indicates Enterprise)
	if licenseExists() {
		return Enterprise
	}

	// 3. Default to Community
	return Community
}

// Initialize sets up the edition based on detection
func Initialize() {
	Current = DetectEdition()
	fmt.Printf("GraphDB Edition: %s\n", Current)
}

// IsEnterprise returns true if running Enterprise edition
func IsEnterprise() bool {
	return Current == Enterprise
}

// IsCommunity returns true if running Community edition
func IsCommunity() bool {
	return Current == Community
}

// RequireEnterprise returns an error if not running Enterprise edition
func RequireEnterprise(feature string) error {
	if !IsEnterprise() {
		return fmt.Errorf("%s requires Enterprise edition (current: %s)", feature, Current)
	}
	return nil
}

// licenseExists checks if a valid license key file exists
func licenseExists() bool {
	// Check for license file in multiple locations
	paths := []string{
		os.Getenv("GRAPHDB_LICENSE_PATH"),
		"/etc/graphdb/license.key",
		"./license.key",
		"./config/license.key",
	}

	for _, path := range paths {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}

	return false
}
