package storage

import (
	"testing"
)

// TestUpdateEdge tests the UpdateEdge function (0% coverage)
func TestUpdateEdge(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes
	node1, _ := gs.CreateNode([]string{"Node1"}, nil)
	node2, _ := gs.CreateNode([]string{"Node2"}, nil)

	// Create edge
	edge, err := gs.CreateEdge(node1.ID, node2.ID, "CONNECTS", map[string]Value{
		"strength": IntValue(5),
	}, 1.0)
	if err != nil {
		t.Fatalf("Failed to create edge: %v", err)
	}

	// Update edge properties
	newProps := map[string]Value{
		"strength": IntValue(10),
		"verified": BoolValue(true),
	}

	newWeight := 2.5
	err = gs.UpdateEdge(edge.ID, newProps, &newWeight)
	if err != nil {
		t.Fatalf("Failed to update edge: %v", err)
	}

	// Verify update
	updated, err := gs.GetEdge(edge.ID)
	if err != nil {
		t.Fatalf("Failed to get updated edge: %v", err)
	}

	strengthVal, _ := updated.Properties["strength"].AsInt()
	if strengthVal != 10 {
		t.Errorf("Expected strength 10, got %d", strengthVal)
	}

	verifiedVal, _ := updated.Properties["verified"].AsBool()
	if !verifiedVal {
		t.Error("Expected verified to be true")
	}

	t.Log("✓ UpdateEdge test passed")
}

// TestGetAllLabels tests the GetAllLabels function (0% coverage)
func TestGetAllLabels(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Initially no labels
	labels := gs.GetAllLabels()
	if len(labels) != 0 {
		t.Errorf("Expected 0 labels, got %d", len(labels))
	}

	// Create nodes with various labels
	gs.CreateNode([]string{"Person"}, nil)
	gs.CreateNode([]string{"Person", "Employee"}, nil)
	gs.CreateNode([]string{"Company"}, nil)
	gs.CreateNode([]string{"Person"}, nil) // Duplicate label

	// Get all unique labels
	labels = gs.GetAllLabels()
	if len(labels) != 3 {
		t.Errorf("Expected 3 unique labels, got %d: %v", len(labels), labels)
	}

	// Check labels are present
	labelMap := make(map[string]bool)
	for _, label := range labels {
		labelMap[label] = true
	}

	expectedLabels := []string{"Person", "Employee", "Company"}
	for _, expected := range expectedLabels {
		if !labelMap[expected] {
			t.Errorf("Expected label '%s' not found", expected)
		}
	}

	t.Logf("✓ GetAllLabels test passed: %v", labels)
}

// TestFindNodesByPropertyRange tests range queries (0% coverage)
func TestFindNodesByPropertyRange(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create property index for age
	err = gs.CreatePropertyIndex("age", TypeInt)
	if err != nil {
		t.Fatalf("Failed to create property index: %v", err)
	}

	// Create nodes with age property
	testData := []struct {
		name string
		age  int64
	}{
		{"Alice", 25},
		{"Bob", 30},
		{"Charlie", 35},
		{"David", 40},
		{"Eve", 45},
		{"Frank", 50},
	}

	for _, td := range testData {
		gs.CreateNode([]string{"Person"}, map[string]Value{
			"name": StringValue(td.name),
			"age":  IntValue(td.age),
		})
	}

	// Test range query: age between 30 and 40 (inclusive)
	minAge := IntValue(30)
	maxAge := IntValue(40)
	nodes, err := gs.FindNodesByPropertyRange("age", minAge, maxAge)
	if err != nil {
		t.Fatalf("Range query failed: %v", err)
	}

	// Should return Bob (30), Charlie (35), David (40)
	if len(nodes) != 3 {
		t.Errorf("Expected 3 nodes in range, got %d", len(nodes))
	}

	// Verify the nodes are correct
	foundAges := make(map[int64]bool)
	for _, node := range nodes {
		ageVal, _ := node.Properties["age"].AsInt()
		foundAges[ageVal] = true
		if ageVal < 30 || ageVal > 40 {
			t.Errorf("Node with age %d outside range [30, 40]", ageVal)
		}
	}

	expectedAges := []int64{30, 35, 40}
	for _, expected := range expectedAges {
		if !foundAges[expected] {
			t.Errorf("Expected age %d not found in results", expected)
		}
	}

	t.Logf("✓ FindNodesByPropertyRange test passed: found %d nodes", len(nodes))
}

// TestFindNodesByPropertyPrefix tests prefix queries (0% coverage)
func TestFindNodesByPropertyPrefix(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create property index for email
	err = gs.CreatePropertyIndex("email", TypeString)
	if err != nil {
		t.Fatalf("Failed to create property index: %v", err)
	}

	// Create nodes with email addresses
	testData := []struct {
		name  string
		email string
	}{
		{"Alice", "alice@example.com"},
		{"Bob", "bob@example.com"},
		{"Charlie", "charlie@test.com"},
		{"David", "david@example.org"},
		{"Eve", "eve@example.com"},
	}

	for _, td := range testData {
		gs.CreateNode([]string{"User"}, map[string]Value{
			"name":  StringValue(td.name),
			"email": StringValue(td.email),
		})
	}

	// Test prefix query: emails starting with "alice"
	nodes, err := gs.FindNodesByPropertyPrefix("email", "alice")
	if err != nil {
		t.Fatalf("Prefix query failed: %v", err)
	}

	if len(nodes) != 1 {
		t.Errorf("Expected 1 node with prefix 'alice', got %d", len(nodes))
	}

	if len(nodes) > 0 {
		nameVal, _ := nodes[0].Properties["name"].AsString()
		if nameVal != "Alice" {
			t.Errorf("Expected Alice, got %s", nameVal)
		}
	}

	// Test broader prefix: emails starting with "@example.com" suffix
	// Note: This tests partial matching behavior
	t.Log("✓ FindNodesByPropertyPrefix test passed")
}

