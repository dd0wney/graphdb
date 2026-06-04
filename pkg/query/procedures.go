// Package query — procedure registry + procedure bodies for Cypher CALL ... YIELD.
//
// History:
//   - C3.1 (PR #175): registry SKELETON (Procedure type, empty map,
//     RegisterProcedure exported func) so CallOperator could compile.
//   - C6 (this file): registers the first real procedure, algo.shortestPath,
//     by wiring to pkg/algorithms.ShortestPathForTenant. Per Decision 6 = B
//     (NEXT_STEPS_2026-05-13.md, resolved 2026-05-13), the algorithm takes
//     storage.Storage (interface) so the procedure passes graph through
//     without type assertion.
//
// Future procedures:
//   - gnn.messagePass — skipped; pkg/gnn doesn't exist on OSS (Subset 🟢
//     audit note carries forward).
//   - llm.generate — dropped; pkg/intelligence doesn't exist on OSS
//     (Decision 4 retired 2026-05-13 by package absence).
//   - Backend-specific procedures may register via RegisterProcedure at init.
package query

import (
	"context"
	"fmt"

	"github.com/dd0wney/graphdb/pkg/algorithms"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// Procedure is the function signature for a Cypher procedure callable via
// `CALL procedure_name(args) YIELD items`. The storage.Storage interface
// argument is intentionally the S1-narrowed type; algorithms exposed as
// procedures take the interface (per Decision 6 = B).
type Procedure func(ctx context.Context, graph storage.Storage, tenantID string, args []any) ([]map[string]any, error)

// procedureRegistry is the dispatch table CallOperator looks up by procedure
// name. Last-writer-wins on collisions; not concurrent-safe by design
// (registration is init-time, not runtime).
var procedureRegistry = map[string]Procedure{
	"algo.shortestPath":   shortestPathProcedure,
	"algo.kHop":           kHopProcedure,
	"algo.nodeSimilarity": nodeSimilarityProcedure,
	"algo.linkPrediction": linkPredictionProcedure,
	"algo.pageRank":       pageRankProcedure,
}

// RegisterProcedure adds a procedure to the registry. Exported so external
// packages can register procedures (e.g. enterprise plugin loader, test
// fixtures) without requiring direct map access.
func RegisterProcedure(name string, proc Procedure) {
	procedureRegistry[name] = proc
}

// shortestPathProcedure exposes algorithms.ShortestPathForTenant as the
// CALL algo.shortestPath(startID, endID) YIELD path procedure.
//
// Returns a single result row: [{"path": []uint64}]. If no path exists
// the inner path slice is nil (algorithm returns (nil, nil) for "no path").
func shortestPathProcedure(_ context.Context, graph storage.Storage, tenantID string, args []any) ([]map[string]any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("algo.shortestPath requires 2 arguments (start, end node IDs); got %d", len(args))
	}

	startID, ok := coerceToUint64(args[0])
	if !ok {
		return nil, fmt.Errorf("algo.shortestPath: start argument must be a numeric node ID; got %T", args[0])
	}
	endID, ok := coerceToUint64(args[1])
	if !ok {
		return nil, fmt.Errorf("algo.shortestPath: end argument must be a numeric node ID; got %T", args[1])
	}

	path, err := algorithms.ShortestPathForTenant(graph, startID, endID, tenantID)
	if err != nil {
		return nil, fmt.Errorf("algo.shortestPath: %w", err)
	}

	return []map[string]any{{"path": path}}, nil
}

