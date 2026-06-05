// Package storage's BTreeGraphStorage is a B+Tree-backed implementation of
// the Storage interface (C2 extraction). It is the first non-*GraphStorage
// implementor of S1's narrowed Storage interface.
//
// Status: PARTIAL. Only the *ForTenant CRUD surface needed by tests is
// fully implemented (CreateNode/Edge, GetNode/Edge, label/type indexes,
// adjacency, statistics, multi-tenancy isolation, snapshot via flush,
// DeleteNodeForTenant). Tenant-blind reads return ErrNodeNotFound /
// ErrEdgeNotFound, mutating tenant-blind methods + UpdateForTenant +
// DeleteEdgeForTenant + UpsertEdgeWithTenant + RemoveNodePropertiesForTenant
// + property-find + vector ops + Batch all return errBTreeBackendUnsupported
// (or no-op nil where the contract is "best-effort cleanup"). BeginBatch
// panics with a clear message because its return is *Batch — there is no
// error channel to surface non-implementation through.
//
// Do not select this backend in production. The compile-time assertion at
// the bottom enforces method-set parity with Storage; it says nothing
// about runtime completeness for stubbed methods.
//
// Surface area follow-ups tracked as C2.1 (write-method completion) and
// R1/F4 (tenant-strict vector redesign).

package storage

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/dd0wney/graphdb/pkg/btree"
	"github.com/dd0wney/graphdb/pkg/encryption"
	"github.com/dd0wney/graphdb/pkg/vector"
)

// errBTreeBackendUnsupported is returned by every BTreeGraphStorage method
// that has no real implementation in this C2 extraction. Callers that wire
// the backend in and hit this error get an explicit signal rather than
// silent (nil, nil) returns that would corrupt graph invariants.
var errBTreeBackendUnsupported = errors.New("operation not implemented in BTree backend (C2 partial; see C2.1 / R1)")

// BTreeGraphStorage is a graph store backed by pkg/btree's persistent
// B+Tree. Keys follow a typed-prefix layout:
//
//	n:{tenant}:{nodeID}              -> JSON(Node)
//	e:{tenant}:{fromID}:{type}:{toID} -> JSON(Edge)
//	ei:{tenant}:{edgeID}             -> primary edge key (lookup index)
//	i:{tenant}:{toID}:{type}:{fromID} -> sentinel (incoming-edge index)
//	l:{tenant}:{label}:{nodeID}      -> sentinel (label index)
//	meta:nextNodeID / meta:nextEdgeID -> 8-byte big-endian counters
//
// Counters are persisted on Close() and Snapshot(); IDs are issued via
// atomic.AddUint64 within the running process.
type BTreeGraphStorage struct {
	tree *btree.Tree

	nextNodeID uint64
	nextEdgeID uint64

	dataDir string
}

// NewBTreeGraphStorage opens (or initializes) a B+Tree-backed graph store
// rooted at dataDir/graph.db.
func NewBTreeGraphStorage(dataDir string) (*BTreeGraphStorage, error) {
	treePath := filepath.Join(dataDir, "graph.db")
	tree, err := btree.Open(treePath)
	if err != nil {
		return nil, err
	}

	gs := &BTreeGraphStorage{
		tree:    tree,
		dataDir: dataDir,
	}

	if val, ok := tree.Get([]byte("meta:nextNodeID")); ok {
		gs.nextNodeID = binary.BigEndian.Uint64(val)
	} else {
		gs.nextNodeID = 1
	}

	if val, ok := tree.Get([]byte("meta:nextEdgeID")); ok {
		gs.nextEdgeID = binary.BigEndian.Uint64(val)
	} else {
		gs.nextEdgeID = 1
	}

	return gs, nil
}

// Close persists ID counters and closes the underlying B+Tree.
func (gs *BTreeGraphStorage) Close() error {
	gs.persistCounters()
	return gs.tree.Close()
}

func (gs *BTreeGraphStorage) persistCounters() {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, atomic.LoadUint64(&gs.nextNodeID))
	_ = gs.tree.Put([]byte("meta:nextNodeID"), buf)

	binary.BigEndian.PutUint64(buf, atomic.LoadUint64(&gs.nextEdgeID))
	_ = gs.tree.Put([]byte("meta:nextEdgeID"), buf)
}

