package tenant

import (
	"context"
	"errors"
	"testing"
)

func TestNewTenantStore(t *testing.T) {
	store := NewTenantStore()

	// Should have default tenant
	tenant, err := store.Get(DefaultTenantID)
	if err != nil {
		t.Fatalf("Failed to get default tenant: %v", err)
	}

	if tenant.ID != DefaultTenantID {
		t.Errorf("Expected default tenant ID %s, got %s", DefaultTenantID, tenant.ID)
	}

	if tenant.Status != TenantStatusActive {
		t.Errorf("Expected default tenant status %s, got %s", TenantStatusActive, tenant.Status)
	}

	// Default tenant should have unlimited quota (nil)
	if tenant.Quota != nil {
		t.Error("Expected default tenant to have nil quota (unlimited)")
	}
}

func TestTenantStore_Create(t *testing.T) {
	tests := []struct {
		name    string
		tenant  *Tenant
		wantErr error
	}{
		{
			name: "Valid tenant",
			tenant: &Tenant{
				ID:   "tenant-1",
				Name: "Test Tenant",
			},
			wantErr: nil,
		},
		{
			name: "Tenant with quota",
			tenant: &Tenant{
				ID:   "tenant-2",
				Name: "Quota Tenant",
				Quota: &TenantQuota{
					MaxNodes:       1000,
					MaxEdges:       5000,
					MaxStorageBytes: 1073741824, // 1GB
				},
			},
			wantErr: nil,
		},
		{
			name: "ID too short",
			tenant: &Tenant{
				ID:   "ab",
				Name: "Short ID Tenant",
			},
			wantErr: ErrInvalidTenantID,
		},
		{
			name: "ID too long",
			tenant: &Tenant{
				ID:   "this-tenant-id-is-way-too-long-and-should-fail-validation-because-it-exceeds-the-maximum-length",
				Name: "Long ID Tenant",
			},
			wantErr: ErrInvalidTenantID,
		},
		{
			name: "Name too short",
			tenant: &Tenant{
				ID:   "tenant-short-name",
				Name: "AB",
			},
			wantErr: ErrInvalidTenantName,
		},
		{
			name: "Invalid ID characters",
			tenant: &Tenant{
				ID:   "tenant@invalid",
				Name: "Invalid ID Tenant",
			},
			wantErr: ErrInvalidTenantID,
		},
		{
			name: "ID starting with hyphen",
			tenant: &Tenant{
				ID:   "-tenant",
				Name: "Invalid Start Tenant",
			},
			wantErr: ErrInvalidTenantID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewTenantStore()
			err := store.Create(tt.tenant)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("Expected error %v, got nil", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify tenant was created
			created, err := store.Get(tt.tenant.ID)
			if err != nil {
				t.Errorf("Failed to get created tenant: %v", err)
				return
			}

			if created.ID != tt.tenant.ID {
				t.Errorf("Expected ID %s, got %s", tt.tenant.ID, created.ID)
			}

			if created.Status != TenantStatusActive {
				t.Errorf("Expected status %s, got %s", TenantStatusActive, created.Status)
			}

			if created.CreatedAt == 0 {
				t.Error("Expected CreatedAt to be set")
			}
		})
	}
}

func TestTenantStore_Create_Duplicate(t *testing.T) {
	store := NewTenantStore()

	tenant := &Tenant{
		ID:   "tenant-dup",
		Name: "First Tenant",
	}

	err := store.Create(tenant)
	if err != nil {
		t.Fatalf("Failed to create first tenant: %v", err)
	}

	// Try to create duplicate
	duplicate := &Tenant{
		ID:   "tenant-dup",
		Name: "Duplicate Tenant",
	}

	err = store.Create(duplicate)
	if err == nil {
		t.Error("Expected error for duplicate tenant, got nil")
	}
	if !errors.Is(err, ErrTenantExists) {
		t.Errorf("Expected ErrTenantExists, got %v", err)
	}
}

