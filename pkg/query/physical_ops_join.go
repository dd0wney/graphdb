package query

import (
	"fmt"
)

// NestedLoopJoinOperator performs a nested loop join (Cartesian product)
// of two input streams. It buffers the Right input in memory.
type NestedLoopJoinOperator struct {
	Left  PhysicalOperator
	Right PhysicalOperator

	leftRow      *BindingSet
	rightResults []*BindingSet
	rightIndex   int
	rightLoaded  bool
}

func (o *NestedLoopJoinOperator) Open(ctx *ExecutionContext) error {
	o.leftRow = nil
	o.rightResults = nil
	o.rightIndex = 0
	o.rightLoaded = false
	if err := o.Left.Open(ctx); err != nil {
		return err
	}
	return o.Right.Open(ctx)
}

func (o *NestedLoopJoinOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	// 1. Ensure Right side is buffered
	if !o.rightLoaded {
		for {
			row, err := o.Right.Next(ctx)
			if err != nil {
				return nil, err
			}
			if row == nil {
				break
			}
			o.rightResults = append(o.rightResults, row)
		}
		o.rightLoaded = true
		if len(o.rightResults) == 0 {
			return nil, nil // Right side empty, product is empty
		}
	}

	for {
		// 2. Need a new Left row?
		if o.leftRow == nil {
			var err error
			o.leftRow, err = o.Left.Next(ctx)
			if err != nil {
				return nil, err
			}
			if o.leftRow == nil {
				return nil, nil // Left side exhausted
			}
			o.rightIndex = 0
		}

		// 3. Yield next combination
		if o.rightIndex < len(o.rightResults) {
			rightRow := o.rightResults[o.rightIndex]
			o.rightIndex++

			// Merge bindings
			newBindings := make(map[string]any, len(o.leftRow.bindings)+len(rightRow.bindings))
			for k, v := range o.leftRow.bindings {
				newBindings[k] = v
			}
			for k, v := range rightRow.bindings {
				// Cypher semantics: if variables overlap, they must match.
				// However, standard Cartesian product just merges.
				// Overlap handling is usually done via subsequent Filter or specialized Joins.
				newBindings[k] = v
			}
			return &BindingSet{bindings: newBindings}, nil
		}

		// 4. Right side exhausted for this Left row, reset
		o.leftRow = nil
	}
}

func (o *NestedLoopJoinOperator) Close(ctx *ExecutionContext) error {
	o.rightResults = nil
	_ = o.Left.Close(ctx)
	return o.Right.Close(ctx)
}

// HashJoinOperator performs an efficient equijoin using an in-memory hash table.
// Build phase: buffers Right input into a hash map.
// Probe phase: streams Left input and probes the map.
type HashJoinOperator struct {
	Left  PhysicalOperator
	Right PhysicalOperator
	Var   string // The common variable to join on

	buildMap    map[string][]*BindingSet
	leftRow     *BindingSet
	matchBuffer []*BindingSet
	matchIndex  int
	built       bool
}

func (o *HashJoinOperator) Open(ctx *ExecutionContext) error {
	o.buildMap = make(map[string][]*BindingSet)
	o.leftRow = nil
	o.matchBuffer = nil
	o.matchIndex = 0
	o.built = false
	if err := o.Left.Open(ctx); err != nil {
		return err
	}
	return o.Right.Open(ctx)
}

func (o *HashJoinOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	// 1. Build Phase: Read all from Right and hash by o.Var
	if !o.built {
		for {
			row, err := o.Right.Next(ctx)
			if err != nil {
				return nil, err
			}
			if row == nil {
				break
			}
			val, ok := row.bindings[o.Var]
			if !ok || val == nil {
				continue // Cannot join on null/missing
			}
			key := fmt.Sprintf("%v", val)
			o.buildMap[key] = append(o.buildMap[key], row)
		}
		o.built = true
		if len(o.buildMap) == 0 {
			return nil, nil // Right side had no joinable rows
		}
	}

	for {
		// 2. Probing Phase: If we have buffered matches from a previous Left row, yield them
		if o.matchIndex < len(o.matchBuffer) {
			rightMatch := o.matchBuffer[o.matchIndex]
			o.matchIndex++

			// Merge bindings
			newBindings := make(map[string]any, len(o.leftRow.bindings)+len(rightMatch.bindings))
			for k, v := range o.leftRow.bindings {
				newBindings[k] = v
			}
			for k, v := range rightMatch.bindings {
				newBindings[k] = v
			}
			return &BindingSet{bindings: newBindings}, nil
		}

		// 3. Need a new Left row to probe with
		var err error
		o.leftRow, err = o.Left.Next(ctx)
		if err != nil {
			return nil, err
		}
		if o.leftRow == nil {
			return nil, nil // Left side exhausted
		}

		val, ok := o.leftRow.bindings[o.Var]
		if !ok || val == nil {
			o.matchBuffer = nil
			continue
		}

		key := fmt.Sprintf("%v", val)
		o.matchBuffer = o.buildMap[key]
		o.matchIndex = 0
		// Loop will continue and yield first match if any
	}
}

func (o *HashJoinOperator) Close(ctx *ExecutionContext) error {
	o.buildMap = nil
	o.matchBuffer = nil
	_ = o.Left.Close(ctx)
	return o.Right.Close(ctx)
}
