package compliance

import (
	"time"
)

// Framework represents a compliance framework
type Framework string

const (
	FrameworkGDPR     Framework = "GDPR"
	FrameworkSOC2     Framework = "SOC2"
	FrameworkHIPAA    Framework = "HIPAA"
	FrameworkPCIDSS   Framework = "PCI-DSS"
	FrameworkFIPS1402 Framework = "FIPS-140-2"
	FrameworkISO27001 Framework = "ISO-27001"
)

// ComplianceStatus represents the status of a control
type ComplianceStatus string

const (
	StatusCompliant     ComplianceStatus = "compliant"
	StatusPartial       ComplianceStatus = "partial"
	StatusNonCompliant  ComplianceStatus = "non_compliant"
	StatusNotApplicable ComplianceStatus = "not_applicable"
)

// Control represents a single compliance control
type Control struct {
	ID          string           `json:"id"`
	Framework   Framework        `json:"framework"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Status      ComplianceStatus `json:"status"`
	Evidence    []Evidence       `json:"evidence,omitempty"`
	Notes       string           `json:"notes,omitempty"`
	LastChecked time.Time        `json:"last_checked"`
}

// Evidence represents evidence of compliance
type Evidence struct {
	Type        string    `json:"type"`
	Description string    `json:"description"`
	Source      string    `json:"source"`
	Timestamp   time.Time `json:"timestamp"`
	Data        string    `json:"data,omitempty"`
}

// ComplianceReport represents a comprehensive compliance report
type ComplianceReport struct {
	Framework    Framework         `json:"framework"`
	GeneratedAt  time.Time         `json:"generated_at"`
	Version      string            `json:"version"`
	Organization string            `json:"organization"`
	Controls     []Control         `json:"controls"`
	Summary      ComplianceSummary `json:"summary"`
}

// ComplianceSummary provides an overview of compliance status
type ComplianceSummary struct {
	TotalControls        int     `json:"total_controls"`
	CompliantControls    int     `json:"compliant_controls"`
	PartialControls      int     `json:"partial_controls"`
	NonCompliantControls int     `json:"non_compliant_controls"`
	NotApplicable        int     `json:"not_applicable"`
	ComplianceScore      float64 `json:"compliance_score"` // 0-100%
}

// SystemInfo holds information about the system being checked
type SystemInfo struct {
	EncryptionEnabled     bool
	TLSEnabled            bool
	AuditLoggingEnabled   bool
	DataMaskingEnabled    bool
	KeyRotationEnabled    bool
	AuthenticationEnabled bool
	AccessControlEnabled  bool
}

// GetFrameworks returns all supported frameworks
func GetFrameworks() []Framework {
	return []Framework{
		FrameworkGDPR,
		FrameworkSOC2,
		FrameworkHIPAA,
		FrameworkPCIDSS,
		FrameworkFIPS1402,
		FrameworkISO27001,
	}
}

// Helper functions

func contains(s string, substrs ...string) bool {
	for _, substr := range substrs {
		if len(s) >= len(substr) {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
		}
	}
	return false
}

func getStatusEmoji(status ComplianceStatus) string {
	switch status {
	case StatusCompliant:
		return "✅"
	case StatusPartial:
		return "⚠️"
	case StatusNonCompliant:
		return "❌"
	case StatusNotApplicable:
		return "➖"
	default:
		return "❓"
	}
}
