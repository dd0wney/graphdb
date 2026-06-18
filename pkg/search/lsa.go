package search

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"unicode"
)

// LSA (Latent Semantic Analysis) semantic index — pure-Go implementation.
//
// Ported from obsidian-wiki/tools/wiki-graph/vectors.go. Uses randomized truncated
// SVD (Halko et al. 2011) to project a document corpus into a low-dimensional
// latent space that captures synonym and co-occurrence structure. Co-resident
// BM25 inverted index supports exact-match ranking as the complementary signal
// for hybrid search.
//
// Three live constraints to know before using:
//
//  1. Not incremental. Any CreateNode/UpdateNode that adds new vocabulary or
//     shifts document frequencies invalidates the model. Callers must rebuild
//     the index (typically at startup or on a schedule). For small corpora
//     (~2.5k docs) this takes 2-8s; scales roughly O(D × T × l) where l ≈ 210.
//
//  2. Deterministic. Seed is fixed in LSAConfig (default 42), so the same
//     corpus produces byte-identical document vectors across rebuilds. This
//     is a feature: downstream callers can cache vectors keyed on corpus hash.
//
//  3. Tenant scoping is provided by the caller, not by this struct. Per-tenant
//     isolation lives in the registry layer above (see tenant_lsa_indexes.go,
//     pkg/api/handlers_search_admin.go's tenant-scoped corpus assembly via
//     GetNodesByLabelForTenant, and pkg/api/handlers_embeddings.go's per-tenant
//     read path). The LSAIndex itself is a pure data structure with no tenancy
//     awareness — pass it documents from one tenant and it will produce a
//     model for that tenant. Verified by TestEmbeddings_TenantIsolation in
//     pkg/api/handlers_embeddings_test.go.

// Document is the input shape for BuildLSAIndex.
//
// Title is amplified in the index by LSAConfig.TitleBoost (default 3×); pass
// an empty string when no title is meaningful. Body is the raw content — any
// YAML frontmatter or leading H1 line is stripped internally before indexing.
type Document struct {
	ID    uint64 // graphdb NodeID
	Title string // optional; amplified by cfg.TitleBoost
	Body  string
}

// LSAConfig knobs. Use DefaultLSAConfig() for the tuned values from the
// wiki-graph implementation this code was ported from.
type LSAConfig struct {
	Dims       int   // latent dimensions after SVD (default 200)
	Oversamp   int   // extra sketch dims for SVD numerical stability (default 10)
	PowerIter  int   // power iterations in randomized SVD (default 2)
	MaxVocab   int   // hard cap on vocabulary size (default 8000)
	MinDocFreq int   // filter terms appearing in fewer docs than this (default 3)
	TitleBoost int   // times to repeat Title to amplify title-term weight (default 3)
	Seed       int64 // RNG seed for determinism (default 42)
}

// DefaultLSAConfig returns the config used by the wiki-graph port. These
// values are tuned for corpora in the 1k-10k document range.
func DefaultLSAConfig() LSAConfig {
	return LSAConfig{
		Dims:       200,
		Oversamp:   10,
		PowerIter:  2,
		MaxVocab:   8000,
		MinDocFreq: 3,
		TitleBoost: 3,
		Seed:       42,
	}
}

