package compliance

import (
	"fmt"
	"os"
)

// Example demonstrates how to use the compliance checker
func ExampleComplianceChecker() {
	// Create system info with current security configuration
	info := SystemInfo{
		EncryptionEnabled:     true,
		TLSEnabled:            true,
		AuditLoggingEnabled:   true,
		DataMaskingEnabled:    true,
		KeyRotationEnabled:    true,
		AuthenticationEnabled: true,
		AccessControlEnabled:  true,
	}

	// Create compliance checker
	checker := NewComplianceChecker(info)

	// Check GDPR compliance
	report, err := checker.CheckCompliance(FrameworkGDPR)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	// Print summary
	fmt.Printf("Framework: %s\n", report.Framework)
	fmt.Printf("Total Controls: %d\n", report.Summary.TotalControls)
	fmt.Printf("Compliant: %d\n", report.Summary.CompliantControls)
	fmt.Printf("Compliance Score: %.1f%%\n", report.Summary.ComplianceScore)

	// Export to different formats
	checker.ExportReport(report, "json", os.Stdout)
	checker.ExportReport(report, "text", os.Stdout)
	checker.ExportReport(report, "markdown", os.Stdout)
}

// Example demonstrates checking multiple frameworks
func ExampleComplianceChecker_multiFramework() {
	info := SystemInfo{
		EncryptionEnabled:     true,
		TLSEnabled:            true,
		AuditLoggingEnabled:   true,
		DataMaskingEnabled:    false, // Not all features enabled
		KeyRotationEnabled:    true,
		AuthenticationEnabled: true,
		AccessControlEnabled:  true,
	}

	checker := NewComplianceChecker(info)

	// Check all frameworks
	frameworks := []Framework{
		FrameworkGDPR,
		FrameworkSOC2,
		FrameworkHIPAA,
		FrameworkPCIDSS,
	}

	for _, framework := range frameworks {
		report, err := checker.CheckCompliance(framework)
		if err != nil {
			continue
		}

		fmt.Printf("\n%s Compliance: %.1f%%\n", framework, report.Summary.ComplianceScore)
	}
}

// Example demonstrates control evaluation
func ExampleComplianceChecker_controlEvaluation() {
	info := SystemInfo{
		EncryptionEnabled:     true,
		TLSEnabled:            false, // TLS not enabled
		AuditLoggingEnabled:   true,
		DataMaskingEnabled:    true,
		KeyRotationEnabled:    true,
		AuthenticationEnabled: true,
		AccessControlEnabled:  true,
	}

	checker := NewComplianceChecker(info)
	report, _ := checker.CheckCompliance(FrameworkSOC2)

	// Show control statuses
	for _, control := range report.Controls {
		fmt.Printf("%s: %s\n", control.ID, control.Status)
		if len(control.Evidence) > 0 {
			fmt.Printf("  Evidence: %s\n", control.Evidence[0].Description)
		}
		if control.Notes != "" {
			fmt.Printf("  Notes: %s\n", control.Notes)
		}
	}
}
