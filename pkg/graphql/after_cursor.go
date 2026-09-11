package graphql

import (
	"fmt"
	"strconv"
)

// listPaging is the pagination shape of one list-field request. It decides
// whether the request can be served index-level (clone only the page, walk in
// ascending ID order) or must fall back to the materialise-everything path.
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

// indexLevel reports whether the storage page methods can serve the request.
// They walk in ascending ID order, which a sort or an offset contradicts.
func (lp listPaging) indexLevel() bool {
	return !lp.hasOffset && !lp.hasOrder
}

// pageFetcher returns one index-level page of at most `limit` items with
// ID > afterID, plus the next cursor (0 on the last page). It is the shape of
// storage.NodesByLabelPageForTenant and storage.EdgesPageForTenant.
type pageFetcher[T any] func(afterID uint64, limit int) ([]T, uint64, error)

// walkPages collects up to `limit` items that pass `keep`, in ascending ID
// order, from successive pages. Every page asks storage for `limit` rows, so a
// filter that drops most rows costs more rounds but never clones more than
// `limit` items per round. The caller's next cursor is the ID of the last
// item returned, which walkPages does not need to know: the items carry it.
func walkPages[T any](afterID uint64, limit int, fetch pageFetcher[T], keep func(T) bool) ([]T, error) {
	out := make([]T, 0, limit)
	if limit <= 0 {
		return out, nil
	}
	for {
		page, next, err := fetch(afterID, limit)
		if err != nil {
			return nil, err
		}
		for _, item := range page {
			if !keep(item) {
				continue
			}
			out = append(out, item)
			if len(out) == limit {
				return out, nil
			}
		}
		if next == 0 {
			return out, nil
		}
		afterID = next
	}
}
