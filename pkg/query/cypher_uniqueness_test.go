package query

import (
	"errors"
	"sync"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// Cypher CREATE and MERGE called CreateNodeWithTenant, so a /query write
// stored a duplicate of a registered key with no error (ADR 0001). These
// tests pin that every Cypher node creation goes through the registry.

func execCypher(e *Executor, text string) (*ResultSet, error) {
	tokens, err := NewLexer(text).Tokenize()
	if err != nil {
		return nil, err
	}
	q, err := NewParser(tokens).Parse()
	if err != nil {
		return nil, err
	}
	return e.Execute(q)
}

func widgetStore(t *testing.T) (*storage.GraphStorage, *Executor) {
	t.Helper()
	gs, cleanup := setupExecutorTestGraph(t)
	t.Cleanup(cleanup)
	if err := gs.RegisterUniquenessRule(storage.UniquenessRule{
		Name: "widget_sku", Label: "Widget", PropertyKey: "sku",
	}); err != nil {
		t.Fatalf("RegisterUniquenessRule: %v", err)
	}
	return gs, NewExecutor(gs)
}

func seedWidget(t *testing.T, gs *storage.GraphStorage, sku string) {
	t.Helper()
	if _, err := gs.CreateNodeWithUniquenessRulesForTenant("default", []string{"Widget"},
		map[string]storage.Value{"sku": storage.StringValue(sku)}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func countWidgetsWithSKU(t *testing.T, gs *storage.GraphStorage, sku string) int {
	t.Helper()
	nodes, err := gs.FindNodesByLabelAcrossTenants("Widget")
	if err != nil {
		t.Fatalf("FindNodesByLabelAcrossTenants: %v", err)
	}
	n := 0
	for _, node := range nodes {
		if v, ok := node.Properties["sku"]; ok {
			if s, err := v.AsString(); err == nil && s == sku {
				n++
			}
		}
	}
	return n
}

func TestCypherCreate_UniquenessRuleRefusesDuplicate(t *testing.T) {
	gs, e := widgetStore(t)
	seedWidget(t, gs, "w-1")

	_, err := execCypher(e, "CREATE (:Widget {sku: 'w-1'})")
	if !errors.Is(err, storage.ErrUniqueConstraintViolation) {
		t.Fatalf("CREATE duplicate err = %v, want ErrUniqueConstraintViolation", err)
	}
	if got := countWidgetsWithSKU(t, gs, "w-1"); got != 1 {
		t.Errorf("Widget nodes with sku w-1 = %d, want 1", got)
	}
}

func TestCypherMerge_MatchesExistingNode(t *testing.T) {
	gs, e := widgetStore(t)
	seedWidget(t, gs, "w-1")
	if _, err := execCypher(e, "MERGE (w:Widget {sku: 'w-1'}) RETURN w"); err != nil {
		t.Fatalf("MERGE existing: %v", err)
	}
	if got := countWidgetsWithSKU(t, gs, "w-1"); got != 1 {
		t.Errorf("Widget nodes with sku w-1 = %d, want 1", got)
	}
}

func TestCypherMerge_CreatesMissingNode(t *testing.T) {
	gs, e := widgetStore(t)
	if _, err := execCypher(e, "MERGE (w:Widget {sku: 'w-new'}) RETURN w"); err != nil {
		t.Fatalf("MERGE missing: %v", err)
	}
	if got := countWidgetsWithSKU(t, gs, "w-new"); got != 1 {
		t.Errorf("Widget nodes with sku w-new = %d, want 1", got)
	}
}

// Sixteen concurrent MERGEs of one key: the match half sees nothing for
// several of them, so the create half must go through the registry and the
// losers must re-match instead of failing.
func TestCypherMerge_ConcurrentSameKey(t *testing.T) {
	gs, e := widgetStore(t)
	const workers = 16
	errs := make([]error, workers)
	var start, wg sync.WaitGroup
	start.Add(1)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			start.Wait()
			_, errs[i] = execCypher(e, "MERGE (w:Widget {sku: 'race'}) RETURN w")
		}(i)
	}
	start.Done()
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
	if got := countWidgetsWithSKU(t, gs, "race"); got != 1 {
		t.Errorf("Widget nodes with sku race = %d, want 1", got)
	}
}
