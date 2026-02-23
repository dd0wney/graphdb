package query

import (
	"fmt"
	"sync"
)

// QueryFunc is a function callable from within a query
type QueryFunc func(args []any) (any, error)

var (
	functionRegistry   = make(map[string]QueryFunc)
	functionRegistryMu sync.RWMutex
)

// RegisterFunction registers a named function for use in queries
func RegisterFunction(name string, fn QueryFunc) {
	functionRegistryMu.Lock()
	defer functionRegistryMu.Unlock()
	functionRegistry[name] = fn
}

// GetFunction retrieves a registered function by name (case-insensitive)
func GetFunction(name string) (QueryFunc, error) {
	functionRegistryMu.RLock()
	defer functionRegistryMu.RUnlock()
	if fn, ok := functionRegistry[name]; ok {
		return fn, nil
	}
	return nil, fmt.Errorf("unknown function: %s", name)
}
