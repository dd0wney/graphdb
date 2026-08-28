package api

import (
	"context"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// decodeResultRows post-processes /query result rows before they're
// serialized as the QueryResponse. Without this, RETURN n / RETURN r
// leak raw *storage.Node / *storage.Edge pointers straight into the
// response: those structs have no json tags (fields serialize
// capitalized — "ID", "Properties" — instead of the API's documented
// lower-case shape), and their Properties values are storage.Value,
// whose {Type, Data} fields are the internal typed/binary encoding
// (Data is base64 once JSON-marshalled). storage.Value intentionally
// has no MarshalJSON — that raw shape is load-bearing for
// persistence/WAL elsewhere — so the decoding has to happen here, at
// the API boundary, not on the type itself (#454).
//
// Aggregation scalars (COUNT/SUM/AVG/MIN/MAX) and already-decoded
// property access (n.name) already arrive as native Go values via
// AggregationComputer.ExtractValue / extractStorageValue — they hit
// the default case below and pass through untouched.
//
// Recursive and idempotent by construction: each case dispatches on a
// concrete raw type (storage.Value, *storage.Node, *storage.Edge,
// []any) and anything that isn't one of those — including this
// function's own output, e.g. *NodeResponse — falls through to the
// default pass-through case. Running it twice is therefore safe.
func (s *Server) decodeResultRows(ctx context.Context, rows []map[string]any) []map[string]any {
	decoded := make([]map[string]any, len(rows))
	for i, row := range rows {
		decoded[i] = s.decodeResultRow(ctx, row)
	}
	return decoded
}

// decodeResultRow decodes a single row's values. Split out from
// decodeResultRows for the same reason nodeToResponse/edgeToResponse
// are split from their callers: one row and one value are the natural
// units to reason about, and keeping decodeResultRows itself to a
// simple map-over-rows loop keeps that function trivially readable.
func (s *Server) decodeResultRow(ctx context.Context, row map[string]any) map[string]any {
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[k] = s.decodeResultValue(ctx, v)
	}
	return out
}

// decodeResultValue is the per-value dispatch described in
// decodeResultRows' doc comment.
func (s *Server) decodeResultValue(ctx context.Context, v any) any {
	switch val := v.(type) {
	case storage.Value:
		return valueToInterface(val)
	case *storage.Node:
		return s.nodeToResponse(ctx, val)
	case *storage.Edge:
		return s.edgeToResponse(ctx, val)
	case []any:
		// COLLECT() results: decode each element with the same dispatch
		// so a collected list of nodes/edges/values comes back fully
		// decoded too, not just the top-level row value.
		items := make([]any, len(val))
		for i, item := range val {
			items[i] = s.decodeResultValue(ctx, item)
		}
		return items
	default:
		// Native scalars, nil, already-decoded maps, and this
		// function's own prior output (*NodeResponse, *EdgeResponse)
		// all pass through unchanged — this is what makes a second
		// pass over already-decoded rows a no-op.
		return v
	}
}
