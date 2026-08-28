package vfstest

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"sync"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// CrashPolicy selects what a simulated power cut does to writes that were never
// synced.
type CrashPolicy int

const (
	// LoseUnsynced discards every write since the last Sync. This is the clean
	// model, and the one graphdb's earlier crash tests used.
	LoseUnsynced CrashPolicy = iota

	// LosePartial applies a random prefix of the unsynced writes and discards
	// the rest, modelling a flush that was interrupted.
	LosePartial

	// ReorderAndLose applies a random SUBSET of the unsynced writes in a random
	// ORDER. This is the interesting one, and the shape graphdb had never
	// tested: SQLite's crash VFS "randomly reorders and corrupts the
	// unsynchronized write operations to simulate the effect of buffered
	// filesystems". Truncation cannot produce it, because the tail can survive
	// while a span in the middle does not.
	ReorderAndLose
)

// CrashFS models a filesystem with a write-back cache.
//
// Writes reach the underlying file immediately, so reads behave normally and
// the store under test works as it would in production. Separately, the driver
// snapshots each file's contents at every Sync and records the writes that
// follow. Crash rolls each file back to its last snapshot and then re-applies
// the recorded writes according to the policy — losing them, losing some, or
// applying them out of order.
//
// This mirrors TH3's approach: snapshot the filesystem at a chosen I/O
// operation, then damage it "in the ways one expects to see following a power
// loss".
type CrashFS struct {
	base vfs.FileSystem
	name string

	mu     sync.Mutex
	files  map[string]*crashFile
	rnd    *rand.Rand
	policy CrashPolicy
}

// NewCrash wraps base. The seed makes a run reproducible: SQLite seeds its PRNG
// for the same reason, because a crash test that cannot be replayed cannot be
// debugged.
func NewCrash(base vfs.FileSystem, name string, seed int64) *CrashFS {
	return &CrashFS{
		base:  base,
		name:  name,
		files: make(map[string]*crashFile),
		rnd:   rand.New(rand.NewSource(seed)),
	}
}

// SetPolicy chooses what the next Crash does.
func (c *CrashFS) SetPolicy(p CrashPolicy) {
	c.mu.Lock()
	c.policy = p
	c.mu.Unlock()
}

// Crash simulates a power cut across every open file.
//
// It returns the number of files it damaged, so a caller can assert that the
// crash actually reached something. A crash test whose crash touched no file
// passes for the wrong reason.
func (c *CrashFS) Crash() (int, error) {
	c.mu.Lock()
	policy := c.policy
	files := make([]*crashFile, 0, len(c.files))
	for _, f := range c.files {
		files = append(files, f)
	}
	rnd := c.rnd
	c.mu.Unlock()

	touched := 0
	for _, f := range files {
		n, err := f.applyCrash(c.base, policy, rnd)
		if err != nil {
			return touched, err
		}
		if n {
			touched++
		}
	}
	return touched, nil
}

func (c *CrashFS) Open(name string, flag int, perm os.FileMode) (vfs.File, error) {
	inner, err := c.base.Open(name, flag, perm)
	if err != nil {
		return nil, err
	}
	cf := &crashFile{File: inner, path: name}
	// Snapshot at open: a file that is never synced still has a known starting
	// state to roll back to.
	if err := cf.snap(); err != nil {
		_ = inner.Close()
		return nil, err
	}
	c.mu.Lock()
	c.files[name] = cf
	c.mu.Unlock()
	return cf, nil
}

func (c *CrashFS) Remove(name string) error {
	c.mu.Lock()
	delete(c.files, name)
	c.mu.Unlock()
	return c.base.Remove(name)
}

func (c *CrashFS) Rename(old, new string) error          { return c.base.Rename(old, new) }
func (c *CrashFS) Stat(name string) (os.FileInfo, error) { return c.base.Stat(name) }
func (c *CrashFS) MkdirAll(p string, m os.FileMode) error {
	return c.base.MkdirAll(p, m)
}
func (c *CrashFS) Name() string { return c.name }

// segment is one write recorded since the last Sync.
type segment struct {
	off  int64
	data []byte
}

