package tenant

import (
	"errors"
)

// Default tenant ID for backward compatibility
const DefaultTenantID = "default"

// Errors for tenant operations
var (
	ErrTenantNotFound     = errors.New("tenant not found")
	ErrTenantExists       = errors.New("tenant already exists")
	ErrTenantSuspended    = errors.New("tenant is suspended")
	ErrTenantDeleted      = errors.New("tenant is deleted")
	ErrInvalidTenantID    = errors.New("invalid tenant ID")
	ErrInvalidTenantName  = errors.New("tenant name must be 3-100 characters")
	ErrQuotaExceeded      = errors.New("tenant quota exceeded")
)

// TenantStatus represents the lifecycle state of a tenant
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusDeleted   TenantStatus = "deleted"
)

// Tenant represents a tenant in the multi-tenant system
type Tenant struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Status      TenantStatus `json:"status"`
	Quota       *TenantQuota `json:"quota,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	CreatedAt   int64        `json:"created_at"`
	UpdatedAt   int64        `json:"updated_at"`
}

// IsActive returns true if the tenant is in active status
func (t *Tenant) IsActive() bool {
	return t.Status == TenantStatusActive
}

// TenantQuota defines resource limits for a tenant
type TenantQuota struct {
	MaxNodes       int64 `json:"max_nodes"`        // -1 for unlimited
	MaxEdges       int64 `json:"max_edges"`        // -1 for unlimited
	MaxStorageBytes int64 `json:"max_storage_bytes"` // -1 for unlimited
}

// DefaultQuota returns the default quota configuration
func DefaultQuota() *TenantQuota {
	return &TenantQuota{
		MaxNodes:       1000000,     // 1M nodes
		MaxEdges:       5000000,     // 5M edges
		MaxStorageBytes: 10737418240, // 10GB
	}
}

// IsUnlimited returns true if the quota has no limits
func (q *TenantQuota) IsUnlimited() bool {
	return q.MaxNodes == -1 && q.MaxEdges == -1 && q.MaxStorageBytes == -1
}

// TenantUsage tracks current resource usage for a tenant
type TenantUsage struct {
	TenantID     string `json:"tenant_id"`
	NodeCount    int64  `json:"node_count"`
	EdgeCount    int64  `json:"edge_count"`
	StorageBytes int64  `json:"storage_bytes"`
	LastUpdated  int64  `json:"last_updated"`
}

// NewTenantUsage creates a new usage tracker for a tenant
func NewTenantUsage(tenantID string) *TenantUsage {
	return &TenantUsage{
		TenantID: tenantID,
	}
}

// CheckNodeQuota returns an error if creating a node would exceed the quota
func (u *TenantUsage) CheckNodeQuota(quota *TenantQuota) error {
	if quota == nil || quota.MaxNodes == -1 {
		return nil // No limit
	}
	if u.NodeCount >= quota.MaxNodes {
		return ErrQuotaExceeded
	}
	return nil
}

// CheckEdgeQuota returns an error if creating an edge would exceed the quota
func (u *TenantUsage) CheckEdgeQuota(quota *TenantQuota) error {
	if quota == nil || quota.MaxEdges == -1 {
		return nil // No limit
	}
	if u.EdgeCount >= quota.MaxEdges {
		return ErrQuotaExceeded
	}
	return nil
}

// TenantInfo is a summary view of tenant for API responses
type TenantInfo struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Status      TenantStatus `json:"status"`
	NodeCount   int64        `json:"node_count"`
	EdgeCount   int64        `json:"edge_count"`
	QuotaUsage  *QuotaUsage  `json:"quota_usage,omitempty"`
}

// QuotaUsage shows quota limits alongside current usage
type QuotaUsage struct {
	NodesUsed     int64   `json:"nodes_used"`
	NodesLimit    int64   `json:"nodes_limit"`    // -1 if unlimited
	NodesPercent  float64 `json:"nodes_percent"`  // 0-100, -1 if unlimited
	EdgesUsed     int64   `json:"edges_used"`
	EdgesLimit    int64   `json:"edges_limit"`    // -1 if unlimited
	EdgesPercent  float64 `json:"edges_percent"`  // 0-100, -1 if unlimited
	StorageUsed   int64   `json:"storage_used"`
	StorageLimit  int64   `json:"storage_limit"`  // -1 if unlimited
	StoragePercent float64 `json:"storage_percent"` // 0-100, -1 if unlimited
}

// NewQuotaUsage creates a QuotaUsage from quota and usage
func NewQuotaUsage(quota *TenantQuota, usage *TenantUsage) *QuotaUsage {
	qu := &QuotaUsage{
		NodesUsed:    usage.NodeCount,
		EdgesUsed:    usage.EdgeCount,
		StorageUsed:  usage.StorageBytes,
	}

	if quota == nil {
		qu.NodesLimit = -1
		qu.EdgesLimit = -1
		qu.StorageLimit = -1
		qu.NodesPercent = -1
		qu.EdgesPercent = -1
		qu.StoragePercent = -1
		return qu
	}

	qu.NodesLimit = quota.MaxNodes
	qu.EdgesLimit = quota.MaxEdges
	qu.StorageLimit = quota.MaxStorageBytes

	if quota.MaxNodes > 0 {
		qu.NodesPercent = float64(usage.NodeCount) / float64(quota.MaxNodes) * 100
	} else {
		qu.NodesPercent = -1
	}

	if quota.MaxEdges > 0 {
		qu.EdgesPercent = float64(usage.EdgeCount) / float64(quota.MaxEdges) * 100
	} else {
		qu.EdgesPercent = -1
	}

	if quota.MaxStorageBytes > 0 {
		qu.StoragePercent = float64(usage.StorageBytes) / float64(quota.MaxStorageBytes) * 100
	} else {
		qu.StoragePercent = -1
	}

	return qu
}
