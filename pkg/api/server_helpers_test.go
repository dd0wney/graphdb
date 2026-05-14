package api

import (
	"reflect"
	"testing"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func TestValueToInterface(t *testing.T) {
	ts := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		input storage.Value
		want  any
	}{
		{
			name:  "string",
			input: storage.StringValue("hello"),
			want:  "hello",
		},
		{
			name:  "string empty",
			input: storage.StringValue(""),
			want:  "",
		},
		{
			name:  "int positive",
			input: storage.IntValue(42),
			want:  int64(42),
		},
		{
			name:  "int negative",
			input: storage.IntValue(-7),
			want:  int64(-7),
		},
		{
			name:  "float",
			input: storage.FloatValue(3.14),
			want:  3.14,
		},
		{
			name:  "bool true",
			input: storage.BoolValue(true),
			want:  true,
		},
		{
			name:  "bool false",
			input: storage.BoolValue(false),
			want:  false,
		},
		{
			name:  "timestamp",
			input: storage.TimestampValue(ts),
			want:  ts.Format(time.RFC3339),
		},
		{
			name:  "vector",
			input: storage.VectorValue([]float32{1.0, 2.0, 3.5}),
			want:  []float32{1.0, 2.0, 3.5},
		},
		{
			name:  "string array",
			input: storage.StringArrayValue([]string{"a", "b", "c"}),
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "int array",
			input: storage.IntArrayValue([]int64{1, 2, 3}),
			want:  []int64{1, 2, 3},
		},
		{
			name:  "float array",
			input: storage.FloatArrayValue([]float64{1.5, 2.5}),
			want:  []float64{1.5, 2.5},
		},
		{
			name:  "bool array",
			input: storage.BoolArrayValue([]bool{true, false, true}),
			want:  []bool{true, false, true},
		},
		{
			name:  "raw bytes preserved",
			input: storage.BytesValue([]byte{0x01, 0x02, 0x03}),
			want:  []byte{0x01, 0x02, 0x03},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := valueToInterface(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("valueToInterface(%v) = %v (%T), want %v (%T)",
					tt.input, got, got, tt.want, tt.want)
			}
		})
	}
}

func TestValueToInterface_RoundTripFromConvertToValue(t *testing.T) {
	// Round-trip: JSON-decoded inputs go through convertToValue, then
	// valueToInterface should produce a Go value that round-trips back to
	// the same JSON-decoded shape. Confirms the two helpers are inverses
	// for the JSON-supported types (string, int, float, bool).
	s := &Server{}
	tests := []struct {
		name string
		in   any
	}{
		{"string", "hello"},
		{"int as float64 (JSON shape)", float64(42)},
		{"float", 3.14},
		{"bool", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := s.convertToValue(tt.in)
			out := valueToInterface(v)

			// JSON's int-as-float64 collapses through convertToValue's
			// fast path: any whole-number float64 becomes IntValue, so
			// round-tripping a float64(42) yields int64(42), not
			// float64(42). Normalise for the assertion.
			if f, ok := tt.in.(float64); ok && f == float64(int64(f)) {
				if got, ok := out.(int64); !ok || got != int64(f) {
					t.Errorf("round-trip whole-number float: in=%v out=%v (%T), want int64(%d)",
						tt.in, out, out, int64(f))
				}
				return
			}

			if !reflect.DeepEqual(out, tt.in) {
				t.Errorf("round-trip in=%v out=%v (%T)", tt.in, out, out)
			}
		})
	}
}

// TestConvertToValue_FloatArrayRoundTrips pins the 2026-05-14 fix
// for silent-failure shape #7: JSON arrays of numbers (embedding
// vectors, weight lists, etc.) used to fall through the convertToValue
// switch into the fmt.Sprintf default, getting stored as a string of
// Go's "%v" format ("[0.1 0.2 0.3]" — space-separated, no commas).
// On read-back, no JSON parser could recognise it as an array. Net
// effect: every embedding written via the REST surface stored as a
// string, came back as a string, and any client trying to use it as
// a vector silently saw "embedding_len=0."
//
// Pins both directions: convertToValue maps to TypeFloatArray, and
// valueToInterface round-trips back to []float64 so JSON marshal
// produces a real array.
func TestConvertToValue_FloatArrayRoundTrips(t *testing.T) {
	s := &Server{}
	// []any{float64, ...} is what JSON unmarshal of an array like
	// [-0.844, 0.274, -0.153] produces.
	in := []any{-0.844, 0.274, -0.153, 0.987}
	v := s.convertToValue(in)
	if v.Type != storage.TypeFloatArray {
		t.Fatalf("convertToValue([]float-like) type = %v, want TypeFloatArray", v.Type)
	}
	out := valueToInterface(v)
	arr, ok := out.([]float64)
	if !ok {
		t.Fatalf("valueToInterface returned %T, want []float64", out)
	}
	if len(arr) != len(in) {
		t.Fatalf("round-trip len = %d, want %d", len(arr), len(in))
	}
	for i, f := range arr {
		want := in[i].(float64)
		if f != want {
			t.Errorf("round-trip [%d] = %v, want %v", i, f, want)
		}
	}
}

// TestConvertToValue_MixedArrayHitsLegacyPath documents the deliberate
// non-fix in this PR: arrays with non-float64 elements (string arrays,
// mixed-type arrays) still hit the fmt.Sprintf fallback. The right fix
// for those is to extend allFloat64 into allStrings/allBools/etc — a
// separate scope.
func TestConvertToValue_MixedArrayHitsLegacyPath(t *testing.T) {
	s := &Server{}
	// Mixed array: should NOT be treated as a float array.
	in := []any{1.0, "two", 3.0}
	v := s.convertToValue(in)
	if v.Type != storage.TypeString {
		t.Errorf("mixed array stored as %v, expected TypeString (legacy path)", v.Type)
	}
}

func TestValueToInterface_UnknownTypeFallsBack(t *testing.T) {
	// Synthesise a Value with a Type the helper doesn't know. Should fall
	// back to the raw bytes (preserves current base64 behaviour rather
	// than corrupting the response shape).
	v := storage.Value{
		Type: storage.ValueType(255), // intentionally unknown
		Data: []byte{0xDE, 0xAD},
	}
	got := valueToInterface(v)
	want := []byte{0xDE, 0xAD}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unknown type: got %v (%T), want %v", got, got, want)
	}
}
