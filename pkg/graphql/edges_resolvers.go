package graphql

import (
	"encoding/json"
	"fmt"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
)

// createEdgeResolver creates a resolver for fetching a single edge by ID
func createEdgeResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		idStr, ok := p.Args["id"].(string)
		if !ok {
			return nil, fmt.Errorf("id argument is required")
		}

		var id uint64
		if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
			return nil, fmt.Errorf("invalid edge id %q: %w", idStr, err)
		}

		// Audit A6c-graphql-resolvers: tenant-scoped edge read.
		tenantID := tenant.MustFromContext(p.Context)
		edge, err := gs.GetEdgeForTenant(id, tenantID)
		if err != nil {
			return nil, err
		}

		return edge, nil
	}
}

// createEdgesResolver creates a resolver for fetching all edges
func createEdgesResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		// Audit A6c-graphql-resolvers: replace the
		// "iterate from 1..stats.EdgeCount via GetEdge" anti-pattern
		// (which leaked edges across tenants) with a single
		// tenant-scoped enumeration.
		tenantID := tenant.MustFromContext(p.Context)
		edges := gs.GetAllEdgesForTenant(tenantID)

		// Apply pagination if specified
		limit, limitOk := p.Args["limit"].(int)
		offset, offsetOk := p.Args["offset"].(int)

		// Apply offset
		if offsetOk && offset > 0 {
			if offset >= len(edges) {
				return []*storage.Edge{}, nil
			}
			edges = edges[offset:]
		}

		// Apply limit
		if limitOk && limit >= 0 {
			if limit < len(edges) {
				edges = edges[:limit]
			}
		}

		return edges, nil
	}
}

// createEdgeMutationResolver creates a resolver for createEdge mutation
func createEdgeMutationResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		// Get arguments
		fromNodeIDStr, ok := p.Args["fromNodeId"].(string)
		if !ok {
			return nil, fmt.Errorf("fromNodeId argument is required")
		}

		toNodeIDStr, ok := p.Args["toNodeId"].(string)
		if !ok {
			return nil, fmt.Errorf("toNodeId argument is required")
		}

		edgeType, ok := p.Args["type"].(string)
		if !ok {
			return nil, fmt.Errorf("type argument is required")
		}

		// Parse node IDs
		var fromNodeID, toNodeID uint64
		if _, err := fmt.Sscanf(fromNodeIDStr, "%d", &fromNodeID); err != nil {
			return nil, fmt.Errorf("invalid fromNodeId %q: %w", fromNodeIDStr, err)
		}
		if _, err := fmt.Sscanf(toNodeIDStr, "%d", &toNodeID); err != nil {
			return nil, fmt.Errorf("invalid toNodeId %q: %w", toNodeIDStr, err)
		}

		// Get optional weight (default to 1.0)
		weight := 1.0
		if w, ok := p.Args["weight"].(float64); ok {
			weight = w
		}

		// Parse properties if provided
		properties := make(map[string]storage.Value)
		if propsJSON, ok := p.Args["properties"].(string); ok && propsJSON != "" {
			var propsMap map[string]any
			if err := json.Unmarshal([]byte(propsJSON), &propsMap); err != nil {
				return nil, fmt.Errorf("invalid properties JSON: %w", err)
			}

			for k, v := range propsMap {
				properties[k] = convertToStorageValue(v)
			}
		}

		// Audit A6c-graphql-resolvers: tenant-scoped edge create.
		// CreateEdgeWithTenant is tenant-strict on from/to node
		// verification (audit A6a follow-up #20), so cross-tenant
		// edge creation surfaces ErrNodeNotFound here.
		tenantID := tenant.MustFromContext(p.Context)
		edge, err := gs.CreateEdgeWithTenant(tenantID, fromNodeID, toNodeID, edgeType, properties, weight)
		if err != nil {
			return nil, fmt.Errorf("failed to create edge: %w", err)
		}

		return edge, nil
	}
}

// updateEdgeMutationResolver creates a resolver for updateEdge mutation
func updateEdgeMutationResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		// Get ID argument
		idStr, ok := p.Args["id"].(string)
		if !ok {
			return nil, fmt.Errorf("id argument is required")
		}

		var id uint64
		if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
			return nil, fmt.Errorf("invalid edge id %q: %w", idStr, err)
		}

		// Parse properties if provided
		var properties map[string]storage.Value
		if propsJSON, ok := p.Args["properties"].(string); ok && propsJSON != "" {
			var propsMap map[string]any
			if err := json.Unmarshal([]byte(propsJSON), &propsMap); err != nil {
				return nil, fmt.Errorf("invalid properties JSON: %w", err)
			}

			properties = make(map[string]storage.Value)
			for k, v := range propsMap {
				properties[k] = convertToStorageValue(v)
			}
		}

		// Get weight if provided
		var weight *float64
		if w, ok := p.Args["weight"].(float64); ok {
			weight = &w
		}

		// Audit A6c-graphql-resolvers: tenant-scoped update.
		tenantID := tenant.MustFromContext(p.Context)
		if err := gs.UpdateEdgeForTenant(id, properties, weight, tenantID); err != nil {
			return nil, fmt.Errorf("failed to update edge: %w", err)
		}

		edge, err := gs.GetEdgeForTenant(id, tenantID)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve updated edge: %w", err)
		}

		return edge, nil
	}
}

// deleteEdgeMutationResolver creates a resolver for deleteEdge mutation
func deleteEdgeMutationResolver(gs *storage.GraphStorage) graphql.FieldResolveFn {
	return func(p graphql.ResolveParams) (any, error) {
		idStr, ok := p.Args["id"].(string)
		if !ok {
			return nil, fmt.Errorf("id argument is required")
		}

		var id uint64
		if _, err := fmt.Sscanf(idStr, "%d", &id); err != nil {
			return nil, fmt.Errorf("invalid edge id %q: %w", idStr, err)
		}

		// Audit A6c-graphql-resolvers: tenant-scoped delete.
		tenantID := tenant.MustFromContext(p.Context)
		if err := gs.DeleteEdgeForTenant(id, tenantID); err != nil {
			return nil, fmt.Errorf("edge not found: %w", err)
		}

		return map[string]any{
			"success": true,
			"id":      idStr,
		}, nil
	}
}
