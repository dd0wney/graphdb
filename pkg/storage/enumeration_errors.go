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

// err returns nil for a complete enumeration, and otherwise an
// *IncompleteEnumeration naming the records that were skipped. `what` names
// the enumeration, so a caller reading a log line can tell which of the seven
// produced it.
func (d *enumerationDamage) err(what string) error {
	if !d.any() {
		return nil
	}
	return &IncompleteEnumeration{what: what, ids: d.ids, cause: d.cause}
}

// IncompleteEnumeration is the error the seven enumeration methods return when
// they skipped a record they could not decode. It always accompanies a partial
// result: the slice beside it holds every record that DID decode (ADR 0003).
//
// EVERY FIELD IS UNEXPORTED, AND THAT IS THE POINT. The only thing a caller can
// read out is the COUNT, through SkippedRecordCount. `pkg/api` must put a count
// and never a record ID into an HTTP response — getNode and
// respondIncompleteEnumeration log the IDs and the snapshot byte offset but
// send a fixed sentence, a rule PR #526 set deliberately. Exporting the ID
// slice would make breaking that rule a one-line mistake in a handler. Keeping
// it unexported means the rule holds by construction rather than by review.
//
// The full detail is still available where it belongs: Error() renders the IDs
// for a log line, and errors.Is reaches ErrRecordUnreadable and the underlying
// cause.
type IncompleteEnumeration struct {
	what  string
	ids   []uint64
	cause error
}

// Error renders the message for an operator's log. It names the IDs.
func (e *IncompleteEnumeration) Error() string {
	return fmt.Sprintf("%s enumeration incomplete: %d record(s) skipped (%s): %v",
		e.what, len(e.ids), e.namedIDs(), e.cause)
}

// Unwrap returns both the class sentinel and the underlying cause, so
// errors.Is(err, ErrRecordUnreadable) holds unconditionally, and
// errors.Is(err, alloc.ErrNoMemory) still separates a refused allocation from
// bit rot. Attaching the sentinel here rather than in the message means the
// guarantee does not depend on how the cause happened to be worded.
func (e *IncompleteEnumeration) Unwrap() []error {
	return []error{ErrRecordUnreadable, e.cause}
}

// SkippedRecordCount reports how many records an enumeration could not decode.
//
// It reports (0, false) for any other error, and for nil. A caller uses the
// boolean to tell "the enumeration was incomplete, and here is a usable partial
// result" from "something else failed, and there is nothing to serve".
//
// The count is what an HTTP or RPC layer may safely pass on to the caller who
// asked. The IDs are not: see the type comment on IncompleteEnumeration.
func SkippedRecordCount(err error) (int, bool) {
	var inc *IncompleteEnumeration
	if errors.As(err, &inc) {
		return len(inc.ids), true
	}
	return 0, false
}

// namedIDs formats up to maxNamedUnreadableIDs of the skipped IDs.
func (e *IncompleteEnumeration) namedIDs() string {
	shown := e.ids
	var more string
	if len(shown) > maxNamedUnreadableIDs {
		shown = shown[:maxNamedUnreadableIDs]
		more = fmt.Sprintf(", and %d more", len(e.ids)-maxNamedUnreadableIDs)
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
