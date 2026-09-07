package query

// An opt-in is only as opt-in as its zero value, on EVERY surface.
//
// PathOptions travels from ExecuteWithOptions through four internal executors:
// the plain path, PROFILE, both segments of a WITH chain, and each segment of a
// UNION. OPTIONAL MATCH builds its own MatchStep and joins at matchPattern, so
// it inherits whichever ExecutionContext was built above it. MERGE does NOT
// inherit it that way — it builds a sub-context of its own.
//
// The table below drives the first three tests and holds the four surfaces whose
// row counts match fanGraph directly. UNION and MERGE need different assertions,
// so each has its own test lower down. An earlier version of this comment
// claimed UNION was in the table when it was not, which is why the code review
// caught an untested surface.
//
// Two failures are possible on each of those surfaces and this file gates both:
//
//	the option is dropped   an opt-in caller silently gets AllSimplePaths back
//	the option is invented  a caller who passed nothing gets DistinctNodes
//
// The second is the dangerous one. "Every distinct simple path" is the behaviour
// today, and a surface that quietly changed it would change the meaning of
// existing queries with no error and no version bump.
//
// ExecuteWithText is the deliberate exception, pinned by the last test here: it
// reads and fills the plan cache and calls executePlan directly, so it never
// sees a PathOptions value. That limitation is documented on ExecuteWithOptions,
// and this file makes it a fact rather than a comment.

import (
	"errors"
	"testing"
)

// pathOptionSurfaces are the query spellings that must all carry the options.
// Each reaches the sink of fanGraph, so AllSimplePaths owes fanRoutes rows and
// DistinctNodes owes exactly one.
var pathOptionSurfaces = []struct {
	name  string
	query string
}{
	{"plain", fanSinkQuery},
	{"PROFILE", "PROFILE " + fanSinkQuery},
	{"WITH chain", "MATCH (a:Root)-[:LINK*1..2]->(b:Sink) WITH b RETURN b.name"},
	{"OPTIONAL MATCH", "MATCH (a:Root) OPTIONAL MATCH (a)-[:LINK*1..2]->(b:Sink) RETURN b.name"},
}

// The opt-in must reach every surface. A surface that drops it answers the
// caller's question with a different traversal and says nothing.
func TestDistinctNodesReachesEveryExecutionSurface(t *testing.T) {
	for _, s := range pathOptionSurfaces {
		t.Run(s.name, func(t *testing.T) {
			_, e, cleanup := fanGraph(t, fanRoutes)
			defer cleanup()

			rs, err := runQueryWithOptions(t, e, s.query, PathOptions{Semantics: DistinctNodes})
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			if len(rs.Rows) != 1 {
				t.Errorf("%s returned %d rows under DistinctNodes, want 1. The options did "+
					"not reach this surface, so an opt-in caller silently got the default.",
					s.name, len(rs.Rows))
			}
		})
	}
}

// The negative control, on every surface. The zero value must mean today's
// behaviour everywhere, or the mode is a silent change of meaning rather than
// an opt-in.
func TestZeroPathOptionsIsUnchangedOnEveryExecutionSurface(t *testing.T) {
	for _, s := range pathOptionSurfaces {
		t.Run(s.name, func(t *testing.T) {
			_, e, cleanup := fanGraph(t, fanRoutes)
			defer cleanup()

			rs, err := runQueryWithOptions(t, e, s.query, PathOptions{})
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			if len(rs.Rows) != fanRoutes {
				t.Errorf("%s returned %d rows under the zero PathOptions, want %d. The "+
					"default enumerates every distinct simple path, and this surface no "+
					"longer does.", s.name, len(rs.Rows), fanRoutes)
			}
		})
	}
}

// The expansion filter travels the same four surfaces. A filter that applies on
// the plain path and not under PROFILE would make PROFILE report timings for a
// traversal the caller never asked for.
func TestExpandFilterReachesEveryExecutionSurface(t *testing.T) {
	for _, s := range pathOptionSurfaces {
		t.Run(s.name, func(t *testing.T) {
			_, e, cleanup := fanGraph(t, fanRoutes)
			defer cleanup()

			called := 0
			opts := PathOptions{Expand: func(Expansion) bool {
				called++
				return false // reject everything: nothing past the first hop survives
			}}
			rs, err := runQueryWithOptions(t, e, s.query, opts)
			if err != nil {
				t.Fatalf("query failed: %v", err)
			}
			if called == 0 {
				t.Fatalf("the filter was never called on %s, so the options did not reach "+
					"this surface", s.name)
			}
			// Count rows that actually bound a sink. OPTIONAL MATCH keeps its
			// left row with a null when the optional pattern matches nothing,
			// which is correct Cypher, so a row count alone cannot tell "the
			// filter worked" from "the filter did nothing" on that surface.
			bound := 0
			for _, row := range rs.Rows {
				if row[rs.Columns[0]] != nil {
					bound++
				}
			}
			if bound != 0 {
				t.Errorf("%s bound %d sinks although the filter rejected every candidate",
					s.name, bound)
			}
		})
	}
}

