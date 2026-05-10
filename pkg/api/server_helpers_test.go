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