// --- StorageReader: tenant-aware reads (real implementations) -----------

func (gs *BTreeGraphStorage) GetNodeForTenant(nodeID uint64, tenantID string) (*Node, error) {
	val, ok := gs.tree.Get(gs.nodeKey(tenantID, nodeID))
	if !ok {
		return nil, ErrNodeNotFound
	}
	var node Node
	if err := json.Unmarshal(val, &node); err != nil {
		return nil, err
	}
	return &node, nil
}

func (gs *BTreeGraphStorage) GetNodesByLabelForTenant(tenantID string, label string) []*Node {
	prefix := gs.labelPrefix(tenantID, label)
	cursor := gs.tree.Cursor(prefix)

	var nodes []*Node
	for {
		k, _, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), string(prefix)) {
			break
		}
		// l:{tenant}:{label}:{nodeID}
		parts := strings.Split(string(k), ":")
		if len(parts) < 4 {
			continue
		}
		var nodeID uint64
		if _, err := fmt.Sscanf(parts[3], "%d", &nodeID); err != nil {
			continue
		}
		if node, err := gs.GetNodeForTenant(nodeID, tenantID); err == nil {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func (gs *BTreeGraphStorage) GetAllNodesForTenant(tenantID string) []*Node {
	prefix := []byte(fmt.Sprintf("n:%s:", tenantID))
	cursor := gs.tree.Cursor(prefix)

	var nodes []*Node
	for {
		k, v, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), string(prefix)) {
			break
		}
		var node Node
		if err := json.Unmarshal(v, &node); err == nil {
			nodes = append(nodes, &node)
		}
	}
	return nodes
}

func (gs *BTreeGraphStorage) CountNodesForTenant(tenantID string) uint64 {
	return uint64(len(gs.GetAllNodesForTenant(tenantID)))
}

func (gs *BTreeGraphStorage) GetEdgeForTenant(edgeID uint64, tenantID string) (*Edge, error) {
	// Edges are keyed by (from,type,to) for adjacency scans; the ei: index
	// gives O(log n) lookup by edgeID alone.
	idxKey := []byte(fmt.Sprintf("ei:%s:%d", tenantID, edgeID))
	edgeKey, ok := gs.tree.Get(idxKey)
	if !ok {
		return nil, ErrEdgeNotFound
	}
	val, ok := gs.tree.Get(edgeKey)
	if !ok {
		return nil, ErrEdgeNotFound
	}
	var edge Edge
	if err := json.Unmarshal(val, &edge); err != nil {
		return nil, err
	}
	return &edge, nil
}

func (gs *BTreeGraphStorage) GetOutgoingEdgesForTenant(nodeID uint64, tenantID string) ([]*Edge, error) {
	prefix := []byte(fmt.Sprintf("e:%s:%d:", tenantID, nodeID))
	cursor := gs.tree.Cursor(prefix)

	var edges []*Edge
	for {
		k, v, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), string(prefix)) {
			break
		}
		var edge Edge
		if err := json.Unmarshal(v, &edge); err == nil {
			edges = append(edges, &edge)
		}
	}
	return edges, nil
}

func (gs *BTreeGraphStorage) GetIncomingEdgesForTenant(nodeID uint64, tenantID string) ([]*Edge, error) {
	prefix := []byte(fmt.Sprintf("i:%s:%d:", tenantID, nodeID))
	cursor := gs.tree.Cursor(prefix)

	var edges []*Edge
	for {
		k, _, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), string(prefix)) {
			break
		}
		// Incoming key: i:{tenant}:{toID}:{type}:{fromID}.
		// Reconstruct primary edge key e:{tenant}:{fromID}:{type}:{toID}.
		parts := strings.Split(string(k), ":")
		if len(parts) < 5 {
			continue
		}
		toID := parts[2]
		edgeType := parts[3]
		fromID := parts[4]
		eKey := []byte(fmt.Sprintf("e:%s:%s:%s:%s", tenantID, fromID, edgeType, toID))
		val, ok := gs.tree.Get(eKey)
		if !ok {
			continue
		}
		var edge Edge
		if err := json.Unmarshal(val, &edge); err == nil {
			edges = append(edges, &edge)
		}
	}
	return edges, nil
}

