// Package query's planner.go translates a Query AST into a tree of
// PhysicalOperators (C4.0 extraction + C4.1 q.Call reinstate). Consumes
// C3.0's operator types + C3.1's CallOperator.
//
// Status: PARTIAL. The planner runs only when wired by a planner-driven
// executor path; the existing Step-based Executor (executor.go +
// executor_steps.go) keeps serving /v1/cypher. C5 (parser CALL/YIELD) +
// C3.1 (CallOperator) + this file's q.Call block now form the full
// AST → operator translation for CALL ... YIELD; execution requires
// C6's procedure bodies (Decision-6-gated in NEXT_STEPS_2026-05-13.md).
//
// Deferred:
//   - Planner-level unit tests — deferred (mirroring the C1.0/C1.1 and
//     C3.0/C3.1 splits). The q.Call reinstate in C4.1 specifically should
//     get a planner test that confirms a parsed CALL query produces a
//     plan tree containing CallOperator at the right position.
//
// Several archive comments are preserved verbatim: "Simplified" on the
// OPTIONAL MATCH path, "for the spike" on the linear-expansion comment,
// "redundant but safe" on isWhereConsumedByIndex. These are honest spike
// markers — left intact to keep the lift surgical; cleanup belongs in a
// follow-up once tests anchor the contract.
package query

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// Planner translates a Query AST into a tree of PhysicalOperators.
type Planner struct {
	graph storage.Storage
}

// NewPlanner creates a new query planner.
func NewPlanner(graph storage.Storage) *Planner {
	return &Planner{graph: graph}
}

// Plan creates a physical execution plan for the given query.
func (p *Planner) Plan(ctx context.Context, q *Query) (PhysicalOperator, error) {
	return p.PlanSub(ctx, q, nil)
}

// PlanSub creates an execution plan, potentially with an existing input source.
func (p *Planner) PlanSub(ctx context.Context, q *Query, input PhysicalOperator) (PhysicalOperator, error) {
	_, span := otel.Tracer("query").Start(ctx, "Planner.PlanSub")
	defer span.End()

	// Handle UNION
	if q.Union != nil && q.UnionNext != nil {
		left, err := p.PlanSub(ctx, q.withoutUnion(), input)
		if err != nil {
			return nil, err
		}
		right, err := p.PlanSub(ctx, q.UnionNext, input)
		if err != nil {
			return nil, err
		}
		return &UnionOperator{
			Left:  left,
			Right: right,
			All:   q.Union.All,
		}, nil
	}

	op := input
	// 1. Handle MATCH clause
	if q.Match != nil {
		// Optimization: Look ahead at WHERE for index seek
		matchOp, err := p.planMatchWithOptimization(q.Match, q.Where)
		if err != nil {
			return nil, err
		}

		if op != nil {
			// Join previous segments with new MATCH results
			op = &NestedLoopJoinOperator{
				Left:  op,
				Right: matchOp,
			}
		} else {
			op = matchOp
		}
	}

	// 1.2 Handle OPTIONAL MATCH
	for _, om := range q.OptionalMatches {
		if len(om.Patterns) > 0 {
			op = &OptionalMatchOperator{
				Input:   op,
				Pattern: om.Patterns[0], // Simplified
			}
		}
	}

	// 1.4 Handle UNWIND
	if q.Unwind != nil {
		op = &UnwindOperator{
			Input:      op,
			Expression: q.Unwind.Expression,
			Alias:      q.Unwind.Alias,
		}
	}

	// 1.5 Handle CALL clause
	if q.Call != nil {
		op = &CallOperator{
			Input:         op,
			ProcedureName: q.Call.ProcedureName,
			Arguments:     q.Call.Arguments,
			YieldItems:    q.Call.YieldItems,
		}
	}

	// 2. Handle WHERE clause (if not fully consumed by index seek)
	if q.Where != nil && !p.isWhereConsumedByIndex(q.Where) {
		op = &FilterOperator{
			Input:      op,
			Expression: q.Where.Expression,
		}
	}

	// 2.1 Handle CREATE clause
	if q.Create != nil {
		op = &CreateOperator{
			Input:    op,
			Patterns: q.Create.Patterns,
		}
	}

	// 2.2 Handle SET clause
	if q.Set != nil {
		op = &SetOperator{
			Input:       op,
			Assignments: q.Set.Assignments,
		}
	}

	// 2.3 Handle DELETE clause
	if q.Delete != nil {
		op = &DeleteOperator{
			Input:     op,
			Variables: q.Delete.Variables,
			Detach:    q.Delete.Detach,
		}
	}

	// 2.4 Handle REMOVE clause
	if q.Remove != nil {
		op = &RemoveOperator{
			Input: op,
			Items: q.Remove.Items,
		}
	}

	// 2.5 Handle MERGE clause
	if q.Merge != nil {
		op = &MergeOperator{
			Input:    op,
			Pattern:  q.Merge.Pattern,
			OnMatch:  q.Merge.OnMatch,
			OnCreate: q.Merge.OnCreate,
		}
	}

	// 3. Handle Aggregations or Projection
	if q.Return != nil {
		if p.hasAggregates(q.Return) {
			op = &AggregateOperator{
				Input: op,
				Items: q.Return.Items,
			}
		} else {
			op = &ProjectOperator{
				Input: op,
				Items: q.Return.Items,
			}
		}
	} else if q.With != nil && q.Next != nil {
		// WITH clause - project and then continue with next query segment
		projected := &ProjectOperator{
			Input: op,
			Items: q.With.Items,
		}

		return p.PlanSub(ctx, q.Next, projected)
	}

	return op, nil
}

