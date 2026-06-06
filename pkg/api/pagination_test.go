package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// TestListNodes_CursorPagination pins the cursor lifecycle on GET /nodes:
// first page (no cursor) sets X-Next-Cursor; supplying that cursor returns
// the next page; the final page omits X-Next-Cursor. Plus invalid-input
// handling and composition with the ?label= filter.
func TestListNodes_CursorPagination(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	// Seed 25 nodes under tenant-A. IDs are assigned monotonically by
	// storage so pagination order is deterministic.
	var ids []uint64
	for i := 0; i < 25; i++ {
		n, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"Doc"}, map[string]storage.Value{
			"i": storage.IntValue(int64(i)),
		})
		ids = append(ids, n.ID)
	}
	// Tenant-B isolation control: 10 nodes that must not appear in
	// tenant-A's pagination.
	for i := 0; i < 10; i++ {
		_, _ = server.graph.CreateNodeWithTenant("tenant-B", []string{"Doc"}, nil)
	}

	// Helper: GET /nodes with the given query, return parsed page + the
	// X-Next-Cursor header (empty string if absent).
	fetchPage := func(t *testing.T, query string) ([]NodeResponse, string) {
		t.Helper()
		req := reqWithTenant(t, http.MethodGet, "/nodes"+query, nil, "tenant-A")
		rr := httptest.NewRecorder()
		server.handleNodes(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s: want 200, got %d body=%s", query, rr.Code, rr.Body.String())
		}
		var got []NodeResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return got, rr.Header().Get(CursorHeader)
	}

	t.Run("first page with limit=10 returns 10 items + cursor", func(t *testing.T) {
		page, next := fetchPage(t, "?limit=10")
		if len(page) != 10 {
			t.Errorf("len(page) = %d, want 10", len(page))
		}
		if next == "" {
			t.Error("X-Next-Cursor missing on non-final page")
		}
		// Page must be sorted by ID ascending.
		for i := 1; i < len(page); i++ {
			if page[i-1].ID >= page[i].ID {
				t.Errorf("page not sorted: [%d]=%d, [%d]=%d", i-1, page[i-1].ID, i, page[i].ID)
			}
		}
	})

	t.Run("cursor walks the full corpus exactly once", func(t *testing.T) {
		seen := make(map[uint64]bool)
		query := "?limit=10"
		for iter := 0; iter < 10; iter++ {
			page, next := fetchPage(t, query)
			for _, n := range page {
				if seen[n.ID] {
					t.Errorf("iter %d: node ID %d appeared twice", iter, n.ID)
				}
				seen[n.ID] = true
			}
			if next == "" {
				break
			}
			query = "?cursor=" + next + "&limit=10"
		}
		if len(seen) != 25 {
			t.Errorf("walked %d distinct items, want 25", len(seen))
		}
		// All seen items must be tenant-A's nodes — cross-tenant isolation.
		for id := range seen {
			found := false
			for _, want := range ids {
				if id == want {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("walked ID %d is not in tenant-A's seed; cross-tenant leak", id)
			}
		}
	})

	t.Run("final page omits X-Next-Cursor", func(t *testing.T) {
		// limit=25 returns all in one shot → no next.
		_, next := fetchPage(t, "?limit=25")
		if next != "" {
			t.Errorf("X-Next-Cursor present on final page: %q", next)
		}
	})

	t.Run("page past end returns empty + no cursor", func(t *testing.T) {
		largeID := ids[len(ids)-1] + 1000
		page, next := fetchPage(t, "?cursor="+strconv.FormatUint(largeID, 10))
		if len(page) != 0 {
			t.Errorf("len(page) past end = %d, want 0", len(page))
		}
		if next != "" {
			t.Errorf("X-Next-Cursor on past-end page: %q", next)
		}
	})

	t.Run("default limit when ?limit= absent", func(t *testing.T) {
		// 25 nodes, default limit is 100 → all returned, no cursor.
		page, next := fetchPage(t, "")
		if len(page) != 25 {
			t.Errorf("default-limit page size = %d, want 25 (default limit covers full corpus)", len(page))
		}
		if next != "" {
			t.Errorf("X-Next-Cursor present when full corpus fits in one default-limit page: %q", next)
		}
	})

	t.Run("composes with ?label=Doc", func(t *testing.T) {
		page, _ := fetchPage(t, "?label=Doc&limit=5")
		if len(page) != 5 {
			t.Errorf("len(page) with label filter = %d, want 5", len(page))
		}
		for _, n := range page {
			foundDoc := false
			for _, l := range n.Labels {
				if l == "Doc" {
					foundDoc = true
				}
			}
			if !foundDoc {
				t.Errorf("page item missing Doc label: %+v", n.Labels)
			}
		}
	})

	t.Run("label cursor walk returns every matching item exactly once", func(t *testing.T) {
		// Walk all 25 Doc-labelled nodes in pages of 8; every item must appear
		// exactly once and all 25 must be seen before the cursor is exhausted.
		seen := make(map[uint64]bool)
		query := "?label=Doc&limit=8"
		for iter := 0; iter < 10; iter++ {
			page, next := fetchPage(t, query)
			for _, n := range page {
				if seen[n.ID] {
					t.Errorf("iter %d: node ID %d appeared twice in label walk", iter, n.ID)
				}
				seen[n.ID] = true
				foundDoc := false
				for _, l := range n.Labels {
					if l == "Doc" {
						foundDoc = true
					}
				}
				if !foundDoc {
					t.Errorf("label walk returned node %d without Doc label", n.ID)
				}
			}
			if next == "" {
				break
			}
			query = "?label=Doc&cursor=" + next + "&limit=8"
		}
		if len(seen) != 25 {
			t.Errorf("label walk saw %d distinct items, want 25", len(seen))
		}
	})

	t.Run("invalid limit/cursor returns 400", func(t *testing.T) {
		cases := []struct {
			name  string
			query string
		}{
			{"non-numeric limit", "?limit=abc"},
			{"zero limit", "?limit=0"},
			{"negative limit", "?limit=-1"},
			{fmt.Sprintf("limit above max (%d)", MaxPageLimit), "?limit=" + strconv.Itoa(MaxPageLimit+1)},
			{"non-numeric cursor", "?cursor=xyz"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := reqWithTenant(t, http.MethodGet, "/nodes"+tc.query, nil, "tenant-A")
				rr := httptest.NewRecorder()
				server.handleNodes(rr, req)
				if rr.Code != http.StatusBadRequest {
					t.Errorf("status: want 400, got %d body=%s", rr.Code, rr.Body.String())
				}
			})
		}
	})
}

