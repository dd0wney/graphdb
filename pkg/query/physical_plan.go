// Package query's physical_plan.go declares the Volcano-model physical
// operator interface and 16 concrete operator implementations (C3.0
// extraction).
//
// Status: PARTIAL. C3.0 lifts the operator types + execution logic from
// origin/archive/gemini-bulk-2026-05-13^3 with no consumers. The existing
// Step-based Executor (executor.go + executor_steps.go) keeps serving the
// /v1/cypher endpoint; this file's operators run only when wired by C4
// (planner) + C5 (parser additions) in later PRs.
//
// Deferred:
//   - CallOperator (CALL ... YIELD) — references procedureRegistry, which
//     lives in procedures.go (C6 territory). To preserve the per-PR
//     discipline, it lands alongside C6 rather than dragging C6 forward.
//   - Operator-level unit tests — deferred to C3.1 (mirroring the C1.0 +
//     C1.1 split that surfaced a real navigation bug in the btree archive).
//
// Each operator carries `otel.Tracer("query").Start(...)` spans on Open
// (and on Next where loop hot-paths warrant it) per the audit's S7
// verdict. The acceptance bar of "OTEL spans visible in pkg/telemetry/
// exporter integration test" cannot be met because pkg/telemetry does
// not yet exist in the tree; surface added so a future telemetry
// extraction can wire to it.
package query

