package query

import (
	"fmt"
	"log"

	"github.com/dd0wney/graphdb/pkg/storage"
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
		// nil, lists and maps: the converter REST and GraphQL share, so a
		// Cypher write stores what a REST write stores. This used to be
		// fmt.Sprintf("%v"), which stored null as "<nil>" and a list as
		// "[a b]" with no error. The scalar cases above stay Cypher's own:
		// ValueFromJSON would turn a whole float such as 2.0 into an int.
		return storage.ValueFromJSON(v)
	}
}

// convertCreateProperty converts a CREATE/MERGE property value to a
// storage.Value, failing loud on an unresolved *ParameterRef (#237). Such a
// value means the query ran without parameter substitution (Execute /
// ExecuteWithContext instead of ExecuteWithParams[Context]); previously it was
// %v-stringified and the literal "&{name}" was persisted — silent corruption.
func convertCreateProperty(val any) (storage.Value, error) {
	if ref, ok := val.(*ParameterRef); ok {
		return storage.Value{}, fmt.Errorf("unresolved query parameter $%s: parameterized queries must run via ExecuteWithParams", ref.Name)
	}
	return convertToStorageValue(val), nil
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
	// Use index for O(1) lookup, scoped to caller's tenant.
	// Audit A6c-query (2026-05-08).
	nodes, err := ctx.graph.FindNodesByPropertyIndexedForTenant(ils.propertyKey, ils.value, ctx.tenantID)
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
	// CREATE acts once for each row, and with no rows it creates nothing.
	for _, row := range ctx.results {
		for _, pattern := range cs.create.Patterns {
			if err := cs.createPattern(ctx, pattern, row); err != nil {
				return err
			}
		}
	}
	return nil
}

// createPattern creates one pattern for one row. A node whose variable the row
// already binds is reused, never created again; the new nodes and the new
// relationship are bound into the row so later clauses see them.
func (cs *CreateStep) createPattern(ctx *ExecutionContext, pattern *Pattern, row *BindingSet) error {
	// Pattern nodes without a variable are found again by pointer: the
	// relationship's From and To are the same *NodePattern values.
	nodes := make(map[*NodePattern]*storage.Node, len(pattern.Nodes))
	for _, np := range pattern.Nodes {
		node, err := cs.bindOrCreateNode(ctx, np, row)
		if err != nil {
			return err
		}
		nodes[np] = node
	}

	for _, rel := range pattern.Relationships {
		from, err := createEndpoint(nodes, rel.From, row)
		if err != nil {
			return err
		}
		to, err := createEndpoint(nodes, rel.To, row)
		if err != nil {
			return err
		}

		props := make(map[string]storage.Value)
		for key, val := range rel.Properties {
			sv, err := convertCreateProperty(val)
			if err != nil {
				return err
			}
			props[key] = sv
		}

		// Audit A6c-query: tenant-scoped edge create. Uses
		// CreateEdgeWithTenant which is tenant-strict on from/to
		// node verification (A6a follow-up #20), so a Cypher CREATE
		// referencing a foreign-tenant node ID surfaces
		// ErrNodeNotFound.
		edge, err := ctx.graph.CreateEdgeWithTenant(ctx.tenantID, from.ID, to.ID, rel.Type, props, 1.0)
		if err != nil {
			return err
		}
		if rel.Variable != "" {
			row.bindings[rel.Variable] = edge
		}
	}
	return nil
}

// createEndpoint resolves a relationship endpoint: the node the pattern just
// bound or created, or, for an endpoint that is not one of the pattern's
// nodes (a hand-built AST), the node the row binds to its variable.
func createEndpoint(nodes map[*NodePattern]*storage.Node, np *NodePattern, row *BindingSet) (*storage.Node, error) {
	if np == nil {
		return nil, fmt.Errorf("CREATE: relationship has no endpoint")
	}
	if node := nodes[np]; node != nil {
		return node, nil
	}
	if node, ok := row.bindings[np.Variable].(*storage.Node); ok && node != nil {
		return node, nil
	}
	return nil, fmt.Errorf("CREATE: relationship endpoint %q is not bound to a node", np.Variable)
}

