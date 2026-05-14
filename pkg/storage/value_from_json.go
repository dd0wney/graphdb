package storage

import "fmt"

// ValueFromJSON converts a JSON-decoded Go value (the result of
// json.Unmarshal into `any` / `map[string]any` / `[]any`) into a
// typed storage.Value.
//
// Dispatches on Go type:
//
//   - string         → TypeString
//   - int, int64     → TypeInt
//   - float64        → TypeInt if whole-number, TypeFloat otherwise
//     (JSON numbers always arrive as float64; we
//     collapse whole numbers to TypeInt for better
//     downstream compatibility).
//   - bool           → TypeBool
//   - []any          → dispatches on element type for the all-same-type
//     cases (float64, string, int/int64, bool); mixed
//     or empty arrays fall through to the string path.
//   - anything else  → fmt.Sprintf("%v", v) stored as TypeString
//     (legacy fallback, kept so unknown shapes don't
//     drop the property; surfacing on read is the
//     caller's concern).
//
// Never errors. Inverse of valueToInterface (in pkg/api/server_helpers.go).
//
// This function is the single canonical converter shared between the
// REST handlers (pkg/api/server_helpers.go convertToValue) and the
// GraphQL resolvers (pkg/graphql/mutations_resolvers.go
// convertToStorageValue). Two duplicates with diverging behaviour had
// caused real bugs (2026-05-14 silent-failure shape #7 — REST fix
// alone left coord's GraphQL path broken); consolidation prevents
// the next "fix one, miss the other" incident.
func ValueFromJSON(v any) Value {
	switch val := v.(type) {
	case string:
		return StringValue(val)
	case int:
		return IntValue(int64(val))
	case int64:
		return IntValue(val)
	case float64:
		if val == float64(int64(val)) {
			return IntValue(int64(val))
		}
		return FloatValue(val)
	case bool:
		return BoolValue(val)
	case []any:
		return arrayValueFromJSON(val)
	default:
		return StringValue(fmt.Sprintf("%v", v))
	}
}

// arrayValueFromJSON dispatches an []any (JSON array) on element type.
// All-same-type arrays map to the matching TypeXxxArray. Mixed-type
// or empty arrays fall through to fmt.Sprintf — preserving the
// pre-consolidation behaviour for those shapes (a future change can
// promote mixed arrays to a structured representation if a workload
// needs it).
func arrayValueFromJSON(arr []any) Value {
	if len(arr) == 0 {
		// Empty array carries no element-type signal. Keep legacy
		// fmt.Sprintf path so behaviour is unchanged.
		return StringValue(fmt.Sprintf("%v", arr))
	}
	// One pass: classify element type by inspecting the first element,
	// then verify all remaining elements share that type. Mixed → string
	// fallback.
	switch arr[0].(type) {
	case float64:
		out := make([]float64, len(arr))
		for i, v := range arr {
			f, ok := v.(float64)
			if !ok {
				return StringValue(fmt.Sprintf("%v", arr))
			}
			out[i] = f
		}
		return FloatArrayValue(out)
	case string:
		out := make([]string, len(arr))
		for i, v := range arr {
			s, ok := v.(string)
			if !ok {
				return StringValue(fmt.Sprintf("%v", arr))
			}
			out[i] = s
		}
		return StringArrayValue(out)
	case bool:
		out := make([]bool, len(arr))
		for i, v := range arr {
			b, ok := v.(bool)
			if !ok {
				return StringValue(fmt.Sprintf("%v", arr))
			}
			out[i] = b
		}
		return BoolArrayValue(out)
	case int, int64:
		// Unusual: Go-typed ints from non-JSON callers. JSON itself
		// always produces float64, but this branch keeps parity with
		// the scalar `case int / case int64` above for Go callers
		// that build []any directly.
		out := make([]int64, len(arr))
		for i, v := range arr {
			switch n := v.(type) {
			case int:
				out[i] = int64(n)
			case int64:
				out[i] = n
			default:
				return StringValue(fmt.Sprintf("%v", arr))
			}
		}
		return IntArrayValue(out)
	default:
		return StringValue(fmt.Sprintf("%v", arr))
	}
}
