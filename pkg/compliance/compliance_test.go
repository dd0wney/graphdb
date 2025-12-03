package compliance

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNewComplianceChecker(t *testing.T) {
	info := SystemInfo{
		EncryptionEnabled:     true,
		TLSEnabled:            true,
		AuditLoggingEnabled:   true,
		DataMaskingEnabled:    true,
		KeyRotationEnabled:    true,
		AuthenticationEnabled: true,
		AccessControlEnabled:  true,
	}

	checker := NewComplianceChecker(info)
	if checker == nil {
		t.Fatal("NewComplianceChecker() returned nil")
	}

	if checker.systemInfo.EncryptionEnabled != true {
		t.Error("SystemInfo not properly set")
	}

	// Verify all frameworks have controls initialized
	frameworks := GetFrameworks()
	for _, framework := range frameworks {
		count := checker.GetControlCount(framework)
		if count == 0 {
			t.Errorf("Framework %s has no controls initialized", framework)
		}
	}
}

func TestGetFrameworks(t *testing.T) {
	frameworks := GetFrameworks()

	expectedFrameworks := []Framework{
		FrameworkGDPR,
		FrameworkSOC2,
		FrameworkHIPAA,
		FrameworkPCIDSS,
		FrameworkFIPS1402,
		FrameworkISO27001,
	}

	if len(frameworks) != len(expectedFrameworks) {
		t.Errorf("GetFrameworks() returned %d frameworks, want %d", len(frameworks), len(expectedFrameworks))
	}

	for _, expected := range expectedFrameworks {
		found := false
		for _, actual := range frameworks {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Framework %s not found in GetFrameworks()", expected)
		}
	}
}

func TestGetControlCount(t *testing.T) {
	info := SystemInfo{}
	checker := NewComplianceChecker(info)

	tests := []struct {
		framework Framework
		minCount  int
	}{
		{FrameworkGDPR, 3},
		{FrameworkSOC2, 3},
		{FrameworkHIPAA, 3},
		{FrameworkPCIDSS, 3},
		{FrameworkFIPS1402, 2},
		{FrameworkISO27001, 3},
	}

	for _, tt := range tests {
		count := checker.GetControlCount(tt.framework)
		if count < tt.minCount {
			t.Errorf("GetControlCount(%s) = %d, want at least %d", tt.framework, count, tt.minCount)
		}
	}
}

func TestCheckCompliance_AllFrameworks(t *testing.T) {
	info := SystemInfo{
		EncryptionEnabled:     true,
		TLSEnabled:            true,
		AuditLoggingEnabled:   true,
		DataMaskingEnabled:    true,
		KeyRotationEnabled:    true,
		AuthenticationEnabled: true,
		AccessControlEnabled:  true,
	}

	checker := NewComplianceChecker(info)
	frameworks := GetFrameworks()

	for _, framework := range frameworks {
		t.Run(string(framework), func(t *testing.T) {
			report, err := checker.CheckCompliance(framework)
			if err != nil {
				t.Fatalf("CheckCompliance(%s) failed: %v", framework, err)
			}

			if report == nil {
				t.Fatal("CheckCompliance() returned nil report")
			}

			if report.Framework != framework {
				t.Errorf("Report framework = %s, want %s", report.Framework, framework)
			}

			if len(report.Controls) == 0 {
				t.Error("Report has no controls")
			}

			if report.Summary.TotalControls != len(report.Controls) {
				t.Errorf("Summary TotalControls = %d, want %d", report.Summary.TotalControls, len(report.Controls))
			}

			if report.Organization != "GraphDB" {
				t.Errorf("Organization = %s, want GraphDB", report.Organization)
			}

			if report.Version == "" {
				t.Error("Report version is empty")
			}
		})
	}
}

func TestCheckCompliance_UnsupportedFramework(t *testing.T) {
	info := SystemInfo{}
	checker := NewComplianceChecker(info)

	_, err := checker.CheckCompliance(Framework("INVALID"))
	if err == nil {
		t.Error("CheckCompliance() should fail for unsupported framework")
	}
}

func TestEvaluateControl_Encryption(t *testing.T) {
	tests := []struct {
		name              string
		encryptionEnabled bool
		controlID         string
		expectedStatus    ComplianceStatus
	}{
		{
			name:              "Encryption enabled",
			encryptionEnabled: true,
			controlID:         "TEST-ENCRYPT",
			expectedStatus:    StatusCompliant,
		},
		{
			name:              "Encryption disabled",
			encryptionEnabled: false,
			controlID:         "TEST-ENCRYPT",
			expectedStatus:    StatusNonCompliant,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := SystemInfo{
				EncryptionEnabled: tt.encryptionEnabled,
			}
			checker := NewComplianceChecker(info)

			control := Control{
				ID:          tt.controlID,
				Framework:   FrameworkGDPR,
				Title:       "Test Encryption Control",
				Description: "Test description",
			}

			evaluated := checker.evaluateControl(control)

			if evaluated.Status != tt.expectedStatus {
				t.Errorf("Status = %s, want %s", evaluated.Status, tt.expectedStatus)
			}

			if evaluated.LastChecked.IsZero() {
				t.Error("LastChecked not set")
			}
		})
	}
}

