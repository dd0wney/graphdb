package graphql

import (
	"context"
	"errors"

	"github.com/dd0wney/cluso-graphdb/pkg/masking"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// MaskingDeps bundles the per-tenant masking dependencies that GraphQL
// resolvers need to apply a tenant's masking policy at response-shaping
// time. The deps are server-lifecycle and captured in resolver closures
// (mirroring how *storage.GraphStorage is captured); the actual tenant
// and policy are resolved per-request via context.
//
// A nil *MaskingDeps means masking is disabled for this schema (e.g.,
// CLI builds, tests, or schema variants that aren't on the production
// API path). All resolver hook sites tolerate nil deps gracefully.
type MaskingDeps struct {
	Store  *masking.PolicyStore
	Masker *masking.Masker
}

// applyMaskingPolicyForGraphQL is the GraphQL-side twin of the REST
// Server.applyMaskingPolicy hook. Resolvers call this just before
// iterating node.Properties / edge.Properties to ensure tenant-policy
// masking is consistent across REST and GraphQL response surfaces.
//
// Behaviour matches the REST hook (F3 design doc §6):
//
//   - nil deps / nil store / nil masker / nil ctx → pass props through.
//   - No tenant resolvable from ctx → pass props through (caller's
//     responsibility to set tenant via middleware; without it, masking
//     can't be tenant-correct).
//   - No policy for this tenant → pass props through.
//   - Internal store errors (other than ErrPolicyNotFound) → pass
//     props through. Defense-in-depth: a misconfigured store must not
//     break customer reads. The audit log surfaces the gap.
//
// Empty input is returned as-is; callers don't need to pre-check len.
func applyMaskingPolicyForGraphQL(
	ctx context.Context, deps *MaskingDeps, props map[string]storage.Value,
) map[string]storage.Value {
	if deps == nil || deps.Store == nil || deps.Masker == nil || ctx == nil {
		return props
	}
	if len(props) == 0 {
		return props
	}
	tenantID, ok := tenant.FromContext(ctx)
	if !ok || tenantID == "" {
		return props
	}
	policy, err := deps.Store.Get(tenantID)
	if err != nil {
		if errors.Is(err, masking.ErrPolicyNotFound) {
			return props
		}
		return props
	}
	return policy.ApplyToStorageValues(props, deps.Masker)
}