func (gs *BTreeGraphStorage) GetEdgesByTypeForTenant(tenantID string, edgeType string) []*Edge {
	prefix := []byte(fmt.Sprintf("e:%s:", tenantID))
	cursor := gs.tree.Cursor(prefix)

	var edges []*Edge
	for {
		k, v, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), string(prefix)) {
			break
		}
		var edge Edge
		if err := json.Unmarshal(v, &edge); err != nil {
			continue
		}
		if edge.Type == edgeType {
			edges = append(edges, &edge)
		}
	}
	return edges
}

func (gs *BTreeGraphStorage) GetAllEdgesForTenant(tenantID string) []*Edge {
	prefix := []byte(fmt.Sprintf("e:%s:", tenantID))
	cursor := gs.tree.Cursor(prefix)

	var edges []*Edge
	for {
		k, v, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), string(prefix)) {
			break
		}
		var edge Edge
		if err := json.Unmarshal(v, &edge); err == nil {
			edges = append(edges, &edge)
		}
	}
	return edges
}

func (gs *BTreeGraphStorage) CountEdgesForTenant(tenantID string) uint64 {
	return uint64(len(gs.GetAllEdgesForTenant(tenantID)))
}

func (gs *BTreeGraphStorage) GetLabelsForTenant(tenantID string) []string {
	prefix := []byte(fmt.Sprintf("l:%s:", tenantID))
	cursor := gs.tree.Cursor(prefix)

	labelSet := make(map[string]struct{})
	for {
		k, _, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), string(prefix)) {
			break
		}
		// l:{tenant}:{label}:{nodeID}
		parts := strings.Split(string(k), ":")
		if len(parts) >= 3 {
			labelSet[parts[2]] = struct{}{}
		}
	}

	labels := make([]string, 0, len(labelSet))
	for l := range labelSet {
		labels = append(labels, l)
	}
	return labels
}

func (gs *BTreeGraphStorage) GetEdgeTypesForTenant(tenantID string) []string {
	prefix := []byte(fmt.Sprintf("e:%s:", tenantID))
	cursor := gs.tree.Cursor(prefix)

	typeSet := make(map[string]struct{})
	for {
		k, _, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), string(prefix)) {
			break
		}
		// e:{tenant}:{from}:{type}:{to}
		parts := strings.Split(string(k), ":")
		if len(parts) >= 4 {
			typeSet[parts[3]] = struct{}{}
		}
	}

	types := make([]string, 0, len(typeSet))
	for t := range typeSet {
		types = append(types, t)
	}
	return types
}

// --- StorageReader: tenant-blind reads (admin / cross-tenant aggregates) -

func (gs *BTreeGraphStorage) GetAllLabels() []string {
	cursor := gs.tree.Cursor([]byte("l:"))

	labelSet := make(map[string]struct{})
	for {
		k, _, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), "l:") {
			break
		}
		parts := strings.Split(string(k), ":")
		if len(parts) >= 3 {
			labelSet[parts[2]] = struct{}{}
		}
	}

	labels := make([]string, 0, len(labelSet))
	for l := range labelSet {
		labels = append(labels, l)
	}
	return labels
}

func (gs *BTreeGraphStorage) GetAllNodesAcrossTenants() []*Node {
	cursor := gs.tree.Cursor([]byte("n:"))

	var nodes []*Node
	for {
		k, v, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), "n:") {
			break
		}
		var node Node
		if err := json.Unmarshal(v, &node); err == nil {
			nodes = append(nodes, &node)
		}
	}
	return nodes
}

func (gs *BTreeGraphStorage) GetAllEdgesAcrossTenants() []*Edge {
	cursor := gs.tree.Cursor([]byte("e:"))

	var edges []*Edge
	for {
		k, v, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), "e:") {
			break
		}
		var edge Edge
		if err := json.Unmarshal(v, &edge); err == nil {
			edges = append(edges, &edge)
		}
	}
	return edges
}

// Tenant-blind point reads have no canonical answer in a tenant-strict
// storage layout — by construction every node/edge here is owned by a
// tenant. Returning ErrNodeNotFound / ErrEdgeNotFound is the same error
// shape used for cross-tenant misses elsewhere; callers must use the
// *ForTenant variants.

