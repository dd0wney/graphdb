package vfstest

import (
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/dd0wney/graphdb/pkg/vfs"
)

// Role names the actor that performs an operation.
//
// FaultFS counts every operation the process performs, which is enough for a
// single-threaded sweep. Under concurrency that counter means nothing: the
// N-th operation overall is a different operation on every run, because the
// scheduler picks the interleaving. So a sweep over it visits an arbitrary
// subset of the error paths while appearing to visit all of them, and still
// terminates and passes.
//
// A per-role counter is stable as long as each actor's own sequence is stable,
// which SweepRole verifies rather than assumes.
type Role string

// RoleUnknown is the role of an operation the classifier does not recognise.
// It is a real role with its own counter, so an unclassified path cannot join
// a role a fault is armed on without anyone noticing.
const RoleUnknown Role = "unknown"

// Op names a filesystem operation, for the trace and for the classifier.
type Op int

const (
	OpOpen Op = iota
	OpRemove
	OpRename
	OpStat
	OpMkdirAll
	OpReadDir
	OpRead
	OpWrite
	OpSync
	OpTruncate
	OpClose
)

func (o Op) String() string {
	switch o {
	case OpOpen:
		return "open"
	case OpRemove:
		return "remove"
	case OpRename:
		return "rename"
	case OpStat:
		return "stat"
	case OpMkdirAll:
		return "mkdirall"
	case OpReadDir:
		return "readdir"
	case OpRead:
		return "read"
	case OpWrite:
		return "write"
	case OpSync:
		return "sync"
	case OpTruncate:
		return "truncate"
	case OpClose:
		return "close"
	}
	return "unknown"
}

// Key names a failure point structurally: which actor, which kind of
// operation, and which occurrence of that kind within that actor.
//
// The sweep walks by ordinal because that is how it reaches every operation.
// It reports what it found by Key, because an ordinal only means anything
// against the exact scenario that produced it: "the flush worker's second
// sync" survives an extra write before it, and "the flush worker's 7th
// operation" does not.
type Key struct {
	Role Role
	Op   Op
	Nth  int
}

func (k Key) String() string { return fmt.Sprintf("%s/%s#%d", k.Role, k.Op, k.Nth) }

// Classifier attributes an operation to a role.
//
// It is called for Open and for the filesystem-level operations. A file takes
// the role assigned at Open and keeps it for every Read, Write, Sync, Truncate
// and Close, so those are never classified. That is correct wherever one actor
// opens a file and performs all of its I/O, which holds in pkg/lsm.
type Classifier func(op Op, name string, flag int) Role

// RoleFS wraps a filesystem, attributes each operation to a role, and can fail
// or park one role's operations.
type RoleFS struct {
	base     vfs.FileSystem
	name     string
	classify Classifier

	mu     sync.Mutex
	counts map[Role]int
	kinds  map[Role]map[Op]int
	traces map[Role][]string

	target Role
	nth    int

	targetKey    Key
	hasTargetKey bool

	fired    bool
	firedKey Key

	pause     *Pause
	pauseRole Role
	pauseNth  int

	// Discovery mode. rng is shared and used only under mu.
	rng      *rand.Rand
	jitter   bool
	failRole Role
	failProb float64
}

// NewRoles wraps base. Pass vfs.OS() for a driver that touches a real disk.
func NewRoles(base vfs.FileSystem, name string, classify Classifier) *RoleFS {
	return &RoleFS{
		base:     base,
		name:     name,
		classify: classify,
		counts:   make(map[Role]int),
		kinds:    make(map[Role]map[Op]int),
		traces:   make(map[Role][]string),
	}
}

// FailNthOpForRole arms a fault on the N-th operation of one role, counting
// from 1. It resets the counters, so each sweep step starts from the same
// place.
func (r *RoleFS) FailNthOpForRole(role Role, n int) {
	r.mu.Lock()
	r.target, r.nth = role, n
	r.hasTargetKey = false
	r.resetLocked()
	r.mu.Unlock()
}

// FailAtKey arms a fault on one structural failure point. This is how a case
// found by the sweep is pinned as a regression test that no longer depends on
// the ordinal the sweep happened to walk.
func (r *RoleFS) FailAtKey(k Key) {
	r.mu.Lock()
	r.targetKey, r.hasTargetKey = k, true
	r.target, r.nth = "", 0
	r.resetLocked()
	r.mu.Unlock()
}

