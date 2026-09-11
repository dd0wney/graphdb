package graphql

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/storage/storagetest"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// afterCursorFixture seeds `n` Person nodes, each with an integer property
// `n` in insertion order, and a KNOWS edge from every node to its successor.
// It returns the store and a limits schema with a small default limit so a
// cursor walk needs several pages.
func afterCursorFixture(t *testing.T, n int) (*storage.GraphStorage, graphql.Schema) {
	t.Helper()
	gs, err := storage.NewGraphStorageWithConfig(storage.StorageConfig{
		DataDir:        t.TempDir(),
		BulkImportMode: true,
	})
	if err != nil {
		t.Fatalf("NewGraphStorageWithConfig: %v", err)
	}
	t.Cleanup(func() { _ = gs.Close() })

	var prev *storage.Node
	for i := 0; i < n; i++ {
		node, err := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
			"n": storage.IntValue(int64(i)),
		})
		if err != nil {
			t.Fatalf("CreateNode %d: %v", i, err)
		}
		if prev != nil {
			if _, err := gs.CreateEdge(prev.ID, node.ID, "KNOWS", nil, 1.0); err != nil {
				t.Fatalf("CreateEdge %d: %v", i, err)
			}
		}
		prev = node
	}

	schema, err := GenerateSchemaWithLimits(gs, &LimitConfig{DefaultLimit: 40, MaxLimit: 1000})
	if err != nil {
		t.Fatalf("GenerateSchemaWithLimits: %v", err)
	}
	return gs, schema
}

// runIDs executes `query` and returns the `id` of every item under the
// top-level field `field`. It fails the test on a GraphQL error.
func runIDs(t *testing.T, schema graphql.Schema, field, query string, vars map[string]any) []string {
	t.Helper()
	result := graphql.Do(graphql.Params{Schema: schema, RequestString: query, VariableValues: vars})
	if result.HasErrors() {
		t.Fatalf("query %s failed: %v", query, result.Errors)
	}
	items, ok := result.Data.(map[string]any)[field].([]any)
	if !ok {
		t.Fatalf("field %q missing from %v", field, result.Data)
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.(map[string]any)["id"].(string))
	}
	return ids
}

// whereVars wraps a filter as the `$where` variable, because WhereInput is a
// scalar with no literal parser: the schema accepts it through a variable only.
func whereVars(filter map[string]any) map[string]any {
	return map[string]any{"where": filter}
}

// pageQuery builds a one-field query. When `vars` carries a `where` filter the
// query declares the variable and passes it to the field.
func pageQuery(field, args string, vars map[string]any) string {
	if vars == nil {
		return fmt.Sprintf(`{ %s(%s) { id } }`, field, args)
	}
	return fmt.Sprintf(`query($where: WhereInput) { %s(%s, where: $where) { id } }`, field, args)
}

// walkByOffset pages through `field` with limit/offset until a short page.
func walkByOffset(t *testing.T, schema graphql.Schema, field string, vars map[string]any, limit int) []string {
	t.Helper()
	var all []string
	for offset := 0; ; offset += limit {
		q := pageQuery(field, fmt.Sprintf(`limit: %d, offset: %d`, limit, offset), vars)
		page := runIDs(t, schema, field, q, vars)
		all = append(all, page...)
		if len(page) < limit {
			return all
		}
	}
}

// walkByAfter pages through `field` with limit/after until a short page.
// The cursor is the id of the last item on the previous page, the same
// contract as the REST X-Next-Cursor header.
func walkByAfter(t *testing.T, schema graphql.Schema, field string, vars map[string]any, limit int) []string {
	t.Helper()
	var all []string
	after := ""
	for pages := 0; ; pages++ {
		if pages > 100 {
			t.Fatalf("cursor walk of %s did not end after %d pages", field, pages)
		}
		afterArg := ""
		if after != "" {
			afterArg = fmt.Sprintf(`, after: "%s"`, after)
		}
		q := pageQuery(field, fmt.Sprintf(`limit: %d%s`, limit, afterArg), vars)
		page := runIDs(t, schema, field, q, vars)
		all = append(all, page...)
		if len(page) < limit {
			return all
		}
		after = page[len(page)-1]
	}
}

func sortedNodeIDs(t *testing.T, gs *storage.GraphStorage) []string {
	t.Helper()
	nodes, err := gs.GetNodesByLabelForTenant("", "Person")
	if err != nil {
		t.Fatalf("GetNodesByLabelForTenant: %v", err)
	}
	ids := make([]uint64, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = fmt.Sprintf("%d", id)
	}
	return out
}

func assertSameIDs(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d ids, want %d", what, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s: position %d: got %s, want %s", what, i, got[i], want[i])
		}
	}
}