func (gs *BTreeGraphStorage) GetNode(nodeID uint64) (*Node, error)            { return nil, ErrNodeNotFound }
func (gs *BTreeGraphStorage) GetEdge(edgeID uint64) (*Edge, error)            { return nil, ErrEdgeNotFound }
func (gs *BTreeGraphStorage) GetOutgoingEdges(nodeID uint64) ([]*Edge, error) { return nil, nil }
func (gs *BTreeGraphStorage) GetIncomingEdges(nodeID uint64) ([]*Edge, error) { return nil, nil }
func (gs *BTreeGraphStorage) FindNodesByLabelAcrossTenants(label string) ([]*Node, error) {
	return nil, nil
}

func (gs *BTreeGraphStorage) FindNodesByPropertyForTenant(key string, value Value, tenantID string) ([]*Node, error) {
	return nil, errBTreeBackendUnsupported
}
func (gs *BTreeGraphStorage) FindNodesByPropertyIndexedForTenant(key string, value Value, tenantID string) ([]*Node, error) {
	return nil, errBTreeBackendUnsupported
}

func (gs *BTreeGraphStorage) HasPropertyIndex(key string) bool { return false }

// --- StorageReader: WithNodeRefForTenant + GetStatistics -----------------

func (gs *BTreeGraphStorage) WithNodeRefForTenant(nodeID uint64, tenantID string, fn func(*Node) error) error {
	node, err := gs.GetNodeForTenant(nodeID, tenantID)
	if err != nil {
		return err
	}
	return fn(node)
}

func (gs *BTreeGraphStorage) GetStatistics() Statistics {
	return Statistics{
		NodeCount: uint64(len(gs.GetAllNodesAcrossTenants())),
		EdgeCount: uint64(len(gs.GetAllEdgesAcrossTenants())),
	}
}

// --- StorageWriter: tenant-aware mutations (real implementations) -------

func (gs *BTreeGraphStorage) CreateNodeWithTenant(tenantID string, labels []string, properties map[string]Value) (*Node, error) {
	nodeID := atomic.AddUint64(&gs.nextNodeID, 1) - 1
	node := &Node{
		ID:         nodeID,
		Labels:     labels,
		Properties: properties,
		TenantID:   tenantID,
	}

	val, err := json.Marshal(node)
	if err != nil {
		return nil, err
	}
	if err := gs.tree.Put(gs.nodeKey(tenantID, nodeID), val); err != nil {
		return nil, err
	}

	for _, label := range labels {
		_ = gs.tree.Put(gs.labelKey(tenantID, label, nodeID), []byte{1})
	}

	return node, nil
}

func (gs *BTreeGraphStorage) CreateEdgeWithTenant(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error) {
	edgeID := atomic.AddUint64(&gs.nextEdgeID, 1) - 1
	edge := &Edge{
		ID:         edgeID,
		FromNodeID: fromID,
		ToNodeID:   toID,
		Type:       edgeType,
		Properties: properties,
		Weight:     weight,
		TenantID:   tenantID,
	}

	val, err := json.Marshal(edge)
	if err != nil {
		return nil, err
	}
	eKey := gs.edgeKey(tenantID, fromID, edgeType, toID)
	if err := gs.tree.Put(eKey, val); err != nil {
		return nil, err
	}

	// Secondary indexes: edgeID -> primary key, and incoming adjacency.
	_ = gs.tree.Put([]byte(fmt.Sprintf("ei:%s:%d", tenantID, edgeID)), eKey)
	_ = gs.tree.Put(gs.incomingEdgeKey(tenantID, toID, edgeType, fromID), []byte{1})

	return edge, nil
}

// DeleteNodeForTenant removes the node record and its label-index entries.
//
// TODO(C2.1): unlike (*GraphStorage).DeleteNodeForTenant, this does NOT
// cascade outgoing/incoming edges. Tracked as a behavioral gap; callers
// that depend on cascade delete must use the in-memory backend until C2.1.
func (gs *BTreeGraphStorage) DeleteNodeForTenant(nodeID uint64, tenantID string) error {
	node, err := gs.GetNodeForTenant(nodeID, tenantID)
	if err != nil {
		return err
	}

	for _, label := range node.Labels {
		_ = gs.tree.Delete(gs.labelKey(tenantID, label, nodeID))
	}

	return gs.tree.Delete(gs.nodeKey(tenantID, nodeID))
}

// --- StorageWriter: stubs (errBTreeBackendUnsupported / panic / no-op) --

