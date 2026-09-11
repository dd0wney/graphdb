package graphql

import (
	"fmt"
	"sort"
	"strconv"
)

// listPaging is the pagination shape of one list-field request. It decides
// whether the request can be served index-level (one storage page call that
// clones only the page) or must fall back to the materialise-everything path.
//
// The ID cursor contract mirrors the REST X-Next-Cursor header: `after` is the
// ID of the last item the caller has seen, and a page shorter than `limit` is
// the last page. A property sort has no ID order, and an offset counts rows
// from a start the cursor already moved, so `after` refuses both instead of
// silently serving a page from the wrong position.
type listPaging struct {
	afterID   uint64
	hasAfter  bool
	hasOffset bool // offset > 0; offset: 0 is the start and means nothing
	hasOrder  bool
}

// parseListPaging reads `after`, `offset` and `orderBy` from the field
// arguments and rejects an `after` cursor that is not an ID, or one paired
// with an argument that contradicts ID order.
func parseListPaging(args map[string]any) (listPaging, error) {
	var lp listPaging
	if offset, ok := args["offset"].(int); ok && offset > 0 {
		lp.hasOffset = true
	}
	lp.hasOrder = parseOrderBy(args) != nil

	afterStr, ok := args["after"].(string)
	if !ok {
		return lp, nil
	}
	id, err := strconv.ParseUint(afterStr, 10, 64)
	if err != nil {
		return lp, fmt.Errorf("invalid after cursor %q: %w", afterStr, err)
	}
	lp.afterID = id
	lp.hasAfter = true
	if lp.hasOrder {
		return lp, fmt.Errorf("after cannot be combined with orderBy: the cursor walks in ID order")
	}
	if lp.hasOffset {
		return lp, fmt.Errorf("after cannot be combined with offset: the cursor already fixes the start")
	}
	return lp, nil
}

// indexLevel reports whether one storage page call can serve the request.
// The page methods walk in ascending ID order, which a sort or an offset
// contradicts, and they cannot apply a `where` filter, so a filtered request
// materialises instead (see seekPastID for why it does not loop over pages).
func (lp listPaging) indexLevel(filtered bool) bool {
	return !lp.hasOffset && !lp.hasOrder && !filtered
}

// seekPastID drops the leading items whose ID is <= afterID. `items` must be
// in ascending ID order, which the materialise path guarantees when no
// orderBy is present (the storage enumerations return sorted IDs). It serves
// `after` on requests with a `where` filter, which must not loop over storage
// pages: every page call re-sorts the full ID set, so a narrow filter would
// turn one scan into N/limit scans.
func seekPastID[T any](items []T, idOf func(T) uint64, afterID uint64) []T {
	start := sort.Search(len(items), func(i int) bool { return idOf(items[i]) > afterID })
	return items[start:]
}