// LSAIndex holds the LSA model and BM25 index built once at corpus load.
// After BuildLSAIndex returns, queries (FoldQuery, BM25Score) are sub-millisecond.
type LSAIndex struct {
	dims  int
	vocab map[string]int32 // term → column index
	// globalWeight is the per-term global weighting factor applied during
	// both build and query folding. Switched from inverse document
	// frequency (IDF: log(D/df + 1)) to log-entropy (Dumais 1991:
	// 1 + sum_d(p_dt * log p_dt) / log D, where p_dt = tf_dt / gf_t) in
	// the A2 PR. Quality delta is corpus-dependent — log-entropy tends
	// to down-weight terms whose distribution across documents is close
	// to uniform (low signal) more aggressively than IDF.
	globalWeight []float32
	b            [][]float32 // l×T: B = Q^T × X, used for query folding
	ub           [][]float32 // l×k: top-k eigenvectors of Gram(B)
	// docVecsQ holds the L2-normalized document embeddings quantized to int8
	// using fixed scale lsaQuantScale (=127). Trades ~0.8% max per-component
	// quantization error for 4× memory reduction vs the previous float32
	// representation. For an L2-normalized vector all components lie in
	// [-1, 1], so the scale-127 mapping never overflows int8 range. Lookups
	// dequantize on demand (DocVector); the inner loop in TopKByVector folds
	// the dequantization into the dot product so the only cost is one
	// int8→float32 cast per term plus a single division at the loop tail.
	//
	// Per-vector scale was considered (better resolution for vectors whose
	// max-abs component is well below 1) but rejected: ~17% extra storage
	// for ≤2× resolution gain on the typical L2-normalized vector where
	// components are O(1/sqrt(dims)).
	docVecsQ  [][]int8
	nodeIDs   []uint64       // row index → graphdb NodeID
	nodeIDMap map[uint64]int // NodeID → row index
	content   map[uint64]string

	// BM25 inverted index over the same tokenization used for LSA, so query
	// stemming is consistent across both scorers.
	bm25Post  map[string][]bm25Entry // term → [(docIdx, termFreq)]
	bm25Dlen  []int                  // document token count per doc
	bm25Avgdl float64                // corpus-average document length
}

// bm25Entry is one term's posting for one document. Fields are exported
// because LSAIndex round-trips through encoding/gob in lsa_persistence.go;
// gob skips unexported fields. The struct itself stays unexported because
// no caller outside this package needs to construct one.
type bm25Entry struct {
	Doc int
	TF  int
}

// sparseRow is one document's TF-IDF vector in compressed form.
type sparseRow struct {
	idx []int32
	val []float32
}

// lsaQuantScale is the fixed int8 quantization scale for L2-normalized
// document embeddings. Multiplying a float in [-1, 1] by 127 maps it to
// [-127, 127], the symmetric int8 range. Dequantization is float32(q) /
// lsaQuantScale. Max per-component error is 1/127 ≈ 0.79%, which is well
// below the LSA-noise-floor that the algorithm itself introduces. The
// constant is internal — callers see only float32 vectors at the
// DocVector / TopKByVector / FoldQuery boundaries.
const lsaQuantScale float32 = 127.0

// quantizeFloat32 maps a slice of L2-normalized floats to symmetric int8
// using lsaQuantScale. Components outside [-1, 1] are clamped (defensive
// against numerical drift; mathematically unreachable for a properly
// L2-normalized vector but cheap insurance against signed-overflow
// surprises if the invariant is ever broken upstream).
func quantizeFloat32(v []float32) []int8 {
	out := make([]int8, len(v))
	for i, x := range v {
		q := x * lsaQuantScale
		switch {
		case q > 127:
			out[i] = 127
		case q < -127:
			out[i] = -127
		default:
			// round-to-nearest via +0.5 / -0.5 then truncate.
			if q >= 0 {
				out[i] = int8(q + 0.5)
			} else {
				out[i] = int8(q - 0.5)
			}
		}
	}
	return out
}

// lsaStop filters common English and wiki-noise tokens. The "wiki/tags/created/
// updated" entries are carried over from the port source; they're harmless in
// a graphdb context and preserve byte-identical tokenization across repos.
var lsaStop = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true,
	"not": true, "you": true, "all": true, "can": true, "has": true,
	"was": true, "have": true, "with": true, "this": true, "from": true,
	"that": true, "they": true, "will": true, "been": true, "when": true,
	"who": true, "what": true, "how": true, "its": true, "into": true,
	"also": true, "each": true, "which": true, "than": true, "more": true,
	"used": true, "may": true, "one": true, "two": true, "three": true,
	"see": true, "per": true, "via": true, "use": true, "our": true,
	"any": true, "such": true, "other": true, "their": true, "these": true,
	"both": true, "between": true, "where": true, "while": true, "must": true,
	"wiki": true, "tags": true, "created": true, "updated": true,
	"based": true, "using": true, "within": true, "across": true,
}

