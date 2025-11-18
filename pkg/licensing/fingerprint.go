package licensing

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"runtime"
	"sort"
	"strings"
)

// HardwareFingerprint represents a unique hardware identifier
type HardwareFingerprint struct {
	Hash       string            `json:"hash"`
	Components map[string]string `json:"components"`
	Generated  string            `json:"generated_at"`
}

// GenerateFingerprint creates a unique hardware fingerprint based on:
// - CPU architecture and cores
// - Primary MAC address
// - Hostname
// This allows licenses to be tied to specific deployments
func GenerateFingerprint() (*HardwareFingerprint, error) {
	components := make(map[string]string)

	// Get CPU information
	components["cpu_arch"] = runtime.GOARCH
	components["cpu_os"] = runtime.GOOS
	components["cpu_cores"] = fmt.Sprintf("%d", runtime.NumCPU())

	// Get hostname
	hostname, err := os.Hostname()
	if err != nil {
		// Non-fatal - use empty string
		hostname = "unknown"
	}
	components["hostname"] = hostname

	// Get primary MAC address
	macAddr, err := getPrimaryMACAddress()
	if err != nil {
		// Non-fatal - use empty string
		macAddr = "unknown"
	}
	components["mac_address"] = macAddr

	// Create deterministic hash from components
	hash := hashComponents(components)

	return &HardwareFingerprint{
		Hash:       hash,
		Components: components,
		Generated:  "",
	}, nil
}

// getPrimaryMACAddress returns the MAC address of the first non-loopback interface
func getPrimaryMACAddress() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	// Sort interfaces by name for deterministic ordering
	sort.Slice(interfaces, func(i, j int) bool {
		return interfaces[i].Name < interfaces[j].Name
	})

	for _, iface := range interfaces {
		// Skip loopback and interfaces without MAC addresses
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if len(iface.HardwareAddr) == 0 {
			continue
		}

		// Return first valid MAC address
		return iface.HardwareAddr.String(), nil
	}

	return "", fmt.Errorf("no network interfaces found")
}

// hashComponents creates a SHA-256 hash from the hardware components
func hashComponents(components map[string]string) string {
	// Sort keys for deterministic hash
	keys := make([]string, 0, len(components))
	for k := range components {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build concatenated string
	var builder strings.Builder
	for _, k := range keys {
		builder.WriteString(k)
		builder.WriteString("=")
		builder.WriteString(components[k])
		builder.WriteString(";")
	}

	// Hash the concatenated string
	hash := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(hash[:])
}

// VerifyFingerprint checks if the current hardware matches the provided fingerprint
func VerifyFingerprint(expectedHash string) (bool, error) {
	current, err := GenerateFingerprint()
	if err != nil {
		return false, err
	}

	return current.Hash == expectedHash, nil
}

// BindLicenseToFingerprint stores the hardware fingerprint in license metadata
func (l *License) BindToFingerprint(fingerprint *HardwareFingerprint) {
	if l.Metadata == nil {
		l.Metadata = make(map[string]string)
	}
	l.Metadata["hardware_fingerprint"] = fingerprint.Hash
}

// VerifyHardwareBinding checks if the license is bound to the current hardware
// Returns true if:
// - License has no hardware binding (not enforced)
// - Current hardware matches the bound fingerprint
func (l *License) VerifyHardwareBinding() (bool, error) {
	if l.Metadata == nil {
		// No binding - allow
		return true, nil
	}

	expectedHash, exists := l.Metadata["hardware_fingerprint"]
	if !exists {
		// No binding - allow
		return true, nil
	}

	// Verify current hardware matches
	return VerifyFingerprint(expectedHash)
}

// GetFingerprint returns the hardware fingerprint bound to this license, if any
func (l *License) GetFingerprint() (string, bool) {
	if l.Metadata == nil {
		return "", false
	}
	hash, exists := l.Metadata["hardware_fingerprint"]
	return hash, exists
}
