package query

import (
	"fmt"
	"sort"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

func init() {
	RegisterFunction("type", fnType)
	RegisterFunction("labels", fnLabels)
	RegisterFunction("id", fnID)
	RegisterFunction("keys", fnKeys)
	RegisterFunction("properties", fnProperties)
}

// fnType returns the type (label) of a relationship
func fnType(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("type() requires 1 argument")
	}
	if args[0] == nil {
		return nil, nil
	}
	edge, ok := args[0].(*storage.Edge)
	if !ok {
		return nil, fmt.Errorf("type() requires a relationship argument, got %T", args[0])
	}
	return edge.Type, nil
}

// fnLabels returns the labels of a node as []any
func fnLabels(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("labels() requires 1 argument")
	}
	if args[0] == nil {
		return nil, nil
	}
	node, ok := args[0].(*storage.Node)
	if !ok {
		return nil, fmt.Errorf("labels() requires a node argument, got %T", args[0])
	}
	result := make([]any, len(node.Labels))
	for i, label := range node.Labels {
		result[i] = label
	}
	return result, nil
}

// fnID returns the internal ID of a node or edge
func fnID(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("id() requires 1 argument")
	}
	if args[0] == nil {
		return nil, nil
	}
	switch v := args[0].(type) {
	case *storage.Node:
		return int64(v.ID), nil
	case *storage.Edge:
		return int64(v.ID), nil
	default:
		return nil, fmt.Errorf("id() requires a node or relationship argument, got %T", args[0])
	}
}

// fnKeys returns sorted property key names of a node or edge
func fnKeys(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("keys() requires 1 argument")
	}
	if args[0] == nil {
		return nil, nil
	}

	var props map[string]storage.Value
	switch v := args[0].(type) {
	case *storage.Node:
		props = v.Properties
	case *storage.Edge:
		props = v.Properties
	default:
		return nil, fmt.Errorf("keys() requires a node or relationship argument, got %T", args[0])
	}

	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]any, len(keys))
	for i, k := range keys {
		result[i] = k
	}
	return result, nil
}

// fnProperties returns all properties as map[string]any
func fnProperties(args []any) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("properties() requires 1 argument")
	}
	if args[0] == nil {
		return nil, nil
	}

	var props map[string]storage.Value
	switch v := args[0].(type) {
	case *storage.Node:
		props = v.Properties
	case *storage.Edge:
		props = v.Properties
	default:
		return nil, fmt.Errorf("properties() requires a node or relationship argument, got %T", args[0])
	}

	result := make(map[string]any, len(props))
	for k, v := range props {
		result[k] = extractStorageValue(v)
	}
	return result, nil
}

// extractStorageValue converts a storage.Value to its native Go type.
// Shared helper used by edge property access and schema functions.
func extractStorageValue(val storage.Value) any {
	switch val.Type {
	case storage.TypeInt:
		if v, err := val.AsInt(); err == nil {
			return v
		}
	case storage.TypeString:
		if v, err := val.AsString(); err == nil {
			return v
		}
	case storage.TypeFloat:
		if v, err := val.AsFloat(); err == nil {
			return v
		}
	case storage.TypeBool:
		if v, err := val.AsBool(); err == nil {
			return v
		}
	case storage.TypeVector:
		if v, err := val.AsVector(); err == nil {
			return v
		}
	}
	return nil
}
