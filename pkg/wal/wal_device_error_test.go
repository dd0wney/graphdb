package wal

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/alloc"
	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// walRole names the actor in the RoleFS driver these tests install. There is
// only one, because a WAL recovery is single-threaded.
const walRole = vfstest.Role("wal")

// newReadFaultFS returns a driver that can fail a read of the WAL file.
//
// vfstest.FaultFS cannot: its file type overrides Write, Sync and Close and
// inherits Read from the base file, so a read on it never fails. RoleFS is the
// only driver in this repository that can fail a read, and it reports the
// failure as fmt.Errorf("read %s: %w", name, ErrInjected) — a wrapped sentinel,
// not an *os.PathError. That shape is the whole point of these tests.
func newReadFaultFS(name string) *vfstest.RoleFS {
	return vfstest.NewRoles(vfs.OS(), name, func(vfstest.Op, string, int) vfstest.Role {
		return walRole
	})
}

// seedDeviceFaultWAL writes count records of payload bytes each through the OS driver and
// closes the log, so a later reopen has a known number of good records to
// recover.
func seedDeviceFaultWAL(t *testing.T, dir string, count, payload int) {
	t.Helper()

	w, err := NewWAL(dir)
	if err != nil {
		t.Fatalf("NewWAL: %v", err)
	}
	data := []byte(strings.Repeat("x", payload))
	for i := 0; i < count; i++ {
		if _, err := w.Append(OpCreateNode, data); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// TestWAL_ReadAll_DeviceErrorIsNotATornTail is the regression gate for a lost
// record.
//
// A device error part way through recovery says nothing about the records after
// it. If ReadAll reports that error as a torn tail — a short entry list and no
// error — then recoverLSN takes currentLSN from the last entry it happened to
// read, the next Append reuses an LSN that a later record on disk already
// holds, and the NEXT recovery drops that record at the non-advancing-LSN
// guard.
//
// RED against the pre-fix code: isResourceError matched only *os.PathError, and
// the driver reports fmt.Errorf("read %s: %w", name, vfstest.ErrInjected).
func TestWAL_ReadAll_DeviceErrorIsNotATornTail(t *testing.T) {
	// 20 records of 1000 bytes is about 20 KiB, and bufio reads 4096 bytes at
	// a time, so the third read lands in the middle of the log rather than at
	// either end. The assertions below check that, so the sizing cannot drift
	// unnoticed.
	const (
		records = 20
		payload = 1000
		failOn  = 3
	)

	dir := t.TempDir()
	seedDeviceFaultWAL(t, dir, records, payload)

	fs := newReadFaultFS("wal-read-device-error")
	w, err := NewWALWithFS(dir, fs)
	if err != nil {
		t.Fatalf("NewWALWithFS with no fault armed: %v", err)
	}
	defer func() { _ = w.Close() }()

	// Arming resets the driver's counters, so the reads the open above already
	// performed do not count towards failOn.
	fs.FailAtKey(vfstest.Key{Role: walRole, Op: vfstest.OpRead, Nth: failOn})

	entries, err := w.ReadAll()

	// The fault must have reached the driver. Without this the assertions
	// below cannot tell a fired fault from a read path that never ran.
	if !fs.Fired() {
		t.Fatalf("the injected read fault never fired; the WAL performed %d operations",
			fs.OpsForRole(walRole))
	}
	if err == nil {
		t.Fatalf("ReadAll returned %d of %d entries and a nil error. A device error "+
			"reported as a clean end of log makes recoverLSN reuse an LSN that a later "+
			"record on disk already holds", len(entries), records)
	}
	if !errors.Is(err, vfstest.ErrInjected) {
		t.Fatalf("ReadAll error = %v, want the injected device error", err)
	}
	// The failure has to be part way through, or the test proves nothing about
	// the records that follow it.
	if len(entries) == 0 || len(entries) >= records {
		t.Fatalf("ReadAll returned %d of %d entries; the fault must land part way "+
			"through the log", len(entries), records)
	}
}

// TestWAL_Open_DeviceErrorDuringRecoveryRefusesToOpen covers the production
// entry point. NewWALWithFS recovers the LSN before it hands the log back, so a
// device error there must refuse the open. An open that succeeds on a partial
// read is the state in which the next Append overwrites a live LSN.
func TestWAL_Open_DeviceErrorDuringRecoveryRefusesToOpen(t *testing.T) {
	const (
		records = 20
		payload = 1000
		failOn  = 3
	)

	dir := t.TempDir()
	seedDeviceFaultWAL(t, dir, records, payload)

	fs := newReadFaultFS("wal-open-device-error")
	fs.FailAtKey(vfstest.Key{Role: walRole, Op: vfstest.OpRead, Nth: failOn})

	w, err := NewWALWithFS(dir, fs)
	if err == nil {
		lsn := w.GetCurrentLSN()
		_ = w.Close()
		t.Fatalf("NewWALWithFS succeeded with currentLSN %d after a device error during "+
			"recovery of %d records; it must refuse the open", lsn, records)
	}
	if !fs.Fired() {
		t.Fatal("the injected read fault never fired; the assertion above proved nothing")
	}
	if !errors.Is(err, vfstest.ErrInjected) {
		t.Fatalf("NewWALWithFS error = %v, want the injected device error", err)
	}
}

// TestWAL_ErrorClassification_Table pins every classification the recovery loop
// makes.
//
// Each row names one error and the answer isResourceError must give it. A wrong
// row costs either a spurious startup failure (data called a resource failure)
// or a lost record (a resource failure called a torn tail).
//
// The last two rows are the gap this table exists to close. A driver outside
// this repository may return any error it likes, and vfs.FileSystem is a
// published interface, so an unrecognised error MUST classify as a resource
// failure. If a later change makes the default "data" again, those rows go red.
func TestWAL_ErrorClassification_Table(t *testing.T) {
	deviceErr := &os.PathError{Op: "read", Path: "wal.log", Err: errors.New("input/output error")}

	tests := []struct {
		name string
		err  error
		// resource is true when the read stopped because the reader could not
		// obtain something, so records may still follow the failure point.
		resource bool
		why      string
	}{
		{
			name: "nil",
			err:  nil,
			why:  "no error is not a failure of any kind",
		},
		{
			name: "io.EOF",
			err:  io.EOF,
			why:  "the log ended on a record boundary; ReadAll breaks before it asks",
		},
		{
			name: "io.ErrUnexpectedEOF",
			err:  io.ErrUnexpectedEOF,
			why:  "a short read inside a record is a torn tail, and nothing after it was durable",
		},
		{
			name: "wrapped io.ErrUnexpectedEOF",
			err:  fmt.Errorf("read record 4: %w", io.ErrUnexpectedEOF),
			why:  "wrapping must not change the answer",
		},
		{
			name: "errWALRecordTooLarge",
			err:  errWALRecordTooLarge,
			why:  "the header claims a length this package never writes",
		},
		{
			name:     "alloc.ErrNoMemory",
			err:      alloc.ErrNoMemory,
			resource: true,
			why:      "the record length is data, but a refused allocation is not",
		},
		{
			name:     "wrapped alloc.ErrNoMemory",
			err:      fmt.Errorf("read data: %w", alloc.ErrNoMemory),
			resource: true,
			why:      "wrapping must not change the answer",
		},
		{
			name:     "*os.PathError from the OS driver",
			err:      deviceErr,
			resource: true,
			why:      "the device failed, so records may still follow",
		},
		{
			name:     "wrapped *os.PathError",
			err:      fmt.Errorf("recover: %w", deviceErr),
			resource: true,
			why:      "wrapping must not change the answer",
		},
		{
			name:     "a driver sentinel wrapped by fmt.Errorf",
			err:      fmt.Errorf("read %s: %w", "wal.log", vfstest.ErrInjected),
			resource: true,
			why:      "this is the shape pkg/vfs/vfstest RoleFS returns, and it is not an *os.PathError",
		},
		{
			name:     "an error shape no driver in this repository produces",
			err:      errors.New("some future driver: the disk went away"),
			resource: true,
			why:      "an unrecognised error must be safe by default, or a new driver reopens this gap in silence",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isResourceError(tc.err); got != tc.resource {
				t.Errorf("isResourceError(%v) = %v, want %v: %s", tc.err, got, tc.resource, tc.why)
			}
		})
	}
}