func TestEvaluateControl_TLS(t *testing.T) {
	info := SystemInfo{
		TLSEnabled: true,
	}
	checker := NewComplianceChecker(info)

	control := Control{
		ID:          "TEST-TLS",
		Framework:   FrameworkSOC2,
		Title:       "TLS Control",
		Description: "Test TLS",
	}

	evaluated := checker.evaluateControl(control)

	if evaluated.Status != StatusCompliant {
		t.Errorf("Status = %s, want %s", evaluated.Status, StatusCompliant)
	}

	if len(evaluated.Evidence) == 0 {
		t.Error("No evidence added for compliant control")
	}
}

func TestEvaluateControl_AuditLogging(t *testing.T) {
	info := SystemInfo{
		AuditLoggingEnabled: true,
	}
	checker := NewComplianceChecker(info)

	control := Control{
		ID:          "TEST-AUDIT-LOG",
		Framework:   FrameworkHIPAA,
		Title:       "Audit Logging Control",
		Description: "Test audit logging",
	}

	evaluated := checker.evaluateControl(control)

	if evaluated.Status != StatusCompliant {
		t.Errorf("Status = %s, want %s", evaluated.Status, StatusCompliant)
	}
}

func TestEvaluateControl_AccessControl(t *testing.T) {
	info := SystemInfo{
		AuthenticationEnabled: true,
		AccessControlEnabled:  true,
	}
	checker := NewComplianceChecker(info)

	control := Control{
		ID:          "TEST-ACCESS",
		Framework:   FrameworkPCIDSS,
		Title:       "Access Control",
		Description: "Test access control",
	}

	evaluated := checker.evaluateControl(control)

	if evaluated.Status != StatusCompliant {
		t.Errorf("Status = %s, want %s", evaluated.Status, StatusCompliant)
	}
}

func TestCalculateSummary(t *testing.T) {
	checker := NewComplianceChecker(SystemInfo{})

	controls := []Control{
		{Status: StatusCompliant},
		{Status: StatusCompliant},
		{Status: StatusPartial},
		{Status: StatusNonCompliant},
		{Status: StatusNotApplicable},
	}

	summary := checker.calculateSummary(controls)

	if summary.TotalControls != 5 {
		t.Errorf("TotalControls = %d, want 5", summary.TotalControls)
	}

	if summary.CompliantControls != 2 {
		t.Errorf("CompliantControls = %d, want 2", summary.CompliantControls)
	}

	if summary.PartialControls != 1 {
		t.Errorf("PartialControls = %d, want 1", summary.PartialControls)
	}

	if summary.NonCompliantControls != 1 {
		t.Errorf("NonCompliantControls = %d, want 1", summary.NonCompliantControls)
	}

	if summary.NotApplicable != 1 {
		t.Errorf("NotApplicable = %d, want 1", summary.NotApplicable)
	}

	// Score calculation: (2*100 + 1*50) / 4 applicable = 62.5%
	expectedScore := 62.5
	if summary.ComplianceScore != expectedScore {
		t.Errorf("ComplianceScore = %.1f, want %.1f", summary.ComplianceScore, expectedScore)
	}
}

func TestCalculateSummary_AllCompliant(t *testing.T) {
	checker := NewComplianceChecker(SystemInfo{})

	controls := []Control{
		{Status: StatusCompliant},
		{Status: StatusCompliant},
		{Status: StatusCompliant},
	}

	summary := checker.calculateSummary(controls)

	if summary.ComplianceScore != 100.0 {
		t.Errorf("ComplianceScore = %.1f, want 100.0", summary.ComplianceScore)
	}
}

func TestCalculateSummary_NoApplicableControls(t *testing.T) {
	checker := NewComplianceChecker(SystemInfo{})

	controls := []Control{
		{Status: StatusNotApplicable},
		{Status: StatusNotApplicable},
	}

	summary := checker.calculateSummary(controls)

	if summary.ComplianceScore != 0.0 {
		t.Errorf("ComplianceScore = %.1f, want 0.0", summary.ComplianceScore)
	}
}