func (q *Query) withoutUnion() *Query {
	copy := *q
	copy.Union = nil
	copy.UnionNext = nil
	return &copy
}

func (p *Planner) hasAggregates(r *ReturnClause) bool {
	for _, item := range r.Items {
		if item.Aggregate != "" {
			return true
		}
	}
	return false
}

func (p *Planner) planMatchWithOptimization(m *MatchClause, w *WhereClause) (PhysicalOperator, error) {
	if len(m.Patterns) == 0 {
		return nil, fmt.Errorf("MATCH clause must have at least one pattern")
	}

	var op PhysicalOperator
	boundVars := make(map[string]bool)

	for _, pattern := range m.Patterns {
		patternOp, err := p.planSinglePattern(pattern, w)
		if err != nil {
			return nil, err
		}

		// Update bound vars for the new pattern
		patternVars := p.getPatternVars(pattern)

		if op == nil {
			op = patternOp
		} else {
			// Check for common variables to perform an Equijoin (Hash Join)
			commonVar := ""
			for v := range patternVars {
				if boundVars[v] {
					commonVar = v
					break
				}
			}

			if commonVar != "" {
				op = &HashJoinOperator{
					Left:  op,
					Right: patternOp,
					Var:   commonVar,
				}
			} else {
				// No common variables, Cartesian Product
				op = &NestedLoopJoinOperator{
					Left:  op,
					Right: patternOp,
				}
			}
		}

		for v := range patternVars {
			boundVars[v] = true
		}
	}

	return op, nil
}

func (p *Planner) getPatternVars(pattern *Pattern) map[string]bool {
	vars := make(map[string]bool)
	for _, node := range pattern.Nodes {
		if node.Variable != "" {
			vars[node.Variable] = true
		}
	}
	for _, rel := range pattern.Relationships {
		if rel.Variable != "" {
			vars[rel.Variable] = true
		}
	}
	return vars
}

func (p *Planner) planSinglePattern(pattern *Pattern, w *WhereClause) (PhysicalOperator, error) {
	if len(pattern.Nodes) == 0 {
		return nil, fmt.Errorf("pattern must have at least one node")
	}

	// 1. Plan the starting node (with index seek if possible)
	startNode := pattern.Nodes[0]
	var op PhysicalOperator

	// Check for index seek opportunity on the starting node
	if w != nil {
		if prop, val, ok := p.findEqualityCondition(w.Expression, startNode.Variable); ok {
			if p.graph.HasPropertyIndex(prop) {
				op = &IndexSeekOperator{
					Variable:    startNode.Variable,
					PropertyKey: prop,
					Value:       convertToStorageValue(val),
				}
			}
		}
	}

	if op == nil {
		label := ""
		if len(startNode.Labels) > 0 {
			label = startNode.Labels[0]
		}
		op = &NodeScanOperator{
			Variable: startNode.Variable,
			Label:    label,
		}
	}

	// 2. Chain expansions for each relationship
	// Map from NodePattern pointer to its index in the Nodes slice isn't reliable if the AST
	// structure is complex. We'll use the Relationships slice to drive the expansion.
	for _, rel := range pattern.Relationships {
		// For the spike, assume simple linear expansion: n1-r1-n2-r2-n3
		// We'll expand from From to To.
		op = &ExpandOperator{
			Input:     op,
			SourceVar: rel.From.Variable,
			TargetVar: rel.To.Variable,
			EdgeVar:   rel.Variable,
			EdgeType:  rel.Type,
		}
	}

	return op, nil
}

func (p *Planner) findEqualityCondition(expr Expression, variable string) (string, any, bool) {
	// Simple rule: look for n.prop = value
	if bin, ok := expr.(*BinaryExpression); ok && bin.Operator == "=" {
		if prop, ok := bin.Left.(*PropertyExpression); ok && prop.Variable == variable {
			if lit, ok := bin.Right.(*LiteralExpression); ok {
				return prop.Property, lit.Value, true
			}
		}
		// Also check value = n.prop
		if prop, ok := bin.Right.(*PropertyExpression); ok && prop.Variable == variable {
			if lit, ok := bin.Left.(*LiteralExpression); ok {
				return prop.Property, lit.Value, true
			}
		}
	}
	return "", nil, false
}

func (p *Planner) isWhereConsumedByIndex(w *WhereClause) bool {
	// For spike, assume if we found an equality condition it's consumed.
	// In real life, we'd only consume the specific part of the expression.
	return false // Keep FilterOperator just in case for now (redundant but safe)
}
