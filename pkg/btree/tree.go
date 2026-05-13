// Package btree implements a disk-backed B+Tree primitive.
//
// TODO(C1.1): this package has no btree-level unit tests. The
// integration-level coverage in pkg/storage/btree_storage_test.go
// exercises Put/Get/Delete via BTreeGraphStorage, but Put/Get/Delete
// behavior, cursor iteration, split/merge boundaries, and persistence
// across pager reopens are not directly tested here.
package btree

import (
	"bytes"
	"encoding/binary"
	"sync"
)

// Tree represents a B+Tree
type Tree struct {
	pager *Pager
	root  uint64
	mu    sync.RWMutex
}

// Open opens or creates a B+Tree at the given path
func Open(path string) (*Tree, error) {
	pager, err := NewPager(path)
	if err != nil {
		return nil, err
	}

	t := &Tree{
		pager: pager,
	}

	if pager.maxPage == 0 {
		// Initialize tree
		headerPage, err := pager.AllocatePage()
		if err != nil {
			return nil, err
		}

		rootNodePage, err := pager.AllocatePage()
		if err != nil {
			return nil, err
		}

		rootNode := NewNode(rootNodePage.ID, true)
		if err := rootNode.Serialize(); err != nil {
			return nil, err
		}
		if err := pager.WritePage(rootNode.page); err != nil {
			return nil, err
		}

		// Save root ID in header page
		binary.BigEndian.PutUint64(headerPage.Data[0:8], rootNode.ID)
		if err := pager.WritePage(headerPage); err != nil {
			return nil, err
		}

		t.root = rootNode.ID
	} else {
		// Load root ID from header page
		headerPage, err := pager.ReadPage(0)
		if err != nil {
			return nil, err
		}
		t.root = binary.BigEndian.Uint64(headerPage.Data[0:8])
	}

	return t, nil
}

// Get retrieves a value by key
func (t *Tree) Get(key []byte) ([]byte, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	node, err := t.findLeaf(t.root, key)
	if err != nil {
		return nil, false
	}

	idx := node.findKey(key)
	if idx < len(node.Keys) && bytes.Equal(node.Keys[idx], key) {
		val := node.Values[idx]
		if len(val) == 0 {
			return nil, false
		}
		return val, true
	}

	return nil, false
}

// Put inserts a key-value pair
func (t *Tree) Put(key, value []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	rootNode, err := t.readNode(t.root)
	if err != nil {
		return err
	}

	// Check if root needs to split
	if t.isNodeFull(rootNode) {
		newRoot, err := t.pager.AllocatePage()
		if err != nil {
			return err
		}

		newRootNode := NewNode(newRoot.ID, false)
		newRootNode.Children = []uint64{t.root}

		if err := t.splitChild(newRootNode, 0, rootNode); err != nil {
			return err
		}

		t.root = newRootNode.ID
		// Update header
		headerPage, err := t.pager.ReadPage(0)
		if err != nil {
			return err
		}
		binary.BigEndian.PutUint64(headerPage.Data[0:8], t.root)
		if err := t.pager.WritePage(headerPage); err != nil {
			return err
		}

		return t.insertNonFull(newRootNode, key, value)
	}

	return t.insertNonFull(rootNode, key, value)
}

// Delete marks a key as deleted by writing a zero-length value.
// Get and the cursor's Next() treat zero-length values as absent.
//
// TODO(C1.1): replace with real B+Tree delete (key removal + leaf
// underflow rebalance) or document tombstone semantics as the
// intended behavior. Tombstone-only Delete leaks space on the
// delete-and-rewrite-key pattern (a re-Put of the same key
// overwrites in place, but pure deletes never reclaim).
func (t *Tree) Delete(key []byte) error {
	return t.Put(key, nil)
}

func (t *Tree) findLeaf(nodeID uint64, key []byte) (*Node, error) {
	node, err := t.readNode(nodeID)
	if err != nil {
		return nil, err
	}

	if node.IsLeaf {
		return node, nil
	}

	idx := node.findKey(key)
	return t.findLeaf(node.Children[idx], key)
}

func (t *Tree) readNode(pageID uint64) (*Node, error) {
	page, err := t.pager.ReadPage(pageID)
	if err != nil {
		return nil, err
	}
	return DeserializeNode(page)
}

// TODO(C1.1): name this 20 (e.g. maxKeysPerNode) and explain the
// derivation — page size vs. average key/value size. As a literal
// it makes split-rebalance behavior opaque to readers.
func (t *Tree) isNodeFull(n *Node) bool {
	return len(n.Keys) >= 20
}

