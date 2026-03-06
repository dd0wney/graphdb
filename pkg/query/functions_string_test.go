package query

import "testing"

func TestStringFunctions(t *testing.T) {
	tests := []struct {
		name     string
		fn       string
		args     []any
		expected any
	}{
		{"toLower", "toLower", []any{"HELLO"}, "hello"},
		{"toLower nil", "toLower", []any{nil}, ""},
		{"toUpper", "toUpper", []any{"hello"}, "HELLO"},
		{"toString int", "toString", []any{int64(42)}, "42"},
		{"toString string", "toString", []any{"abc"}, "abc"},
		{"trim", "trim", []any{"  hello  "}, "hello"},
		{"replace", "replace", []any{"hello world", "world", "go"}, "hello go"},
		{"substring 2 args", "substring", []any{"hello", int64(1)}, "ello"},
		{"substring 3 args", "substring", []any{"hello", int64(1), int64(3)}, "ell"},
		{"substring past end", "substring", []any{"hi", int64(0), int64(100)}, "hi"},
		{"split", "split", []any{"a,b,c", ","}, []any{"a", "b", "c"}},
		{"startsWith true", "startsWith", []any{"hello", "hel"}, true},
		{"startsWith false", "startsWith", []any{"hello", "world"}, false},
		{"endsWith true", "endsWith", []any{"hello", "llo"}, true},
		{"endsWith false", "endsWith", []any{"hello", "hel"}, false},
		{"contains true", "contains", []any{"hello world", "world"}, true},
		{"contains false", "contains", []any{"hello world", "xyz"}, false},
		{"size string", "size", []any{"hello"}, int64(5)},
		{"size list", "size", []any{[]any{1, 2, 3}}, int64(3)},
		{"size nil", "size", []any{nil}, int64(0)},
		{"length", "length", []any{"abc"}, int64(3)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fn, err := GetFunction(tt.fn)
			if err != nil {
				t.Fatalf("GetFunction(%q) failed: %v", tt.fn, err)
			}

			result, err := fn(tt.args)
			if err != nil {
				t.Fatalf("Function call failed: %v", err)
			}

			// Handle slice comparison
			if expectedSlice, ok := tt.expected.([]any); ok {
				resultSlice, ok := result.([]any)
				if !ok {
					t.Fatalf("Expected []any, got %T", result)
				}
				if len(resultSlice) != len(expectedSlice) {
					t.Fatalf("Expected %d elements, got %d", len(expectedSlice), len(resultSlice))
				}
				for i, v := range expectedSlice {
					if resultSlice[i] != v {
						t.Errorf("Element[%d]: expected %v, got %v", i, v, resultSlice[i])
					}
				}
				return
			}

			if result != tt.expected {
				t.Errorf("Expected %v (%T), got %v (%T)", tt.expected, tt.expected, result, result)
			}
		})
	}
}
