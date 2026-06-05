package api

import (
	"encoding/json"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// #224: a property value written via the REST path (ValueFromJSON) must read
// back (valueToInterface) as the same JSON shape. Previously null/objects/
// nested/mixed/empty-array values were %v-stringified at ingestion, so they
// round-tripped to "<nil>" / "map[]" / a Go-formatted blob.
func TestValueRoundTrip_JSONFidelity(t *testing.T) {
	cases := []struct {
		name string
		in   any
	}{
		{"null", nil},
		{"empty object", map[string]any{}},
		{"nested object", map[string]any{
			"s":   "x",
			"n":   float64(2),
			"b":   true,
			"arr": []any{float64(1), float64(2)},
			"sub": map[string]any{"deep": "value"},
		}},
		{"empty array", []any{}},
		{"mixed array", []any{float64(1), "two", true}},
		// Typed kinds must still round-trip unchanged.
		{"string", "hello"},
		{"bool", true},
		{"string array", []any{"a", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := valueToInterface(storage.ValueFromJSON(tc.in))

			// Compare via canonical JSON to sidestep map ordering and the
			// int/float representation of inner numbers (JSON has one
			// number type). The issue is about *shape* fidelity.
			wantJSON, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("marshal input: %v", err)
			}
			gotJSON, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("marshal round-tripped value: %v", err)
			}
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("round-trip mismatch:\n  in   = %s\n  out  = %s", wantJSON, gotJSON)
			}
		})
	}
}

// Guard the exact corruption strings from the issue title can no longer appear.
func TestValueRoundTrip_NoGoFormattedBlobs(t *testing.T) {
	for _, in := range []any{nil, map[string]any{}} {
		got := valueToInterface(storage.ValueFromJSON(in))
		if s, ok := got.(string); ok && (s == "<nil>" || s == "map[]") {
			t.Errorf("value %v round-tripped to Go-formatted string %q", in, s)
		}
	}
}
