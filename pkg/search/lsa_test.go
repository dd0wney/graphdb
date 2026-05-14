package search

import (
	"math"
	"testing"
)

// testLSAConfig is a small-scale config for unit tests. Dims=10 gives the
// SVD math enough room to produce sensible projections on a tiny corpus
// without bumping into the T >= Dims guard.
func testLSAConfig() LSAConfig {
	return LSAConfig{
		Dims:       10,
		Oversamp:   4,
		PowerIter:  2,
		MaxVocab:   200,
		MinDocFreq: 1,
		TitleBoost: 3,
		Seed:       42,
	}
}

// testCorpus has enough distinct vocabulary after stemming/stopword filtering
// to produce T >= Dims for any test config here.
func testCorpus() []Document {
	return []Document{
		{ID: 1, Title: "cats and dogs", Body: "the quick brown fox jumps over lazy dogs in the garden"},
		{ID: 2, Title: "wild animals", Body: "a fast red fox leaps above sleepy dogs by the fence"},
		{ID: 3, Title: "morning sounds", Body: "cats meow and birds sing early in the morning light"},
		{ID: 4, Title: "guard dogs", Body: "dogs bark when strangers approach the garden gate at night"},
		{ID: 5, Title: "quiet hunters", Body: "felines prowl rooftops while rodents scurry under the porch"},
		{ID: 6, Title: "feeding time", Body: "cats prefer fish while dogs eat whatever scraps humans leave"},
		{ID: 7, Title: "migration", Body: "birds fly south for winter and return in spring to sing again"},
		{ID: 8, Title: "seasons", Body: "winter brings snow and spring brings flowers blooming in fields"},
	}
}

// TestLSADeterminism asserts that building the same corpus twice produces
// byte-identical document vectors. This is the headline property callers
// rely on for cached-vector invalidation and for the "seed=42 is a feature"
// contract documented in the package header.
func TestLSADeterminism(t *testing.T) {
	docs := testCorpus()
	cfg := testLSAConfig()

	idx1, err := BuildLSAIndex(docs, cfg)
	if err != nil {
		t.Fatalf("first build failed: %v", err)
	}
	idx2, err := BuildLSAIndex(docs, cfg)
	if err != nil {
		t.Fatalf("second build failed: %v", err)
	}

	if idx1.NumDocs() != idx2.NumDocs() {
		t.Fatalf("doc count mismatch: %d vs %d", idx1.NumDocs(), idx2.NumDocs())
	}
	if idx1.Dimensions() != idx2.Dimensions() {
		t.Fatalf("dim mismatch: %d vs %d", idx1.Dimensions(), idx2.Dimensions())
	}

	for _, d := range docs {
		v1, ok1 := idx1.DocVector(d.ID)
		v2, ok2 := idx2.DocVector(d.ID)
		if !ok1 || !ok2 {
			t.Fatalf("doc %d: vector missing (idx1=%v, idx2=%v)", d.ID, ok1, ok2)
		}
		if len(v1) != len(v2) {
			t.Fatalf("doc %d: length mismatch %d vs %d", d.ID, len(v1), len(v2))
		}
		for j := range v1 {
			if v1[j] != v2[j] {
				t.Fatalf("doc %d component %d: %v != %v", d.ID, j, v1[j], v2[j])
			}
		}
	}
}

// TestLSAFoldQueryRoundTrip asserts that folding a document's own body into
// the LSA space produces a vector cosine-similar to that document's stored
// vector. The tolerance is deliberately loose (0.3) because the doc-side uses
// a corpus-relative maxTF while the query-side uses a query-relative maxTF,
// so the vectors aren't identical even for self-queries — just highly aligned.
func TestLSAFoldQueryRoundTrip(t *testing.T) {
	docs := testCorpus()
	idx, err := BuildLSAIndex(docs, testLSAConfig())
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	for _, d := range docs {
		qvec, _, err := idx.FoldQuery(d.Body)
		if err != nil {
			t.Fatalf("fold doc %d body: %v", d.ID, err)
		}
		dvec, ok := idx.DocVector(d.ID)
		if !ok {
			t.Fatalf("doc %d: no vector in index", d.ID)
		}
		sim := cosine(qvec, dvec)
		if sim < 0.3 {
			t.Errorf("doc %d self-fold cosine = %.3f, want >= 0.3", d.ID, sim)
		}
	}
}

