package storage

import (
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/pools"
)

// Key prefixes for LSM storage
const (
	KeyPrefixNode     = 'n' // n:<nodeID> -> Node
	KeyPrefixEdge     = 'e' // e:<edgeID> -> Edge
	KeyPrefixOutEdge  = 'o' // o:<fromID>:<toID> -> edgeID
	KeyPrefixInEdge   = 'i' // i:<toID>:<fromID> -> edgeID
	KeyPrefixLabel    = 'l' // l:<label>:<nodeID> -> empty
	KeyPrefixProperty = 'p' // p:<key>:<value>:<nodeID> -> empty
	KeyPrefixMeta     = 'm' // m:<key> -> value (metadata like counters)
)

// Key generation functions using pooled buffers for efficiency
// These use pools.BufferBuilder to reduce allocations in hot paths

func makeNodeKey(nodeID uint64) []byte {
	b := pools.NewBufferBuilder(9)
	_ = b.WriteByte(KeyPrefixNode)
	b.WriteUint64BE(nodeID)
	return b.Bytes()
}

func makeEdgeKey(edgeID uint64) []byte {
	b := pools.NewBufferBuilder(9)
	_ = b.WriteByte(KeyPrefixEdge)
	b.WriteUint64BE(edgeID)
	return b.Bytes()
}

func makeOutEdgeKey(fromID, toID uint64) []byte {
	b := pools.NewBufferBuilder(17)
	_ = b.WriteByte(KeyPrefixOutEdge)
	b.WriteUint64BE(fromID)
	b.WriteUint64BE(toID)
	return b.Bytes()
}

func makeInEdgeKey(toID, fromID uint64) []byte {
	b := pools.NewBufferBuilder(17)
	_ = b.WriteByte(KeyPrefixInEdge)
	b.WriteUint64BE(toID)
	b.WriteUint64BE(fromID)
	return b.Bytes()
}

func makeLabelKey(label string, nodeID uint64) []byte {
	b := pools.NewBufferBuilder(1 + len(label) + 1 + 8)
	_ = b.WriteByte(KeyPrefixLabel)
	b.WriteString(label)
	_ = b.WriteByte(':')
	b.WriteUint64BE(nodeID)
	return b.Bytes()
}

func makePropertyKey(propKey string, value Value, nodeID uint64) []byte {
	valueStr := fmt.Sprintf("%v", value.Data)
	b := pools.NewBufferBuilder(1 + len(propKey) + 1 + len(valueStr) + 1 + 8)
	_ = b.WriteByte(KeyPrefixProperty)
	b.WriteString(propKey)
	_ = b.WriteByte(':')
	b.WriteString(valueStr)
	_ = b.WriteByte(':')
	b.WriteUint64BE(nodeID)
	return b.Bytes()
}

func makeMetaKey(key string) []byte {
	b := pools.NewBufferBuilder(1 + len(key))
	_ = b.WriteByte(KeyPrefixMeta)
	b.WriteString(key)
	return b.Bytes()
}
