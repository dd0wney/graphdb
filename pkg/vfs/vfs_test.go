package vfs_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// stubFS is a driver that does nothing. These tests are about the registry,
// not about I/O, so it never has to work.
type stubFS struct{ name string }

func (s stubFS) Open(string, int, os.FileMode) (vfs.File, error) { return nil, errors.New("stub") }
func (s stubFS) Remove(string) error                             { return errors.New("stub") }
func (s stubFS) Rename(string, string) error                     { return errors.New("stub") }
func (s stubFS) Stat(string) (os.FileInfo, error)                { return nil, errors.New("stub") }
func (s stubFS) MkdirAll(string, os.FileMode) error              { return errors.New("stub") }
func (s stubFS) ReadDir(string) ([]os.DirEntry, error)           { return nil, errors.New("stub") }
func (s stubFS) Name() string                                    { return s.name }

// Resolve is the reason this file exists.
//
// It had no test at all until the coupling-coverage measure reported it at
// 0 of 6 statements, which put it joint-last of the fourteen coupling sites.
// Its own doc comment says why an unknown name must be an error:
//
//	falling back would run a test against the real filesystem while the test
//	believed it had installed a fault driver — a pass that means nothing
//
// So the function that exists to stop a meaningless pass was the one nothing
// exercised.
func TestResolveUnknownNameIsAnErrorAndNotAFallback(t *testing.T) {
	fs, err := vfs.Resolve("no-such-driver")
	if err == nil {
		t.Fatalf("Resolve gave a driver named %q for a name nothing registered", fs.Name())
	}
	if !strings.Contains(err.Error(), "no-such-driver") {
		t.Errorf("the error does not name the driver that was asked for: %v", err)
	}
	if fs != nil {
		t.Errorf("Resolve returned a driver alongside its error: %q", fs.Name())
	}
}

func TestResolveEmptyNameGivesTheDefault(t *testing.T) {
	fs, err := vfs.Resolve("")
	if err != nil {
		t.Fatalf("Resolve(\"\"): %v", err)
	}
	if fs.Name() != "os" {
		t.Errorf("Resolve(\"\") gave %q, want the os driver", fs.Name())
	}
}

func TestResolveGivesTheRegisteredDriver(t *testing.T) {
	const name = "resolve-test-driver"
	if err := vfs.Register(name, stubFS{name: name}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { vfs.Unregister(name) })

	fs, err := vfs.Resolve(name)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", name, err)
	}
	if fs.Name() != name {
		t.Errorf("Resolve(%q) gave %q", name, fs.Name())
	}
}

// The default must stay the OS driver whatever else is registered. A store
// that did not ask for a fault driver must never get one.
func TestDefaultIsAlwaysTheOSDriver(t *testing.T) {
	const name = "default-test-driver"
	if err := vfs.Register(name, stubFS{name: name}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() { vfs.Unregister(name) })

	if got := vfs.Default().Name(); got != "os" {
		t.Errorf("Default() is %q after another driver was registered", got)
	}
}

// Replacing "os" would change the behaviour of every test in the same binary,
// including the ones that never asked for a driver.
func TestRegisterRefusesToReplaceTheOSDriver(t *testing.T) {
	if err := vfs.Register("os", stubFS{name: "impostor"}); err == nil {
		t.Fatal("Register replaced the os driver")
	}
	if got := vfs.Default().Name(); got != "os" {
		t.Errorf("the default is now %q", got)
	}
	if fs, _ := vfs.Get("os"); fs == nil || fs.Name() != "os" {
		t.Error("the os driver was replaced in the registry")
	}
}

func TestRegisterRefusesNil(t *testing.T) {
	if err := vfs.Register("nil-driver", nil); err == nil {
		t.Fatal("Register accepted a nil driver")
	}
	if _, ok := vfs.Get("nil-driver"); ok {
		t.Error("a nil driver reached the registry")
	}
}

func TestUnregisterRefusesToRemoveTheOSDriver(t *testing.T) {
	vfs.Unregister("os")
	if fs, ok := vfs.Get("os"); !ok || fs.Name() != "os" {
		t.Fatal("Unregister removed the os driver")
	}
}

func TestUnregisterRemovesAndIsSafeForAnAbsentName(t *testing.T) {
	const name = "unregister-test-driver"
	if err := vfs.Register(name, stubFS{name: name}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	vfs.Unregister(name)
	if _, ok := vfs.Get(name); ok {
		t.Error("the driver is still registered")
	}
	vfs.Unregister(name) // must not panic for a name that is not there
}

// Registering the same name twice replaces the driver, which is what a test
// that installs a fresh fault driver per case relies on.
func TestRegisterTwiceReplaces(t *testing.T) {
	const name = "replace-test-driver"
	if err := vfs.Register(name, stubFS{name: "first"}); err != nil {
		t.Fatalf("Register first: %v", err)
	}
	t.Cleanup(func() { vfs.Unregister(name) })
	if err := vfs.Register(name, stubFS{name: "second"}); err != nil {
		t.Fatalf("Register second: %v", err)
	}
	fs, ok := vfs.Get(name)
	if !ok {
		t.Fatal("the driver is gone after the second Register")
	}
	if fs.Name() != "second" {
		t.Errorf("Get gave %q, want the second driver", fs.Name())
	}
}
