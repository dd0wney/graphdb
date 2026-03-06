package query

import (
	"context"
	"fmt"
	"time"
)

// StepDescriber provides human-readable descriptions of execution steps
type StepDescriber interface {
	StepName() string
	StepDetail() string
}

// Implement StepDescriber on all step types

func (ms *MatchStep) StepName() string   { return "MatchStep" }
func (ms *MatchStep) StepDetail() string  { return fmt.Sprintf("patterns=%d", len(ms.match.Patterns)) }
func (fs *FilterStep) StepName() string   { return "FilterStep" }
func (fs *FilterStep) StepDetail() string { return "WHERE filter" }
func (cs *CreateStep) StepName() string   { return "CreateStep" }
func (cs *CreateStep) StepDetail() string {
	return fmt.Sprintf("patterns=%d", len(cs.create.Patterns))
}
func (ss *SetStep) StepName() string   { return "SetStep" }
func (ss *SetStep) StepDetail() string { return fmt.Sprintf("assignments=%d", len(ss.set.Assignments)) }
func (ds *DeleteStep) StepName() string   { return "DeleteStep" }
func (ds *DeleteStep) StepDetail() string { return fmt.Sprintf("variables=%d", len(ds.delete.Variables)) }
func (rs *ReturnStep) StepName() string   { return "ReturnStep" }
func (rs *ReturnStep) StepDetail() string {
	return fmt.Sprintf("items=%d limit=%d skip=%d", len(rs.returnClause.Items), rs.limit, rs.skip)
}
func (ils *IndexLookupStep) StepName() string { return "IndexLookupStep" }
func (ils *IndexLookupStep) StepDetail() string {
	return fmt.Sprintf("property=%s variable=%s", ils.propertyKey, ils.variable)
}

// buildExplainResult serializes the execution plan steps as rows
func buildExplainResult(plan *ExecutionPlan) *ResultSet {
	result := &ResultSet{
		Columns: []string{"step", "detail"},
		Rows:    make([]map[string]any, 0, len(plan.Steps)),
	}

	for _, step := range plan.Steps {
		row := map[string]any{
			"step":   "Unknown",
			"detail": "",
		}
		if describer, ok := step.(StepDescriber); ok {
			row["step"] = describer.StepName()
			row["detail"] = describer.StepDetail()
		}
		result.Rows = append(result.Rows, row)
	}

	result.Count = len(result.Rows)
	return result
}

// executeWithProfiling executes the plan while collecting per-step timing and cardinality
func (e *Executor) executeWithProfiling(ctx context.Context, plan *ExecutionPlan, query *Query) (*ResultSet, error) {
	execCtx := &ExecutionContext{
		context:  ctx,
		graph:    e.graph,
		bindings: make(map[string]any),
		results:  make([]*BindingSet, 0),
	}

	execCtx.results = append(execCtx.results, &BindingSet{
		bindings: make(map[string]any),
	})

	profiles := make([]StepProfile, 0, len(plan.Steps))

	for i, step := range plan.Steps {
		if err := execCtx.CheckCancellation(); err != nil {
			return nil, fmt.Errorf("cancelled at step %d: %w", i, err)
		}

		start := time.Now()
		if err := step.Execute(execCtx); err != nil {
			return nil, err
		}
		elapsed := time.Since(start)

		sp := StepProfile{
			Duration: elapsed,
			RowsOut:  len(execCtx.results),
		}
		if describer, ok := step.(StepDescriber); ok {
			sp.StepName = describer.StepName()
			sp.Detail = describer.StepDetail()
		}
		profiles = append(profiles, sp)
	}

	if err := execCtx.CheckCancellation(); err != nil {
		return nil, fmt.Errorf("cancelled before building results: %w", err)
	}

	var result *ResultSet
	if query.Return != nil {
		result = e.buildResultSet(execCtx, query.Return, query.Limit, query.Skip)
	} else {
		result = &ResultSet{
			Columns: []string{"affected"},
			Rows:    []map[string]any{{"affected": len(execCtx.results)}},
			Count:   len(execCtx.results),
		}
	}

	result.Profile = profiles
	return result, nil
}
