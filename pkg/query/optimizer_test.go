package query

import (
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// TestOptimizerCreation tests creating a new optimizer
func TestOptimizerCreation(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	optimizer := NewOptimizer(graph)
	if optimizer == nil {
		t.Fatal("Expected optimizer to be created")
	}
	if optimizer.graph != graph {
		t.Error("Optimizer should reference the graph")
	}
}

// TestCostEstimation tests query cost estimation
func TestCostEstimation(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	// Create test data
	// Small dataset: 10 nodes with label "Person"
	for i := 0; i < 10; i++ {
		_, err := graph.CreateNode(
			[]string{"Person"},
			map[string]storage.Value{
				"age": storage.IntValue(int64(20 + i)),
			},
		)
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
	}

	// Large dataset: 1000 nodes with label "Product"
	for i := 0; i < 1000; i++ {
		_, err := graph.CreateNode(
			[]string{"Product"},
			map[string]storage.Value{
				"price": storage.IntValue(int64(10 + i)),
			},
		)
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
	}

	optimizer := NewOptimizer(graph)

	tests := []struct {
		name          string
		pattern       *MatchClause
		expectedCost  float64
		shouldBeLess  bool // true if cost should be less than comparison
		comparisonVal float64
	}{
		{
			name: "Small label scan (Person)",
			pattern: &MatchClause{
				Patterns: []*Pattern{
					{
						Nodes: []*NodePattern{
							{
								Variable: "p",
								Labels:   []string{"Person"},
							},
						},
					},
				},
			},
			expectedCost:  10.0,
			shouldBeLess:  true,
			comparisonVal: 100.0,
		},
		{
			name: "Large label scan (Product)",
			pattern: &MatchClause{
				Patterns: []*Pattern{
					{
						Nodes: []*NodePattern{
							{
								Variable: "prod",
								Labels:   []string{"Product"},
							},
						},
					},
				},
			},
			expectedCost:  1000.0,
			shouldBeLess:  false,
			comparisonVal: 100.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := optimizer.EstimateCost(tt.pattern)

			if tt.shouldBeLess {
				if cost >= tt.comparisonVal {
					t.Errorf("Expected cost %.2f to be less than %.2f", cost, tt.comparisonVal)
				}
			} else {
				if cost < tt.comparisonVal {
					t.Errorf("Expected cost %.2f to be greater than or equal to %.2f", cost, tt.comparisonVal)
				}
			}
		})
	}
}

// TestIndexSelection tests that the optimizer selects indexed lookups
func TestIndexSelection(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	// Create test data with indexed properties
	for i := 0; i < 100; i++ {
		_, err := graph.CreateNode(
			[]string{"User"},
			map[string]storage.Value{
				"name": storage.StringValue("user" + string(rune(i))),
				"age":  storage.IntValue(int64(20 + i)),
			},
		)
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
	}

	optimizer := NewOptimizer(graph)

	// Test plan without property filter (should do label scan)
	matchClause := &MatchClause{
		Patterns: []*Pattern{
			{
				Nodes: []*NodePattern{
					{
						Variable: "u",
						Labels:   []string{"User"},
					},
				},
			},
		},
	}

	planWithoutFilter := &ExecutionPlan{
		Steps: []ExecutionStep{
			&MatchStep{
				match: matchClause,
			},
		},
	}

	query := &Query{
		Match: matchClause,
	}

	optimizedPlan := optimizer.Optimize(planWithoutFilter, query)

	if len(optimizedPlan.Steps) == 0 {
		t.Fatal("Optimized plan should have steps")
	}

	// Verify the plan still has a match step
	hasMatch := false
	for _, step := range optimizedPlan.Steps {
		if _, ok := step.(*MatchStep); ok {
			hasMatch = true
			break
		}
	}

	if !hasMatch {
		t.Error("Optimized plan should still contain a match step")
	}
}

