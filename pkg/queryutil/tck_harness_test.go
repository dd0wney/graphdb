package queryutil

// The openCypher TCK harness.
//
// How it relates to TestCypherKnownGaps (cypher_known_gaps_test.go): that
// table stays. It holds the gaps that have a coord task, each with the query
// and the correct behaviour written out. This file is the measured count. It
// runs a subset of the openCypher TCK (testdata/opencypher-tck, see the README
// there) on the production path and compares every scenario outcome with
// testdata/tck_baseline.tsv. A parser or executor fix moves a number that
// TestTCKBaseline checks, and the same pull request records the new number.
//
// The harness is a measuring instrument, so its comparison never reports
// "pass" for a check that it did not fully make. Where it cannot judge a
// scenario, for example when a result holds a node literal, the outcome is
// "unsupported". TestTCKHarnessSelfTest proves that each outcome can occur.
//
// Environment:
//
//	GRAPHDB_TCK_UPDATE=1   write the baseline file again and pass
//	GRAPHDB_TCK_VERBOSE=1  log the reason for each scenario that is not a pass
//
// Files of the harness: tck_gherkin_test.go reads the feature files,
// tck_values_test.go holds the value model and the strict comparison,
// tck_run_test.go runs a scenario, tck_effects_test.go measures side effects.

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

const (
	tckFeatureDir  = "testdata/opencypher-tck"
	tckSelfTestDir = "testdata/tck-selftest"
	tckBaselineTSV = "testdata/tck_baseline.tsv"
)

type tckRow struct {
	feature, name, outcome, detail string
}

func (r tckRow) key() string { return r.feature + "\t" + r.name }

var tckOutcomes = []string{outPass, outFail, outNoParse, outUnsupported}

func tckRunAll(t *testing.T, dir string) []tckRow {
	t.Helper()
	scs, err := loadTCKDir(dir)
	if err != nil {
		t.Fatalf("read feature files: %v", err)
	}
	work := t.TempDir()
	rows := make([]tckRow, 0, len(scs))
	for _, sc := range scs {
		res := tckRunScenario(sc, work)
		rows = append(rows, tckRow{sc.feature, sc.name, res.outcome, res.detail})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].key() < rows[j].key() })
	return rows
}

func TestTCKBaseline(t *testing.T) {
	rows := tckRunAll(t, tckFeatureDir)
	logTCKCounts(t, rows)
	if os.Getenv("GRAPHDB_TCK_VERBOSE") != "" {
		for _, r := range rows {
			if r.outcome != outPass {
				t.Logf("%s | %s : %s : %s", r.feature, r.name, r.outcome, r.detail)
			}
		}
	}

	if os.Getenv("GRAPHDB_TCK_UPDATE") == "1" {
		if err := writeTCKBaseline(rows); err != nil {
			t.Fatalf("write baseline: %v", err)
		}
		t.Logf("wrote %s with %d scenarios", tckBaselineTSV, len(rows))
		return
	}

	base, err := readTCKBaseline()
	if err != nil {
		t.Fatalf("%v (GRAPHDB_TCK_UPDATE=1 writes the baseline)", err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.key()] = true
		old, ok := base[r.key()]
		switch {
		case !ok:
			t.Errorf("%s | %s : no baseline, now %s", r.feature, r.name, r.outcome)
		case old != r.outcome:
			t.Errorf("%s | %s : baseline %s, now %s", r.feature, r.name, old, r.outcome)
		}
	}
	for k, old := range base {
		if !seen[k] {
			f, n, _ := strings.Cut(k, "\t")
			t.Errorf("%s | %s : baseline %s, scenario no longer exists", f, n, old)
		}
	}
}