// lsaStem applies light suffix stripping. Not a full Porter stemmer — targets
// the most common inflections that fragment related terms across documents.
func lsaStem(word string) string {
	n := len(word)
	switch {
	case n > 4 && strings.HasSuffix(word, "ing"):
		return word[:n-3]
	case n > 4 && strings.HasSuffix(word, "tion"):
		return word[:n-4]
	case n > 4 && strings.HasSuffix(word, "ated"):
		return word[:n-2]
	case n > 3 && strings.HasSuffix(word, "ed"):
		return word[:n-2]
	case n > 3 && strings.HasSuffix(word, "er"):
		return word[:n-2]
	case n > 3 && strings.HasSuffix(word, "es"):
		return word[:n-2]
	case n > 3 && strings.HasSuffix(word, "ly"):
		return word[:n-2]
	case n > 2 && strings.HasSuffix(word, "s"):
		return word[:n-1]
	}
	return word
}

// lsaTokenize splits text into normalized, stemmed tokens, excluding stop words.
// Hyphens are treated as word separators so "east-west" yields both "east" and "west".
func lsaTokenize(text string) []string {
	var tokens []string
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len(word) >= 3 && !lsaStop[word] {
			stemmed := lsaStem(word)
			if len(stemmed) >= 3 {
				tokens = append(tokens, stemmed)
			}
		}
	}
	return tokens
}

// lsaContent strips YAML frontmatter and a leading H1 title line from content.
func lsaContent(content string) string {
	if strings.HasPrefix(content, "---") {
		rest := content[3:]
		if end := strings.Index(rest, "---"); end >= 0 {
			content = strings.TrimSpace(rest[end+3:])
		}
	}
	if idx := strings.Index(content, "\n"); idx >= 0 {
		if strings.HasPrefix(strings.TrimSpace(content[:idx]), "#") {
			content = strings.TrimSpace(content[idx+1:])
		}
	}
	return content
}

