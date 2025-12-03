package pools

import (
	"sync"
)

// Buffer size classes for efficient reuse
const (
	TinySize   = 16    // For small keys, headers
	SmallSize  = 64    // For typical keys
	MediumSize = 256   // For small values, serialized data
	LargeSize  = 1024  // For larger values
	HugeSize   = 4096  // For batch operations
	MaxPool    = 65536 // Don't pool buffers larger than this
)

// BytePool provides size-class based pooling for byte slices.
// This reduces GC pressure by reusing buffers of appropriate sizes.
type BytePool struct {
	tiny   sync.Pool // <= 16 bytes
	small  sync.Pool // <= 64 bytes
	medium sync.Pool // <= 256 bytes
	large  sync.Pool // <= 1024 bytes
	huge   sync.Pool // <= 4096 bytes
}

// NewBytePool creates a new byte pool with pre-allocated buffers.
func NewBytePool() *BytePool {
	return &BytePool{
		tiny: sync.Pool{
			New: func() any {
				b := make([]byte, 0, TinySize)
				return &b
			},
		},
		small: sync.Pool{
			New: func() any {
				b := make([]byte, 0, SmallSize)
				return &b
			},
		},
		medium: sync.Pool{
			New: func() any {
				b := make([]byte, 0, MediumSize)
				return &b
			},
		},
		large: sync.Pool{
			New: func() any {
				b := make([]byte, 0, LargeSize)
				return &b
			},
		},
		huge: sync.Pool{
			New: func() any {
				b := make([]byte, 0, HugeSize)
				return &b
			},
		},
	}
}

// Get returns a byte slice with at least the requested capacity.
// The returned slice has length 0 and the specified capacity.
func (p *BytePool) Get(size int) []byte {
	var pool *sync.Pool
	switch {
	case size <= TinySize:
		pool = &p.tiny
	case size <= SmallSize:
		pool = &p.small
	case size <= MediumSize:
		pool = &p.medium
	case size <= LargeSize:
		pool = &p.large
	case size <= HugeSize:
		pool = &p.huge
	default:
		// Too large to pool, allocate directly
		return make([]byte, 0, size)
	}

	bp, ok := pool.Get().(*[]byte)
	if !ok || cap(*bp) < size {
		// Pool returned wrong type or too small, allocate new
		return make([]byte, 0, size)
	}
	return (*bp)[:0]
}

// GetSized returns a byte slice with exactly the requested length.
func (p *BytePool) GetSized(size int) []byte {
	b := p.Get(size)
	return b[:size]
}

// Put returns a byte slice to the pool for reuse.
// Slices larger than MaxPool are not pooled.
func (p *BytePool) Put(b []byte) {
	c := cap(b)
	if c > MaxPool {
		return // Don't pool oversized buffers
	}

	// Reset slice to zero length
	b = b[:0]

	var pool *sync.Pool
	switch {
	case c <= TinySize:
		pool = &p.tiny
	case c <= SmallSize:
		pool = &p.small
	case c <= MediumSize:
		pool = &p.medium
	case c <= LargeSize:
		pool = &p.large
	case c <= HugeSize:
		pool = &p.huge
	default:
		return
	}

	pool.Put(&b)
}

// Default global byte pool
var defaultBytePool = NewBytePool()

// GetBytes returns a byte slice from the default pool.
func GetBytes(size int) []byte {
	return defaultBytePool.Get(size)
}

// GetBytesSized returns a byte slice with exact length from the default pool.
func GetBytesSized(size int) []byte {
	return defaultBytePool.GetSized(size)
}

// PutBytes returns a byte slice to the default pool.
func PutBytes(b []byte) {
	defaultBytePool.Put(b)
}