// A full `after` walk over the typed node field must enumerate the same IDs,
// in the same order, as the offset walk and as the storage enumeration.
func TestAfterCursorNodesWalkEqualsOffsetWalk(t *testing.T) {
	gs, schema := afterCursorFixture(t, 250)
	const limit = 100

	byOffset := walkByOffset(t, schema, "persons", nil, limit)
	byAfter := walkByAfter(t, schema, "persons", nil, limit)
	fromStore := sortedNodeIDs(t, gs)

	if len(fromStore) != 250 {
		t.Fatalf("fixture: store has %d Person nodes, want 250", len(fromStore))
	}
	assertSameIDs(t, "offset walk vs store", byOffset, fromStore)
	assertSameIDs(t, "after walk vs offset walk", byAfter, byOffset)
}

// The same equivalence for the `edges` field.
func TestAfterCursorEdgesWalkEqualsOffsetWalk(t *testing.T) {
	_, schema := afterCursorFixture(t, 250)
	const limit = 100

	byOffset := walkByOffset(t, schema, "edges", nil, limit)
	byAfter := walkByAfter(t, schema, "edges", nil, limit)

	if len(byOffset) != 249 {
		t.Fatalf("fixture: offset walk found %d edges, want 249", len(byOffset))
	}
	assertSameIDs(t, "after walk vs offset walk", byAfter, byOffset)
}

// With a `where` filter the cursor path walks pages and filters each one,
// so every page is full until the last, and the union equals the offset walk.
func TestAfterCursorWithWhereEqualsOffsetWalk(t *testing.T) {
	_, schema := afterCursorFixture(t, 250)
	const limit = 30
	where := whereVars(map[string]any{"n": map[string]any{"gte": 100}})

	byOffset := walkByOffset(t, schema, "persons", where, limit)
	byAfter := walkByAfter(t, schema, "persons", where, limit)

	if len(byOffset) != 150 {
		t.Fatalf("fixture: offset walk with where found %d nodes, want 150", len(byOffset))
	}
	assertSameIDs(t, "after walk with where vs offset walk", byAfter, byOffset)
}

// `after` refuses the combinations that have no ID-order meaning, and a
// cursor that is not an ID. A silent fallback to the offset path would
// hand the caller a page from the wrong position.
func TestAfterCursorRefusals(t *testing.T) {
	_, schema := afterCursorFixture(t, 10)

	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"nodes: after with orderBy", `{ persons(after: "3", orderBy: {field: "n", direction: "DESC"}) { id } }`, "orderBy"},
		{"nodes: after with offset", `{ persons(after: "3", offset: 2) { id } }`, "offset"},
		{"nodes: bad cursor", `{ persons(after: "not-an-id") { id } }`, "invalid after cursor"},
		{"edges: after with orderBy", `{ edges(after: "3", orderBy: {field: "weight", direction: "ASC"}) { id } }`, "orderBy"},
		{"edges: after with offset", `{ edges(after: "3", offset: 2) { id } }`, "offset"},
		{"edges: bad cursor", `{ edges(after: "not-an-id") { id } }`, "invalid after cursor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := graphql.Do(graphql.Params{Schema: schema, RequestString: tc.query})
			if !result.HasErrors() {
				t.Fatalf("query succeeded, want an error naming %q", tc.want)
			}
			msg := result.Errors[0].Message
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("error %q does not name %q", msg, tc.want)
			}
		})
	}
}

// graphql-go coerces an integer literal to the ID scalar, so `after: 3` and
// `after: "3"` must page identically. This guards the type assertion in
// parseListPaging against a change in the library's coercion.
func TestAfterCursorAcceptsIntegerLiteral(t *testing.T) {
	_, schema := afterCursorFixture(t, 10)
	all := runIDs(t, schema, "persons", `{ persons { id } }`, nil)
	third := all[2]

	quoted := runIDs(t, schema, "persons", fmt.Sprintf(`{ persons(after: "%s") { id } }`, third), nil)
	bare := runIDs(t, schema, "persons", fmt.Sprintf(`{ persons(after: %s) { id } }`, third), nil)

	assertSameIDs(t, "bare integer cursor vs quoted cursor", bare, quoted)
	assertSameIDs(t, "quoted cursor vs tail of the full list", quoted, all[3:])

	// The same value through a variable, as a client library sends it.
	var thirdID int
	if _, err := fmt.Sscanf(third, "%d", &thirdID); err != nil {
		t.Fatalf("fixture id %q is not a number: %v", third, err)
	}
	viaVar := runIDs(t, schema, "persons", `query($after: ID) { persons(after: $after) { id } }`,
		map[string]any{"after": thirdID})
	assertSameIDs(t, "integer variable cursor vs quoted cursor", viaVar, quoted)
}

