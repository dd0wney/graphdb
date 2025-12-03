package licensing

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNewStore(t *testing.T) {
	tests := []struct {
		name    string
		dataDir string
		wantErr bool
		setup   func(string)
		cleanup func(string)
	}{
		{
			name:    "Create new store in non-existent directory",
			dataDir: filepath.Join(os.TempDir(), "test-store-new"),
			wantErr: false,
		},
		{
			name:    "Create store in existing directory",
			dataDir: filepath.Join(os.TempDir(), "test-store-existing"),
			wantErr: false,
			setup: func(dir string) {
				os.MkdirAll(dir, 0755)
			},
		},
		{
			name:    "Load existing licenses",
			dataDir: filepath.Join(os.TempDir(), "test-store-with-data"),
			wantErr: false,
			setup: func(dir string) {
				os.MkdirAll(dir, 0755)
				// Create a test license file
				licenses := map[string]*License{
					"test-id-1": {
						ID:    "test-id-1",
						Key:   "CGDB-TEST-KEY1-1111-1111",
						Email: "test@example.com",
						Type:  LicenseTypeProfessional,
					},
				}
				data, _ := json.MarshalIndent(licenses, "", "  ")
				os.WriteFile(filepath.Join(dir, "licenses.json"), data, 0600)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			if tt.setup != nil {
				tt.setup(tt.dataDir)
			}

			// Cleanup after test
			defer func() {
				os.RemoveAll(tt.dataDir)
				if tt.cleanup != nil {
					tt.cleanup(tt.dataDir)
				}
			}()

			store, err := NewStore(tt.dataDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewStore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && store == nil {
				t.Error("NewStore() returned nil store")
			}

			// Verify store initialized correctly
			if store != nil {
				if store.dataDir != tt.dataDir {
					t.Errorf("Store dataDir = %v, want %v", store.dataDir, tt.dataDir)
				}
				if store.licenses == nil {
					t.Error("Store licenses map is nil")
				}
				if store.byKey == nil {
					t.Error("Store byKey map is nil")
				}
			}
		})
	}
}

func TestStore_SaveLoad(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "test-store-save-load")
	defer os.RemoveAll(dataDir)

	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Create test licenses
	license1 := &License{
		ID:         "id-1",
		Key:        "CGDB-TEST-KEY1-1111-1111",
		Email:      "test1@example.com",
		Type:       LicenseTypeProfessional,
		CustomerID: "cust-1",
		Status:     "active",
		CreatedAt:  time.Now(),
	}

	license2 := &License{
		ID:         "id-2",
		Key:        "CGDB-TEST-KEY2-2222-2222",
		Email:      "test2@example.com",
		Type:       LicenseTypeEnterprise,
		CustomerID: "cust-2",
		Status:     "active",
		CreatedAt:  time.Now(),
	}

	// Create licenses
	if err := store.CreateLicense(context.Background(), license1); err != nil {
		t.Fatalf("CreateLicense() error = %v", err)
	}
	if err := store.CreateLicense(context.Background(), license2); err != nil {
		t.Fatalf("CreateLicense() error = %v", err)
	}

	// Verify file was created
	filePath := filepath.Join(dataDir, "licenses.json")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("licenses.json was not created")
	}

	// Create new store to test loading
	store2, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Verify licenses were loaded
	loaded1, err := store2.GetLicense(context.Background(), "id-1")
	if err != nil {
		t.Errorf("GetLicense(id-1) error = %v", err)
	}
	if loaded1.Key != license1.Key {
		t.Errorf("Loaded license key = %v, want %v", loaded1.Key, license1.Key)
	}

	loaded2, err := store2.GetLicense(context.Background(), "id-2")
	if err != nil {
		t.Errorf("GetLicense(id-2) error = %v", err)
	}
	if loaded2.Email != license2.Email {
		t.Errorf("Loaded license email = %v, want %v", loaded2.Email, license2.Email)
	}

	// Verify byKey index was rebuilt
	loadedByKey, err := store2.GetLicenseByKey(context.Background(), "CGDB-TEST-KEY1-1111-1111")
	if err != nil {
		t.Errorf("GetLicenseByKey() error = %v", err)
	}
	if loadedByKey.ID != "id-1" {
		t.Errorf("Loaded license ID = %v, want id-1", loadedByKey.ID)
	}
}

