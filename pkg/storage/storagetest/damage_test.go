package storagetest

import (
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// A fault injector nobody has watched fail is the artefact most likely to be
// lying. These tests do two jobs: they prove the injector damages the record
// it names, and they prove it REFUSES rather than damaging the wrong byte.

func mmapStore(t *testing.T, dir string) *storage.GraphStorage {
	t.Helper()
	cfg := storage.DefaultStorageConfig(dir)
	cfg.UseMmapSnapshot = true
	gs, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("open store in %s: %v", dir, err)
	}
	return gs
}

// TestDamageTakesEffect is the end-to-end proof. The damaged record must reach
// the reopened store as ErrRecordUnreadable, and its undamaged neighbour must
// still read — without the second half, a reopen that failed entirely would
// look the same.
func TestDamageTakesEffect(t *testing.T) {
	dir := t.TempDir()
	gs := mmapStore(t, dir)

	target, err := gs.CreateNodeWithTenant("owner", []string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	survivor, err := gs.CreateNodeWithTenant("owner", []string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create survivor: %v", err)
	}
	edge, err := gs.CreateEdgeWithTenant("owner", target.ID, survivor.ID, "LINKS", nil, 1)
	if err != nil {
		t.Fatalf("create edge: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	DamageNodeRecord(t, dir, target.ID)
	DamageEdgeRecord(t, dir, edge.ID)

	gs2 := mmapStore(t, dir)
	defer func() { _ = gs2.Close() }()

	if _, err := gs2.GetNodeForTenant(target.ID, "owner"); !errors.Is(err, storage.ErrRecordUnreadable) {
		t.Errorf("the damaged node must read as unreadable, got %v", err)
	}
	if _, err := gs2.GetEdgeForTenant(edge.ID, "owner"); !errors.Is(err, storage.ErrRecordUnreadable) {
		t.Errorf("the damaged edge must read as unreadable, got %v", err)
	}
	if _, err := gs2.GetNodeForTenant(survivor.ID, "owner"); err != nil {
		t.Errorf("the undamaged neighbour must still read: %v", err)
	}
}

// TestDamageRefusesTheWrongOffset is the control on the check that matters
// most. It redirects the directory entry for one node at ANOTHER node's
// record, which is exactly what a stale format constant would do, and asserts
// that the injector refuses instead of damaging the wrong record.
func TestDamageRefusesTheWrongOffset(t *testing.T) {
	dir := t.TempDir()
	gs := mmapStore(t, dir)

	first, err := gs.CreateNodeWithTenant("owner", []string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create first: %v", err)
	}
	second, err := gs.CreateNodeWithTenant("owner", []string{"Thing"}, nil)
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := SnapshotPath(dir)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	minID := binary.LittleEndian.Uint64(raw[hMinNodeID:])
	dir0 := binary.LittleEndian.Uint64(raw[hNodeDir:])
	secondOff := binary.LittleEndian.Uint64(raw[dir0+(second.ID-minID)*8:])
	// Point the FIRST node's directory slot at the SECOND node's record.
	binary.LittleEndian.PutUint64(raw[dir0+(first.ID-minID)*8:], secondOff)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err = damageRecord(dir, first.ID, nodeKind)
	if err == nil {
		t.Fatal("the injector followed a directory entry to the wrong record and damaged it anyway")
	}
	if !strings.Contains(err.Error(), "the offset is wrong") {
		t.Errorf("want the wrong-offset refusal, got %v", err)
	}
}

// TestDamageRefusesUnknownFiles covers the remaining ways the injector can be
// pointed at bytes it does not understand. Each must be an error, never a
// silent write.
func TestDamageRefusesUnknownFiles(t *testing.T) {
	t.Run("no snapshot", func(t *testing.T) {
		if err := damageRecord(t.TempDir(), 1, nodeKind); err == nil {
			t.Fatal("an empty directory holds no snapshot, so the injector must refuse")
		}
	})

	t.Run("id outside the range", func(t *testing.T) {
		dir := t.TempDir()
		gs := mmapStore(t, dir)
		n, err := gs.CreateNodeWithTenant("owner", []string{"Thing"}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := gs.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		err = damageRecord(dir, n.ID+9999, nodeKind)
		if err == nil {
			t.Fatal("an ID no record carries must be refused")
		}
		if !strings.Contains(err.Error(), "outside the snapshot's ID range") {
			t.Errorf("want the range refusal, got %v", err)
		}
	})

	t.Run("unknown format version", func(t *testing.T) {
		dir := t.TempDir()
		gs := mmapStore(t, dir)
		n, err := gs.CreateNodeWithTenant("owner", []string{"Thing"}, nil)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := gs.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		path := SnapshotPath(dir)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		binary.LittleEndian.PutUint32(raw[hVersion:], wantVersion+1)
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		err = damageRecord(dir, n.ID, nodeKind)
		if err == nil {
			t.Fatal("a snapshot version this package does not know must be refused")
		}
		if !strings.Contains(err.Error(), "format version") {
			t.Errorf("want the version refusal, got %v", err)
		}
	})
}
