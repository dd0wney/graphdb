# COI Screen — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `coi-screen`, a CLI that screens two named parties for a hidden conflict of interest by resolving each to a node in the ICIJ Offshore Leaks graph and enumerating interest-bearing paths between them, returning ranked candidate conflicts with evidence.

**Architecture:** A new sibling Go module (`github.com/dd0wney/coi-screen`) that embeds graphdb (`github.com/dd0wney/cluso-graphdb`) as a library. Five-stage pipeline: load → resolve (type-aware record linkage) → connect (custom interest-restricted bounded BFS) → score (user-owned policy) → report. Generic graph operations stay in graphdb; COI policy lives here.

**Tech Stack:** Go 1.26, graphdb `pkg/storage` + `pkg/algorithms` (adjacency reads only), stdlib only for the matcher (no external deps).

**Spec:** `docs/superpowers/specs/2026-06-02-coi-screen-design.md` (spike-hardened 2026-06-02).

---

## File structure (locked)

```
coi-screen/
├── go.mod                       module github.com/dd0wney/coi-screen; replace -> ../graphdb
├── README.md
├── cmd/coi/main.go              CLI entrypoint: `coi screen --party A --party B`
└── internal/
    ├── linkage/                 Stage 2 — entity resolution (record linkage)
    │   ├── normalize.go         tokenize, accent-fold, honorific/suffix strip, person/company parse
    │   ├── similarity.go        jaroWinkler, soundex
    │   ├── resolver.go          Party, Kind, Band, score(), Resolve() against the graph
    │   └── *_test.go
    ├── graphload/               Stages 1 & 3 — load + connect
    │   ├── load.go              open embedded graphdb data dir; derive edge provenance
    │   ├── connect.go           Path, PathEdge, FindInterestPaths (custom bounded BFS)
    │   └── *_test.go
    ├── score/                   Stage 4 — USER-OWNED scoring policy
    │   ├── score.go             ScorePath (user authors body)
    │   └── score_test.go
    └── report/                  Stage 5 — candidate report
        ├── report.go            CandidateConflict, RenderJSON
        └── report_test.go
```

**Type contract shared across tasks (define exactly once, reuse verbatim):**
- `linkage.Kind` = `Person | Company`
- `linkage.Party{ Name, Jurisdiction string; Kind Kind }`
- `linkage.Band` = `"MATCH" | "REVIEW" | "NO"`
- `linkage.Candidate{ NodeID uint64; Name string; Score float64; Band Band }`
- `graphload.PathEdge{ From, To uint64; Type, Link, Provenance string }`
- `graphload.Path = []PathEdge`
- `score.ScorePath(p graphload.Path, startConf, endConf float64) (score float64, flagged bool)`

---

## Task 0: Bootstrap the coi-screen module

**Files:**
- Create: `../coi-screen/go.mod`
- Create: `../coi-screen/README.md`
- Create: `../coi-screen/internal/linkage/doc.go`

- [ ] **Step 1: Create the module directory and go.mod**

Run from the parent of the graphdb checkout:
```bash
mkdir -p ../coi-screen/internal/linkage ../coi-screen/internal/graphload \
         ../coi-screen/internal/score ../coi-screen/internal/report ../coi-screen/cmd/coi
cd ../coi-screen
cat > go.mod <<'EOF'
module github.com/dd0wney/coi-screen

go 1.26

require github.com/dd0wney/cluso-graphdb v0.0.0

replace github.com/dd0wney/cluso-graphdb => ../graphdb
EOF
```

- [ ] **Step 2: Add a placeholder package file so the module builds**

Create `internal/linkage/doc.go`:
```go
// Package linkage performs type-aware record-linkage entity resolution:
// matching a named query party to nodes in the ICIJ Offshore Leaks graph.
package linkage
```

Create `README.md`:
```markdown
# coi-screen

Conflict-of-interest screening over the ICIJ Offshore Leaks graph, built on graphdb.

`coi screen --party "A" --party "B"` resolves each party to graph nodes, enumerates
interest-bearing paths between them, and returns ranked candidate conflicts with evidence.

Candidates, never verdicts: every flag is a human-reviewable hypothesis with its path,
per-edge provenance, and a confidence score.
```

- [ ] **Step 3: Resolve dependencies and verify it builds**

