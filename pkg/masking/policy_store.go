package masking

import (
	"errors"
	"sync"
	"time"
)

// ErrPolicyNotFound is returned by PolicyStore.Get when no policy is
// set for the requested tenant. Callers MAY treat this as "apply no
// masking" — the API GET handler distinguishes the case with a 404 so
// operators see when a policy is genuinely absent vs empty.
var ErrPolicyNotFound = errors.New("masking policy not found for tenant")

// PolicyStore is the in-memory tenant → Policy registry. Per design
// doc §3 Decision 4a, this is in-memory only: policies are lost on
// restart until the operator re-POSTs them. A snapshot-persisted
// upgrade (Decision 4b) is the next-PR option if/when commercial
// direction lands as "hosted."
//
// Concurrency: a single sync.RWMutex protects the map. The expected
// access pattern is heavily-read (every read-path response checks the
// policy) and rarely-written (operators set policies on tenant
// provisioning, change rarely). The shard-per-tenant idiom used for
// nodes/edges in pkg/storage (gs.nodeShards [256]) is overkill here
// because the tenant count is small and writes are rare.
type PolicyStore struct {
	mu       sync.RWMutex
	policies map[string]*Policy // keyed by tenantID
}

// NewPolicyStore constructs an empty PolicyStore.
func NewPolicyStore() *PolicyStore {
	return &PolicyStore{
		policies: make(map[string]*Policy),
	}
}

// Get returns the policy for tenantID, or (nil, ErrPolicyNotFound) if
// none is set. The returned *Policy is a deep clone — callers can
// mutate it freely without affecting the store.
func (s *PolicyStore) Get(tenantID string) (*Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.policies[tenantID]
	if !ok {
		return nil, ErrPolicyNotFound
	}
	return p.Clone(), nil
}

// Set installs or replaces the policy for tenantID. The TenantID field
// on the policy is set from the argument (so callers can't accidentally
// store policy-for-A under key-B). UpdatedAt is stamped to now.
//
// The store retains a deep clone — the caller's *Policy is theirs to
// mutate after the call returns.
func (s *PolicyStore) Set(tenantID string, p *Policy) {
	if p == nil {
		return
	}
	stored := p.Clone()
	stored.TenantID = tenantID
	stored.UpdatedAt = time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies[tenantID] = stored
}

// Delete removes the policy for tenantID. Returns true if a policy was
// present, false if no-op (already absent).
func (s *PolicyStore) Delete(tenantID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.policies[tenantID]
	if ok {
		delete(s.policies, tenantID)
	}
	return ok
}

// Tenants returns the sorted set of tenant IDs that have a policy set.
// Used by admin diagnostics; not exposed via the F3 endpoints in PR-3a.
func (s *PolicyStore) Tenants() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.policies))
	for k := range s.policies {
		out = append(out, k)
	}
	return out
}
