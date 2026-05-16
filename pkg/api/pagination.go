package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// DefaultPageLimit is the page size when ?limit= is absent.
const DefaultPageLimit = 100

// MaxPageLimit caps any caller-supplied ?limit= value to prevent
// memory pressure from "give me everything in one request" DOS shapes.
// 1000 matches the GraphQL MaxLimit default in pkg/graphql.
const MaxPageLimit = 1000

// CursorHeader is the response header name for the next-page cursor.
// Absent from the response when the current page is the last one.
const CursorHeader = "X-Next-Cursor"

// pageRequest captures the parsed ?cursor= and ?limit= query parameters
// for a paginated list endpoint. Zero-value cursor means "start from the
// beginning"; zero-value limit is replaced with DefaultPageLimit.
type pageRequest struct {
	cursor uint64
	limit  int
}

// parsePageRequest extracts ?cursor= and ?limit= from the request.
// Empty values are treated as absent (defaults applied). Returns an
// HTTP status + message pair when present-but-malformed; the caller
// responds with that status.
//
// Contract:
//   - cursor must parse as uint64 (the cursor is just the ID of the last
//     item from the previous page). Non-numeric values are caller bugs;
//     400 surfaces them early rather than silently degrading to "page 1."
//   - limit must be a positive integer in [1, MaxPageLimit]. Zero or
//     negative is a caller bug; > MaxPageLimit is rejected to prevent
//     DOS.
//   - Both empty → cursor=0 (start), limit=DefaultPageLimit.
func parsePageRequest(r *http.Request) (pageRequest, int, string) {
	p := pageRequest{limit: DefaultPageLimit}
	q := r.URL.Query()

	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return pageRequest{}, http.StatusBadRequest, "limit must be a positive integer"
		}
		if n > MaxPageLimit {
			return pageRequest{}, http.StatusBadRequest, fmt.Sprintf("limit must be at most %d", MaxPageLimit)
		}
		p.limit = n
	}

	if s := q.Get("cursor"); s != "" {
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return pageRequest{}, http.StatusBadRequest, "cursor must be a valid integer"
		}
		p.cursor = n
	}

	return p, 0, ""
}

// paginateNodes returns the requested page of nodes (sorted by ID
// ascending, items with ID > cursor) plus the next cursor value
// (0 if the returned page is the last one).
//
// The sort is in-place on the caller's slice — pass a slice you own
// (the storage primitives return fresh slices, so this is safe at the
// call site). Sort cost is O(N log N) on the materialized list; for
// the API surface today this is acceptable since the list is already
// materialized before pagination. When pagination moves into storage,
// the sort moves with it and this helper retires.
func paginateNodes(items []*storage.Node, p pageRequest) ([]*storage.Node, uint64) {
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	start := 0
	for start < len(items) && items[start].ID <= p.cursor {
		start++
	}
	end := start + p.limit
	if end > len(items) {
		end = len(items)
	}
	page := items[start:end]
	var next uint64
	if end < len(items) {
		// next cursor = ID of the last item on the current page; the
		// next request will return items with ID > this value.
		next = page[len(page)-1].ID
	}
	return page, next
}

// paginateEdges is the edge equivalent of paginateNodes. Edges are
// sorted by ID ascending (Edge.ID is a uint64 like Node.ID); cursor
// semantics are identical.
func paginateEdges(items []*storage.Edge, p pageRequest) ([]*storage.Edge, uint64) {
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	start := 0
	for start < len(items) && items[start].ID <= p.cursor {
		start++
	}
	end := start + p.limit
	if end > len(items) {
		end = len(items)
	}
	page := items[start:end]
	var next uint64
	if end < len(items) {
		next = page[len(page)-1].ID
	}
	return page, next
}

// writeNextCursor sets the X-Next-Cursor header on the response when a
// next page exists. Centralized so the header name is defined once.
func writeNextCursor(w http.ResponseWriter, next uint64) {
	if next != 0 {
		w.Header().Set(CursorHeader, strconv.FormatUint(next, 10))
	}
}
