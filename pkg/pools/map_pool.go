package pools

import (
	"sync"
)

// StringMapPool pools map[string]any for property maps.
type StringMapPool struct {
	pool sync.Pool
}

// NewStringMapPool creates a new string map pool.
func NewStringMapPool() *StringMapPool {
	return &StringMapPool{
		pool: sync.Pool{
			New: func() any {
				return make(map[string]any, 8)
			},
		},
	}
}

// Get returns a cleared map from the pool.
func (p *StringMapPool) Get() map[string]any {
	m, ok := p.pool.Get().(map[string]any)
	if !ok {
		return make(map[string]any, 8)
	}
	// Clear the map
	clear(m)
	return m
}

// Put returns a map to the pool.
func (p *StringMapPool) Put(m map[string]any) {
	if m == nil || len(m) > 1000 {
		return // Don't pool nil or very large maps
	}
	p.pool.Put(m)
}

// Default global string map pool
var defaultStringMapPool = NewStringMapPool()

// GetStringMap returns a string map from the default pool.
func GetStringMap() map[string]any {
	return defaultStringMapPool.Get()
}

// PutStringMap returns a string map to the default pool.
func PutStringMap(m map[string]any) {
	defaultStringMapPool.Put(m)
}
