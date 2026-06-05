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
//     or empty arrays store as TypeJSON.
//   - nil / map / any other shape → TypeJSON (the JSON encoding),
//     so null/objects/nested structures round-trip instead of being
//     stringified to "<nil>" / "map[]" (#224).
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
		// nil (JSON null), map (JSON object), and any other shape:
		// preserve the structure as TypeJSON instead of %v-stringifying.
		return jsonValueOrString(v)
	}
}

// jsonValueOrString stores v as TypeJSON, falling back to the legacy
// %v-string only if v isn't JSON-marshallable (never happens for
// json.Unmarshal output, but preserves the "never drop the property"
// guarantee for arbitrary Go callers).
func jsonValueOrString(v any) Value {
	jv, err := JSONValue(v)
	if err != nil {
		return StringValue(fmt.Sprintf("%v", v))
	}
	return jv
}

// arrayValueFromJSON dispatches an []any (JSON array) on element type.
// All-same-type arrays map to the matching TypeXxxArray. Mixed-type or
// empty arrays store as TypeJSON so they round-trip to a proper JSON
// array instead of a %v-stringified blob (#224).
func arrayValueFromJSON(arr []any) Value {
	if len(arr) == 0 {
		// Empty array carries no element-type signal — store as TypeJSON
		// so it round-trips to [] rather than the string "[]".
		return jsonValueOrString(arr)
	}
	// One pass: classify element type by inspecting the first element,
	// then verify all remaining elements share that type. Mixed → TypeJSON.
	switch arr[0].(type) {
	case float64:
		out := make([]float64, len(arr))
		for i, v := range arr {
			f, ok := v.(float64)
			if !ok {
				return jsonValueOrString(arr)
			}
			out[i] = f
		}
		return FloatArrayValue(out)
	case string:
		out := make([]string, len(arr))
		for i, v := range arr {
			s, ok := v.(string)
			if !ok {
				return jsonValueOrString(arr)
			}
			out[i] = s
		}
		return StringArrayValue(out)
	case bool:
		out := make([]bool, len(arr))
		for i, v := range arr {
			b, ok := v.(bool)
			if !ok {
				return jsonValueOrString(arr)
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
				return jsonValueOrString(arr)
			}
		}
		return IntArrayValue(out)
	default:
		return jsonValueOrString(arr)
	}
}