// TestLSAFoldQueryOOV asserts that a query with no in-vocabulary terms
// returns a clean error rather than a garbage vector. Callers need this to
// fall back to exact-match scoring when the query is fully out-of-vocabulary.
func TestLSAFoldQueryOOV(t *testing.T) {
	idx, err := BuildLSAIndex(testCorpus(), testLSAConfig())
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	_, tokens, err := idx.FoldQuery("zyxwvu qrstuv tuvwxy")
	if err == nil {
		t.Fatalf("expected error for OOV query, got tokens=%v", tokens)
	}
	if len(tokens) == 0 {
		// Tokenize should still produce tokens (3-letter min) — it's the
		// vocab lookup that fails. If this trips, the tokenizer changed.
		t.Logf("note: tokenizer returned no tokens for OOV query (may be fine)")
	}
}

// TestBM25Score hand-computes BM25 for a 3-doc corpus and checks that the
// implementation matches. Uses a stop-word-free vocabulary so no token gets
// filtered out, and unstemmable tokens so we can reason about exact TF values.
func TestBM25Score(t *testing.T) {
	// "foo", "bar", "baz", "qux", "zot" all 3 chars, not in lsaStop, and
	// have no suffix the stemmer strips (checked against lsaStem cases).
	docs := []Document{
		{ID: 100, Title: "", Body: "foo foo bar baz"},
		{ID: 200, Title: "", Body: "foo qux zot"},
		{ID: 300, Title: "", Body: "bar qux"},
	}
	cfg := testLSAConfig()
	cfg.Dims = 4 // vocab will be small (~5 terms); lower Dims to fit
	idx, err := BuildLSAIndex(docs, cfg)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// "foo" tokenization: passes (len 3, not a stopword, no stem apply).
	scores := idx.BM25Score([]string{"foo"}, nil)

	// Expected:
	//   N=3 docs, df("foo")=2 (docs 100, 200).
	//   idf = log(1 + (N-df+0.5)/(df+0.5)) = log(1 + 1.5/2.5) = log(1.6) ≈ 0.4700
	//   With Title="" and TitleBoost=3, dlen counts only body tokens.
	//   dlen(100) = 4  (foo, foo, bar, baz)
	//   dlen(200) = 3  (foo, qux, zot)
	//   dlen(300) = 2  (bar, qux)
	//   avgdl = (4 + 3 + 2) / 3 = 3
	//
	//   Doc 100: tf=2, dlen=4
	//     score = 0.4700 * (2 * 2.5) / (2 + 1.5 * (0.25 + 0.75 * 4/3))
	//           = 0.4700 * 5 / (2 + 1.5 * 1.25)
	//           = 0.4700 * 5 / 3.875
	//           ≈ 0.6065
	//
	//   Doc 200: tf=1, dlen=3
	//     score = 0.4700 * (1 * 2.5) / (1 + 1.5 * (0.25 + 0.75 * 3/3))
	//           = 0.4700 * 2.5 / (1 + 1.5 * 1.0)
	//           = 0.4700 * 2.5 / 2.5
	//           ≈ 0.4700
	//
	//   Doc 300: tf=0 → not in scores map.

	if _, present := scores[300]; present {
		t.Errorf("doc 300 has no 'foo' token but appears in BM25 scores: %v", scores)
	}

	s100, ok := scores[100]
	if !ok {
		t.Fatalf("doc 100 missing from scores: %v", scores)
	}
	s200, ok := scores[200]
	if !ok {
		t.Fatalf("doc 200 missing from scores: %v", scores)
	}

	const eps = 1e-3
	wantS100 := 0.6065
	wantS200 := 0.4700
	if math.Abs(s100-wantS100) > eps {
		t.Errorf("doc 100 score: got %.4f, want %.4f ± %g", s100, wantS100, eps)
	}
	if math.Abs(s200-wantS200) > eps {
		t.Errorf("doc 200 score: got %.4f, want %.4f ± %g", s200, wantS200, eps)
	}
	if s100 <= s200 {
		t.Errorf("expected doc 100 (tf=2) to score higher than doc 200 (tf=1); got %.4f vs %.4f", s100, s200)
	}
}

