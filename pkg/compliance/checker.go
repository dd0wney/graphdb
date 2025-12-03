package compliance

import (
	"fmt"
	"time"
)

// ComplianceChecker checks compliance status for various frameworks
type ComplianceChecker struct {
	controls   map[Framework][]Control
	systemInfo SystemInfo
}

// NewComplianceChecker creates a new compliance checker
func NewComplianceChecker(info SystemInfo) *ComplianceChecker {
	checker := &ComplianceChecker{
		controls:   make(map[Framework][]Control),
		systemInfo: info,
	}

	// Initialize controls for each framework
	checker.initializeGDPRControls()
	checker.initializeSOC2Controls()
	checker.initializeHIPAAControls()
	checker.initializePCIDSSControls()
	checker.initializeFIPS1402Controls()
	checker.initializeISO27001Controls()

	return checker
}

// CheckCompliance evaluates compliance for a specific framework
func (c *ComplianceChecker) CheckCompliance(framework Framework) (*ComplianceReport, error) {
	controls, exists := c.controls[framework]
	if !exists {
		return nil, fmt.Errorf("framework %s not supported", framework)
	}

	// Evaluate each control
	evaluatedControls := make([]Control, len(controls))
	for i, control := range controls {
		evaluatedControls[i] = c.evaluateControl(control)
	}

	// Generate summary
	summary := c.calculateSummary(evaluatedControls)

	report := &ComplianceReport{
		Framework:    framework,
		GeneratedAt:  time.Now(),
		Version:      "1.0.0",
		Organization: "GraphDB",
		Controls:     evaluatedControls,
		Summary:      summary,
	}

	return report, nil
}

// evaluateControl evaluates a single control based on system info
func (c *ComplianceChecker) evaluateControl(control Control) Control {
	now := time.Now()
	control.LastChecked = now

	// Evaluate based on control ID patterns
	switch {
	// Encryption controls
	case contains(control.ID, "ENCRYPT", "CRYPTO"):
		if c.systemInfo.EncryptionEnabled {
			control.Status = StatusCompliant
			control.Evidence = append(control.Evidence, Evidence{
				Type:        "Configuration",
				Description: "AES-256-GCM encryption enabled for data at rest",
				Source:      "Encryption Engine",
				Timestamp:   now,
			})
		} else {
			control.Status = StatusNonCompliant
		}

	// TLS/Network security controls
	case contains(control.ID, "TLS", "TRANSPORT", "NETWORK"):
		if c.systemInfo.TLSEnabled {
			control.Status = StatusCompliant
			control.Evidence = append(control.Evidence, Evidence{
				Type:        "Configuration",
				Description: "TLS 1.2+ enabled for all network communication",
				Source:      "TLS Configuration",
				Timestamp:   now,
			})
		} else {
			control.Status = StatusNonCompliant
		}

	// Audit logging controls
	case contains(control.ID, "AUDIT", "LOG", "MONITOR"):
		if c.systemInfo.AuditLoggingEnabled {
			control.Status = StatusCompliant
			control.Evidence = append(control.Evidence, Evidence{
				Type:        "Configuration",
				Description: "Comprehensive audit logging with tamper-proof hash chains",
				Source:      "Audit Logger",
				Timestamp:   now,
			})
		} else {
			control.Status = StatusNonCompliant
		}

	// Data masking/privacy controls
	case contains(control.ID, "PRIVACY", "MASK", "ANONYMIZE"):
		if c.systemInfo.DataMaskingEnabled {
			control.Status = StatusCompliant
			control.Evidence = append(control.Evidence, Evidence{
				Type:        "Configuration",
				Description: "PII masking enabled for logs and exports",
				Source:      "Data Masking",
				Timestamp:   now,
			})
		} else {
			control.Status = StatusPartial
			control.Notes = "Data masking available but may not be enabled for all outputs"
		}

	// Key management controls
	case contains(control.ID, "KEY", "ROTATION"):
		if c.systemInfo.KeyRotationEnabled {
			control.Status = StatusCompliant
			control.Evidence = append(control.Evidence, Evidence{
				Type:        "Configuration",
				Description: "Key rotation supported with versioning",
				Source:      "Key Manager",
				Timestamp:   now,
			})
		} else {
			control.Status = StatusPartial
		}

	// Access control
	case contains(control.ID, "ACCESS", "AUTH"):
		if c.systemInfo.AuthenticationEnabled && c.systemInfo.AccessControlEnabled {
			control.Status = StatusCompliant
			control.Evidence = append(control.Evidence, Evidence{
				Type:        "Configuration",
				Description: "JWT-based authentication with role-based access control",
				Source:      "Authentication System",
				Timestamp:   now,
			})
		} else {
			control.Status = StatusPartial
		}

	default:
		// Default to partial if we can't automatically determine
		control.Status = StatusPartial
		control.Notes = "Manual verification required"
	}

	return control
}

// calculateSummary calculates compliance summary statistics
func (c *ComplianceChecker) calculateSummary(controls []Control) ComplianceSummary {
	summary := ComplianceSummary{
		TotalControls: len(controls),
	}

	for _, control := range controls {
		switch control.Status {
		case StatusCompliant:
			summary.CompliantControls++
		case StatusPartial:
			summary.PartialControls++
		case StatusNonCompliant:
			summary.NonCompliantControls++
		case StatusNotApplicable:
			summary.NotApplicable++
		}
	}

	// Calculate compliance score
	// Compliant = 100%, Partial = 50%, Non-compliant = 0%
	applicableControls := summary.TotalControls - summary.NotApplicable
	if applicableControls > 0 {
		score := float64(summary.CompliantControls)*100 + float64(summary.PartialControls)*50
		summary.ComplianceScore = score / float64(applicableControls)
	}

	return summary
}

// GetControlCount returns the number of controls for a framework
func (c *ComplianceChecker) GetControlCount(framework Framework) int {
	if controls, exists := c.controls[framework]; exists {
		return len(controls)
	}
	return 0
}