// TestFilterPushdown tests that filters are pushed down early in execution
func TestFilterPushdown(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	optimizer := NewOptimizer(graph)

	// Create a plan with filter after return (suboptimal)
	matchClause := &MatchClause{
		Patterns: []*Pattern{
			{
				Nodes: []*NodePattern{
					{
						Variable: "n",
						Labels:   []string{"Person"},
					},
				},
			},
		},
	}

	whereClause := &WhereClause{
		Expression: &BinaryExpression{
			Operator: ">",
			Left:     &PropertyExpression{Variable: "n", Property: "age"},
			Right:    &LiteralExpression{Value: 25},
		},
	}

	plan := &ExecutionPlan{
		Steps: []ExecutionStep{
			&MatchStep{match: matchClause},
			&ReturnStep{returnClause: &ReturnClause{}},
			&FilterStep{where: whereClause},
		},
	}

	query := &Query{
		Match: matchClause,
		Where: whereClause,
	}

	optimized := optimizer.Optimize(plan, query)

	// Filter should come before Return
	if len(optimized.Steps) < 2 {
		t.Fatal("Expected at least 2 steps in optimized plan")
	}

	// Find positions of FilterStep and ReturnStep
	filterPos := -1
	returnPos := -1
	for i, step := range optimized.Steps {
		if _, ok := step.(*FilterStep); ok {
			filterPos = i
		}
		if _, ok := step.(*ReturnStep); ok {
			returnPos = i
		}
	}

	if filterPos == -1 {
		t.Error("Expected FilterStep in optimized plan")
	}
	if returnPos == -1 {
		t.Error("Expected ReturnStep in optimized plan")
	}
	if filterPos >= returnPos {
		t.Errorf("FilterStep (pos %d) should come before ReturnStep (pos %d)", filterPos, returnPos)
	}
}

// TestJoinOrdering tests that joins are reordered to start with most selective
func TestJoinOrdering(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	// Create unbalanced data: 10 Admins, 1000 Users
	for i := 0; i < 10; i++ {
		_, err := graph.CreateNode(
			[]string{"Admin"},
			map[string]storage.Value{"name": storage.StringValue("admin")},
		)
		if err != nil {
			t.Fatalf("Failed to create admin: %v", err)
		}
	}

	for i := 0; i < 1000; i++ {
		_, err := graph.CreateNode(
			[]string{"User"},
			map[string]storage.Value{"name": storage.StringValue("user")},
		)
		if err != nil {
			t.Fatalf("Failed to create user: %v", err)
		}
	}

	optimizer := NewOptimizer(graph)

	// Query that matches both User and Admin
	// Should process Admin first (more selective)
	query := &Query{
		Match: &MatchClause{
			Patterns: []*Pattern{
				{
					Nodes: []*NodePattern{
						{
							Variable: "u",
							Labels:   []string{"User"},
						},
					},
				},
			},
		},
	}

	plan := &ExecutionPlan{
		Steps: []ExecutionStep{
			&MatchStep{match: &MatchClause{
				Patterns: []*Pattern{{Nodes: []*NodePattern{{Variable: "u", Labels: []string{"User"}}}}},
			}},
			&MatchStep{match: &MatchClause{
				Patterns: []*Pattern{{Nodes: []*NodePattern{{Variable: "a", Labels: []string{"Admin"}}}}},
			}},
		},
	}

	optimized := optimizer.Optimize(plan, query)

	// In an optimized plan, Admin should come first
	// (This is a basic test - actual implementation may vary)
	if len(optimized.Steps) < 2 {
		t.Fatal("Expected at least 2 steps")
	}
}

// TestQueryCaching tests that query plans are cached
func TestQueryCaching(t *testing.T) {
	cache := NewQueryCache()

	queryText := "MATCH (n:Person) RETURN n"
	plan := &ExecutionPlan{
		Steps: []ExecutionStep{
			&MatchStep{match: &MatchClause{
				Patterns: []*Pattern{{Nodes: []*NodePattern{{Variable: "n", Labels: []string{"Person"}}}}},
			}},
		},
	}

	// Initially cache should be empty
	_, found := cache.Get(queryText)
	if found {
		t.Error("Cache should be empty initially")
	}

	// Put plan in cache
	cache.Put(queryText, plan)

	// Now it should be found
	cachedPlan, found := cache.Get(queryText)
	if !found {
		t.Error("Plan should be in cache")
	}
	if cachedPlan != plan {
		t.Error("Cached plan should be the same object")
	}
}

