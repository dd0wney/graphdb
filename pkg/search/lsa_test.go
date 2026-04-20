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
