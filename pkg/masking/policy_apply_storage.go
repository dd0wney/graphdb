package masking

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ApplyToStorageValues is the storage.Value-typed twin of Apply, used by
// GraphQL response paths that iterate node.Properties / edge.Properties
// directly (i.e., not via the REST nodeToResponse helper, which works on
// map[string]any after the storage layer's value-decoding step).
//
// Behavioural contract matches Apply: nil receiver, empty input, or nil
// masker each return props unchanged. The masker-nil case is a defensive
// audit-observable gap rather than a fail-closed crash; F3 design doc §6
// requires read paths never break on masking misconfiguration — better to
// surface an unmasked response and log the gap.
//
// Masked output is always TypeString because the masking strategies
// (StrategyPartial / StrategyTokenize / StrategyHash / StrategyFull /
// StrategyRandom) emit strings. A TypeInt value masked under
// StrategyFull becomes a TypeString containing "***" (or the configured
// replacement). This mirrors Apply's behaviour (its any-typed return is
// also a string post-masking) and keeps the policy semantics
// type-independent: "operator named this property → it's masked,
// however it was typed."
//
// The returned map is freshly allocated; the input map is not mutated.
// Unmasked entries share their storage.Value (which is a value type
// holding a []byte slice header, not a deep copy of the bytes).
func (p *Policy) ApplyToStorageValues(
	props map[string]storage.Value, masker *Masker,
) map[string]storage.Value {
	if p == nil || len(props) == 0 {
		return props
	}
	if masker == nil {
		return props
	}

	out := make(map[string]storage.Value, len(props))
	for name, value := range props {
		out[name] = p.applyValueToStorage(name, value, masker)
	}
	return out
}

// applyValueToStorage resolves the strategy for one (name, storage.Value)
// pair. Mirrors applyValue but typed for the storage layer.
func (p *Policy) applyValueToStorage(name string, value storage.Value, masker *Masker) storage.Value {
	if strategy, ok := p.Properties[name]; ok {
		if strategy == StrategyNone {
			return value
		}
		masked := masker.ApplyStrategy(storageValueToString(value), strategy, FieldTypeGeneric)
		return storage.StringValue(masked)
	}

	// Auto-detect: name-based field-type detection, value-level regex.
	// Skips non-strings — the regex heuristics operate on string content,
	// and inferring "this Int property is an SSN" from the name alone
	// would over-mask (an `id` field is an int, the detector fires on
	// `id`, the value gets corrupted).
	if p.AutoDetect && value.Type == storage.TypeString {
		s, err := value.AsString()
		if err != nil {
			return value
		}
		fieldType := masker.detectFieldType(name)
		if fieldType == FieldTypeGeneric {
			return value
		}
		return storage.StringValue(masker.MaskString(s, fieldType))
	}

	return value
}

// storageValueToString collapses a storage.Value to its canonical string
// form for masking. Mirrors anyToString from policy_apply.go but operates
// over the storage.Value tagged union.
//
// Errors from the As* methods only fire when Type and Data are
// mismatched — a storage-layer invariant violation. On that path we
// return "" rather than crash the response (masking is on the read
// path; the F3 design doc §6 forbids fail-closed behaviour). Arrays
// and other compound types fall through to a hex dump of the raw data
// bytes — a niche path that keeps the masked output correctly opaque.
func storageValueToString(v storage.Value) string {
	switch v.Type {
	case storage.TypeString:
		s, err := v.AsString()
		if err != nil {
			return ""
		}
		return s
	case storage.TypeInt:
		i, err := v.AsInt()
		if err != nil {
			return ""
		}
		return fmt.Sprintf("%d", i)
	case storage.TypeFloat:
		f, err := v.AsFloat()
		if err != nil {
			return ""
		}
		return fmt.Sprintf("%g", f)
	case storage.TypeBool:
		b, err := v.AsBool()
		if err != nil {
			return ""
		}
		if b {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%x", v.Data)
	}
}
