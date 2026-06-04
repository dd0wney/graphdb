// Package retrieval implements graph-augmented retrieval: a
// LangChain-style "Retriever" interface backed by hybrid search +
// graph traversal. See docs/F2_GRAPHRAG_DESIGN.md for the design.
//
// Algorithm sketch:
//  1. Seed retrieval — tenant-scoped hybrid search (FTS + LSA via RRF)
//  2. Multi-source BFS expansion from seeds (bounded by MaxHops AND a
//     hard 50-node cap; dense graphs at 3 hops can return thousands)
//  3. Score combination — alpha * normalized_seed_score + beta * exp(-d/tau)
//     where d is graph-distance from the contributing seed
//  4. Top-K filter
//  5. Token-budget drop (lowest-score chunks dropped whole)
//
// Tenant scoping rests on audit Track A (PRs #17-#27): every storage
// call uses *ForTenant. The pkg/api/audit_regression_test.go suite
// (A7 #27) catches any cross-tenant regression at the contract level.
package retrieval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/dd0wney/graphdb/pkg/search"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// Defaults from F2 spike (#28) §2 Q4–Q6. Configurable per-request via
// Options; out-of-range values are clamped or replaced.
const (
	DefaultK         = 10
	DefaultMaxTokens = 4096
	DefaultMaxHops   = 2
	DefaultAlpha     = 0.7
	DefaultBeta      = 0.3
	DefaultTau       = 2.0

	// HardNodeCap is the absolute ceiling on visited nodes during BFS
	// expansion, regardless of MaxHops or the candidate pool. Prevents
	// pathological blow-up in dense graphs where 3-hop expansion can
	// return thousands of nodes. Quality-impacting (may hide relevant
	// nodes in dense graphs) but correctness-preserving.
	HardNodeCap = 50

	// hybridOverFetchMultiplier scales the OverFetchK passed to
	// hybrid search. Seeds drive expansion, so a slightly wider seed
	// pool helps the BFS find more diverse candidates.
	hybridOverFetchMultiplier = 3

	// hybridSeedAlpha is the FTS-vs-LSA balance for seed retrieval.
	// Independent from Options.Alpha (which is the seed-vs-graph
	// score weight in the final ranking).
	hybridSeedAlpha = 0.5
)

// Options configures one retrieval call. Zero values fall back to
// the package defaults (see constants above).
type Options struct {
	// K caps the number of chunks returned (after token-budget drop).
	K int

	// MaxTokens is the token budget. Chunks are dropped whole, lowest
	// score first, until the total estimated tokens fits.
	MaxTokens int

	// MaxHops bounds the BFS depth from any seed. 0 = seeds only.
	MaxHops int

	// Alpha weights the normalized seed score (0..1) in the final
	// ranking. Beta weights the graph-distance term exp(-d/Tau).
	// Together they need not sum to 1 — they're independent dials.
	Alpha float64
	Beta  float64
	Tau   float64

	// Labels optionally restricts seed-stage candidates to nodes with
	// at least one of these labels. Expansion is unrestricted (the
	// graph signal is the point; labels apply at seed-time only).
	Labels []string
}

// applyDefaults fills in zero/negative fields with package defaults.
// Mutates the receiver — callers pass by value, this gets called on
// the local copy.
func (o *Options) applyDefaults() {
	if o.K <= 0 {
		o.K = DefaultK
	}
	if o.MaxTokens <= 0 {
		o.MaxTokens = DefaultMaxTokens
	}
	if o.MaxHops < 0 {
		o.MaxHops = DefaultMaxHops
	}
	if o.Alpha < 0 || o.Alpha > 1 {
		o.Alpha = DefaultAlpha
	}
	if o.Beta < 0 || o.Beta > 1 {
		o.Beta = DefaultBeta
	}
	if o.Tau <= 0 {
		o.Tau = DefaultTau
	}
}

// Chunk is one ranked retrieval result. Content is the text payload
// (suitable for an LLM prompt); SourcePath is the BFS path from the
// contributing seed to this node — the graph-specific signal that
// distinguishes graph-augmented retrieval from plain vector RAG.
//
// Without SourcePath, downstream consumers can't explain *why* a
// chunk is in context. F2 spike §2 Q6 calls this out as the
// load-bearing graph signal.
type Chunk struct {
	NodeID     uint64   `json:"node_id"`
	Score      float64  `json:"score"`
	Content    string   `json:"content"`
	Label      string   `json:"label,omitempty"` // first label, hint for downstream
	SourcePath []uint64 `json:"source_path"`     // [seedID, ..., NodeID]
}

// Result bundles ranked chunks + diagnostics. Degraded forwards the
// hybrid-search degraded flag ("no-lsa-index", "query-out-of-vocabulary")
// so callers can surface degradation reasons. TookMs is wall-clock for
// the full retrieval pipeline.
type Result struct {
	Chunks   []Chunk
	Degraded string
	TookMs   int64
}