// BuildLSAIndex constructs an LSA model and co-resident BM25 index from docs.
// Heavy linear algebra (SVD, Jacobi eigendecomposition) runs here; queries
// against the returned index are sub-millisecond.
//
// Returns an error if the corpus is empty or produces fewer unique vocabulary
// terms than cfg.Dims (LSA cannot project into a higher-dimensional latent
// space than the vocabulary supports).
func BuildLSAIndex(docs []Document, cfg LSAConfig) (*LSAIndex, error) {
	if len(docs) == 0 {
		return nil, fmt.Errorf("empty document corpus")
	}
	if cfg.Dims <= 0 || cfg.Oversamp < 0 || cfg.MaxVocab <= 0 || cfg.MinDocFreq < 1 {
		return nil, fmt.Errorf("invalid LSAConfig: %+v", cfg)
	}

	type docEntry struct {
		id      uint64
		text    string
		content string // stripped body, kept for snippet generation
	}
	prepared := make([]docEntry, 0, len(docs))
	for _, d := range docs {
		stripped := lsaContent(d.Body)
		text := strings.Repeat(d.Title+" ", cfg.TitleBoost) + stripped
		prepared = append(prepared, docEntry{d.ID, text, stripped})
	}
	D := len(prepared)

	// --- Vocabulary construction ---
	allTF := make([]map[string]int, D)
	dfCount := make(map[string]int, 12000)
	for d, dc := range prepared {
		tf := make(map[string]int, 200)
		for _, tok := range lsaTokenize(dc.text) {
			tf[tok]++
		}
		allTF[d] = tf
		for tok := range tf {
			dfCount[tok]++
		}
	}
	type termDF struct {
		term string
		df   int
	}
	var cands []termDF
	for term, df := range dfCount {
		if df >= cfg.MinDocFreq && df < D {
			cands = append(cands, termDF{term, df})
		}
	}
	// Stable sort: df desc, then term asc — makes the vocab deterministic
	// regardless of Go map iteration order, which matters for the
	// byte-identical-rebuild guarantee.
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].df != cands[j].df {
			return cands[i].df > cands[j].df
		}
		return cands[i].term < cands[j].term
	})
	if len(cands) > cfg.MaxVocab {
		cands = cands[:cfg.MaxVocab]
	}
	T := len(cands)
	if T < cfg.Dims {
		return nil, fmt.Errorf("vocabulary size %d below configured dims %d; reduce Dims or provide a larger corpus", T, cfg.Dims)
	}

	vocab := make(map[string]int32, T)
	for i, c := range cands {
		vocab[c.term] = int32(i)
	}

	// --- Log-entropy global weighting (Dumais 1991) ---
	//
	// Global weight g_t = 1 + (1/log D) * sum_d (p_dt * log p_dt) where
	// p_dt = tf_dt / gf_t and gf_t is the corpus-total frequency of term t.
	// The summand is 0 by convention when p_dt is 0 (term doesn't appear in
	// that doc); log(1)=0 falls out naturally when p_dt=1 (term concentrated
	// in a single doc). For D==1 the log(D) divisor is 0 and the entropy
	// adjustment is undefined — degrade to global weight 1 (pure local
	// weighting), which is the only sensible single-doc behavior.
	//
	// Replaces the previous IDF weighting log(D/df + 1). The two formulas
	// produce similar shapes for typical corpora; log-entropy down-weights
	// near-uniform terms more aggressively, which is the headline
	// retrieval-quality win.
	gf := make([]int, T)
	for _, tf := range allTF {
		for term, c := range tf {
			if tidx, ok := vocab[term]; ok {
				gf[tidx] += c
			}
		}
	}
	var invLogD float64
	if D > 1 {
		invLogD = 1.0 / math.Log(float64(D))
	}
	entropyAccum := make([]float64, T)
	for _, tf := range allTF {
		for term, c := range tf {
			tidx, ok := vocab[term]
			if !ok || gf[tidx] == 0 {
				continue
			}
			p := float64(c) / float64(gf[tidx])
			entropyAccum[tidx] += p * math.Log(p)
		}
	}
	globalWeight := make([]float32, T)
	for i := range globalWeight {
		globalWeight[i] = float32(1.0 + entropyAccum[i]*invLogD)
	}

	// --- Sparse weighted-term matrix X (D×T) ---
	//
	// Local weight: log(1 + tf_dt). Combined with globalWeight from above,
	// each cell is log(1+tf) * g_t. Rows are L2-normalized — keeps cosine
	// similarity well-defined regardless of doc length.
	X := make([]sparseRow, D)
	for d, tf := range allTF {
		if len(tf) == 0 {
			continue
		}
		// Deterministic insertion order: iterate tf map keys in sorted order.
		terms := make([]string, 0, len(tf))
		for term := range tf {
			terms = append(terms, term)
		}
		sort.Strings(terms)
		for _, term := range terms {
			tidx, ok := vocab[term]
			if !ok {
				continue
			}
			count := tf[term]
			local := float32(math.Log(1.0 + float64(count)))
			X[d].idx = append(X[d].idx, tidx)
			X[d].val = append(X[d].val, local*globalWeight[tidx])
		}
		norm := float32(0)
		for _, v := range X[d].val {
			norm += v * v
		}
		if norm > 0 {
			inv := float32(1 / math.Sqrt(float64(norm)))
			for i := range X[d].val {
				X[d].val[i] *= inv
			}
		}
	}

	// --- Randomized truncated SVD ---
	l := cfg.Dims + cfg.Oversamp
	rng := rand.New(rand.NewSource(cfg.Seed))
	Omega := lsaRandMatrix(rng, T, l)
	Y := lsaSparseMulDense(X, Omega, D, l)
	for i := 0; i < cfg.PowerIter; i++ {
		Z := lsaSparseTMulDense(X, Y, T, l)
		Y = lsaSparseMulDense(X, Z, D, l)
	}
	Q := lsaQR(Y, D, l)
	B := lsaLeftMul(Q, X, D, l, T)
	G := lsaGram(B, l, T)
	eigenVals, eigenVecs := lsaJacobi(G, l)

	order := make([]int, l)
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return eigenVals[order[a]] > eigenVals[order[b]] })
	k := cfg.Dims
	UB := make([][]float32, l)
	for i := range UB {
		UB[i] = make([]float32, k)
		for j := 0; j < k; j++ {
			UB[i][j] = eigenVecs[i][order[j]]
		}
	}

	// --- Document embeddings ---
	//
	// Compute each doc's projection into the k-dim latent space, L2-normalize
	// it, then quantize to int8 via lsaQuantScale. The quantization step
	// happens last so the L2 invariant the cosine math relies on is preserved
	// in the (logical) float-space representation; downstream code dequantizes
	// on demand.
	docVecsQ := make([][]int8, D)
	nodeIDs := make([]uint64, D)
	nodeIDMap := make(map[uint64]int, D)
	content := make(map[uint64]string, D)
	for d := range prepared {
		nodeIDs[d] = prepared[d].id
		nodeIDMap[prepared[d].id] = d
		content[prepared[d].id] = prepared[d].content
		vec := make([]float32, k)
		for i := 0; i < l; i++ {
			qi := Q[d][i]
			if qi == 0 {
				continue
			}
			ubi := UB[i]
			for j := 0; j < k; j++ {
				vec[j] += qi * ubi[j]
			}
		}
		norm := float32(0)
		for _, v := range vec {
			norm += v * v
		}
		if norm > 0 {
			inv := float32(1 / math.Sqrt(float64(norm)))
			for j := range vec {
				vec[j] *= inv
			}
		}
		docVecsQ[d] = quantizeFloat32(vec)
	}

	// --- BM25 inverted index ---
	bm25Post := make(map[string][]bm25Entry, T)
	bm25Dlen := make([]int, D)
	totalLen := 0
	for d, dc := range prepared {
		toks := lsaTokenize(dc.text)
		bm25Dlen[d] = len(toks)
		totalLen += len(toks)
		tf := make(map[string]int, len(toks))
		for _, tok := range toks {
			tf[tok]++
		}
		// Deterministic posting order: sort terms before appending.
		terms := make([]string, 0, len(tf))
		for term := range tf {
			terms = append(terms, term)
		}
		sort.Strings(terms)
		for _, term := range terms {
			bm25Post[term] = append(bm25Post[term], bm25Entry{d, tf[term]})
		}
	}
	bm25Avgdl := float64(totalLen) / float64(D)

	return &LSAIndex{
		dims:         k,
		vocab:        vocab,
		globalWeight: globalWeight,
		b:            B,
		ub:           UB,
		docVecsQ:     docVecsQ,
		nodeIDs:      nodeIDs,
		nodeIDMap:    nodeIDMap,
		content:      content,
		bm25Post:     bm25Post,
		bm25Dlen:     bm25Dlen,
		bm25Avgdl:    bm25Avgdl,
	}, nil
}

