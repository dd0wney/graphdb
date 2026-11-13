package query

import (
	"errors"
	"fmt"
	"reflect"
)

// filterOrderViolation reports why the optimizer's output moved a FilterStep
// relative to the plan that buildExecutionPlan made, or nil when it did not.
//
// The invariant is about meaning, not about which optimization ran. A write
// step runs on every row that reaches it, so a FilterStep that ends up after
// the write no longer limits the write. That is how an index lookup made
// `MATCH (p) WHERE p.pid = 1 AND p.k > 5 DELETE p` delete a node that the
// WHERE rejects. Stating the rule over the whole plan means a new pass, or a
// new access step, cannot break it without this check seeing it.
//
// A filter may move above a row-neutral step (isRowNeutral) that came before
// it. It may not move across any other step in either direction.
//
// A step is the same step when it is the same pointer. Two exceptions let an
// optimizer swap an access path and still be checked:
//
//   - A step that came before the first filter may be replaced or dropped. The
//     MatchStep that becomes an IndexLookupStep is the case this exists for.
//   - A step that the optimizer added must sit before the first filter.
//
// A step that came after the first filter must survive by identity. If a pass
// replaces such a step, this check cannot tell whether the replacement kept its
// place, so it refuses. The refusal is safe: Optimize then runs the plan it was
// given.
func filterOrderViolation(before, after *ExecutionPlan) error {
	if before == nil {
		return nil
	}
	firstFilter := indexOfFirstFilter(before.Steps)
	if firstFilter < 0 {
		return nil
	}
	if after == nil {
		return errors.New("the optimizer returned no plan")
	}

	// Plans hold a handful of steps, so a linear scan beats a map here, and
	// Optimize runs this on every query.
	if err := checkComparable("optimized", after.Steps); err != nil {
		return err
	}
	if err := checkComparable("input", before.Steps); err != nil {
		return err
	}
	for i, s := range after.Steps {
		if indexOfStep(after.Steps[i+1:], s) >= 0 {
			return fmt.Errorf("step %T appears twice in the optimized plan", s)
		}
	}
	for i, s := range before.Steps {
		if i >= firstFilter && indexOfStep(after.Steps, s) < 0 {
			return fmt.Errorf("%T at step %d, at or after the first filter, is missing from the optimized plan", s, i)
		}
	}

	firstFilterOut := len(after.Steps)
	for i, s := range before.Steps {
		if _, ok := s.(*FilterStep); !ok {
			continue
		}
		q := indexOfStep(after.Steps, s)
		if q < firstFilterOut {
			firstFilterOut = q
		}
		for j, other := range before.Steps {
			p := indexOfStep(after.Steps, other)
			if j == i || p < 0 {
				continue
			}
			if j > i && p < q {
				return fmt.Errorf("%T came after the FilterStep in the input plan and is before it in the optimized plan", other)
			}
			if j < i && p > q && !isRowNeutral(other) {
				return fmt.Errorf("%T came before the FilterStep in the input plan and is after it in the optimized plan", other)
			}
		}
	}

	for i, s := range after.Steps {
		if i > firstFilterOut && indexOfStep(before.Steps, s) < 0 {
			return fmt.Errorf("the optimizer added %T after a FilterStep", s)
		}
	}
	return nil
}

// checkComparable refuses a plan whose steps cannot be compared with ==, which
// would panic. Every step type is a pointer today; this keeps a future value
// type from turning the guard into a crash.
func checkComparable(which string, steps []ExecutionStep) error {
	for i, s := range steps {
		if s == nil || !reflect.TypeOf(s).Comparable() {
			return fmt.Errorf("step %d of the %s plan (%T) cannot be matched by identity", i, which, s)
		}
	}
	return nil
}

func indexOfStep(steps []ExecutionStep, want ExecutionStep) int {
	for i, s := range steps {
		if s == want {
			return i
		}
	}
	return -1
}

// isRowNeutral reports whether a step leaves the rows and bindings as it found
// them, so that a FilterStep may swap places with it. ReturnStep.Execute does
// nothing: the projection runs after the last step. The answer defaults to no,
// because a step missing from this list only costs an optimization.
func isRowNeutral(s ExecutionStep) bool {
	_, ok := s.(*ReturnStep)
	return ok
}

func indexOfFirstFilter(steps []ExecutionStep) int {
	for i, s := range steps {
		if _, ok := s.(*FilterStep); ok {
			return i
		}
	}
	return -1
}
