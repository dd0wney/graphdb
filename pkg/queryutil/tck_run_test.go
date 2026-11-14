package queryutil

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/dd0wney/graphdb/pkg/query"
	"github.com/dd0wney/graphdb/pkg/storage"
)

// Outcomes of one scenario. Each scenario gets exactly one.
const (
	outPass        = "pass"        // every step ran and every check held
	outFail        = "fail"        // the query ran and a check did not hold
	outNoParse     = "no-parse"    // the lexer or parser refused a query
	outUnsupported = "unsupported" // the harness cannot judge the scenario
	// outErrorUnchecked: the scenario expected an error and an error came at
	// some phase. The harness checks neither the class nor the phase, so this
	// is not a pass. A pass means the harness fully compared expected rows.
	outErrorUnchecked = "error-unchecked"
)

type tckResult struct {
	outcome string
	detail  string // why, for a person who reads a failure
}

const (
	expNone = iota
	expRows
	expEmpty
	expError
	expUnread // a result step that the harness runs but cannot check
)

// tckSpec is what the steps of one scenario ask for.
type tckSpec struct {
	setup    []string
	params   map[string]any
	query    string
	hasQuery bool

	expect  int
	ordered bool
	header  []string
	rows    [][]string

	noSideEffects bool
	sideTable     map[string]int // nil when the scenario has no table

	hard     string // a step the harness does not know: do not run the scenario
	deferred string // a step the harness cannot check: run, then report unsupported
}

var tckErrorStep = regexp.MustCompile(`^an? [A-Za-z]+ should be raised at (compile time|runtime|any time)`)

// tckInterpret gives the steps of a scenario their meaning.
func tckInterpret(sc *tckScenario) *tckSpec {
	sp := &tckSpec{params: map[string]any{}}
	for _, st := range sc.steps {
		switch {
		case st.text == "an empty graph" || st.text == "any graph":
		case st.text == "having executed:":
			if !st.hasDoc {
				sp.hard = "having executed without a docstring"
			}
			sp.setup = append(sp.setup, st.doc)
		case st.text == "executing query:":
			if !st.hasDoc || sp.hasQuery {
				sp.hard = "query step without a docstring, or a second query"
			}
			sp.query, sp.hasQuery = st.doc, true
		case st.text == "parameters are:":
			for _, row := range st.table {
				v, ok := tckPlainValue(tckParseValue(rowCell(row, 1)))
				if len(row) != 2 || !ok {
					sp.deferred = "a parameter value that the harness cannot read"
					continue
				}
				sp.params[row[0]] = v
			}
		case st.text == "the result should be, in any order:" || st.text == "the result should be, in order:":
			if len(st.table) < 1 {
				sp.hard = "result step without a table"
				continue
			}
			sp.expect, sp.ordered = expRows, strings.HasSuffix(st.text, "in order:")
			sp.header, sp.rows = st.table[0], st.table[1:]
		case st.text == "the result should be empty":
			sp.expect = expEmpty
		case strings.HasPrefix(st.text, "the result should be"):
			sp.expect = expUnread
			sp.deferred = "result step not read by the harness: " + st.text
		case tckErrorStep.MatchString(st.text):
			// The class and the phase are not checked. An error at any phase
			// satisfies the step, but the outcome is error-unchecked and never
			// pass, because the harness did not compare what the error was.
			// This is on purpose: the product names its errors differently
			// from the TCK.
			sp.expect = expError
		case st.text == "no side effects":
			sp.noSideEffects = true
		case st.text == "the side effects should be:":
			sp.sideTable = map[string]int{}
			for _, row := range st.table {
				n, err := strconv.Atoi(rowCell(row, 1))
				if len(row) != 2 || err != nil {
					sp.deferred = "side effects table that the harness cannot read"
					continue
				}
				sp.sideTable[row[0]] = n
			}
		default:
			sp.hard = "step not known to the harness: " + st.text
		}
	}
	switch {
	case sp.hard != "":
	case !sp.hasQuery:
		sp.hard = "no query step"
	case sp.expect == expNone:
		sp.hard = "no result or error step"
	}
	return sp
}

func rowCell(row []string, i int) string {
	if i < len(row) {
		return row[i]
	}
	return ""
}

// tckPlainValue turns a parsed value into a Go value for a parameter.
func tckPlainValue(v tckVal) (any, bool) {
	switch v.kind {
	case kNull:
		return nil, true
	case kInt:
		return v.i, true
	case kFloat:
		return v.f, true
	case kString:
		return v.s, true
	case kBool:
		return v.b, true
	case kList:
		out := make([]any, len(v.list))
		for i, e := range v.list {
			g, ok := tckPlainValue(e)
			if !ok {
				return nil, false
			}
			out[i] = g
		}
		return out, true
	case kMap:
		out := map[string]any{}
		for k, e := range v.m {
			g, ok := tckPlainValue(e)
			if !ok {
				return nil, false
			}
			out[k] = g
		}
		return out, true
	}
	return nil, false
}

