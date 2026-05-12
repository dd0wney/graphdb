package query

import (
	"context"
	"fmt"
	
	"time"

	"go.opentelemetry.io/otel"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

var tracer = otel.Tracer("query")

const (
	// DefaultQueryTimeout is the default timeout for query execution
	DefaultQueryTimeout = 30 * time.Second

	// MaxQueryTimeout is the maximum allowed query timeout
	MaxQueryTimeout = 5 * time.Minute
)

// Executor executes parsed queries against a graph
type Executor struct {
	graph        storage.Storage
	optimizer    *Optimizer
	cache        *QueryCache
	queryTimeout time.Duration
	searchIndex  any // *search.FullTextIndex, stored as any to avoid import cycle

	// Vector search closures (set via SetVectorSearch)
	vectorSimilarity VectorSimilarityFunc
	vectorSearch     VectorSearchFunc
	hasVectorIndex   HasVectorIndexFunc
	getNode          GetNodeFunc
}

// NewExecutor creates a new query executor
func NewExecutor(graph storage.Storage) *Executor {
	return &Executor{
		graph:        graph,
		optimizer:    NewOptimizer(graph),
		cache:        NewQueryCache(),
		queryTimeout: DefaultQueryTimeout,
	}
}

// NewExecutorWithTimeout creates a new query executor with custom timeout
func NewExecutorWithTimeout(graph storage.Storage, timeout time.Duration) *Executor {
	return &Executor{
		graph:        graph,
		optimizer:    NewOptimizer(graph),
		cache:        NewQueryCache(),
		queryTimeout: ValidateQueryTimeout(timeout),
	}
}

// SetQueryTimeout sets the query timeout
func (e *Executor) SetQueryTimeout(timeout time.Duration) {
	e.queryTimeout = ValidateQueryTimeout(timeout)
}

// Execute executes a query and returns results.
// Includes panic recovery to prevent server crashes from malformed queries.
// Uses the default query timeout.
func (e *Executor) Execute(query *Query) (*ResultSet, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e.queryTimeout)
	defer cancel()
	return e.ExecuteWithContext(ctx, query)
}

// ExecuteWithContext executes a query using the Volcano Physical Operator engine.
// Includes context support for cancellation and timeout, and panic recovery.
func (e *Executor) ExecuteWithContext(ctx context.Context, query *Query) (result *ResultSet, err error) {
	ctx, span := tracer.Start(ctx, "Executor.ExecuteWithContext")
	defer span.End()

	// Panic recovery
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("query execution panicked: %v", r)
		}
	}()

	// Build and drive the physical plan
	planner := NewPlanner(e.graph)
	op, err := planner.Plan(ctx, query)
	if err != nil {
		return nil, err
	}

	execCtx := newExecutionContext(ctx, e.graph)
	if err := op.Open(execCtx); err != nil {
		return nil, fmt.Errorf("failed to open query plan: %w", err)
	}
	defer op.Close(execCtx)

	rows := make([]map[string]any, 0)
	for {
		// Check for cancellation during execution
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		binding, err := op.Next(execCtx)
		if err != nil {
			return nil, err
		}
		if binding == nil {
			break
		}

		// Convert binding to row
		row := make(map[string]any)
		for k, v := range binding.bindings {
			row[k] = v
		}
		rows = append(rows, row)

		if query.Limit > 0 && len(rows) >= query.Limit {
			break
		}
	}

	return &ResultSet{
		Rows:  rows,
		Count: len(rows),
	}, nil
}

// ExecuteWithParams executes a parameterized query. Parameters are provided as a map
// and injected into the query before execution. ParameterRef values in property maps
// are resolved to actual values, and parameters are made available in bindings as "$name" keys.
func (e *Executor) ExecuteWithParams(query *Query, params map[string]any) (*ResultSet, error) {
	// Validate and resolve ParameterRef values in property maps
	if err := resolveParameters(query, params); err != nil {
		return nil, err
	}

	// Validate that all ParameterExpression references have corresponding params
	if err := validateParameterExpressions(query, params); err != nil {
		return nil, err
	}

	// Inject params into initial bindings with "$" prefix to avoid collision with variables
	bindings := &BindingSet{bindings: make(map[string]any)}
	for k, v := range params {
		bindings.bindings["$"+k] = v
	}
	query.InitialBindings = []*BindingSet{bindings}

	return e.Execute(query)
}

