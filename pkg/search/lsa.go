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
//  3. Not tenant-scoped. Mirrors the current posture of FullTextIndex in this
//     package. Per-tenant isolation is a follow-up PR for both indexes together.

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
	dims      int
	vocab     map[string]int32 // term → column index
	idf       []float32        // IDF weight per vocab term
	b         [][]float32      // l×T: B = Q^T × X, used for query folding
	ub        [][]float32      // l×k: top-k eigenvectors of Gram(B)
	docVecs   [][]float32      // D×k: L2-normalized document embeddings
	nodeIDs   []uint64         // row index → graphdb NodeID
	nodeIDMap map[uint64]int   // NodeID → row index
	content   map[uint64]string

	// BM25 inverted index over the same tokenization used for LSA, so query
	// stemming is consistent across both scorers.
	bm25Post  map[string][]bm25Entry // term → [(docIdx, termFreq)]
	bm25Dlen  []int                  // document token count per doc
	bm25Avgdl float64                // corpus-average document length
}

type bm25Entry struct {
	doc int
	tf  int
}

// sparseRow is one document's TF-IDF vector in compressed form.
type sparseRow struct {
	idx []int32
	val []float32
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
	idf := make([]float32, T)
	for i, c := range cands {
		vocab[c.term] = int32(i)
		idf[i] = float32(math.Log(float64(D)/float64(c.df) + 1))
	}

	// --- Sparse TF-IDF matrix X (D×T) ---
	X := make([]sparseRow, D)
	for d, tf := range allTF {
		maxTF := 0
		for _, c := range tf {
			if c > maxTF {
				maxTF = c
			}
		}
		if maxTF == 0 {
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
			tfw := float32(0.5 + 0.5*float64(count)/float64(maxTF))
			X[d].idx = append(X[d].idx, tidx)
			X[d].val = append(X[d].val, tfw*idf[tidx])
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
	docVecs := make([][]float32, D)
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
		docVecs[d] = vec
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
		dims:      k,
		vocab:     vocab,
		idf:       idf,
		b:         B,
		ub:        UB,
		docVecs:   docVecs,
		nodeIDs:   nodeIDs,
		nodeIDMap: nodeIDMap,
		content:   content,
		bm25Post:  bm25Post,
		bm25Dlen:  bm25Dlen,
		bm25Avgdl: bm25Avgdl,
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
	maxTF := float32(0)
	for _, v := range tfMap {
		if v > maxTF {
			maxTF = v
		}
	}

	l := len(i.b)
	lVec := make([]float32, l)
	for row := 0; row < l; row++ {
		bi := i.b[row]
		for tidx, count := range tfMap {
			tfw := float32(0.5) + float32(0.5)*count/maxTF
			lVec[row] += bi[tidx] * tfw * i.idf[tidx]
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
			nodeID := i.nodeIDs[e.doc]
			if candidates != nil && !candidates[nodeID] {
				continue
			}
			dl := float64(i.bm25Dlen[e.doc])
			tf := float64(e.tf)
			scores[nodeID] += idf * (tf * (k1 + 1)) / (tf + k1*(1-b+b*dl/i.bm25Avgdl))
		}
	}
	return scores
}

// DocVector returns the L2-normalized LSA embedding for the given NodeID.
// The second return is false if the NodeID was not present in the corpus.
func (i *LSAIndex) DocVector(id uint64) ([]float32, bool) {
	d, ok := i.nodeIDMap[id]
	if !ok {
		return nil, false
	}
	// Return a copy so callers can't mutate index state.
	out := make([]float32, len(i.docVecs[d]))
	copy(out, i.docVecs[d])
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

// --- Linear algebra helpers ---

func lsaRandMatrix(rng *rand.Rand, rows, cols int) [][]float32 {
	m := make([][]float32, rows)
	for i := range m {
		m[i] = make([]float32, cols)
		for j := range m[i] {
			m[i][j] = float32(rng.NormFloat64())
		}
	}
	return m
}

// lsaSparseMulDense: Y = X × A   (D×l = sparse(D×T) × dense(T×l))
func lsaSparseMulDense(X []sparseRow, A [][]float32, D, l int) [][]float32 {
	Y := make([][]float32, D)
	for d := range Y {
		Y[d] = make([]float32, l)
		yd := Y[d]
		for k, tidx := range X[d].idx {
			v := X[d].val[k]
			ar := A[tidx]
			for j := 0; j < l; j++ {
				yd[j] += v * ar[j]
			}
		}
	}
	return Y
}

// lsaSparseTMulDense: Z = X^T × Y   (T×l = sparse(D×T)^T × dense(D×l))
func lsaSparseTMulDense(X []sparseRow, Y [][]float32, T, l int) [][]float32 {
	Z := make([][]float32, T)
	for t := range Z {
		Z[t] = make([]float32, l)
	}
	for d, row := range X {
		yd := Y[d]
		for k, tidx := range row.idx {
			v := row.val[k]
			zt := Z[tidx]
			for j := 0; j < l; j++ {
				zt[j] += v * yd[j]
			}
		}
	}
	return Z
}

// lsaQR orthonormalizes columns of Y (D×l) via modified Gram-Schmidt.
func lsaQR(Y [][]float32, D, l int) [][]float32 {
	cols := make([][]float32, l)
	for j := range cols {
		cols[j] = make([]float32, D)
		for d := 0; d < D; d++ {
			cols[j][d] = Y[d][j]
		}
	}
	for j := 0; j < l; j++ {
		for prev := 0; prev < j; prev++ {
			dot := float32(0)
			for d := 0; d < D; d++ {
				dot += cols[j][d] * cols[prev][d]
			}
			for d := 0; d < D; d++ {
				cols[j][d] -= dot * cols[prev][d]
			}
		}
		norm := float32(0)
		for d := 0; d < D; d++ {
			norm += cols[j][d] * cols[j][d]
		}
		if norm > 1e-12 {
			inv := float32(1 / math.Sqrt(float64(norm)))
			for d := 0; d < D; d++ {
				cols[j][d] *= inv
			}
		}
	}
	Q := make([][]float32, D)
	for d := range Q {
		Q[d] = make([]float32, l)
		for j := 0; j < l; j++ {
			Q[d][j] = cols[j][d]
		}
	}
	return Q
}

// lsaLeftMul: B = Q^T × X   (l×T = dense(D×l)^T × sparse(D×T))
func lsaLeftMul(Q [][]float32, X []sparseRow, D, l, T int) [][]float32 {
	B := make([][]float32, l)
	for i := range B {
		B[i] = make([]float32, T)
	}
	for d, row := range X {
		qd := Q[d]
		for k, tidx := range row.idx {
			v := row.val[k]
			for i := 0; i < l; i++ {
				B[i][tidx] += qd[i] * v
			}
		}
	}
	return B
}

// lsaGram: G = B × B^T   (l×l symmetric; eigenvalues = squared singular values of B)
func lsaGram(B [][]float32, l, T int) [][]float32 {
	G := make([][]float32, l)
	for i := range G {
		G[i] = make([]float32, l)
	}
	for i := 0; i < l; i++ {
		bi := B[i]
		for j := i; j < l; j++ {
			bj := B[j]
			dot := float32(0)
			for t := 0; t < T; t++ {
				dot += bi[t] * bj[t]
			}
			G[i][j] = dot
			G[j][i] = dot
		}
	}
	return G
}

// lsaJacobi computes eigendecomposition of symmetric G (n×n) via cyclic Jacobi sweeps.
// Returns (eigenvalues, V) where columns of V are eigenvectors.
func lsaJacobi(G [][]float32, n int) ([]float32, [][]float32) {
	A := make([][]float32, n)
	for i := range A {
		A[i] = make([]float32, n)
		copy(A[i], G[i])
	}
	V := make([][]float32, n)
	for i := range V {
		V[i] = make([]float32, n)
		V[i][i] = 1
	}

	const maxSweeps = 50
	const tol = float32(1e-6)

	for sweep := 0; sweep < maxSweeps; sweep++ {
		maxOff := float32(0)
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				v := A[i][j]
				if v < 0 {
					v = -v
				}
				if v > maxOff {
					maxOff = v
				}
			}
		}
		if maxOff < tol {
			break
		}
		for p := 0; p < n-1; p++ {
			for q := p + 1; q < n; q++ {
				apq := A[p][q]
				if apq < 0 {
					apq = -apq
				}
				if apq < 1e-9 {
					continue
				}
				tau := (A[q][q] - A[p][p]) / (2 * A[p][q])
				var t float32
				if tau >= 0 {
					t = 1 / (tau + float32(math.Sqrt(float64(1+tau*tau))))
				} else {
					t = 1 / (tau - float32(math.Sqrt(float64(1+tau*tau))))
				}
				c := float32(1 / math.Sqrt(float64(1+t*t)))
				s := t * c
				App, Aqq, Apq := A[p][p], A[q][q], A[p][q]
				A[p][p] = App - t*Apq
				A[q][q] = Aqq + t*Apq
				A[p][q] = 0
				A[q][p] = 0
				for r := 0; r < n; r++ {
					if r == p || r == q {
						continue
					}
					Apr := A[p][r]
					Aqr := A[q][r]
					A[p][r] = c*Apr - s*Aqr
					A[r][p] = A[p][r]
					A[q][r] = s*Apr + c*Aqr
					A[r][q] = A[q][r]
				}
				for r := 0; r < n; r++ {
					Vrp := V[r][p]
					Vrq := V[r][q]
					V[r][p] = c*Vrp - s*Vrq
					V[r][q] = s*Vrp + c*Vrq
				}
			}
		}
	}

	eigenVals := make([]float32, n)
	for i := range eigenVals {
		eigenVals[i] = A[i][i]
	}
	return eigenVals, V
}