const tckQueryTimeout = 5 * time.Second

// tckRunQuery follows the production path of runGapQuery: sanitize, lex,
// parse, execute. The refused result is true when one of the first three
// phases refused the query.
func tckRunQuery(exec *query.Executor, q string, params map[string]any) (rs *query.ResultSet, refused bool, err error) {
	sanitized, err := query.SanitizeQuery(q)
	if err != nil {
		return nil, true, fmt.Errorf("sanitize: %w", err)
	}
	tokens, err := query.NewLexer(sanitized).Tokenize()
	if err != nil {
		return nil, true, fmt.Errorf("lex: %w", err)
	}
	parsed, err := query.NewParser(tokens).Parse()
	if err != nil {
		return nil, true, fmt.Errorf("parse: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), tckQueryTimeout)
	defer cancel()
	if len(params) > 0 {
		rs, err = exec.ExecuteWithParamsContext(ctx, parsed, params)
	} else {
		rs, err = exec.ExecuteWithContext(ctx, parsed)
	}
	return rs, false, err
}

// tckRunScenario runs one scenario on a new empty store under workDir.
func tckRunScenario(sc *tckScenario, workDir string) (res tckResult) {
	// A panic in the product is a failure of the scenario. It must not stop
	// the run, and it must not read as a refusal.
	defer func() {
		if r := recover(); r != nil {
			res = tckResult{outFail, fmt.Sprintf("panic: %v", r)}
		}
	}()
	sp := tckInterpret(sc)
	if sp.hard != "" {
		return tckResult{outUnsupported, sp.hard}
	}
	dir, err := os.MkdirTemp(workDir, "s")
	if err != nil {
		return tckResult{outFail, "harness: " + err.Error()}
	}
	defer os.RemoveAll(dir)
	g, err := storage.NewGraphStorage(dir)
	if err != nil {
		return tckResult{outFail, "harness: NewGraphStorage: " + err.Error()}
	}
	defer g.Close()
	exec, _ := WireCapabilities(query.NewExecutor(g), g)

	for _, s := range sp.setup {
		_, refused, err := tckRunQuery(exec, s, nil)
		if refused {
			return tckResult{outNoParse, "setup: " + err.Error()}
		}
		if err != nil {
			return tckResult{outFail, "setup query failed: " + err.Error()}
		}
	}

	before, snapErr := tckSnapshot(g)
	rs, refused, qerr := tckRunQuery(exec, sp.query, sp.params)
	switch {
	case sp.expect == expError:
		// no early return: the side effect checks still run
	case refused:
		return tckResult{outNoParse, qerr.Error()}
	case qerr != nil:
		return tckResult{outFail, "error where a result was expected: " + qerr.Error()}
	}
	var checks []tckResult
	if sp.expect == expError {
		if qerr == nil {
			return tckResult{outFail, "no error where an error was expected"}
		}
		checks = append(checks, tckResult{outErrorUnchecked, "error came; class and phase not checked"})
	} else if sp.expect != expUnread {
		checks = append(checks, tckCheckResult(sp, rs))
	}
	if sp.noSideEffects || sp.sideTable != nil {
		checks = append(checks, tckCheckSideEffects(g, sp, before, snapErr))
	}
	if sp.deferred != "" {
		checks = append(checks, tckResult{outUnsupported, sp.deferred})
	}
	return tckCombine(checks)
}

// tckCombine gives fail over unsupported over error-unchecked over pass.
func tckCombine(checks []tckResult) tckResult {
	best := tckResult{outcome: outPass}
	for _, c := range checks {
		switch {
		case c.outcome == outFail:
			return c
		case c.outcome == outUnsupported:
			best = c
		case c.outcome == outErrorUnchecked && best.outcome == outPass:
			best = c
		}
	}
	return best
}

// tckCheckResult compares the result set with the expected table.
func tckCheckResult(sp *tckSpec, rs *query.ResultSet) tckResult {
	if rs == nil {
		return tckResult{outFail, "nil result set and nil error"}
	}
	if sp.expect == expEmpty {
		if len(rs.Rows) != 0 {
			return tckResult{outFail, fmt.Sprintf("expected no rows, got %d", len(rs.Rows))}
		}
		return tckResult{outPass, ""}
	}
	if !slices.Equal(sp.header, rs.Columns) {
		return tckResult{outFail, fmt.Sprintf("columns %q, want %q", rs.Columns, sp.header)}
	}
	if len(rs.Rows) != len(sp.rows) {
		return tckResult{outFail, fmt.Sprintf("%d rows, want %d", len(rs.Rows), len(sp.rows))}
	}
	want := make([][]tckVal, len(sp.rows))
	for i, r := range sp.rows {
		if len(r) != len(sp.header) {
			return tckResult{outUnsupported, "expected row with a wrong cell count"}
		}
		want[i] = make([]tckVal, len(r))
		for j, c := range r {
			want[i][j] = tckParseValue(c)
		}
	}
	got := make([][]tckVal, len(rs.Rows))
	for i, row := range rs.Rows {
		got[i] = make([]tckVal, len(sp.header))
		for j, col := range sp.header {
			v, ok := row[col]
			if !ok {
				return tckResult{outFail, fmt.Sprintf("row %d has no column %q", i, col)}
			}
			got[i][j] = tckGoValue(v)
		}
	}
	rowCmp := func(w, g []tckVal) tckTri {
		res := triEq
		for k := range w {
			res = combineTri(res, tckCompare(w[k], g[k]))
		}
		return res
	}
	if sp.ordered {
		res := triEq
		for i := range want {
			res = combineTri(res, rowCmp(want[i], got[i]))
		}
		return tckVerdict(res, fmt.Sprintf("rows differ in order or value:\n  got  %s\n  want %s", tckShow(got), tckShow(want)))
	}
	return tckVerdict(tckMatchMultiset(want, got, rowCmp), fmt.Sprintf("rows differ as a multiset:\n  got  %s\n  want %s", tckShow(got), tckShow(want)))
}

func tckVerdict(t tckTri, detail string) tckResult {
	switch t {
	case triEq:
		return tckResult{outPass, ""}
	case triNe:
		return tckResult{outFail, detail}
	}
	return tckResult{outUnsupported, "expected or actual value that the harness cannot compare (node, relationship or path)"}
}

// tckMatchMultiset decides whether two row lists hold the same rows in any
// order. It answers equal only if a perfect matching exists and every matched
// pair is equal. It answers not equal if no perfect matching exists even when
// every unknown pair counts as equal. Otherwise it answers unknown.
func tckMatchMultiset(want, got [][]tckVal, rowCmp func(w, g []tckVal) tckTri) tckTri {
	n := len(want)
	cmp := make([][]tckTri, n)
	anyUnknown := false
	for i := range want {
		cmp[i] = make([]tckTri, n)
		for j := range got {
			cmp[i][j] = rowCmp(want[i], got[j])
			anyUnknown = anyUnknown || cmp[i][j] == triUnknown
		}
	}
	match := func(possible func(i, j int) bool) bool {
		owner := make([]int, n)
		for j := range owner {
			owner[j] = -1
		}
		var try func(i int, seen []bool) bool
		try = func(i int, seen []bool) bool {
			for j := 0; j < n; j++ {
				if !possible(i, j) || seen[j] {
					continue
				}
				seen[j] = true
				if owner[j] < 0 || try(owner[j], seen) {
					owner[j] = i
					return true
				}
			}
			return false
		}
		for i := 0; i < n; i++ {
			if !try(i, make([]bool, n)) {
				return false
			}
		}
		return true
	}
	if !match(func(i, j int) bool { return cmp[i][j] != triNe }) {
		return triNe
	}
	if anyUnknown {
		return triUnknown
	}
	return triEq
}

func tckShow(rows [][]tckVal) string {
	parts := make([]string, len(rows))
	for i, r := range rows {
		cells := make([]string, len(r))
		for j, v := range r {
			cells[j] = tckString(v)
		}
		parts[i] = "(" + strings.Join(cells, ", ") + ")"
	}
	return strings.Join(parts, " ")
}

func tckString(v tckVal) string {
	switch v.kind {
	case kNull:
		return "null"
	case kInt:
		return strconv.FormatInt(v.i, 10)
	case kFloat:
		return strconv.FormatFloat(v.f, 'g', -1, 64) + "f"
	case kString:
		return strconv.Quote(v.s)
	case kBool:
		return strconv.FormatBool(v.b)
	case kList:
		parts := make([]string, len(v.list))
		for i, e := range v.list {
			parts[i] = tckString(e)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case kMap:
		keys := make([]string, 0, len(v.m))
		for k := range v.m {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = k + ": " + tckString(v.m[k])
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case kNode:
		return "<node>"
	case kEdge:
		return "<relationship>"
	}
	return "<opaque " + v.s + ">"
}