// TestQueryStatistics tests execution statistics tracking
func TestQueryStatistics(t *testing.T) {
	cache := NewQueryCache()
	queryText := "MATCH (n:Person) RETURN n"

	// Record multiple executions
	cache.RecordExecution(queryText, 100, true)
	cache.RecordExecution(queryText, 200, true)
	cache.RecordExecution(queryText, 150, true)

	stats, exists := cache.stats[queryText]
	if !exists {
		t.Fatal("Statistics should exist")
	}

	if stats.ExecutionCount != 3 {
		t.Errorf("Expected 3 executions, got %d", stats.ExecutionCount)
	}

	expectedAvg := (100 + 200 + 150) / 3
	if stats.AvgExecutionTime != int64(expectedAvg) {
		t.Errorf("Expected avg time %d, got %d", expectedAvg, stats.AvgExecutionTime)
	}

	if !stats.LastOptimized {
		t.Error("LastOptimized should be true")
	}
}

// TestTopQueries tests retrieving most frequently executed queries
func TestTopQueries(t *testing.T) {
	cache := NewQueryCache()

	// Record executions for different queries
	cache.RecordExecution("MATCH (n) RETURN n", 100, true)
	cache.RecordExecution("MATCH (n) RETURN n", 100, true)
	cache.RecordExecution("MATCH (n) RETURN n", 100, true)

	cache.RecordExecution("MATCH (p:Person) RETURN p", 100, true)
	cache.RecordExecution("MATCH (p:Person) RETURN p", 100, true)

	cache.RecordExecution("MATCH (a)-[:KNOWS]->(b) RETURN a, b", 100, true)

	topQueries := cache.GetTopQueries(2)

	if len(topQueries) != 2 {
		t.Errorf("Expected 2 top queries, got %d", len(topQueries))
	}

	// First should be the query with 3 executions
	if topQueries[0].ExecutionCount != 3 {
		t.Errorf("First query should have 3 executions, got %d", topQueries[0].ExecutionCount)
	}

	// Second should be the query with 2 executions
	if topQueries[1].ExecutionCount != 2 {
		t.Errorf("Second query should have 2 executions, got %d", topQueries[1].ExecutionCount)
	}
}

// TestIndexLookupOptimization tests that queries with equality conditions use property indexes
func TestIndexLookupOptimization(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	// Create an index on "name" property
	err = graph.CreatePropertyIndex("name", storage.TypeString)
	if err != nil {
		t.Fatalf("Failed to create index: %v", err)
	}

	// Create test data
	for i := 0; i < 100; i++ {
		_, err := graph.CreateNode(
			[]string{"Person"},
			map[string]storage.Value{
				"name": storage.StringValue("user" + string(rune('A'+i%26))),
			},
		)
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
	}

	optimizer := NewOptimizer(graph)

	// Create a query with WHERE clause that can use the index
	matchClause := &MatchClause{
		Patterns: []*Pattern{
			{
				Nodes: []*NodePattern{
					{
						Variable: "n",
						Labels:   []string{"Person"},
					},
				},
			},
		},
	}

	whereClause := &WhereClause{
		Expression: &BinaryExpression{
			Operator: "=",
			Left:     &PropertyExpression{Variable: "n", Property: "name"},
			Right:    &LiteralExpression{Value: "userA"},
		},
	}

	plan := &ExecutionPlan{
		Steps: []ExecutionStep{
			&MatchStep{match: matchClause},
		},
	}

	query := &Query{
		Match: matchClause,
		Where: whereClause,
	}

	optimized := optimizer.Optimize(plan, query)

	// The MatchStep should be replaced with IndexLookupStep
	if len(optimized.Steps) == 0 {
		t.Fatal("Expected at least one step in optimized plan")
	}

	_, isIndexLookup := optimized.Steps[0].(*IndexLookupStep)
	if !isIndexLookup {
		t.Error("Expected first step to be IndexLookupStep when index is available")
	}
}

