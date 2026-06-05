// Package tenant provides graphdb's multi-tenancy primitives.
//
// Two pieces work together. The context helpers [WithTenant] and [FromContext]
// (and [MustFromContext]) thread a tenant ID through a request's
// context.Context; the storage and API layers use that ID to scope every
// operation. [TenantStore] (from [NewTenantStore]) holds per-tenant metadata —
// name, status, quota, and usage.
//
// A "default" tenant is pre-created for backward compatibility with tenant-blind
// callers. Internally the store keys by tenantid.TenantID; its public methods
// accept plain tenant-ID strings for HTTP-handler convenience.
package tenant
