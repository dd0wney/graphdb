// Package tenantid defines the canonical identifier type for tenants.
//
// This is a leaf package — it depends on nothing inside graphdb. Every
// other package that needs to refer to a tenant should import this
// package and use the TenantID type rather than a raw string. That way
// tenant IDs can never be silently mixed up with other string values
// (node label, property name, user ID) at the type level.
//
// The package was introduced in audit task A1 (2026-05-06) as the
// foundation for tenant-isolation work in Track A. See
// docs/AUDIT_architecture_2026-05-06.md MED-3 for the design rationale
// and docs/AUDIT_fixes_plan_2026-05-06.md for the migration plan.
package tenantid

// TenantID identifies a tenant within graphdb.
//
// Defined type (not a type alias) so that the Go compiler refuses to
// silently convert from raw strings — accidental mixups between tenant
// IDs and other string values surface as type errors at the call site.
// Conversion at boundaries (HTTP request parsing, persistence) is
// explicit via TenantID(s) or s.String().
type TenantID string

// Empty is the zero value of TenantID. Use IsEmpty() rather than
// comparing to Empty directly when checking for absence.
const Empty TenantID = ""

// Default is the implicit tenant used when no tenant is supplied
// (single-tenant deployments and backwards-compatible request paths).
// Mirrors pkg/tenant.DefaultTenantID; both must stay in sync until
// pkg/tenant migrates fully to this type.
const Default TenantID = "default"

// String returns the underlying string representation. Useful when
// passing a TenantID to APIs that have not yet been migrated to accept
// the typed form (HTTP error messages, logs, audit events).
func (t TenantID) String() string {
	return string(t)
}

// IsEmpty reports whether the TenantID is the zero value.
func (t TenantID) IsEmpty() bool {
	return t == Empty
}
