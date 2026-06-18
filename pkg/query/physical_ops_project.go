package query

import (
	"fmt"
)

// FilterOperator filters rows based on a predicate.
type FilterOperator struct {
	Input      PhysicalOperator
	Expression Expression // Use existing Expression interface if available
}

func (o *FilterOperator) Open(ctx *ExecutionContext) error {
	return o.Input.Open(ctx)
}

func (o *FilterOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	for {
		binding, err := o.Input.Next(ctx)
		if err != nil {
			return nil, err
		}
		if binding == nil {
			return nil, nil
		}

		match, err := o.Expression.Eval(binding.bindings)
		if err != nil {
			return nil, err
		}

		if match {
			return binding, nil
		}
	}
}

func (o *FilterOperator) Close(ctx *ExecutionContext) error {
	return o.Input.Close(ctx)
}

// ProjectOperator transforms rows into the final result format.
type ProjectOperator struct {
	Input PhysicalOperator
	Items []*ReturnItem
}

func (o *ProjectOperator) Open(ctx *ExecutionContext) error {
	return o.Input.Open(ctx)
}

func (o *ProjectOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	binding, err := o.Input.Next(ctx)
	if err != nil || binding == nil {
		return binding, err
	}

	newBindings := make(map[string]any, len(o.Items))
	for _, item := range o.Items {
		alias := item.Alias
		if alias == "" && item.Expression != nil {
			alias = item.Expression.Variable
		}
		if alias == "" {
			continue
		}

		if item.ValueExpr != nil {
			val, err := item.ValueExpr.EvalValue(binding.bindings)
			if err != nil {
				return nil, err
			}
			newBindings[alias] = val
		} else if item.Expression != nil {
			if item.Expression.Property == "" {
				newBindings[alias] = binding.bindings[item.Expression.Variable]
			} else {
				val := extractValue(item.Expression, binding.bindings)
				newBindings[alias] = val
			}
		}
	}

	return &BindingSet{bindings: newBindings}, nil
}

func (o *ProjectOperator) Close(ctx *ExecutionContext) error {
	return o.Input.Close(ctx)
}

// UnwindOperator expands list values into individual rows.
type UnwindOperator struct {
	Input      PhysicalOperator
	Expression *PropertyExpression
	Alias      string

	curInput *BindingSet
	items    []any
	index    int
}

func (o *UnwindOperator) Open(ctx *ExecutionContext) error {
	return o.Input.Open(ctx)
}

func (o *UnwindOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	for {
		if o.curInput == nil {
			var err error
			o.curInput, err = o.Input.Next(ctx)
			if err != nil || o.curInput == nil {
				return nil, err
			}

			val := extractValue(o.Expression, o.curInput.bindings)
			if val == nil {
				o.curInput = nil
				continue
			}

			switch v := val.(type) {
			case []any:
				o.items = v
			default:
				o.items = []any{v}
			}
			o.index = 0
		}

		if o.index < len(o.items) {
			item := o.items[o.index]
			o.index++

			newBindings := make(map[string]any, len(o.curInput.bindings)+1)
			for k, v := range o.curInput.bindings {
				newBindings[k] = v
			}
			newBindings[o.Alias] = item
			return &BindingSet{bindings: newBindings}, nil
		}

		o.curInput = nil
	}
}

func (o *UnwindOperator) Close(ctx *ExecutionContext) error {
	return o.Input.Close(ctx)
}

// UnionOperator combines results from two query segments.
type UnionOperator struct {
	Left  PhysicalOperator
	Right PhysicalOperator
	All   bool // If false, deduplicate

	seen     map[string]bool
	leftDone bool
}

func (o *UnionOperator) Open(ctx *ExecutionContext) error {
	o.leftDone = false
	if !o.All {
		o.seen = make(map[string]bool)
	}
	if err := o.Left.Open(ctx); err != nil {
		return err
	}
	return o.Right.Open(ctx)
}

func (o *UnionOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	for {
		var row *BindingSet
		var err error

		if !o.leftDone {
			row, err = o.Left.Next(ctx)
			if err != nil {
				return nil, err
			}
			if row == nil {
				o.leftDone = true
				continue
			}
		} else {
			row, err = o.Right.Next(ctx)
			if err != nil || row == nil {
				return row, err
			}
		}

		if !o.All {
			// Deduplicate based on all bindings
			// We use a simplified key for the spike
			key := fmt.Sprintf("%v", row.bindings)
			if o.seen[key] {
				continue
			}
			o.seen[key] = true
		}

		return row, nil
	}
}

func (o *UnionOperator) Close(ctx *ExecutionContext) error {
	o.seen = nil
	_ = o.Left.Close(ctx)
	return o.Right.Close(ctx)
}

// AggregateOperator performs grouped aggregations.
type AggregateOperator struct {
	Input PhysicalOperator
	Items []*ReturnItem

	results []*BindingSet
	index   int
}

func (o *AggregateOperator) Open(ctx *ExecutionContext) error {
	if err := o.Input.Open(ctx); err != nil {
		return err
	}

	// Drain input and group
	var allInputs []*BindingSet
	for {
		binding, err := o.Input.Next(ctx)
		if err != nil {
			return err
		}
		if binding == nil {
			break
		}
		allInputs = append(allInputs, binding)
	}

	if len(allInputs) == 0 {
		// handle empty aggregation (e.g. COUNT(*) should return 0)
		// For simplicity in spike, return empty
		return nil
	}

	// Identify group-by items
	var groupBy []*PropertyExpression
	for _, item := range o.Items {
		if item.Aggregate == "" {
			groupBy = append(groupBy, item.Expression)
		}
	}

	computer := &AggregationComputer{}

	// Temporarily swap ctx.results for grouping logic
	origResults := ctx.results
	ctx.results = allInputs

	groupedResults := computer.ComputeGroupedAggregates(ctx, o.Items, groupBy)

	ctx.results = origResults

	// Convert map[string]any results back to BindingSets
	o.results = make([]*BindingSet, 0, len(groupedResults))
	for _, res := range groupedResults {
		o.results = append(o.results, &BindingSet{bindings: res})
	}
	o.index = 0
	return nil
}

func (o *AggregateOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	if o.index >= len(o.results) {
		return nil, nil
	}
	row := o.results[o.index]
	o.index++
	return row, nil
}

func (o *AggregateOperator) Close(ctx *ExecutionContext) error {
	o.results = nil
	return o.Input.Close(ctx)
}
