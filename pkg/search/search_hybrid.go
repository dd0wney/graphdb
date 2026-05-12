package search

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// HybridSearchOpts configures the RRF-merged hybrid search.
//
// Audit F2 #2 (2026-05-08): factored from
// pkg/api/handlers_hybrid_search.go's inline RRF composition so
// non-handler callers (notably pkg/retrieval/ for F2 GraphRAG) can
// invoke hybrid search without duplicating the merge logic.
type HybridSearchOpts struct {
	// OverFetchK is the per-stage candidate pool size. Caller
	// typically sets it to ~3× the desired final result count to
	// give the RRF merge enough overlap to discriminate.
	OverFetchK int

	// Alpha weights FTS vs LSA contribution. 0.5 is balanced.
	// 1.0 = FTS only, 0.0 = LSA only. Out-of-range values are
	// clamped to [0, 1].
	Alpha float64
}

// HybridHit is one RRF-merged candidate. FTSRank and LSARank expose
// the per-stage rank so callers can see why a candidate scored where
// it did; -1 indicates the stage did not return this candidate.
//
// FTSNode is the storage node when the FTS stage returned it. LSA-only
// candidates have FTSNode == nil; the caller is responsible for
// hydrating them via storage if needed (we don't pull
// storage.Storage into pkg/search just for that — it's a
// caller-side concern).
type HybridHit struct {
	NodeID  uint64
	Score   float64
	FTSRank int
	LSARank int
	FTSNode *storage.Node
}

// HybridSearchResult bundles the merged hits with a degraded
// indicator. Degraded is non-empty when the hybrid path fell back to
// a single stage:
//
//	"no-lsa-index"             — tenant has no LSA index built
//	"query-out-of-vocabulary"  — query terms aren't in the LSA vocab
//
// Note: "no-fts-match" is not surfaced here — an empty FTS top-k is
// not a degradation, it's just a result. The caller may choose to
// flag this externally based on len(Hits) and Degraded together.
type HybridSearchResult struct {
	Hits     []HybridHit
	Degraded string
}

// rrfK is the Reciprocal Rank Fusion constant from Cormack et al.
// 2009. Same constant as the original handler — moved here as the
// canonical home now that hybrid search has multiple consumers.
const rrfK = 60

// SearchHybridForTenant performs RRF-merged FTS + LSA search scoped
// to a single tenant. The caller supplies the per-tenant index
// containers; this function does the per-tenant Get internally.
//
// Returns the merged candidate list plus a degraded indicator. The
// caller is responsible for:
//   - label / property post-filters (need storage access)
//   - pagination (offset / limit beyond the candidate pool)
//   - node hydration of LSA-only hits if needed
//
// This split keeps pkg/search as the merge primitive and pushes
// API-shaped concerns (HTTP, storage hydration, response shape) to
// the caller. F2's pkg/retrieval/ uses this directly for GraphRAG
// seed retrieval; pkg/api/handlers_hybrid_search.go also calls it.
func SearchHybridForTenant(
	searchIdx *TenantIndexes,
	lsaIdx *TenantLSAIndexes,
	tenantID, query string,
	opts HybridSearchOpts,
) (*HybridSearchResult, error) {
	alpha := opts.Alpha
	if alpha < 0 {
		alpha = 0
	}
	if alpha > 1 {
		alpha = 1
	}

	overFetchK := opts.OverFetchK
	if overFetchK <= 0 {
		overFetchK = rrfK
	}

	fts := searchIdx.Get(tenantID)
	ftsResults, err := fts.SearchTopK(query, overFetchK)
	if err != nil {
		return nil, err
	}

	ftsRank := make(map[uint64]int, len(ftsResults))
	for i, r := range ftsResults {
		ftsRank[r.NodeID] = i
	}
	ftsNodeByID := make(map[uint64]*storage.Node, len(ftsResults))
	for _, r := range ftsResults {
		if r.Node != nil {
			ftsNodeByID[r.NodeID] = r.Node
		}
	}

	var lsaResults []LSAResult
	var degraded string
	lsa := lsaIdx.Get(tenantID)
	if lsa == nil {
		degraded = "no-lsa-index"
	} else {
		qvec, _, foldErr := lsa.FoldQuery(query)
		if foldErr != nil {
			degraded = "query-out-of-vocabulary"
		} else {
			lsaResults, err = lsa.TopKByVector(qvec, overFetchK)
			if err != nil {
				return nil, err
			}
		}
	}

	lsaRank := make(map[uint64]int, len(lsaResults))
	for i, r := range lsaResults {
		lsaRank[r.NodeID] = i
	}

	// RRF merge over union of candidates.
	candidates := make(map[uint64]struct{}, len(ftsRank)+len(lsaRank))
	for id := range ftsRank {
		candidates[id] = struct{}{}
	}
	for id := range lsaRank {
		candidates[id] = struct{}{}
	}

	hits := make([]HybridHit, 0, len(candidates))
	for id := range candidates {
		hit := HybridHit{NodeID: id, FTSRank: -1, LSARank: -1, FTSNode: ftsNodeByID[id]}
		if r, ok := ftsRank[id]; ok {
			hit.Score += alpha / float64(rrfK+r+1)
			hit.FTSRank = r
		}
		if r, ok := lsaRank[id]; ok {
			hit.Score += (1 - alpha) / float64(rrfK+r+1)
			hit.LSARank = r
		}
		// At alpha=1.0 (or 0.0), one stage contributes nothing to
		// any candidate. Candidates appearing only in the
		// contribution-free stage have score=0; drop them rather
		// than trail the meaningful results as zero-score noise.
		if hit.Score <= 0 {
			continue
		}
		hits = append(hits, hit)
	}

	sortHybridHits(hits)
	return &HybridSearchResult{Hits: hits, Degraded: degraded}, nil
}

// sortHybridHits sorts hits by score desc, then by NodeID asc for
// determinism (otherwise score-ties order is map-iteration random).
func sortHybridHits(hits []HybridHit) {
	// Local insertion sort — len(hits) is bounded by 2*OverFetchK
	// which the caller typically caps at a few hundred. Avoiding
	// sort.Slice keeps this allocation-free.
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hybridLess(hits[j], hits[j-1]); j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
}

func hybridLess(a, b HybridHit) bool {
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	return a.NodeID < b.NodeID
}
