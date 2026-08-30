package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/storage/storagetest"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// The paginated list endpoints serve the partial page.
//
// ADR 0003 chose a partial result beside the error at the storage layer, and
// rejected (nil, err) because one damaged record must not become a total
// outage for a tenant. The first cut of this API layer then threw that partial
// page away and answered 500, which reintroduced the same outage one level up:
// every page of the tenant's list failed, including intact ones, and the
// caller never got a cursor to step past the gap.
//
// The paginated endpoints now serve the page, answer 200, and set
// X-EnumerationIncomplete to the COUNT of records that would not decode.
//
// The header carries a count and never an ID. pkg/api keeps record IDs and
// snapshot byte offsets out of responses (getNode and
// respondIncompleteEnumeration both log them and send a fixed sentence), and
// storage.SkippedRecordCount is the only accessor, so the IDs are not reachable
// from here even by accident.
//
// The WHOLE-GRAPH enumerations keep the 500. A partial answer to "index every
// document under this label" is an index that is silently wrong, and no header
// makes it right. See TestWholeGraphEnumerationStillRefuses below.

// partialPageFixture is a reopened mmap store holding one tenant with five
// nodes and five edges, of which the SECOND node record and the SECOND edge
// record no longer decode.
//
// Five and two are not arbitrary. With ?limit=2 the scan meets the damaged
// record INSIDE the first page's window and still fills the page from the
// records after it, so one request exercises all three things at once: the
// partial page, the count, and a usable cursor.
type partialPageFixture struct {
	server *Server
	// liveNodes and liveEdges are the IDs that still decode, ascending.
	liveNodes []uint64
	liveEdges []uint64
}

func newPartialPageFixture(t *testing.T, damage bool) partialPageFixture {
	t.Helper()

	dir := t.TempDir()
	cfg := storage.DefaultStorageConfig(dir)
	cfg.UseMmapSnapshot = true // the JSON path has no record to damage

	gs, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	nodeIDs := make([]uint64, 0, 5)
	for i := 0; i < 5; i++ {
		n, cerr := gs.CreateNodeWithTenant("owner", []string{"Thing"},
			map[string]storage.Value{"name": storage.StringValue(fmt.Sprintf("n%d", i))})
		if cerr != nil {
			t.Fatalf("create node %d: %v", i, cerr)
		}
		nodeIDs = append(nodeIDs, n.ID)
	}

	// Every edge joins nodes 0 and 2, which are never damaged. An edge hanging
	// off the damaged node would give the edge rows two possible causes.
	edgeIDs := make([]uint64, 0, 5)
	for i := 0; i < 5; i++ {
		e, cerr := gs.CreateEdgeWithTenant("owner", nodeIDs[0], nodeIDs[2], "LINK", nil, 1)
		if cerr != nil {
			t.Fatalf("create edge %d: %v", i, cerr)
		}
		edgeIDs = append(edgeIDs, e.ID)
	}

	if err := gs.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	liveNodes, liveEdges := nodeIDs, edgeIDs
	if damage {
		storagetest.DamageNodeRecord(t, dir, nodeIDs[1])
		storagetest.DamageEdgeRecord(t, dir, edgeIDs[1])
		liveNodes = []uint64{nodeIDs[0], nodeIDs[2], nodeIDs[3], nodeIDs[4]}
		liveEdges = []uint64{edgeIDs[0], edgeIDs[2], edgeIDs[3], edgeIDs[4]}
	}

	reopened, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	if damage {
		// POSITIVE CONTROL on the injector, taken at the storage layer. A
		// fault that had quietly stopped working would make every assertion
		// below pass for the wrong reason.
		if _, err := reopened.GetNodeForTenant(nodeIDs[1], "owner"); err == nil {
			t.Fatalf("the node fault did not take: node %d still reads", nodeIDs[1])
		}
		if _, err := reopened.GetEdgeForTenant(edgeIDs[1], "owner"); err == nil {
			t.Fatalf("the edge fault did not take: edge %d still reads", edgeIDs[1])
		}
	}
	// NEGATIVE CONTROL: a reopen that lost everything would look like a clean
	// pass on the "serves what decoded" half.
	if _, err := reopened.GetNodeForTenant(nodeIDs[0], "owner"); err != nil {
		t.Fatalf("the first undamaged node must still read: %v", err)
	}

	server, err := NewServerWithDataDir(reopened, 8080, dir)
	if err != nil {
		t.Fatalf("build server: %v", err)
	}

	return partialPageFixture{server: server, liveNodes: liveNodes, liveEdges: liveEdges}
}

