package query

import (
	"fmt"
	"time"
)

// StepDescriber provides human-readable descriptions of execution steps
type StepDescriber interface {
	StepName() string
	StepDetail() string
}

// Implement StepDescriber on all step types

func (ms *MatchStep) StepName() string    { return "MatchStep" }
func (ms *MatchStep) StepDetail() string  { return fmt.Sprintf("patterns=%d", len(ms.match.Patterns)) }
func (fs *FilterStep) StepName() string   { return "FilterStep" }
func (fs *FilterStep) StepDetail() string { return "WHERE filter" }
func (cs *CreateStep) StepName() string   { return "CreateStep" }
func (cs *CreateStep) StepDetail() string {
	return fmt.Sprintf("patterns=%d", len(cs.create.Patterns))
}
func (ss *SetStep) StepName() string    { return "SetStep" }
func (ss *SetStep) StepDetail() string  { return fmt.Sprintf("assignments=%d", len(ss.set.Assignments)) }
func (ds *DeleteStep) StepName() string { return "DeleteStep" }
func (ds *DeleteStep) StepDetail() string {
	return fmt.Sprintf("variables=%d", len(ds.delete.Variables))
}
func (rs *ReturnStep) StepName() string { return "ReturnStep" }
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

// runStep runs one step. Under PROFILE it also records the step's timing and
// the number of rows it left. Every segment of a WITH chain and every part of a
// UNION runs its steps through here, so a profile covers the whole statement and
// the query returns the rows it returns without PROFILE.
func runStep(execCtx *ExecutionContext, step ExecutionStep, profiles *[]StepProfile) error {
	if !profiling(execCtx.context) {
		return step.Execute(execCtx)
	}
	start := time.Now()
	if err := step.Execute(execCtx); err != nil {
		return err
	}
	sp := StepProfile{
		Duration: time.Since(start),
		RowsOut:  len(execCtx.results),
	}
	if describer, ok := step.(StepDescriber); ok {
		sp.StepName = describer.StepName()
		sp.Detail = describer.StepDetail()
	}
	*profiles = append(*profiles, sp)
	return nil
}
