package vfs

import (
	"fmt"
	"sync"
)

// The registry mirrors sqlite3_vfs_register: a driver is installed by name and
// selected by name, so a caller can choose one through configuration without
// linking against the implementation.
var (
	mu       sync.RWMutex
	drivers  = map[string]FileSystem{"os": OS()}
	fallback = OS()
)

// Register installs a driver under a name. Registering a name twice replaces
// the previous driver, which is what a test that installs a fresh fault driver
// per case wants.
//
// Registering over "os" is refused: it is the default every caller falls back
// to, and a test that replaced it globally would change the behaviour of every
// other test in the same binary.
func Register(name string, fs FileSystem) error {
	if name == "os" {
		return fmt.Errorf("vfs: cannot replace the %q driver", name)
	}
	if fs == nil {
		return fmt.Errorf("vfs: driver %q is nil", name)
	}
	mu.Lock()
	defer mu.Unlock()
	drivers[name] = fs
	return nil
}

// Unregister removes a driver. It is a no-op for a name that is not registered,
// and it refuses to remove "os".
func Unregister(name string) {
	if name == "os" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	delete(drivers, name)
}

// Get returns the driver registered under a name.
func Get(name string) (FileSystem, bool) {
	mu.RLock()
	defer mu.RUnlock()
	fs, ok := drivers[name]
	return fs, ok
}

// Default returns the driver to use when a caller names none. It is always the
// OS driver: a store that did not ask for a fault driver must never get one.
func Default() FileSystem { return fallback }

// Resolve turns a configured name into a driver. An empty name yields the
// default. An unknown name is an error rather than a silent fallback, because
// falling back would run a test against the real filesystem while the test
// believed it had installed a fault driver — a pass that means nothing.
func Resolve(name string) (FileSystem, error) {
	if name == "" {
		return Default(), nil
	}
	fs, ok := Get(name)
	if !ok {
		return nil, fmt.Errorf("vfs: no driver registered as %q", name)
	}
	return fs, nil
}
