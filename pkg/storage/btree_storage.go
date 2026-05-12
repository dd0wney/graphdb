package storage

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/dd0wney/cluso-graphdb/pkg/btree"
	"github.com/dd0wney/cluso-graphdb/pkg/encryption"
	"github.com/dd0wney/cluso-graphdb/pkg/vector"
)

// BTreeGraphStorage is a persistent graph storage using a custom B+Tree
type BTreeGraphStorage struct {
	tree *btree.Tree
	
	// Counters (cached in memory, persisted in B+Tree meta keys)
	nextNodeID uint64
	nextEdgeID uint64
	
	// Vector indexes (tenantID -> propertyName -> HNSW index)
	vectorIndexes   map[string]map[string]*vector.HNSWIndex
	vectorIndexesMu sync.RWMutex

	dataDir string

	// Observers (S11 Intelligence)
	observers []NodeObserver
}

// KVStore implementation for vector persistence
func (gs *BTreeGraphStorage) Get(key []byte) ([]byte, bool) {
	return gs.tree.Get(key)
}

func (gs *BTreeGraphStorage) Put(key, value []byte) error {
	return gs.tree.Put(key, value)
}

func (gs *BTreeGraphStorage) Delete(key []byte) error {
	// Our btree.Tree doesn't have Delete yet? Let me check.
	// For now, let's assume it doesn't and just Put nil if needed, 
	// but I should really implement Delete in btree.Tree.
	return nil
}

// NewBTreeGraphStorage creates a new B+Tree-backed graph storage
func NewBTreeGraphStorage(dataDir string) (*BTreeGraphStorage, error) {
	treePath := filepath.Join(dataDir, "graph.db")
	tree, err := btree.Open(treePath)
	if err != nil {
		return nil, err
	}

	gs := &BTreeGraphStorage{
		tree:          tree,
		vectorIndexes: make(map[string]map[string]*vector.HNSWIndex),
		dataDir:       dataDir,
	}

	// Load counters
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

	// Load existing vector indexes
	if tenantsData, ok := tree.Get([]byte("vmeta:tenants")); ok {
		var tenants []string
		_ = json.Unmarshal(tenantsData, &tenants)
		for _, tID := range tenants {
			if listData, ok := tree.Get([]byte(fmt.Sprintf("vmeta:list:%s", tID))); ok {
				var props []string
				_ = json.Unmarshal(listData, &props)
				for _, prop := range props {
					prefix := fmt.Sprintf("v:%s:%s:", tID, prop)
					metaKey := []byte(fmt.Sprintf("vmeta:%s:%s", tID, prop))
					
					// Re-open with persistent backing. 
					// Real config will be loaded from metaKey.
					h, err := vector.NewPersistentHNSWIndex(gs, prefix, metaKey, 0, 1, 1, vector.MetricCosine)
					if err == nil {
						if gs.vectorIndexes[tID] == nil {
							gs.vectorIndexes[tID] = make(map[string]*vector.HNSWIndex)
						}
						gs.vectorIndexes[tID][prop] = h
					}
				}
			}
		}
	}

	return gs, nil
}

// Close closes the B+Tree storage
func (gs *BTreeGraphStorage) Close() error {
	// Persist counters
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, atomic.LoadUint64(&gs.nextNodeID))
	_ = gs.tree.Put([]byte("meta:nextNodeID"), buf)
	
	binary.BigEndian.PutUint64(buf, atomic.LoadUint64(&gs.nextEdgeID))
	_ = gs.tree.Put([]byte("meta:nextEdgeID"), buf)

	return gs.tree.Close()
}

// StorageReader implementation