// ExecuteWithText is the fourth execution path and it does NOT carry the
// options: it reads or fills the plan cache and calls executePlan directly.
//
// This test pins that as a fact. A caller who wants the options parses first and
// calls ExecuteWithOptions, giving up the plan cache. If someone later wires the
// options into the cached path, this test must be updated deliberately rather
// than discovered by a consumer whose rows changed.
func TestExecuteWithTextAlwaysRunsTheDefaultSemantics(t *testing.T) {
	_, e, cleanup := fanGraph(t, fanRoutes)
	defer cleanup()

	tokens, err := NewLexer(fanSinkQuery).Tokenize()
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	parsed, err := NewParser(tokens).Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	rs, err := e.ExecuteWithText(fanSinkQuery, parsed)
	if err != nil {
		t.Fatalf("ExecuteWithText: %v", err)
	}
	if len(rs.Rows) != fanRoutes {
		t.Errorf("ExecuteWithText returned %d rows, want %d. It has no PathOptions "+
			"parameter, so it must run the default semantics.", len(rs.Rows), fanRoutes)
	}
}

// MERGE is a fifth surface, and it was missed.
//
// MergeStep.Execute builds its sub-context as a struct literal rather than
// through newExecutionContext, so it copied context, graph, tenantID and
// bindings and dropped pathOpts. A caller that uses Expand as a visibility rule
// got rows for nodes that rule excludes, and ON MATCH SET then wrote to them.
//
// Changing the constructor signature does not prevent this. A struct literal
// bypasses the constructor, so the guarantee has to live on a helper that every
// sub-context uses.
func TestMergeCarriesPathOptions(t *testing.T) {
	f := gateGraph(t)
	defer f.cleanup()

	offered := 0
	opts := PathOptions{Expand: func(x Expansion) bool {
		offered++
		return x.To.ID != f.ids["gate"]
	}}

	rs, err := runQueryWithOptions(t, f.e,
		"MERGE (a:Root)-[:LINK*1..4]->(b:Hidden) RETURN b.name", opts)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if offered == 0 {
		t.Fatalf("the filter was never called, so MERGE ran a traversal with no options at "+
			"all. rows=%v", rs.Rows)
	}

	// Everything labelled :Hidden sits behind the rejected gate, so the match
	// half must find none of it.
	for _, row := range rs.Rows {
		v := row[rs.Columns[0]]
		for _, hidden := range []string{"s1", "s2", "s3"} {
			if v == hidden {
				t.Errorf("MERGE returned %q from behind the rejected gate; a caller using "+
					"the filter as a visibility rule sees a node that rule excludes, and "+
					"ON MATCH SET would write to it", hidden)
			}
		}
	}
}

// A MERGE whose match half stops at an engine limit must say so.
//
// MergeStep.Execute discards matchCtx.truncation. This is not caused by
// PathOptions — it predates them — but it is the same defect the repository
// keeps closing: a limit that acts and does not tell the caller.
func TestMergeReportsTruncationFromItsMatchHalf(t *testing.T) {
	_, e, cleanup := chainGraph(t, MaxAllowedTraversalDepth+2)
	defer cleanup()

	rs, err := runQueryWithOptions(t, e,
		"MERGE (a:Root)-[:LINK*]->(b:Node) RETURN b.name", PathOptions{})
	if rs == nil || len(rs.Rows) == 0 {
		t.Fatalf("no rows came back, so this test cannot tell a lost signal from a broken "+
			"query: rs=%v err=%v", rs, err)
	}
	if !errors.Is(err, ErrTraversalTruncated) {
		t.Errorf("the MERGE match half stopped at the depth cap of %d on a chain of %d and "+
			"reported success: %v", MaxAllowedTraversalDepth, MaxAllowedTraversalDepth+2, err)
	}
}

// Each segment of a UNION must carry the options.
//
// UNION ALL, not UNION: plain UNION removes duplicate rows, so twenty identical
// sink rows collapse to one on their own. A test over plain UNION cannot tell
// "DistinctNodes worked" from "UNION removed the duplicates", which is the trap
// this test exists to avoid.
func TestUnionCarriesPathOptionsInEverySegment(t *testing.T) {
	const unionAll = fanSinkQuery + " UNION ALL " + fanSinkQuery

	_, e, cleanup := fanGraph(t, fanRoutes)
	defer cleanup()

	distinct, err := runQueryWithOptions(t, e, unionAll, PathOptions{Semantics: DistinctNodes})
	if err != nil {
		t.Fatalf("DistinctNodes: %v", err)
	}
	if len(distinct.Rows) != 2 {
		t.Errorf("UNION ALL of two DistinctNodes segments returned %d rows, want 2. A "+
			"segment that dropped the options contributed %d rows of its own.",
			len(distinct.Rows), fanRoutes)
	}

	zero, err := runQueryWithOptions(t, e, unionAll, PathOptions{})
	if err != nil {
		t.Fatalf("zero value: %v", err)
	}
	if len(zero.Rows) != 2*fanRoutes {
		t.Errorf("UNION ALL under the zero PathOptions returned %d rows, want %d",
			len(zero.Rows), 2*fanRoutes)
	}
}