// Retriever holds the dependencies needed to perform retrieval.
// Construct one per Server (it's stateless beyond the references
// it holds; safe to share across requests).
type Retriever struct {
	Graph     *storage.GraphStorage
	SearchIdx *search.TenantIndexes
	LSAIdx    *search.TenantLSAIndexes
}

// NewRetriever wires the retrieval primitives.
func NewRetriever(graph *storage.GraphStorage, searchIdx *search.TenantIndexes, lsaIdx *search.TenantLSAIndexes) *Retriever {
	return &Retriever{
		Graph:     graph,
		SearchIdx: searchIdx,
		LSAIdx:    lsaIdx,
	}
}

// Retrieve performs graph-augmented retrieval for the caller's tenant.
// Returns ranked chunks ready for LLM context injection.
//
// All graph access uses *ForTenant variants (audit Track A). The
// returned chunks include SourcePath metadata — the BFS path from the
// contributing seed — for downstream citation / explanation.
func (r *Retriever) Retrieve(ctx context.Context, query, tenantID string, opts Options) (*Result, error) {
	opts.applyDefaults()
	start := time.Now()

	// 1. Seed retrieval via hybrid search.
	hybridRes, err := search.SearchHybridForTenant(r.SearchIdx, r.LSAIdx, tenantID, query, search.HybridSearchOpts{
		OverFetchK: opts.K * hybridOverFetchMultiplier,
		Alpha:      hybridSeedAlpha,
	})
	if err != nil {
		return nil, fmt.Errorf("retrieval seed search: %w", err)
	}
	seeds := hybridRes.Hits

	// Optional seed-stage label filter. Expansion is unrestricted —
	// the graph signal is the point.
	if len(opts.Labels) > 0 {
		seeds = filterSeedsByLabel(r.Graph, tenantID, seeds, opts.Labels)
	}

	if len(seeds) == 0 {
		return &Result{
			Chunks:   nil,
			Degraded: hybridRes.Degraded,
			TookMs:   time.Since(start).Milliseconds(),
		}, nil
	}

	// Normalize seed scores to [0, 1] using the top score. Empty
	// hybrid result is handled above; top score is always > 0
	// (zero-score candidates dropped by SearchHybridForTenant).
	maxSeedScore := seeds[0].Score

	// 2. Multi-source BFS expansion. distance[id] = hop count from
	// the contributing seed; predecessor[id] = previous node on the
	// BFS path (used to reconstruct SourcePath).
	distance := make(map[uint64]int, len(seeds))
	predecessor := make(map[uint64]uint64, len(seeds))
	contributingSeed := make(map[uint64]uint64, len(seeds))
	seedScore := make(map[uint64]float64, len(seeds))

	frontier := make([]uint64, 0, len(seeds))
	for _, s := range seeds {
		distance[s.NodeID] = 0
		predecessor[s.NodeID] = s.NodeID // seed maps to itself
		contributingSeed[s.NodeID] = s.NodeID
		seedScore[s.NodeID] = s.Score / maxSeedScore // normalized
		frontier = append(frontier, s.NodeID)
	}

	for hop := 1; hop <= opts.MaxHops && len(distance) < HardNodeCap; hop++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		next := frontier[:0:0] // reset, retain backing array unsuitable since we mutate distance during the loop
		for _, nodeID := range frontier {
			if len(distance) >= HardNodeCap {
				break
			}
			edges, err := r.Graph.GetOutgoingEdgesForTenant(nodeID, tenantID)
			if err != nil {
				continue
			}
			for _, edge := range edges {
				if _, seen := distance[edge.ToNodeID]; seen {
					continue
				}
				distance[edge.ToNodeID] = hop
				predecessor[edge.ToNodeID] = nodeID
				contributingSeed[edge.ToNodeID] = contributingSeed[nodeID]
				next = append(next, edge.ToNodeID)
				if len(distance) >= HardNodeCap {
					break
				}
			}
		}
		frontier = next
		if len(frontier) == 0 {
			break // no further reach
		}
	}

	// 3. Score every visited node.
	type ranked struct {
		ID    uint64
		Score float64
	}
	rankedAll := make([]ranked, 0, len(distance))
	for nodeID, d := range distance {
		seedID := contributingSeed[nodeID]
		s := opts.Alpha*seedScore[seedID] + opts.Beta*math.Exp(-float64(d)/opts.Tau)
		rankedAll = append(rankedAll, ranked{ID: nodeID, Score: s})
	}

	sort.Slice(rankedAll, func(i, j int) bool {
		if rankedAll[i].Score != rankedAll[j].Score {
			return rankedAll[i].Score > rankedAll[j].Score
		}
		return rankedAll[i].ID < rankedAll[j].ID // determinism on score ties
	})

	// 4. Trim to K *before* hydrating — avoids fetching nodes we
	// won't return. Token-budget trim happens after hydration since
	// it depends on content length.
	if len(rankedAll) > opts.K {
		rankedAll = rankedAll[:opts.K]
	}

	// 5. Hydrate + build chunks. Skip nodes we can't fetch (deleted
	// or out-of-tenant — both surface as ErrNodeNotFound from
	// GetNodeForTenant) and nodes with empty content.
	fts := r.SearchIdx.Get(tenantID)
	chunks := make([]Chunk, 0, len(rankedAll))
	for _, r2 := range rankedAll {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		node, err := r.Graph.GetNodeForTenant(r2.ID, tenantID)
		if err != nil {
			continue
		}
		content := chunkContent(node, fts)
		if content == "" {
			continue
		}
		chunks = append(chunks, Chunk{
			NodeID:     r2.ID,
			Score:      r2.Score,
			Content:    content,
			Label:      firstLabel(node),
			SourcePath: buildSourcePath(r2.ID, predecessor),
		})
	}

	// 6. Token-budget drop. Chunks are already sorted by score desc;
	// drop from the tail until total estimated tokens ≤ MaxTokens.
	chunks = applyTokenBudget(chunks, opts.MaxTokens)

	return &Result{
		Chunks:   chunks,
		Degraded: hybridRes.Degraded,
		TookMs:   time.Since(start).Milliseconds(),
	}, nil
}