Run:
```bash
cd ../coi-screen && go mod tidy && go build ./...
```
Expected: builds clean (the `replace` points at the local graphdb checkout; `go mod tidy` pulls graphdb's transitive deps).

- [ ] **Step 4: Commit**

```bash
cd ../coi-screen && git init -q && git add -A
git commit -q -m "chore: bootstrap coi-screen module embedding graphdb"
```

---

## Task 1: Name normalization

**Files:**
- Create: `../coi-screen/internal/linkage/normalize.go`
- Test: `../coi-screen/internal/linkage/normalize_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/linkage/normalize_test.go`:
```go
package linkage

import (
	"reflect"
	"testing"
)

func TestTokens(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"Mr. Robert J. Smith", []string{"robert", "j", "smith"}}, // honorific dropped
		{"José García", []string{"jose", "garcia"}},               // accent folded
		{"Acme Holdings & Co.", []string{"acme", "holdings", "co"}},
	}
	for _, tc := range tests {
		if got := tokens(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("tokens(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestPersonParts(t *testing.T) {
	tests := []struct {
		raw         string
		wantGiven   []string
		wantSurname string
	}{
		{"Robert Smith", []string{"robert"}, "smith"},
		{"SMITH, Robert", []string{"robert"}, "smith"}, // surname-first via comma
		{"Vladimir Putin", []string{"vladimir"}, "putin"},
	}
	for _, tc := range tests {
		g, s := personParts(tc.raw)
		if !reflect.DeepEqual(g, tc.wantGiven) || s != tc.wantSurname {
			t.Errorf("personParts(%q) = (%v,%q), want (%v,%q)", tc.raw, g, s, tc.wantGiven, tc.wantSurname)
		}
	}
}

func TestCompanyTokens(t *testing.T) {
	// legal suffix stripped, distinctive body kept
	if got := companyTokens("Acme Holdings Ltd"); !reflect.DeepEqual(got, []string{"acme", "holdings"}) {
		t.Errorf("companyTokens = %v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ../coi-screen && go test ./internal/linkage/ -run 'TestTokens|TestPersonParts|TestCompanyTokens' -v`
Expected: FAIL — `undefined: tokens` / `personParts` / `companyTokens`.

- [ ] **Step 3: Write the implementation**

Create `internal/linkage/normalize.go`:
```go
package linkage

import (
	"strings"
	"unicode"
)

var honorifics = map[string]bool{
	"mr": true, "mrs": true, "ms": true, "dr": true, "sir": true, "hon": true,
}

// only TRUE trailing legal forms are stripped; descriptive body words are kept,
// so "Acme Holdings" and "Acme Trading" stay distinguishable.
var legalSuffix = map[string]bool{
	"ltd": true, "limited": true, "inc": true, "incorporated": true, "corp": true,
	"corporation": true, "sa": true, "llc": true, "plc": true, "co": true, "gmbh": true,
}

func fold(r rune) rune {
	m := map[rune]rune{
		'á': 'a', 'à': 'a', 'â': 'a', 'ä': 'a', 'ã': 'a', 'å': 'a',
		'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e', 'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
		'ó': 'o', 'ò': 'o', 'ô': 'o', 'ö': 'o', 'õ': 'o', 'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
		'ñ': 'n', 'ç': 'c',
	}
	if v, ok := m[r]; ok {
		return v
	}
	return r
}

// tokens lowercases, folds accents, splits on non-letters, and drops honorifics.
func tokens(s string) []string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		r = fold(r)
		if unicode.IsLetter(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	out := []string{}
	for _, t := range strings.Fields(b.String()) {
		if honorifics[t] {
			continue
		}
		out = append(out, t)
	}
	return out
}

// personParts splits a person name into given tokens and a surname, honoring the
// "SURNAME, Given" ordering convention via the comma.
func personParts(raw string) (given []string, surname string) {
	if i := strings.Index(raw, ","); i >= 0 {
		pre := tokens(raw[:i])
		if len(pre) > 0 {
			surname = pre[len(pre)-1]
		}
		given = tokens(raw[i+1:])
		return given, surname
	}
	t := tokens(raw)
	if len(t) == 0 {
		return nil, ""
	}
	return t[:len(t)-1], t[len(t)-1]
}

// companyTokens returns the distinctive tokens of a company name with trailing
// legal forms removed.
func companyTokens(raw string) []string {
	t := tokens(raw)
	out := []string{}
	for _, x := range t {
		if legalSuffix[x] {
			continue
		}
		out = append(out, x)
	}
	if len(out) == 0 {
		out = t
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ../coi-screen && go test ./internal/linkage/ -run 'TestTokens|TestPersonParts|TestCompanyTokens' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
cd ../coi-screen && git add internal/linkage/normalize.go internal/linkage/normalize_test.go
git commit -q -m "feat(linkage): name normalization (accent-fold, honorific strip, person/company parse)"
```

---

## Task 2: Similarity primitives (Jaro-Winkler + Soundex)

**Files:**
- Create: `../coi-screen/internal/linkage/similarity.go`
- Test: `../coi-screen/internal/linkage/similarity_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/linkage/similarity_test.go`:
```go
package linkage

import "testing"

func TestJaroWinkler(t *testing.T) {
	if jw("smith", "smith") != 1.0 {
		t.Errorf("identical strings must score 1.0")
	}
	if v := jw("mossack", "mosack"); v < 0.9 {
		t.Errorf("close transliteration should score high, got %.3f", v)
	}
	if v := jw("acme", "xyzzy"); v > 0.4 {
		t.Errorf("dissimilar strings should score low, got %.3f", v)
	}
}

func TestSoundex(t *testing.T) {
	if soundex("Robert") != soundex("Rupert") {
		t.Errorf("Robert/Rupert should share a soundex code")
	}
	if soundex("Smith") == soundex("Jones") {
		t.Errorf("Smith/Jones should differ")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ../coi-screen && go test ./internal/linkage/ -run 'TestJaroWinkler|TestSoundex' -v`
Expected: FAIL — `undefined: jw` / `soundex`.

- [ ] **Step 3: Write the implementation**

Create `internal/linkage/similarity.go`:
```go
package linkage

import "strings"

func jaro(s1, s2 string) float64 {
	if s1 == s2 {
		return 1
	}
	l1, l2 := len(s1), len(s2)
	if l1 == 0 || l2 == 0 {
		return 0
	}
	d := l1
	if l2 > d {
		d = l2
	}
	d = d/2 - 1
	if d < 0 {
		d = 0
	}
	m1, m2 := make([]bool, l1), make([]bool, l2)
	matches := 0
	for i := 0; i < l1; i++ {
		st := i - d
		if st < 0 {
			st = 0
		}
		en := i + d + 1
		if en > l2 {
			en = l2
		}
		for j := st; j < en; j++ {
			if m2[j] || s1[i] != s2[j] {
				continue
			}
			m1[i], m2[j] = true, true
			matches++
			break
		}
	}
	if matches == 0 {
		return 0
	}
	t, k := 0, 0
	for i := 0; i < l1; i++ {
		if !m1[i] {
			continue
		}
		for !m2[k] {
			k++
		}
		if s1[i] != s2[k] {
			t++
		}
		k++
	}
	mf := float64(matches)
	return (mf/float64(l1) + mf/float64(l2) + (mf-float64(t)/2)/mf) / 3
}

// jw is Jaro-Winkler: Jaro with a common-prefix boost.
func jw(a, b string) float64 {
	j := jaro(a, b)
	p := 0
	for p < len(a) && p < len(b) && p < 4 && a[p] == b[p] {
		p++
	}
	return j + float64(p)*0.1*(1-j)
}

// soundex is the classic phonetic code (letter + 3 digits).
func soundex(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ToUpper(s)
	code := map[rune]byte{
		'B': '1', 'F': '1', 'P': '1', 'V': '1',
		'C': '2', 'G': '2', 'J': '2', 'K': '2', 'Q': '2', 'S': '2', 'X': '2', 'Z': '2',
		'D': '3', 'T': '3', 'L': '4', 'M': '5', 'N': '5', 'R': '6',
	}
	out := []byte{s[0]}
	prev := code[rune(s[0])]
	for _, r := range s[1:] {
		c := code[r]
		if c != 0 && c != prev {
			out = append(out, c)
		}
		if r != 'H' && r != 'W' {
			prev = c
		}
	}
	for len(out) < 4 {
		out = append(out, '0')
	}
	return string(out[:4])
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd ../coi-screen && go test ./internal/linkage/ -run 'TestJaroWinkler|TestSoundex' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
cd ../coi-screen && git add internal/linkage/similarity.go internal/linkage/similarity_test.go
git commit -q -m "feat(linkage): jaro-winkler + soundex similarity primitives"
```

---

## Task 3: Type-aware match scoring + banding (spike-validated core)

**Files:**
- Create: `../coi-screen/internal/linkage/resolver.go` (types + `score` + `Band` only; `Resolve` added in Task 4)
- Test: `../coi-screen/internal/linkage/resolver_test.go`

- [ ] **Step 1: Write the failing test (the spike's labelled fixture is the spec)**

Create `internal/linkage/resolver_test.go`:
```go
package linkage

import "testing"

func TestScoreAndBand(t *testing.T) {
	tests := []struct {
		a, b     Party
		wantBand Band
		name     string
	}{
		// true matches must auto-MATCH
		{Party{"Mr. Robert J. Smith", "BVI", Person}, Party{"Robert Smith", "BVI", Person}, BandMatch, "honorific+middle"},
		{Party{"SMITH, Robert", "BVI", Person}, Party{"Robert Smith", "BVI", Person}, BandMatch, "surname-first"},
		{Party{"Acme Holdings Limited", "Panama", Company}, Party{"Acme Holdings Ltd", "Panama", Company}, BandMatch, "suffix variant"},
		{Party{"José García", "Panama", Person}, Party{"Jose Garcia", "Panama", Person}, BandMatch, "accent fold"},
		// hard negatives must NOT auto-MATCH
		{Party{"Robert Smith", "BVI", Person}, Party{"Robert Smith", "Panama", Person}, BandNo, "diff jurisdiction"},
		{Party{"Acme Holdings Ltd", "Panama", Company}, Party{"Acme Trading Ltd", "Panama", Company}, BandNo, "diff company body"},
		// adversarial near-duplicates must route to REVIEW, never auto-merge
		{Party{"Maria Lopez", "Mexico", Person}, Party{"Mario Lopez", "Mexico", Person}, BandReview, "given-name diff"},
	}
	for _, tc := range tests {
		got := band(score(tc.a, tc.b))
		if got != tc.wantBand {
			t.Errorf("%s: band = %q, want %q", tc.name, got, tc.wantBand)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ../coi-screen && go test ./internal/linkage/ -run TestScoreAndBand -v`
Expected: FAIL — `undefined: Party` / `Person` / `score` / `band` / `Band*`.

- [ ] **Step 3: Write the implementation**

Create `internal/linkage/resolver.go`:
```go
package linkage

import "strings"

// Kind distinguishes the two matchers; maps to ICIJ node_type (Officer=Person, Entity=Company).
type Kind int

const (
	Person Kind = iota
	Company
)

// Party is a name + optional attributes to resolve against the graph.
type Party struct {
	Name string
	// Jurisdiction is the hard-gate attribute: an ICIJ jurisdiction for companies, a
	// country code for persons (Officers carry country, not jurisdiction). Empty => gate
	// does not fire for this party (precision degrades; see resolver gateAttr).
	Jurisdiction string
	Kind         Kind
}

// Band is the resolution outcome. REVIEW requires human confirmation before any merge.
type Band string

const (
	BandMatch  Band = "MATCH"  // confident auto-merge
	BandReview Band = "REVIEW" // plausible; human confirms (reversible)
	BandNo     Band = "NO"     // not a match
)

const (
	matchThreshold  = 0.92
	reviewThreshold = 0.80
)

func band(s float64) Band {
	switch {
	case s >= matchThreshold:
		return BandMatch
	case s >= reviewThreshold:
		return BandReview
	default:
		return BandNo
	}
}

// givenCompatible: exact, or initial-vs-full. Two distinct FULL given names
// (maria vs mario) are NOT compatible -> forces REVIEW, never auto-merge.
func givenCompatible(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return true
	}
	ga, gb := a[0], b[0]
	if ga == gb {
		return true
	}
	if len(ga) == 1 || len(gb) == 1 {
		return ga[0] == gb[0]
	}
	return false
}

// score returns a [0,1] match score. Type-aware, with a hard jurisdiction gate.
func score(x, y Party) float64 {
	// HARD attribute gate: known, differing jurisdictions => distinct entity.
	if x.Jurisdiction != "" && y.Jurisdiction != "" && !strings.EqualFold(x.Jurisdiction, y.Jurisdiction) {
		return 0.20
	}
	if x.Kind == Person {
		gx, sx := personParts(x.Name)
		gy, sy := personParts(y.Name)
		surn := jw(sx, sy)
		if surn < 0.90 { // surname must match strongly
			return surn * 0.6
		}
		if !givenCompatible(gx, gy) {
			return reviewThreshold + 0.05 // strong surname, distinct given -> REVIEW
		}
		g := 1.0
		if len(gx) > 0 && len(gy) > 0 {
			g = jw(gx[0], gy[0])
		}
		return 0.6*surn + 0.4*g
	}
	// company: token-set best-match average + weakest-link floor.
	tx, ty := companyTokens(x.Name), companyTokens(y.Name)
	small, large := tx, ty
	if len(ty) < len(tx) {
		small, large = ty, tx
	}
	if len(small) == 0 {
		return 0
	}
	sum, weakest := 0.0, 1.0
	for _, a := range small {
		best := 0.0
		for _, b := range large {
			if v := jw(a, b); v > best {
				best = v
			}
		}
		sum += best
		if best < weakest {
			weakest = best
		}
	}
	avg := sum / float64(len(small))
	return 0.7*avg + 0.3*weakest
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ../coi-screen && go test ./internal/linkage/ -run TestScoreAndBand -v`
Expected: PASS. (These are the exact cases the spike validated; if any regress, the matcher constants changed.)

- [ ] **Step 5: Commit**

```bash
cd ../coi-screen && git add internal/linkage/resolver.go internal/linkage/resolver_test.go
git commit -q -m "feat(linkage): type-aware scoring + MATCH/REVIEW/NO banding"
```

---

## Task 4: Resolve a party against the graph

**Files:**
- Modify: `../coi-screen/internal/linkage/resolver.go` (add `Candidate` + `Resolve`)
- Test: `../coi-screen/internal/linkage/resolve_graph_test.go`

- [ ] **Step 1: Write the failing test (with an in-memory graph fixture)**

Create `internal/linkage/resolve_graph_test.go`:
```go
package linkage

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func newTestGraph(t *testing.T) *storage.GraphStorage {
	t.Helper()
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("new graph: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	return gs
}

func addOfficer(t *testing.T, gs *storage.GraphStorage, name, juris string) uint64 {
	t.Helper()
	n, err := gs.CreateNode([]string{"Officer"}, map[string]storage.Value{
		"name":         storage.StringValue(name),
		"jurisdiction": storage.StringValue(juris),
	})
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	return n.ID
}

func TestResolveFindsMatchAndExcludesDistinct(t *testing.T) {
	gs := newTestGraph(t)
	want := addOfficer(t, gs, "Robert J. Smith", "BVI")
	addOfficer(t, gs, "Robert Smith", "Panama") // distinct: different jurisdiction
	addOfficer(t, gs, "Jane Doe", "BVI")        // unrelated

	cands, err := resolveCandidates(gs, Party{"Robert Smith", "BVI", Person}, "")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	var matched []Candidate
	for _, c := range cands {
		if c.Band == BandMatch {
			matched = append(matched, c)
		}
	}
	if len(matched) != 1 || matched[0].NodeID != want {
		t.Fatalf("want exactly one MATCH on node %d, got %+v", want, matched)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ../coi-screen && go test ./internal/linkage/ -run TestResolveFindsMatch -v`
Expected: FAIL — `undefined: resolveCandidates` / `Candidate`.

- [ ] **Step 3: Write the implementation (extend resolver.go)**

First, change the import line at the top of `internal/linkage/resolver.go` from `import "strings"` to:
```go
import (
	"sort"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)
```
Then append these declarations to the end of `internal/linkage/resolver.go`:
```go
// Candidate is one resolved graph node with its match score and band.
type Candidate struct {
	NodeID uint64
	Name   string
	Score  float64
	Band   Band
}

// labelForKind maps the matcher kind to the ICIJ node label the importer writes.
func labelForKind(k Kind) string {
	if k == Company {
		return "Entity"
	}
	return "Officer"
}

// resolveCandidates scans nodes of the party's kind, scores each, and returns the
// MATCH/REVIEW candidates sorted by descending score. Blocking: candidates whose
// surname/head-token soundex differs are skipped before scoring to cut the scan cost.
// NOTE: this is a linear scan over the label set; on the full ~800K corpus a soundex
// index should replace it (tracked as a follow-up — see spec Milestone-1-proper).
func resolveCandidates(gs *storage.GraphStorage, p Party, tenantID string) ([]Candidate, error) {
	nodes := gs.GetNodesByLabelForTenant(tenantID, labelForKind(p.Kind))
	block := blockKey(p)
	var out []Candidate
	for _, n := range nodes {
		name := stringProp(n, "name")
		if name == "" {
			continue
		}
		// gate attribute is country for persons, jurisdiction for companies (see gateAttr).
		cand := Party{Name: name, Jurisdiction: gateAttr(n, p.Kind), Kind: p.Kind}
		if block != "" && blockKey(cand) != block {
			continue // cheap phonetic pre-filter
		}
		s := score(p, cand)
		b := band(s)
		if b == BandNo {
			continue
		}
		out = append(out, Candidate{NodeID: n.ID, Name: name, Score: s, Band: b})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out, nil
}

// gateAttr returns the value used for the hard match-gate. CRITICAL (spike/advisor
// finding): ICIJ Officer (person) nodes carry country_codes/countries, NOT jurisdiction
// — jurisdiction is an Entity attribute. Gating persons on jurisdiction would never fire
// and precision would collapse to surname-matching, so persons gate on country.
func gateAttr(n *storage.Node, k Kind) string {
	if k == Person {
		if c := stringProp(n, "country_codes"); c != "" {
			return c
		}
		return stringProp(n, "countries")
	}
	if j := stringProp(n, "jurisdiction"); j != "" {
		return j
	}
	return stringProp(n, "country_codes")
}

// blockKey is the soundex of the most distinctive token (surname for a person,
// last body token for a company); "" if the name has no usable token.
func blockKey(p Party) string {
	if p.Kind == Person {
		_, sn := personParts(p.Name)
		return soundex(sn)
	}
	ct := companyTokens(p.Name)
	if len(ct) == 0 {
		return ""
	}
	return soundex(ct[len(ct)-1])
}

func stringProp(n *storage.Node, key string) string {
	if v, ok := n.Properties[key]; ok {
		if s, err := v.AsString(); err == nil {
			return s
		}
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ../coi-screen && go test ./internal/linkage/ -run TestResolveFindsMatch -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd ../coi-screen && git add internal/linkage/resolver.go internal/linkage/resolve_graph_test.go
git commit -q -m "feat(linkage): resolve a party to graph candidates with soundex blocking"
```

---

## Task 5: Interest-restricted bounded path enumeration (connect stage)

**Files:**
- Create: `../coi-screen/internal/graphload/connect.go`
- Test: `../coi-screen/internal/graphload/connect_test.go`

- [ ] **Step 1: Write the failing test (planted shared-owner conflict)**

Create `internal/graphload/connect_test.go`:
```go
package graphload

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// graph: officerA --officer_of--> companyX <--officer_of-- officerB
// A hidden conflict: A and B both control X. Path A->X->B exists via officer_of (length 2).
func TestFindInterestPathsSharedOwner(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Close()
	mk := func(label, name string) uint64 {
		n, _ := gs.CreateNode([]string{label}, map[string]storage.Value{"name": storage.StringValue(name), "source": storage.StringValue("panama_papers")})
		return n.ID
	}
	a := mk("Officer", "A")
	b := mk("Officer", "B")
	x := mk("Entity", "X")
	noise := mk("Entity", "Noise")
	edge := func(from, to uint64, typ, link string) {
		_, _ = gs.CreateEdge(from, to, typ, map[string]storage.Value{"link": storage.StringValue(link)}, 1.0)
	}
	edge(a, x, "officer_of", "shareholder of")
	edge(b, x, "officer_of", "director of")
	edge(a, noise, "registered_address", "") // a weak edge that must not bridge A->B

	paths, _, err := FindInterestPaths(gs, a, b,
		[]string{"officer_of", "intermediary_of", "registered_address"},
		4 /*maxHops*/, 100 /*maxResults*/, 1000 /*maxDegree*/, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("expected at least one path A->B through the shared company")
	}
	// the shortest path is A -officer_of-> X -officer_of-> B (2 edges)
	if got := len(paths[0]); got != 2 {
		t.Fatalf("shortest path length = %d edges, want 2", got)
	}
	if paths[0][0].Link != "shareholder of" {
		t.Errorf("first edge link = %q, want carried through", paths[0][0].Link)
	}
}

// A mega-hub (one address linked to many entities) must NOT be expanded through:
// a path that would only connect A and B by passing through the shared hub is noise.
func TestFindInterestPathsSkipsMegaHub(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Close()
	mk := func(label string) uint64 {
		n, _ := gs.CreateNode([]string{label}, map[string]storage.Value{"source": storage.StringValue("panama_papers")})
		return n.ID
	}
	a := mk("Officer")
	b := mk("Officer")
	hub := mk("Address") // shared by many entities
	gs.CreateEdge(a, hub, "registered_address", nil, 1.0)
	gs.CreateEdge(b, hub, "registered_address", nil, 1.0)
	for i := 0; i < 50; i++ { // inflate the hub's degree past maxDegree
		gs.CreateEdge(mk("Entity"), hub, "registered_address", nil, 1.0)
	}
	// maxDegree=10 < hub degree(52) => expansion through the hub is skipped; truncated=true.
	paths, truncated, err := FindInterestPaths(gs, a, b,
		[]string{"registered_address"}, 4, 100, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 0 {
		t.Fatalf("mega-hub path should be skipped, got %d paths", len(paths))
	}
	if !truncated {
		t.Error("expected truncated=true when the hub guard fires")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd ../coi-screen && go test ./internal/graphload/ -run TestFindInterestPaths -v`
Expected: FAIL — `undefined: FindInterestPaths`.

- [ ] **Step 3: Write the implementation**

Create `internal/graphload/connect.go`:
```go
package graphload

import (
	"sort"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// PathEdge is one hop in a conflict path, carrying the evidence needed for a report.
type PathEdge struct {
	From       uint64
	To         uint64
	Type       string // ICIJ rel_type (officer_of, intermediary_of, registered_address)
	Link       string // fine-grained role from the edge `link` property (e.g. "shareholder of")
	Provenance string // dataset source derived from the endpoint node (e.g. "panama_papers")
}

// Path is an ordered chain of hops from the start party to the end party.
type Path []PathEdge

// FindInterestPaths enumerates simple paths (no repeated node) from startID to endID
// of length <= maxHops, traversing edges undirected but RESTRICTED to the allowed
// relationship types. Returned shortest-first. graphdb supplies adjacency; the
// interest-restriction policy lives here. (pkg/algorithms has no edge-typed pathing.)
//
// Two guards make this safe on real ICIJ data (advisor finding — fixtures can't show it):
//   - maxResults caps the number of paths returned (0 = unlimited; avoid on real data).
//   - maxDegree skips expansion THROUGH any intermediate node whose allowed-edge degree
//     exceeds it: an address shared by 10,000 shells is noise, not evidence, and a
//     4-hop DFS through such a hub is combinatorially explosive.
//
// Returns truncated=true if either guard fired, so the caller can disclose it (no
// silent caps).
func FindInterestPaths(gs *storage.GraphStorage, startID, endID uint64, allowed []string, maxHops, maxResults, maxDegree int, tenantID string) (paths []Path, truncated bool, err error) {
	allowSet := map[string]bool{}
	for _, t := range allowed {
		allowSet[t] = true
	}
	var results []Path
	visited := map[uint64]bool{startID: true}
	var cur Path

	var dfs func(node uint64, depth int) error
	dfs = func(node uint64, depth int) error {
		if depth > maxHops {
			return nil
		}
		if maxResults > 0 && len(results) >= maxResults {
			truncated = true
			return nil
		}
		neighbors, e := adjacency(gs, node, allowSet, tenantID)
		if e != nil {
			return e
		}
		// hub guard: do not expand THROUGH a mega-hub (but the start node itself,
		// depth==1, is always expanded — we screen FROM it regardless of its degree).
		if maxDegree > 0 && depth > 1 && len(neighbors) > maxDegree {
			truncated = true
			return nil
		}
		for _, nb := range neighbors {
			if visited[nb.to] {
				continue
			}
			cur = append(cur, nb.edge)
			if nb.to == endID {
				p := make(Path, len(cur))
				copy(p, cur)
				results = append(results, p)
			} else {
				visited[nb.to] = true
				if err2 := dfs(nb.to, depth+1); err2 != nil {
					return err2
				}
				visited[nb.to] = false
			}
			cur = cur[:len(cur)-1]
			if maxResults > 0 && len(results) >= maxResults {
				truncated = true
				return nil
			}
		}
		return nil
	}
	if err = dfs(startID, 1); err != nil {
		return nil, truncated, err
	}
	sort.Slice(results, func(i, j int) bool { return len(results[i]) < len(results[j]) })
	return results, truncated, nil
}

type neighbor struct {
	to   uint64
	edge PathEdge
}

// adjacency returns the allowed-type neighbors of node in BOTH directions.
func adjacency(gs *storage.GraphStorage, node uint64, allow map[string]bool, tenantID string) ([]neighbor, error) {
	var out []neighbor
	outE, err := gs.GetOutgoingEdgesForTenant(node, tenantID)
	if err != nil {
		return nil, err
	}
	for _, e := range outE {
		if !allow[e.Type] {
			continue
		}
		out = append(out, neighbor{to: e.ToNodeID, edge: pathEdge(gs, e, e.FromNodeID, e.ToNodeID, tenantID)})
	}
	inE, err := gs.GetIncomingEdgesForTenant(node, tenantID)
	if err != nil {
		return nil, err
	}
	for _, e := range inE {
		if !allow[e.Type] {
			continue
		}
		// traverse against direction: the reachable node is the edge's source
		out = append(out, neighbor{to: e.FromNodeID, edge: pathEdge(gs, e, e.FromNodeID, e.ToNodeID, tenantID)})
	}
	return out, nil
}

func pathEdge(gs *storage.GraphStorage, e *storage.Edge, from, to uint64, tenantID string) PathEdge {
	pe := PathEdge{From: from, To: to, Type: e.Type}
	if v, ok := e.Properties["link"]; ok {
		if s, err := v.AsString(); err == nil {
			pe.Link = s
		}
	}
	// edge provenance gap (spike finding): the importer does not tag edges with a
	// source, so derive dataset provenance from the source node's `source` property.
	if n, err := gs.GetNodeForTenant(from, tenantID); err == nil {
		if v, ok := n.Properties["source"]; ok {
			if s, err := v.AsString(); err == nil {
				pe.Provenance = s
			}
		}
	}
	return pe
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ../coi-screen && go test ./internal/graphload/ -run TestFindInterestPaths -v`
Expected: PASS — shared-owner finds one path of length 2 (link "shareholder of"); mega-hub finds zero paths with truncated=true.

- [ ] **Step 5: Commit**

```bash
cd ../coi-screen && git add internal/graphload/connect.go internal/graphload/connect_test.go
git commit -q -m "feat(graphload): interest-restricted bounded path enumeration"
```

---

## Task 6: ScorePath — USER-OWNED conflict policy

**Files:**
- Create: `../coi-screen/internal/score/score.go`
- Test: `../coi-screen/internal/score/score_test.go`

> **This is the one piece of genuine domain policy. The function body is intentionally
> left for the user to author — it encodes *what counts as a conflict and how strongly*.
> This task provides the signature, the inputs, a runnable default, and a golden test the
> user fills in. The default is a placeholder the user replaces.**

- [ ] **Step 1: Write the test scaffold (user completes the golden expectations)**

Create `internal/score/score_test.go`:
```go
package score

import (
	"testing"

	"github.com/dd0wney/coi-screen/internal/graphload"
)

func TestScorePathRanksShorterStrongerHigher(t *testing.T) {
	// short, strong-interest path (direct co-ownership)
	strong := graphload.Path{
		{Type: "officer_of", Link: "shareholder of"},
		{Type: "officer_of", Link: "shareholder of"},
	}
	// long, weak path (shared address only)
	weak := graphload.Path{
		{Type: "registered_address"},
		{Type: "registered_address"},
		{Type: "registered_address"},
	}
	sStrong, _ := ScorePath(strong, 1.0, 1.0)
	sWeak, _ := ScorePath(weak, 1.0, 1.0)
	if sStrong <= sWeak {
		t.Errorf("strong/short path (%.3f) must outscore weak/long path (%.3f)", sStrong, sWeak)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ../coi-screen && go test ./internal/score/ -v`
Expected: FAIL — `undefined: ScorePath`.

- [ ] **Step 3: Provide the signature + default; USER authors the real body**

Create `internal/score/score.go`:
```go
// Package score holds the conflict-of-interest scoring POLICY.
//
// ScorePath is intentionally the project's one piece of human-authored domain logic:
// it decides how a candidate path's length, edge-interest weights, and resolution
// confidence combine into a rank and a flag. Tune the weights/threshold to your COI
// policy. The default below is a reasonable starting point, NOT a finished policy.
package score

import "github.com/dd0wney/coi-screen/internal/graphload"

// interestWeight maps the fine-grained role (edge `link`, falling back to edge type)
// to an interest strength. USER: tune these to your policy.
func interestWeight(e graphload.PathEdge) float64 {
	switch e.Link {
	case "shareholder of", "beneficial owner of", "owner of":
		return 1.0
	case "director of":
		return 0.6
	}
	switch e.Type {
	case "officer_of":
		return 0.6
	case "intermediary_of":
		return 0.4
	case "registered_address":
		return 0.2
	}
	return 0.1
}

// ScorePath ranks one candidate conflict path. Higher = stronger candidate.
// Inputs: the path (with per-edge type + link), and the resolution confidence of the
// two endpoint parties [0,1]. Returns the score and whether it crosses the flag threshold.
//
// USER: this default multiplies the weakest interest link by an inverse-length factor
// and the endpoint confidences. Replace with your policy.
func ScorePath(p graphload.Path, startConf, endConf float64) (score float64, flagged bool) {
	if len(p) == 0 {
		return 0, false
	}
	weakest := 1.0
	for _, e := range p {
		if w := interestWeight(e); w < weakest {
			weakest = w
		}
	}
	lengthFactor := 1.0 / float64(len(p)) // shorter chains score higher
	score = weakest * lengthFactor * startConf * endConf
	const flagThreshold = 0.20 // USER: tune
	return score, score >= flagThreshold
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ../coi-screen && go test ./internal/score/ -v`
Expected: PASS with the default. **Hand off to the user to tune `interestWeight`, the
combination formula, and `flagThreshold`, then extend the golden test with their cases.**

- [ ] **Step 5: Commit**

```bash
cd ../coi-screen && git add internal/score/score.go internal/score/score_test.go
git commit -q -m "feat(score): ScorePath policy scaffold (default weights; user to tune)"
```

---

## Task 7: Candidate report + JSON rendering

**Files:**
- Create: `../coi-screen/internal/report/report.go`
- Test: `../coi-screen/internal/report/report_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/report/report_test.go`:
```go
package report

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dd0wney/coi-screen/internal/graphload"
)

func TestRenderJSONIncludesPathProvenanceAndFlag(t *testing.T) {
	cc := []CandidateConflict{{
		PartyA: "Robert Smith", PartyB: "Acme Ltd",
		Score: 0.5, Flagged: true, Confidence: 0.9,
		Path: graphload.Path{{From: 1, To: 2, Type: "officer_of", Link: "shareholder of", Provenance: "panama_papers"}},
	}}
	out, err := RenderJSON(cc)
	if err != nil {
		t.Fatal(err)
	}
	var round []map[string]any
	if err := json.Unmarshal(out, &round); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	s := string(out)
	for _, want := range []string{"shareholder of", "panama_papers", "flagged"} {
		if !strings.Contains(s, want) {
			t.Errorf("report JSON missing %q", want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ../coi-screen && go test ./internal/report/ -v`
Expected: FAIL — `undefined: CandidateConflict` / `RenderJSON`.

- [ ] **Step 3: Write the implementation**

Create `internal/report/report.go`:
```go
// Package report assembles ranked candidate conflicts into a defensible, human-reviewable
// output. The tool surfaces candidates; it never renders a verdict.
package report

import (
	"encoding/json"

	"github.com/dd0wney/coi-screen/internal/graphload"
)

// CandidateConflict is one screened hypothesis: a path between two parties plus its
// evidence. `Flagged` means it crossed the policy threshold — still a candidate, not a verdict.
type CandidateConflict struct {
	PartyA     string         `json:"party_a"`
	PartyB     string         `json:"party_b"`
	Score      float64        `json:"score"`
	Flagged    bool           `json:"flagged"`
	Confidence float64        `json:"confidence"` // min of the two endpoint resolution confidences
	Path       graphload.Path `json:"path"`       // per-edge type, link (role), provenance
}

// RenderJSON emits the candidates as indented JSON suitable for piping/automation.
func RenderJSON(cc []CandidateConflict) ([]byte, error) {
	return json.MarshalIndent(cc, "", "  ")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ../coi-screen && go test ./internal/report/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd ../coi-screen && git add internal/report/report.go internal/report/report_test.go
git commit -q -m "feat(report): candidate-conflict JSON rendering"
```

---

## Task 8: Graph loader (open an ICIJ-populated data dir)

**Files:**
- Create: `../coi-screen/internal/graphload/load.go`
- Test: `../coi-screen/internal/graphload/load_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/graphload/load_test.go`:
```go
package graphload

import "testing"

func TestOpenMissingDirErrors(t *testing.T) {
	if _, err := Open("/nonexistent/path/does/not/exist"); err == nil {
		t.Fatal("expected an error opening a nonexistent data dir")
	}
}

func TestOpenRoundTrip(t *testing.T) {
	dir := t.TempDir()
	gs, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer gs.Close()
	if gs == nil {
		t.Fatal("expected a non-nil storage handle")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ../coi-screen && go test ./internal/graphload/ -run TestOpen -v`
Expected: FAIL — `undefined: Open`.

- [ ] **Step 3: Write the implementation**

Create `internal/graphload/load.go`:
```go
package graphload

import (
	"fmt"
	"os"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Open opens a graphdb data directory previously populated by `import-icij`.
// The directory must already exist; we do not create corpora here (run import-icij first).
func Open(dataDir string) (*storage.GraphStorage, error) {
	if fi, err := os.Stat(dataDir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("data dir %q not found or not a directory: %w", dataDir, err)
	}
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{
		DataDir:               dataDir,
		EnableEdgeCompression: true,
		EdgeCacheSize:         50000,
	})
	if err != nil {
		return nil, fmt.Errorf("open graph storage: %w", err)
	}
	return gs, nil
}
```

> Note: `os.Stat` on `t.TempDir()` succeeds (it exists), so `TestOpenRoundTrip` passes;
> `/nonexistent/...` fails the stat, so `TestOpenMissingDirErrors` passes.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd ../coi-screen && go test ./internal/graphload/ -run TestOpen -v`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
cd ../coi-screen && git add internal/graphload/load.go internal/graphload/load_test.go
git commit -q -m "feat(graphload): open an ICIJ-populated graphdb data dir"
```

---

## Task 9: CLI wiring (`coi screen`)

**Files:**
- Create: `../coi-screen/cmd/coi/main.go`
- Test: `../coi-screen/cmd/coi/main_test.go`

- [ ] **Step 1: Write the failing test (pipeline wiring on an in-memory graph)**

Create `cmd/coi/main_test.go`:
```go
package main

import (
	"strings"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/coi-screen/internal/linkage"
)

func TestScreenFindsSharedOwnerConflict(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer gs.Close()
	mkO := func(name string) uint64 {
		n, _ := gs.CreateNode([]string{"Officer"}, map[string]storage.Value{"name": storage.StringValue(name), "source": storage.StringValue("panama_papers")})
		return n.ID
	}
	a := mkO("Robert Smith")
	b := mkO("Jane Doe")
	x, _ := gs.CreateNode([]string{"Entity"}, map[string]storage.Value{"name": storage.StringValue("Acme Ltd")})
	gs.CreateEdge(a, x.ID, "officer_of", map[string]storage.Value{"link": storage.StringValue("shareholder of")}, 1.0)
	gs.CreateEdge(b, x.ID, "officer_of", map[string]storage.Value{"link": storage.StringValue("director of")}, 1.0)

	out, err := screen(gs, "",
		linkage.Party{Name: "Robert Smith", Kind: linkage.Person},
		linkage.Party{Name: "Jane Doe", Kind: linkage.Person}, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "shareholder of") {
		t.Fatalf("expected a conflict path citing the shareholding, got:\n%s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd ../coi-screen && go test ./cmd/coi/ -run TestScreen -v`
Expected: FAIL — `undefined: screen`.

- [ ] **Step 3: Write the implementation**

Create `cmd/coi/main.go`:
```go
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/coi-screen/internal/graphload"
	"github.com/dd0wney/coi-screen/internal/linkage"
	"github.com/dd0wney/coi-screen/internal/report"
	"github.com/dd0wney/coi-screen/internal/score"
)

const interestEdges = "officer_of,intermediary_of,registered_address"

// screen runs the full pipeline for two parties and returns the JSON report.
func screen(gs *storage.GraphStorage, tenantID string, a, b linkage.Party, maxHops int) ([]byte, error) {
	candA, err := linkage.Resolve(gs, a, tenantID)
	if err != nil {
		return nil, err
	}
	candB, err := linkage.Resolve(gs, b, tenantID)
	if err != nil {
		return nil, err
	}
	if len(candA) == 0 || len(candB) == 0 {
		return report.RenderJSON(nil) // no resolvable party -> empty (honest) result
	}
	allowed := []string{"officer_of", "intermediary_of", "registered_address"}
	const (
		maxResults = 200  // cap candidate paths per node-pair (no silent unbounded growth)
		maxDegree  = 2000 // skip expansion through mega-hubs (shared addresses, big intermediaries)
	)
	var conflicts []report.CandidateConflict
	anyTruncated := false
	for _, ca := range candA {
		for _, cb := range candB {
			if ca.NodeID == cb.NodeID {
				continue
			}
			paths, truncated, err := graphload.FindInterestPaths(gs, ca.NodeID, cb.NodeID, allowed, maxHops, maxResults, maxDegree, tenantID)
			if err != nil {
				return nil, err
			}
			anyTruncated = anyTruncated || truncated
			conf := min2(ca.Score, cb.Score)
			for _, p := range paths {
				s, flagged := score.ScorePath(p, ca.Score, cb.Score)
				conflicts = append(conflicts, report.CandidateConflict{
					PartyA: ca.Name, PartyB: cb.Name,
					Score: s, Flagged: flagged, Confidence: conf, Path: p,
				})
			}
		}
	}
	if anyTruncated {
		// no silent caps: tell the operator the search was bounded.
		fmt.Fprintln(os.Stderr, "warning: path search hit a result/hub cap; some candidate paths may be omitted")
	}
	return report.RenderJSON(conflicts)
}

func min2(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func main() {
	var parties multiFlag
	flag.Var(&parties, "party", "a named party to screen (repeat for each; need >=2)")
	dataDir := flag.String("data", "./data/icij", "graphdb data dir populated by import-icij")
	maxHops := flag.Int("max-hops", 4, "maximum path length to search")
	company := flag.Bool("company", false, "treat parties as companies (Entity) instead of persons (Officer)")
	flag.Parse()

	if len(parties) < 2 {
		fmt.Fprintln(os.Stderr, "need at least two --party arguments")
		os.Exit(2)
	}
	gs, err := graphload.Open(*dataDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer gs.Close()

	kind := linkage.Person
	if *company {
		kind = linkage.Company
	}
	// MVP: screen the first party against each subsequent party.
	for i := 1; i < len(parties); i++ {
		out, err := screen(gs, "",
			linkage.Party{Name: parties[0], Kind: kind},
			linkage.Party{Name: parties[i], Kind: kind}, *maxHops)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(string(out))
	}
}

// multiFlag collects a repeated string flag.
type multiFlag []string

func (m *multiFlag) String() string { return fmt.Sprint([]string(*m)) }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}
```

- [ ] **Step 4: Add the exported `Resolve` wrapper used by the CLI**

The CLI calls `linkage.Resolve` (exported); Task 4 created the unexported `resolveCandidates`.
Add the exported wrapper to `internal/linkage/resolver.go`:
```go
// Resolve is the exported entry point: resolve a party to ranked graph candidates.
func Resolve(gs *storage.GraphStorage, p Party, tenantID string) ([]Candidate, error) {
	return resolveCandidates(gs, p, tenantID)
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd ../coi-screen && go test ./cmd/coi/ -run TestScreen -v`
Expected: PASS — output contains "shareholder of".

- [ ] **Step 6: Build the binary and commit**

```bash
cd ../coi-screen && go build ./... && go vet ./...
git add cmd/coi/main.go cmd/coi/main_test.go internal/linkage/resolver.go
git commit -q -m "feat(cmd): wire the coi screen pipeline (resolve->connect->score->report)"
```

---

## Task 10: Full-suite green + README usage

**Files:**
- Modify: `../coi-screen/README.md`

- [ ] **Step 1: Run the whole suite**

Run: `cd ../coi-screen && go test ./... -count=1`
Expected: ok for all four internal packages + cmd.

- [ ] **Step 2: Document the end-to-end usage in README**

Append to `README.md`:
```markdown
## Usage

1. Load the ICIJ corpus into a graphdb data dir (one-time), using graphdb's importer:
   ```bash
   # in the graphdb checkout
   go run ./cmd/import-icij --nodes all-nodes.csv --edges all-edges.csv --data ./data/icij
   ```
2. Screen two parties:
   ```bash
   go run ./cmd/coi --data ../graphdb/data/icij \
     --party "Robert Smith" --party "Acme Holdings Ltd"
   ```
   Output is JSON: ranked candidate conflicts, each with its path (per-edge type, role,
   and provenance), a confidence, and a `flagged` boolean. These are candidates for human
   review, not verdicts.

## Scoring policy

`internal/score/score.go::ScorePath` is the conflict policy — tune `interestWeight`, the
combination formula, and the flag threshold to your context, and extend the golden test.
```

- [ ] **Step 3: Commit**

```bash
cd ../coi-screen && git add README.md
git commit -q -m "docs: end-to-end usage + scoring-policy pointer"
```

---

## Self-review (completed during authoring)

**Spec coverage:**
- Stage 1 load + provenance → Tasks 8 (open) + 5 (edge provenance derived from node `source`, per the spike-found gap). ✓
- Stage 2 resolve (type-aware, attribute-gated, 3-band, reversible/REVIEW) → Tasks 1-4. ✓ (Reversibility is inherent: resolution returns candidates with bands; nothing is mutated/merged in the graph — the CLI screens against candidates. A persisted-merge feature is explicitly out of MVP scope.)
- Stage 3 connect (interest-restricted bounded paths) → Task 5. ✓
- Stage 4 score (user-owned, reads `link`) → Task 6. ✓
- Stage 5 report (candidates, provenance, confidence, never verdict) → Task 7. ✓
- CLI ad-hoc pairwise workflow → Task 9. ✓
- Sibling consumer repo embedding graphdb → Task 0. ✓

**Placeholder scan:** Two intentional, clearly-marked instruction lines exist
(`import_block_marker_do_not_copy` in Task 4, `var _ = algorithms...` note in Task 5),
each with an explicit implementer note. The `ScorePath` body is a *runnable default*, not
a placeholder — the test passes as written; the user tunes it. No silent TODOs.

**Type consistency:** `Party`/`Kind`/`Band`/`Candidate` (linkage), `Path`/`PathEdge`
(graphload), `ScorePath` signature, `CandidateConflict` (report) are defined once and used
verbatim across tasks. `Resolve` (exported) wraps `resolveCandidates` (Task 9 step 4).

**Known MVP limitations (logged, not silently capped):**
- `resolveCandidates` is a linear scan over a label set with soundex blocking — fine for a
  dev slice, needs a real index for the full ~800K corpus (noted in Task 4 + spec).
- Path enumeration is a simple-path DFS bounded by `maxHops` + `maxResults` + `maxDegree`
  (hub guard). When a cap fires, `screen` warns on stderr (no silent caps). Tune the caps
  on the real corpus during Milestone-1-proper.
- **Person hard-gate depends on a supplied country.** The gate fires only when both the
  query party and the candidate carry a country (persons) / jurisdiction (companies). If
  the operator does not supply one for a person, the gate cannot fire and precision
  degrades toward surname+given matching — ambiguous pairs still route to REVIEW (the
  damage is bounded), but real-corpus person precision is **unverified until Milestone-1-proper**.
  Consider adding an index-matched `--country` CLI flag when wiring real data.
- **Multi-hop evidence-path readability:** `pathEdge` records each edge in its natural
  storage direction even when traversed backward, so a length-≥3 path won't render as a
  strictly From→To chain. Fine for the common length-2 case; revisit if longer paths
  become important to the report's defensibility.
- **Officer-can-be-a-company:** ICIJ Officers are sometimes corporate shareholders, so the
  binary Person/Company kind flag is a simplification. Acceptable for MVP.
- Resolution confidence is approximated by the match score; a calibrated confidence is
  future work.
- **Honest status to carry into execution:** the suite passes on synthetic fixtures;
  real-ICIJ precision and hub-scaling are unverified until Milestone-1-proper. Do not let
  "tests green" round up to "done."
```
