package query

import (
	"fmt"

	"go.opentelemetry.io/otel"
)

// CallOperator executes a procedure call (e.g., algorithm).
// Procedure dispatch goes through procedureRegistry (pkg/query/procedures.go).
// C3.1 lands the operator + skeleton registry; C6 registers actual procedures
// once Decision 6 (S1↔algorithms storage-type wiring) is resolved.
type CallOperator struct {
	Input         PhysicalOperator
	ProcedureName string
	Arguments     []Expression
	YieldItems    []string // Variables to bind from result

	curInput *BindingSet
	results  []map[string]any
	index    int
	executed bool // For standalone CALL
}

func (o *CallOperator) Open(ctx *ExecutionContext) error {
	_, span := otel.Tracer("query").Start(ctx.context, "CallOperator.Open")
	defer span.End()

	o.executed = false
	if o.Input != nil {
		return o.Input.Open(ctx)
	}
	return nil
}

func (o *CallOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	for {
		if o.index >= len(o.results) {
			// Fetch next input or run once if standalone
			var row *BindingSet
			var err error
			if o.Input != nil {
				row, err = o.Input.Next(ctx)
				if err != nil {
					return nil, err
				}
				if row == nil {
					return nil, nil // All done
				}
			} else {
				if o.executed {
					return nil, nil
				}
				o.executed = true
				row = &BindingSet{bindings: make(map[string]any)}
			}
			o.curInput = row

			// Resolve and execute procedure
			proc, ok := procedureRegistry[o.ProcedureName]
			if !ok {
				return nil, fmt.Errorf("unknown procedure: %s", o.ProcedureName)
			}

			// Evaluate arguments
			argValues := make([]any, len(o.Arguments))
			for i, arg := range o.Arguments {
				argValues[i] = extractValue(arg, row.bindings)
			}

			results, err := proc(ctx.context, ctx.graph, ctx.tenantID, argValues)
			if err != nil {
				return nil, err
			}
			o.results = results
			o.index = 0
		}

		// Yield result from procedure for current input row
		res := o.results[o.index]
		o.index++

		newBindings := make(map[string]any)
		for k, v := range o.curInput.bindings {
			newBindings[k] = v
		}
		for _, yield := range o.YieldItems {
			if val, ok := res[yield]; ok {
				newBindings[yield] = val
			}
		}

		return &BindingSet{bindings: newBindings}, nil
	}
}

func (o *CallOperator) Close(ctx *ExecutionContext) error {
	o.results = nil
	return nil
}
