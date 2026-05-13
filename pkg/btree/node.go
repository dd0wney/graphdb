package btree

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

const (
	MaxKeySize   = 128
	MaxValueSize = 1024
)

// Node represents a B+Tree node
type Node struct {
	ID       uint64
	IsLeaf   bool
	Keys     [][]byte
	Children []uint64 // For internal nodes
	Values   [][]byte // For leaf nodes
	NextPage uint64   // For leaf nodes (linked list)
	page     *Page
}

// NewNode creates a new B+Tree node
func NewNode(pageID uint64, isLeaf bool) *Node {
	return &Node{
		ID:     pageID,
		IsLeaf: isLeaf,
		page:   &Page{ID: pageID, Data: make([]byte, PageSize)},
	}
}

// Serialize serializes the node into its page data
func (n *Node) Serialize() error {
	buf := n.page.Data
	for i := range buf {
		buf[i] = 0 // Clear page
	}

	pos := 0
	if n.IsLeaf {
		buf[pos] = 1
	} else {
		buf[pos] = 0
	}
	pos++

	binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(n.Keys)))
	pos += 2

	if n.IsLeaf {
		binary.BigEndian.PutUint64(buf[pos:pos+8], n.NextPage)
		pos += 8

		for i := 0; i < len(n.Keys); i++ {
			// Key length
			binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(n.Keys[i])))
			pos += 2
			// Key data
			copy(buf[pos:pos+len(n.Keys[i])], n.Keys[i])
			pos += len(n.Keys[i])

			// Value length
			binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(n.Values[i])))
			pos += 2
			// Value data
			copy(buf[pos:pos+len(n.Values[i])], n.Values[i])
			pos += len(n.Values[i])
		}
	} else {
		// Internal node
		for i := 0; i < len(n.Children); i++ {
			binary.BigEndian.PutUint64(buf[pos:pos+8], n.Children[i])
			pos += 8
		}

		for i := 0; i < len(n.Keys); i++ {
			// Key length
			binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(n.Keys[i])))
			pos += 2
			// Key data
			copy(buf[pos:pos+len(n.Keys[i])], n.Keys[i])
			pos += len(n.Keys[i])
		}
	}

	if pos > PageSize {
		return fmt.Errorf("node overflow: pos %d > %d", pos, PageSize)
	}

	return nil
}

// DeserializeNode deserializes a node from a page
func DeserializeNode(page *Page) (*Node, error) {
	n := &Node{
		ID:   page.ID,
		page: page,
	}

	buf := page.Data
	pos := 0

	n.IsLeaf = buf[pos] == 1
	pos++

	numKeys := int(binary.BigEndian.Uint16(buf[pos : pos+2]))
	pos += 2

	if n.IsLeaf {
		n.NextPage = binary.BigEndian.Uint64(buf[pos : pos+8])
		pos += 8

		n.Keys = make([][]byte, numKeys)
		n.Values = make([][]byte, numKeys)

		for i := 0; i < numKeys; i++ {
			keyLen := int(binary.BigEndian.Uint16(buf[pos : pos+2]))
			pos += 2
			n.Keys[i] = make([]byte, keyLen)
			copy(n.Keys[i], buf[pos:pos+keyLen])
			pos += keyLen

			valLen := int(binary.BigEndian.Uint16(buf[pos : pos+2]))
			pos += 2
			n.Values[i] = make([]byte, valLen)
			copy(n.Values[i], buf[pos:pos+valLen])
			pos += valLen
		}
	} else {
		n.Children = make([]uint64, numKeys+1)
		for i := 0; i <= numKeys; i++ {
			n.Children[i] = binary.BigEndian.Uint64(buf[pos : pos+8])
			pos += 8
		}

		n.Keys = make([][]byte, numKeys)
		for i := 0; i < numKeys; i++ {
			keyLen := int(binary.BigEndian.Uint16(buf[pos : pos+2]))
			pos += 2
			n.Keys[i] = make([]byte, keyLen)
			copy(n.Keys[i], buf[pos:pos+keyLen])
			pos += keyLen
		}
	}

	return n, nil
}

// findKey returns the index of the first key >= target. Used for
// in-leaf operations: locating an existing key for update, finding
// the slot where a new key would be inserted, etc.
func (n *Node) findKey(target []byte) int {
	for i, key := range n.Keys {
		if bytes.Compare(key, target) >= 0 {
			return i
		}
	}
	return len(n.Keys)
}

// findChild returns the index of the child to descend into for
// target on an internal node. Returns the first i where Keys[i] >
// target, or len(Keys) if target is >= all keys.
//
// This is distinct from findKey because of the leaf-split convention:
// when a leaf splits, the split key is the first key of the right
// leaf (the right leaf keeps [mid:end]) and is also copied into the
// parent. So a lookup for a key equal to splitKey must descend right,
// not left — i.e. comparisons must be strict, not >=.
func (n *Node) findChild(target []byte) int {
	for i, key := range n.Keys {
		if bytes.Compare(key, target) > 0 {
			return i
		}
	}
	return len(n.Keys)
}