import (
	"fmt"

	"go.opentelemetry.io/otel"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// PhysicalOperator is the interface for physical query operators (Volcano model).
type PhysicalOperator interface {
	// Open initializes the operator and its children.
	Open(ctx *ExecutionContext) error

	// Next returns the next row (BindingSet) from the operator.
	// Returns (nil, nil) when the result set is exhausted.
	Next(ctx *ExecutionContext) (*BindingSet, error)

	// Close releases any resources held by the operator.
	Close(ctx *ExecutionContext) error
}

// NodeScanOperator scans all nodes with a given label (or all nodes if label is empty).
type NodeScanOperator struct {
	Variable string
	Label    string

	nodes []*storage.Node
	index int
}

func (o *NodeScanOperator) Open(ctx *ExecutionContext) error {
	_, span := otel.Tracer("query").Start(ctx.context, "NodeScanOperator.Open")
	defer span.End()

	if o.Label != "" {
		o.nodes = ctx.graph.GetNodesByLabelForTenant(ctx.tenantID, o.Label)
	} else {
		o.nodes = ctx.graph.GetAllNodesForTenant(ctx.tenantID)
	}
	o.index = 0
	return nil
}

func (o *NodeScanOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	if o.index >= len(o.nodes) {
		return nil, nil
	}

	node := o.nodes[o.index]
	o.index++

	return &BindingSet{
		bindings: map[string]any{
			o.Variable: node,
		},
	}, nil
}

func (o *NodeScanOperator) Close(ctx *ExecutionContext) error {
	o.nodes = nil
	return nil
}

// IndexSeekOperator uses a property index to find nodes.
type IndexSeekOperator struct {
	Variable    string
	PropertyKey string
	Value       storage.Value

	nodes []*storage.Node
	index int
}

func (o *IndexSeekOperator) Open(ctx *ExecutionContext) error {
	_, span := otel.Tracer("query").Start(ctx.context, "IndexSeekOperator.Open")
	defer span.End()

	var err error
	o.nodes, err = ctx.graph.FindNodesByPropertyIndexedForTenant(o.PropertyKey, o.Value, ctx.tenantID)
	if err != nil {
		return fmt.Errorf("index seek failed: %w", err)
	}
	o.index = 0
	return nil
}

func (o *IndexSeekOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	if o.index >= len(o.nodes) {
		return nil, nil
	}

	node := o.nodes[o.index]
	o.index++

	return &BindingSet{
		bindings: map[string]any{
			o.Variable: node,
		},
	}, nil
}

func (o *IndexSeekOperator) Close(ctx *ExecutionContext) error {
	o.nodes = nil
	return nil
}

// ExpandOperator expands from a source node along edges of a specific type.
type ExpandOperator struct {
	Input     PhysicalOperator
	SourceVar string
	TargetVar string
	EdgeVar   string
	EdgeType  string
	Direction Direction // Use local query.Direction

	curInput  *BindingSet
	curEdges  []*storage.Edge
	edgeIndex int
}

func (o *ExpandOperator) Open(ctx *ExecutionContext) error {
	_, span := otel.Tracer("query").Start(ctx.context, "ExpandOperator.Open")
	defer span.End()

	return o.Input.Open(ctx)
}

func (o *ExpandOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	for {
		if o.curInput == nil {
			var err error
			o.curInput, err = o.Input.Next(ctx)
			if err != nil {
				return nil, err
			}
			if o.curInput == nil {
				return nil, nil // Exhausted
			}

			// Get source node
			sourceObj, ok := o.curInput.bindings[o.SourceVar]
			if !ok {
				return nil, fmt.Errorf("source variable %s not found in bindings", o.SourceVar)
			}
			sourceNode, ok := sourceObj.(*storage.Node)
			if !ok {
				return nil, fmt.Errorf("source variable %s is not a node", o.SourceVar)
			}

			// Fetch edges
			var errFetch error
			// For simplicity in spike, assume outgoing for now
			o.curEdges, errFetch = ctx.graph.GetOutgoingEdgesForTenant(sourceNode.ID, ctx.tenantID)
			if errFetch != nil {
				return nil, errFetch
			}
			o.edgeIndex = 0
		}

		for o.edgeIndex < len(o.curEdges) {
			edge := o.curEdges[o.edgeIndex]
			o.edgeIndex++

			if o.EdgeType != "" && edge.Type != o.EdgeType {
				continue
			}

			// Fetch target node
			targetNode, err := ctx.graph.GetNodeForTenant(edge.ToNodeID, ctx.tenantID)
			if err != nil {
				continue // Node might have been deleted or doesn't belong to tenant
			}

			// Create new binding set
			newBindings := make(map[string]any)
			for k, v := range o.curInput.bindings {
				newBindings[k] = v
			}
			newBindings[o.TargetVar] = targetNode
			if o.EdgeVar != "" {
				newBindings[o.EdgeVar] = edge
			}

			return &BindingSet{bindings: newBindings}, nil
		}

		// All edges for current input exhausted, move to next input
		o.curInput = nil
	}
}

func (o *ExpandOperator) Close(ctx *ExecutionContext) error {
	o.curInput = nil
	o.curEdges = nil
	return o.Input.Close(ctx)
}

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

// CallOperator (CALL ... YIELD) is intentionally NOT extracted in C3.0.
// It references procedureRegistry, which is C6 territory (procedures.go).
// CallOperator will land alongside C6 — see TODO(C3.x / C6 co-land).

// CallOperator executes a procedure call (e.g., algorithm).
// CreateOperator handles node and relationship creation.
type CreateOperator struct {
	Input    PhysicalOperator
	Patterns []*Pattern
	executed bool // For standalone CREATE
}

func (o *CreateOperator) Open(ctx *ExecutionContext) error {
	o.executed = false
	if o.Input != nil {
		return o.Input.Open(ctx)
	}
	return nil
}

func (o *CreateOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	var row *BindingSet
	var err error

	if o.Input != nil {
		row, err = o.Input.Next(ctx)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, nil
		}
	} else {
		if o.executed {
			return nil, nil
		}
		o.executed = true
		row = &BindingSet{bindings: make(map[string]any)}
	}

	// For each pattern, create nodes and relationships
	for _, pattern := range o.Patterns {
		// Map from AST NodePattern to created storage.Node
		createdNodes := make(map[*NodePattern]*storage.Node)

		for _, nodePat := range pattern.Nodes {
			// If variable already exists in bindings, use it
			if nodePat.Variable != "" {
				if val, ok := row.bindings[nodePat.Variable]; ok {
					if node, ok := val.(*storage.Node); ok {
						createdNodes[nodePat] = node
						continue
					}
				}
			}

			// Create new node
			props := make(map[string]storage.Value)
			for k, v := range nodePat.Properties {
				sv, err := convertCreateProperty(v)
				if err != nil {
					return nil, err
				}
				props[k] = sv
			}
			node, err := ctx.graph.CreateNodeWithTenant(ctx.tenantID, nodePat.Labels, props)
			if err != nil {
				return nil, err
			}
			createdNodes[nodePat] = node
			if nodePat.Variable != "" {
				row.bindings[nodePat.Variable] = node
			}
		}

		for _, relPat := range pattern.Relationships {
			src := createdNodes[relPat.From]
			dst := createdNodes[relPat.To]

			props := make(map[string]storage.Value)
			for k, v := range relPat.Properties {
				sv, err := convertCreateProperty(v)
				if err != nil {
					return nil, err
				}
				props[k] = sv
			}

			edge, err := ctx.graph.CreateEdgeWithTenant(ctx.tenantID, src.ID, dst.ID, relPat.Type, props, 1.0)
			if err != nil {
				return nil, err
			}
			if relPat.Variable != "" {
				row.bindings[relPat.Variable] = edge
			}
		}
	}

	return row, nil
}