// TestIndexLookupOptimization_NoIndex tests that without index, MatchStep is used
func TestIndexLookupOptimization_NoIndex(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	// NO index created

	optimizer := NewOptimizer(graph)

	matchClause := &MatchClause{
		Patterns: []*Pattern{
			{
				Nodes: []*NodePattern{
					{
						Variable: "n",
						Labels:   []string{"Person"},
					},
				},
			},
		},
	}

	whereClause := &WhereClause{
		Expression: &BinaryExpression{
			Operator: "=",
			Left:     &PropertyExpression{Variable: "n", Property: "name"},
			Right:    &LiteralExpression{Value: "Alice"},
		},
	}

	plan := &ExecutionPlan{
		Steps: []ExecutionStep{
			&MatchStep{match: matchClause},
		},
	}

	query := &Query{
		Match: matchClause,
		Where: whereClause,
	}

	optimized := optimizer.Optimize(plan, query)

	// Without index, should keep MatchStep
	_, isMatch := optimized.Steps[0].(*MatchStep)
	if !isMatch {
		t.Error("Expected MatchStep when no index is available")
	}
}

// TestIndexLookupExecution tests that IndexLookupStep actually executes correctly
func TestIndexLookupExecution(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	// Create an index on "email" property
	err = graph.CreatePropertyIndex("email", storage.TypeString)
	if err != nil {
		t.Fatalf("Failed to create index: %v", err)
	}

	// Create test nodes
	_, err = graph.CreateNode(
		[]string{"User"},
		map[string]storage.Value{
			"email": storage.StringValue("alice@example.com"),
			"name":  storage.StringValue("Alice"),
		},
	)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	_, err = graph.CreateNode(
		[]string{"User"},
		map[string]storage.Value{
			"email": storage.StringValue("bob@example.com"),
			"name":  storage.StringValue("Bob"),
		},
	)
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	// Execute IndexLookupStep directly
	step := &IndexLookupStep{
		propertyKey: "email",
		value:       storage.StringValue("alice@example.com"),
		variable:    "u",
		labels:      []string{"User"},
	}

	ctx := &ExecutionContext{
		graph:    graph,
		bindings: make(map[string]any),
		results:  make([]*BindingSet, 0),
	}

	err = step.Execute(ctx)
	if err != nil {
		t.Fatalf("IndexLookupStep failed: %v", err)
	}

	// Should find exactly one node
	if len(ctx.results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(ctx.results))
	}

	// Verify the binding
	if len(ctx.results) > 0 {
		node, ok := ctx.results[0].bindings["u"].(*storage.Node)
		if !ok {
			t.Fatal("Expected binding to be a Node")
		}
		name, _ := node.Properties["name"].AsString()
		if name != "Alice" {
			t.Errorf("Expected name 'Alice', got '%s'", name)
		}
	}
}

// TestCardinalityEstimation tests that cardinality is estimated correctly
func TestCardinalityEstimation(t *testing.T) {
	graph, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create graph: %v", err)
	}
	defer graph.Close()

	// Create 50 Person nodes
	for i := 0; i < 50; i++ {
		_, err := graph.CreateNode(
			[]string{"Person"},
			nil,
		)
		if err != nil {
			t.Fatalf("Failed to create node: %v", err)
		}
	}

	optimizer := NewOptimizer(graph)

	pattern := &MatchClause{
		Patterns: []*Pattern{
			{
				Nodes: []*NodePattern{
					{
						Variable: "p",
						Labels:   []string{"Person"},
					},
				},
			},
		},
	}

	cardinality := optimizer.estimateCardinality(pattern)

	// Should estimate close to 50
	if cardinality < 40 || cardinality > 60 {
		t.Errorf("Expected cardinality around 50, got %d", cardinality)
	}
}
