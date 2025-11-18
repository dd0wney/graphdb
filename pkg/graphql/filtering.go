package graphql

import (
	"fmt"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// FilterCondition represents a single filter condition
type FilterCondition struct {
	Field    string
	Operator string
	Value    interface{}
}

// FilterExpression represents a filter that can be simple or logical
type FilterExpression struct {
	// For simple conditions
	Conditions []FilterCondition

	// For logical operators
	AND []FilterExpression
	OR  []FilterExpression
	NOT *FilterExpression
}

// parseWhere parses the where argument into a filter expression
func parseWhere(args map[string]interface{}) *FilterExpression {
	whereArg, ok := args["where"]
	if !ok {
		return nil
	}

	whereMap, ok := whereArg.(map[string]interface{})
	if !ok {
		return nil
	}

	return parseFilterExpression(whereMap)
}

// parseFilterExpression recursively parses a filter expression
func parseFilterExpression(whereMap map[string]interface{}) *FilterExpression {
	expr := &FilterExpression{}

	for key, value := range whereMap {
		switch key {
		case "AND":
			// Parse AND array
			andArray, ok := value.([]interface{})
			if !ok {
				continue
			}
			for _, item := range andArray {
				itemMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				expr.AND = append(expr.AND, *parseFilterExpression(itemMap))
			}

		case "OR":
			// Parse OR array
			orArray, ok := value.([]interface{})
			if !ok {
				continue
			}
			for _, item := range orArray {
				itemMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				expr.OR = append(expr.OR, *parseFilterExpression(itemMap))
			}

		case "NOT":
			// Parse NOT object
			notMap, ok := value.(map[string]interface{})
			if !ok {
				continue
			}
			notExpr := parseFilterExpression(notMap)
			expr.NOT = notExpr

		default:
			// Regular field condition
			conditionMap, ok := value.(map[string]interface{})
			if !ok {
				continue
			}

			// Parse operators for this field
			for operator, opValue := range conditionMap {
				expr.Conditions = append(expr.Conditions, FilterCondition{
					Field:    key,
					Operator: operator,
					Value:    opValue,
				})
			}
		}
	}

	return expr
}

// evaluateFilter checks if a node matches the filter expression
func evaluateFilter(node *storage.Node, expr *FilterExpression) bool {
	if expr == nil {
		return true // No filter means match all
	}

	return evaluateFilterExpression(node, expr)
}

// evaluateFilterExpression recursively evaluates a filter expression
func evaluateFilterExpression(node *storage.Node, expr *FilterExpression) bool {
	// Evaluate NOT first
	if expr.NOT != nil {
		return !evaluateFilterExpression(node, expr.NOT)
	}

	// Evaluate OR - at least one must match
	if len(expr.OR) > 0 {
		for _, orExpr := range expr.OR {
			if evaluateFilterExpression(node, &orExpr) {
				return true
			}
		}
		return false
	}

	// Evaluate AND - all must match
	if len(expr.AND) > 0 {
		for _, andExpr := range expr.AND {
			if !evaluateFilterExpression(node, &andExpr) {
				return false
			}
		}
		return true
	}

	// Evaluate simple conditions - all must match (implicit AND)
	for _, condition := range expr.Conditions {
		if !evaluateCondition(node, condition) {
			return false
		}
	}

	return true
}

// evaluateCondition checks if a node matches a single filter condition
func evaluateCondition(node *storage.Node, condition FilterCondition) bool {
	// Get the property value from the node
	propertyValue, exists := node.Properties[condition.Field]
	if !exists {
		return false
	}

	// Extract the actual value based on type
	var nodeValue interface{}
	switch propertyValue.Type {
	case storage.TypeString:
		nodeValue, _ = propertyValue.AsString()
	case storage.TypeInt:
		nodeValue, _ = propertyValue.AsInt()
	case storage.TypeFloat:
		nodeValue, _ = propertyValue.AsFloat()
	case storage.TypeBool:
		nodeValue, _ = propertyValue.AsBool()
	default:
		return false
	}

	// Evaluate based on operator
	switch condition.Operator {
	case "eq":
		return evaluateEquals(nodeValue, condition.Value)
	case "gt":
		return evaluateGreaterThan(nodeValue, condition.Value)
	case "lt":
		return evaluateLessThan(nodeValue, condition.Value)
	case "gte":
		return evaluateGreaterThanOrEqual(nodeValue, condition.Value)
	case "lte":
		return evaluateLessThanOrEqual(nodeValue, condition.Value)
	case "contains":
		return evaluateContains(nodeValue, condition.Value)
	case "in":
		return evaluateIn(nodeValue, condition.Value)
	default:
		return false
	}
}

// evaluateEquals checks if two values are equal
func evaluateEquals(nodeValue, filterValue interface{}) bool {
	switch nv := nodeValue.(type) {
	case string:
		fv, ok := filterValue.(string)
		return ok && nv == fv
	case int64:
		// Handle both int and float filter values
		switch fv := filterValue.(type) {
		case int:
			return nv == int64(fv)
		case int64:
			return nv == fv
		case float64:
			return float64(nv) == fv
		}
	case float64:
		switch fv := filterValue.(type) {
		case float64:
			return nv == fv
		case int:
			return nv == float64(fv)
		case int64:
			return nv == float64(fv)
		}
	case bool:
		fv, ok := filterValue.(bool)
		return ok && nv == fv
	}
	return false
}

// evaluateGreaterThan checks if nodeValue > filterValue
func evaluateGreaterThan(nodeValue, filterValue interface{}) bool {
	return compareNumeric(nodeValue, filterValue) > 0
}

// evaluateLessThan checks if nodeValue < filterValue
func evaluateLessThan(nodeValue, filterValue interface{}) bool {
	return compareNumeric(nodeValue, filterValue) < 0
}

// evaluateGreaterThanOrEqual checks if nodeValue >= filterValue
func evaluateGreaterThanOrEqual(nodeValue, filterValue interface{}) bool {
	return compareNumeric(nodeValue, filterValue) >= 0
}

// evaluateLessThanOrEqual checks if nodeValue <= filterValue
func evaluateLessThanOrEqual(nodeValue, filterValue interface{}) bool {
	return compareNumeric(nodeValue, filterValue) <= 0
}

// evaluateContains checks if a string contains a substring
func evaluateContains(nodeValue, filterValue interface{}) bool {
	nv, ok1 := nodeValue.(string)
	fv, ok2 := filterValue.(string)
	if !ok1 || !ok2 {
		return false
	}
	return strings.Contains(nv, fv)
}

// evaluateIn checks if nodeValue is in the filter array
func evaluateIn(nodeValue, filterValue interface{}) bool {
	filterArray, ok := filterValue.([]interface{})
	if !ok {
		return false
	}

	for _, item := range filterArray {
		if evaluateEquals(nodeValue, item) {
			return true
		}
	}
	return false
}

// compareNumeric compares two numeric values
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func compareNumeric(a, b interface{}) int {
	var aFloat, bFloat float64

	// Convert a to float64
	switch av := a.(type) {
	case int64:
		aFloat = float64(av)
	case float64:
		aFloat = av
	case int:
		aFloat = float64(av)
	default:
		return 0
	}

	// Convert b to float64
	switch bv := b.(type) {
	case int64:
		bFloat = float64(bv)
	case float64:
		bFloat = bv
	case int:
		bFloat = float64(bv)
	default:
		return 0
	}

	if aFloat < bFloat {
		return -1
	} else if aFloat > bFloat {
		return 1
	}
	return 0
}

// evaluateEdgeFilter checks if an edge matches the filter expression
func evaluateEdgeFilter(edge *storage.Edge, expr *FilterExpression) bool {
	if expr == nil {
		return true
	}

	return evaluateEdgeFilterExpression(edge, expr)
}

// evaluateEdgeFilterExpression recursively evaluates a filter expression for edges
func evaluateEdgeFilterExpression(edge *storage.Edge, expr *FilterExpression) bool {
	// Evaluate NOT first
	if expr.NOT != nil {
		return !evaluateEdgeFilterExpression(edge, expr.NOT)
	}

	// Evaluate OR - at least one must match
	if len(expr.OR) > 0 {
		for _, orExpr := range expr.OR {
			if evaluateEdgeFilterExpression(edge, &orExpr) {
				return true
			}
		}
		return false
	}

	// Evaluate AND - all must match
	if len(expr.AND) > 0 {
		for _, andExpr := range expr.AND {
			if !evaluateEdgeFilterExpression(edge, &andExpr) {
				return false
			}
		}
		return true
	}

	// Evaluate simple conditions - all must match (implicit AND)
	for _, condition := range expr.Conditions {
		if !evaluateEdgeCondition(edge, condition) {
			return false
		}
	}

	return true
}

// evaluateEdgeCondition checks if an edge matches a single filter condition
func evaluateEdgeCondition(edge *storage.Edge, condition FilterCondition) bool {
	// Check if filtering by weight
	if condition.Field == "weight" {
		switch condition.Operator {
		case "eq":
			return evaluateEquals(edge.Weight, condition.Value)
		case "gt":
			return evaluateGreaterThan(edge.Weight, condition.Value)
		case "lt":
			return evaluateLessThan(edge.Weight, condition.Value)
		case "gte":
			return evaluateGreaterThanOrEqual(edge.Weight, condition.Value)
		case "lte":
			return evaluateLessThanOrEqual(edge.Weight, condition.Value)
		}
		return false
	}

	// Check edge properties
	propertyValue, exists := edge.Properties[condition.Field]
	if !exists {
		return false
	}

	var edgeValue interface{}
	switch propertyValue.Type {
	case storage.TypeString:
		edgeValue, _ = propertyValue.AsString()
	case storage.TypeInt:
		edgeValue, _ = propertyValue.AsInt()
	case storage.TypeFloat:
		edgeValue, _ = propertyValue.AsFloat()
	case storage.TypeBool:
		edgeValue, _ = propertyValue.AsBool()
	default:
		return false
	}

	switch condition.Operator {
	case "eq":
		return evaluateEquals(edgeValue, condition.Value)
	case "gt":
		return evaluateGreaterThan(edgeValue, condition.Value)
	case "lt":
		return evaluateLessThan(edgeValue, condition.Value)
	case "gte":
		return evaluateGreaterThanOrEqual(edgeValue, condition.Value)
	case "lte":
		return evaluateLessThanOrEqual(edgeValue, condition.Value)
	case "contains":
		return evaluateContains(edgeValue, condition.Value)
	case "in":
		return evaluateIn(edgeValue, condition.Value)
	default:
		return false
	}
}

// GenerateSchemaWithFiltering generates a GraphQL schema with filtering support
func GenerateSchemaWithFiltering(gs *storage.GraphStorage) (graphql.Schema, error) {
	labels := gs.GetAllLabels()
	nodeTypes := make(map[string]*graphql.Object)

	// Create where input type (we'll use a generic JSON-like structure)
	whereInputType := graphql.NewScalar(graphql.ScalarConfig{
		Name:        "WhereInput",
		Description: "Filter conditions for queries",
		Serialize: func(value interface{}) interface{} {
			return value
		},
	})

	// Create orderBy input type once
	orderByInputType := createOrderByInputType()

	// Create node types
	for _, label := range labels {
		nodeTypes[label] = createNodeType(label)
	}

	// Create edge type
	edgeType := createEdgeType()

	// Create query fields
	queryFields := graphql.Fields{
		"health": &graphql.Field{
			Type: graphql.String,
			Resolve: func(p graphql.ResolveParams) (interface{}, error) {
				return "ok", nil
			},
		},
	}

	// Add node queries with filtering
	for _, label := range labels {
		nodeType := nodeTypes[label]

		// Singular query (no filtering needed)
		singularName := strings.ToLower(label)
		queryFields[singularName] = &graphql.Field{
			Type: nodeType,
			Args: graphql.FieldConfigArgument{
				"id": &graphql.ArgumentConfig{
					Type: graphql.NewNonNull(graphql.ID),
				},
			},
			Resolve: createNodeResolver(gs, label),
		}

		// Plural query with filtering
		pluralName := strings.ToLower(label) + "s"
		queryFields[pluralName] = &graphql.Field{
			Type: graphql.NewList(nodeType),
			Args: graphql.FieldConfigArgument{
				"where": &graphql.ArgumentConfig{
					Type: whereInputType,
				},
				"orderBy": &graphql.ArgumentConfig{
					Type: orderByInputType,
				},
				"limit": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
				"offset": &graphql.ArgumentConfig{
					Type: graphql.Int,
				},
			},
			Resolve: createNodesResolverWithFiltering(gs, label),
		}
	}

	// Add edge queries with filtering
	queryFields["edge"] = &graphql.Field{
		Type: edgeType,
		Args: graphql.FieldConfigArgument{
			"id": &graphql.ArgumentConfig{
				Type: graphql.NewNonNull(graphql.ID),
			},
		},
		Resolve: createEdgeResolver(gs),
	}

	queryFields["edges"] = &graphql.Field{
		Type: graphql.NewList(edgeType),
		Args: graphql.FieldConfigArgument{
			"where": &graphql.ArgumentConfig{
				Type: whereInputType,
			},
			"orderBy": &graphql.ArgumentConfig{
				Type: orderByInputType,
			},
			"limit": &graphql.ArgumentConfig{
				Type: graphql.Int,
			},
			"offset": &graphql.ArgumentConfig{
				Type: graphql.Int,
			},
		},
		Resolve: createEdgesResolverWithFiltering(gs),
	}

	queryType := graphql.NewObject(graphql.ObjectConfig{
		Name:   "Query",
		Fields: queryFields,
	})

	schema, err := graphql.NewSchema(graphql.SchemaConfig{
		Query: queryType,
	})

	if err != nil {
		return graphql.Schema{}, fmt.Errorf("failed to create schema: %w", err)
	}

	return schema, nil
}

// createNodesResolverWithFiltering creates a resolver with filtering support
func createNodesResolverWithFiltering(gs *storage.GraphStorage, label string) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Get all nodes with this label
		nodes, err := gs.FindNodesByLabel(label)
		if err != nil {
			return nil, err
		}

		// Apply filtering
		filterConditions := parseWhere(p.Args)
		var filteredNodes []*storage.Node
		for _, node := range nodes {
			if evaluateFilter(node, filterConditions) {
				filteredNodes = append(filteredNodes, node)
			}
		}

		// Apply sorting if specified
		orderBy := parseOrderBy(p.Args)
		filteredNodes = sortNodes(filteredNodes, orderBy)

		// Apply pagination
		limit, limitOk := p.Args["limit"].(int)
		offset, offsetOk := p.Args["offset"].(int)

		if offsetOk && offset > 0 {
			if offset >= len(filteredNodes) {
				return []*storage.Node{}, nil
			}
			filteredNodes = filteredNodes[offset:]
		}

		if limitOk && limit > 0 {
			if limit < len(filteredNodes) {
				filteredNodes = filteredNodes[:limit]
			}
		}

		return filteredNodes, nil
	}
}