// listProbe drives a raw list handler with a tenant in context, exactly as
// equivalence_sweep_test.go's probe does, and returns the whole recorder so a
// caller can read headers as well as the body.
func listProbe(t *testing.T, s *Server, tenantID, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r = r.WithContext(tenant.WithTenant(context.Background(), tenantID))
	w := httptest.NewRecorder()

	mux := http.NewServeMux()
	mux.HandleFunc("/nodes", s.handleNodes)
	mux.HandleFunc("/edges", s.handleEdges)
	mux.ServeHTTP(w, r)
	return w
}

// idsFromBody decodes a list response body into its "id" values, in order.
func idsFromBody(t *testing.T, body []byte) []uint64 {
	t.Helper()
	var items []struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(body, &items); err != nil {
		t.Fatalf("decode body %q: %v", string(body), err)
	}
	got := make([]uint64, 0, len(items))
	for _, it := range items {
		got = append(got, it.ID)
	}
	return got
}

func assertIDs(t *testing.T, what string, got, want []uint64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s returned ids %v, want %v: the page must hold every record that DID decode",
			what, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("%s returned ids %v, want %v", what, got, want)
			return
		}
	}
}

// TestPaginatedListServesPartialPageWhenARecordIsDamaged is the gate for the
// API half of ADR 0003.
//
// Four assertions, and the fourth is the one the change exists for: the caller
// gets a cursor and can fetch the rest. Denying the cursor is what turned a
// localized defect into a total outage for the tenant.
// CONSUMER CONTRACT: CC14-paginated-list-serves-partial-page — all REST consumers (ADR 0003)
func TestPaginatedListServesPartialPageWhenARecordIsDamaged(t *testing.T) {
	t.Run("nodes", func(t *testing.T) {
		f := newPartialPageFixture(t, true)

		got := listProbe(t, f.server, "owner", "/nodes?limit=2")

		if got.Code != http.StatusOK {
			t.Fatalf("status %d, want 200: a damaged record must not deny the caller "+
				"the records that did decode. body=%s", got.Code, got.Body.String())
		}
		assertIDs(t, "GET /nodes?limit=2", idsFromBody(t, got.Body.Bytes()),
			[]uint64{f.liveNodes[0], f.liveNodes[1]})

		if h := got.Header().Get(IncompleteEnumerationHeader); h != "1" {
			t.Errorf("%s = %q, want \"1\": the count of records that would not decode",
				IncompleteEnumerationHeader, h)
		}
		cursor := got.Header().Get(CursorHeader)
		if cursor == "" {
			t.Fatalf("no %s: without a cursor the caller cannot step past the damaged "+
				"record, which is the outage this change exists to stop", CursorHeader)
		}

		// The caller pages on. This is the assertion that separates "serve the
		// partial page" from "serve one partial page and then stop".
		next := listProbe(t, f.server, "owner", "/nodes?limit=2&cursor="+cursor)
		if next.Code != http.StatusOK {
			t.Fatalf("following the cursor gave status %d, want 200. body=%s",
				next.Code, next.Body.String())
		}
		assertIDs(t, "the page after the damaged record",
			idsFromBody(t, next.Body.Bytes()), []uint64{f.liveNodes[2], f.liveNodes[3]})
		if h := next.Header().Get(IncompleteEnumerationHeader); h != "" {
			t.Errorf("%s = %q on a page whose scan met no damaged record, want absent",
				IncompleteEnumerationHeader, h)
		}
	})

	t.Run("edges", func(t *testing.T) {
		f := newPartialPageFixture(t, true)

		got := listProbe(t, f.server, "owner", "/edges?limit=2")

		if got.Code != http.StatusOK {
			t.Fatalf("status %d, want 200. body=%s", got.Code, got.Body.String())
		}
		assertIDs(t, "GET /edges?limit=2", idsFromBody(t, got.Body.Bytes()),
			[]uint64{f.liveEdges[0], f.liveEdges[1]})
		if h := got.Header().Get(IncompleteEnumerationHeader); h != "1" {
			t.Errorf("%s = %q, want \"1\"", IncompleteEnumerationHeader, h)
		}
		if got.Header().Get(CursorHeader) == "" {
			t.Fatalf("no %s on the edge page", CursorHeader)
		}
	})

	// ?type= routes through EdgesByTypePageForTenant, a different storage
	// method from the unfiltered branch above. A guard on one is not a guard
	// on the other — the same reasoning as rows 7 and 8 of the equivalence
	// sweep.
	t.Run("edges by type", func(t *testing.T) {
		f := newPartialPageFixture(t, true)

		got := listProbe(t, f.server, "owner", "/edges?type=LINK&limit=2")

		if got.Code != http.StatusOK {
			t.Fatalf("status %d, want 200. body=%s", got.Code, got.Body.String())
		}
		if h := got.Header().Get(IncompleteEnumerationHeader); h != "1" {
			t.Errorf("%s = %q, want \"1\"", IncompleteEnumerationHeader, h)
		}
	})

	// ?label= routes through NodesByLabelPageForTenant, likewise.
	t.Run("nodes by label", func(t *testing.T) {
		f := newPartialPageFixture(t, true)

		got := listProbe(t, f.server, "owner", "/nodes?label=Thing&limit=2")

		if got.Code != http.StatusOK {
			t.Fatalf("status %d, want 200. body=%s", got.Code, got.Body.String())
		}
		if h := got.Header().Get(IncompleteEnumerationHeader); h != "1" {
			t.Errorf("%s = %q, want \"1\"", IncompleteEnumerationHeader, h)
		}
	})
}

