package btree

import (
	"bytes"
	"testing"
)

func TestNode_SerializeDeserialize_Leaf(t *testing.T) {
	leaf := NewNode(7, true)
	leaf.Keys = [][]byte{[]byte("alpha"), []byte("beta"), []byte("gamma")}
	leaf.Values = [][]byte{[]byte("1"), []byte("two"), []byte("three!")}
	leaf.NextPage = 99

	if err := leaf.Serialize(); err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	got, err := DeserializeNode(leaf.page)
	if err != nil {
		t.Fatalf("DeserializeNode: %v", err)
	}

	if got.ID != leaf.ID || !got.IsLeaf || got.NextPage != leaf.NextPage {
		t.Fatalf("header mismatch: ID=%d IsLeaf=%v NextPage=%d, want %d/true/%d",
			got.ID, got.IsLeaf, got.NextPage, leaf.ID, leaf.NextPage)
	}
	if len(got.Keys) != len(leaf.Keys) {
		t.Fatalf("Keys len %d, want %d", len(got.Keys), len(leaf.Keys))
	}
	for i := range leaf.Keys {
		if !bytes.Equal(got.Keys[i], leaf.Keys[i]) {
			t.Errorf("Keys[%d]=%q, want %q", i, got.Keys[i], leaf.Keys[i])
		}
		if !bytes.Equal(got.Values[i], leaf.Values[i]) {
			t.Errorf("Values[%d]=%q, want %q", i, got.Values[i], leaf.Values[i])
		}
	}
}

func TestNode_SerializeDeserialize_Internal(t *testing.T) {
	internal := NewNode(11, false)
	internal.Keys = [][]byte{[]byte("m"), []byte("z")}
	internal.Children = []uint64{2, 3, 4}

	if err := internal.Serialize(); err != nil {
		t.Fatalf("Serialize: %v", err)
	}

	got, err := DeserializeNode(internal.page)
	if err != nil {
		t.Fatalf("DeserializeNode: %v", err)
	}

	if got.ID != internal.ID || got.IsLeaf {
		t.Fatalf("header mismatch: ID=%d IsLeaf=%v, want %d/false", got.ID, got.IsLeaf, internal.ID)
	}
	if len(got.Children) != len(internal.Children) {
		t.Fatalf("Children len %d, want %d", len(got.Children), len(internal.Children))
	}
	for i := range internal.Children {
		if got.Children[i] != internal.Children[i] {
			t.Errorf("Children[%d]=%d, want %d", i, got.Children[i], internal.Children[i])
		}
	}
	for i := range internal.Keys {
		if !bytes.Equal(got.Keys[i], internal.Keys[i]) {
			t.Errorf("Keys[%d]=%q, want %q", i, got.Keys[i], internal.Keys[i])
		}
	}
}

func TestNode_FindKey(t *testing.T) {
	n := NewNode(0, true)
	n.Keys = [][]byte{[]byte("b"), []byte("d"), []byte("f")}

	cases := []struct {
		target string
		want   int
	}{
		{"a", 0}, // before all
		{"b", 0}, // exact-first
		{"c", 1}, // between
		{"d", 1}, // exact-middle
		{"e", 2}, // between
		{"f", 2}, // exact-last
		{"g", 3}, // after all
	}
	for _, tc := range cases {
		if got := n.findKey([]byte(tc.target)); got != tc.want {
			t.Errorf("findKey(%q)=%d, want %d", tc.target, got, tc.want)
		}
	}
}