func (t *Tree) insertNonFull(n *Node, key, value []byte) error {
	idx := n.findKey(key)

	if n.IsLeaf {
		if idx < len(n.Keys) && bytes.Equal(n.Keys[idx], key) {
			// Update existing
			n.Values[idx] = value
		} else {
			// Insert new
			n.Keys = append(n.Keys, nil)
			copy(n.Keys[idx+1:], n.Keys[idx:])
			n.Keys[idx] = key

			n.Values = append(n.Values, nil)
			copy(n.Values[idx+1:], n.Values[idx:])
			n.Values[idx] = value
		}
		if err := n.Serialize(); err != nil {
			return err
		}
		return t.pager.WritePage(n.page)
	}

	child, err := t.readNode(n.Children[idx])
	if err != nil {
		return err
	}

	if t.isNodeFull(child) {
		if err := t.splitChild(n, idx, child); err != nil {
			return err
		}
		if bytes.Compare(key, n.Keys[idx]) > 0 {
			child, err = t.readNode(n.Children[idx+1])
			if err != nil {
				return err
			}
		} else {
			child, err = t.readNode(n.Children[idx])
			if err != nil {
				return err
			}
		}
	}

	return t.insertNonFull(child, key, value)
}

func (t *Tree) splitChild(parent *Node, idx int, child *Node) error {
	newNodePage, err := t.pager.AllocatePage()
	if err != nil {
		return err
	}
	newNode := NewNode(newNodePage.ID, child.IsLeaf)

	mid := len(child.Keys) / 2
	splitKey := child.Keys[mid]

	if child.IsLeaf {
		newNode.Keys = child.Keys[mid:]
		newNode.Values = child.Values[mid:]
		newNode.NextPage = child.NextPage

		child.Keys = child.Keys[:mid]
		child.Values = child.Values[:mid]
		child.NextPage = newNode.ID
	} else {
		newNode.Keys = child.Keys[mid+1:]
		newNode.Children = child.Children[mid+1:]

		child.Keys = child.Keys[:mid]
		child.Children = child.Children[:mid+1]
	}

	// Update parent
	parent.Keys = append(parent.Keys, nil)
	copy(parent.Keys[idx+1:], parent.Keys[idx:])
	parent.Keys[idx] = splitKey

	parent.Children = append(parent.Children, 0)
	copy(parent.Children[idx+2:], parent.Children[idx+1:])
	parent.Children[idx+1] = newNode.ID

	if err := child.Serialize(); err != nil {
		return err
	}
	if err := t.pager.WritePage(child.page); err != nil {
		return err
	}

	if err := newNode.Serialize(); err != nil {
		return err
	}
	if err := t.pager.WritePage(newNode.page); err != nil {
		return err
	}

	if err := parent.Serialize(); err != nil {
		return err
	}
	return t.pager.WritePage(parent.page)
}

// Cursor returns a cursor for range scans starting at key
func (t *Tree) Cursor(startKey []byte) *Cursor {
	t.mu.RLock()
	defer t.mu.RUnlock()

	leaf, err := t.findLeaf(t.root, startKey)
	if err != nil {
		return nil
	}

	idx := leaf.findKey(startKey)
	return &Cursor{
		tree:    t,
		curLeaf: leaf,
		curIdx:  idx,
	}
}

// Cursor represents an iterator over B+Tree keys
type Cursor struct {
	tree    *Tree
	curLeaf *Node
	curIdx  int
}

// Next moves to the next key-value pair
func (c *Cursor) Next() ([]byte, []byte, bool) {
	if c.curLeaf == nil {
		return nil, nil, false
	}

	if c.curIdx >= len(c.curLeaf.Keys) {
		if c.curLeaf.NextPage == 0 {
			c.curLeaf = nil
			return nil, nil, false
		}

		nextLeaf, err := c.tree.readNode(c.curLeaf.NextPage)
		if err != nil {
			c.curLeaf = nil
			return nil, nil, false
		}
		c.curLeaf = nextLeaf
		c.curIdx = 0

		if len(c.curLeaf.Keys) == 0 {
			return c.Next()
		}
	}

	key := c.curLeaf.Keys[c.curIdx]
	val := c.curLeaf.Values[c.curIdx]
	c.curIdx++

	// Skip deleted items
	if len(val) == 0 {
		return c.Next()
	}

	return key, val, true
}

// Close closes the tree
func (t *Tree) Close() error {
	return t.pager.Close()
}

// Flush flushes the tree to disk
func (t *Tree) Flush() error {
	return t.pager.Flush()
}
