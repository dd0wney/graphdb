package query

import (
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// Go integer and float kinds other than int64 and float64 reach arithmetic
// through a procedure column (count is an int) and through a library caller's
// parameter. They compute as int64 and float64.
func TestArithmeticOnEachGoNumberKind(t *testing.T) {
	ex := arithmeticStore(t)
	for _, tc := range []struct{ text, want string }{
		{`CALL algo.detectCycles() YIELD count RETURN count + 1 AS x`, "1"},
		{`CALL algo.detectCycles() YIELD count RETURN -count AS x`, "0"},
		{`CALL algo.detectCycles() YIELD count WITH count AS c RETURN c * 2 AS x`, "0"},
	} {
		rs, err := runCypher(t, ex, tc.text)
		if err != nil {
			t.Errorf("%q: %v", tc.text, err)
			continue
		}
		if got := cellsOf(rs, "x"); len(got) != 1 || got[0] != tc.want {
			t.Errorf("%q: x = %v, want [%s]", tc.text, got, tc.want)
		}
	}

	for _, kind := range []struct {
		name string
		n    any
		div  string
	}{
		{"int", int(5), "2"},
		{"int32", int32(5), "2"},
		{"uint8", uint8(5), "2"},
		{"float32", float32(5), "2.5"},
	} {
		for _, tc := range []struct{ text, want string }{
			{`RETURN $n + 1 AS x`, "6"},
			{`RETURN $n / 2 AS x`, kind.div},
			{`RETURN -$n AS x`, "-5"},
		} {
			tokens, err := NewLexer(tc.text).Tokenize()
			if err != nil {
				t.Fatal(err)
			}
			q, err := NewParser(tokens).Parse()
			if err != nil {
				t.Fatal(err)
			}
			rs, err := ex.ExecuteWithParams(q, map[string]any{"n": kind.n})
			if err != nil {
				t.Errorf("%s %q: %v", kind.name, tc.text, err)
				continue
			}
			if got := cellsOf(rs, "x"); len(got) != 1 || got[0] != tc.want {
				t.Errorf("%s %q: x = %v, want [%s]", kind.name, tc.text, got, tc.want)
			}
		}
	}
}

// The executor has no transaction, so a refusal in a later clause comes after
// the write steps changed the store. This test records that. It must change
// when the executor gets a rollback: then the store state below must be the
// state before the query.
func TestArithmeticRefusalAfterWriteKeepsTheWrite(t *testing.T) {
	ex := arithmeticStore(t)

	wantRefusal(t, ex, `CREATE (n:New {v: 1}) RETURN n.v / 0 AS x`, "division by zero")
	wantCells(t, ex, `MATCH (n:New) RETURN n.v AS v`, "v", "1")

	wantRefusal(t, ex, `MATCH (n:Person) SET n.y = 1 RETURN n.name + 1 AS x`, "cannot apply +")
	wantCells(t, ex, `MATCH (n:Person) RETURN n.y AS y`, "y", "1", "1")
}

// storeWithX has one node with age 5 and one with age 0, both with x = 42.
func storeWithX(t *testing.T) (*storage.GraphStorage, *Executor, uint64, uint64) {
	t.Helper()
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gs.Close() })
	mk := func(age int64) uint64 {
		n, err := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"age": storage.IntValue(age), "x": storage.IntValue(42),
		})
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	first, second := mk(5), mk(0)
	return gs, NewExecutor(gs), first, second
}

func storedX(t *testing.T, gs *storage.GraphStorage, id uint64) (storage.Value, bool) {
	t.Helper()
	n, err := gs.GetNode(id)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := n.Properties["x"]
	return v, ok
}

func wantStoredInt(t *testing.T, gs *storage.GraphStorage, id uint64, label string, want int64) {
	t.Helper()
	v, ok := storedX(t, gs, id)
	if !ok {
		t.Errorf("%s: x removed, want %d", label, want)
		return
	}
	if got, err := v.AsInt(); err != nil || got != want {
		t.Errorf("%s: x = %v (%v), want %d", label, v, err, want)
	}
}

func TestSetWithNestedArithmeticRefuses(t *testing.T) {
	t.Run("nested arithmetic", func(t *testing.T) {
		gs, ex, first, second := storeWithX(t)
		wantRefusal(t, ex, `MATCH (n:Person) SET n.x = 100 / n.age + 1`, "division by zero")
		wantStoredInt(t, gs, second, "age-0 node", 42)
		// The row before the refusal is written: there is no rollback
		// (see TestArithmeticRefusalAfterWriteKeepsTheWrite).
		wantStoredInt(t, gs, first, "age-5 node", 21)
	})
	t.Run("inside a function", func(t *testing.T) {
		gs, ex, first, second := storeWithX(t)
		wantRefusal(t, ex, `MATCH (n:Person) SET n.x = toString(1 / n.age)`, "division by zero")
		wantStoredInt(t, gs, second, "age-0 node", 42)
		// Age 5 gives the string "0" before the refusal. Same no-rollback
		// behaviour as above.
		v, ok := storedX(t, gs, first)
		if !ok {
			t.Fatal("age-5 node: x removed")
		}
		if s, err := v.AsString(); err != nil || s != "0" {
			t.Errorf("age-5 node: x = %v (%v), want the string 0 written", v, err)
		}
	})
}

func TestListPlusMessage(t *testing.T) {
	ex := arithmeticStore(t)
	wantRefusal(t, ex, `RETURN [1] + [2] AS x`, "list concatenation with + is not supported")
	wantRefusal(t, ex, `RETURN 1 + [2] AS x`, "list concatenation with + is not supported")
	wantRefusal(t, ex, `RETURN 'a' + 1 AS x`, "cannot apply + to string and integer")
}