// TestListEdges_CursorPagination is the edge-side mirror of
// TestListNodes_CursorPagination — same lifecycle assertions, plus
// composition with the ?from=/?to=/?type= filters from the prior PRs.
func TestListEdges_CursorPagination(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	a1, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"Node"}, nil)
	a2, _ := server.graph.CreateNodeWithTenant("tenant-A", []string{"Node"}, nil)
	// 30 edges from a1 → a2, mixed types. IDs are assigned monotonically
	// by storage so pagination order is deterministic without us tracking
	// them.
	for i := 0; i < 20; i++ {
		_, _ = server.graph.CreateEdgeWithTenant("tenant-A", a1.ID, a2.ID, "KNOWS", nil, 1.0)
	}
	for i := 0; i < 10; i++ {
		_, _ = server.graph.CreateEdgeWithTenant("tenant-A", a1.ID, a2.ID, "LIKES", nil, 1.0)
	}

	fetchPage := func(t *testing.T, query string) ([]EdgeResponse, string) {
		t.Helper()
		req := reqWithTenant(t, http.MethodGet, "/edges"+query, nil, "tenant-A")
		rr := httptest.NewRecorder()
		server.handleEdges(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s: want 200, got %d body=%s", query, rr.Code, rr.Body.String())
		}
		var got []EdgeResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return got, rr.Header().Get(CursorHeader)
	}

	t.Run("cursor walks the full corpus exactly once", func(t *testing.T) {
		seen := make(map[uint64]bool)
		query := "?limit=8"
		for iter := 0; iter < 10; iter++ {
			page, next := fetchPage(t, query)
			for _, e := range page {
				if seen[e.ID] {
					t.Errorf("iter %d: edge ID %d appeared twice", iter, e.ID)
				}
				seen[e.ID] = true
			}
			if next == "" {
				break
			}
			query = "?cursor=" + next + "&limit=8"
		}
		if len(seen) != 30 {
			t.Errorf("walked %d distinct items, want 30", len(seen))
		}
	})

	t.Run("composes with ?type=KNOWS", func(t *testing.T) {
		page, next := fetchPage(t, "?type=KNOWS&limit=10")
		if len(page) != 10 {
			t.Errorf("len(page) with type filter = %d, want 10", len(page))
		}
		for _, e := range page {
			if e.Type != "KNOWS" {
				t.Errorf("page item type = %q, want KNOWS", e.Type)
			}
		}
		if next == "" {
			t.Error("X-Next-Cursor missing on non-final filtered page")
		}
	})

	t.Run("type cursor walk returns every KNOWS edge exactly once", func(t *testing.T) {
		// Walk all 20 KNOWS edges in pages of 7; every item must appear
		// exactly once and all 20 must be seen before the cursor is exhausted.
		seen := make(map[uint64]bool)
		query := "?type=KNOWS&limit=7"
		for iter := 0; iter < 10; iter++ {
			page, next := fetchPage(t, query)
			for _, e := range page {
				if seen[e.ID] {
					t.Errorf("iter %d: edge ID %d appeared twice in type walk", iter, e.ID)
				}
				seen[e.ID] = true
				if e.Type != "KNOWS" {
					t.Errorf("type walk returned edge %d with type %q, want KNOWS", e.ID, e.Type)
				}
			}
			if next == "" {
				break
			}
			query = "?type=KNOWS&cursor=" + next + "&limit=7"
		}
		if len(seen) != 20 {
			t.Errorf("type walk saw %d distinct items, want 20", len(seen))
		}
	})

	t.Run("composes with ?from=A1", func(t *testing.T) {
		page, _ := fetchPage(t, "?from="+strconv.FormatUint(a1.ID, 10)+"&limit=5")
		if len(page) != 5 {
			t.Errorf("len(page) with from filter = %d, want 5", len(page))
		}
		for _, e := range page {
			if e.FromNodeID != a1.ID {
				t.Errorf("page item FromNodeID = %d, want %d", e.FromNodeID, a1.ID)
			}
		}
	})

	t.Run("invalid limit/cursor returns 400", func(t *testing.T) {
		cases := []struct {
			name  string
			query string
		}{
			{"non-numeric limit", "?limit=foo"},
			{"non-numeric cursor", "?cursor=bar"},
			{"limit above max", "?limit=" + strconv.Itoa(MaxPageLimit+1)},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				req := reqWithTenant(t, http.MethodGet, "/edges"+tc.query, nil, "tenant-A")
				rr := httptest.NewRecorder()
				server.handleEdges(rr, req)
				if rr.Code != http.StatusBadRequest {
					t.Errorf("status: want 400, got %d body=%s", rr.Code, rr.Body.String())
				}
			})
		}
	})
}
