package masking

import "time"

// Policy is a per-tenant masking specification. Operators set a Policy
// per tenant via the F3 compliance API; the Server applies it to every
// outgoing node/edge response on the REST read path.
//
// Schema is property-name-keyed: an operator names specific properties
// (e.g. "email", "ssn") and chooses a MaskingStrategy for each. If
// AutoDetect is true and a property is NOT named in Properties, the
// Masker's heuristics (regex-based: email/phone/cc/ssn/apikey/ip) get a
// second-pass shot at it.
//
// A nil or empty Policy is equivalent to "no masking" — the property
// flows through verbatim. This is deliberate: the default for a tenant
// with no policy set is the pre-F3 behaviour (everything visible),
// matching the design doc §3 Decision 4's "policies are lost on restart"
// caveat (in-memory PolicyStore — a restart resets every tenant to "no
// policy" until they re-POST).
type Policy struct {
	// TenantID is the tenant this policy applies to. PolicyStore keys
	// by tenant; this is the storage key duplicated in the value for
	// API-response convenience.
	TenantID string `json:"tenant_id"`

	// Properties is an explicit allow-list of property names to mask,
	// each with its chosen strategy. Highest priority — wins over
	// AutoDetect.
	Properties map[string]MaskingStrategy `json:"properties,omitempty"`

	// AutoDetect, when true, runs Masker's auto-detect heuristics on
	// any property NOT named in Properties. Lower priority than the
	// explicit list. Off by default — opt-in for tenants that want
	// pattern-based masking without enumerating every property.
	AutoDetect bool `json:"auto_detect"`

	// UpdatedAt is the wall-clock time the policy was last written.
	// Surfaced in GET responses so operators can correlate with audit
	// events.
	UpdatedAt time.Time `json:"updated_at"`
}

// Clone returns a deep copy of the Policy. PolicyStore uses this to
// hand out snapshots that callers can mutate without affecting the
// store's authoritative copy.
func (p *Policy) Clone() *Policy {
	if p == nil {
		return nil
	}
	cp := &Policy{
		TenantID:   p.TenantID,
		AutoDetect: p.AutoDetect,
		UpdatedAt:  p.UpdatedAt,
	}
	if p.Properties != nil {
		cp.Properties = make(map[string]MaskingStrategy, len(p.Properties))
		for k, v := range p.Properties {
			cp.Properties[k] = v
		}
	}
	return cp
}