type crashFile struct {
	vfs.File
	path string

	mu       sync.Mutex
	snapshot []byte
	unsynced []segment
	pos      int64
}

// snap records the file's current contents as the rollback point.
func (f *crashFile) snap() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapLocked()
}

func (f *crashFile) snapLocked() error {
	if _, err := f.File.Seek(0, io.SeekStart); err != nil {
		return err
	}
	buf, err := io.ReadAll(f.File)
	if err != nil {
		return err
	}
	f.snapshot = buf
	f.unsynced = nil
	// Restore the position the caller expects to continue writing at.
	if _, err := f.File.Seek(f.pos, io.SeekStart); err != nil {
		return err
	}
	return nil
}

func (f *crashFile) Write(p []byte) (int, error) {
	f.mu.Lock()
	off := f.pos
	rec := make([]byte, len(p))
	copy(rec, p)
	f.unsynced = append(f.unsynced, segment{off: off, data: rec})
	f.mu.Unlock()

	n, err := f.File.Write(p)
	f.mu.Lock()
	f.pos += int64(n)
	f.mu.Unlock()
	return n, err
}

func (f *crashFile) WriteAt(p []byte, off int64) (int, error) {
	f.mu.Lock()
	rec := make([]byte, len(p))
	copy(rec, p)
	f.unsynced = append(f.unsynced, segment{off: off, data: rec})
	f.mu.Unlock()
	return f.File.WriteAt(p, off)
}

func (f *crashFile) Seek(offset int64, whence int) (int64, error) {
	pos, err := f.File.Seek(offset, whence)
	if err == nil {
		f.mu.Lock()
		f.pos = pos
		f.mu.Unlock()
	}
	return pos, err
}

func (f *crashFile) Sync() error {
	if err := f.File.Sync(); err != nil {
		return err
	}
	// Everything up to here is durable, so it becomes the new rollback point.
	return f.snap()
}

// applyCrash computes the file's post-crash content and writes it.
//
// It rewrites through a FRESH handle from the base filesystem rather than the
// caller's. graphdb opens its WAL with O_APPEND, and Go rejects WriteAt on an
// append-mode file — every write goes to the end regardless of offset, so a
// rollback through that handle is impossible. Building the target content in
// memory and replacing the file is also the more faithful model: a power cut
// leaves a state, not a sequence of repair operations.
//
// It reports whether anything was unsynced, so a caller can tell a crash that
// damaged something from one that found nothing to damage.
func (f *crashFile) applyCrash(base vfs.FileSystem, policy CrashPolicy, rnd *rand.Rand) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.unsynced) == 0 {
		return false, nil
	}

	pending := f.unsynced
	f.unsynced = nil

	var replay []segment
	switch policy {
	case LoseUnsynced:
		replay = nil
	case LosePartial:
		replay = pending[:rnd.Intn(len(pending)+1)]
	case ReorderAndLose:
		// Keep each write with probability 1/2, then shuffle what survives.
		for _, seg := range pending {
			if rnd.Intn(2) == 0 {
				replay = append(replay, seg)
			}
		}
		rnd.Shuffle(len(replay), func(i, j int) { replay[i], replay[j] = replay[j], replay[i] })
	}

	// Start from the durable content and apply whatever survived, in whatever
	// order survived. A write past the end extends the file, which is what a
	// filesystem does.
	target := make([]byte, len(f.snapshot))
	copy(target, f.snapshot)
	for _, seg := range replay {
		end := seg.off + int64(len(seg.data))
		if end > int64(len(target)) {
			grown := make([]byte, end)
			copy(grown, target)
			target = grown
		}
		copy(target[seg.off:end], seg.data)
	}

	out, err := base.Open(f.path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return false, fmt.Errorf("crash rewrite open %s: %w", f.path, err)
	}
	if _, err := out.Write(target); err != nil {
		_ = out.Close()
		return false, fmt.Errorf("crash rewrite %s: %w", f.path, err)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return false, err
	}
	if err := out.Close(); err != nil {
		return false, err
	}

	// The caller's handle now describes a file that changed underneath it,
	// which is exactly what a process sees after a crash and restart. Its new
	// durable baseline is the content just written.
	f.snapshot = target
	return true, nil
}