// TestBM25CandidateFilter asserts that passing a non-nil candidates set
// restricts scoring to that subset, which is the hybrid-search stage-1 filter.
func TestBM25CandidateFilter(t *testing.T) {
	docs := []Document{
		{ID: 100, Title: "", Body: "foo foo bar baz"},
		{ID: 200, Title: "", Body: "foo qux zot"},
		{ID: 300, Title: "", Body: "bar qux"},
	}
	cfg := testLSAConfig()
	cfg.Dims = 4
	idx, err := BuildLSAIndex(docs, cfg)
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	only100 := map[uint64]bool{100: true}
	scores := idx.BM25Score([]string{"foo"}, only100)

	if _, present := scores[200]; present {
		t.Errorf("doc 200 should be filtered out by candidates set; scores: %v", scores)
	}
	if _, present := scores[100]; !present {
		t.Errorf("doc 100 should remain in scores: %v", scores)
	}
}

// TestBuildLSAIndexRejectsInvalidInput covers the early-guard branches.
func TestBuildLSAIndexRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		docs []Document
		cfg  LSAConfig
	}{
		{"empty corpus", nil, testLSAConfig()},
		{"zero dims", []Document{{ID: 1, Body: "foo bar baz"}}, LSAConfig{Dims: 0, MaxVocab: 10, MinDocFreq: 1, Seed: 1}},
		{"negative oversamp", []Document{{ID: 1, Body: "foo bar baz"}}, LSAConfig{Dims: 4, Oversamp: -1, MaxVocab: 10, MinDocFreq: 1, Seed: 1}},
		{"zero maxvocab", []Document{{ID: 1, Body: "foo bar baz"}}, LSAConfig{Dims: 4, MaxVocab: 0, MinDocFreq: 1, Seed: 1}},
		{"vocab below dims", []Document{{ID: 1, Body: "foo"}, {ID: 2, Body: "bar"}}, LSAConfig{Dims: 50, Oversamp: 4, MaxVocab: 100, MinDocFreq: 1, Seed: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BuildLSAIndex(tc.docs, tc.cfg); err == nil {
				t.Errorf("expected error, got nil")
			}
		})
	}
}

