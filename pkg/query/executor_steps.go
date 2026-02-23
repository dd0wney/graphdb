package query

import (
	"fmt"
	"log"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// FilterStep executes a WHERE clause
type FilterStep struct {
	where      *WhereClause
	strictMode bool // When true, return first error; when false, log and continue
}

func (fs *FilterStep) Execute(ctx *ExecutionContext) error {
	filtered := make([]*BindingSet, 0)
	var evalErrors []error

	for i, binding := range ctx.results {
		// Check for cancellation periodically (every 1000 rows for large result sets)
		if i > 0 && i%1000 == 0 {
			if err := ctx.CheckCancellation(); err != nil {
				return fmt.Errorf("filter cancelled after processing %d rows: %w", i, err)
			}
		}

		// Evaluate expression with this binding
		match, err := fs.where.Expression.Eval(binding.bindings)
		if err != nil {
			if fs.strictMode {
				return fmt.Errorf("filter evaluation failed at row %d: %w", i, err)
			}
			// Track error but continue (lenient mode)
			evalErrors = append(evalErrors, fmt.Errorf("row %d: %w", i, err))
			continue
		}

		if match {
			filtered = append(filtered, binding)
		}
	}

	ctx.results = filtered

	// Log warning if errors occurred in lenient mode
	if len(evalErrors) > 0 {
		log.Printf("WARNING: %d filter evaluation errors occurred and were skipped", len(evalErrors))
	}

	return nil
}

// convertToStorageValue converts a generic any value to storage.Value
func convertToStorageValue(val any) storage.Value {
	switch v := val.(type) {
	case string:
		return storage.StringValue(v)
	case int64:
		return storage.IntValue(v)
	case float64:
		return storage.FloatValue(v)
	case bool:
		return storage.BoolValue(v)
	default:
		return storage.StringValue(fmt.Sprintf("%v", v))
	}
}

// IndexLookupStep uses a property index for efficient node lookup
// This replaces a full scan when the optimizer detects an indexable equality condition
type IndexLookupStep struct {
	propertyKey string        // The indexed property key
	value       storage.Value // The value to lookup
	variable    string        // Variable name to bind results to
	labels      []string      // Optional label filters to apply
}

func (ils *IndexLookupStep) Execute(ctx *ExecutionContext) error {
	// Use index for O(1) lookup
	nodes, err := ctx.graph.FindNodesByPropertyIndexed(ils.propertyKey, ils.value)
	if err != nil {
		// Index lookup failed, this shouldn't happen if optimizer did its job
		return fmt.Errorf("index lookup failed: %w", err)
	}

	newResults := make([]*BindingSet, 0, len(nodes))

	for _, node := range nodes {
		// Apply label filter if specified
		if len(ils.labels) > 0 {
			hasAllLabels := true
			for _, requiredLabel := range ils.labels {
				found := false
				for _, nodeLabel := range node.Labels {
					if nodeLabel == requiredLabel {
						found = true
						break
					}
				}
				if !found {
					hasAllLabels = false
					break
				}
			}
			if !hasAllLabels {
				continue
			}
		}

		// Create binding for this node
		newBinding := &BindingSet{bindings: make(map[string]any)}
		if ils.variable != "" {
			newBinding.bindings[ils.variable] = node
		}
		newResults = append(newResults, newBinding)
	}

	ctx.results = newResults
	return nil
}

// CreateStep executes a CREATE clause
type CreateStep struct {
	create *CreateClause
}

func (cs *CreateStep) Execute(ctx *ExecutionContext) error {
	for _, pattern := range cs.create.Patterns {
		// Create nodes first
		if err := cs.createNodes(ctx, pattern); err != nil {
			return err
		}

		// Create relationships
		if err := cs.createRelationships(ctx, pattern); err != nil {
			return err
		}
	}

	return nil
}

// createNodes creates nodes for a pattern
func (cs *CreateStep) createNodes(ctx *ExecutionContext, pattern *Pattern) error {
	for _, nodePattern := range pattern.Nodes {
		// Convert properties
		props := make(map[string]storage.Value)
		for key, val := range nodePattern.Properties {
			props[key] = convertToStorageValue(val)
		}

		// Create node
		node, err := ctx.graph.CreateNode(nodePattern.Labels, props)
		if err != nil {
			return err
		}

		// Bind variable
		if nodePattern.Variable != "" {
			for _, binding := range ctx.results {
				binding.bindings[nodePattern.Variable] = node
			}
		}
	}
	return nil
}

// createRelationships creates relationships for a pattern
func (cs *CreateStep) createRelationships(ctx *ExecutionContext, pattern *Pattern) error {
	for _, relPattern := range pattern.Relationships {
		// Safe type assertions with validation
		if len(ctx.results) == 0 {
			return fmt.Errorf("no bindings available for relationship creation")
		}

		fromNode, err := cs.getNodeFromBinding(ctx, relPattern.From.Variable, "from")
		if err != nil {
			return err
		}

		toNode, err := cs.getNodeFromBinding(ctx, relPattern.To.Variable, "to")
		if err != nil {
			return err
		}

		props := make(map[string]storage.Value)
		for key, val := range relPattern.Properties {
			props[key] = convertToStorageValue(val)
		}

		_, err = ctx.graph.CreateEdge(fromNode.ID, toNode.ID, relPattern.Type, props, 1.0)
		if err != nil {
			return err
		}
	}
	return nil
}

// getNodeFromBinding extracts a node from bindings with proper error handling
func (cs *CreateStep) getNodeFromBinding(ctx *ExecutionContext, variable, role string) (*storage.Node, error) {
	nodeInterface, exists := ctx.results[0].bindings[variable]
	if !exists {
		return nil, fmt.Errorf("%s node variable '%s' not bound", role, variable)
	}
	node, ok := nodeInterface.(*storage.Node)
	if !ok {
		return nil, fmt.Errorf("%s node variable '%s' is not a Node", role, variable)
	}
	return node, nil
}

// SetStep executes a SET clause
type SetStep struct {
	set *SetClause
}

func (ss *SetStep) Execute(ctx *ExecutionContext) error {
	for _, binding := range ctx.results {
		for _, assignment := range ss.set.Assignments {
			if err := ss.executeAssignment(ctx, binding, assignment); err != nil {
				return err
			}
		}
	}

	return nil
}

// executeAssignment executes a single property assignment
func (ss *SetStep) executeAssignment(ctx *ExecutionContext, binding *BindingSet, assignment *Assignment) error {
	// Get node from binding
	obj, ok := binding.bindings[assignment.Variable]
	if !ok {
		return nil // Variable not bound, skip
	}

	node, ok := obj.(*storage.Node)
	if !ok {
		return nil // Not a node, skip
	}

	// Create updated properties map
	updatedProps := make(map[string]storage.Value)
	for k, v := range node.Properties {
		updatedProps[k] = v
	}
	updatedProps[assignment.Property] = convertToStorageValue(assignment.Value)

	// Update in storage
	if err := ctx.graph.UpdateNode(node.ID, updatedProps); err != nil {
		return fmt.Errorf("failed to update node %d: %w", node.ID, err)
	}

	return nil
}

// DeleteStep executes a DELETE clause
type DeleteStep struct {
	delete *DeleteClause
}

func (ds *DeleteStep) Execute(ctx *ExecutionContext) error {
	for _, binding := range ctx.results {
		for _, variable := range ds.delete.Variables {
			if err := ds.deleteVariable(ctx, binding, variable); err != nil {
				return err
			}
		}
	}

	return nil
}

// deleteVariable deletes a single variable from bindings
func (ds *DeleteStep) deleteVariable(ctx *ExecutionContext, binding *BindingSet, variable string) error {
	obj, ok := binding.bindings[variable]
	if !ok {
		return nil // Variable not bound, skip
	}

	node, ok := obj.(*storage.Node)
	if !ok {
		return nil // Not a node, skip
	}

	// Delete node (DeleteNode automatically handles edge deletion)
	if err := ctx.graph.DeleteNode(node.ID); err != nil {
		return fmt.Errorf("failed to delete node %d: %w", node.ID, err)
	}

	return nil
}

// MergeStep executes a MERGE clause (match-or-create)
type MergeStep struct {
	merge *MergeClause
}

func (ms *MergeStep) Execute(ctx *ExecutionContext) error {
	// Try to match the pattern
	matchStep := &MatchStep{match: &MatchClause{Patterns: []*Pattern{ms.merge.Pattern}}}

	// Save current results, try matching
	savedResults := ctx.results
	matchCtx := &ExecutionContext{
		context:  ctx.context,
		graph:    ctx.graph,
		bindings: make(map[string]any),
		results: []*BindingSet{
			{bindings: make(map[string]any)},
		},
	}

	if err := matchStep.Execute(matchCtx); err != nil {
		return err
	}

	if len(matchCtx.results) > 0 {
		// Found — apply ON MATCH SET if present
		ctx.results = matchCtx.results
		if ms.merge.OnMatch != nil {
			setStep := &SetStep{set: ms.merge.OnMatch}
			if err := setStep.Execute(ctx); err != nil {
				return err
			}
		}
	} else {
		// Not found — create via pattern
		ctx.results = savedResults
		createStep := &CreateStep{create: &CreateClause{Patterns: []*Pattern{ms.merge.Pattern}}}
		if err := createStep.Execute(ctx); err != nil {
			return err
		}

		// Apply ON CREATE SET if present
		if ms.merge.OnCreate != nil {
			setStep := &SetStep{set: ms.merge.OnCreate}
			if err := setStep.Execute(ctx); err != nil {
				return err
			}
		}
	}

	return nil
}

func (ms *MergeStep) StepName() string   { return "MergeStep" }
func (ms *MergeStep) StepDetail() string { return "match-or-create" }

// UnwindStep executes an UNWIND clause - expands list values into individual bindings
type UnwindStep struct {
	unwind *UnwindClause
}

func (us *UnwindStep) Execute(ctx *ExecutionContext) error {
	newResults := make([]*BindingSet, 0)

	for _, binding := range ctx.results {
		// Extract the value to unwind
		var val any
		expr := us.unwind.Expression
		if expr.Property == "" {
			// Variable reference without property — get the raw binding value
			val = binding.bindings[expr.Variable]
		} else {
			val = extractValue(expr, binding.bindings)
		}
		if val == nil {
			continue // Skip nil values
		}

		// Convert to list
		var items []any
		switch v := val.(type) {
		case []any:
			items = v
		default:
			// Non-list values treated as single-element list
			items = []any{v}
		}

		// Create one new binding per element
		for _, item := range items {
			newBinding := &BindingSet{bindings: make(map[string]any, len(binding.bindings)+1)}
			for k, v := range binding.bindings {
				newBinding.bindings[k] = v
			}
			newBinding.bindings[us.unwind.Alias] = item
			newResults = append(newResults, newBinding)
		}
	}

	ctx.results = newResults
	return nil
}

func (us *UnwindStep) StepName() string { return "UnwindStep" }
func (us *UnwindStep) StepDetail() string {
	return fmt.Sprintf("alias=%s", us.unwind.Alias)
}

// ReturnStep executes a RETURN clause
type ReturnStep struct {
	returnClause *ReturnClause
	limit        int
	skip         int
}

func (rs *ReturnStep) Execute(ctx *ExecutionContext) error {
	// SKIP and LIMIT are applied in buildResultSet, not here
	// This prevents double-applying pagination
	return nil
}