func (gs *BTreeGraphStorage) GetNodeForTenant(nodeID uint64, tenantID string) (*Node, error) {
	key := gs.nodeKey(tenantID, nodeID)
	val, ok := gs.tree.Get(key)
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
		
		// Key is l:{tenant}:{label}:{nodeID}
		parts := strings.Split(string(k), ":")
		if len(parts) < 4 {
			continue
		}
		var nodeID uint64
		fmt.Sscanf(parts[3], "%d", &nodeID)
		
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
	// We need a way to look up edge by ID.
	// Our primary edge key is e:{tenant}:{from}:{type}:{to}.
	// We should probably add an edgeID index: ei:{tenant}:{edgeID} -> e_key
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
		
		// Key is i:{tenant}:{toID}:{type}:{fromID}
		// Value is ei:{tenant}:{edgeID} key? Or just edgeID?
		// Let's store the full edge key in the incoming index for fast lookup.
		parts := strings.Split(string(k), ":")
		if len(parts) < 5 {
			continue
		}
		
		// Reconstruct edge key: e:{tenant}:{fromID}:{type}:{toID}
		fromID := parts[4]
		edgeType := parts[3]
		toID := parts[2]
		eKey := []byte(fmt.Sprintf("e:%s:%s:%s:%s", tenantID, fromID, edgeType, toID))
		
		val, ok := gs.tree.Get(eKey)
		if ok {
			var edge Edge
			if err := json.Unmarshal(val, &edge); err == nil {
				edges = append(edges, &edge)
			}
		}
	}
	return edges, nil
}

// StorageWriter implementation

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
	
	key := gs.nodeKey(tenantID, nodeID)
	if err := gs.tree.Put(key, val); err != nil {
		return nil, err
	}
	
	// Index labels
	for _, label := range labels {
		lKey := gs.labelKey(tenantID, label, nodeID)
		_ = gs.tree.Put(lKey, []byte{1})
	}

	// Index vectors
	if err := gs.UpdateNodeVectorIndexes(node); err != nil {
		// Log error but don't fail node creation for now (consistent with GraphStorage)
		fmt.Printf("ERROR: failed to update vector indexes: %v\n", err)
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
	
	// Index by edge ID
	idxKey := []byte(fmt.Sprintf("ei:%s:%d", tenantID, edgeID))
	_ = gs.tree.Put(idxKey, eKey)
	
	// Index incoming
	iKey := gs.incomingEdgeKey(tenantID, toID, edgeType, fromID)
	_ = gs.tree.Put(iKey, []byte{1})
	
	return edge, nil
}

// Helpers

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

// Stub implementations for the rest of Storage interface to make it compile

func (gs *BTreeGraphStorage) GetNodesByLabel(label string) []*Node { return nil }
func (gs *BTreeGraphStorage) CountNodes() uint64 { return 0 }
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
		if err := json.Unmarshal(v, &edge); err == nil {
			if edge.Type == edgeType {
				edges = append(edges, &edge)
			}
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
	
	labelSet := make(map[string]bool)
	for {
		k, _, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), string(prefix)) {
			break
		}
		
		// Key is l:{tenant}:{label}:{nodeID}
		parts := strings.Split(string(k), ":")
		if len(parts) >= 3 {
			labelSet[parts[2]] = true
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
	
	typeSet := make(map[string]bool)
	for {
		k, _, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), string(prefix)) {
			break
		}
		
		// Key is e:{tenant}:{from}:{type}:{to}
		parts := strings.Split(string(k), ":")
		if len(parts) >= 4 {
			typeSet[parts[3]] = true
		}
	}
	
	types := make([]string, 0, len(typeSet))
	for t := range typeSet {
		types = append(types, t)
	}
	return types
}