func TestTenantStore_Get(t *testing.T) {
	store := NewTenantStore()

	tenant := &Tenant{
		ID:   "tenant-get",
		Name: "Get Test Tenant",
	}
	store.Create(tenant)

	tests := []struct {
		name     string
		tenantID string
		wantErr  error
	}{
		{
			name:     "Existing tenant",
			tenantID: "tenant-get",
			wantErr:  nil,
		},
		{
			name:     "Default tenant",
			tenantID: DefaultTenantID,
			wantErr:  nil,
		},
		{
			name:     "Non-existent tenant",
			tenantID: "nonexistent",
			wantErr:  ErrTenantNotFound,
		},
		{
			name:     "Empty tenant ID",
			tenantID: "",
			wantErr:  ErrInvalidTenantID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := store.Get(tt.tenantID)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("Expected error %v, got nil", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result.ID != tt.tenantID {
				t.Errorf("Expected ID %s, got %s", tt.tenantID, result.ID)
			}
		})
	}
}

func TestTenantStore_GetActive(t *testing.T) {
	store := NewTenantStore()

	// Create active tenant
	active := &Tenant{ID: "active", Name: "Active Tenant"}
	store.Create(active)

	// Create and suspend tenant
	suspended := &Tenant{ID: "suspended", Name: "Suspended Tenant"}
	store.Create(suspended)
	store.Suspend("suspended")

	// Create and delete tenant
	deleted := &Tenant{ID: "deleted", Name: "Deleted Tenant"}
	store.Create(deleted)
	store.Delete("deleted")

	tests := []struct {
		name     string
		tenantID string
		wantErr  error
	}{
		{
			name:     "Active tenant",
			tenantID: "active",
			wantErr:  nil,
		},
		{
			name:     "Suspended tenant",
			tenantID: "suspended",
			wantErr:  ErrTenantSuspended,
		},
		{
			name:     "Deleted tenant",
			tenantID: "deleted",
			wantErr:  ErrTenantDeleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := store.GetActive(tt.tenantID)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("Expected error %v, got nil", tt.wantErr)
					return
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("Expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

func TestTenantStore_Update(t *testing.T) {
	store := NewTenantStore()

	tenant := &Tenant{
		ID:   "tenant-update",
		Name: "Original Name",
	}
	store.Create(tenant)

	// Update the tenant
	updated := &Tenant{
		ID:          "tenant-update",
		Name:        "Updated Name",
		Description: "New description",
	}

	err := store.Update(updated)
	if err != nil {
		t.Fatalf("Failed to update tenant: %v", err)
	}

	// Verify update
	result, _ := store.Get("tenant-update")
	if result.Name != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got %s", result.Name)
	}
	if result.Description != "New description" {
		t.Errorf("Expected description 'New description', got %s", result.Description)
	}
}

func TestTenantStore_Delete(t *testing.T) {
	store := NewTenantStore()

	tenant := &Tenant{
		ID:   "tenant-delete",
		Name: "Delete Test Tenant",
	}
	store.Create(tenant)

	err := store.Delete("tenant-delete")
	if err != nil {
		t.Fatalf("Failed to delete tenant: %v", err)
	}

	// Tenant should still exist but be marked deleted
	result, err := store.Get("tenant-delete")
	if err != nil {
		t.Fatalf("Failed to get deleted tenant: %v", err)
	}

	if result.Status != TenantStatusDeleted {
		t.Errorf("Expected status %s, got %s", TenantStatusDeleted, result.Status)
	}

	// List should not include deleted tenants
	list := store.List()
	for _, ten := range list {
		if ten.ID == "tenant-delete" {
			t.Error("List() should not include deleted tenants")
		}
	}
}

func TestTenantStore_Delete_DefaultTenant(t *testing.T) {
	store := NewTenantStore()

	err := store.Delete(DefaultTenantID)
	if err == nil {
		t.Error("Expected error when deleting default tenant, got nil")
	}
}

func TestTenantStore_List(t *testing.T) {
	store := NewTenantStore()

	// Create some tenants
	store.Create(&Tenant{ID: "tenant-a", Name: "Tenant A"})
	store.Create(&Tenant{ID: "tenant-b", Name: "Tenant B"})
	store.Create(&Tenant{ID: "tenant-c", Name: "Tenant C"})
	store.Delete("tenant-c") // Delete one

	list := store.List()

	// Should have default + tenant-a + tenant-b = 3 (not tenant-c which is deleted)
	if len(list) != 3 {
		t.Errorf("Expected 3 tenants, got %d", len(list))
	}

	// Verify deleted tenant not in list
	for _, ten := range list {
		if ten.ID == "tenant-c" {
			t.Error("Deleted tenant should not appear in List()")
		}
	}
}

func TestTenantStore_Suspend_Activate(t *testing.T) {
	store := NewTenantStore()

	tenant := &Tenant{
		ID:   "tenant-suspend",
		Name: "Suspend Test Tenant",
	}
	store.Create(tenant)

	// Suspend
	err := store.Suspend("tenant-suspend")
	if err != nil {
		t.Fatalf("Failed to suspend tenant: %v", err)
	}

	result, _ := store.Get("tenant-suspend")
	if result.Status != TenantStatusSuspended {
		t.Errorf("Expected status %s, got %s", TenantStatusSuspended, result.Status)
	}

	// Activate
	err = store.Activate("tenant-suspend")
	if err != nil {
		t.Fatalf("Failed to activate tenant: %v", err)
	}

	result, _ = store.Get("tenant-suspend")
	if result.Status != TenantStatusActive {
		t.Errorf("Expected status %s, got %s", TenantStatusActive, result.Status)
	}
}

func TestTenantStore_Usage(t *testing.T) {
	store := NewTenantStore()

	tenant := &Tenant{
		ID:   "tenant-usage",
		Name: "Usage Test Tenant",
	}
	store.Create(tenant)

	// Increment node count
	for i := 0; i < 5; i++ {
		store.IncrementNodeCount("tenant-usage")
	}

	// Increment edge count
	for i := 0; i < 10; i++ {
		store.IncrementEdgeCount("tenant-usage")
	}

	usage, err := store.GetUsage("tenant-usage")
	if err != nil {
		t.Fatalf("Failed to get usage: %v", err)
	}

	if usage.NodeCount != 5 {
		t.Errorf("Expected node count 5, got %d", usage.NodeCount)
	}

	if usage.EdgeCount != 10 {
		t.Errorf("Expected edge count 10, got %d", usage.EdgeCount)
	}

	// Decrement
	store.DecrementNodeCount("tenant-usage")
	store.DecrementEdgeCount("tenant-usage")

	usage, _ = store.GetUsage("tenant-usage")
	if usage.NodeCount != 4 {
		t.Errorf("Expected node count 4, got %d", usage.NodeCount)
	}
	if usage.EdgeCount != 9 {
		t.Errorf("Expected edge count 9, got %d", usage.EdgeCount)
	}
}

func TestTenantStore_Quota(t *testing.T) {
	store := NewTenantStore()

	tenant := &Tenant{
		ID:   "tenant-quota",
		Name: "Quota Test Tenant",
		Quota: &TenantQuota{
			MaxNodes: 3,
			MaxEdges: 5,
		},
	}
	store.Create(tenant)

	// Should be able to create nodes up to limit
	for i := 0; i < 3; i++ {
		err := store.CheckNodeQuota("tenant-quota")
		if err != nil {
			t.Errorf("Unexpected quota error at node %d: %v", i, err)
		}
		store.IncrementNodeCount("tenant-quota")
	}

	// Should fail quota check now
	err := store.CheckNodeQuota("tenant-quota")
	if err == nil {
		t.Error("Expected quota error, got nil")
	}
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("Expected ErrQuotaExceeded, got %v", err)
	}
}

func TestTenantStore_UnlimitedQuota(t *testing.T) {
	store := NewTenantStore()

	tenant := &Tenant{
		ID:   "tenant-unlimited",
		Name: "Unlimited Tenant",
		Quota: &TenantQuota{
			MaxNodes: -1, // Unlimited
			MaxEdges: -1,
		},
	}
	store.Create(tenant)

	// Add many nodes
	for i := 0; i < 100; i++ {
		store.IncrementNodeCount("tenant-unlimited")
	}

	// Should not fail quota check
	err := store.CheckNodeQuota("tenant-unlimited")
	if err != nil {
		t.Errorf("Unexpected quota error for unlimited tenant: %v", err)
	}
}

func TestTenantStore_Exists(t *testing.T) {
	store := NewTenantStore()

	store.Create(&Tenant{ID: "exists", Name: "Exists Tenant"})

	if !store.Exists("exists") {
		t.Error("Expected tenant 'exists' to exist")
	}

	if !store.Exists(DefaultTenantID) {
		t.Error("Expected default tenant to exist")
	}

	if store.Exists("nonexistent") {
		t.Error("Expected tenant 'nonexistent' to not exist")
	}
}

func TestTenantStore_Count(t *testing.T) {
	store := NewTenantStore()

	// Should start with 1 (default tenant)
	if store.Count() != 1 {
		t.Errorf("Expected count 1, got %d", store.Count())
	}

	store.Create(&Tenant{ID: "count-1", Name: "Count 1"})
	store.Create(&Tenant{ID: "count-2", Name: "Count 2"})

	if store.Count() != 3 {
		t.Errorf("Expected count 3, got %d", store.Count())
	}

	// Delete one - count should decrease
	store.Delete("count-1")

	if store.Count() != 2 {
		t.Errorf("Expected count 2 after delete, got %d", store.Count())
	}
}

func TestTenantStore_GetTenantInfo(t *testing.T) {
	store := NewTenantStore()

	tenant := &Tenant{
		ID:   "tenant-info",
		Name: "Info Test Tenant",
		Quota: &TenantQuota{
			MaxNodes: 100,
			MaxEdges: 500,
		},
	}
	store.Create(tenant)

	// Add some usage
	for i := 0; i < 25; i++ {
		store.IncrementNodeCount("tenant-info")
	}
	for i := 0; i < 100; i++ {
		store.IncrementEdgeCount("tenant-info")
	}

	info, err := store.GetTenantInfo("tenant-info")
	if err != nil {
		t.Fatalf("Failed to get tenant info: %v", err)
	}

	if info.ID != "tenant-info" {
		t.Errorf("Expected ID 'tenant-info', got %s", info.ID)
	}

	if info.NodeCount != 25 {
		t.Errorf("Expected node count 25, got %d", info.NodeCount)
	}

	if info.EdgeCount != 100 {
		t.Errorf("Expected edge count 100, got %d", info.EdgeCount)
	}

	if info.QuotaUsage == nil {
		t.Fatal("Expected quota usage to be populated")
	}

	if info.QuotaUsage.NodesPercent != 25.0 {
		t.Errorf("Expected nodes percent 25.0, got %f", info.QuotaUsage.NodesPercent)
	}

	if info.QuotaUsage.EdgesPercent != 20.0 {
		t.Errorf("Expected edges percent 20.0, got %f", info.QuotaUsage.EdgesPercent)
	}
}

// Context tests

func TestWithTenant_FromContext(t *testing.T) {
	ctx := context.Background()

	// Add tenant to context
	ctx = WithTenant(ctx, "my-tenant")

	// Extract tenant
	tenantID, ok := FromContext(ctx)
	if !ok {
		t.Error("Expected to find tenant in context")
	}

	if tenantID != "my-tenant" {
		t.Errorf("Expected tenant 'my-tenant', got %s", tenantID)
	}
}

func TestWithTenant_EmptyDefaults(t *testing.T) {
	ctx := context.Background()

	// Empty tenant should default to DefaultTenantID
	ctx = WithTenant(ctx, "")

	tenantID, ok := FromContext(ctx)
	if !ok {
		t.Error("Expected to find tenant in context")
	}

	if tenantID != DefaultTenantID {
		t.Errorf("Expected default tenant, got %s", tenantID)
	}
}

func TestMustFromContext(t *testing.T) {
	// With tenant set
	ctx := WithTenant(context.Background(), "my-tenant")
	if MustFromContext(ctx) != "my-tenant" {
		t.Errorf("Expected 'my-tenant', got %s", MustFromContext(ctx))
	}

	// Without tenant - should return default
	ctx = context.Background()
	if MustFromContext(ctx) != DefaultTenantID {
		t.Errorf("Expected default tenant, got %s", MustFromContext(ctx))
	}
}

func TestFromContext_NilContext(t *testing.T) {
	tenantID, ok := FromContext(nil)
	if ok {
		t.Error("Expected ok to be false for nil context")
	}
	if tenantID != "" {
		t.Errorf("Expected empty string, got %s", tenantID)
	}
}

func TestIsDefaultTenant(t *testing.T) {
	tests := []struct {
		tenantID string
		expected bool
	}{
		{"", true},
		{DefaultTenantID, true},
		{"other-tenant", false},
		{"DEFAULT", false}, // Case sensitive
	}

	for _, tt := range tests {
		result := IsDefaultTenant(tt.tenantID)
		if result != tt.expected {
			t.Errorf("IsDefaultTenant(%q) = %v, expected %v", tt.tenantID, result, tt.expected)
		}
	}
}

// Types tests

func TestTenant_IsActive(t *testing.T) {
	tests := []struct {
		status   TenantStatus
		expected bool
	}{
		{TenantStatusActive, true},
		{TenantStatusSuspended, false},
		{TenantStatusDeleted, false},
	}

	for _, tt := range tests {
		tenant := &Tenant{Status: tt.status}
		if tenant.IsActive() != tt.expected {
			t.Errorf("Tenant.IsActive() with status %s = %v, expected %v", tt.status, tenant.IsActive(), tt.expected)
		}
	}
}

func TestTenantQuota_IsUnlimited(t *testing.T) {
	tests := []struct {
		name     string
		quota    *TenantQuota
		expected bool
	}{
		{
			name:     "All unlimited",
			quota:    &TenantQuota{MaxNodes: -1, MaxEdges: -1, MaxStorageBytes: -1},
			expected: true,
		},
		{
			name:     "Nodes limited",
			quota:    &TenantQuota{MaxNodes: 100, MaxEdges: -1, MaxStorageBytes: -1},
			expected: false,
		},
		{
			name:     "All limited",
			quota:    &TenantQuota{MaxNodes: 100, MaxEdges: 500, MaxStorageBytes: 1024},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.quota.IsUnlimited() != tt.expected {
				t.Errorf("IsUnlimited() = %v, expected %v", tt.quota.IsUnlimited(), tt.expected)
			}
		})
	}
}

func TestNewQuotaUsage(t *testing.T) {
	usage := &TenantUsage{
		NodeCount:    25,
		EdgeCount:    100,
		StorageBytes: 5368709120, // 5GB
	}

	quota := &TenantQuota{
		MaxNodes:       100,
		MaxEdges:       500,
		MaxStorageBytes: 10737418240, // 10GB
	}

	qu := NewQuotaUsage(quota, usage)

	if qu.NodesUsed != 25 {
		t.Errorf("Expected NodesUsed 25, got %d", qu.NodesUsed)
	}

	if qu.NodesLimit != 100 {
		t.Errorf("Expected NodesLimit 100, got %d", qu.NodesLimit)
	}

	if qu.NodesPercent != 25.0 {
		t.Errorf("Expected NodesPercent 25.0, got %f", qu.NodesPercent)
	}

	if qu.EdgesPercent != 20.0 {
		t.Errorf("Expected EdgesPercent 20.0, got %f", qu.EdgesPercent)
	}

	if qu.StoragePercent != 50.0 {
		t.Errorf("Expected StoragePercent 50.0, got %f", qu.StoragePercent)
	}
}

func TestNewQuotaUsage_NilQuota(t *testing.T) {
	usage := &TenantUsage{
		NodeCount: 100,
		EdgeCount: 500,
	}

	qu := NewQuotaUsage(nil, usage)

	if qu.NodesLimit != -1 {
		t.Errorf("Expected NodesLimit -1 for nil quota, got %d", qu.NodesLimit)
	}

	if qu.NodesPercent != -1 {
		t.Errorf("Expected NodesPercent -1 for nil quota, got %f", qu.NodesPercent)
	}
}

func TestDefaultQuota(t *testing.T) {
	quota := DefaultQuota()

	if quota.MaxNodes != 1000000 {
		t.Errorf("Expected MaxNodes 1000000, got %d", quota.MaxNodes)
	}

	if quota.MaxEdges != 5000000 {
		t.Errorf("Expected MaxEdges 5000000, got %d", quota.MaxEdges)
	}

	if quota.MaxStorageBytes != 10737418240 {
		t.Errorf("Expected MaxStorageBytes 10737418240, got %d", quota.MaxStorageBytes)
	}
}
