package query

import (
	"context"
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/intelligence"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
	"github.com/stretchr/testify/assert"
	"time"
)

func TestCypherSpike_E2E(t *testing.T) {
	dataDir, _ := os.MkdirTemp("", "cypher-spike-*")
	defer os.RemoveAll(dataDir)

	graph, _ := storage.NewGraphStorage(dataDir)
	defer graph.Close()

	// Seed data
	tenantID := "default"
	ctx := tenant.WithTenant(context.Background(), tenantID)
	
	n1, _ := graph.CreateNodeWithTenant(tenantID, []string{"Person"}, map[string]storage.Value{"name": storage.StringValue("Alice")})
	n2, _ := graph.CreateNodeWithTenant(tenantID, []string{"Person"}, map[string]storage.Value{"name": storage.StringValue("Bob")})
	graph.CreateEdgeWithTenant(tenantID, n1.ID, n2.ID, "KNOWS", nil, 1.0)

	planner := NewPlanner(graph)
	ec := newExecutionContext(ctx, graph)

	t.Run("Simple Match", func(t *testing.T) {
		q := &Query{
			Match: &MatchClause{Patterns: []*Pattern{{Nodes: []*NodePattern{{Variable: "n", Labels: []string{"Person"}}}}}},
			Return: &ReturnClause{Items: []*ReturnItem{{Expression: &PropertyExpression{Variable: "n"}}}},
		}
		op, err := planner.Plan(context.Background(), q)
		assert.NoError(t, err)
		op.Open(ec)
		defer op.Close(ec)
		count := 0
		for {
			row, _ := op.Next(ec)
			if row == nil { break }
			count++
		}
		assert.GreaterOrEqual(t, count, 2)
	})

	t.Run("Match and Expand", func(t *testing.T) {
		nodeA := &NodePattern{Variable: "a", Labels: []string{"Person"}}
		nodeB := &NodePattern{Variable: "b", Labels: []string{"Person"}}
		q := &Query{
			Match: &MatchClause{
				Patterns: []*Pattern{
					{
						Nodes: []*NodePattern{nodeA, nodeB},
						Relationships: []*RelationshipPattern{{Variable: "r", Type: "KNOWS", Direction: DirectionOutgoing, From: nodeA, To: nodeB}},
					},
				},
			},
			Return: &ReturnClause{Items: []*ReturnItem{{Expression: &PropertyExpression{Variable: "a"}}, {Expression: &PropertyExpression{Variable: "b"}}}},
		}
		op, err := planner.Plan(context.Background(), q)
		assert.NoError(t, err)
		op.Open(ec)
		defer op.Close(ec)
		found := false
		for {
			row, _ := op.Next(ec)
			if row == nil { break }
			a := row.bindings["a"].(*storage.Node)
			b := row.bindings["b"].(*storage.Node)
			nameA, _ := a.Properties["name"].AsString()
			nameB, _ := b.Properties["name"].AsString()
			if nameA == "Alice" && nameB == "Bob" { found = true }
		}
		assert.True(t, found)
	})

	t.Run("CREATE Node", func(t *testing.T) {
		q := &Query{
			Create: &CreateClause{Patterns: []*Pattern{{Nodes: []*NodePattern{{Variable: "n", Labels: []string{"Person"}, Properties: map[string]any{"name": "Charlie"}}}}}},
			Return: &ReturnClause{Items: []*ReturnItem{{Expression: &PropertyExpression{Variable: "n"}}}},
		}
		op, err := planner.Plan(context.Background(), q)
		assert.NoError(t, err)
		op.Open(ec)
		defer op.Close(ec)
		row, _ := op.Next(ec)
		assert.NotNil(t, row)
		n := row.bindings["n"].(*storage.Node)
		name, _ := n.Properties["name"].AsString()
		assert.Equal(t, "Charlie", name)
	})

	t.Run("SET Property", func(t *testing.T) {
		q := &Query{
			Match: &MatchClause{Patterns: []*Pattern{{Nodes: []*NodePattern{{Variable: "n", Labels: []string{"Person"}}}}}},
			Where: &WhereClause{Expression: &BinaryExpression{Left: &PropertyExpression{Variable: "n", Property: "name"}, Operator: "=", Right: &LiteralExpression{Value: "Alice"}}},
			Set: &SetClause{Assignments: []*Assignment{{Variable: "n", Property: "age", Value: int64(30)}}},
			Return: &ReturnClause{Items: []*ReturnItem{{Expression: &PropertyExpression{Variable: "n"}}}},
		}
		op, err := planner.Plan(context.Background(), q)
		assert.NoError(t, err)
		op.Open(ec)
		defer op.Close(ec)
		row, _ := op.Next(ec)
		assert.NotNil(t, row)
		n := row.bindings["n"].(*storage.Node)
		age, _ := n.Properties["age"].AsInt()
		assert.Equal(t, int64(30), age)
	})

	t.Run("DELETE Node", func(t *testing.T) {
		q := &Query{
			Match: &MatchClause{Patterns: []*Pattern{{Nodes: []*NodePattern{{Variable: "n", Labels: []string{"Person"}}}}}},
			Where: &WhereClause{Expression: &BinaryExpression{Left: &PropertyExpression{Variable: "n", Property: "name"}, Operator: "=", Right: &LiteralExpression{Value: "Bob"}}},
			Delete: &DeleteClause{Variables: []string{"n"}},
		}
		op, err := planner.Plan(context.Background(), q)
		assert.NoError(t, err)
		op.Open(ec)
		defer op.Close(ec)
		row, _ := op.Next(ec)
		assert.NotNil(t, row)
		n := row.bindings["n"].(*storage.Node)
		_, err = graph.GetNodeForTenant(n.ID, tenantID)
		assert.Error(t, err)
	})

	t.Run("UNION", func(t *testing.T) {
		graph.CreateNodeWithTenant(tenantID, []string{"Person"}, map[string]storage.Value{"name": storage.StringValue("Edward")})
		graph.CreateNodeWithTenant(tenantID, []string{"Person"}, map[string]storage.Value{"name": storage.StringValue("Frank")})
		q := &Query{
			Match: &MatchClause{Patterns: []*Pattern{{Nodes: []*NodePattern{{Variable: "n", Labels: []string{"Person"}}}}}},
			Where: &WhereClause{Expression: &BinaryExpression{Left: &PropertyExpression{Variable: "n", Property: "name"}, Operator: "=", Right: &LiteralExpression{Value: "Edward"}}},
			Return: &ReturnClause{Items: []*ReturnItem{{Expression: &PropertyExpression{Variable: "n", Property: "name"}, Alias: "name"}}},
			Union: &UnionClause{All: true},
			UnionNext: &Query{
				Match: &MatchClause{Patterns: []*Pattern{{Nodes: []*NodePattern{{Variable: "n", Labels: []string{"Person"}}}}}},
				Where: &WhereClause{Expression: &BinaryExpression{Left: &PropertyExpression{Variable: "n", Property: "name"}, Operator: "=", Right: &LiteralExpression{Value: "Frank"}}},
				Return: &ReturnClause{Items: []*ReturnItem{{Expression: &PropertyExpression{Variable: "n", Property: "name"}, Alias: "name"}}},
			},
		}
		op, err := planner.Plan(context.Background(), q)
		assert.NoError(t, err)
		op.Open(ec)
		defer op.Close(ec)
		names := make([]string, 0)
		for {
			row, _ := op.Next(ec)
			if row == nil { break }
			names = append(names, row.bindings["name"].(string))
		}
		assert.ElementsMatch(t, []string{"Edward", "Frank"}, names)
	})

	t.Run("Aggregations", func(t *testing.T) {
		q := &Query{
			Match: &MatchClause{Patterns: []*Pattern{{Nodes: []*NodePattern{{Variable: "n", Labels: []string{"Person"}}}}}},
			Return: &ReturnClause{Items: []*ReturnItem{{Aggregate: "COUNT", Expression: &PropertyExpression{Variable: "n"}, Alias: "count"}}},
		}
		op, err := planner.Plan(context.Background(), q)
		assert.NoError(t, err)
		op.Open(ec)
		defer op.Close(ec)
		row, _ := op.Next(ec)
		assert.NotNil(t, row)
		assert.GreaterOrEqual(t, row.bindings["count"], 1)
	})

	t.Run("MERGE Node", func(t *testing.T) {
		q := &Query{
			Merge: &MergeClause{
				Pattern: &Pattern{Nodes: []*NodePattern{{Variable: "n", Labels: []string{"Person"}, Properties: map[string]any{"name": "Diana"}}}},
				OnCreate: &SetClause{Assignments: []*Assignment{{Variable: "n", Property: "status", Value: "new"}}},
			},
			Return: &ReturnClause{Items: []*ReturnItem{{Expression: &PropertyExpression{Variable: "n"}}}},
		}
		op, err := planner.Plan(context.Background(), q)
		assert.NoError(t, err)
		op.Open(ec)
		defer op.Close(ec)
		row, _ := op.Next(ec)
		assert.NotNil(t, row)
		n := row.bindings["n"].(*storage.Node)
		name, _ := n.Properties["name"].AsString()
		assert.Equal(t, "Diana", name)

		qMatch := &Query{
			Merge: &MergeClause{
				Pattern: &Pattern{Nodes: []*NodePattern{{Variable: "n", Labels: []string{"Person"}, Properties: map[string]any{"name": "Diana"}}}},
				OnMatch: &SetClause{Assignments: []*Assignment{{Variable: "n", Property: "status", Value: "existing"}}},
			},
			Return: &ReturnClause{Items: []*ReturnItem{{Expression: &PropertyExpression{Variable: "n"}}}},
		}
		op2, _ := planner.Plan(context.Background(), qMatch)
		ec2 := newExecutionContext(ctx, graph)
		op2.Open(ec2)
		defer op2.Close(ec2)
		row2, _ := op2.Next(ec2)
		n2 := row2.bindings["n"].(*storage.Node)
		status2, _ := n2.Properties["status"].AsString()
		assert.Equal(t, "existing", status2)
	})

	t.Run("Multi-Hop Match", func(t *testing.T) {
		// Seed: a -> b -> c
		a, _ := graph.CreateNodeWithTenant(tenantID, []string{"Node"}, map[string]storage.Value{"name": storage.StringValue("A")})
		b, _ := graph.CreateNodeWithTenant(tenantID, []string{"Node"}, map[string]storage.Value{"name": storage.StringValue("B")})
		c, _ := graph.CreateNodeWithTenant(tenantID, []string{"Node"}, map[string]storage.Value{"name": storage.StringValue("C")})
		graph.CreateEdgeWithTenant(tenantID, a.ID, b.ID, "NEXT", nil, 1.0)
		graph.CreateEdgeWithTenant(tenantID, b.ID, c.ID, "NEXT", nil, 1.0)

		// MATCH (a {name: 'A'})-[:NEXT]->(b)-[:NEXT]->(c) RETURN c.name
		nodeA := &NodePattern{Variable: "a", Labels: []string{"Node"}, Properties: map[string]any{"name": "A"}}
		nodeB := &NodePattern{Variable: "b"}
		nodeC := &NodePattern{Variable: "c"}

		q := &Query{
			Match: &MatchClause{
				Patterns: []*Pattern{
					{
						Nodes: []*NodePattern{nodeA, nodeB, nodeC},
						Relationships: []*RelationshipPattern{
							{From: nodeA, To: nodeB, Type: "NEXT", Variable: "r1"},
							{From: nodeB, To: nodeC, Type: "NEXT", Variable: "r2"},
						},
					},
				},
			},
			Return: &ReturnClause{
				Items: []*ReturnItem{
					{Expression: &PropertyExpression{Variable: "c", Property: "name"}, Alias: "cname"},
				},
			},
		}

		op, err := planner.Plan(context.Background(), q)
		assert.NoError(t, err)

		err = op.Open(ec)
		assert.NoError(t, err)
		defer op.Close(ec)

		row, err := op.Next(ec)
		assert.NoError(t, err)
		assert.NotNil(t, row)
		assert.Equal(t, "C", row.bindings["cname"])
	})

	t.Run("Cartesian Join", func(t *testing.T) {
		// Seed: two disjoint nodes
		graph.CreateNodeWithTenant(tenantID, []string{"Letter"}, map[string]storage.Value{"val": storage.StringValue("X")})
		graph.CreateNodeWithTenant(tenantID, []string{"Number"}, map[string]storage.Value{"val": storage.IntValue(1)})

		// MATCH (a:Letter), (b:Number) RETURN a.val, b.val
		q := &Query{
			Match: &MatchClause{
				Patterns: []*Pattern{
					{Nodes: []*NodePattern{{Variable: "a", Labels: []string{"Letter"}}}},
					{Nodes: []*NodePattern{{Variable: "b", Labels: []string{"Number"}}}},
				},
			},
			Return: &ReturnClause{
				Items: []*ReturnItem{
					{Expression: &PropertyExpression{Variable: "a", Property: "val"}, Alias: "aval"},
					{Expression: &PropertyExpression{Variable: "b", Property: "val"}, Alias: "bval"},
				},
			},
		}

		op, err := planner.Plan(context.Background(), q)
		assert.NoError(t, err)

		err = op.Open(ec)
		assert.NoError(t, err)
		defer op.Close(ec)

		row, err := op.Next(ec)
		assert.NoError(t, err)
		assert.NotNil(t, row)
		assert.Equal(t, "X", row.bindings["aval"])
		assert.Equal(t, int64(1), row.bindings["bval"])
	})

	t.Run("Hash Join Selection", func(t *testing.T) {
		// MATCH (a:Person), (b:Person) WHERE a.name = b.name RETURN a, b
		// Both patterns bind 'a' and 'b', but let's use a simpler case:
		// MATCH (a:Person), (b:Person {name: "Alice"})
		// This should Cartesian join, but if we do:
		// MATCH (a:Person), (a)-[:KNOWS]->(b)
		// Wait, the planner handles single pattern multi-hop via Expand.
		
		// To force a Hash Join between disjoint patterns:
		// MATCH (n:Person), (m:Person) WHERE n = m RETURN n, m
		
		q := &Query{
			Match: &MatchClause{
				Patterns: []*Pattern{
					{Nodes: []*NodePattern{{Variable: "n", Labels: []string{"Person"}}}},
					{Nodes: []*NodePattern{{Variable: "n"}}}, // Shared variable 'n'
				},
			},
			Return: &ReturnClause{
				Items: []*ReturnItem{{Expression: &PropertyExpression{Variable: "n"}}},
			},
		}

		op, err := planner.Plan(context.Background(), q)
		assert.NoError(t, err)

		err = op.Open(ec)
		assert.NoError(t, err)
		defer op.Close(ec)

		row, err := op.Next(ec)
		assert.NoError(t, err)
		assert.NotNil(t, row)
	})

	t.Run("LLM Procedure Call", func(t *testing.T) {
		// CALL llm.generate("Hello", "gpt-4") YIELD response RETURN response
		q := &Query{
			Call: &CallClause{
				ProcedureName: "llm.generate",
				Arguments:     []Expression{&LiteralExpression{Value: "Hello"}, &LiteralExpression{Value: "gpt-4"}},
				YieldItems:    []string{"response"},
			},
			Return: &ReturnClause{
				Items: []*ReturnItem{{Expression: &PropertyExpression{Variable: "response"}}},
			},
		}

		op, err := planner.Plan(context.Background(), q)
		assert.NoError(t, err)

		err = op.Open(ec)
		assert.NoError(t, err)
		defer op.Close(ec)

		row, err := op.Next(ec)
		assert.NoError(t, err)
		assert.NotNil(t, row)
		assert.Contains(t, row.bindings["response"].(string), "[MOCK RESPONSE for model gpt-4]")
	})

	t.Run("Auto-Embeddings", func(t *testing.T) {
		// Setup embedder
		embedder := intelligence.NewEmbedder(graph)
		embedder.AddPolicy(intelligence.EmbeddingPolicy{
			Label:          "Document",
			SourceProperty: "content",
			TargetProperty: "embedding",
		})
		graph.AddObserver(embedder)

		// CREATE (d:Document {content: "This is a test document."})
		q := &Query{
			Create: &CreateClause{
				Patterns: []*Pattern{
					{
						Nodes: []*NodePattern{{Variable: "d", Labels: []string{"Document"}, Properties: map[string]any{"content": "This is a test document."}}},
					},
				},
			},
			Return: &ReturnClause{
				Items: []*ReturnItem{{Expression: &PropertyExpression{Variable: "d"}}},
			},
		}

		op, err := planner.Plan(context.Background(), q)
		assert.NoError(t, err)

		err = op.Open(ec)
		assert.NoError(t, err)
		defer op.Close(ec)

		row, err := op.Next(ec)
		assert.NoError(t, err)
		assert.NotNil(t, row)
		d := row.bindings["d"].(*storage.Node)

		// Wait for background embedding
		time.Sleep(100 * time.Millisecond)

		// Check for embedding
		node, _ := graph.GetNodeForTenant(d.ID, tenantID)
		_, hasEmbedding := node.Properties["embedding"]
		assert.True(t, hasEmbedding, "Node should have an automatically generated embedding")
	})
}