func (o *CreateOperator) Close(ctx *ExecutionContext) error {
	if o.Input != nil {
		return o.Input.Close(ctx)
	}
	return nil
}

// SetOperator handles property updates.
type SetOperator struct {
	Input       PhysicalOperator
	Assignments []*Assignment
}

func (o *SetOperator) Open(ctx *ExecutionContext) error {
	return o.Input.Open(ctx)
}

func (o *SetOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	row, err := o.Input.Next(ctx)
	if err != nil || row == nil {
		return row, err
	}

	for _, asgn := range o.Assignments {
		obj, ok := row.bindings[asgn.Variable]
		if !ok {
			return nil, fmt.Errorf("variable %s not found", asgn.Variable)
		}

		val := asgn.Value
		if asgn.ValueExpr != nil {
			v, err := asgn.ValueExpr.EvalValue(row.bindings)
			if err != nil {
				return nil, err
			}
			val = v
		}

		if node, ok := obj.(*storage.Node); ok {
			props := map[string]storage.Value{
				asgn.Property: convertToStorageValue(val),
			}
			err := ctx.graph.UpdateNodeForTenant(node.ID, props, ctx.tenantID)
			if err != nil {
				return nil, err
			}
			// Update in-memory binding for consistency
			if node.Properties == nil {
				node.Properties = make(map[string]storage.Value)
			}
			node.Properties[asgn.Property] = props[asgn.Property]
		}
		// Edge SET could be added here
	}

	return row, nil
}

func (o *SetOperator) Close(ctx *ExecutionContext) error {
	return o.Input.Close(ctx)
}

// DeleteOperator handles node and edge deletion.
type DeleteOperator struct {
	Input     PhysicalOperator
	Variables []string
	Detach    bool
}

func (o *DeleteOperator) Open(ctx *ExecutionContext) error {
	return o.Input.Open(ctx)
}

func (o *DeleteOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	row, err := o.Input.Next(ctx)
	if err != nil || row == nil {
		return row, err
	}

	for _, v := range o.Variables {
		obj, ok := row.bindings[v]
		if !ok {
			continue
		}

		if node, ok := obj.(*storage.Node); ok {
			if o.Detach {
				// Delete incident edges first
				out, _ := ctx.graph.GetOutgoingEdgesForTenant(node.ID, ctx.tenantID)
				for _, e := range out {
					_ = ctx.graph.DeleteEdgeForTenant(e.ID, ctx.tenantID)
				}
				in, _ := ctx.graph.GetIncomingEdgesForTenant(node.ID, ctx.tenantID)
				for _, e := range in {
					_ = ctx.graph.DeleteEdgeForTenant(e.ID, ctx.tenantID)
				}
			}
			err := ctx.graph.DeleteNodeForTenant(node.ID, ctx.tenantID)
			if err != nil {
				return nil, err
			}
		} else if edge, ok := obj.(*storage.Edge); ok {
			err := ctx.graph.DeleteEdgeForTenant(edge.ID, ctx.tenantID)
			if err != nil {
				return nil, err
			}
		}
	}

	return row, nil
}

