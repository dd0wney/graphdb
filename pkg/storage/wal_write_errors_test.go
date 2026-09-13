package storage

// The contract of a single-op write whose WAL append fails: the write
// returns an error that wraps ErrWALWriteFailed, the in-memory change stays
// applied, the result the caller receives is valid, and the change is on
// disk after the next Close (PR #602 forces that rewrite after any WAL
// failure). Before this contract, enqueueWAL, waitWALPending and writeToWAL
// (persistence_wal.go) logged the failure to stderr and reported success;
// every case below then failed at its first assertion with a nil error.
//
// The reopen check in each case also gates WAL.Truncate: it used to flush
// the buffered writer, which after a failed append holds the sticky error,
// so the truncate was refused and the next open replayed the pre-failure
// creates over the snapshot. The four delete and drop cases came back.

import (
	"errors"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vector"
	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// walFixture carries whatever a case's setup function builds so the same
// case's op/inMemory/persisted functions can find it again. Only the fields
// a given case needs are populated; the rest stay zero.
type walFixture struct {
	nodeID  uint64
	nodeIDs []uint64
	edgeID  uint64
	edgeIDs []uint64
	fromID  uint64
	midID   uint64
	toID    uint64
	propKey string
}

// walWriteErrorCase is one public write path under test.
//
//   - setup runs with the fault disarmed and builds whatever the write needs.
//   - op arms the fault, performs the write, and returns its error.
//   - inMemory checks the change is visible in the same store.
//   - persisted checks the change survived a Close and a reopen.
type walWriteErrorCase struct {
	name      string
	setup     func(t *testing.T, gs *GraphStorage) *walFixture
	op        func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error
	inMemory  func(t *testing.T, gs *GraphStorage, fx *walFixture)
	persisted func(t *testing.T, gs *GraphStorage, fx *walFixture)
}

func TestWALWriteError_SingleOpWritesReturnTheError(t *testing.T) {
	// Larger than the WAL's 4 KB bufio buffer, so the write it forces fails
	// inside wal.writeEntry rather than at the buffered writer's Flush.
	const blobSize = 1 << 20
	bigBlob := strings.Repeat("x", blobSize)

	mustCreateNode := func(t *testing.T, gs *GraphStorage, name string) *Node {
		t.Helper()
		n, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue(name)})
		if err != nil {
			t.Fatalf("setup: create node %s: %v", name, err)
		}
		return n
	}

	cases := []walWriteErrorCase{
		{
			name: "CreateNodeWithTenant",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				return &walFixture{}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				faults.FailWrite(vfstest.Once, 0)
				n, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"blob": StringValue(bigBlob)})
				if n != nil {
					fx.nodeID = n.ID
				}
				return err
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				got, err := gs.GetNodeForTenant(fx.nodeID, rtTenantA)
				if err != nil {
					t.Fatalf("in-memory read: %v", err)
				}
				if s, _ := got.Properties["blob"].AsString(); len(s) != blobSize {
					t.Fatalf("in-memory blob length: got %d want %d", len(s), blobSize)
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				got, err := gs.GetNodeForTenant(fx.nodeID, rtTenantA)
				if err != nil {
					t.Fatalf("after reopen: %v", err)
				}
				if s, _ := got.Properties["blob"].AsString(); len(s) != blobSize {
					t.Fatalf("after reopen, blob length: got %d want %d", len(s), blobSize)
				}
			},
		},
		{
			name: "CreateNodesWithTenant",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				return &walFixture{}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				specs := []NodeSpec{
					{Labels: []string{"Person"}, Properties: map[string]Value{"name": StringValue("a")}},
					{Labels: []string{"Person"}, Properties: map[string]Value{"name": StringValue("b")}},
				}
				faults.FailWrite(vfstest.Once, 0)
				ids, err := gs.CreateNodesWithTenant(rtTenantA, specs)
				fx.nodeIDs = ids
				return err
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if len(fx.nodeIDs) != 2 {
					t.Fatalf("expected 2 created node IDs, got %d", len(fx.nodeIDs))
				}
				for _, id := range fx.nodeIDs {
					if _, err := gs.GetNodeForTenant(id, rtTenantA); err != nil {
						t.Fatalf("in-memory read of node %d: %v", id, err)
					}
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				for _, id := range fx.nodeIDs {
					if _, err := gs.GetNodeForTenant(id, rtTenantA); err != nil {
						t.Fatalf("after reopen, node %d: %v", id, err)
					}
				}
			},
		},
		{
			name: "UpdateNodeForTenant",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				n := mustCreateNode(t, gs, "orig")
				return &walFixture{nodeID: n.ID}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				faults.FailWrite(vfstest.Once, 0)
				return gs.UpdateNodeForTenant(fx.nodeID, map[string]Value{"blob": StringValue(bigBlob)}, rtTenantA)
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				got, err := gs.GetNodeForTenant(fx.nodeID, rtTenantA)
				if err != nil {
					t.Fatalf("in-memory read: %v", err)
				}
				if s, _ := got.Properties["blob"].AsString(); len(s) != blobSize {
					t.Fatalf("in-memory blob length: got %d want %d", len(s), blobSize)
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				got, err := gs.GetNodeForTenant(fx.nodeID, rtTenantA)
				if err != nil {
					t.Fatalf("after reopen: %v", err)
				}
				if s, _ := got.Properties["blob"].AsString(); len(s) != blobSize {
					t.Fatalf("after reopen, blob length: got %d want %d", len(s), blobSize)
				}
			},
		},
		{
			name: "RemoveNodeProperties",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				n, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{
					"temp": StringValue("gone"),
					"keep": StringValue("stays"),
				})
				if err != nil {
					t.Fatalf("setup: create node: %v", err)
				}
				return &walFixture{nodeID: n.ID}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				faults.FailWrite(vfstest.Once, 0)
				return gs.RemoveNodeProperties(fx.nodeID, []string{"temp"})
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				got, err := gs.GetNodeForTenant(fx.nodeID, rtTenantA)
				if err != nil {
					t.Fatalf("in-memory read: %v", err)
				}
				if _, has := got.Properties["temp"]; has {
					t.Fatal("in-memory: removed property \"temp\" is still present")
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				got, err := gs.GetNodeForTenant(fx.nodeID, rtTenantA)
				if err != nil {
					t.Fatalf("after reopen: %v", err)
				}
				if _, has := got.Properties["temp"]; has {
					t.Fatal("after reopen: removed property \"temp\" came back")
				}
			},
		},
		{
			name: "DeleteNodeForTenant",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				n := mustCreateNode(t, gs, "doomed")
				return &walFixture{nodeID: n.ID}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				faults.FailWrite(vfstest.Once, 0)
				return gs.DeleteNodeForTenant(fx.nodeID, rtTenantA)
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if _, err := gs.GetNodeForTenant(fx.nodeID, rtTenantA); !errors.Is(err, ErrNodeNotFound) {
					t.Fatalf("in-memory: expected ErrNodeNotFound, got %v", err)
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if _, err := gs.GetNodeForTenant(fx.nodeID, rtTenantA); !errors.Is(err, ErrNodeNotFound) {
					t.Fatalf("after reopen: expected ErrNodeNotFound, got %v", err)
				}
			},
		},
		{
			name: "CreateEdgeWithTenant",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				a := mustCreateNode(t, gs, "a")
				b := mustCreateNode(t, gs, "b")
				return &walFixture{fromID: a.ID, toID: b.ID}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				faults.FailWrite(vfstest.Once, 0)
				e, err := gs.CreateEdgeWithTenant(rtTenantA, fx.fromID, fx.toID, "REL", map[string]Value{"blob": StringValue(bigBlob)}, 1.0)
				if e != nil {
					fx.edgeID = e.ID
				}
				return err
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				got, err := gs.GetEdgeForTenant(fx.edgeID, rtTenantA)
				if err != nil {
					t.Fatalf("in-memory read: %v", err)
				}
				if s, _ := got.Properties["blob"].AsString(); len(s) != blobSize {
					t.Fatalf("in-memory blob length: got %d want %d", len(s), blobSize)
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				got, err := gs.GetEdgeForTenant(fx.edgeID, rtTenantA)
				if err != nil {
					t.Fatalf("after reopen: %v", err)
				}
				if s, _ := got.Properties["blob"].AsString(); len(s) != blobSize {
					t.Fatalf("after reopen, blob length: got %d want %d", len(s), blobSize)
				}
			},
		},
		{
			name: "CreateEdgesWithTenant",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				a := mustCreateNode(t, gs, "a")
				b := mustCreateNode(t, gs, "b")
				c := mustCreateNode(t, gs, "c")
				return &walFixture{fromID: a.ID, midID: b.ID, toID: c.ID}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				specs := []EdgeSpec{
					{FromID: fx.fromID, ToID: fx.midID, Type: "REL", Weight: 1},
					{FromID: fx.midID, ToID: fx.toID, Type: "REL", Weight: 2},
				}
				faults.FailWrite(vfstest.Once, 0)
				ids, err := gs.CreateEdgesWithTenant(rtTenantA, specs)
				fx.edgeIDs = ids
				return err
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if len(fx.edgeIDs) != 2 {
					t.Fatalf("expected 2 created edge IDs, got %d", len(fx.edgeIDs))
				}
				for _, id := range fx.edgeIDs {
					if _, err := gs.GetEdgeForTenant(id, rtTenantA); err != nil {
						t.Fatalf("in-memory read of edge %d: %v", id, err)
					}
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				for _, id := range fx.edgeIDs {
					if _, err := gs.GetEdgeForTenant(id, rtTenantA); err != nil {
						t.Fatalf("after reopen, edge %d: %v", id, err)
					}
				}
			},
		},
		{
			name: "UpdateEdgeForTenant",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				a := mustCreateNode(t, gs, "a")
				b := mustCreateNode(t, gs, "b")
				e, err := gs.CreateEdgeWithTenant(rtTenantA, a.ID, b.ID, "REL", map[string]Value{"name": StringValue("orig")}, 1.0)
				if err != nil {
					t.Fatalf("setup: create edge: %v", err)
				}
				return &walFixture{edgeID: e.ID}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				w := 42.0
				faults.FailWrite(vfstest.Once, 0)
				return gs.UpdateEdgeForTenant(fx.edgeID, map[string]Value{"blob": StringValue(bigBlob)}, &w, rtTenantA)
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				got, err := gs.GetEdgeForTenant(fx.edgeID, rtTenantA)
				if err != nil {
					t.Fatalf("in-memory read: %v", err)
				}
				if got.Weight != 42.0 {
					t.Fatalf("in-memory weight: got %v want 42", got.Weight)
				}
				if s, _ := got.Properties["blob"].AsString(); len(s) != blobSize {
					t.Fatalf("in-memory blob length: got %d want %d", len(s), blobSize)
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				got, err := gs.GetEdgeForTenant(fx.edgeID, rtTenantA)
				if err != nil {
					t.Fatalf("after reopen: %v", err)
				}
				if got.Weight != 42.0 {
					t.Fatalf("after reopen, weight: got %v want 42", got.Weight)
				}
				if s, _ := got.Properties["blob"].AsString(); len(s) != blobSize {
					t.Fatalf("after reopen, blob length: got %d want %d", len(s), blobSize)
				}
			},
		},
		{
			name: "DeleteEdgeForTenant",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				a := mustCreateNode(t, gs, "a")
				b := mustCreateNode(t, gs, "b")
				e, err := gs.CreateEdgeWithTenant(rtTenantA, a.ID, b.ID, "REL", nil, 1.0)
				if err != nil {
					t.Fatalf("setup: create edge: %v", err)
				}
				return &walFixture{edgeID: e.ID}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				faults.FailWrite(vfstest.Once, 0)
				return gs.DeleteEdgeForTenant(fx.edgeID, rtTenantA)
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if _, err := gs.GetEdgeForTenant(fx.edgeID, rtTenantA); !errors.Is(err, ErrEdgeNotFound) {
					t.Fatalf("in-memory: expected ErrEdgeNotFound, got %v", err)
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if _, err := gs.GetEdgeForTenant(fx.edgeID, rtTenantA); !errors.Is(err, ErrEdgeNotFound) {
					t.Fatalf("after reopen: expected ErrEdgeNotFound, got %v", err)
				}
			},
		},
		{
			name: "UpsertEdgeWithTenant",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				a := mustCreateNode(t, gs, "a")
				b := mustCreateNode(t, gs, "b")
				return &walFixture{fromID: a.ID, toID: b.ID}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				faults.FailWrite(vfstest.Once, 0)
				e, created, err := gs.UpsertEdgeWithTenant(rtTenantA, fx.fromID, fx.toID, "REL", map[string]Value{"name": StringValue("v")}, 1.0)
				if e != nil {
					fx.edgeID = e.ID
				}
				if err == nil && !created {
					t.Fatal("expected UpsertEdgeWithTenant to take the create branch (no prior edge)")
				}
				return err
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if _, err := gs.GetEdgeForTenant(fx.edgeID, rtTenantA); err != nil {
					t.Fatalf("in-memory read: %v", err)
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if _, err := gs.GetEdgeForTenant(fx.edgeID, rtTenantA); err != nil {
					t.Fatalf("after reopen: %v", err)
				}
			},
		},
		{
			name: "CreatePropertyIndex",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				return &walFixture{propKey: "idx_create"}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				faults.FailWrite(vfstest.Once, 0)
				return gs.CreatePropertyIndex(fx.propKey, TypeString)
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if _, ok := gs.propertyIndexes[fx.propKey]; !ok {
					t.Fatal("in-memory: property index was not created")
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if _, ok := gs.propertyIndexes[fx.propKey]; !ok {
					t.Fatal("after reopen: property index did not survive")
				}
			},
		},
		{
			name: "DropPropertyIndex",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				const key = "idx_drop"
				if err := gs.CreatePropertyIndex(key, TypeString); err != nil {
					t.Fatalf("setup: create property index: %v", err)
				}
				return &walFixture{propKey: key}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				faults.FailWrite(vfstest.Once, 0)
				return gs.DropPropertyIndex(fx.propKey)
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if _, ok := gs.propertyIndexes[fx.propKey]; ok {
					t.Fatal("in-memory: dropped property index is still present")
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if _, ok := gs.propertyIndexes[fx.propKey]; ok {
					t.Fatal("after reopen: dropped property index came back")
				}
			},
		},
		{
			name: "CreateVectorIndex",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				return &walFixture{propKey: "vec_create"}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				faults.FailWrite(vfstest.Once, 0)
				return gs.CreateVectorIndex(fx.propKey, 3, 16, 200, vector.MetricCosine)
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if !gs.HasVectorIndex(fx.propKey) {
					t.Fatal("in-memory: vector index was not created")
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if !gs.HasVectorIndex(fx.propKey) {
					t.Fatal("after reopen: vector index did not survive")
				}
			},
		},
		{
			name: "DropVectorIndex",
			setup: func(t *testing.T, gs *GraphStorage) *walFixture {
				const key = "vec_drop"
				if err := gs.CreateVectorIndex(key, 3, 16, 200, vector.MetricCosine); err != nil {
					t.Fatalf("setup: create vector index: %v", err)
				}
				return &walFixture{propKey: key}
			},
			op: func(t *testing.T, gs *GraphStorage, faults *vfstest.FaultFS, fx *walFixture) error {
				faults.FailWrite(vfstest.Once, 0)
				return gs.DropVectorIndex(fx.propKey)
			},
			inMemory: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if gs.HasVectorIndex(fx.propKey) {
					t.Fatal("in-memory: dropped vector index is still present")
				}
			},
			persisted: func(t *testing.T, gs *GraphStorage, fx *walFixture) {
				if gs.HasVectorIndex(fx.propKey) {
					t.Fatal("after reopen: dropped vector index came back")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			faults := vfstest.NewFaults(vfs.OS(), tc.name)
			cfg := jsonConfig(dir)
			cfg.FS = faults
			gs := openJSON(t, cfg)

			fx := tc.setup(t, gs)

			err := tc.op(t, gs, faults, fx)
			if err == nil {
				t.Fatalf("%s returned a nil error: the single-op WAL write path swallowed the injected failure", tc.name)
			}
			if !errors.Is(err, ErrWALWriteFailed) {
				t.Fatalf("%s error does not satisfy errors.Is(err, ErrWALWriteFailed): %v", tc.name, err)
			}
			if !faults.Fired() {
				t.Fatal("the write fault never fired, so the WAL append succeeded and this test proves nothing")
			}

			tc.inMemory(t, gs, fx)

			faults.Clear()
			// Close must succeed: the truncate resets the buffered writer
			// instead of flushing the sticky error through it.
			if err := gs.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}

			gs2 := openJSON(t, cfg)
			defer gs2.Close()
			tc.persisted(t, gs2, fx)
		})
	}
}