func TestExportReport_JSON(t *testing.T) {
	info := SystemInfo{
		EncryptionEnabled: true,
		TLSEnabled:        true,
	}
	checker := NewComplianceChecker(info)

	report, err := checker.CheckCompliance(FrameworkGDPR)
	if err != nil {
		t.Fatalf("CheckCompliance() failed: %v", err)
	}

	var buf bytes.Buffer
	err = checker.ExportReport(report, "json", &buf)
	if err != nil {
		t.Fatalf("ExportReport() failed: %v", err)
	}

	// Verify it's valid JSON
	var decoded ComplianceReport
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Errorf("Invalid JSON output: %v", err)
	}

	if decoded.Framework != FrameworkGDPR {
		t.Errorf("Decoded framework = %s, want %s", decoded.Framework, FrameworkGDPR)
	}
}

func TestExportReport_Text(t *testing.T) {
	info := SystemInfo{
		EncryptionEnabled: true,
	}
	checker := NewComplianceChecker(info)

	report, err := checker.CheckCompliance(FrameworkSOC2)
	if err != nil {
		t.Fatalf("CheckCompliance() failed: %v", err)
	}

	var buf bytes.Buffer
	err = checker.ExportReport(report, "text", &buf)
	if err != nil {
		t.Fatalf("ExportReport() failed: %v", err)
	}

	output := buf.String()

	// Verify essential content
	if !strings.Contains(output, "Compliance Report: SOC2") {
		t.Error("Text output missing framework name")
	}

	if !strings.Contains(output, "Total Controls:") {
		t.Error("Text output missing total controls")
	}

	if !strings.Contains(output, "Compliance Score:") {
		t.Error("Text output missing compliance score")
	}
}

func TestExportReport_Markdown(t *testing.T) {
	info := SystemInfo{
		AuditLoggingEnabled: true,
	}
	checker := NewComplianceChecker(info)

	report, err := checker.CheckCompliance(FrameworkHIPAA)
	if err != nil {
		t.Fatalf("CheckCompliance() failed: %v", err)
	}

	var buf bytes.Buffer
	err = checker.ExportReport(report, "markdown", &buf)
	if err != nil {
		t.Fatalf("ExportReport() failed: %v", err)
	}

	output := buf.String()

	// Verify markdown formatting
	if !strings.Contains(output, "# HIPAA Compliance Report") {
		t.Error("Markdown output missing header")
	}

	if !strings.Contains(output, "## Summary") {
		t.Error("Markdown output missing summary section")
	}

	if !strings.Contains(output, "| Metric | Value |") {
		t.Error("Markdown output missing summary table")
	}

	if !strings.Contains(output, "## Controls") {
		t.Error("Markdown output missing controls section")
	}
}

func TestExportReport_UnsupportedFormat(t *testing.T) {
	info := SystemInfo{}
	checker := NewComplianceChecker(info)

	report, err := checker.CheckCompliance(FrameworkGDPR)
	if err != nil {
		t.Fatalf("CheckCompliance() failed: %v", err)
	}

	var buf bytes.Buffer
	err = checker.ExportReport(report, "invalid", &buf)
	if err == nil {
		t.Error("ExportReport() should fail for unsupported format")
	}
}

func TestGDPRControls(t *testing.T) {
	info := SystemInfo{}
	checker := NewComplianceChecker(info)

	count := checker.GetControlCount(FrameworkGDPR)
	if count != 5 {
		t.Errorf("GDPR control count = %d, want 5", count)
	}

	// Verify specific controls exist
	report, _ := checker.CheckCompliance(FrameworkGDPR)
	controlIDs := make(map[string]bool)
	for _, control := range report.Controls {
		controlIDs[control.ID] = true
	}

	expectedControls := []string{
		"GDPR-ART32-ENCRYPT",
		"GDPR-ART32-PSEUDONYMIZE",
		"GDPR-ART30-LOGS",
		"GDPR-ART32-ACCESS",
		"GDPR-ART25-PRIVACY",
	}

	for _, id := range expectedControls {
		if !controlIDs[id] {
			t.Errorf("GDPR control %s not found", id)
		}
	}
}

func TestSOC2Controls(t *testing.T) {
	info := SystemInfo{}
	checker := NewComplianceChecker(info)

	count := checker.GetControlCount(FrameworkSOC2)
	if count != 5 {
		t.Errorf("SOC2 control count = %d, want 5", count)
	}
}

func TestHIPAAControls(t *testing.T) {
	info := SystemInfo{}
	checker := NewComplianceChecker(info)

	count := checker.GetControlCount(FrameworkHIPAA)
	if count != 5 {
		t.Errorf("HIPAA control count = %d, want 5", count)
	}
}

