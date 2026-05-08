package graphql

import (
	"encoding/json"
	"fmt"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/dd0wney/cluso-graphdb/pkg/tenant"
)

// createNodeMutationResolver creates a resolver for createNode mutation
func createNodeMutationResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		// Get labels argument
		labelsArg, ok := p.Args["labels"].([]any)
		if !ok {
			return nil, fmt.Errorf("labels argument is required")
		}

		// Convert to string slice
		labels := make([]string, len(labelsArg))
		for i, label := range labelsArg {
			labels[i] = label.(string)
		}

		// Get properties argument
		propertiesJSON, ok := p.Args["properties"].(string)
		if !ok {
			return nil, fmt.Errorf("properties argument is required")
		}

		// Parse properties JSON
		var propsMap map[string]any
		if err := json.Unmarshal([]byte(propertiesJSON), &propsMap); err != nil {
			return nil, fmt.Errorf("invalid properties JSON: %w", err)
		}

		// Convert to storage.Value map
		properties := make(map[string]storage.Value)
		for k, v := range propsMap {
			properties[k] = convertToStorageValue(v)
		}

		// Create node in storage, scoped to caller's tenant.
		// Audit A6c-graphql-resolvers (2026-05-08).
		tenantID := tenant.MustFromContext(p.Context)
		node, err := gs.CreateNodeWithTenant(tenantID, labels, properties)
		if err != nil {
			return nil, fmt.Errorf("failed to create node: %w", err)
		}

		return node, nil
	}
}

// updateNodeMutationResolver creates a resolver for updateNode mutation
func updateNodeMutationResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		// Get ID argument
		idStr, ok := p.Args["id"].(string)
		if !ok {
			return nil, fmt.Errorf("id argument is required")
		}

		// Convert string ID to uint64
		var id uint64
		if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
			return nil, fmt.Errorf("invalid id %q: %w", idStr, err)
		}

		// Get properties argument
		propertiesJSON, ok := p.Args["properties"].(string)
		if !ok {
			return nil, fmt.Errorf("properties argument is required")
		}

		// Parse properties JSON
		var propsMap map[string]any
		if err := json.Unmarshal([]byte(propertiesJSON), &propsMap); err != nil {
			return nil, fmt.Errorf("invalid properties JSON: %w", err)
		}

		// Convert to storage.Value map
		properties := make(map[string]storage.Value)
		for k, v := range propsMap {
			properties[k] = convertToStorageValue(v)
		}

		// Audit A6c-graphql-resolvers: tenant-scoped update.
		tenantID := tenant.MustFromContext(p.Context)
		if err := gs.UpdateNodeForTenant(id, properties, tenantID); err != nil {
			return nil, fmt.Errorf("node not found: %w", err)
		}

		node, err := gs.GetNodeForTenant(id, tenantID)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve updated node: %w", err)
		}

		return node, nil
	}
}

// deleteNodeMutationResolver creates a resolver for deleteNode mutation
func deleteNodeMutationResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		// Get ID argument
		idStr, ok := p.Args["id"].(string)
		if !ok {
			return nil, fmt.Errorf("id argument is required")
		}

		// Convert string ID to uint64
		var id uint64
		if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
			return nil, fmt.Errorf("invalid id %q: %w", idStr, err)
		}

		// Audit A6c-graphql-resolvers: tenant-scoped delete.
		tenantID := tenant.MustFromContext(p.Context)
		if err := gs.DeleteNodeForTenant(id, tenantID); err != nil {
			return nil, fmt.Errorf("node not found: %w", err)
		}

		// Return success result
		return map[string]any{
			"success": true,
			"id":      idStr,
		}, nil
	}
}

// convertToStorageValue converts a Go any to storage.Value
func convertToStorageValue(v any) storage.Value {
	switch val := v.(type) {
	case string:
		return storage.StringValue(val)
	case int:
		return storage.IntValue(int64(val))
	case int64:
		return storage.IntValue(val)
	case float64:
		// JSON numbers are always float64, but if it's a whole number,
		// store it as an int for better type compatibility
		if val == float64(int64(val)) {
			return storage.IntValue(int64(val))
		}
		return storage.FloatValue(val)
	case bool:
		return storage.BoolValue(val)
	default:
		return storage.StringValue(fmt.Sprintf("%v", val))
	}
}
