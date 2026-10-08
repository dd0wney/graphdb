package storage

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// The mmap snapshot stores property counts, key lengths, label counts, label
// lengths, tenant ID lengths, edge type lengths and membership-key lengths as
// uint16. A larger value wrapped with no error, and the record did not decode
// after the next reopen: an acknowledged write was lost. Every write path must
// refuse such a record before it reaches memory, in either snapshot mode.

const tooWide = maxRecordFieldWidth + 1

func widthTestStore(t *testing.T) *GraphStorage {
	t.Helper()
	gs, err := NewGraphStorageWithConfig(DefaultStorageConfig(t.TempDir()))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	return gs
}

func manyProps(n int) map[string]Value {
	props := make(map[string]Value, n)
	for i := 0; i < n; i++ {
		props[fmt.Sprintf("k%d", i)] = IntValue(int64(i))
	}
	return props
}

func manyLabels(n int) []string {
	labels := make([]string, n)
	for i := range labels {
		labels[i] = fmt.Sprintf("L%d", i)
	}
	return labels
}

func assertTooWide(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, ErrRecordTooWide) {
		t.Fatalf("%s: err = %v, want ErrRecordTooWide", what, err)
	}
}

func TestRecordWidth_NodeCreateRefusesOversize(t *testing.T) {
	// The membership key is kind + tenant + 0x00 + label, so the label limit
	// depends on the tenant: "default" leaves 65,535 - 9 bytes for a label.
	membershipLabel := strings.Repeat("L", maxRecordFieldWidth-len("default")-1)

	cases := []struct {
		name   string
		tenant string
		labels []string
		props  map[string]Value
	}{
		{"property key", "", []string{"A"}, map[string]Value{strings.Repeat("k", tooWide): IntValue(1)}},
		{"property count", "", []string{"A"}, manyProps(tooWide)},
		{"label length", "", []string{strings.Repeat("L", tooWide)}, nil},
		{"label in membership key", "", []string{membershipLabel}, nil},
		{"label count", "", manyLabels(tooWide), nil},
		{"tenant ID", strings.Repeat("t", tooWide), []string{"A"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs := widthTestStore(t)
			_, err := gs.CreateNodeWithTenant(tc.tenant, tc.labels, tc.props)
			assertTooWide(t, "CreateNodeWithTenant", err)
			if n := len(gs.GetAllNodesAcrossTenants()); n != 0 {
				t.Fatalf("a refused create left %d nodes", n)
			}
		})
	}
}

func TestRecordWidth_EdgeCreateRefusesOversize(t *testing.T) {
	cases := []struct {
		name     string
		edgeType string
		props    map[string]Value
	}{
		{"edge type", strings.Repeat("T", tooWide), nil},
		{"edge type in membership key", strings.Repeat("T", maxRecordFieldWidth-len("default")-1), nil},
		{"property key", "R", map[string]Value{strings.Repeat("k", tooWide): IntValue(1)}},
		{"property count", "R", manyProps(tooWide)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs := widthTestStore(t)
			a, _ := gs.CreateNode([]string{"A"}, nil)
			b, _ := gs.CreateNode([]string{"B"}, nil)

			_, err := gs.CreateEdge(a.ID, b.ID, tc.edgeType, tc.props, 1)
			assertTooWide(t, "CreateEdge", err)

			batch := gs.BeginBatch()
			_, err = batch.AddEdge(a.ID, b.ID, tc.edgeType, tc.props, 1)
			assertTooWide(t, "Batch.AddEdge", err)

			tx, err := gs.BeginTransaction()
			if err != nil {
				t.Fatalf("BeginTransaction: %v", err)
			}
			if _, err := tx.CreateEdge(a.ID, b.ID, tc.edgeType, tc.props, 1); err != nil {
				assertTooWide(t, "Transaction.CreateEdge", err)
			} else {
				assertTooWide(t, "Transaction.Commit (edge)", tx.Commit())
			}

			out, err := gs.GetOutgoingEdges(a.ID)
			if err != nil || len(out) != 0 {
				t.Fatalf("refused edge creates left edges %v (err %v)", out, err)
			}
		})
	}
}