// CreateNodeWithUniquePropertyForTenant is the B-lite atomic uniqueness
// primitive used by graphdb-coord. Returning a typed error (not silent
// (nil, nil)) keeps coord state from being silently corrupted if anyone
// wires this backend in.
func (gs *BTreeGraphStorage) CreateNodeWithUniquePropertyForTenant(tenantID string, labels []string, properties map[string]Value, uniqueLabel string, uniquePropertyKey string) (*Node, error) {
	return nil, errBTreeBackendUnsupported
}

func (gs *BTreeGraphStorage) UpdateNodeForTenant(nodeID uint64, properties map[string]Value, tenantID string) error {
	return errBTreeBackendUnsupported
}

func (gs *BTreeGraphStorage) RemoveNodePropertiesForTenant(nodeID uint64, keys []string, tenantID string) error {
	return errBTreeBackendUnsupported
}

func (gs *BTreeGraphStorage) UpdateEdgeForTenant(edgeID uint64, properties map[string]Value, weight *float64, tenantID string) error {
	return errBTreeBackendUnsupported
}

func (gs *BTreeGraphStorage) DeleteEdgeForTenant(edgeID uint64, tenantID string) error {
	return errBTreeBackendUnsupported
}

func (gs *BTreeGraphStorage) UpsertEdgeWithTenant(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, bool, error) {
	return nil, false, errBTreeBackendUnsupported
}

func (gs *BTreeGraphStorage) CreateNode(labels []string, properties map[string]Value) (*Node, error) {
	return nil, errBTreeBackendUnsupported
}
func (gs *BTreeGraphStorage) CreateEdge(fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error) {
	return nil, errBTreeBackendUnsupported
}
func (gs *BTreeGraphStorage) UpdateNode(nodeID uint64, properties map[string]Value) error {
	return errBTreeBackendUnsupported
}
func (gs *BTreeGraphStorage) DeleteNode(nodeID uint64) error { return errBTreeBackendUnsupported }
func (gs *BTreeGraphStorage) RemoveNodeProperties(nodeID uint64, keys []string) error {
	return errBTreeBackendUnsupported
}
func (gs *BTreeGraphStorage) UpdateEdge(edgeID uint64, properties map[string]Value, weight *float64) error {
	return errBTreeBackendUnsupported
}
func (gs *BTreeGraphStorage) DeleteEdge(edgeID uint64) error { return errBTreeBackendUnsupported }

// BeginBatch's signature returns *Batch — there is no error channel, so a
// silent nil return would nil-deref on the first operation. Panic is the
// least-surprising failure mode here; production code must select
// *GraphStorage if batches are needed.
func (gs *BTreeGraphStorage) BeginBatch() *Batch {
	panic("BeginBatch: not implemented in BTree backend (C2 partial; use *GraphStorage)")
}

// --- Storage: vector index methods (deferred to R1 / F4 redesign) -------
//
// pkg/vector's persistent-index API (NewPersistentHNSWIndex, SaveMetadata)
// no longer exists in tree; the F4 redesign (Track R1) will reintroduce
// tenant-strict vector ops with proper KV-backed persistence. Until then
// every vector method on this backend is a no-op or returns an error.

func (gs *BTreeGraphStorage) VectorSearch(propertyName string, query []float32, k int, ef int) ([]vector.SearchResult, error) {
	return nil, errBTreeBackendUnsupported
}
func (gs *BTreeGraphStorage) ListVectorIndexes() []string             { return nil }
func (gs *BTreeGraphStorage) HasVectorIndex(propertyName string) bool { return false }
func (gs *BTreeGraphStorage) CreateVectorIndex(propertyName string, dimensions int, m int, efConstruction int, metric vector.DistanceMetric) error {
	return errBTreeBackendUnsupported
}
func (gs *BTreeGraphStorage) DropVectorIndex(propertyName string) error {
	return errBTreeBackendUnsupported
}
func (gs *BTreeGraphStorage) GetVectorIndexMetric(propertyName string) (vector.DistanceMetric, error) {
	return "", errBTreeBackendUnsupported
}