// kHopProcedure exposes algorithms.KHopNeighboursForTenant as the
// CALL algo.kHop(sourceID, maxHops [, direction [, edgeTypes]]) YIELD
// byHop, distances, totalReachable procedure.
//
// Returns one row: [{"byHop": map[int][]uint64, "distances":
// map[uint64]int, "totalReachable": int}]. Direction defaults to "out";
// accepts "out" | "in" | "both". edgeTypes is an optional list of edge
// type names; nil means "all types."
func kHopProcedure(_ context.Context, graph storage.Storage, tenantID string, args []any) ([]map[string]any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("algo.kHop requires at least 2 arguments (sourceID, maxHops); got %d", len(args))
	}

	sourceID, ok := coerceToUint64(args[0])
	if !ok {
		return nil, fmt.Errorf("algo.kHop: sourceID must be a numeric node ID; got %T", args[0])
	}
	maxHops, ok := coerceToInt(args[1])
	if !ok {
		return nil, fmt.Errorf("algo.kHop: maxHops must be an integer; got %T", args[1])
	}

	opts := algorithms.DefaultKHopOptions()
	opts.MaxHops = maxHops
	opts.Direction = algorithms.DirectionOut

	if len(args) >= 3 {
		dirStr, ok := args[2].(string)
		if !ok {
			return nil, fmt.Errorf("algo.kHop: direction must be a string; got %T", args[2])
		}
		switch dirStr {
		case "out":
			opts.Direction = algorithms.DirectionOut
		case "in":
			opts.Direction = algorithms.DirectionIn
		case "both":
			opts.Direction = algorithms.DirectionBoth
		default:
			return nil, fmt.Errorf("algo.kHop: unknown direction %q (out|in|both)", dirStr)
		}
	}

	if len(args) >= 4 {
		types, ok := coerceToStringSlice(args[3])
		if !ok {
			return nil, fmt.Errorf("algo.kHop: edgeTypes must be a list of strings; got %T", args[3])
		}
		opts.EdgeTypes = types
	}

	result, err := algorithms.KHopNeighboursForTenant(graph, sourceID, opts, tenantID)
	if err != nil {
		return nil, fmt.Errorf("algo.kHop: %w", err)
	}

	return []map[string]any{{
		"byHop":          result.ByHop,
		"distances":      result.Distances,
		"totalReachable": result.TotalReachable,
	}}, nil
}

// nodeSimilarityProcedure exposes algorithms.NodeSimilarityPairForTenant
// as the CALL algo.nodeSimilarity(nodeA, nodeB [, metric]) YIELD score
// procedure.
//
// Returns one row: [{"score": float64}]. Metric defaults to "jaccard";
// accepts "jaccard" | "overlap" | "cosine". Direction is fixed to "out"
// — call sites that need other directions should be added when there's
// a concrete need (the Phase A handoff scope is just the metric).
func nodeSimilarityProcedure(_ context.Context, graph storage.Storage, tenantID string, args []any) ([]map[string]any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("algo.nodeSimilarity requires at least 2 arguments (nodeA, nodeB); got %d", len(args))
	}
	nodeA, ok := coerceToUint64(args[0])
	if !ok {
		return nil, fmt.Errorf("algo.nodeSimilarity: nodeA must be a numeric node ID; got %T", args[0])
	}
	nodeB, ok := coerceToUint64(args[1])
	if !ok {
		return nil, fmt.Errorf("algo.nodeSimilarity: nodeB must be a numeric node ID; got %T", args[1])
	}

	opts := algorithms.DefaultNodeSimilarityOptions()
	if len(args) >= 3 {
		metricStr, ok := args[2].(string)
		if !ok {
			return nil, fmt.Errorf("algo.nodeSimilarity: metric must be a string; got %T", args[2])
		}
		switch metricStr {
		case "jaccard":
			opts.Metric = algorithms.SimilarityJaccard
		case "overlap":
			opts.Metric = algorithms.SimilarityOverlap
		case "cosine":
			opts.Metric = algorithms.SimilarityCosine
		default:
			return nil, fmt.Errorf("algo.nodeSimilarity: unknown metric %q (jaccard|overlap|cosine)", metricStr)
		}
	}

	score, err := algorithms.NodeSimilarityPairForTenant(graph, nodeA, nodeB, opts, tenantID)
	if err != nil {
		return nil, fmt.Errorf("algo.nodeSimilarity: %w", err)
	}
	return []map[string]any{{"score": score}}, nil
}