// resolveParameters replaces ParameterRef values in pattern property maps with actual param values
func resolveParameters(query *Query, params map[string]any) error {
	if query.Match != nil {
		for _, pattern := range query.Match.Patterns {
			if err := resolvePatternParams(pattern, params); err != nil {
				return err
			}
		}
	}
	if query.Create != nil {
		for _, pattern := range query.Create.Patterns {
			if err := resolvePatternParams(pattern, params); err != nil {
				return err
			}
		}
	}
	if query.Merge != nil {
		if err := resolvePatternParams(query.Merge.Pattern, params); err != nil {
			return err
		}
	}
	return nil
}

func resolvePatternParams(pattern *Pattern, params map[string]any) error {
	for _, node := range pattern.Nodes {
		resolved, err := resolvePropertyParams(node.Properties, params)
		if err != nil {
			return err
		}
		node.Properties = resolved
	}
	for _, rel := range pattern.Relationships {
		resolved, err := resolvePropertyParams(rel.Properties, params)
		if err != nil {
			return err
		}
		rel.Properties = resolved
	}
	return nil
}

// resolvePropertyParams returns a new map with ParameterRef values replaced by actual values.
// The original map is not modified, making repeated calls with different params safe.
func resolvePropertyParams(props map[string]any, params map[string]any) (map[string]any, error) {
	if props == nil {
		return nil, nil
	}
	resolved := make(map[string]any, len(props))
	for key, val := range props {
		if ref, ok := val.(*ParameterRef); ok {
			actual, exists := params[ref.Name]
			if !exists {
				return nil, fmt.Errorf("missing parameter: $%s", ref.Name)
			}
			resolved[key] = actual
		} else {
			resolved[key] = val
		}
	}
	return resolved, nil
}

// validateParameterExpressions walks all expression trees in the query to find
// ParameterExpression nodes and ensures corresponding params exist.
func validateParameterExpressions(query *Query, params map[string]any) error {
	if query.Where != nil {
		if err := validateExprParams(query.Where.Expression, params); err != nil {
			return err
		}
	}
	for _, om := range query.OptionalMatches {
		if om.Where != nil {
			if err := validateExprParams(om.Where.Expression, params); err != nil {
				return err
			}
		}
	}
	if query.Return != nil {
		for _, item := range query.Return.Items {
			if item.ValueExpr != nil {
				if err := validateExprParams(item.ValueExpr, params); err != nil {
					return err
				}
			}
		}
	}
	if query.With != nil {
		if query.With.Where != nil {
			if err := validateExprParams(query.With.Where.Expression, params); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateExprParams(expr Expression, params map[string]any) error {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ParameterExpression:
		if _, ok := params[e.Name]; !ok {
			return fmt.Errorf("missing parameter: $%s", e.Name)
		}
	case *BinaryExpression:
		if err := validateExprParams(e.Left, params); err != nil {
			return err
		}
		return validateExprParams(e.Right, params)
	case *FunctionCallExpression:
		for _, arg := range e.Args {
			if err := validateExprParams(arg, params); err != nil {
				return err
			}
		}
	case *CaseExpression:
		if err := validateExprParams(e.Operand, params); err != nil {
			return err
		}
		for _, wc := range e.WhenClauses {
			if err := validateExprParams(wc.Condition, params); err != nil {
				return err
			}
			if err := validateExprParams(wc.Result, params); err != nil {
				return err
			}
		}
		if err := validateExprParams(e.ElseResult, params); err != nil {
			return err
		}
	}
	return nil
}

// executeUnion executes two query segments and combines their results.
// UNION deduplicates rows; UNION ALL preserves all rows.

// ExecuteWithText executes a query from text and uses query caching
func (e *Executor) ExecuteWithText(queryText string, query *Query) (*ResultSet, error) {
	// TODO: Port query cache to Volcano engine (store PhysicalOperator trees)
	return e.ExecuteWithContext(context.Background(), query)
}

// extractValueFromBinding extracts a value from a binding, handling node properties