// createEdgesResolverWithFiltering creates an edge resolver with filtering support
func createEdgesResolverWithFiltering(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (interface{}, error) {
		// Get all edges
		stats := gs.GetStatistics()
		edges := make([]*storage.Edge, 0)

		for edgeID := uint64(1); edgeID <= stats.EdgeCount; edgeID++ {
			edge, err := gs.GetEdge(edgeID)
			if err != nil {
				continue
			}
			edges = append(edges, edge)
		}

		// Apply filtering
		filterConditions := parseWhere(p.Args)
		var filteredEdges []*storage.Edge
		for _, edge := range edges {
			if evaluateEdgeFilter(edge, filterConditions) {
				filteredEdges = append(filteredEdges, edge)
			}
		}

		// Apply sorting if specified
		orderBy := parseOrderBy(p.Args)
		filteredEdges = sortEdges(filteredEdges, orderBy)

		// Apply pagination
		limit, limitOk := p.Args["limit"].(int)
		offset, offsetOk := p.Args["offset"].(int)

		if offsetOk && offset > 0 {
			if offset >= len(filteredEdges) {
				return []*storage.Edge{}, nil
			}
			filteredEdges = filteredEdges[offset:]
		}

		if limitOk && limit > 0 {
			if limit < len(filteredEdges) {
				filteredEdges = filteredEdges[:limit]
			}
		}

		return filteredEdges, nil
	}
}
