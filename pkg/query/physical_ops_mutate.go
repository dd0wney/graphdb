package query

import (
	"fmt"

	"github.com/dd0wney/graphdb/pkg/storage"
)

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