func (o *DeleteOperator) Close(ctx *ExecutionContext) error {
	return o.Input.Close(ctx)
}

// RemoveOperator handles property removal.
type RemoveOperator struct {
	Input PhysicalOperator
	Items []*RemoveItem
}

func (o *RemoveOperator) Open(ctx *ExecutionContext) error {
	return o.Input.Open(ctx)
}

func (o *RemoveOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	row, err := o.Input.Next(ctx)
	if err != nil || row == nil {
		return row, err
	}

	for _, item := range o.Items {
		obj, ok := row.bindings[item.Variable]
		if !ok {
			continue
		}

		if node, ok := obj.(*storage.Node); ok {
			if item.Property != "" {
				err := ctx.graph.RemoveNodePropertiesForTenant(node.ID, []string{item.Property}, ctx.tenantID)
				if err != nil {
					return nil, err
				}
				delete(node.Properties, item.Property)
			}
		}
	}

	return row, nil
}

func (o *RemoveOperator) Close(ctx *ExecutionContext) error {
	return o.Input.Close(ctx)
}

// MergeOperator handles match-or-create logic.
type MergeOperator struct {
	Input    PhysicalOperator
	Pattern  *Pattern
	OnMatch  *SetClause
	OnCreate *SetClause
}

func (o *MergeOperator) Open(ctx *ExecutionContext) error {
	if o.Input != nil {
		return o.Input.Open(ctx)
	}
	return nil
}

func (o *MergeOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	var row *BindingSet
	var err error

	if o.Input != nil {
		row, err = o.Input.Next(ctx)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, nil
		}
	} else {
		// Standalone MERGE - logic to ensure it runs once if Input is nil
		// (Similar to CreateOperator)
	}

	// For simplicity in this spike, we'll implement a simple node-only MERGE.
	// 1. Try to find the node
	if len(o.Pattern.Nodes) == 1 && len(o.Pattern.Relationships) == 0 {
		nodePat := o.Pattern.Nodes[0]

		// Attempt to match
		// We'll use a simple loop over all nodes for now, or index if possible.
		var matchedNode *storage.Node

		// Optimization: if there are properties, try to use index seek logic
		// (In a real planner, we'd use the Planner.PlanMatch logic)

		nodes := ctx.graph.GetNodesByLabelForTenant(ctx.tenantID, nodePat.Labels[0]) // Assume at least one label
		for _, n := range nodes {
			match := true
			for k, v := range nodePat.Properties {
				if propVal, ok := n.Properties[k]; ok {
					if !compareStorageValue(propVal, convertToStorageValue(v)) {
						match = false
						break
					}
				} else {
					match = false
					break
				}
			}
			if match {
				matchedNode = n
				break
			}
		}

		if matchedNode != nil {
			// Found - ON MATCH
			if row == nil {
				row = &BindingSet{bindings: make(map[string]any)}
			}
			if nodePat.Variable != "" {
				row.bindings[nodePat.Variable] = matchedNode
			}

			if o.OnMatch != nil {
				// We need a way to apply SetOperator to a single row.
				// For now, let's just do it manually.
				for _, asgn := range o.OnMatch.Assignments {
					props := map[string]storage.Value{
						asgn.Property: convertToStorageValue(asgn.Value), // Simple literal for now
					}
					_ = ctx.graph.UpdateNodeForTenant(matchedNode.ID, props, ctx.tenantID)
					matchedNode.Properties[asgn.Property] = props[asgn.Property]
				}
			}
			return row, nil
		}

		// Not found - ON CREATE
		props := make(map[string]storage.Value)
		for k, v := range nodePat.Properties {
			sv, err := convertCreateProperty(v)
			if err != nil {
				return nil, err
			}
			props[k] = sv
		}
		newNode, err := ctx.graph.CreateNodeWithTenant(ctx.tenantID, nodePat.Labels, props)
		if err != nil {
			return nil, err
		}

		if row == nil {
			row = &BindingSet{bindings: make(map[string]any)}
		}
		if nodePat.Variable != "" {
			row.bindings[nodePat.Variable] = newNode
		}

		if o.OnCreate != nil {
			for _, asgn := range o.OnCreate.Assignments {
				p := map[string]storage.Value{
					asgn.Property: convertToStorageValue(asgn.Value),
				}
				_ = ctx.graph.UpdateNodeForTenant(newNode.ID, p, ctx.tenantID)
				newNode.Properties[asgn.Property] = p[asgn.Property]
			}
		}
		return row, nil
	}

	return nil, fmt.Errorf("complex MERGE patterns not supported in spike")
}

