package query

import (
	"fmt"
	"maps"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// propertyTarget is a bound node or edge that SET and REMOVE write to. Each
// write also updates the bound entity, so a later assignment in the same
// clause reads the value just written.
type propertyTarget interface {
	setProperty(ctx *ExecutionContext, key string, val storage.Value) error
	removeProperty(ctx *ExecutionContext, key string) error
}

// resolvePropertyTarget returns what variable is bound to in binding, or nil
// for a null from OPTIONAL MATCH, which Cypher treats as a no-op. Anything
// else that cannot hold properties is refused rather than skipped: a skipped
// write still reports its rows as affected, which is how SET r.w on an edge
// used to change nothing.
func resolvePropertyTarget(binding *BindingSet, clause, variable string) (propertyTarget, error) {
	obj, ok := binding.bindings[variable]
	if !ok {
		return nil, fmt.Errorf("%s %s: variable is not defined by the query", clause, variable)
	}

	switch v := obj.(type) {
	case nil:
		return nil, nil
	case *storage.Node:
		return nodeTarget{v}, nil
	case *storage.Edge:
		return edgeTarget{v}, nil
	case []*storage.Edge:
		return nil, fmt.Errorf("%s %s: a variable-length relationship has no properties; match each relationship with a one-hop pattern", clause, variable)
	default:
		return nil, fmt.Errorf("%s %s: cannot write a property of a value of type %T", clause, variable, obj)
	}
}

type nodeTarget struct{ node *storage.Node }

func (t nodeTarget) setProperty(ctx *ExecutionContext, key string, val storage.Value) error {
	updated := withKey(t.node.Properties, key, val)

	// Audit A6c-query: tenant-scoped update.
	if err := ctx.graph.UpdateNodeForTenant(t.node.ID, updated, ctx.tenantID); err != nil {
		return fmt.Errorf("failed to update node %d: %w", t.node.ID, err)
	}
	t.node.Properties = updated
	return nil
}

func (t nodeTarget) removeProperty(ctx *ExecutionContext, key string) error {
	// Audit A6c-query: tenant-scoped property removal.
	if err := ctx.graph.RemoveNodePropertiesForTenant(t.node.ID, []string{key}, ctx.tenantID); err != nil {
		return fmt.Errorf("failed to remove property %s from node %d: %w", key, t.node.ID, err)
	}
	t.node.Properties = withoutKey(t.node.Properties, key)
	return nil
}

type edgeTarget struct{ edge *storage.Edge }

func (t edgeTarget) setProperty(ctx *ExecutionContext, key string, val storage.Value) error {
	if err := ctx.graph.PatchEdgeForTenant(t.edge.ID, map[string]storage.Value{key: val}, nil, nil, ctx.tenantID); err != nil {
		return fmt.Errorf("failed to update edge %d: %w", t.edge.ID, err)
	}
	t.edge.Properties = withKey(t.edge.Properties, key, val)
	return nil
}

func (t edgeTarget) removeProperty(ctx *ExecutionContext, key string) error {
	if err := ctx.graph.PatchEdgeForTenant(t.edge.ID, nil, []string{key}, nil, ctx.tenantID); err != nil {
		return fmt.Errorf("failed to remove property %s from edge %d: %w", key, t.edge.ID, err)
	}
	t.edge.Properties = withoutKey(t.edge.Properties, key)
	return nil
}

// withKey and withoutKey return a changed copy of props. The bound entity's
// map is replaced, not edited, because other rows may hold the same map.
func withKey(props map[string]storage.Value, key string, val storage.Value) map[string]storage.Value {
	updated := make(map[string]storage.Value, len(props)+1)
	maps.Copy(updated, props)
	updated[key] = val
	return updated
}

func withoutKey(props map[string]storage.Value, key string) map[string]storage.Value {
	remaining := maps.Clone(props)
	delete(remaining, key)
	return remaining
}
