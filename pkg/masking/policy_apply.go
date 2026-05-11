package masking

import "fmt"

// Apply returns a deep copy of props with this Policy's masking rules
// applied. Resolution order per property name:
//
//  1. Policy.Properties[name] — explicit operator-set strategy.
//  2. If AutoDetect is true, Masker.detectFieldType identifies the
//     name's likely sensitivity (email/phone/ssn/...) and applies that
//     FieldType's configured strategy.
//  3. Otherwise the value flows through unmasked.
//
// Non-string property values whose name appears in Policy.Properties
// are coerced to string for masking — operator named the property,
// they expect it masked. Numbers/bools/maps not named in Properties
// flow through unchanged (auto-detect is regex-based and only fires
// on strings).
//
// Apply does NOT mutate the input map. Returns a new map with the
// same shape; callers can freely store/serialize the result.
//
// masker is the application's shared Masker instance. Passed in
// rather than constructed because Masker holds a token cache for
// StrategyTokenize — re-instantiating per call would lose token
// consistency across requests for the same value.
//
// Nil receiver (no policy for this tenant): returns props unchanged
// — the caller can save the copy if they want, but Apply skips the
// work.
func (p *Policy) Apply(props map[string]any, masker *Masker) map[string]any {
	if p == nil || len(props) == 0 {
		return props
	}
	if masker == nil {
		// Defensive: a misconfigured server shouldn't crash the
		// response path. Pass through unmasked so the gap is
		// observable in audit logs rather than corrupted output.
		return props
	}

	out := make(map[string]any, len(props))
	for name, value := range props {
		out[name] = p.applyValue(name, value, masker)
	}
	return out
}

// applyValue resolves the strategy for one (name, value) pair and
// applies it.
func (p *Policy) applyValue(name string, value any, masker *Masker) any {
	// Explicit override wins.
	if strategy, ok := p.Properties[name]; ok {
		return applyStrategyToAny(value, strategy, FieldTypeGeneric, masker)
	}

	// Auto-detect fallback: pattern-based field-type detection on the
	// property NAME, then apply that FieldType's configured strategy.
	if p.AutoDetect {
		s, ok := value.(string)
		if !ok {
			// AutoDetect runs on strings only — name-based detection
			// can pick a FieldType but the value-level heuristics are
			// regex over string content.
			return value
		}
		fieldType := masker.detectFieldType(name)
		if fieldType == FieldTypeGeneric {
			// No specific type detected — leave alone. (The Masker's
			// content-level autoMaskString would catch embedded
			// patterns inside the value, but Policy semantics are
			// stronger: only mask what we can clearly identify.)
			return s
		}
		return masker.MaskString(s, fieldType)
	}

	return value
}

// applyStrategyToAny is the strategy dispatcher for Policy-named
// properties. Non-string values are coerced via fmt.Sprintf — the
// operator named this property, so the contract is "this is masked,
// however it was typed."
func applyStrategyToAny(value any, strategy MaskingStrategy, fieldType FieldType, masker *Masker) any {
	if strategy == StrategyNone {
		return value
	}
	str, ok := value.(string)
	if !ok {
		// Coerce to string for masking. The operator named this
		// property in the policy; they expect it masked.
		str = anyToString(value)
	}
	return masker.ApplyStrategy(str, strategy, fieldType)
}

// anyToString converts a non-string value to its canonical string
// form for masking. Mirrors fmt.Sprint("%v") for primitives and
// JSON-shaped types operators are likely to encounter in node/edge
// properties.
func anyToString(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case []byte:
		return string(v)
	default:
		// Numbers, slices, maps — fall through to fmt to avoid
		// per-numeric-type switch bloat. Properties storing a map
		// or slice will get masked as their fmt-formatted string;
		// that's a niche path but the alternative (recurse + mask)
		// would let the operator's intent ("mask this") leak into
		// arbitrary nested data shapes.
		return fmt.Sprintf("%v", v)
	}
}
