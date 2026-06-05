package storage

import "testing"

// #224: JSON values the typed enum can't represent (null, objects, empty/mixed
// arrays) were stringified via fmt.Sprintf("%v", ...) at ingestion — baking
// "<nil>" / "map[]" into stored bytes. They must now store as TypeJSON so the
// original shape survives the round-trip.

func TestValueFromJSON_StructuredKindsAreTypeJSON(t *testing.T) {
	cases := []struct {
		name string
		in   any
	}{
		{"null", nil},
		{"empty object", map[string]any{}},
		{"nested object", map[string]any{"s": "x", "n": float64(2), "b": true, "arr": []any{float64(1)}}},
		{"empty array", []any{}},
		{"mixed array", []any{float64(1), "two", true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := ValueFromJSON(tc.in)
			if v.Type != TypeJSON {
				t.Errorf("ValueFromJSON(%v).Type = %v, want TypeJSON", tc.in, v.Type)
			}
		})
	}
}

// Regression guard: the existing typed kinds must keep their exact types — this
// is what catches an accidental enum-value shift from appending TypeJSON.
func TestValueFromJSON_TypedKindsUnchanged(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want ValueType
	}{
		{"string", "hello", TypeString},
		{"int via float64", float64(7), TypeInt},
		{"float", float64(1.5), TypeFloat},
		{"bool", true, TypeBool},
		{"string array", []any{"a", "b"}, TypeStringArray},
		{"float array", []any{1.5, 2.5}, TypeFloatArray},
		// JSON whole-number arrays decode to []float64 → TypeFloatArray
		// (the whole-number→int collapse applies only to scalars).
		{"whole-number array via float64", []any{float64(1), float64(2)}, TypeFloatArray},
		// Go-native int slices (non-JSON callers) take the int branch.
		{"int array via go ints", []any{1, 2}, TypeIntArray},
		{"bool array", []any{true, false}, TypeBoolArray},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValueFromJSON(tc.in).Type; got != tc.want {
				t.Errorf("ValueFromJSON(%v).Type = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}