func (r *RoleFS) resetLocked() {
	r.fired = false
	r.firedKey = Key{}
	r.counts = make(map[Role]int)
	r.kinds = make(map[Role]map[Op]int)
	r.traces = make(map[Role][]string)
}

// Jitter makes the driver yield the processor, and sometimes sleep briefly,
// before each operation.
//
// Go offers no control over its scheduler, so this is the only lever a driver
// has on the interleaving. It widens the windows between one actor's
// operations, which makes states reachable that a tight loop never visits. It
// is a discovery aid and not a proof of anything: an interleaving that jitter
// never produces is still an interleaving that can happen.
func (r *RoleFS) Jitter(seed int64) {
	r.mu.Lock()
	r.jitter = true
	if r.rng == nil {
		r.rng = rand.New(rand.NewSource(seed))
	}
	r.mu.Unlock()
}

// FailRandomly arms a fault on each of a role's operations with probability p,
// chosen from the seed.
//
// Exactly one fault fires per run: the driver disarms after it fires, so a
// failing run names one failure point and Explore can hand it back as a Key.
func (r *RoleFS) FailRandomly(seed int64, role Role, p float64) {
	r.mu.Lock()
	if r.rng == nil {
		r.rng = rand.New(rand.NewSource(seed))
	}
	r.failRole, r.failProb = role, p
	r.mu.Unlock()
}

// PauseBeforeNthOpForRole parks the role's N-th operation, counting from 1,
// until the returned Pause is released.
//
// The pause releases itself when the test ends, so a scenario that fails
// before its Release cannot hang the suite or leak the actor's goroutine.
func (r *RoleFS) PauseBeforeNthOpForRole(t *testing.T, role Role, n int) *Pause {
	t.Helper()
	p := newPause()
	r.mu.Lock()
	r.pause, r.pauseRole, r.pauseNth = p, role, n
	r.mu.Unlock()
	t.Cleanup(func() { p.Release(nil) })
	return p
}

// Fired reports whether the armed fault fired. A sweep ends when it did not.
func (r *RoleFS) Fired() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.fired }

// FiredKey returns the structural key of the operation the fault fired on.
func (r *RoleFS) FiredKey() (Key, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.firedKey, r.fired
}

// OpsForRole reports how many operations a role performed.
func (r *RoleFS) OpsForRole(role Role) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[role]
}

// Trace returns the role's operation names in order. SweepRole compares it
// between runs, because a sweep over "the N-th operation" means nothing if the
// sequence is not the same each time.
func (r *RoleFS) Trace(role Role) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.traces[role]))
	copy(out, r.traces[role])
	return out
}

// step records one operation and reports what should happen to it: the barrier
// to block on if this is the parked operation, and whether the armed fault
// fires.
//
// The caller blocks OUTSIDE r.mu. Holding the driver's lock while an actor is
// parked would stop every other actor as well, which would turn the barrier
// into a global freeze and destroy the interleaving it exists to expose.
func (r *RoleFS) step(op Op, role Role, _ string) (*Pause, bool, time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.counts[role]++
	n := r.counts[role]
	if r.kinds[role] == nil {
		r.kinds[role] = make(map[Op]int)
	}
	r.kinds[role][op]++
	key := Key{Role: role, Op: op, Nth: r.kinds[role][op]}
	r.traces[role] = append(r.traces[role], op.String())

	var block *Pause
	if r.pause != nil && role == r.pauseRole && n == r.pauseNth {
		block = r.pause
		// One arrival only. A second would wait on a channel nothing sends to.
		r.pause = nil
	}

	fail := (r.target != "" && role == r.target && n == r.nth) ||
		(r.hasTargetKey && key == r.targetKey)
	if !fail && r.failRole != "" && role == r.failRole && r.rng.Float64() < r.failProb {
		fail = true
		r.failRole = "" // one fault per run, so a failure names one point
	}
	if fail {
		r.fired, r.firedKey = true, key
	}

	// A negative nap means jitter is off. Zero is a real value the generator
	// produces, so it cannot double as "disabled" — without this the
	// deterministic sweep would yield on every operation too.
	nap := time.Duration(-1)
	if r.jitter {
		nap = time.Duration(r.rng.Intn(3)) * 100 * time.Microsecond
	}
	return block, fail, nap
}