func TestStore_LoadCorruptedJSON(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "test-store-corrupted")
	defer os.RemoveAll(dataDir)

	// Create directory and corrupted JSON file
	os.MkdirAll(dataDir, 0755)
	corruptedData := []byte(`{"invalid": json syntax}`)
	os.WriteFile(filepath.Join(dataDir, "licenses.json"), corruptedData, 0600)

	// Attempt to create store with corrupted data
	_, err := NewStore(dataDir)
	if err == nil {
		t.Error("NewStore() expected error with corrupted JSON, got nil")
	}
}

func TestStore_LoadNonExistentFile(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "test-store-empty")
	defer os.RemoveAll(dataDir)

	// Create store in empty directory (no licenses.json)
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore() with non-existent file should succeed, got error = %v", err)
	}

	// Verify store is empty
	licenses, _ := store.ListLicenses(context.Background())
	if len(licenses) != 0 {
		t.Errorf("New store should have 0 licenses, got %d", len(licenses))
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "test-store-concurrent")
	defer os.RemoveAll(dataDir)

	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Concurrent writes
	var wg sync.WaitGroup
	numGoroutines := 50
	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			license := &License{
				ID:    string(rune('A' + id)),
				Key:   generateTestKey(id),
				Email: "test@example.com",
				Type:  LicenseTypeProfessional,
			}
			if err := store.CreateLicense(context.Background(), license); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent CreateLicense() error = %v", err)
	}

	// Verify all licenses were created
	licenses, _ := store.ListLicenses(context.Background())
	if len(licenses) != numGoroutines {
		t.Errorf("Expected %d licenses, got %d", numGoroutines, len(licenses))
	}
}

func TestStore_ConcurrentReadWrite(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "test-store-rw-concurrent")
	defer os.RemoveAll(dataDir)

	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Create initial license
	initialLicense := &License{
		ID:    "test-id",
		Key:   "CGDB-TEST-KEY0-0000-0000",
		Email: "test@example.com",
		Type:  LicenseTypeProfessional,
	}
	store.CreateLicense(context.Background(), initialLicense)

	var wg sync.WaitGroup
	readErrors := make(chan error, 100)
	writeErrors := make(chan error, 50)

	// Concurrent readers
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.GetLicense(context.Background(), "test-id")
			if err != nil {
				readErrors <- err
			}
		}()
	}

	// Concurrent writers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			license := &License{
				ID:    string(rune('A' + id)),
				Key:   generateTestKey(id),
				Email: "test@example.com",
				Type:  LicenseTypeEnterprise,
			}
			if err := store.CreateLicense(context.Background(), license); err != nil {
				writeErrors <- err
			}
		}(i)
	}

	wg.Wait()
	close(readErrors)
	close(writeErrors)

	// Check for errors
	for err := range readErrors {
		t.Errorf("Concurrent read error = %v", err)
	}
	for err := range writeErrors {
		t.Errorf("Concurrent write error = %v", err)
	}
}

func TestStore_UpdateNonExistentLicense(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "test-store-update-nonexistent")
	defer os.RemoveAll(dataDir)

	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	license := &License{
		ID:    "nonexistent-id",
		Key:   "CGDB-TEST-KEY0-0000-0000",
		Email: "test@example.com",
		Type:  LicenseTypeProfessional,
	}

	err = store.UpdateLicense(context.Background(), license)
	if err == nil {
		t.Error("UpdateLicense() expected error for nonexistent license, got nil")
	}
}

func TestStore_GetNonExistentLicense(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "test-store-get-nonexistent")
	defer os.RemoveAll(dataDir)

	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_, err = store.GetLicense(context.Background(), "nonexistent-id")
	if err == nil {
		t.Error("GetLicense() expected error for nonexistent license, got nil")
	}
}

func TestStore_GetLicenseByKeyNotFound(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "test-store-getbykey-notfound")
	defer os.RemoveAll(dataDir)

	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	_, err = store.GetLicenseByKey(context.Background(), "CGDB-INVALID-KEY0-0000-0000")
	if err == nil {
		t.Error("GetLicenseByKey() expected error for nonexistent key, got nil")
	}
}

