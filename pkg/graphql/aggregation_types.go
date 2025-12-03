package graphql

import (
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// buildNodeAggregateTypes creates node types and aggregate types for a label
func buildNodeAggregateTypes(gs *storage.GraphStorage, label string) (*graphql.Object, *graphql.Object) {
	// Create regular node type
	nodeType := graphql.NewObject(graphql.ObjectConfig{
		Name: label,
		Fields: graphql.Fields{
			"id":         &graphql.Field{Type: graphql.String},
			"labels":     &graphql.Field{Type: graphql.NewList(graphql.String)},
			"properties": &graphql.Field{Type: graphql.String},
		},
	})

	// Get sample nodes to discover properties
	sampleNodes, _ := gs.FindNodesByLabel(label)
	propertyFields := graphql.Fields{}

	// Build a map of property keys to their types
	if len(sampleNodes) > 0 {
		propertyTypes := make(map[string]storage.ValueType)
		for _, node := range sampleNodes {
			for key, value := range node.Properties {
				// Store the first type we see for each property
				if _, exists := propertyTypes[key]; !exists {
					propertyTypes[key] = value.Type
				}
			}
		}

		// Create fields for min/max/sum (preserve original types)
		minMaxSumFields := graphql.Fields{}
		// Create fields for avg (always float)
		avgFields := graphql.Fields{}

		for key := range propertyTypes {
			// Use Float for all numeric types to preserve int64 precision
			minMaxSumFields[key] = &graphql.Field{Type: graphql.Float}
			avgFields[key] = &graphql.Field{Type: graphql.Float}
		}

		// If no properties found, add a dummy field to avoid empty object
		if len(minMaxSumFields) == 0 {
			minMaxSumFields["_"] = &graphql.Field{Type: graphql.String}
			avgFields["_"] = &graphql.Field{Type: graphql.String}
		}

		propertyFields = minMaxSumFields
	} else {
		// If no properties found, add a dummy field to avoid empty object
		propertyFields["_"] = &graphql.Field{Type: graphql.String}
	}

	// Create separate aggregate field types
	minFieldsType := graphql.NewObject(graphql.ObjectConfig{
		Name:   label + "AggregateMinFields",
		Fields: propertyFields,
	})
	maxFieldsType := graphql.NewObject(graphql.ObjectConfig{
		Name:   label + "AggregateMaxFields",
		Fields: propertyFields,
	})
	sumFieldsType := graphql.NewObject(graphql.ObjectConfig{
		Name:   label + "AggregateSumFields",
		Fields: propertyFields,
	})

	// Create avg fields with float type
	avgPropertyFields := graphql.Fields{}
	if len(sampleNodes) > 0 {
		propertyTypes := make(map[string]storage.ValueType)
		for _, node := range sampleNodes {
			for key, value := range node.Properties {
				if _, exists := propertyTypes[key]; !exists {
					propertyTypes[key] = value.Type
				}
			}
		}
		for key := range propertyTypes {
			avgPropertyFields[key] = &graphql.Field{Type: graphql.Float}
		}
	}
	if len(avgPropertyFields) == 0 {
		avgPropertyFields["_"] = &graphql.Field{Type: graphql.String}
	}
	avgFieldsType := graphql.NewObject(graphql.ObjectConfig{
		Name:   label + "AggregateAvgFields",
		Fields: avgPropertyFields,
	})

	// Create aggregate type
	aggregateType := graphql.NewObject(graphql.ObjectConfig{
		Name: label + "Aggregate",
		Fields: graphql.Fields{
			"count": &graphql.Field{
				Type: graphql.Int,
			},
			"min": &graphql.Field{
				Type: minFieldsType,
			},
			"max": &graphql.Field{
				Type: maxFieldsType,
			},
			"avg": &graphql.Field{
				Type: avgFieldsType,
			},
			"sum": &graphql.Field{
				Type: sumFieldsType,
			},
		},
	})

	return nodeType, aggregateType
}

// buildEdgeAggregateType creates the edge aggregate type
func buildEdgeAggregateType() *graphql.Object {
	// Create edge aggregate fields type
	edgeAggregateFieldsType := graphql.NewObject(graphql.ObjectConfig{
		Name: "EdgeAggregateFields",
		Fields: graphql.Fields{
			"weight": &graphql.Field{Type: graphql.Float},
		},
	})

	// Create edge aggregate type
	return graphql.NewObject(graphql.ObjectConfig{
		Name: "EdgeAggregate",
		Fields: graphql.Fields{
			"count": &graphql.Field{Type: graphql.Int},
			"min":   &graphql.Field{Type: edgeAggregateFieldsType},
			"max":   &graphql.Field{Type: edgeAggregateFieldsType},
			"avg":   &graphql.Field{Type: edgeAggregateFieldsType},
			"sum":   &graphql.Field{Type: edgeAggregateFieldsType},
		},
	})
}
