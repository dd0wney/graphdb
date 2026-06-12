package compliance

import (
	"strings"
	"testing"
)

// TestGDPRRightToErasureControl pins the GDPR Article 17 (Right to Erasure)
// control's posture after security audit M-1 Option A shipped (#396): tenant
// deletion purges the WAL synchronously (CompactWAL/TruncateUpTo on the
// delete-tenant path), so the control is Compliant as a structural property
// of the codebase — no SystemInfo flag gates it. Node/edge-level deletes are
// erased from the in-memory graph and the next snapshot immediately but stay
// in the WAL until the next compaction; the control's Notes must keep
// documenting that window honestly rather than claiming blanket instant
// erasure.
func TestGDPRRightToErasureControl(t *testing.T) {
	checker := NewComplianceChecker(SystemInfo{})
	report, err := checker.CheckCompliance(FrameworkGDPR)
	if err != nil {
		t.Fatalf("CheckCompliance(GDPR): %v", err)
	}

	var ctrl *Control
	for i := range report.Controls {
		if report.Controls[i].ID == "GDPR-ART17-ERASURE" {
			ctrl = &report.Controls[i]
			break
		}
	}
	if ctrl == nil {
		t.Fatal("GDPR framework is missing the Article 17 Right to Erasure control")
	}

	if ctrl.Status != StatusCompliant {
		t.Errorf("erasure status = %s, want %s (tenant-delete WAL purge is synchronous since M-1 Option A)", ctrl.Status, StatusCompliant)
	}

	if len(ctrl.Evidence) == 0 {
		t.Fatal("erasure control must carry evidence for the synchronous WAL purge")
	}
	evidence := strings.ToLower(ctrl.Evidence[0].Description + " " + ctrl.Evidence[0].Source)
	if !strings.Contains(evidence, "wal") {
		t.Errorf("erasure evidence must cite the WAL purge mechanism; got %q", evidence)
	}

	// Honesty guard: node/edge-level deletes still have a bounded WAL
	// remanence window (until the next compaction). The Notes must say so.
	notes := strings.ToLower(ctrl.Notes)
	if !strings.Contains(notes, "wal") || !strings.Contains(notes, "compaction") {
		t.Errorf("erasure Notes must document the node/edge-level WAL window until compaction; got %q", ctrl.Notes)
	}
}
