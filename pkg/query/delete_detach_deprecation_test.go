package query

import (
	"strings"
	"testing"
)

// A plain DELETE of a node that still has relationships removes them too. In
// openCypher that is an error unless the query says DETACH DELETE. graphdb
// keeps the 1.x behaviour, which docs/STABILITY_POLICY.md protects, but every
// such delete must say that it is deprecated and will be refused in v2.0.

func (f *deleteFixture) runResult(t *testing.T, query string) *ResultSet {
	t.Helper()
	rs, err := f.executor.Execute(parseCallInput(t, query))
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return rs
}

func assertDetachNotice(t *testing.T, rs *ResultSet, want bool) {
	t.Helper()
	found := false
	for _, n := range rs.Notices {
		if n.Code == NoticePlainDeleteDetach && strings.Contains(n.Message, "DETACH DELETE") {
			found = true
		}
	}
	if found != want {
		t.Fatalf("detach deprecation notice present = %v, want %v; notices = %q", found, want, rs.Notices)
	}
}

func TestDeletePlain_ConnectedNode_StillDeletesAndNotesDeprecation(t *testing.T) {
	f := newDeleteFixture(t)

	rs := f.runResult(t, "MATCH (a:P) DELETE a")

	assertDetachNotice(t, rs, true)
	if f.nodeExists(t, f.a.ID) || f.edgeExists(t, f.ab.ID) || f.edgeExists(t, f.ac.ID) {
		t.Fatal("the 1.x behaviour changed: the node and its relationships must still be deleted")
	}
}

func TestDeletePlain_NoNoticeWhenNothingIsDetached(t *testing.T) {
	cases := []string{
		"MATCH (a:P) DETACH DELETE a",
		"MATCH (b:Q {name: 'b'}) OPTIONAL MATCH (b)-[r]-() DELETE r, b",
		"MATCH (a:P)-[r:R]->(x:Q) DELETE r, a",
	}
	for _, query := range cases {
		t.Run(query, func(t *testing.T) {
			f := newDeleteFixture(t)
			assertDetachNotice(t, f.runResult(t, query), false)
		})
	}

	t.Run("node with no relationships", func(t *testing.T) {
		f := newDeleteFixture(t)
		lone := mustOracleNode(t, f.gs, "Lone", nil)
		assertDetachNotice(t, f.runResult(t, "MATCH (n:Lone) DELETE n"), false)
		if f.nodeExists(t, lone.ID) {
			t.Fatal("the lone node was not deleted")
		}
	})
}

// The notice must survive the paths that build the result elsewhere.
func TestDeletePlain_NoticeSurvivesWithAndProfile(t *testing.T) {
	for _, query := range []string{
		"MATCH (a:P) WITH a DELETE a",
		"PROFILE MATCH (a:P) DELETE a",
	} {
		t.Run(query, func(t *testing.T) {
			f := newDeleteFixture(t)
			assertDetachNotice(t, f.runResult(t, query), true)
		})
	}
}

// a -R-> b and a -R-> c; deleting a and b removes two relationships, and the
// one between a and b counts once.
func TestDeletePlain_NoticeCountsEachRelationshipOnce(t *testing.T) {
	f := newDeleteFixture(t)

	rs := f.runResult(t, "MATCH (a:P)-[:R]->(b:Q {name: 'b'}) DELETE a, b")

	var msg string
	for _, n := range rs.Notices {
		if n.Code == NoticePlainDeleteDetach {
			msg = n.Message
		}
	}
	if !strings.Contains(msg, "removed 2 relationships of 2 nodes") {
		t.Fatalf("notice = %q, want it to report 2 relationships of 2 nodes", msg)
	}
}

// UNION builds its own combined result; the notice of either side must reach it.
func TestDeletePlain_NoticeSurvivesUnion(t *testing.T) {
	f := newDeleteFixture(t)
	mustOracleNode(t, f.gs, "Lone", nil)

	assertDetachNotice(t, f.runResult(t, "MATCH (n:Lone) DELETE n UNION MATCH (a:P) DELETE a"), true)
}