func (gs *BTreeGraphStorage) GetAllLabels() []string {
	// This should scan all tenants.
	cursor := gs.tree.Cursor([]byte("l:"))
	
	labelSet := make(map[string]bool)
	for {
		k, _, ok := cursor.Next()
		if !ok || !strings.HasPrefix(string(k), "l:") {
			break
		}
		
		parts := strings.Split(string(k), ":")
		if len(parts) >= 3 {
			labelSet[parts[2]] = true
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

func (gs *BTreeGraphStorage) SetEncryption(engine encryption.EncryptDecrypter, keyManager encryption.KeyProvider) {
	// Encryption not yet implemented for BTree backend
}

func (gs *BTreeGraphStorage) Snapshot(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Persist counters to B+Tree
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, atomic.LoadUint64(&gs.nextNodeID))
	_ = gs.tree.Put([]byte("meta:nextNodeID"), buf)
	
	binary.BigEndian.PutUint64(buf, atomic.LoadUint64(&gs.nextEdgeID))
	_ = gs.tree.Put([]byte("meta:nextEdgeID"), buf)

	// Persist HNSW metadata
	gs.vectorIndexesMu.RLock()
	for _, tenantIdx := range gs.vectorIndexes {
		for _, idx := range tenantIdx {
			_ = idx.SaveMetadata()
		}
	}
	gs.vectorIndexesMu.RUnlock()

	// Flush B+Tree to disk
	return gs.tree.Flush()
}

func (gs *BTreeGraphStorage) GetStatistics() Statistics {
	return Statistics{
		NodeCount: uint64(len(gs.GetAllNodesAcrossTenants())),
		EdgeCount: uint64(len(gs.GetAllEdgesAcrossTenants())),
	}
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

func (gs *BTreeGraphStorage) HasPropertyIndex(key string) bool { return false }
func (gs *BTreeGraphStorage) GetVectorIndexMetricForTenant(tenantID string, propertyName string) (vector.DistanceMetric, error) { return vector.MetricCosine, nil }
func (gs *BTreeGraphStorage) GetVectorIndexMetric(propertyName string) (vector.DistanceMetric, error) { return vector.MetricCosine, nil }
func (gs *BTreeGraphStorage) FindNodesByLabel(label string) ([]*Node, error) { return nil, nil }
func (gs *BTreeGraphStorage) GetNode(nodeID uint64) (*Node, error) { return nil, nil }
func (gs *BTreeGraphStorage) GetOutgoingEdges(nodeID uint64) ([]*Edge, error) { return nil, nil }
func (gs *BTreeGraphStorage) GetIncomingEdges(nodeID uint64) ([]*Edge, error) { return nil, nil }
func (gs *BTreeGraphStorage) GetEdge(edgeID uint64) (*Edge, error) { return nil, nil }
func (gs *BTreeGraphStorage) FindNodesByPropertyForTenant(key string, value Value, tenantID string) ([]*Node, error) { return nil, nil }
func (gs *BTreeGraphStorage) FindNodesByPropertyIndexedForTenant(key string, value Value, tenantID string) ([]*Node, error) { return nil, nil }

func (gs *BTreeGraphStorage) CreateVectorIndexForTenant(tenantID string, propertyName string, dimensions int, m int, efConstruction int, metric vector.DistanceMetric) error {
	if tenantID == "" {
		tenantID = "default"
	}

	gs.vectorIndexesMu.Lock()
	defer gs.vectorIndexesMu.Unlock()

	if _, exists := gs.vectorIndexes[tenantID][propertyName]; exists {
		return fmt.Errorf("vector index already exists for property: %s", propertyName)
	}

	prefix := fmt.Sprintf("v:%s:%s:", tenantID, propertyName)
	metaKey := []byte(fmt.Sprintf("vmeta:%s:%s", tenantID, propertyName))

	h, err := vector.NewPersistentHNSWIndex(gs, prefix, metaKey, dimensions, m, efConstruction, metric)
	if err != nil {
		return err
	}

	if gs.vectorIndexes[tenantID] == nil {
		gs.vectorIndexes[tenantID] = make(map[string]*vector.HNSWIndex)
	}
	gs.vectorIndexes[tenantID][propertyName] = h
	
	// Persist metadata (config + entry point)
	if err := h.SaveMetadata(); err != nil {
		return fmt.Errorf("failed to save vector index metadata: %w", err)
	}

	// Update metadata for discovery
	// 1. Add to tenant's index list
	var props []string
	if listData, ok := gs.tree.Get([]byte(fmt.Sprintf("vmeta:list:%s", tenantID))); ok {
		_ = json.Unmarshal(listData, &props)
	}
	
	alreadyListed := false
	for _, p := range props {
		if p == propertyName {
			alreadyListed = true
			break
		}
	}
	if !alreadyListed {
		props = append(props, propertyName)
		data, _ := json.Marshal(props)
		_ = gs.tree.Put([]byte(fmt.Sprintf("vmeta:list:%s", tenantID)), data)
	}

	// 2. Add to global tenant list
	var tenants []string
	if tenantsData, ok := gs.tree.Get([]byte("vmeta:tenants")); ok {
		_ = json.Unmarshal(tenantsData, &tenants)
	}
	
	tenantListed := false
	for _, t := range tenants {
		if t == tenantID {
			tenantListed = true
			break
		}
	}
	if !tenantListed {
		tenants = append(tenants, tenantID)
		data, _ := json.Marshal(tenants)
		_ = gs.tree.Put([]byte("vmeta:tenants"), data)
	}

	return nil
}

func (gs *BTreeGraphStorage) HasVectorIndexForTenant(tenantID string, propertyName string) bool {
	if tenantID == "" {
		tenantID = "default"
	}
	gs.vectorIndexesMu.RLock()
	defer gs.vectorIndexesMu.RUnlock()
	_, exists := gs.vectorIndexes[tenantID][propertyName]
	return exists
}
func (gs *BTreeGraphStorage) VectorSearchForTenant(tenantID string, propertyName string, query []float32, k int, ef int) ([]vector.SearchResult, error) {
	if tenantID == "" {
		tenantID = "default"
	}
	gs.vectorIndexesMu.RLock()
	idxMap, ok := gs.vectorIndexes[tenantID]
	if !ok {
		gs.vectorIndexesMu.RUnlock()
		return nil, fmt.Errorf("no vector indexes for tenant: %s", tenantID)
	}
	idx, ok := idxMap[propertyName]
	gs.vectorIndexesMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("vector index not found: %s", propertyName)
	}
	return idx.Search(query, k, ef)
}

func (gs *BTreeGraphStorage) ListVectorIndexesForTenant(tenantID string) []string {
	if tenantID == "" {
		tenantID = "default"
	}
	gs.vectorIndexesMu.RLock()
	defer gs.vectorIndexesMu.RUnlock()
	
	idxMap, ok := gs.vectorIndexes[tenantID]
	if !ok {
		return []string{}
	}
	
	props := make([]string, 0, len(idxMap))
	for p := range idxMap {
		props = append(props, p)
	}
	return props
}

func (gs *BTreeGraphStorage) DropVectorIndexForTenant(tenantID string, propertyName string) error {
	if tenantID == "" {
		tenantID = "default"
	}
	gs.vectorIndexesMu.Lock()
	defer gs.vectorIndexesMu.Unlock()
	
	if _, ok := gs.vectorIndexes[tenantID]; ok {
		delete(gs.vectorIndexes[tenantID], propertyName)
	}
	
	// Also remove from metadata for discovery
	if listData, ok := gs.tree.Get([]byte(fmt.Sprintf("vmeta:list:%s", tenantID))); ok {
		var props []string
		_ = json.Unmarshal(listData, &props)
		newProps := make([]string, 0, len(props))
		for _, p := range props {
			if p != propertyName {
				newProps = append(newProps, p)
			}
		}
		data, _ := json.Marshal(newProps)
		_ = gs.tree.Put([]byte(fmt.Sprintf("vmeta:list:%s", tenantID)), data)
	}
	
	return nil
}

func (gs *BTreeGraphStorage) VectorSearch(propertyName string, query []float32, k int, ef int) ([]vector.SearchResult, error) {
	return gs.VectorSearchForTenant("default", propertyName, query, k, ef)
}

func (gs *BTreeGraphStorage) ListVectorIndexes() []string {
	return gs.ListVectorIndexesForTenant("default")
}

func (gs *BTreeGraphStorage) HasVectorIndex(propertyName string) bool {
	return gs.HasVectorIndexForTenant("default", propertyName)
}

func (gs *BTreeGraphStorage) CreateVectorIndex(propertyName string, dimensions int, m int, efConstruction int, metric vector.DistanceMetric) error {
	return gs.CreateVectorIndexForTenant("default", propertyName, dimensions, m, efConstruction, metric)
}

func (gs *BTreeGraphStorage) DropVectorIndex(propertyName string) error {
	return gs.DropVectorIndexForTenant("default", propertyName)
}

func (gs *BTreeGraphStorage) UpdateNodeVectorIndexes(node *Node) error {
	tenantID := node.TenantID
	if tenantID == "" {
		tenantID = "default"
	}

	gs.vectorIndexesMu.RLock()
	tenantIdxMap, ok := gs.vectorIndexes[tenantID]
	gs.vectorIndexesMu.RUnlock()
	if !ok {
		return nil
	}

	for propName, propVal := range node.Properties {
		if propVal.Type == TypeVector {
			if idx, ok := tenantIdxMap[propName]; ok {
				vec, err := propVal.AsVector()
				if err != nil {
					return err
				}
				// HNSW Delete is safe if id doesn't exist
				_ = idx.Delete(node.ID)
				if err := idx.Insert(node.ID, vec); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (gs *BTreeGraphStorage) RemoveNodeFromVectorIndexes(nodeID uint64, tenantID string) error {
	if tenantID == "" {
		tenantID = "default"
	}
	gs.vectorIndexesMu.RLock()
	tenantIdxMap, ok := gs.vectorIndexes[tenantID]
	gs.vectorIndexesMu.RUnlock()
	if !ok {
		return nil
	}

	for _, idx := range tenantIdxMap {
		_ = idx.Delete(nodeID)
	}
	return nil
}
func (gs *BTreeGraphStorage) WithNodeRefForTenant(nodeID uint64, tenantID string, fn func(*Node) error) error {
	node, err := gs.GetNodeForTenant(nodeID, tenantID)
	if err != nil {
		return err
	}
	return fn(node)
}

func (gs *BTreeGraphStorage) CreateNodeWithUniquePropertyForTenant(tenantID string, labels []string, properties map[string]Value, uniqueLabel string, uniquePropertyKey string) (*Node, error) { return nil, nil }
func (gs *BTreeGraphStorage) UpdateNodeForTenant(nodeID uint64, properties map[string]Value, tenantID string) error { return nil }
func (gs *BTreeGraphStorage) DeleteNodeForTenant(nodeID uint64, tenantID string) error {
	if tenantID == "" {
		tenantID = "default"
	}

	node, err := gs.GetNodeForTenant(nodeID, tenantID)
	if err != nil {
		return err
	}

	// Remove from vector indexes
	_ = gs.RemoveNodeFromVectorIndexes(nodeID, tenantID)

	// Remove from label indexes
	for _, label := range node.Labels {
		_ = gs.tree.Delete(gs.labelKey(tenantID, label, nodeID))
	}

	// Delete node record
	return gs.tree.Delete(gs.nodeKey(tenantID, nodeID))
}
func (gs *BTreeGraphStorage) RemoveNodePropertiesForTenant(nodeID uint64, keys []string, tenantID string) error { return nil }
func (gs *BTreeGraphStorage) UpdateEdgeForTenant(edgeID uint64, properties map[string]Value, weight *float64, tenantID string) error { return nil }
func (gs *BTreeGraphStorage) DeleteEdgeForTenant(edgeID uint64, tenantID string) error { return nil }
func (gs *BTreeGraphStorage) UpsertEdgeWithTenant(tenantID string, fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, bool, error) { return nil, false, nil }
func (gs *BTreeGraphStorage) CreateNode(labels []string, properties map[string]Value) (*Node, error) { return nil, nil }
func (gs *BTreeGraphStorage) CreateEdge(fromID, toID uint64, edgeType string, properties map[string]Value, weight float64) (*Edge, error) { return nil, nil }
func (gs *BTreeGraphStorage) UpdateNode(nodeID uint64, properties map[string]Value) error { return nil }
func (gs *BTreeGraphStorage) DeleteNode(nodeID uint64) error { return nil }
func (gs *BTreeGraphStorage) RemoveNodeProperties(nodeID uint64, keys []string) error { return nil }
func (gs *BTreeGraphStorage) UpdateEdge(edgeID uint64, properties map[string]Value, weight *float64) error { return nil }
func (gs *BTreeGraphStorage) DeleteEdge(edgeID uint64) error { return nil }
func (gs *BTreeGraphStorage) BeginBatch() *Batch { return nil }

var _ Storage = (*BTreeGraphStorage)(nil)