// gate applies what step decided. It returns the error the operation must
// report, or nil to proceed.
func gate(block *Pause, fail bool, nap time.Duration, op Op, name string) error {
	// Outside the driver's lock, for the same reason the barrier is: holding
	// it while sleeping would stop every other actor and destroy the very
	// interleaving the jitter exists to produce.
	if nap >= 0 && block == nil {
		runtime.Gosched()
		if nap > 0 {
			time.Sleep(nap)
		}
	}
	if block != nil {
		if err := block.arrive(); err != nil {
			return err
		}
	}
	if fail {
		return fmt.Errorf("%s %s: %w", op, name, ErrInjected)
	}
	return nil
}

func (r *RoleFS) Open(name string, flag int, perm os.FileMode) (vfs.File, error) {
	role := r.classify(OpOpen, name, flag)
	block, fail, nap := r.step(OpOpen, role, name)
	if err := gate(block, fail, nap, OpOpen, name); err != nil {
		return nil, err
	}
	inner, err := r.base.Open(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return &roleFile{File: inner, fs: r, role: role}, nil
}

func (r *RoleFS) Remove(name string) error {
	role := r.classify(OpRemove, name, 0)
	block, fail, nap := r.step(OpRemove, role, name)
	if err := gate(block, fail, nap, OpRemove, name); err != nil {
		return err
	}
	return r.base.Remove(name)
}

func (r *RoleFS) Rename(oldpath, newpath string) error {
	role := r.classify(OpRename, oldpath, 0)
	block, fail, nap := r.step(OpRename, role, oldpath)
	if err := gate(block, fail, nap, OpRename, oldpath); err != nil {
		return err
	}
	return r.base.Rename(oldpath, newpath)
}

func (r *RoleFS) Stat(name string) (os.FileInfo, error) {
	role := r.classify(OpStat, name, 0)
	block, fail, nap := r.step(OpStat, role, name)
	if err := gate(block, fail, nap, OpStat, name); err != nil {
		return nil, err
	}
	return r.base.Stat(name)
}

func (r *RoleFS) MkdirAll(path string, perm os.FileMode) error {
	role := r.classify(OpMkdirAll, path, 0)
	block, fail, nap := r.step(OpMkdirAll, role, path)
	if err := gate(block, fail, nap, OpMkdirAll, path); err != nil {
		return err
	}
	return r.base.MkdirAll(path, perm)
}

// ReadDir lists a directory. pkg/lsm enumerates its SSTables this way, and a
// driver that cannot fail a listing cannot test what happens when one fails.
func (r *RoleFS) ReadDir(name string) ([]os.DirEntry, error) {
	role := r.classify(OpReadDir, name, 0)
	block, fail, nap := r.step(OpReadDir, role, name)
	if err := gate(block, fail, nap, OpReadDir, name); err != nil {
		return nil, err
	}
	return r.base.ReadDir(name)
}

func (r *RoleFS) Name() string { return r.name }

// roleFile carries the role assigned when the file was opened.
type roleFile struct {
	vfs.File
	fs   *RoleFS
	role Role
}

func (rf *roleFile) op(op Op) error {
	block, fail, nap := rf.fs.step(op, rf.role, rf.Name())
	return gate(block, fail, nap, op, rf.Name())
}

func (rf *roleFile) Read(p []byte) (int, error) {
	if err := rf.op(OpRead); err != nil {
		return 0, err
	}
	return rf.File.Read(p)
}

func (rf *roleFile) Write(p []byte) (int, error) {
	if err := rf.op(OpWrite); err != nil {
		return 0, err
	}
	return rf.File.Write(p)
}

func (rf *roleFile) Sync() error {
	if err := rf.op(OpSync); err != nil {
		return err
	}
	return rf.File.Sync()
}

func (rf *roleFile) Truncate(size int64) error {
	if err := rf.op(OpTruncate); err != nil {
		return err
	}
	return rf.File.Truncate(size)
}

func (rf *roleFile) Close() error {
	err := rf.op(OpClose)
	// The descriptor is released whatever the driver decided. A fault driver
	// that leaked one would make every test using it leak, and the leak would
	// be blamed on the code under test. FaultFS.Close does the same.
	closeErr := rf.File.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// RoleFS must satisfy the published interface. This fails to compile if
// vfs.FileSystem grows a method, which is the signal that the driver needs a
// considered implementation of it rather than a silently inherited one.
var _ vfs.FileSystem = (*RoleFS)(nil)