func TestStore_GetLicenseByCustomer(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "test-store-get-by-customer")
	defer os.RemoveAll(dataDir)

	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	license := &License{
		ID:         "test-id",
		Key:        "CGDB-TEST-KEY0-0000-0000",
		Email:      "test@example.com",
		Type:       LicenseTypeProfessional,
		CustomerID: "cust-123",
	}
	store.CreateLicense(context.Background(), license)

	// Test finding by customer
	found, err := store.GetLicenseByCustomer(context.Background(), "cust-123")
	if err != nil {
		t.Errorf("GetLicenseByCustomer() error = %v", err)
	}
	if found.ID != "test-id" {
		t.Errorf("GetLicenseByCustomer() ID = %v, want test-id", found.ID)
	}

	// Test customer not found
	_, err = store.GetLicenseByCustomer(context.Background(), "nonexistent-customer")
	if err == nil {
		t.Error("GetLicenseByCustomer() expected error for nonexistent customer, got nil")
	}
}

func TestStore_ListLicenses(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "test-store-list")
	defer os.RemoveAll(dataDir)

	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Empty store
	licenses, _ := store.ListLicenses(context.Background())
	if len(licenses) != 0 {
		t.Errorf("ListLicenses() on empty store = %d, want 0", len(licenses))
	}

	// Add licenses
	for i := 0; i < 5; i++ {
		license := &License{
			ID:    string(rune('A' + i)),
			Key:   generateTestKey(i),
			Email: "test@example.com",
			Type:  LicenseTypeProfessional,
		}
		store.CreateLicense(context.Background(), license)
	}

	// List licenses
	licenses, _ = store.ListLicenses(context.Background())
	if len(licenses) != 5 {
		t.Errorf("ListLicenses() = %d, want 5", len(licenses))
	}
}

func TestStore_Ping(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "test-store-ping")
	defer os.RemoveAll(dataDir)

	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Ping should always succeed for file-based store
	if err := store.Ping(context.Background()); err != nil {
		t.Errorf("Ping() error = %v, want nil", err)
	}
}

func TestStore_Close(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "test-store-close")
	defer os.RemoveAll(dataDir)

	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	// Close should always succeed for file-based store
	if err := store.Close(); err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

func TestStore_FilePermissions(t *testing.T) {
	dataDir := filepath.Join(os.TempDir(), "test-store-permissions")
	defer os.RemoveAll(dataDir)

	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	license := &License{
		ID:    "test-id",
		Key:   "CGDB-TEST-KEY0-0000-0000",
		Email: "test@example.com",
		Type:  LicenseTypeProfessional,
	}
	store.CreateLicense(context.Background(), license)

	// Check file permissions (should be 0600 for security)
	filePath := filepath.Join(dataDir, "licenses.json")
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	mode := info.Mode()
	expectedMode := os.FileMode(0600)
	if mode.Perm() != expectedMode {
		t.Errorf("File permissions = %v, want %v", mode.Perm(), expectedMode)
	}
}

// Helper function to generate test keys
func generateTestKey(id int) string {
	return "CGDB-TEST-" + string(rune('A'+id)) + "000-0000-0000"
}

// Benchmark store operations
func BenchmarkStore_CreateLicense(b *testing.B) {
	dataDir := filepath.Join(os.TempDir(), "bench-store-create")
	defer os.RemoveAll(dataDir)

	store, _ := NewStore(dataDir)
	license := &License{
		ID:    "bench-id",
		Key:   "CGDB-BENCH-KEY0-0000-0000",
		Email: "bench@example.com",
		Type:  LicenseTypeProfessional,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		license.ID = string(rune('A' + (i % 26)))
		store.CreateLicense(context.Background(), license)
	}
}

func BenchmarkStore_GetLicense(b *testing.B) {
	dataDir := filepath.Join(os.TempDir(), "bench-store-get")
	defer os.RemoveAll(dataDir)

	store, _ := NewStore(dataDir)
	license := &License{
		ID:    "bench-id",
		Key:   "CGDB-BENCH-KEY0-0000-0000",
		Email: "bench@example.com",
		Type:  LicenseTypeProfessional,
	}
	store.CreateLicense(context.Background(), license)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.GetLicense(context.Background(), "bench-id")
	}
}