// FoldQuery maps a text query into the LSA latent space (k-dim, L2-normalized)
// and returns the shared tokenization so callers can feed the same tokens into
// BM25Score without re-running the tokenizer.
//
// Returns an error if no query term maps to a vocabulary entry (out-of-vocab
// query) or if the projection collapses to the zero vector.
func (i *LSAIndex) FoldQuery(query string) (vec []float32, tokens []string, err error) {
	tokens = lsaTokenize(query)
	tfMap := make(map[int32]float32, len(tokens))
	for _, tok := range tokens {
		if idx, ok := i.vocab[tok]; ok {
			tfMap[idx]++
		}
	}
	if len(tfMap) == 0 {
		return nil, tokens, fmt.Errorf("no vocabulary terms matched in query %q", query)
	}

	// Mirror the build-side weighting (Dumais 1991 log-entropy): local =
	// log(1 + tf), global = stored entropy weight per term. The maxTF
	// normalization the previous augmented-TF formula used is no longer
	// needed; log(1 + tf) is its own attenuation curve.
	l := len(i.b)
	lVec := make([]float32, l)
	for row := 0; row < l; row++ {
		bi := i.b[row]
		for tidx, count := range tfMap {
			local := float32(math.Log(1.0 + float64(count)))
			lVec[row] += bi[tidx] * local * i.globalWeight[tidx]
		}
	}

	kDim := len(i.ub[0])
	vec = make([]float32, kDim)
	for row := 0; row < l; row++ {
		lv := lVec[row]
		if lv == 0 {
			continue
		}
		ubi := i.ub[row]
		for j := 0; j < kDim; j++ {
			vec[j] += ubi[j] * lv
		}
	}
	norm := float32(0)
	for _, v := range vec {
		norm += v * v
	}
	if norm == 0 {
		return nil, tokens, fmt.Errorf("query %q maps to zero vector in LSA space", query)
	}
	inv := float32(1 / math.Sqrt(float64(norm)))
	for j := range vec {
		vec[j] *= inv
	}
	return vec, tokens, nil
}

