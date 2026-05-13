package btree

import (
	"bytes"
	"fmt"
	"path/filepath"
	"testing"
)

// newTreeForTest opens a fresh tree in t.TempDir() and registers
// cleanup. Returns the tree and the path so tests that need to
// close-and-reopen can do so without re-resolving the path.
func newTreeForTest(t *testing.T) (*Tree, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "btree.db")
	tree, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = tree.Close() })
	return tree, path
}

func TestTree_PutGet(t *testing.T) {
	tree, _ := newTreeForTest(t)

	if err := tree.Put([]byte("k1"), []byte("v1")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok := tree.Get([]byte("k1"))
	if !ok {
		t.Fatalf("Get returned not-found for known key")
	}
	if !bytes.Equal(got, []byte("v1")) {
		t.Fatalf("Get returned %q, want %q", got, "v1")
	}
}

func TestTree_PutGet_Overwrite(t *testing.T) {
	tree, _ := newTreeForTest(t)

	if err := tree.Put([]byte("k"), []byte("v1")); err != nil {
		t.Fatalf("Put v1: %v", err)
	}
	if err := tree.Put([]byte("k"), []byte("v2")); err != nil {
		t.Fatalf("Put v2: %v", err)
	}

	got, ok := tree.Get([]byte("k"))
	if !ok || !bytes.Equal(got, []byte("v2")) {
		t.Fatalf("Get after overwrite returned (%q, %v), want (v2, true)", got, ok)
	}
}

func TestTree_Get_NotFound(t *testing.T) {
	tree, _ := newTreeForTest(t)

	if _, ok := tree.Get([]byte("missing")); ok {
		t.Fatalf("Get returned ok=true for missing key")
	}
}

func TestTree_Delete_TombstoneSemantics(t *testing.T) {
	tree, _ := newTreeForTest(t)

	if err := tree.Put([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := tree.Delete([]byte("k")); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, ok := tree.Get([]byte("k")); ok {
		t.Fatalf("Get returned ok=true after Delete; tombstone semantics broken")
	}
}

func TestTree_Delete_ResurrectViaPut(t *testing.T) {
	tree, _ := newTreeForTest(t)

	if err := tree.Put([]byte("k"), []byte("v1")); err != nil {
		t.Fatalf("Put v1: %v", err)
	}
	if err := tree.Delete([]byte("k")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := tree.Put([]byte("k"), []byte("v2")); err != nil {
		t.Fatalf("Put v2: %v", err)
	}

	got, ok := tree.Get([]byte("k"))
	if !ok || !bytes.Equal(got, []byte("v2")) {
		t.Fatalf("Get after resurrect returned (%q, %v), want (v2, true)", got, ok)
	}
}

func TestTree_Cursor_OrderedIteration(t *testing.T) {
	tree, _ := newTreeForTest(t)

	// Insert in non-sorted order.
	for _, k := range []string{"c", "a", "b", "e", "d"} {
		if err := tree.Put([]byte(k), []byte("v-"+k)); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}

	c := tree.Cursor(nil)
	if c == nil {
		t.Fatalf("Cursor returned nil")
	}

	var got []string
	for {
		k, _, ok := c.Next()
		if !ok {
			break
		}
		got = append(got, string(k))
	}

	want := []string{"a", "b", "c", "d", "e"}
	if len(got) != len(want) {
		t.Fatalf("got %v keys, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q, want %q", i, got[i], want[i])
		}
	}
}

func TestTree_Cursor_SkipsDeleted(t *testing.T) {
	tree, _ := newTreeForTest(t)

	for _, k := range []string{"a", "b", "c"} {
		if err := tree.Put([]byte(k), []byte("v")); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}
	if err := tree.Delete([]byte("b")); err != nil {
		t.Fatalf("Delete b: %v", err)
	}

	c := tree.Cursor(nil)
	var keys []string
	for {
		k, _, ok := c.Next()
		if !ok {
			break
		}
		keys = append(keys, string(k))
	}

	want := []string{"a", "c"}
	if len(keys) != len(want) || keys[0] != want[0] || keys[1] != want[1] {
		t.Fatalf("Cursor skipped-deleted iteration got %v, want %v", keys, want)
	}
}

func TestTree_Cursor_StartAtKey(t *testing.T) {
	tree, _ := newTreeForTest(t)

	for _, k := range []string{"a", "b", "c", "d"} {
		if err := tree.Put([]byte(k), []byte("v")); err != nil {
			t.Fatalf("Put %s: %v", k, err)
		}
	}

	c := tree.Cursor([]byte("c"))
	first, _, ok := c.Next()
	if !ok {
		t.Fatalf("Cursor.Next at start=c returned no entry")
	}
	if string(first) != "c" {
		t.Fatalf("Cursor at start=c returned %q, want c", first)
	}
}

func TestTree_Persist_AcrossReopen(t *testing.T) {
	tree, path := newTreeForTest(t)

	if err := tree.Put([]byte("survivor"), []byte("payload")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := tree.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := tree.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()

	got, ok := reopened.Get([]byte("survivor"))
	if !ok || !bytes.Equal(got, []byte("payload")) {
		t.Fatalf("after reopen got (%q, %v), want (payload, true)", got, ok)
	}
}

// TestTree_Split_TriggersAtMaxKeysPerNode loads enough small entries
// to force at least one split, then verifies all entries are still
// reachable via Get. This is the only test that asserts on the
// split path; it does not assert the internal-node structure
// because that's implementation-dependent.
func TestTree_Split_TriggersAtMaxKeysPerNode(t *testing.T) {
	tree, _ := newTreeForTest(t)

	// Insert maxKeysPerNode * 3 entries to guarantee multiple splits.
	n := maxKeysPerNode * 3
	for i := 0; i < n; i++ {
		k := []byte(fmt.Sprintf("k%05d", i))
		v := []byte(fmt.Sprintf("v%05d", i))
		if err := tree.Put(k, v); err != nil {
			t.Fatalf("Put #%d: %v", i, err)
		}
	}

	// Verify every key is still retrievable.
	for i := 0; i < n; i++ {
		k := []byte(fmt.Sprintf("k%05d", i))
		want := []byte(fmt.Sprintf("v%05d", i))
		got, ok := tree.Get(k)
		if !ok {
			t.Fatalf("Get #%d returned not-found post-split", i)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("Get #%d returned %q, want %q", i, got, want)
		}
	}
}
