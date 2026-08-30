package storage

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// This file holds the machinery behind ADR 0003, "An error return for the
// seven enumeration methods" (docs/adr/0003-enumeration-error-returns.md).
//
// Seven public methods walk a membership list and materialize records. Before
// ADR 0003 each of them skipped a record it could not decode and returned a
// shorter slice with no signal, so an incomplete enumeration was
// byte-indistinguishable from a complete one. They now return a partial result
// BESIDE a non-nil error:
//
//   - the slice holds every record that DID decode, so a caller that wants
//     best-effort data keeps it;
//   - the error is non-nil when any record was skipped, wraps
//     ErrRecordUnreadable, and names the offending IDs;
//   - the error is nil when the enumeration was complete.
//
// Returning nil beside the error was rejected: one damaged record must not make
// a whole tenant unreadable (ADR 0003, alternative 4).

// maxNamedUnreadableIDs caps how many IDs an enumeration error names in its
// message. A tenant whose whole snapshot section rotted would otherwise build
// a very long string on an error path, which is the worst place to allocate
// one. The COUNT stays exact; only the list is truncated, and the message says
// how many it left out.
const maxNamedUnreadableIDs = 8

// unknownRecordID marks a skipped record whose ID the enumeration could not
// recover. Zero is safe as that marker because node and edge IDs come from
// atomic.AddUint64 counters and therefore start at 1, so no real record has
// it. namedIDs renders it as "?" rather than as the number 0, which would read
// as a claim about a record that does not exist.
const unknownRecordID uint64 = 0

// enumerationDamage accumulates the records an enumeration walked past but
// could not produce.
//
// The zero value is ready to use and err() returns nil for it, so an
// enumeration that completed allocates nothing here.
type enumerationDamage struct {
	// ids are the records that did not decode, in the order they were met.
	ids []uint64
	// cause is the FIRST underlying error, kept so errors.Is can still reach
	// past ErrRecordUnreadable to alloc.ErrNoMemory — the two causes of an
	// unreadable record are damage and a refused allocation, and a caller that
	// wants to separate refusal from rot needs the original (errors.go).
	cause error
}

// note records that id could not be produced, and why.
//
// A record that is simply ABSENT is not damage. The enumerations collect IDs
// under gs.mu.RLock and read records under per-shard RLocks afterwards, so a
// record deleted in between is expected to vanish. Skipping it is the
// documented non-atomic-snapshot tradeoff that GetAllNodesForTenant has always
// made, and turning it into an error would make every concurrent delete look
// like corruption. Only an error that is neither ErrNodeNotFound nor
// ErrEdgeNotFound reaches the tally.
//
// id may be unknownRecordID when the enumeration cannot recover an ID for the
// record it could not read. BTreeGraphStorage's edge scan is the one case: its
// key holds the endpoints rather than the edge ID, and its value is the thing
// that failed to decode.
func (d *enumerationDamage) note(id uint64, err error) {
	if err == nil || recordVanished(err) {
		return
	}
	d.ids = append(d.ids, id)
	if d.cause == nil {
		d.cause = err
	}
}

// any reports whether the enumeration skipped at least one record it should
// have been able to produce.
func (d *enumerationDamage) any() bool { return len(d.ids) > 0 }

// err returns nil for a complete enumeration, and otherwise an error that
// wraps ErrRecordUnreadable and names the records that were skipped. `what`
// names the enumeration, so a caller reading a log line can tell which of the
// seven produced it.
func (d *enumerationDamage) err(what string) error {
	if !d.any() {
		return nil
	}
	// The sentinel is attached explicitly only when the cause does not already
	// carry it. Every cause that reaches here through the mmap decoder does
	// carry it (mmap_snapshot_format.go), and wrapping it twice would print
	// "record unreadable" twice in one message. The guarantee that
	// errors.Is(err, ErrRecordUnreadable) holds is the same on both branches;
	// only the wording differs.
	if errors.Is(d.cause, ErrRecordUnreadable) {
		return fmt.Errorf("%s enumeration incomplete: %d record(s) skipped (%s): %w",
			what, len(d.ids), d.namedIDs(), d.cause)
	}
	return fmt.Errorf("%s enumeration incomplete: %d record(s) skipped (%s): %w: %w",
		what, len(d.ids), d.namedIDs(), ErrRecordUnreadable, d.cause)
}

// namedIDs formats up to maxNamedUnreadableIDs of the skipped IDs.
func (d *enumerationDamage) namedIDs() string {
	shown := d.ids
	var more string
	if len(shown) > maxNamedUnreadableIDs {
		shown = shown[:maxNamedUnreadableIDs]
		more = fmt.Sprintf(", and %d more", len(d.ids)-maxNamedUnreadableIDs)
	}
	parts := make([]string, 0, len(shown))
	for _, id := range shown {
		if id == unknownRecordID {
			parts = append(parts, "?")
			continue
		}
		parts = append(parts, strconv.FormatUint(id, 10))
	}
	return strings.Join(parts, ", ") + more
}

// recordVanished reports whether err says the record is simply not there.
//
// Both sentinels are checked regardless of which enumeration asked, because
// the node and edge paths share this helper and a mis-routed sentinel would
// otherwise be reported as damage.
func recordVanished(err error) bool {
	return errors.Is(err, ErrNodeNotFound) || errors.Is(err, ErrEdgeNotFound)
}
