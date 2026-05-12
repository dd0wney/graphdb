package vector

import (
	"encoding/binary"
	"math"
)

// SerializeHNSWNode serializes an hnswNode into a compact binary format
func SerializeHNSWNode(n *hnswNode) []byte {
	// Calculate size
	size := 8 + 4 + 4 + (len(n.vector) * 4) + 4 // ID, Level, VecLen, VecData, NumLayers
	for _, layer := range n.friends {
		size += 4 + (len(layer) * 8) // FriendCount, FriendIDs
	}

	buf := make([]byte, size)
	offset := 0

	// ID (8)
	binary.BigEndian.PutUint64(buf[offset:], n.id)
	offset += 8

	// Level (4)
	binary.BigEndian.PutUint32(buf[offset:], uint32(n.level))
	offset += 4

	// Vector (4 + len*4)
	binary.BigEndian.PutUint32(buf[offset:], uint32(len(n.vector)))
	offset += 4
	for _, f := range n.vector {
		binary.BigEndian.PutUint32(buf[offset:], math.Float32bits(f))
		offset += 4
	}

	// Friends (4 + layers*(4 + len*8))
	binary.BigEndian.PutUint32(buf[offset:], uint32(len(n.friends)))
	offset += 4
	for _, layer := range n.friends {
		binary.BigEndian.PutUint32(buf[offset:], uint32(len(layer)))
		offset += 4
		for _, friendID := range layer {
			binary.BigEndian.PutUint64(buf[offset:], friendID)
			offset += 8
		}
	}

	return buf
}

// DeserializeHNSWNode deserializes an hnswNode from a binary buffer
func DeserializeHNSWNode(buf []byte) *hnswNode {
	if len(buf) < 20 { // Min size
		return nil
	}
	offset := 0

	n := &hnswNode{}

	// ID
	n.id = binary.BigEndian.Uint64(buf[offset:])
	offset += 8

	// Level
	n.level = int(binary.BigEndian.Uint32(buf[offset:]))
	offset += 4

	// Vector
	vecLen := int(binary.BigEndian.Uint32(buf[offset:]))
	offset += 4
	n.vector = make([]float32, vecLen)
	for i := 0; i < vecLen; i++ {
		n.vector[i] = math.Float32frombits(binary.BigEndian.Uint32(buf[offset:]))
		offset += 4
	}

	// Friends
	numLayers := int(binary.BigEndian.Uint32(buf[offset:]))
	offset += 4
	n.friends = make([][]uint64, numLayers)
	for i := 0; i < numLayers; i++ {
		friendCount := int(binary.BigEndian.Uint32(buf[offset:]))
		offset += 4
		n.friends[i] = make([]uint64, friendCount)
		for j := 0; j < friendCount; j++ {
			n.friends[i][j] = binary.BigEndian.Uint64(buf[offset:])
			offset += 8
		}
	}

	return n
}