// Tenant-scoped vector index stubs (R3 / S1 closure). Same shapes as the
// tenant-blind variants above — the BTree backend does not implement
// vector indexes (mutating methods return errBTreeBackendUnsupported,
// probing methods return false/empty/error per the F4 spike's unified-
// response convention).
func (gs *BTreeGraphStorage) VectorSearchForTenant(tenantID string, propertyName string, query []float32, k int, ef int) ([]vector.SearchResult, error) {
	return nil, errBTreeBackendUnsupported
}
func (gs *BTreeGraphStorage) ListVectorIndexesForTenant(tenantID string) []string {
	return nil
}
func (gs *BTreeGraphStorage) HasVectorIndexForTenant(tenantID string, propertyName string) bool {
	return false
}
func (gs *BTreeGraphStorage) CreateVectorIndexForTenant(tenantID string, propertyName string, dimensions int, m int, efConstruction int, metric vector.DistanceMetric) error {
	return errBTreeBackendUnsupported
}
func (gs *BTreeGraphStorage) DropVectorIndexForTenant(tenantID string, propertyName string) error {
	return errBTreeBackendUnsupported
}
func (gs *BTreeGraphStorage) GetVectorIndexMetricForTenant(tenantID string, propertyName string) (vector.DistanceMetric, error) {
	return "", errBTreeBackendUnsupported
}

// UpdateNodeVectorIndexes / RemoveNodeFromVectorIndexes are best-effort
// maintenance hooks; returning nil keeps callers like CreateNodeWithTenant
// quiet. Real maintenance is the in-memory backend's domain (R1.x).
func (gs *BTreeGraphStorage) UpdateNodeVectorIndexes(node *Node) error { return nil }
func (gs *BTreeGraphStorage) RemoveNodeFromVectorIndexes(nodeID uint64, tenantID string) error {
	return nil
}

// AddObserver is a no-op on the BTree backend: the underlying observer
// notify mechanism lives on *GraphStorage. Observers attached to a
// BTreeGraphStorage simply never fire. This is intentional — the BTree
// backend is a C2-stage experimental write surface, not an observer
// dispatch host.
func (gs *BTreeGraphStorage) AddObserver(obs NodeObserver) {}

// --- Storage: encryption + snapshot + close -----------------------------

// SetEncryption is a no-op: at-rest encryption for the BTree backend is
// out of scope for C2 (the in-memory backend's encryption integration is
// the canonical path).
func (gs *BTreeGraphStorage) SetEncryption(engine encryption.EncryptDecrypter, keyManager encryption.KeyProvider) {
}

// Snapshot persists ID counters and flushes the underlying B+Tree.
//
// Signature note: matches S1 (`Snapshot() error`, no ctx). The archive
// parent had `Snapshot(ctx)` for cancellability, but R3 (this S1
// closure) deliberately kept the no-ctx shape — see interface.go's
// header comment for the rationale. A future cancelable/streaming
// snapshot would be a new method (e.g., SnapshotStream), not a
// signature change to Snapshot.
func (gs *BTreeGraphStorage) Snapshot() error {
	gs.persistCounters()
	return gs.tree.Flush()
}

// --- Helpers -------------------------------------------------------------

func (gs *BTreeGraphStorage) nodeKey(tenantID string, id uint64) []byte {
	return []byte(fmt.Sprintf("n:%s:%d", tenantID, id))
}

func (gs *BTreeGraphStorage) edgeKey(tenantID string, fromID uint64, edgeType string, toID uint64) []byte {
	return []byte(fmt.Sprintf("e:%s:%d:%s:%d", tenantID, fromID, edgeType, toID))
}

func (gs *BTreeGraphStorage) incomingEdgeKey(tenantID string, toID uint64, edgeType string, fromID uint64) []byte {
	return []byte(fmt.Sprintf("i:%s:%d:%s:%d", tenantID, toID, edgeType, fromID))
}

func (gs *BTreeGraphStorage) labelKey(tenantID string, label string, nodeID uint64) []byte {
	return []byte(fmt.Sprintf("l:%s:%s:%d", tenantID, label, nodeID))
}

func (gs *BTreeGraphStorage) labelPrefix(tenantID string, label string) []byte {
	return []byte(fmt.Sprintf("l:%s:%s:", tenantID, label))
}

// Compile-time assertion: the BTreeGraphStorage method set must remain
// flush with Storage. Removing a method or drifting a signature will fail
// build here, not at the call site.
var _ Storage = (*BTreeGraphStorage)(nil)