func TestPCIDSSControls(t *testing.T) {
	info := SystemInfo{}
	checker := NewComplianceChecker(info)

	count := checker.GetControlCount(FrameworkPCIDSS)
	if count != 6 {
		t.Errorf("PCI-DSS control count = %d, want 6", count)
	}
}

func TestFIPS1402Controls(t *testing.T) {
	info := SystemInfo{}
	checker := NewComplianceChecker(info)

	count := checker.GetControlCount(FrameworkFIPS1402)
	if count != 3 {
		t.Errorf("FIPS 140-2 control count = %d, want 3", count)
	}
}

func TestISO27001Controls(t *testing.T) {
	info := SystemInfo{}
	checker := NewComplianceChecker(info)

	count := checker.GetControlCount(FrameworkISO27001)
	if count != 5 {
		t.Errorf("ISO 27001 control count = %d, want 5", count)
	}
}

func TestComplianceScoreFormula(t *testing.T) {
	checker := NewComplianceChecker(SystemInfo{})

	tests := []struct {
		name          string
		controls      []Control
		expectedScore float64
	}{
		{
			name: "All compliant",
			controls: []Control{
				{Status: StatusCompliant},
				{Status: StatusCompliant},
			},
			expectedScore: 100.0,
		},
		{
			name: "All partial",
			controls: []Control{
				{Status: StatusPartial},
				{Status: StatusPartial},
			},
			expectedScore: 50.0,
		},
		{
			name: "All non-compliant",
			controls: []Control{
				{Status: StatusNonCompliant},
				{Status: StatusNonCompliant},
			},
			expectedScore: 0.0,
		},
		{
			name: "Mixed",
			controls: []Control{
				{Status: StatusCompliant},  // 100
				{Status: StatusPartial},    // 50
				{Status: StatusNonCompliant}, // 0
			},
			expectedScore: 50.0, // (100 + 50 + 0) / 3
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := checker.calculateSummary(tt.controls)
			if summary.ComplianceScore != tt.expectedScore {
				t.Errorf("ComplianceScore = %.1f, want %.1f", summary.ComplianceScore, tt.expectedScore)
			}
		})
	}
}

func TestControlEvidence(t *testing.T) {
	info := SystemInfo{
		EncryptionEnabled: true,
	}
	checker := NewComplianceChecker(info)

	control := Control{
		ID:          "TEST-ENCRYPT",
		Framework:   FrameworkGDPR,
		Title:       "Encryption Control",
		Description: "Test encryption",
	}

	evaluated := checker.evaluateControl(control)

	if len(evaluated.Evidence) == 0 {
		t.Error("Expected evidence to be added for compliant control")
	}

	evidence := evaluated.Evidence[0]
	if evidence.Type == "" {
		t.Error("Evidence type is empty")
	}

	if evidence.Description == "" {
		t.Error("Evidence description is empty")
	}

	if evidence.Source == "" {
		t.Error("Evidence source is empty")
	}

	if evidence.Timestamp.IsZero() {
		t.Error("Evidence timestamp not set")
	}
}

func TestGetStatusEmoji(t *testing.T) {
	tests := []struct {
		status ComplianceStatus
		want   string
	}{
		{StatusCompliant, "✅"},
		{StatusPartial, "⚠️"},
		{StatusNonCompliant, "❌"},
		{StatusNotApplicable, "➖"},
		{ComplianceStatus("unknown"), "❓"},
	}

	for _, tt := range tests {
		got := getStatusEmoji(tt.status)
		if got != tt.want {
			t.Errorf("getStatusEmoji(%s) = %s, want %s", tt.status, got, tt.want)
		}
	}
}

func TestContainsHelper(t *testing.T) {
	tests := []struct {
		name    string
		s       string
		substrs []string
		want    bool
	}{
		{
			name:    "Single match",
			s:       "ENCRYPT",
			substrs: []string{"ENCRYPT"},
			want:    true,
		},
		{
			name:    "Multiple substrs, one matches",
			s:       "TEST-ENCRYPT",
			substrs: []string{"CRYPTO", "ENCRYPT"},
			want:    true,
		},
		{
			name:    "No match",
			s:       "ACCESS",
			substrs: []string{"ENCRYPT", "CRYPTO"},
			want:    false,
		},
		{
			name:    "Partial match",
			s:       "ENCRYPTION-KEY",
			substrs: []string{"ENCRYPT"},
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contains(tt.s, tt.substrs...)
			if got != tt.want {
				t.Errorf("contains(%q, %v) = %v, want %v", tt.s, tt.substrs, got, tt.want)
			}
		})
	}
}