// bindOrCreateNode returns the node the row binds to np's variable, or
// creates one. openCypher refuses labels or properties on a bound variable,
// because they would describe a node the query already has.
func (cs *CreateStep) bindOrCreateNode(ctx *ExecutionContext, np *NodePattern, row *BindingSet) (*storage.Node, error) {
	if np.Variable != "" {
		if existing, ok := row.bindings[np.Variable]; ok {
			node, isNode := existing.(*storage.Node)
			if !isNode || node == nil {
				return nil, fmt.Errorf("CREATE: variable %s is not bound to a node", np.Variable)
			}
			if len(np.Labels) > 0 || len(np.Properties) > 0 {
				return nil, fmt.Errorf("CREATE: variable %s is already bound; it cannot take labels or properties here", np.Variable)
			}
			return node, nil
		}
	}

	props := make(map[string]storage.Value)
	for key, val := range np.Properties {
		sv, err := convertCreateProperty(val)
		if err != nil {
			return nil, err
		}
		props[key] = sv
	}

	// Audit A6c-query: tenant-scoped node create.
	node, err := ctx.graph.CreateNodeWithTenant(ctx.tenantID, np.Labels, props)
	if err != nil {
		return nil, err
	}
	if np.Variable != "" {
		row.bindings[np.Variable] = node
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
	target, err := resolvePropertyTarget(binding, "SET", assignment.Variable)
	if err != nil || target == nil {
		return err
	}

	// Resolve the value: expression RHS takes precedence over literal. An
	// evaluation error refuses the query: extractValue would turn it into
	// nil, and nil removes the property, so a failed expression would
	// delete data.
	val := assignment.Value
	if assignment.ValueExpr != nil {
		v, err := assignment.ValueExpr.EvalValue(binding.bindings)
		if err != nil {
			return fmt.Errorf("SET %s.%s: %w", assignment.Variable, assignment.Property, err)
		}
		val = v
	}

	// SET n.x = null removes the property (Cypher semantics, and the rule
	// PUT follows since #634).
	if val == nil {
		return target.removeProperty(ctx, assignment.Property)
	}
	return target.setProperty(ctx, assignment.Property, convertToStorageValue(val))
}

// RemoveStep executes a REMOVE clause — removes properties from nodes and edges
type RemoveStep struct {
	remove *RemoveClause
}

func (rs *RemoveStep) Execute(ctx *ExecutionContext) error {
	for _, binding := range ctx.results {
		for _, item := range rs.remove.Items {
			if err := rs.removeProperty(ctx, binding, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func (rs *RemoveStep) removeProperty(ctx *ExecutionContext, binding *BindingSet, item *RemoveItem) error {
	target, err := resolvePropertyTarget(binding, "REMOVE", item.Variable)
	if err != nil || target == nil {
		return err
	}
	return target.removeProperty(ctx, item.Property)
}

func (rs *RemoveStep) StepName() string   { return "RemoveStep" }
func (rs *RemoveStep) StepDetail() string { return fmt.Sprintf("items=%d", len(rs.remove.Items)) }

// DeleteStep executes a DELETE clause
type DeleteStep struct {
	delete *DeleteClause
}

// Execute collects every node and edge the rows bind before deleting any of
// them. One entity can appear in many rows, and a node delete cascades to its
// edges, so deleting row by row either deletes twice or deletes an edge its
// node already took with it; both surface as a spurious "not found" after the
// delete has happened. Edges go first for the same reason.
func (ds *DeleteStep) Execute(ctx *ExecutionContext) error {
	targets := newDeleteTargets()
	for _, binding := range ctx.results {
		for _, variable := range ds.delete.Variables {
			if err := targets.add(binding, variable); err != nil {
				return err
			}
		}
	}

	// Audit A6c-query: the *ForTenant deletes refuse an entity of another
	// tenant, so a row can never delete outside the caller's tenant.
	for _, id := range targets.edgeIDs {
		if err := ctx.graph.DeleteEdgeForTenant(id, ctx.tenantID); err != nil {
			return fmt.Errorf("failed to delete edge %d: %w", id, err)
		}
	}
	if !ds.delete.Detach {
		if err := ds.noteDetachedRelationships(ctx, targets.nodeIDs); err != nil {
			return err
		}
	}
	for _, id := range targets.nodeIDs {
		if err := ctx.graph.DeleteNodeForTenant(id, ctx.tenantID); err != nil {
			return fmt.Errorf("failed to delete node %d: %w", id, err)
		}
	}
	return nil
}

// noteDetachedRelationships records a deprecation notice when a plain DELETE
// is about to remove relationships the query did not name. openCypher refuses
// such a delete unless it says DETACH DELETE; graphdb 1.x removes them, and
// docs/STABILITY_POLICY.md keeps that until v2.0, which will refuse. It runs
// after the named relationships are gone, so whatever a node still has is
// exactly what the node delete will remove with it.
func (ds *DeleteStep) noteDetachedRelationships(ctx *ExecutionContext, nodeIDs []uint64) error {
	// One set across all nodes: a relationship between two deleted nodes is
	// removed once, not once for each end.
	detached := make(map[uint64]struct{})
	nodes := 0
	for _, id := range nodeIDs {
		out, err := ctx.graph.GetOutgoingEdgesForTenant(id, ctx.tenantID)
		if err != nil {
			return fmt.Errorf("DELETE: relationships of node %d: %w", id, err)
		}
		in, err := ctx.graph.GetIncomingEdgesForTenant(id, ctx.tenantID)
		if err != nil {
			return fmt.Errorf("DELETE: relationships of node %d: %w", id, err)
		}
		if len(out)+len(in) > 0 {
			nodes++
		}
		for _, e := range append(out, in...) {
			detached[e.ID] = struct{}{} // a self-loop is in both lists
		}
	}
	rels := len(detached)
	if nodes > 0 {
		ctx.notices = append(ctx.notices, Notice{
			Code: NoticePlainDeleteDetach,
			Message: fmt.Sprintf("deprecated: plain DELETE removed %d relationships of %d nodes; use DETACH DELETE. "+
				"graphdb v2.0 will refuse a plain DELETE of a node that has relationships", rels, nodes),
		})
	}
	return nil
}

// deleteTargets holds the distinct nodes and edges a DELETE names, in the
// order they were first bound.
type deleteTargets struct {
	nodeIDs, edgeIDs []uint64
	seenNodes        map[uint64]struct{}
	seenEdges        map[uint64]struct{}
}

func newDeleteTargets() *deleteTargets {
	return &deleteTargets{seenNodes: map[uint64]struct{}{}, seenEdges: map[uint64]struct{}{}}
}

// add records what variable is bound to in binding. Anything it cannot delete
// is refused rather than skipped: a skipped DELETE still reports its rows as
// affected, which is how DELETE r on an edge used to change nothing.
func (dt *deleteTargets) add(binding *BindingSet, variable string) error {
	obj, ok := binding.bindings[variable]
	if !ok {
		return fmt.Errorf("DELETE %s: variable is not defined by the query", variable)
	}

	switch v := obj.(type) {
	case nil:
		return nil // OPTIONAL MATCH found nothing; deleting null is a no-op
	case *storage.Node:
		if _, seen := dt.seenNodes[v.ID]; !seen {
			dt.seenNodes[v.ID] = struct{}{}
			dt.nodeIDs = append(dt.nodeIDs, v.ID)
		}
	case *storage.Edge:
		if _, seen := dt.seenEdges[v.ID]; !seen {
			dt.seenEdges[v.ID] = struct{}{}
			dt.edgeIDs = append(dt.edgeIDs, v.ID)
		}
	case []*storage.Edge:
		return fmt.Errorf("DELETE %s: deleting a variable-length relationship is not supported; match each relationship with a one-hop pattern and delete that", variable)
	default:
		return fmt.Errorf("DELETE %s: cannot delete a value of type %T", variable, obj)
	}
	return nil
}

// MergeStep executes a MERGE clause (match-or-create)
type MergeStep struct {
	merge *MergeClause
}

// Execute decides match-or-create for each row on its own, from that row's
// bindings. It used to match with an empty binding, ignoring every variable
// the row bound, and to decide for all rows at once: one matching row dropped
// the rows that had no match, and created nothing for them.
func (ms *MergeStep) Execute(ctx *ExecutionContext) error {
	matchStep := &MatchStep{match: &MatchClause{Patterns: []*Pattern{ms.merge.Pattern}}}
	createStep := &CreateStep{create: &CreateClause{Patterns: []*Pattern{ms.merge.Pattern}}}

	out := make([]*BindingSet, 0, len(ctx.results))
	for _, row := range ctx.results {
		// Inherit tenantID from parent ctx (audit A6c-query) — the
		// sub-context must scope to the same tenant.
		rowCtx := ctx.subContext()
		rowCtx.results = []*BindingSet{row}
		if err := matchStep.Execute(rowCtx); err != nil {
			return err
		}
		// The match half's truncation belongs to the caller. Dropping it made
		// a MERGE whose match stopped at an engine limit report a complete
		// answer.
		if rowCtx.truncation != nil {
			ctx.noteTruncation(rowCtx.truncation)
		}

		set := ms.merge.OnMatch
		if len(rowCtx.results) == 0 {
			rowCtx.results = []*BindingSet{row}
			if err := createStep.Execute(rowCtx); err != nil {
				return err
			}
			set = ms.merge.OnCreate
		}
		if set != nil {
			if err := (&SetStep{set: set}).Execute(rowCtx); err != nil {
				return err
			}
		}
		out = append(out, rowCtx.results...)
	}
	ctx.results = out
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

// OptionalMatchStep executes an OPTIONAL MATCH clause with left-outer-join semantics.
// When no match is found, variables from the pattern are set to nil (null propagation).
type OptionalMatchStep struct {
	match *MatchClause
	where *WhereClause
}

func (oms *OptionalMatchStep) Execute(ctx *ExecutionContext) error {
	matchHelper := &MatchStep{match: oms.match}
	newResults := make([]*BindingSet, 0)

	for _, binding := range ctx.results {
		matches := make([]*BindingSet, 0)

		for _, pattern := range oms.match.Patterns {
			patternMatches, err := matchHelper.matchPattern(ctx, pattern, binding)
			if err != nil {
				return err
			}
			matches = append(matches, patternMatches...)
		}

		// Apply scoped WHERE filter if present
		if oms.where != nil && len(matches) > 0 {
			filtered := make([]*BindingSet, 0)
			for _, r := range matches {
				match, err := oms.where.Expression.Eval(r.bindings)
				if err != nil {
					continue
				}
				if match {
					filtered = append(filtered, r)
				}
			}
			matches = filtered
		}

		if len(matches) > 0 {
			newResults = append(newResults, matches...)
		} else {
			// No match: create null bindings for all new variables in the pattern
			nullBinding := &BindingSet{bindings: make(map[string]any, len(binding.bindings))}
			for k, v := range binding.bindings {
				nullBinding.bindings[k] = v
			}
			for _, pattern := range oms.match.Patterns {
				for _, node := range pattern.Nodes {
					if node.Variable != "" {
						if _, exists := nullBinding.bindings[node.Variable]; !exists {
							nullBinding.bindings[node.Variable] = nil
						}
					}
				}
				for _, rel := range pattern.Relationships {
					if rel.Variable != "" {
						if _, exists := nullBinding.bindings[rel.Variable]; !exists {
							nullBinding.bindings[rel.Variable] = nil
						}
					}
				}
			}
			newResults = append(newResults, nullBinding)
		}
	}

	ctx.results = newResults
	return nil
}

func (oms *OptionalMatchStep) StepName() string { return "OptionalMatchStep" }
func (oms *OptionalMatchStep) StepDetail() string {
	return fmt.Sprintf("patterns=%d", len(oms.match.Patterns))
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