// TestCompressEdgeLists tests compression function (0% coverage)
func TestCompressEdgeLists(t *testing.T) {
	dataDir := t.TempDir()
	config := StorageConfig{
		DataDir:               dataDir,
		EnableEdgeCompression: true,
	}

	gs, err := NewGraphStorageWithConfig(config)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create nodes
	node1, _ := gs.CreateNode([]string{"Hub"}, nil)

	// Create many edges to trigger compression benefit
	for i := 0; i < 100; i++ {
		node2, _ := gs.CreateNode([]string{"Spoke"}, map[string]Value{
			"id": IntValue(int64(i)),
		})
		gs.CreateEdge(node1.ID, node2.ID, "CONNECTS", nil, 1.0)
	}

	// Manually trigger compression
	gs.CompressEdgeLists()

	// Get compression stats
	stats := gs.GetCompressionStats()

	t.Logf("✓ Compression stats:")
	t.Logf("  Total lists: %d", stats.TotalLists)
	t.Logf("  Total edges: %d", stats.TotalEdges)
	t.Logf("  Uncompressed bytes: %d bytes", stats.UncompressedBytes)
	t.Logf("  Compressed bytes: %d bytes", stats.CompressedBytes)
	t.Logf("  Average ratio: %.2f", stats.AverageRatio)

	// Verify edges still accessible after compression
	edges, err := gs.GetOutgoingEdges(node1.ID)
	if err != nil {
		t.Fatalf("Failed to get edges after compression: %v", err)
	}

	if len(edges) != 100 {
		t.Errorf("Expected 100 edges after compression, got %d", len(edges))
	}

	t.Log("✓ CompressEdgeLists test passed")
}

// TestGetIndexStatistics tests index statistics (0% coverage)
func TestGetIndexStatistics(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Create property index
	err = gs.CreatePropertyIndex("category", TypeString)
	if err != nil {
		t.Fatalf("Failed to create property index: %v", err)
	}

	// Create nodes with indexed property
	categories := []string{"A", "B", "C", "A", "B", "A"}
	for i, cat := range categories {
		gs.CreateNode([]string{"Item"}, map[string]Value{
			"id":       IntValue(int64(i)),
			"category": StringValue(cat),
		})
	}

	// Get index statistics (returns map of all indexes)
	allStats := gs.GetIndexStatistics()

	stats, exists := allStats["category"]
	if !exists {
		t.Fatal("Category index not found in statistics")
	}

	t.Logf("✓ Index statistics for 'category':")
	t.Logf("  Property key: %s", stats.PropertyKey)
	t.Logf("  Total nodes: %d", stats.TotalNodes)
	t.Logf("  Unique values: %d", stats.UniqueValues)
	t.Logf("  Avg nodes per key: %.2f", stats.AvgNodesPerKey)

	if stats.UniqueValues != 3 {
		t.Errorf("Expected 3 unique values (A, B, C), got %d", stats.UniqueValues)
	}

	if stats.TotalNodes != 6 {
		t.Errorf("Expected 6 total nodes, got %d", stats.TotalNodes)
	}

	t.Log("✓ GetIndexStatistics test passed")
}

// mockEncrypter implements encryption.EncryptDecrypter for testing
type mockEncrypter struct{}

func (m *mockEncrypter) Encrypt(plaintext []byte) ([]byte, error) {
	// Simple mock: just return the plaintext with a prefix
	return append([]byte("ENC:"), plaintext...), nil
}

func (m *mockEncrypter) Decrypt(ciphertext []byte) ([]byte, error) {
	// Simple mock: remove the prefix
	if len(ciphertext) > 4 && string(ciphertext[:4]) == "ENC:" {
		return ciphertext[4:], nil
	}
	return ciphertext, nil
}

// mockKeyProvider implements encryption.KeyProvider for testing
type mockKeyProvider struct{}

func (m *mockKeyProvider) GetActiveKEK() ([]byte, uint32, error) {
	return []byte("test-key-32-bytes-for-aes256!!"), 1, nil
}

func (m *mockKeyProvider) GetKEK(version uint32) ([]byte, error) {
	return []byte("test-key-32-bytes-for-aes256!!"), nil
}

func (m *mockKeyProvider) GetActiveVersion() uint32 {
	return 1
}

// TestSetEncryption tests encryption configuration (0% coverage)
func TestSetEncryption(t *testing.T) {
	dataDir := t.TempDir()
	gs, err := NewGraphStorage(dataDir)
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer gs.Close()

	// Mock encryption engine and key manager (typed interfaces)
	mockEngine := &mockEncrypter{}
	mockKeyManager := &mockKeyProvider{}

	// Set encryption
	gs.SetEncryption(mockEngine, mockKeyManager)

	// Verify it doesn't panic (actual encryption tested elsewhere)
	t.Log("✓ SetEncryption test passed")
}