// TestWALWriteError_EveryWALBackend runs the CreateNodeWithTenant case across
// the three WAL backends a JSON-mode store can use. On the batched backend
// the flush runs on a background goroutine and the error comes back through
// the *wal.Pending returned by enqueueWAL — waitWALPending discards it today,
// same as the plain and compressed paths, so the same assertions apply.
func TestWALWriteError_EveryWALBackend(t *testing.T) {
	backends := []struct {
		name   string
		modify func(cfg *StorageConfig)
	}{
		{name: "plain", modify: func(cfg *StorageConfig) {}},
		{name: "batched", modify: func(cfg *StorageConfig) { cfg.EnableBatching = true }},
		{name: "compressed", modify: func(cfg *StorageConfig) { cfg.EnableCompression = true }},
	}

	for _, b := range backends {
		t.Run(b.name, func(t *testing.T) {
			dir := t.TempDir()
			faults := vfstest.NewFaults(vfs.OS(), b.name)
			cfg := jsonConfig(dir)
			b.modify(&cfg)
			cfg.FS = faults
			gs := openJSON(t, cfg)

			faults.FailWrite(vfstest.Once, 0)
			n, err := gs.CreateNodeWithTenant(rtTenantA, []string{"Person"}, map[string]Value{"name": StringValue("x")})
			if err == nil {
				t.Fatalf("%s: CreateNodeWithTenant returned a nil error", b.name)
			}
			if !errors.Is(err, ErrWALWriteFailed) {
				t.Fatalf("%s: error does not satisfy errors.Is(err, ErrWALWriteFailed): %v", b.name, err)
			}
			if !faults.Fired() {
				t.Fatal("the write fault never fired, so the WAL append succeeded and this test proves nothing")
			}
			if n == nil {
				t.Fatalf("%s: CreateNodeWithTenant returned a nil node alongside the error", b.name)
			}

			if _, err := gs.GetNodeForTenant(n.ID, rtTenantA); err != nil {
				t.Fatalf("%s: in-memory read: %v", b.name, err)
			}

			faults.Clear()
			if err := gs.Close(); err != nil {
				t.Logf("%s: close: %v", b.name, err)
			}

			gs2 := openJSON(t, cfg)
			defer gs2.Close()
			if _, err := gs2.GetNodeForTenant(n.ID, rtTenantA); err != nil {
				t.Fatalf("%s: after reopen: %v", b.name, err)
			}
		})
	}
}
