package storage

import (
	"reflect"
	"testing"
)

// TestValueFromJSON covers the scalar dispatch paths plus the
// pre-consolidation behaviour for unknown shapes. Array dispatch has
// its own table below.
func TestValueFromJSON(t *testing.T) {
	tests := []struct {
		name    string
		in      any
		wantTyp ValueType
	}{
		{"string", "hello", TypeString},
		{"int", int(42), TypeInt},
		{"int64", int64(42), TypeInt},
		{"float64 whole-number collapses to int", float64(42), TypeInt},
		{"float64 fractional stays float", 3.14, TypeFloat},
		{"bool true", true, TypeBool},
		{"bool false", false, TypeBool},
		// Structured / unrepresentable shapes now store as TypeJSON so
		// they round-trip instead of being %v-stringified (#224).
		{"struct stores as JSON", struct{ Foo int }{Foo: 1}, TypeJSON},
		{"nil stores as JSON null", nil, TypeJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := ValueFromJSON(tt.in)
			if v.Type != tt.wantTyp {
				t.Errorf("ValueFromJSON(%v) type = %v, want %v", tt.in, v.Type, tt.wantTyp)
			}
		})
	}
}

// TestValueFromJSON_ArrayDispatch covers []any dispatch. The
// consolidation extended the all-float64 path (originally added for
// shape #7) to also cover all-string / all-int / all-bool arrays;
// pin each so a future refactor doesn't quietly regress them.
func TestValueFromJSON_ArrayDispatch(t *testing.T) {
	tests := []struct {
		name    string
		in      []any
		wantTyp ValueType
	}{
		{"all float64", []any{0.1, 0.2, 0.3}, TypeFloatArray},
		{"all string", []any{"a", "b", "c"}, TypeStringArray},
		{"all bool", []any{true, false, true}, TypeBoolArray},
		{"all int (Go-typed callers)", []any{int(1), int(2), int(3)}, TypeIntArray},
		{"all int64 (Go-typed callers)", []any{int64(1), int64(2)}, TypeIntArray},
		{"mixed int and int64", []any{int(1), int64(2)}, TypeIntArray},
		// Mixed-type and empty arrays have no single element-type signal,
		// so they store as TypeJSON and round-trip to a proper JSON array
		// instead of a %v-stringified blob (#224).
		{"mixed float and string", []any{0.1, "x"}, TypeJSON},
		{"mixed types", []any{0.1, true, "x"}, TypeJSON},
		{"empty array", []any{}, TypeJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := ValueFromJSON(tt.in)
			if v.Type != tt.wantTyp {
				t.Errorf("ValueFromJSON(%v) type = %v, want %v", tt.in, v.Type, tt.wantTyp)
			}
		})
	}
}

// TestValueFromJSON_FloatArrayRoundTrips is the canonical fixture
// for shape #7 (2026-05-14). Embedding vectors used to fall through
// the fmt.Sprintf default and store as "[0.1 0.2 0.3]" string;
// now they store as TypeFloatArray and AsFloatArray decodes them
// back losslessly. Without this round-trip working, coord's
// embedding pipeline silently dropped every vector.
func TestValueFromJSON_FloatArrayRoundTrips(t *testing.T) {
	in := []any{-0.844, 0.274, -0.153, 0.987}
	v := ValueFromJSON(in)
	if v.Type != TypeFloatArray {
		t.Fatalf("type = %v, want TypeFloatArray", v.Type)
	}
	arr, err := v.AsFloatArray()
	if err != nil {
		t.Fatalf("AsFloatArray: %v", err)
	}
	if len(arr) != len(in) {
		t.Fatalf("len = %d, want %d", len(arr), len(in))
	}
	for i, f := range arr {
		want := in[i].(float64)
		if f != want {
			t.Errorf("[%d] = %v, want %v", i, f, want)
		}
	}
}

// TestValueFromJSON_StringArrayRoundTrips pins the all-string array
// path the consolidation added beyond shape #7. Previously hit the
// fmt.Sprintf default — any caller passing a JSON array of strings
// (e.g., a `tags` property) lost the array shape on round-trip.
func TestValueFromJSON_StringArrayRoundTrips(t *testing.T) {
	in := []any{"alpha", "beta", "gamma"}
	v := ValueFromJSON(in)
	if v.Type != TypeStringArray {
		t.Fatalf("type = %v, want TypeStringArray", v.Type)
	}
	arr, err := v.AsStringArray()
	if err != nil {
		t.Fatalf("AsStringArray: %v", err)
	}
	want := []string{"alpha", "beta", "gamma"}
	if !reflect.DeepEqual(arr, want) {
		t.Errorf("got %v, want %v", arr, want)
	}
}
