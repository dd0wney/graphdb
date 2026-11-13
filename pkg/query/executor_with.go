package query

import (
	"strings"
)

// checkWithAggregates refuses a WITH aggregate that cannot be bound or
// computed. It looks at the query text only, so the verdict is the same for
// zero rows as for a million.
func checkWithAggregates(items []*ReturnItem) error {
	aggregating := hasAggregates(items)
	for _, item := range items {
		if item.Aggregate == "" {
			// A key is bound under its alias, or under its variable when it is
			// a bare variable. Anything else has no name, so every row would
			// get a nil key and fall into one group.
			bare := item.Expression != nil && item.Expression.Property == ""
			if aggregating && item.Alias == "" && !bare {
				return refusef("WITH: a grouping key that is not a variable needs a name; add AS <name>")
			}
			continue
		}
		// An aggregate has no name of its own to bind, and WITH is the only
		// place its result is visible to the next segment.
		if item.Alias == "" {
			return refusef("WITH: the aggregate %s(...) needs a name for its result; add AS <name>", withAggName(item))
		}
		// The parser leaves Expression nil when the argument is not a property
		// or a variable (for example a literal). RETURN then counts rows and
		// collects nothing, which is a wrong answer, so WITH refuses.
		if item.Expression == nil {
			return refusef("WITH: %s(...) AS %s: the argument must be a variable or a property", withAggName(item), item.Alias)
		}
	}
	return nil
}

// checkWithChain checks the WITH of this segment and of each later segment.
// A later segment runs only after this one has run, and this one may have
// written, so a refusal found later would leave those writes behind.
func checkWithChain(query *Query) error {
	for q := query; q != nil && q.With != nil; q = q.Next {
		if err := checkWithAggregates(q.With.Items); err != nil {
			return err
		}
	}
	return nil
}

func withAggName(item *ReturnItem) string {
	return strings.ToLower(item.Aggregate)
}

// projectWith projects the bindings of a segment through the WITH items. With
// no aggregate each binding projects on its own. With one, the bindings group
// as they do under RETURN.
func (e *Executor) projectWith(execCtx *ExecutionContext, items []*ReturnItem) ([]*BindingSet, error) {
	computer := &AggregationComputer{}
	if !hasAggregates(items) {
		projected := make([]*BindingSet, 0, len(execCtx.results))
		for _, binding := range execCtx.results {
			b, err := e.projectWithBinding(binding, items, computer)
			if err != nil {
				return nil, err
			}
			projected = append(projected, b)
		}
		return projected, nil
	}

	if err := checkAggregateInputs(execCtx, items); err != nil {
		return nil, err
	}
	return e.groupedWithProjection(execCtx, items, computer)
}

// projectWithBinding applies the non-aggregate WITH items to one binding.
func (e *Executor) projectWithBinding(binding *BindingSet, items []*ReturnItem, computer *AggregationComputer) (*BindingSet, error) {
	newBinding := &BindingSet{bindings: make(map[string]any)}

	for _, item := range items {
		if item.Aggregate != "" {
			continue
		}
		alias := item.Alias
		if alias == "" && item.Expression != nil {
			alias = item.Expression.Variable
		}
		if alias == "" {
			continue
		}

		if item.ValueExpr != nil {
			v, err := evalOperand(item.ValueExpr, binding.bindings)
			if err != nil {
				return nil, err
			}
			newBinding.bindings[alias] = v
		} else if item.Expression != nil {
			if item.Expression.Property == "" {
				// Pass the whole node/variable through
				newBinding.bindings[alias] = binding.bindings[item.Expression.Variable]
			} else {
				newBinding.bindings[alias] = e.extractValueFromBinding(binding, item.Expression, computer)
			}
		}
	}
	return newBinding, nil
}

// groupedWithProjection makes one binding for each group of the segment's
// bindings. The non-aggregate items are the key, as they are under RETURN.
func (e *Executor) groupedWithProjection(execCtx *ExecutionContext, items []*ReturnItem, computer *AggregationComputer) ([]*BindingSet, error) {
	keyItems := nonAggregateItems(items)

	var groups []*bindingGroup
	if len(keyItems) == 0 {
		// Only aggregates: everything is one group, even when it is empty, as
		// RETURN count(n) over no rows gives one row holding 0.
		groups = []*bindingGroup{{members: execCtx.results}}
	} else {
		var err error
		groups, err = groupBindings(execCtx.results, func(binding *BindingSet) ([]any, error) {
			projected, err := e.projectWithBinding(binding, keyItems, computer)
			if err != nil {
				return nil, err
			}
			parts := make([]any, 0, len(keyItems))
			for _, item := range keyItems {
				parts = append(parts, projected.bindings[withKeyName(item)])
			}
			return parts, nil
		})
		if err != nil {
			return nil, err
		}
	}

	out := make([]*BindingSet, 0, len(groups))
	for _, g := range groups {
		newBinding := &BindingSet{bindings: make(map[string]any)}
		if g.first != nil {
			var err error
			newBinding, err = e.projectWithBinding(g.first, keyItems, computer)
			if err != nil {
				return nil, err
			}
		}
		for name, value := range computer.ComputeAggregates(execCtx.aggregationContext(g.members), items) {
			// count and the like give a Go int, but every integer that a
			// property or a literal yields is an int64, and compareOrdered
			// calls an int and an int64 equal. Without this, WITH ... WHERE
			// c > 1 would drop every group.
			if n, ok := value.(int); ok {
				value = int64(n)
			}
			newBinding.bindings[name] = value
		}
		out = append(out, newBinding)
	}
	return out, nil
}

// withKeyName is the name projectWithBinding binds a key item under.
func withKeyName(item *ReturnItem) string {
	if item.Alias == "" && item.Expression != nil {
		return item.Expression.Variable
	}
	return item.Alias
}
