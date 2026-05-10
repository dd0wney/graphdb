package api

import (
	"context"
	"net/http"
	"sync"
)

type auditCollectorKeyType struct{}

var auditCollectorKey = auditCollectorKeyType{}

// auditCollector is a per-request mutable holder for audit-relevant identity
// and scope. Inner middlewares (requireAuth, withTenant) write into it; the
// outer auditMiddleware reads from it after the request is served.
//
// Why this exists: requireAuth and withTenant both attach claims/tenant to
// the request via r.WithContext(ctx), producing a new *http.Request whose
// context additions are visible only downstream of the wrap. The outer
// auditMiddleware holds a reference to the pre-wrap request, so it never
// sees those additions when reading r.Context().Value(...) directly. A
// pointer-via-context lets all layers share state without the
// upstream-visibility problem — the pointer is installed once at the
// outermost layer (auditCollectorMiddleware), and subsequent layers
// dereference it through context lookups.
type auditCollector struct {
	mu       sync.Mutex
	UserID   string
	Username string
	TenantID string
}

func (c *auditCollector) snapshot() (userID, username, tenantID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.UserID, c.Username, c.TenantID
}

// auditCollectorMiddleware attaches a fresh *auditCollector to the request
// context. Must be installed OUTER of auditMiddleware (so auditMiddleware
// can read after next returns) AND outer of any inner middleware that
// writes to the collector (requireAuth, withTenant). See server.go's
// Start() chain assembly for the production wiring.
func (s *Server) auditCollectorMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		coll := &auditCollector{}
		ctx := context.WithValue(r.Context(), auditCollectorKey, coll)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// getAuditCollector returns the collector attached to ctx by
// auditCollectorMiddleware, or nil if not present (e.g., a handler
// invoked outside the audit chain).
func getAuditCollector(ctx context.Context) *auditCollector {
	if v, ok := ctx.Value(auditCollectorKey).(*auditCollector); ok {
		return v
	}
	return nil
}

// setAuditUser writes the authenticated identity into the request's
// auditCollector for downstream auditMiddleware to read. No-op if no
// collector is attached (handler invoked outside the audit chain).
func setAuditUser(ctx context.Context, userID, username string) {
	c := getAuditCollector(ctx)
	if c == nil {
		return
	}
	c.mu.Lock()
	c.UserID = userID
	c.Username = username
	c.mu.Unlock()
}

// setAuditTenant writes the resolved tenant ID into the request's
// auditCollector. No-op if no collector is attached.
func setAuditTenant(ctx context.Context, tenantID string) {
	c := getAuditCollector(ctx)
	if c == nil {
		return
	}
	c.mu.Lock()
	c.TenantID = tenantID
	c.mu.Unlock()
}
