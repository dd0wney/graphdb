package storage

// Coord task graphdb:v1.4-wal-backend-switch-policy.
//
// The task was seeded on the premise that #619's guard already refuses a WAL
// backend switch, and that only a repair path was missing. A probe showed the
// premise was wrong: the switch SILENTLY DROPPED two acknowledged writes.
//
// Why the older guard misses it. The plain and batched backends write wal.log;
// the compressed backend writes wal_compressed.log, both in <dataDir>/wal.
// Flipping StorageConfig.EnableCompression reads a different file rather than
// the old one. On a first switch that file does not exist, so the recovered
// LSN is 0, and guardWALNotBehindSnapshot returns nil on a zero
// (compact_wal.go). The previous backend's file then sits on disk holding
// writes no snapshot covers, and replay never sees them.
//
// guardWALBackendNotSwitched closes the hole by refusing when the other
// backend's file still holds bytes. A clean Close leaves that file empty,
// because Truncate replaces it, so an ordinary switch still opens.

import (
	"errors"
	"os"
	"testing"
)

// switchDirections is both ways round. A fix that only looked for
// wal_compressed.log would pass the plain-to-compressed row alone.
var switchDirections = []struct {
	name             string
	fromCompression  bool
	toCompression    bool
	foreignFileIsFor string
}{
	{"plain to compressed", false, true, "wal.log"},
	{"compressed to plain", true, false, "wal_compressed.log"},
}

func backendSwitchConfig(dir string, compression bool) StorageConfig {
	c := jsonConfig(dir)
	c.EnableCompression = compression
	return c
}

// TestWALBackendSwitch_RefusesWhenTheOtherBackendHoldsWrites is the red test
// for the defect. Before guardWALBackendNotSwitched it failed with:
//
//	SILENT DATA LOSS: the open succeeded and dropped 2 acknowledged write(s):
//	node IDs [2 3]. .../wal/wal.log still holds them; the compressed backend
//	read .../wal/wal_compressed.log, which did not exist, so its recovered
//	LSN was 0 and guardWALNotBehindSnapshot returned nil.
//
// Sequence: create A, snapshot so the boundary covers it, then create B and C
// which live only in the WAL. Abandon the store without Close, which is what a
// crash leaves. Reopen with EnableCompression flipped.
func TestWALBackendSwitch_RefusesWhenTheOtherBackendHoldsWrites(t *testing.T) {
	for _, d := range switchDirections {
		t.Run(d.name, func(t *testing.T) {
			dir := t.TempDir()
			before := backendSwitchConfig(dir, d.fromCompression)

			gs, err := NewGraphStorageWithConfig(before)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if _, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"},
				map[string]Value{"n": StringValue("A")}); err != nil {
				t.Fatalf("create A: %v", err)
			}
			if err := gs.Snapshot(); err != nil {
				t.Fatalf("snapshot: %v", err)
			}
			for _, name := range []string{"B", "C"} {
				if _, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"},
					map[string]Value{"n": StringValue(name)}); err != nil {
					t.Fatalf("create %s: %v", name, err)
				}
			}

			// Positive control: the old backend's file must really hold the
			// two writes, or this row proves nothing about the switch.
			oldWAL := walFilePath(dir, before)
			info, statErr := os.Stat(oldWAL)
			if statErr != nil {
				t.Fatalf("stat %s: %v", oldWAL, statErr)
			}
			if info.Size() == 0 {
				t.Fatalf("premise broken: %s is empty after two un-snapshotted writes", oldWAL)
			}

			// No Close: a crash leaves the store exactly here.

			after := backendSwitchConfig(dir, d.toCompression)
			gs2, err := NewGraphStorageWithConfig(after)
			if err == nil {
				_ = gs2.Close()
				t.Fatalf("the open SUCCEEDED after a backend switch; %s still holds %d bytes "+
					"of un-snapshotted writes", oldWAL, info.Size())
			}
			if !errors.Is(err, ErrWALBackendSwitched) {
				t.Fatalf("open error does not satisfy errors.Is(err, ErrWALBackendSwitched): %v", err)
			}
			// The operator has to find the file, so the message must name it.
			if got := err.Error(); !containsAll(got, oldWAL, "EnableCompression") {
				t.Errorf("the refusal must name the foreign file and the setting; got: %s", got)
			}
		})
	}
}

// TestWALBackendSwitch_OpensAfterACleanClose is the other half of the
// contract, and the one that keeps the guard from being a nuisance. Close
// snapshots and truncates, which replaces the old backend's file with an empty
// one, so the switch is safe and must NOT refuse.
//
// Without this row the guard could refuse every switch forever and still look
// correct.
func TestWALBackendSwitch_OpensAfterACleanClose(t *testing.T) {
	for _, d := range switchDirections {
		t.Run(d.name, func(t *testing.T) {
			dir := t.TempDir()
			before := backendSwitchConfig(dir, d.fromCompression)

			gs, err := NewGraphStorageWithConfig(before)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			a, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"},
				map[string]Value{"n": StringValue("A")})
			if err != nil {
				t.Fatalf("create A: %v", err)
			}
			if err := gs.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			after := backendSwitchConfig(dir, d.toCompression)
			gs2, err := NewGraphStorageWithConfig(after)
			if err != nil {
				t.Fatalf("a switch after a clean Close must open, got: %v", err)
			}
			defer func() { _ = gs2.Close() }()

			if _, err := gs2.GetNodeForTenant(a.ID, rtTenantA); err != nil {
				t.Errorf("node A missing after a clean switch: %v", err)
			}
			assertGraphInvariants(t, gs2)
		})
	}
}

// TestWALBackendSwitch_SameBackendIsUnaffected proves the guard reads the
// OTHER backend's file and not the active one. A store reopened on the same
// backend with un-snapshotted writes must open and replay them, exactly as
// before this change.
func TestWALBackendSwitch_SameBackendIsUnaffected(t *testing.T) {
	for _, compression := range []bool{false, true} {
		name := "plain"
		if compression {
			name = "compressed"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			cfg := backendSwitchConfig(dir, compression)

			gs, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			b, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"},
				map[string]Value{"n": StringValue("B")})
			if err != nil {
				t.Fatalf("create B: %v", err)
			}

			// No Close, so B is in the WAL only.
			gs2, err := NewGraphStorageWithConfig(cfg)
			if err != nil {
				t.Fatalf("reopen on the same backend must not refuse, got: %v", err)
			}
			defer func() { _ = gs2.Close() }()

			if _, err := gs2.GetNodeForTenant(b.ID, rtTenantA); err != nil {
				t.Errorf("node B was not replayed on a same-backend reopen: %v", err)
			}
		})
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