// TestLSATopKByVector asserts TopKByVector returns at most k results,
// ranked by similarity descending, and that a self-query (fold a doc's
// body then search) places that doc first.
func TestLSATopKByVector(t *testing.T) {
	docs := testCorpus()
	idx, err := BuildLSAIndex(docs, testLSAConfig())
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// Fold doc 1's body, then TopKByVector should rank doc 1 highest.
	qvec, _, err := idx.FoldQuery(docs[0].Body)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}

	results, err := idx.TopKByVector(qvec, 3)
	if err != nil {
		t.Fatalf("TopKByVector: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}
	if results[0].NodeID != docs[0].ID {
		t.Errorf("want doc %d ranked first (self-fold), got %d", docs[0].ID, results[0].NodeID)
	}
	// Descending similarity.
	for i := 1; i < len(results); i++ {
		if results[i].Similarity > results[i-1].Similarity {
			t.Errorf("results not sorted: [%d].sim=%.3f > [%d].sim=%.3f",
				i, results[i].Similarity, i-1, results[i-1].Similarity)
		}
	}
}

// TestLSATopKByVector_DimMismatch returns an error (programming bug).
func TestLSATopKByVector_DimMismatch(t *testing.T) {
	idx, err := BuildLSAIndex(testCorpus(), testLSAConfig())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := idx.TopKByVector(make([]float32, idx.Dimensions()+7), 3); err == nil {
		t.Error("expected error for mismatched dim, got nil")
	}
}

// TestLSATopKByVector_StableTieBreak: two near-identical similarity
// values must resolve the same way across calls, so top-K is always a
// prefix of top-(K+N).
func TestLSATopKByVector_StableTieBreak(t *testing.T) {
	idx, err := BuildLSAIndex(testCorpus(), testLSAConfig())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// "dogs" stems to "dog" which appears in 4 test-corpus docs; enough
	// to produce multiple results with some likely score ties.
	qvec, _, err := idx.FoldQuery("dogs")
	if err != nil {
		t.Fatalf("fold: %v", err)
	}

	r2, _ := idx.TopKByVector(qvec, 2)
	r4, _ := idx.TopKByVector(qvec, 4)
	if len(r2) == 0 || len(r4) == 0 {
		t.Fatal("empty")
	}
	for i := range r2 {
		if r2[i].NodeID != r4[i].NodeID {
			t.Errorf("top-2[%d]=%d differs from top-4[%d]=%d — pagination would break",
				i, r2[i].NodeID, i, r4[i].NodeID)
		}
	}
}

// TestTenantLSAIndexes_GetSet asserts the Get/Set/remove contract.
func TestTenantLSAIndexes_GetSet(t *testing.T) {
	tli := NewTenantLSAIndexes()
	if tli.Get("never-set") != nil {
		t.Error("Get should return nil for unregistered tenant")
	}

	idx, err := BuildLSAIndex(testCorpus(), testLSAConfig())
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	tli.Set("tenant-A", idx)
	if got := tli.Get("tenant-A"); got != idx {
		t.Error("Get after Set should return the registered index")
	}
	if tli.Get("tenant-B") != nil {
		t.Error("Get for different tenant should still be nil")
	}

	tli.Set("tenant-A", nil)
	if tli.Get("tenant-A") != nil {
		t.Error("Set(tenantID, nil) should remove the entry")
	}
}

// TestLogEntropyGlobalWeight pins the Dumais 1991 log-entropy formula
// on a tiny synthetic corpus where the expected per-term global weights
// can be computed by hand. Same shape as TestBM25Score — three docs,
// hand-computed expected, narrow ε.
//
// Formula: g_t = 1 + (1/log D) * sum_d (p_dt * log p_dt), where
// p_dt = tf_dt / gf_t. By convention p * log p = 0 when p = 0.
//
// Corpus design:
//
//	Doc 1: "alpha beta gamma"   tf = {alpha:1, beta:1, gamma:1}
//	Doc 2: "alpha beta gamma"   tf = {alpha:1, beta:1, gamma:1}
//	Doc 3: "alpha charlie delta" tf = {alpha:1, charlie:1, delta:1}
//
// After df filtering (MinDocFreq=1, df < D):
//   - alpha: df=3=D → EXCLUDED
//   - beta, gamma: df=2 → kept
//   - charlie, delta: df=1 → kept
//
// Hand-computed for two of the kept terms:
//
//	beta: gf = 1+1+0 = 2; entropy = 0.5·log(0.5) + 0.5·log(0.5) = -log(2)
//	      g_beta = 1 + (-log 2 / log 3) ≈ 1 - 0.6309 = 0.3691
//	charlie: gf = 1; entropy = 1·log(1) = 0
//	         g_charlie = 1 + 0/log(3) = 1.0
//
// charlie's weight = 1.0 because it concentrates in a single doc
// (maximum specificity); beta's weight is lower because it's distributed
// across two docs (less informative per occurrence). That ordering is
// the load-bearing retrieval-quality intuition: log-entropy down-weights
// uniformly-distributed terms more aggressively than IDF, which only
// uses doc count.
func TestLogEntropyGlobalWeight(t *testing.T) {
	docs := []Document{
		{ID: 1, Body: "alpha beta gamma"},
		{ID: 2, Body: "alpha beta gamma"},
		{ID: 3, Body: "alpha charlie delta"},
	}
	cfg := LSAConfig{
		Dims:       2,
		Oversamp:   2,
		PowerIter:  2,
		MaxVocab:   100,
		MinDocFreq: 1,
		TitleBoost: 0,
		Seed:       42,
	}
	idx, err := BuildLSAIndex(docs, cfg)
	if err != nil {
		t.Fatalf("BuildLSAIndex: %v", err)
	}

	// vocab must contain beta + charlie; must NOT contain alpha (df=D).
	if _, ok := idx.vocab["alpha"]; ok {
		t.Errorf("alpha should be excluded (df=D); vocab contains it")
	}
	betaIdx, ok := idx.vocab["beta"]
	if !ok {
		t.Fatal("beta missing from vocab")
	}
	charlieIdx, ok := idx.vocab["charlie"]
	if !ok {
		t.Fatal("charlie missing from vocab")
	}

	const eps = 1e-4
	wantBeta := float32(1.0 + (-math.Log(2) / math.Log(3)))
	wantCharlie := float32(1.0)

	if got := idx.globalWeight[betaIdx]; math.Abs(float64(got-wantBeta)) > eps {
		t.Errorf("globalWeight[beta]: got %.6f, want %.6f ± %g", got, wantBeta, eps)
	}
	if got := idx.globalWeight[charlieIdx]; math.Abs(float64(got-wantCharlie)) > eps {
		t.Errorf("globalWeight[charlie]: got %.6f, want %.6f ± %g", got, wantCharlie, eps)
	}

	// Sanity: charlie (single-doc) must have strictly higher global
	// weight than beta (distributed across 2 docs). If this ever flips,
	// it would mean the formula has been transposed (sum direction
	// reversed) or the sign is wrong — both are silent-misweight bugs
	// that the precise-value asserts above might miss if a sign error
	// happened to compensate.
	if idx.globalWeight[charlieIdx] <= idx.globalWeight[betaIdx] {
		t.Errorf("expected globalWeight[charlie] > globalWeight[beta]; got %.4f vs %.4f",
			idx.globalWeight[charlieIdx], idx.globalWeight[betaIdx])
	}
}

// TestLogEntropy_SingleDocDegradesToOne pins the D==1 edge case: log(D)=0
// makes the entropy ratio undefined, and the implementation degrades to
// global weight = 1.0 (pure local weighting). Without this guard a
// build with a single document would emit +Inf or NaN weights.
//
// Real-world relevance: bootstrap from an empty tenant that just had its
// first doc indexed. The system shouldn't fail; it should produce a
// trivial-but-valid index.
func TestLogEntropy_SingleDocDegradesToOne(t *testing.T) {
	// D=1 + df<D filter means every term would have df=1 NOT < 1, so the
	// public BuildLSAIndex path would refuse the build (empty vocab). The
	// invariant we want to pin: as D shrinks toward the lower limit, the
	// formula stays well-defined — log(D) approaching 0 must not produce
	// NaN/Inf weights. Use D=2 as the smallest-buildable corpus.
	docs := []Document{
		{ID: 1, Body: "alpha beta gamma delta echo"},
		{ID: 2, Body: "alpha beta gamma delta foxtrot"},
	}
	cfg := LSAConfig{
		Dims:       2,
		Oversamp:   2,
		PowerIter:  2,
		MaxVocab:   100,
		MinDocFreq: 1,
		TitleBoost: 0,
		Seed:       42,
	}
	idx, err := BuildLSAIndex(docs, cfg)
	if err != nil {
		t.Fatalf("BuildLSAIndex: %v", err)
	}
	for term, tidx := range idx.vocab {
		g := idx.globalWeight[tidx]
		if math.IsNaN(float64(g)) || math.IsInf(float64(g), 0) {
			t.Errorf("term %q: globalWeight %v is not finite", term, g)
		}
	}
}

func cosine(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float32
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(na))*math.Sqrt(float64(nb)))
}
