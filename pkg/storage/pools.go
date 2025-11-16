package storage

import "sync"

// Buffer pools for reducing GC pressure in hot paths

// uint64SlicePool pools []uint64 slices for edge list decompression
var uint64SlicePool = sync.Pool{
	New: func() interface{} {
		// Pre-allocate reasonable capacity (avg 10 edges per node)
		s := make([]uint64, 0, 16)
		return &s
	},
}

// byteSlicePool pools []byte slices for serialization
var byteSlicePool = sync.Pool{
	New: func() interface{} {
		// Pre-allocate reasonable capacity for average edge list
		s := make([]byte, 0, 256)
		return &s
	},
}

// getUint64Slice gets a []uint64 from pool with at least the given capacity
func getUint64Slice(capacity int) []uint64 {
	slice := uint64SlicePool.Get().(*[]uint64)
	if cap(*slice) < capacity {
		// Pool slice too small, allocate new one
		*slice = make([]uint64, 0, capacity)
	}
	*slice = (*slice)[:0] // Reset length to 0, keep capacity
	return *slice
}

// putUint64Slice returns a []uint64 to the pool
func putUint64Slice(slice []uint64) {
	if cap(slice) > 10000 {
		// Don't pool very large slices (> 80KB)
		return
	}
	uint64SlicePool.Put(&slice)
}

// getByteSlice gets a []byte from pool with at least the given capacity
func getByteSlice(capacity int) []byte {
	slice := byteSlicePool.Get().(*[]byte)
	if cap(*slice) < capacity {
		// Pool slice too small, allocate new one
		*slice = make([]byte, 0, capacity)
	}
	*slice = (*slice)[:0] // Reset length to 0, keep capacity
	return *slice
}

// putByteSlice returns a []byte to the pool
func putByteSlice(slice []byte) {
	if cap(slice) > 10000 {
		// Don't pool very large slices (> 10KB)
		return
	}
	byteSlicePool.Put(&slice)
}
