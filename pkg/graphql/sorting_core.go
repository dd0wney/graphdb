package graphql

import (
	"sort"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// OrderByInput represents sorting criteria
type OrderByInput struct {
	Field     string
	Direction string
}

func createOrderByInputType() *graphql.InputObject {
	return graphql.NewInputObject(graphql.InputObjectConfig{
		Name: "OrderByInput",
		Fields: graphql.InputObjectConfigFieldMap{
			"field": &graphql.InputObjectFieldConfig{
				Type: graphql.NewNonNull(graphql.String),
			},
			"direction": &graphql.InputObjectFieldConfig{
				Type: graphql.NewNonNull(graphql.String),
			},
		},
	})
}

// parseOrderBy parses the orderBy argument
func parseOrderBy(args map[string]any) *OrderByInput {
	orderByArg, ok := args["orderBy"]
	if !ok {
		return nil
	}

	orderByMap, ok := orderByArg.(map[string]any)
	if !ok {
		return nil
	}

	field, _ := orderByMap["field"].(string)
	direction, _ := orderByMap["direction"].(string)

	if field == "" || (direction != "ASC" && direction != "DESC") {
		return nil
	}

	return &OrderByInput{
		Field:     field,
		Direction: direction,
	}
}

// sortNodes sorts nodes based on OrderByInput using O(n log n) sort
func sortNodes(nodes []*storage.Node, orderBy *OrderByInput) []*storage.Node {
	if orderBy == nil || len(nodes) == 0 {
		return nodes
	}

	// Create a copy to avoid modifying the original slice
	sorted := make([]*storage.Node, len(nodes))
	copy(sorted, nodes)

	// Use standard library sort for O(n log n) performance
	sort.Slice(sorted, func(i, j int) bool {
		val1 := getNodePropertyValue(sorted[i], orderBy.Field)
		val2 := getNodePropertyValue(sorted[j], orderBy.Field)
		cmp := compareValues(val1, val2)
		if orderBy.Direction == "ASC" {
			return cmp < 0
		}
		return cmp > 0
	})

	return sorted
}

// sortEdges sorts edges based on OrderByInput using O(n log n) sort
func sortEdges(edges []*storage.Edge, orderBy *OrderByInput) []*storage.Edge {
	if orderBy == nil || len(edges) == 0 {
		return edges
	}

	// Create a copy to avoid modifying the original slice
	sorted := make([]*storage.Edge, len(edges))
	copy(sorted, edges)

	// Use standard library sort for O(n log n) performance
	sort.Slice(sorted, func(i, j int) bool {
		var val1, val2 any

		if orderBy.Field == "weight" {
			val1 = sorted[i].Weight
			val2 = sorted[j].Weight
		} else {
			val1 = getEdgePropertyValue(sorted[i], orderBy.Field)
			val2 = getEdgePropertyValue(sorted[j], orderBy.Field)
		}

		cmp := compareValues(val1, val2)
		if orderBy.Direction == "ASC" {
			return cmp < 0
		}
		return cmp > 0
	})

	return sorted
}

// getNodePropertyValue extracts a property value from a node
func getNodePropertyValue(node *storage.Node, field string) any {
	if node.Properties == nil {
		return nil
	}

	value, exists := node.Properties[field]
	if !exists {
		return nil
	}

	return extractStorageValue(value)
}

// getEdgePropertyValue extracts a property value from an edge
func getEdgePropertyValue(edge *storage.Edge, field string) any {
	if edge.Properties == nil {
		return nil
	}

	value, exists := edge.Properties[field]
	if !exists {
		return nil
	}

	return extractStorageValue(value)
}

// extractStorageValue extracts the actual value from a storage.Value
func extractStorageValue(value storage.Value) any {
	switch value.Type {
	case storage.TypeString:
		v, _ := value.AsString()
		return v
	case storage.TypeInt:
		v, _ := value.AsInt()
		return v
	case storage.TypeFloat:
		v, _ := value.AsFloat()
		return v
	case storage.TypeBool:
		v, _ := value.AsBool()
		return v
	default:
		return nil
	}
}

// compareValues compares two values for sorting
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func compareValues(a, b any) int {
	// Handle nil values
	if a == nil && b == nil {
		return 0
	}
	if a == nil {
		return -1
	}
	if b == nil {
		return 1
	}

	// Compare based on type
	switch aVal := a.(type) {
	case string:
		return compareStrings(aVal, b)
	case int64:
		return compareInt64(aVal, b)
	case float64:
		return compareFloat64(aVal, b)
	case bool:
		return compareBools(aVal, b)
	default:
		return 0
	}
}

// compareStrings compares two strings
func compareStrings(a string, b any) int {
	bVal, ok := b.(string)
	if !ok {
		return 0
	}
	if a < bVal {
		return -1
	} else if a > bVal {
		return 1
	}
	return 0
}

// compareInt64 compares two int64 values
func compareInt64(a int64, b any) int {
	bVal, ok := b.(int64)
	if !ok {
		return 0
	}
	if a < bVal {
		return -1
	} else if a > bVal {
		return 1
	}
	return 0
}

// compareFloat64 compares two float64 values
func compareFloat64(a float64, b any) int {
	bVal, ok := b.(float64)
	if !ok {
		return 0
	}
	if a < bVal {
		return -1
	} else if a > bVal {
		return 1
	}
	return 0
}

// compareBools compares two bool values
func compareBools(a bool, b any) int {
	bVal, ok := b.(bool)
	if !ok {
		return 0
	}
	if !a && bVal {
		return -1
	} else if a && !bVal {
		return 1
	}
	return 0
}