// filterSeedsByLabel keeps only seeds whose node has at least one of
// the requested labels. Hydrates LSA-only seeds via GetNodeForTenant.
func filterSeedsByLabel(graph *storage.GraphStorage, tenantID string, seeds []search.HybridHit, labels []string) []search.HybridHit {
	wanted := make(map[string]struct{}, len(labels))
	for _, l := range labels {
		wanted[l] = struct{}{}
	}
	out := seeds[:0]
	for _, s := range seeds {
		var nodeLabels []string
		if s.FTSNode != nil {
			nodeLabels = s.FTSNode.Labels
		} else {
			node, err := graph.GetNodeForTenant(s.NodeID, tenantID)
			if err != nil {
				continue
			}
			nodeLabels = node.Labels
		}
		for _, nl := range nodeLabels {
			if _, ok := wanted[nl]; ok {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// chunkContent picks the best text representation of a node for an
// LLM prompt. Prefers FTS-indexed content (the canonical "what this
// node is about" text); falls back to a JSON-stringified properties
// dump for nodes that aren't in the FTS index (e.g., expanded via
// traversal across an unindexed label).
//
// Returns "" if no useful content is available — the caller drops
// the chunk.
func chunkContent(node *storage.Node, fts *search.FullTextIndex) string {
	if fts != nil {
		if content, ok := fts.NodeContent(node.ID); ok && content != "" {
			return content
		}
	}
	// Fallback: stringify properties. Use JSON for stable ordering
	// (Properties is a map, so direct iteration is non-deterministic).
	if len(node.Properties) == 0 {
		return ""
	}
	flat := make(map[string]any, len(node.Properties))
	for k, v := range node.Properties {
		// Best-effort string rendering. AsString fails for non-string
		// types; fall through to a generic "%v"-equivalent.
		if s, err := v.AsString(); err == nil {
			flat[k] = s
		} else {
			flat[k] = string(v.Data)
		}
	}
	b, err := json.Marshal(flat)
	if err != nil {
		return ""
	}
	return string(b)
}

// firstLabel returns the node's first label, or "" if it has none.
// Used as a hint in Chunk metadata so the LLM (or app code) can group
// chunks by type without re-fetching.
func firstLabel(node *storage.Node) string {
	if len(node.Labels) == 0 {
		return ""
	}
	return node.Labels[0]
}

// buildSourcePath walks the BFS predecessor map from nodeID back to
// the seed (where predecessor[seed] == seed) and returns the path in
// forward order: [seed, ..., nodeID]. For a seed itself, returns
// [seedID] (length 1).
func buildSourcePath(nodeID uint64, predecessor map[uint64]uint64) []uint64 {
	rev := []uint64{nodeID}
	cur := nodeID
	for {
		prev, ok := predecessor[cur]
		if !ok || prev == cur {
			break
		}
		rev = append(rev, prev)
		cur = prev
	}
	// Reverse in place.
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}

// applyTokenBudget drops chunks from the lowest-scored end until the
// total estimated token count is within budget. Chunks are dropped
// whole — no within-chunk truncation, which preserves citation
// integrity (per F2 spike §2 Q5).
//
// If the highest-scored chunk alone exceeds the budget, it's still
// returned (single-chunk-exceeds-budget case from spike §6 risk #1).
// Caller decides whether to surface this as a degraded flag.
func applyTokenBudget(chunks []Chunk, maxTokens int) []Chunk {
	total := 0
	for i, c := range chunks {
		t := estimateTokens(c.Content)
		if i > 0 && total+t > maxTokens {
			return chunks[:i]
		}
		total += t
	}
	return chunks
}

// estimateTokens approximates the LLM token count for content using a
// word-count × 1.3 heuristic (per F2 spike §2 Q5). A real tokenizer
// (tiktoken, sentencepiece) is a v2 candidate if budget overruns
// become a customer issue.
func estimateTokens(content string) int {
	words := len(strings.Fields(content))
	return int(float64(words) * 1.3)
}