// linkPredictionProcedure exposes algorithms.PredictLinkScoreForTenant
// as the CALL algo.linkPrediction(fromID, toID [, method]) YIELD score
// procedure.
//
// Returns one row: [{"score": float64}]. Method defaults to
// "commonNeighbours"; accepts "commonNeighbours" | "adamicAdar" |
// "preferentialAttachment". Note that scores across methods are not
// comparable (counts vs weighted sums vs degree products).
func linkPredictionProcedure(_ context.Context, graph storage.Storage, tenantID string, args []any) ([]map[string]any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("algo.linkPrediction requires at least 2 arguments (fromID, toID); got %d", len(args))
	}
	fromID, ok := coerceToUint64(args[0])
	if !ok {
		return nil, fmt.Errorf("algo.linkPrediction: fromID must be a numeric node ID; got %T", args[0])
	}
	toID, ok := coerceToUint64(args[1])
	if !ok {
		return nil, fmt.Errorf("algo.linkPrediction: toID must be a numeric node ID; got %T", args[1])
	}

	opts := algorithms.DefaultLinkPredictionOptions()
	if len(args) >= 3 {
		methodStr, ok := args[2].(string)
		if !ok {
			return nil, fmt.Errorf("algo.linkPrediction: method must be a string; got %T", args[2])
		}
		switch methodStr {
		case "commonNeighbours":
			opts.Method = algorithms.LinkPredCommonNeighbours
		case "adamicAdar":
			opts.Method = algorithms.LinkPredAdamicAdar
		case "preferentialAttachment":
			opts.Method = algorithms.LinkPredPreferentialAttachment
		default:
			return nil, fmt.Errorf("algo.linkPrediction: unknown method %q (commonNeighbours|adamicAdar|preferentialAttachment)", methodStr)
		}
	}

	score, err := algorithms.PredictLinkScoreForTenant(graph, fromID, toID, opts, tenantID)
	if err != nil {
		return nil, fmt.Errorf("algo.linkPrediction: %w", err)
	}
	return []map[string]any{{"score": score}}, nil
}

// pageRankProcedure exposes algorithms.PageRankForTenant as the
// CALL algo.pageRank([damping [, maxIterations]]) YIELD scores procedure.
//
// Returns one row: [{"scores": map[uint64]float64}]. Defaults match
// algorithms.DefaultPageRankOptions (damping=0.85, maxIterations=100,
// tolerance=1e-6).
//
// PageRank cost grows with the tenant's full subgraph; on large tenants
// callers should consider per-tenant rate limiting before exposing this
// at unbounded HTTP surfaces. The handoff doc (Phase A) flags this as a
// follow-up concern, not in-scope for procedure wiring.
func pageRankProcedure(_ context.Context, graph storage.Storage, tenantID string, args []any) ([]map[string]any, error) {
	opts := algorithms.DefaultPageRankOptions()

	if len(args) >= 1 {
		damping, ok := coerceToFloat64(args[0])
		if !ok {
			return nil, fmt.Errorf("algo.pageRank: damping must be a number; got %T", args[0])
		}
		opts.DampingFactor = damping
	}
	if len(args) >= 2 {
		maxIter, ok := coerceToInt(args[1])
		if !ok {
			return nil, fmt.Errorf("algo.pageRank: maxIterations must be an integer; got %T", args[1])
		}
		opts.MaxIterations = maxIter
	}

	result, err := algorithms.PageRankForTenant(graph, opts, tenantID)
	if err != nil {
		return nil, fmt.Errorf("algo.pageRank: %w", err)
	}
	return []map[string]any{{"scores": result.Scores}}, nil
}

// coerceToUint64 accepts the numeric types Cypher's parser can produce for
// node-ID arguments (int64 from integer literals, uint64 from raw bindings,
// float64 from arithmetic). Returns (0, false) for anything that isn't a
// non-negative number.
func coerceToUint64(v any) (uint64, bool) {
	switch n := v.(type) {
	case uint64:
		return n, true
	case int64:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case int:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case float64:
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	default:
		return 0, false
	}
}

// coerceToInt accepts the numeric types Cypher's parser produces for
// integer-valued arguments (maxHops, maxIterations). Negative values
// are returned as-is; algorithm-level bounds enforcement is the caller's
// responsibility (e.g., KHopOptions.MaxHops < 1 rejection).
func coerceToInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

// coerceToFloat64 accepts the numeric types Cypher's parser produces for
// real-valued arguments (damping factor). Integer types are promoted.
func coerceToFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	default:
		return 0, false
	}
}

// coerceToStringSlice accepts the list shapes Cypher's parser can produce
// for string-list arguments (edgeTypes). The parser emits []any from
// list literals (see parser_expressions.go:parseListLiteral), so the []any
// branch is the common case. nil and []string are also accepted for
// programmatic callers and forward compatibility.
func coerceToStringSlice(v any) ([]string, bool) {
	switch s := v.(type) {
	case nil:
		return nil, true
	case []string:
		return s, true
	case []any:
		out := make([]string, len(s))
		for i, e := range s {
			str, ok := e.(string)
			if !ok {
				return nil, false
			}
			out[i] = str
		}
		return out, true
	default:
		return nil, false
	}
}