func logTCKCounts(t *testing.T, rows []tckRow) {
	t.Helper()
	per := map[string]map[string]int{}
	total := map[string]int{}
	for _, r := range rows {
		if per[r.feature] == nil {
			per[r.feature] = map[string]int{}
		}
		per[r.feature][r.outcome]++
		total[r.outcome]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\n%-18s %6s %6s %9s %12s %6s\n", "feature", "pass", "fail", "no-parse", "unsupported", "total")
	line := func(name string, c map[string]int) {
		sum := 0
		for _, o := range tckOutcomes {
			sum += c[o]
		}
		fmt.Fprintf(&b, "%-18s %6d %6d %9d %12d %6d\n", name, c[outPass], c[outFail], c[outNoParse], c[outUnsupported], sum)
	}
	for _, f := range slices.Sorted(maps.Keys(per)) {
		line(f, per[f])
	}
	line("TOTAL", total)
	t.Log(b.String())
}

func writeTCKBaseline(rows []tckRow) error {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", r.feature, r.name, r.outcome)
	}
	return os.WriteFile(filepath.FromSlash(tckBaselineTSV), []byte(b.String()), 0o644)
}

// readTCKBaseline reads the golden file. A duplicate key, a line with the
// wrong shape or an unknown outcome is an error.
func readTCKBaseline() (map[string]string, error) {
	data, err := os.ReadFile(filepath.FromSlash(tckBaselineTSV))
	if err != nil {
		return nil, fmt.Errorf("read baseline: %w", err)
	}
	out := map[string]string{}
	for n, ln := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		cols := strings.Split(ln, "\t")
		if len(cols) != 3 || !slices.Contains(tckOutcomes, cols[2]) {
			return nil, fmt.Errorf("%s:%d: bad line %q", tckBaselineTSV, n+1, ln)
		}
		k := cols[0] + "\t" + cols[1]
		if _, dup := out[k]; dup {
			return nil, fmt.Errorf("%s:%d: duplicate scenario key %q", tckBaselineTSV, n+1, k)
		}
		out[k] = cols[2]
	}
	return out, nil
}

// TestTCKHarnessSelfTest proves that the harness reports each outcome and that
// its comparison is strict. The scenarios are in testdata/tck-selftest. Each
// scenario name is a key of the table below, and the table has no other key.
func TestTCKHarnessSelfTest(t *testing.T) {
	want := map[string]string{
		"passes":                                  outPass,
		"wrong expected value":                    outFail,
		"integer against float":                   outFail,
		"float against integer":                   outFail,
		"string against integer":                  outFail,
		"list order matters":                      outFail,
		"rows in the wrong sequence, in order":    outFail,
		"rows in the wrong sequence, any order":   outPass,
		"duplicate rows are a multiset":           outFail,
		"wrong column name":                       outFail,
		"query that does not parse":               outNoParse,
		"setup that does not parse":               outNoParse,
		"error expected and the query is refused": outPass,
		"error expected and the query runs":       outFail,
		"no side effects after a create":          outFail,
		"side effects table that holds":           outPass,
		"side effects table that is wrong":        outFail,
		"unknown step":                            outUnsupported,
		"node literal in the result":              outUnsupported,
		"result step that is not read":            outUnsupported,
		"expected empty and rows come back":       outFail,
		"expected empty and none come back":       outPass,
		"parameter is bound":                      outPass,
		"outline {ex 1}":                          outPass,
		"outline {ex 2}":                          outFail,
	}
	rows := tckRunAll(t, tckSelfTestDir)
	got := map[string]tckRow{}
	for _, r := range rows {
		if _, dup := got[r.name]; dup {
			t.Errorf("fixture name %q is used twice", r.name)
		}
		got[r.name] = r
		t.Logf("%-42s %-12s %s", r.name, r.outcome, strings.ReplaceAll(r.detail, "\n", " "))
	}
	for name, outcome := range want {
		r, ok := got[name]
		switch {
		case !ok:
			t.Errorf("fixture scenario %q is missing", name)
		case r.outcome != outcome:
			t.Errorf("%q: outcome %s, want %s (%s)", name, r.outcome, outcome, r.detail)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("fixture scenario %q has no expected outcome in the table", name)
		}
	}
	// Each of the four outcomes must occur, or the table proves too little.
	for _, o := range tckOutcomes {
		if !slices.ContainsFunc(rows, func(r tckRow) bool { return r.outcome == o }) {
			t.Errorf("no fixture gives the outcome %s", o)
		}
	}
}