func TestRecordWidth_UpdateRefusesOversizeAndLeavesTheRecord(t *testing.T) {
	longKey := map[string]Value{strings.Repeat("k", tooWide): IntValue(1)}

	t.Run("PatchNodeForTenant key", func(t *testing.T) {
		gs := widthTestStore(t)
		n, _ := gs.CreateNode([]string{"A"}, map[string]Value{"x": IntValue(1)})
		assertTooWide(t, "PatchNodeForTenant", gs.PatchNodeForTenant(n.ID, longKey, nil, DefaultTenantID))
		assertNodePropCount(t, gs, n.ID, 1)
	})
	t.Run("PatchNodeForTenant count after merge", func(t *testing.T) {
		gs := widthTestStore(t)
		n, _ := gs.CreateNode([]string{"A"}, map[string]Value{"x": IntValue(1)})
		// 65,535 new keys plus the existing one is one too many.
		assertTooWide(t, "PatchNodeForTenant", gs.PatchNodeForTenant(n.ID, manyProps(maxRecordFieldWidth), nil, DefaultTenantID))
		assertNodePropCount(t, gs, n.ID, 1)
	})
	t.Run("UpdateNode", func(t *testing.T) {
		gs := widthTestStore(t)
		n, _ := gs.CreateNode([]string{"A"}, map[string]Value{"x": IntValue(1)})
		assertTooWide(t, "UpdateNode", gs.UpdateNode(n.ID, longKey))
		assertNodePropCount(t, gs, n.ID, 1)
	})
	t.Run("Batch.UpdateNode", func(t *testing.T) {
		gs := widthTestStore(t)
		n, _ := gs.CreateNode([]string{"A"}, map[string]Value{"x": IntValue(1)})
		batch := gs.BeginBatch()
		batch.UpdateNode(n.ID, longKey)
		assertTooWide(t, "Batch.Commit", batch.Commit())
		assertNodePropCount(t, gs, n.ID, 1)
	})
	t.Run("Transaction.UpdateNode", func(t *testing.T) {
		gs := widthTestStore(t)
		n, _ := gs.CreateNode([]string{"A"}, map[string]Value{"x": IntValue(1)})
		tx, err := gs.BeginTransaction()
		if err != nil {
			t.Fatalf("BeginTransaction: %v", err)
		}
		if err := tx.UpdateNode(n.ID, longKey); err != nil {
			assertTooWide(t, "Transaction.UpdateNode", err)
		} else {
			assertTooWide(t, "Transaction.Commit (update)", tx.Commit())
		}
		assertNodePropCount(t, gs, n.ID, 1)
	})
	t.Run("PatchEdgeForTenant and UpsertEdge", func(t *testing.T) {
		gs := widthTestStore(t)
		a, _ := gs.CreateNode([]string{"A"}, nil)
		b, _ := gs.CreateNode([]string{"B"}, nil)
		e, err := gs.CreateEdge(a.ID, b.ID, "R", map[string]Value{"x": IntValue(1)}, 1)
		if err != nil {
			t.Fatalf("CreateEdge: %v", err)
		}
		assertTooWide(t, "PatchEdgeForTenant", gs.PatchEdgeForTenant(e.ID, longKey, nil, nil, DefaultTenantID))
		_, _, err = gs.UpsertEdge(a.ID, b.ID, "R", longKey, 1)
		assertTooWide(t, "UpsertEdge", err)
		got, err := gs.GetEdge(e.ID)
		if err != nil || len(got.Properties) != 1 {
			t.Fatalf("edge after refused updates: %v (err %v), want 1 property", got, err)
		}
	})
}

