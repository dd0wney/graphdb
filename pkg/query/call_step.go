package query

import (
	"sort"
	"strings"
)

// CallStep runs a CALL procedure once per input binding and joins each
// procedure row onto that binding.
//
// The parser stored CALL in Query.Call and buildExecutionPlan dropped it, so
// every CALL answered affected=1: a missing procedure raised nothing and a
// real one never ran. Both refusals below happen before the first row is read,
// so they do not depend on the data (an empty MATCH must refuse too).
type CallStep struct {
	call *CallClause
}

func (s *CallStep) Execute(ctx *ExecutionContext) error {
	proc, ok := procedureRegistry[s.call.ProcedureName]
	if !ok {
		return refusef("unknown procedure: %s", s.call.ProcedureName)
	}

	for _, y := range s.call.YieldItems {
		for _, in := range ctx.results {
			if err := refuseBound(in, y); err != nil {
				return err
			}
		}
	}

	out := make([]*BindingSet, 0, len(ctx.results))
	var columns []string
	columnsSet := false

	for _, in := range ctx.results {
		if err := ctx.CheckCancellation(); err != nil {
			return err
		}

		args := make([]any, len(s.call.Arguments))
		for i, arg := range s.call.Arguments {
			v, err := evalOperand(arg, in.bindings)
			if err != nil {
				return err
			}
			args[i] = v
		}

		rows, err := proc(ctx.context, ctx.graph, ctx.tenantID, args)
		if err != nil {
			return err
		}

		for _, row := range rows {
			// Fix the output columns from the first row only. Procedures return
			// a uniform shape, and a later row must not change the header.
			if !columnsSet {
				columns = s.outputColumns(row)
				columnsSet = true
			}
			b, err := s.bindRow(in, row)
			if err != nil {
				return err
			}
			out = append(out, b)
		}
	}

	// With no procedure row to read, a YIELD list still names the columns.
	if !columnsSet {
		columns = append([]string(nil), s.call.YieldItems...)
	}
	ctx.results = out
	ctx.callColumns = columns
	return nil
}

func (s *CallStep) outputColumns(row map[string]any) []string {
	if len(s.call.YieldItems) > 0 {
		return append([]string(nil), s.call.YieldItems...)
	}
	return sortedColumnNames(row)
}

func (s *CallStep) bindRow(in *BindingSet, row map[string]any) (*BindingSet, error) {
	bindings := make(map[string]any, len(in.bindings)+len(row))
	for k, v := range in.bindings {
		bindings[k] = v
	}

	if len(s.call.YieldItems) == 0 {
		for k, v := range row {
			if err := refuseBound(in, k); err != nil {
				return nil, err
			}
			bindings[k] = v
		}
		return &BindingSet{bindings: bindings}, nil
	}

	for _, y := range s.call.YieldItems {
		v, ok := row[y]
		if !ok {
			return nil, refusef("procedure %s has no column %q; it returns: %s",
				s.call.ProcedureName, y, strings.Join(sortedColumnNames(row), ", "))
		}
		bindings[y] = v
	}
	return &BindingSet{bindings: bindings}, nil
}

// refuseBound stops a procedure column from overwriting a variable an earlier
// clause bound. Without it the node would vanish from the row with no error.
func refuseBound(in *BindingSet, name string) error {
	if _, bound := in.bindings[name]; bound {
		return refusef("column %q is already bound by an earlier clause", name)
	}
	return nil
}

func sortedColumnNames(row map[string]any) []string {
	names := make([]string, 0, len(row))
	for k := range row {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