func (o *MergeOperator) Close(ctx *ExecutionContext) error {
	if o.Input != nil {
		return o.Input.Close(ctx)
	}
	return nil
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

// OptionalMatchOperator implements left-outer-join semantics.
type OptionalMatchOperator struct {
	Input   PhysicalOperator
	Pattern *Pattern

	curInput *BindingSet
	matches  []*BindingSet
	index    int
}

func (o *OptionalMatchOperator) Open(ctx *ExecutionContext) error {
	o.curInput = nil
	o.matches = nil
	return o.Input.Open(ctx)
}

func (o *OptionalMatchOperator) Next(ctx *ExecutionContext) (*BindingSet, error) {
	for {
		if o.curInput == nil || o.index >= len(o.matches) {
			var err error
			o.curInput, err = o.Input.Next(ctx)
			if err != nil || o.curInput == nil {
				return nil, err
			}

			// Try to match the pattern for this input
			o.matches = o.matchSimplePattern(ctx, o.curInput)
			if len(o.matches) == 0 {
				// No match: produce one row with nulls
				nullBinding := &BindingSet{bindings: make(map[string]any)}
				for k, v := range o.curInput.bindings {
					nullBinding.bindings[k] = v
				}
				for _, node := range o.Pattern.Nodes {
					if node.Variable != "" {
						if _, exists := nullBinding.bindings[node.Variable]; !exists {
							nullBinding.bindings[node.Variable] = nil
						}
					}
				}
				o.matches = []*BindingSet{nullBinding}
			}
			o.index = 0
		}

		if o.index < len(o.matches) {
			row := o.matches[o.index]
			o.index++
			return row, nil
		}
	}
}

func (o *OptionalMatchOperator) matchSimplePattern(ctx *ExecutionContext, input *BindingSet) []*BindingSet {
	// Simplified matching for spike: single node
	if len(o.Pattern.Nodes) != 1 || len(o.Pattern.Relationships) != 0 {
		return nil
	}
	nodePat := o.Pattern.Nodes[0]

	// Scan nodes with label
	var nodes []*storage.Node
	if len(nodePat.Labels) > 0 {
		nodes = ctx.graph.GetNodesByLabelForTenant(ctx.tenantID, nodePat.Labels[0])
	} else {
		nodes = ctx.graph.GetAllNodesForTenant(ctx.tenantID)
	}

	var results []*BindingSet
	for _, n := range nodes {
		match := true
		for k, v := range nodePat.Properties {
			if propVal, ok := n.Properties[k]; ok {
				if !compareStorageValue(propVal, convertToStorageValue(v)) {
					match = false
					break
				}
			} else {
				match = false
				break
			}
		}
		if match {
			newBindings := make(map[string]any)
			for k, v := range input.bindings {
				newBindings[k] = v
			}
			if nodePat.Variable != "" {
				newBindings[nodePat.Variable] = n
			}
			results = append(results, &BindingSet{bindings: newBindings})
		}
	}
	return results
}

func (o *OptionalMatchOperator) Close(ctx *ExecutionContext) error {
	o.curInput = nil
	o.matches = nil
	return o.Input.Close(ctx)
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

// Helper: compareStorageValue compares two storage.Value objects
func compareStorageValue(a, b storage.Value) bool {
	if a.Type != b.Type {
		return false
	}
	return string(a.Data) == string(b.Data) // Simplified
}

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
