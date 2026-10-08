package storage

import (
	"runtime"
	"testing"
	"time"
)

// The scalar encoders write a fixed width: 8 bytes for an int, a float and a
// timestamp, 1 for a bool. A value whose Data has another length did not come
// from them (a damaged snapshot record, or a Value built by hand), and
// decoding it must fail with an error, not index past the end of the slice.
func TestValueScalarDecoders_RefuseWrongLength(t *testing.T) {
	decoders := []struct {
		name   string
		typ    ValueType
		width  int
		decode func(Value) error
	}{
		{"AsInt", TypeInt, 8, func(v Value) error { _, err := v.AsInt(); return err }},
		{"AsFloat", TypeFloat, 8, func(v Value) error { _, err := v.AsFloat(); return err }},
		{"AsBool", TypeBool, 1, func(v Value) error { _, err := v.AsBool(); return err }},
		{"AsTimestamp", TypeTimestamp, 8, func(v Value) error { _, err := v.AsTimestamp(); return err }},
	}

	for _, d := range decoders {
		for _, n := range []int{0, d.width - 1, d.width + 1} {
			if n < 0 {
				continue
			}
			t.Run(d.name, func(t *testing.T) {
				var err error
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("%s on %d data bytes panicked: %v", d.name, n, r)
						}
					}()
					err = d.decode(Value{Type: d.typ, Data: make([]byte, n)})
				}()
				if err == nil {
					t.Fatalf("%s on %d data bytes returned no error; it needs exactly %d", d.name, n, d.width)
				}
			})
		}
	}
}

// The checks must not reject what the encoders write.
func TestValueScalarDecoders_RoundTrip(t *testing.T) {
	if got, err := IntValue(-42).AsInt(); err != nil || got != -42 {
		t.Errorf("AsInt(IntValue(-42)) = %d, %v", got, err)
	}
	if got, err := FloatValue(2.5).AsFloat(); err != nil || got != 2.5 {
		t.Errorf("AsFloat(FloatValue(2.5)) = %v, %v", got, err)
	}
	if got, err := BoolValue(true).AsBool(); err != nil || !got {
		t.Errorf("AsBool(BoolValue(true)) = %v, %v", got, err)
	}
	ts := time.Unix(1_791_000_000, 0)
	if got, err := TimestampValue(ts).AsTimestamp(); err != nil || !got.Equal(ts) {
		t.Errorf("AsTimestamp(TimestampValue(%v)) = %v, %v", ts, got, err)
	}
}

// A string array's element count comes from four bytes of the value. Each
// element needs at least four bytes for its length, so a count the data cannot
// hold is damage; it must not size an allocation of up to 2^32 strings.
func TestAsStringArray_RefusesCountTheDataCannotHold(t *testing.T) {
	v := Value{Type: TypeStringArray, Data: []byte{0xFF, 0xFF, 0xFF, 0xFF}}

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := v.AsStringArray()
	runtime.ReadMemStats(&after)

	if err == nil {
		t.Fatal("AsStringArray accepted a count of 2^32-1 with no element data")
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 1<<20 {
		t.Fatalf("AsStringArray allocated %d bytes for a 4-byte value before refusing it", grew)
	}
}
