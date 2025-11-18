package graphql

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// DataLoaderContext holds all DataLoaders for a request
type DataLoaderContext struct {
	Nodes          *DataLoader
	OutgoingEdges  *DataLoader
	IncomingEdges  *DataLoader
}

// GenerateSchemaWithDataLoader creates a GraphQL schema with DataLoader integration
func GenerateSchemaWithDataLoader(gs *storage.GraphStorage) (graphql.Schema, *DataLoaderContext) {
	// Create DataLoaders
	loaders := &DataLoaderContext{
		Nodes:         NewNodeDataLoader(gs),
		OutgoingEdges: NewOutgoingEdgesDataLoader(gs),
		IncomingEdges: NewIncomingEdgesDataLoader(gs),
	}

	// For now, use the existing schema generation
	// In a full implementation, we'd modify resolvers to use the DataLoaders
	schema, _ := GenerateSchemaWithFiltering(gs)

	return schema, loaders
}
