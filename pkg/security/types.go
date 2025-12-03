package security

// VulnerabilityType represents the type of security vulnerability
type VulnerabilityType string

const (
	VulnInjection         VulnerabilityType = "injection"
	VulnPathTraversal     VulnerabilityType = "path_traversal"
	VulnWeakCrypto        VulnerabilityType = "weak_crypto"
	VulnWeakPassword      VulnerabilityType = "weak_password"
	VulnMissingAuth       VulnerabilityType = "missing_auth"
	VulnInsecureTransport VulnerabilityType = "insecure_transport"
	VulnXSS               VulnerabilityType = "xss"
	VulnCSRF              VulnerabilityType = "csrf"
	VulnRateLimit         VulnerabilityType = "rate_limit"
	VulnInfoDisclosure    VulnerabilityType = "info_disclosure"
)

// Severity represents the severity of a vulnerability
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Vulnerability represents a detected security vulnerability
type Vulnerability struct {
	Type        VulnerabilityType
	Severity    Severity
	Description string
	Location    string
	Remediation string
	CVE         string
	CVSS        float64
}
