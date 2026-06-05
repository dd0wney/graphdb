package storage

import (
	"reflect"
	"testing"
)

// #224: a TypeJSON property must survive a snapshot + reopen. TypeJSON is the
// newest enum member, so this also guards that persistence (json.Marshal of the
// Value struct, Type as a numeric tag) tolerates the appended type.
func TestJSONValue_SurvivesReopen(t *testing.T) {
	dir := t.TempDir()

	nested := map[string]any{"s": "x", "n": float64(2), "sub": map[string]any{"deep": true}}
	jv, err := JSONValue(nested)
	if err != nil {
		t.Fatalf("JSONValue: %v", err)
	}
	nullV, err := JSONValue(nil)
	if err != nil {
		t.Fatalf("JSONValue(nil): %v", err)
	}

	var nodeID uint64
	{
		gs, err := NewGraphStorage(dir)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		n, err := gs.CreateNodeWithTenant("default", []string{"N"}, map[string]Value{
			"meta": jv,
			"opt":  nullV,
		})
		if err != nil {
			t.Fatalf("create node: %v", err)
		}
		nodeID = n.ID
		if err := gs.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	}

	gs, err := NewGraphStorage(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() { _ = gs.Close() }()

	n, err := gs.GetNodeForTenant(nodeID, "default")
	if err != nil {
		t.Fatalf("get after reopen: %v", err)
	}

	meta, ok := n.Properties["meta"]
	if !ok || meta.Type != TypeJSON {
		t.Fatalf("meta property: ok=%v type=%v, want TypeJSON", ok, meta.Type)
	}
	got, err := meta.AsJSON()
	if err != nil {
		t.Fatalf("AsJSON: %v", err)
	}
	if !reflect.DeepEqual(got, nested) {
		t.Errorf("meta round-trip = %#v, want %#v", got, nested)
	}

	opt := n.Properties["opt"]
	if opt.Type != TypeJSON {
		t.Errorf("opt type = %v, want TypeJSON", opt.Type)
	}
	gotNull, err := opt.AsJSON()
	if err != nil {
		t.Fatalf("AsJSON(null): %v", err)
	}
	if gotNull != nil {
		t.Errorf("null property round-trip = %#v, want nil", gotNull)
	}
}