// TestPaginatedListOmitsTheHeaderOnAnIntactStore is the NEGATIVE CONTROL.
//
// A header that is always present carries no information. Without this test an
// implementation that set X-Enumeration-Incomplete on every response would
// pass every row above while telling the caller nothing.
// CONSUMER CONTRACT: CC14-paginated-list-serves-partial-page — all REST consumers (ADR 0003)
func TestPaginatedListOmitsTheHeaderOnAnIntactStore(t *testing.T) {
	f := newPartialPageFixture(t, false)

	for _, path := range []string{
		"/nodes?limit=2",
		"/nodes?label=Thing&limit=2",
		"/edges?limit=2",
		"/edges?type=LINK&limit=2",
	} {
		got := listProbe(t, f.server, "owner", path)
		if got.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d, want 200. body=%s", path, got.Code, got.Body.String())
		}
		if h := got.Header().Get(IncompleteEnumerationHeader); h != "" {
			t.Errorf("GET %s set %s = %q on an intact store: a header that is always "+
				"present is not a signal", path, IncompleteEnumerationHeader, h)
		}
	}

	// The store really does hold five of each, so the rows above are not
	// passing because the fixture is empty.
	got := listProbe(t, f.server, "owner", "/nodes?limit=1000")
	if ids := idsFromBody(t, got.Body.Bytes()); len(ids) != 5 {
		t.Fatalf("the intact fixture returned %d nodes, want 5", len(ids))
	}
}

// TestWholeGraphEnumerationStillRefuses pins the OTHER half of the decision.
//
// The paginated endpoints serve a partial page because a page is inherently a
// fragment and the caller holds a cursor for the rest. A whole-graph
// enumeration has neither property: "index every document under this label"
// has no partial answer that means anything, and a search index built from a
// short corpus answers "no match" for the skipped documents until somebody
// rebuilds it. Those endpoints keep the 500.
//
// Without this test, a later change that pushed the partial-result treatment
// everywhere would look like a consistency improvement.
func TestWholeGraphEnumerationStillRefuses(t *testing.T) {
	f := newPartialPageFixture(t, true)

	body := `{"labels":["Thing"],"properties":["name"]}`
	r := httptest.NewRequest(http.MethodPost, "/search/index", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r = r.WithContext(tenant.WithTenant(context.Background(), "owner"))
	w := httptest.NewRecorder()
	f.server.handleSearchIndex(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("POST /search/index gave status %d, want 500: a search index built "+
			"from a short corpus is silently wrong for the life of the index. body=%s",
			w.Code, w.Body.String())
	}
	if h := w.Header().Get(IncompleteEnumerationHeader); h != "" {
		t.Errorf("the refusal path set %s = %q: the header belongs to the paginated "+
			"endpoints, which serve a usable fragment", IncompleteEnumerationHeader, h)
	}
}
