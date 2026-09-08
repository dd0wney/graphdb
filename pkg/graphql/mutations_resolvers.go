package graphql

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/graphql-go/graphql"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/tenant"
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

		// ADR 0001: every create goes through the one uniqueness-rules
		// lookup path, so this resolver carries no domain vocabulary of its
		// own — a deployment's REGISTERED rules decide which labels are
		// unique on which property, not a hardcoded "Claim"/"for_task" pair.
		node, err := gs.CreateNodeWithUniquenessRulesForTenant(tenantID, labels, properties)
		if err != nil {
			// R4: a missing required rule gets fixed wording naming only the
			// caller's own label, never the rule name and never
			// err.Error() from storage (which would name the rule).
			var missing *storage.RequiredRuleMissingError
			if errors.As(err, &missing) {
				return nil, fmt.Errorf(
					"a required uniqueness rule for label %q is not registered; contact the administrator",
					missing.Label,
				)
			}
			if errors.Is(err, storage.ErrUniqueConstraintViolation) {
				// Surface the typed conflict verbatim so callers can
				// match on the message; errors.Is still works upstream.
				return nil, err
			}
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

// convertToStorageValue is the thin wrapper around
// storage.ValueFromJSON, kept as-is to minimise call-site churn in
// the resolver functions that reference it. The real conversion logic
// lives in pkg/storage and is shared with the REST handler path —
// previously the two duplicates diverged and caused silent-failure
// shape #7 (2026-05-14).
func convertToStorageValue(v any) storage.Value {
	return storage.ValueFromJSON(v)
}
