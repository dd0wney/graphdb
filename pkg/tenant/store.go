package tenant

import (
	"fmt"
	"regexp"
	"sync"
	"time"
)

const (
	MinTenantNameLength = 3
	MaxTenantNameLength = 100
	MinTenantIDLength   = 3
	MaxTenantIDLength   = 64
)

// tenantIDRegex validates tenant IDs (alphanumeric, hyphens, underscores)
var tenantIDRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)

// TenantStore manages tenant storage and operations
type TenantStore struct {
	tenants map[string]*Tenant     // tenantID -> Tenant
	usage   map[string]*TenantUsage // tenantID -> TenantUsage
	mu      sync.RWMutex
}

// NewTenantStore creates a new tenant store with a default tenant
func NewTenantStore() *TenantStore {
	store := &TenantStore{
		tenants: make(map[string]*Tenant),
		usage:   make(map[string]*TenantUsage),
	}

	// Create the default tenant for backward compatibility
	now := time.Now().UnixMilli()
	store.tenants[DefaultTenantID] = &Tenant{
		ID:          DefaultTenantID,
		Name:        "Default Tenant",
		Description: "Default tenant for backward compatibility",
		Status:      TenantStatusActive,
		Quota:       nil, // Unlimited for default tenant
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	store.usage[DefaultTenantID] = NewTenantUsage(DefaultTenantID)

	return store
}

// Create creates a new tenant
func (s *TenantStore) Create(tenant *Tenant) error {
	if err := s.validateTenant(tenant); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate ID
	if _, exists := s.tenants[tenant.ID]; exists {
		return fmt.Errorf("%w: %s", ErrTenantExists, tenant.ID)
	}

	// Set timestamps
	now := time.Now().UnixMilli()
	tenant.CreatedAt = now
	tenant.UpdatedAt = now

	// Set default status if not specified
	if tenant.Status == "" {
		tenant.Status = TenantStatusActive
	}

	// Store tenant
	s.tenants[tenant.ID] = tenant
	s.usage[tenant.ID] = NewTenantUsage(tenant.ID)

	return nil
}

// Get retrieves a tenant by ID
func (s *TenantStore) Get(tenantID string) (*Tenant, error) {
	if tenantID == "" {
		return nil, ErrInvalidTenantID
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	tenant, exists := s.tenants[tenantID]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrTenantNotFound, tenantID)
	}

	return tenant, nil
}

// GetActive retrieves a tenant by ID and verifies it's active
func (s *TenantStore) GetActive(tenantID string) (*Tenant, error) {
	tenant, err := s.Get(tenantID)
	if err != nil {
		return nil, err
	}

	switch tenant.Status {
	case TenantStatusSuspended:
		return nil, fmt.Errorf("%w: %s", ErrTenantSuspended, tenantID)
	case TenantStatusDeleted:
		return nil, fmt.Errorf("%w: %s", ErrTenantDeleted, tenantID)
	}

	return tenant, nil
}

// List returns all tenants (excluding deleted)
func (s *TenantStore) List() []*Tenant {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenants := make([]*Tenant, 0, len(s.tenants))
	for _, tenant := range s.tenants {
		if tenant.Status != TenantStatusDeleted {
			tenants = append(tenants, tenant)
		}
	}

	return tenants
}

// ListAll returns all tenants including deleted
func (s *TenantStore) ListAll() []*Tenant {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenants := make([]*Tenant, 0, len(s.tenants))
	for _, tenant := range s.tenants {
		tenants = append(tenants, tenant)
	}

	return tenants
}

// Update updates an existing tenant
func (s *TenantStore) Update(tenant *Tenant) error {
	if tenant.ID == "" {
		return ErrInvalidTenantID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.tenants[tenant.ID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrTenantNotFound, tenant.ID)
	}

	// Validate name if changed
	if tenant.Name != existing.Name {
		if len(tenant.Name) < MinTenantNameLength || len(tenant.Name) > MaxTenantNameLength {
			return ErrInvalidTenantName
		}
	}

	// Update mutable fields
	existing.Name = tenant.Name
	existing.Description = tenant.Description
	existing.Status = tenant.Status
	existing.Quota = tenant.Quota
	existing.Metadata = tenant.Metadata
	existing.UpdatedAt = time.Now().UnixMilli()

	return nil
}

// Delete soft-deletes a tenant (marks as deleted)
func (s *TenantStore) Delete(tenantID string) error {
	if tenantID == "" {
		return ErrInvalidTenantID
	}

	// Prevent deletion of default tenant
	if tenantID == DefaultTenantID {
		return fmt.Errorf("cannot delete default tenant")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tenant, exists := s.tenants[tenantID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrTenantNotFound, tenantID)
	}

	tenant.Status = TenantStatusDeleted
	tenant.UpdatedAt = time.Now().UnixMilli()

	return nil
}

// Suspend suspends a tenant
func (s *TenantStore) Suspend(tenantID string) error {
	if tenantID == "" {
		return ErrInvalidTenantID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tenant, exists := s.tenants[tenantID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrTenantNotFound, tenantID)
	}

	tenant.Status = TenantStatusSuspended
	tenant.UpdatedAt = time.Now().UnixMilli()

	return nil
}

// Activate activates a suspended tenant
func (s *TenantStore) Activate(tenantID string) error {
	if tenantID == "" {
		return ErrInvalidTenantID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tenant, exists := s.tenants[tenantID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrTenantNotFound, tenantID)
	}

	if tenant.Status == TenantStatusDeleted {
		return fmt.Errorf("cannot activate deleted tenant")
	}

	tenant.Status = TenantStatusActive
	tenant.UpdatedAt = time.Now().UnixMilli()

	return nil
}

