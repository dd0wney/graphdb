package query

import "context"

// The "$name" bindings of a parameterized run.
//
// They travel in the context of the run. Every segment of a WITH chain and
// every part of a UNION gets its rows through seedRows, which adds the
// bindings, so a parameter cannot be lost at a segment boundary. A WITH
// projection drops every variable it does not name, and a parameter is not a
// variable, so the rows themselves do not carry it.

type runParamsKey struct{}

// withRunParams returns ctx carrying params for seedRows. The map is read only.
func withRunParams(ctx context.Context, params map[string]any) context.Context {
	if len(params) == 0 {
		return ctx
	}
	return context.WithValue(ctx, runParamsKey{}, params)
}

// seedRows returns the rows that enter a segment. An initial of nil means the
// first segment of a query with nothing seeded: one empty row. Each row gets
// the "$name" binding of every parameter of the run. The rows in initial are
// not written: a caller may hold them, and the executor must not change a
// value that more than one run can see.
func seedRows(ctx context.Context, initial []*BindingSet) []*BindingSet {
	if initial == nil {
		initial = []*BindingSet{{bindings: make(map[string]any)}}
	}
	params, _ := ctx.Value(runParamsKey{}).(map[string]any)
	if len(params) == 0 {
		return initial
	}
	rows := make([]*BindingSet, len(initial))
	for i, row := range initial {
		if carriesParams(row, params) {
			rows[i] = row
			continue
		}
		bindings := make(map[string]any, len(row.bindings)+len(params))
		for k, v := range row.bindings {
			bindings[k] = v
		}
		for k, v := range params {
			bindings["$"+k] = v
		}
		rows[i] = &BindingSet{bindings: bindings, vectorScores: row.vectorScores}
	}
	return rows
}

// carriesParams reports whether row already holds every parameter of the run,
// which is the case for a row that seedRows made. A second pass over it then
// copies nothing.
func carriesParams(row *BindingSet, params map[string]any) bool {
	for k := range params {
		if _, ok := row.bindings["$"+k]; !ok {
			return false
		}
	}
	return true
}
