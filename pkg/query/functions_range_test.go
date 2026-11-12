package query

import "testing"

// The elements are int64, as in a list literal, so IN compares them.
func TestRangeElementType(t *testing.T) {
	v, err := fnRange([]any{int64(1), int64(3)})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]any)
	if !ok || len(list) != 3 {
		t.Fatalf("want a 3 element []any, got %#v", v)
	}
	for _, e := range list {
		if _, ok := e.(int64); !ok {
			t.Errorf("element %#v is %T, want int64", e, e)
		}
	}
}

func TestRangeLength(t *testing.T) {
	cases := []struct {
		start, end, step int64
		want             int64
		wantErr          bool
	}{
		{1, 3, 1, 3, false},
		{3, 1, -1, 3, false},
		{3, 1, 1, 0, false},
		{1, 3, 0, 0, true},
		{0, 999999, 1, 1000000, false},
		{0, 1000000, 1, 0, true},
		{-9223372036854775808, 9223372036854775807, 1, 0, true},
		{9223372036854775807, -9223372036854775808, -9223372036854775808, 2, false},
		{0, 9223372036854775807, 9223372036854775807, 2, false},
	}
	for _, tc := range cases {
		got, err := rangeLength(tc.start, tc.end, tc.step)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("rangeLength(%d,%d,%d) = %d, %v; want %d, error=%v", tc.start, tc.end, tc.step, got, err, tc.want, tc.wantErr)
		}
	}
}
