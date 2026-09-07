package query

// An opt-in is only as opt-in as its zero value, on EVERY surface.
//
// PathOptions travels from ExecuteWithOptions through four internal executors:
// the plain path, PROFILE, both segments of a WITH chain, and each segment of a
// UNION. OPTIONAL MATCH builds its own MatchStep and joins at matchPattern, so
// it inherits whichever ExecutionContext was built above it.
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
