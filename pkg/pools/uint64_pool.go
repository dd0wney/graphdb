package pools

import (
	"sync"
)

// Uint64Pool pools slices of uint64 for edge lists, ID collections, etc.
type Uint64Pool struct {
	small  sync.Pool // <= 16 elements
	medium sync.Pool // <= 64 elements
	large  sync.Pool // <= 256 elements
}

// NewUint64Pool creates a new uint64 slice pool.
func NewUint64Pool() *Uint64Pool {
	return &Uint64Pool{
		small: sync.Pool{
			New: func() any {
				s := make([]uint64, 0, 16)
				return &s
			},
		},
		medium: sync.Pool{
			New: func() any {
				s := make([]uint64, 0, 64)
				return &s
			},
		},
		large: sync.Pool{
			New: func() any {
				s := make([]uint64, 0, 256)
				return &s
			},
		},
	}
}

// Get returns a uint64 slice with at least the requested capacity.
func (p *Uint64Pool) Get(size int) []uint64 {
	var pool *sync.Pool
	switch {
	case size <= 16:
		pool = &p.small
	case size <= 64:
		pool = &p.medium
	case size <= 256:
		pool = &p.large
	default:
		return make([]uint64, 0, size)
	}

	sp, ok := pool.Get().(*[]uint64)
	if !ok || cap(*sp) < size {
		return make([]uint64, 0, size)
	}
	return (*sp)[:0]
}

// Put returns a uint64 slice to the pool.
func (p *Uint64Pool) Put(s []uint64) {
	c := cap(s)
	if c > 10000 {
		return // Don't pool very large slices
	}

	s = s[:0]

	var pool *sync.Pool
	switch {
	case c <= 16:
		pool = &p.small
	case c <= 64:
		pool = &p.medium
	case c <= 256:
		pool = &p.large
	default:
		return
	}

	pool.Put(&s)
}

// Default global uint64 pool
var defaultUint64Pool = NewUint64Pool()

// GetUint64s returns a uint64 slice from the default pool.
func GetUint64s(size int) []uint64 {
	return defaultUint64Pool.Get(size)
}

// PutUint64s returns a uint64 slice to the default pool.
func PutUint64s(s []uint64) {
	defaultUint64Pool.Put(s)
}
