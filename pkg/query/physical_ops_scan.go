package query

import (
	"fmt"

	"go.opentelemetry.io/otel"

	"github.com/dd0wney/graphdb/pkg/storage"
)

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