// GetUsage returns the usage for a tenant
func (s *TenantStore) GetUsage(tenantID string) (*TenantUsage, error) {
	if tenantID == "" {
		return nil, ErrInvalidTenantID
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	usage, exists := s.usage[tenantID]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrTenantNotFound, tenantID)
	}

	return usage, nil
}

// UpdateUsage updates the usage for a tenant
func (s *TenantStore) UpdateUsage(tenantID string, nodeCount, edgeCount, storageBytes int64) error {
	if tenantID == "" {
		return ErrInvalidTenantID
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	usage, exists := s.usage[tenantID]
	if !exists {
		return fmt.Errorf("%w: %s", ErrTenantNotFound, tenantID)
	}

	usage.NodeCount = nodeCount
	usage.EdgeCount = edgeCount
	usage.StorageBytes = storageBytes
	usage.LastUpdated = time.Now().UnixMilli()

	return nil
}

// IncrementNodeCount atomically increments the node count for a tenant
func (s *TenantStore) IncrementNodeCount(tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	usage, exists := s.usage[tenantID]
	if !exists {
		// Auto-create usage for backward compatibility
		usage = NewTenantUsage(tenantID)
		s.usage[tenantID] = usage
	}

	usage.NodeCount++
	usage.LastUpdated = time.Now().UnixMilli()

	return nil
}

// DecrementNodeCount atomically decrements the node count for a tenant
func (s *TenantStore) DecrementNodeCount(tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	usage, exists := s.usage[tenantID]
	if !exists {
		return nil // Nothing to decrement
	}

	if usage.NodeCount > 0 {
		usage.NodeCount--
		usage.LastUpdated = time.Now().UnixMilli()
	}

	return nil
}

// IncrementEdgeCount atomically increments the edge count for a tenant
func (s *TenantStore) IncrementEdgeCount(tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	usage, exists := s.usage[tenantID]
	if !exists {
		// Auto-create usage for backward compatibility
		usage = NewTenantUsage(tenantID)
		s.usage[tenantID] = usage
	}

	usage.EdgeCount++
	usage.LastUpdated = time.Now().UnixMilli()

	return nil
}

// DecrementEdgeCount atomically decrements the edge count for a tenant
func (s *TenantStore) DecrementEdgeCount(tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	usage, exists := s.usage[tenantID]
	if !exists {
		return nil // Nothing to decrement
	}

	if usage.EdgeCount > 0 {
		usage.EdgeCount--
		usage.LastUpdated = time.Now().UnixMilli()
	}

	return nil
}

// CheckNodeQuota checks if a tenant can create a new node
func (s *TenantStore) CheckNodeQuota(tenantID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenant, exists := s.tenants[tenantID]
	if !exists {
		// Allow for backward compatibility (default tenant)
		if tenantID == DefaultTenantID {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrTenantNotFound, tenantID)
	}

	usage, exists := s.usage[tenantID]
	if !exists {
		return nil // No usage tracked yet
	}

	return usage.CheckNodeQuota(tenant.Quota)
}

// CheckEdgeQuota checks if a tenant can create a new edge
func (s *TenantStore) CheckEdgeQuota(tenantID string) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenant, exists := s.tenants[tenantID]
	if !exists {
		// Allow for backward compatibility (default tenant)
		if tenantID == DefaultTenantID {
			return nil
		}
		return fmt.Errorf("%w: %s", ErrTenantNotFound, tenantID)
	}

	usage, exists := s.usage[tenantID]
	if !exists {
		return nil // No usage tracked yet
	}

	return usage.CheckEdgeQuota(tenant.Quota)
}

// Exists returns true if the tenant exists
func (s *TenantStore) Exists(tenantID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.tenants[tenantID]
	return exists
}

// Count returns the number of tenants (excluding deleted)
func (s *TenantStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, tenant := range s.tenants {
		if tenant.Status != TenantStatusDeleted {
			count++
		}
	}

	return count
}

// validateTenant validates tenant fields
func (s *TenantStore) validateTenant(tenant *Tenant) error {
	if tenant == nil {
		return ErrInvalidTenantID
	}

	// Validate ID
	if len(tenant.ID) < MinTenantIDLength || len(tenant.ID) > MaxTenantIDLength {
		return fmt.Errorf("%w: ID must be %d-%d characters", ErrInvalidTenantID, MinTenantIDLength, MaxTenantIDLength)
	}

	if !tenantIDRegex.MatchString(tenant.ID) {
		return fmt.Errorf("%w: ID must be alphanumeric with hyphens/underscores", ErrInvalidTenantID)
	}

	// Validate name
	if len(tenant.Name) < MinTenantNameLength || len(tenant.Name) > MaxTenantNameLength {
		return ErrInvalidTenantName
	}

	return nil
}

// GetTenantInfo returns a summary view of a tenant with usage
func (s *TenantStore) GetTenantInfo(tenantID string) (*TenantInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenant, exists := s.tenants[tenantID]
	if !exists {
		return nil, fmt.Errorf("%w: %s", ErrTenantNotFound, tenantID)
	}

	usage := s.usage[tenantID]
	if usage == nil {
		usage = NewTenantUsage(tenantID)
	}

	info := &TenantInfo{
		ID:        tenant.ID,
		Name:      tenant.Name,
		Status:    tenant.Status,
		NodeCount: usage.NodeCount,
		EdgeCount: usage.EdgeCount,
	}

	if tenant.Quota != nil {
		info.QuotaUsage = NewQuotaUsage(tenant.Quota, usage)
	}

	return info, nil
}