// damageFixture seeds five Thing nodes for tenant "owner" in an mmap store,
// damages the record of the fourth (index 3), reopens, and returns the live
// IDs in order plus a limits schema. The positive and negative controls follow
// pkg/api/enumeration_partial_page_test.go.
func damageFixture(t *testing.T) ([]uint64, graphql.Schema) {
	t.Helper()
	dir := t.TempDir()
	cfg := storage.DefaultStorageConfig(dir)
	cfg.UseMmapSnapshot = true // the JSON path has no record to damage

	gs, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	ids := make([]uint64, 0, 5)
	for i := 0; i < 5; i++ {
		n, cerr := gs.CreateNodeWithTenant("owner", []string{"Thing"},
			map[string]storage.Value{"name": storage.StringValue(fmt.Sprintf("n%d", i))})
		if cerr != nil {
			t.Fatalf("create node %d: %v", i, cerr)
		}
		ids = append(ids, n.ID)
	}
	if err := gs.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	storagetest.DamageNodeRecord(t, dir, ids[3])

	reopened, err := storage.NewGraphStorageWithConfig(cfg)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })

	// POSITIVE CONTROL on the injector: the damaged node must not read.
	if _, err := reopened.GetNodeForTenant(ids[3], "owner"); err == nil {
		t.Fatalf("the node fault did not take: node %d still reads", ids[3])
	}
	// NEGATIVE CONTROL: an undamaged node must still read.
	if _, err := reopened.GetNodeForTenant(ids[0], "owner"); err != nil {
		t.Fatalf("the first undamaged node must still read: %v", err)
	}

	schema, err := GenerateSchemaWithLimits(reopened, &LimitConfig{DefaultLimit: 100, MaxLimit: 1000})
	if err != nil {
		t.Fatalf("GenerateSchemaWithLimits: %v", err)
	}
	return ids, schema
}

// ADR 0003 for the index-level path: a page fails only when its scan meets a
// damaged record. Before this change the resolver scanned the whole set, so
// one damaged record failed every page of the tenant, intact pages included.
// The page that meets the damage still refuses: GraphQL cannot carry a partial
// page beside an error, and a silent short page would hide the loss.
func TestAfterCursorDamageWindowIsThePage(t *testing.T) {
	ids, schema := damageFixture(t)
	ctx := tenant.WithTenant(context.Background(), "owner")
	run := func(query string) *graphql.Result {
		return graphql.Do(graphql.Params{Schema: schema, RequestString: query, Context: ctx})
	}
	idsOf := func(r *graphql.Result) []string {
		items := r.Data.(map[string]any)["things"].([]any)
		out := make([]string, len(items))
		for i, item := range items {
			out[i] = item.(map[string]any)["id"].(string)
		}
		return out
	}
	str := func(id uint64) string { return fmt.Sprintf("%d", id) }

	// Page 1 holds nodes 0 and 1 and probes node 2. Its scan never meets
	// node 3, so the page is complete and must not refuse.
	first := run(`{ things(limit: 2) { id } }`)
	if first.HasErrors() {
		t.Fatalf("page 1 refused although its scan met no damaged record: %v", first.Errors)
	}
	assertSameIDs(t, "page 1", idsOf(first), []string{str(ids[0]), str(ids[1])})

	// Page 2 starts after node 1 and its scan meets node 3. It refuses.
	second := run(fmt.Sprintf(`{ things(limit: 2, after: "%s") { id } }`, str(ids[1])))
	if !second.HasErrors() {
		t.Fatalf("page 2 served %v although its scan met a damaged record", idsOf(second))
	}

	// Page 3 starts after the damaged record and is complete again.
	third := run(fmt.Sprintf(`{ things(limit: 2, after: "%s") { id } }`, str(ids[3])))
	if third.HasErrors() {
		t.Fatalf("page 3 refused although its scan met no damaged record: %v", third.Errors)
	}
	assertSameIDs(t, "page 3", idsOf(third), []string{str(ids[4])})

	// A where filter materialises the whole label, so its window is the
	// whole set: the same page-3 request with a filter meets the damage.
	filtered := graphql.Do(graphql.Params{
		Schema:         schema,
		RequestString:  fmt.Sprintf(`query($where: WhereInput) { things(limit: 2, after: "%s", where: $where) { id } }`, str(ids[3])),
		VariableValues: whereVars(map[string]any{"name": map[string]any{"eq": "n4"}}),
		Context:        ctx,
	})
	if !filtered.HasErrors() {
		t.Fatalf("filtered page served %v although the whole-set scan met a damaged record", idsOf(filtered))
	}
}