// BM25Score returns Okapi BM25 scores (k1=1.5, b=0.75) keyed by graphdb NodeID.
// Only documents whose index contains at least one query token appear in the
// result map — callers should treat missing keys as score 0.
//
// If candidates is non-nil, scoring is restricted to NodeIDs in the set (other
// nodes are skipped). Pass nil to score across the full corpus.
func (i *LSAIndex) BM25Score(tokens []string, candidates map[uint64]bool) map[uint64]float64 {
	const k1, b = 1.5, 0.75
	scores := make(map[uint64]float64)
	N := float64(len(i.nodeIDs))
	if N == 0 {
		return scores
	}
	for _, term := range tokens {
		postings, ok := i.bm25Post[term]
		if !ok {
			continue
		}
		df := float64(len(postings))
		idf := math.Log(1 + (N-df+0.5)/(df+0.5))
		for _, e := range postings {
			nodeID := i.nodeIDs[e.Doc]
			if candidates != nil && !candidates[nodeID] {
				continue
			}
			dl := float64(i.bm25Dlen[e.Doc])
			tf := float64(e.TF)
			scores[nodeID] += idf * (tf * (k1 + 1)) / (tf + k1*(1-b+b*dl/i.bm25Avgdl))
		}
	}
	return scores
}

// DocVector returns the L2-normalized LSA embedding for the given NodeID.
// The second return is false if the NodeID was not present in the corpus.
//
// The returned slice is freshly allocated and dequantized from the int8
// in-memory representation; callers can hold it without worrying about
// index-state mutation. Re-quantization error vs the original float32 is
// at most lsaQuantScale^-1 per component (~0.79%).
func (i *LSAIndex) DocVector(id uint64) ([]float32, bool) {
	d, ok := i.nodeIDMap[id]
	if !ok {
		return nil, false
	}
	qv := i.docVecsQ[d]
	out := make([]float32, len(qv))
	for j, x := range qv {
		out[j] = float32(x) / lsaQuantScale
	}
	return out, true
}

// DocSnippet returns a rune-safe truncated excerpt of the document body for
// presentation. maxLen is a character (rune) count, not a byte count. If maxLen
// <= 0 the full stored content is returned. If the document is not in the
// corpus, returns "".
func (i *LSAIndex) DocSnippet(id uint64, maxLen int) string {
	c, ok := i.content[id]
	if !ok {
		return ""
	}
	if maxLen <= 0 {
		return c
	}
	runes := []rune(c)
	if len(runes) <= maxLen {
		return c
	}
	if maxLen < 4 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

// Dimensions returns the LSA latent dimension count (cfg.Dims).
func (i *LSAIndex) Dimensions() int { return i.dims }

// NumDocs returns the number of documents in the corpus.
func (i *LSAIndex) NumDocs() int { return len(i.nodeIDs) }

// LSAResult is a ranked result from LSA semantic search.
type LSAResult struct {
	NodeID     uint64
	Similarity float32 // cosine similarity in [-1, 1] (typically [0, 1] for stored embeddings)
}

// TopKByVector returns the k documents most similar to qvec, ranked by
// cosine similarity descending. Ties are broken by NodeID ascending so
// the result is a deterministic prefix of any larger K — the same
// property SearchTopK maintains for paginated callers.
//
// qvec must have the same dimensionality as the index (Dimensions()) and
// should be L2-normalized; FoldQuery returns vectors that satisfy both.
// A mismatched dimension returns an error; it's a programming bug, not
// a user error.
//
// No storage I/O — operates entirely on the in-memory int8-quantized doc
// vectors. The dot product fuses the dequantization into the accumulator
// (multiply int8 component then divide once at the loop tail) so the
// quantization shows up as a single division per doc rather than per
// component.
func (i *LSAIndex) TopKByVector(qvec []float32, k int) ([]LSAResult, error) {
	if len(qvec) != i.dims {
		return nil, fmt.Errorf("qvec dim %d != index dim %d", len(qvec), i.dims)
	}
	if k <= 0 {
		k = len(i.docVecsQ)
	}

	scored := make([]LSAResult, 0, len(i.docVecsQ))
	for d, dvq := range i.docVecsQ {
		sim := float32(0)
		for j, x := range dvq {
			sim += qvec[j] * float32(x)
		}
		sim /= lsaQuantScale
		scored = append(scored, LSAResult{NodeID: i.nodeIDs[d], Similarity: sim})
	}

	sort.Slice(scored, func(a, b int) bool {
		if scored[a].Similarity != scored[b].Similarity {
			return scored[a].Similarity > scored[b].Similarity
		}
		return scored[a].NodeID < scored[b].NodeID
	})

	if k < len(scored) {
		scored = scored[:k]
	}
	return scored, nil
}