func TestRecordWidth_BatchAndTransactionNodeCreateRefuseOversize(t *testing.T) {
	gs := widthTestStore(t)
	longKey := map[string]Value{strings.Repeat("k", tooWide): IntValue(1)}

	batch := gs.BeginBatch()
	_, err := batch.AddNode([]string{"A"}, longKey)
	assertTooWide(t, "Batch.AddNode", err)

	tx, err := gs.BeginTransaction()
	if err != nil {
		t.Fatalf("BeginTransaction: %v", err)
	}
	if _, err := tx.CreateNode([]string{"A"}, longKey); err != nil {
		assertTooWide(t, "Transaction.CreateNode", err)
	} else {
		assertTooWide(t, "Transaction.Commit (create)", tx.Commit())
	}
	if n := len(gs.GetAllNodesAcrossTenants()); n != 0 {
		t.Fatalf("refused creates left %d nodes", n)
	}
}

// The limit is inclusive: a record at every maximum must still survive an
// mmap close and reopen, or the check is off by one.
func TestRecordWidth_MaximumRecordRoundTripsThroughMmap(t *testing.T) {
	dir := t.TempDir()
	gs, err := NewGraphStorageWithConfig(DefaultStorageConfig(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	key := strings.Repeat("k", maxRecordFieldWidth)
	label := strings.Repeat("L", maxRecordFieldWidth-len("default")-2)
	n, err := gs.CreateNode([]string{label}, map[string]Value{key: IntValue(7)})
	if err != nil {
		t.Fatalf("create at the maximum widths: %v", err)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	again, err := NewGraphStorageWithConfig(DefaultStorageConfig(dir))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()
	got, err := again.GetNode(n.ID)
	if err != nil {
		t.Fatalf("GetNode after reopen: %v", err)
	}
	if v, err := got.Properties[key].AsInt(); err != nil || v != 7 || len(got.Labels) != 1 || got.Labels[0] != label {
		t.Fatalf("node after reopen: labels %d, value %v (err %v)", len(got.Labels), got.Properties[key], err)
	}
}

// Defence in depth: a record that reaches the encoder anyway (a store written
// by an older binary in JSON mode, then switched to mmap) must fail the
// snapshot write, not wrap.
func TestRecordWidth_MmapEncoderRefusesOversize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.mmap")
	node := &Node{ID: 1, TenantID: "default", Labels: []string{"A"}, Properties: map[string]Value{strings.Repeat("k", tooWide): IntValue(1)}}

	err := writeMmapSnapshotData(path, []*Node{node}, nil, &mmapMetadata{})
	assertTooWide(t, "writeMmapSnapshotData", err)
}

func assertNodePropCount(t *testing.T, gs *GraphStorage, id uint64, want int) {
	t.Helper()
	got, err := gs.GetNode(id)
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if len(got.Properties) != want {
		t.Fatalf("node has %d properties after a refused update, want %d", len(got.Properties), want)
	}
}

// A record stored with tenant "" is keyed under "default" in the membership
// directory, so the key check must use the effective tenant: a label that
// fits beside "" is 7 bytes too long beside "default".
func TestRecordWidth_MmapEncoderUsesEffectiveTenant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot.mmap")
	label := strings.Repeat("L", maxRecordFieldWidth-2)
	node := &Node{ID: 1, TenantID: "", Labels: []string{label}, Properties: map[string]Value{}}

	err := writeMmapSnapshotData(path, []*Node{node}, nil, &mmapMetadata{})
	assertTooWide(t, "writeMmapSnapshotData with tenant \"\"", err)
}

// AddEdge checks at queue time, but the caller keeps the property map and can
// grow it before Commit.
func TestRecordWidth_BatchCommitRechecksAnEdgeGrownAfterAddEdge(t *testing.T) {
	gs := widthTestStore(t)
	a, _ := gs.CreateNode([]string{"A"}, nil)
	b, _ := gs.CreateNode([]string{"B"}, nil)

	props := map[string]Value{"x": IntValue(1)}
	batch := gs.BeginBatch()
	if _, err := batch.AddEdge(a.ID, b.ID, "R", props, 1); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	props[strings.Repeat("k", tooWide)] = IntValue(2)

	assertTooWide(t, "Batch.Commit", batch.Commit())
	if out, _ := gs.GetOutgoingEdges(a.ID); len(out) != 0 {
		t.Fatalf("the grown edge was stored: %v", out)
	}
}
