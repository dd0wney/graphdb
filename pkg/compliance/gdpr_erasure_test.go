package compliance

import (
	"strings"
	"testing"
)

// TestGDPRRightToErasureControl pins security audit M-1 (interim / Option C):
// the GDPR framework must carry an Article 17 (Right to Erasure) control, and
// it must HONESTLY report the WAL remanence window — erasure is immediate for
// the in-memory graph and the next snapshot, but the write-ahead log retains
// the data until compaction. Status is therefore Partial (not Compliant) until
// the WAL-purge fix (Option A, TruncateUpTo) ships and sets ImmediateErasure.
//
// RED against pre-fix: there is no Article 17 control at all.
func TestGDPRRightToErasureControl(t *testing.T) {
	findErasure := func(info SystemInfo) *Control {
		checker := NewComplianceChecker(info)
		report, err := checker.CheckCompliance(FrameworkGDPR)
		if err != nil {
			t.Fatalf("CheckCompliance(GDPR): %v", err)
		}
		for i := range report.Controls {
			if report.Controls[i].ID == "GDPR-ART17-ERASURE" {
				return &report.Controls[i]
			}
		}
		return nil
	}

	// Default posture: WAL purge not yet implemented → Partial + honest note.
	ctrl := findErasure(SystemInfo{})
	if ctrl == nil {
		t.Fatal("GDPR framework is missing the Article 17 Right to Erasure control")
	}
	if ctrl.Status != StatusPartial {
		t.Errorf("erasure status = %s, want %s (WAL remanence window open)", ctrl.Status, StatusPartial)
	}
	if !strings.Contains(strings.ToLower(ctrl.Notes), "wal") {
		t.Errorf("erasure Notes must document the WAL remanence window; got %q", ctrl.Notes)
	}

	// Forward-looking: when the WAL-purge fix (Option A) ships and sets
	// ImmediateErasure, the same control flips to Compliant with no other change.
	if c := findErasure(SystemInfo{ImmediateErasure: true}); c == nil || c.Status != StatusCompliant {
		got := "<nil>"
		if c != nil {
			got = string(c.Status)
		}
		t.Errorf("with ImmediateErasure=true, erasure status = %s, want %s", got, StatusCompliant)
	}
}
