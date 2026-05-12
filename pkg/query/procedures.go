package query

import (
	"context"
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/gnn"
	"github.com/dd0wney/cluso-graphdb/pkg/intelligence"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Procedure represents a callable Cypher procedure.
type Procedure func(ctx context.Context, graph storage.Storage, tenantID string, args []any) ([]map[string]any, error)

var procedureRegistry = map[string]Procedure{
	"algo.shortestPath": shortestPathProcedure,
	"gnn.messagePass":   messagePassProcedure,
	"llm.generate":      llmGenerateProcedure,
}

// RegisterProcedure adds a new procedure to the global registry.
func RegisterProcedure(name string, proc Procedure) {
	procedureRegistry[name] = proc
}

func shortestPathProcedure(ctx context.Context, graph storage.Storage, tenantID string, args []any) ([]map[string]any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("shortestPath requires 2 arguments (start, end)")
	}
	srcID := uint64(coerceToInt(args[0]))
	dstID := uint64(coerceToInt(args[1]))
	
	// Stub for now - in real life this calls pkg/algorithms
	path := []uint64{srcID, dstID}
	return []map[string]any{{"path": path}}, nil
}

func messagePassProcedure(ctx context.Context, graph storage.Storage, tenantID string, args []any) ([]map[string]any, error) {
	if len(args) < 5 {
		return nil, fmt.Errorf("messagePass requires 5 arguments (nodes, featProp, outProp, hops, agg)")
	}

	nodesVal := args[0]
	featProp := args[1].(string)
	outProp := args[2].(string)
	hops := int(coerceToInt(args[3]))
	agg := gnn.AggregationType(args[4].(string))

	var nodeIDs []uint64
	if ids, ok := nodesVal.([]uint64); ok {
		nodeIDs = ids
	} else if node, ok := nodesVal.(*storage.Node); ok {
		nodeIDs = []uint64{node.ID}
	}

	err := gnn.MessagePass(ctx, graph, tenantID, nodeIDs, featProp, outProp, hops, agg)
	if err != nil {
		return nil, err
	}
	return []map[string]any{{"status": "success"}}, nil
}

func llmGenerateProcedure(ctx context.Context, graph storage.Storage, tenantID string, args []any) ([]map[string]any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("llm.generate requires at least 1 argument (prompt)")
	}

	prompt, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("llm.generate: prompt must be a string")
	}

	model := "claude-3-haiku-20240307"
	if len(args) > 1 {
		if m, ok := args[1].(string); ok {
			model = m
		}
	}

	client := intelligence.NewLLMClient(model)
	response, err := client.Generate(ctx, prompt)
	if err != nil {
		return nil, err
	}

	return []map[string]any{{"response": response}}, nil
}

func coerceToInt(v any) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case int:
		return int64(val)
	case float64:
		return int64(val)
	case uint64:
		return int64(val)
	default:
		return 0
	}
}
