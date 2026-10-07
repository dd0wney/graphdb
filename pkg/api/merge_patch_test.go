package api

import (
	"reflect"
	"testing"
)

// TestSplitMergePatch pins the RFC 7396 split: a top-level null removes its key,
// a null inside a value is part of that value, and the removed keys come back
// sorted so the WAL record does not depend on map order.
func TestSplitMergePatch(t *testing.T) {
	nested := map[string]any{"x": nil}
	set, remove := splitMergePatch(map[string]any{
		"b":    nil,
		"a":    nil,
		"keep": "v",
		"meta": nested,
		"list": []any{nil, 1.0},
	})
	if want := []string{"a", "b"}; !reflect.DeepEqual(remove, want) {
		t.Errorf("remove = %v, want %v", remove, want)
	}
	want := map[string]any{"keep": "v", "meta": nested, "list": []any{nil, 1.0}}
	if !reflect.DeepEqual(set, want) {
		t.Errorf("set = %#v, want %#v", set, want)
	}

	set, remove = splitMergePatch(nil)
	if len(set) != 0 || remove != nil {
		t.Errorf("nil body: set = %v remove = %v, want empty", set, remove)
	}
}
