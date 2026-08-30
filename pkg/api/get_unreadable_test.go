package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// GET must tell the OWNER of a damaged record that the record is damaged, and
// must tell everyone else nothing at all.
//
// pkg/storage returns ErrRecordUnreadable when the snapshot directory lists a
// record whose bytes do not decode. getNode and getEdge collapsed every error
// to 404, so the owner — the only principal who can act on the fault — learned
// nothing, and the diagnostic PR #512 added died at the handler.
//
// The two tests below are a pair, and only one of them is the change:
//
//   - TestGetDamagedRecord_OwnerGets500 is the fix. It fails before it.
//   - TestGetDamagedRecord_StrangerStays404 is the GUARD. It passes before and
//     after. A damaged record must stay indistinguishable from an ID that never
//     existed for any caller who does not own it, because the tenant string
//     lives INSIDE the record that would not decode, so a status chosen before
//     ownership is established is a status chosen for a stranger.
//
// The fixture is newDamagedFixture from equivalence_sweep_test.go. It is reused
// rather than copied: it carries a storage-layer positive control that the
// fault took and negative controls that the reopen did not lose the store, so a
// fault injector that quietly stopped working fails there instead of turning
// every assertion here green.

// damagedGetCase is one (kind, path, id) triple. Both tests below walk the same
// two cases — node and edge — because getNode and getEdge are separate handlers
// with separate copies of the mapping, and a guard on one is not a guard on the
// other.
type damagedGetCase struct {
	kind    string
	pathFmt string
	id      func(damagedFixture) uint64
}

var damagedGetCases = []damagedGetCase{
	{kind: "node", pathFmt: "/nodes/%d", id: func(f damagedFixture) uint64 { return f.damagedNode }},
	{kind: "edge", pathFmt: "/edges/%d", id: func(f damagedFixture) uint64 { return f.damagedEdge }},
}

// bodyNamesTheRecord asserts that the response body identifies the record an
// operator has to go and repair, and that it does not carry the decode error's
// own text.
//
// The raw text is excluded deliberately. Today it reads
// "node record at offset 4243: record unreadable: record did not decode" —
// no record bytes, but a byte offset into the customer's snapshot file, which
// is internal layout that no HTTP caller needs. Tomorrow a new decode failure
// could wrap a cause built from the record's own bytes. Pinning the body to a
// fixed sentence written in the handler, rather than to err.Error(), makes that
// a non-event instead of a disclosure.
func bodyNamesTheRecord(t *testing.T, got observable, kind string, id uint64) {
	t.Helper()

	if !strings.Contains(got.body, strconv.FormatUint(id, 10)) {
		t.Errorf("the body does not name %s %d, so an operator cannot tell which "+
			"record to repair: %s", kind, id, got)
	}
	if !strings.Contains(strings.ToLower(got.body), kind) {
		t.Errorf("the body does not say the damaged record is a %s: %s", kind, got)
	}
	for _, leak := range []string{"at offset", "record did not decode", "record unreadable"} {
		if strings.Contains(got.body, leak) {
			t.Errorf("the body carries the decode error's raw text (%q), which is "+
				"internal snapshot detail: %s", leak, got)
		}
	}
}

// TestGetDamagedRecord_OwnerGets500 is the change.
//
// 500 and not 503. 503 tells the caller to try again later; a damaged record on
// disk does not repair itself, so 503 would state something untrue. 500 says
// the server met a condition it cannot fulfil, which is the situation.
func TestGetDamagedRecord_OwnerGets500(t *testing.T) {
	for _, tc := range damagedGetCases {
		t.Run(tc.kind, func(t *testing.T) {
			f := newDamagedFixture(t)
			id := tc.id(f)

			// CONTROL. If the reopen had lost the store, or the fault had
			// damaged the whole file, every read would answer 500 and the
			// assertion below would pass for the wrong reason.
			peer := probe(t, f.server, "owner", http.MethodGet,
				fmt.Sprintf("/nodes/%d", f.ownerPeer), "")
			if peer.status != http.StatusOK {
				t.Fatalf("the owner's UNDAMAGED node must still read 200, so a blanket "+
					"500 cannot be mistaken for the fault under test: got %s", peer)
			}

			got := probe(t, f.server, "owner", http.MethodGet,
				fmt.Sprintf(tc.pathFmt, id), "")

			if got.status != http.StatusInternalServerError {
				t.Errorf("the owner of damaged %s %d got %s, want 500. The owner is the "+
					"only principal who can act on a damaged record, and 404 tells it "+
					"the record is absent, which is false", tc.kind, id, got)
			}
			bodyNamesTheRecord(t, got, tc.kind, id)
		})
	}
}

// TestGetDamagedRecord_StrangerStays404 is the guard, not the change. It must
// pass BEFORE and AFTER the fix.
//
// The comparison is against an ID that never existed, byte for byte, because
// "the stranger got 404" is a weaker claim than "the stranger cannot tell the
// two apart" — two different 404 bodies would still be an existence oracle.
func TestGetDamagedRecord_StrangerStays404(t *testing.T) {
	// Far beyond any ID the fixture creates, and beyond the snapshot's own ID
	// range, so both probes agree that it is absent.
	const neverExisted uint64 = 999999

	for _, tc := range damagedGetCases {
		t.Run(tc.kind, func(t *testing.T) {
			f := newDamagedFixture(t)
			id := tc.id(f)

			// CONTROL. The stranger owns a node of its own. If its membership
			// run were missing, every refusal below would be for the wrong
			// reason and the equality would hold vacuously.
			own := probe(t, f.server, "stranger", http.MethodGet,
				fmt.Sprintf("/nodes/%d", f.strangerPeer), "")
			if own.status != http.StatusOK {
				t.Fatalf("the stranger's OWN node must read 200, or the stranger is being "+
					"refused for a reason this test does not control: got %s", own)
			}

			damaged := probe(t, f.server, "stranger", http.MethodGet,
				fmt.Sprintf(tc.pathFmt, id), "")
			missing := probe(t, f.server, "stranger", http.MethodGet,
				fmt.Sprintf(tc.pathFmt, neverExisted), "")

			if damaged.status != http.StatusNotFound {
				t.Errorf("a stranger asking for damaged %s %d got %s, want 404",
					tc.kind, id, damaged)
			}
			if damaged != missing {
				t.Errorf("existence leak: a DAMAGED %s owned by another tenant is "+
					"distinguishable from one that never existed\n"+
					"  damaged, owned by \"owner\": %s\n"+
					"  never existed:             %s", tc.kind, damaged, missing)
			}
		})
	}
}
