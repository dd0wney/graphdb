package graphql

import (
	"context"
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/pubsub"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
	"github.com/graphql-go/graphql"
)

// NodeEvent represents a node change event
type NodeEvent struct {
	Type  string // "created", "updated", "deleted"
	Node  *storage.Node
	Label string // For filtering by label
}

// EdgeEvent represents an edge change event
type EdgeEvent struct {
	Type     string // "created", "updated", "deleted"
	Edge     *storage.Edge
	EdgeType string // For filtering by edge type
}

// Topic names for subscriptions
const (
	TopicNodeCreated = "node.created"
	TopicNodeUpdated = "node.updated"
	TopicNodeDeleted = "node.deleted"
	TopicEdgeCreated = "edge.created"
	TopicEdgeUpdated = "edge.updated"
	TopicEdgeDeleted = "edge.deleted"
)

// SubscribeToNodeCreated subscribes to node creation events
func SubscribeToNodeCreated(ps *pubsub.PubSub, ctx context.Context, label string) (*pubsub.Subscription, error) {
	topic := fmt.Sprintf("%s.%s", TopicNodeCreated, label)
	return ps.Subscribe(ctx, topic)
}

// SubscribeToNodeUpdated subscribes to node update events for a specific node
func SubscribeToNodeUpdated(ps *pubsub.PubSub, ctx context.Context, nodeID uint64) (*pubsub.Subscription, error) {
	topic := fmt.Sprintf("%s.%d", TopicNodeUpdated, nodeID)
	return ps.Subscribe(ctx, topic)
}

// SubscribeToNodeDeleted subscribes to node deletion events for a specific node
func SubscribeToNodeDeleted(ps *pubsub.PubSub, ctx context.Context, nodeID uint64) (*pubsub.Subscription, error) {
	topic := fmt.Sprintf("%s.%d", TopicNodeDeleted, nodeID)
	return ps.Subscribe(ctx, topic)
}

// SubscribeToEdgeCreated subscribes to edge creation events
func SubscribeToEdgeCreated(ps *pubsub.PubSub, ctx context.Context, edgeType string) (*pubsub.Subscription, error) {
	topic := fmt.Sprintf("%s.%s", TopicEdgeCreated, edgeType)
	return ps.Subscribe(ctx, topic)
}

// SubscribeToEdgeUpdated subscribes to edge update events for a specific edge
func SubscribeToEdgeUpdated(ps *pubsub.PubSub, ctx context.Context, edgeID uint64) (*pubsub.Subscription, error) {
	topic := fmt.Sprintf("%s.%d", TopicEdgeUpdated, edgeID)
	return ps.Subscribe(ctx, topic)
}

// SubscribeToEdgeDeleted subscribes to edge deletion events for a specific edge
func SubscribeToEdgeDeleted(ps *pubsub.PubSub, ctx context.Context, edgeID uint64) (*pubsub.Subscription, error) {
	topic := fmt.Sprintf("%s.%d", TopicEdgeDeleted, edgeID)
	return ps.Subscribe(ctx, topic)
}

// PublishNodeCreated publishes a node creation event
func PublishNodeCreated(ps *pubsub.PubSub, node *storage.Node) {
	event := &NodeEvent{
		Type: "created",
		Node: node,
	}

	// Publish to each label topic
	for _, label := range node.Labels {
		topic := fmt.Sprintf("%s.%s", TopicNodeCreated, label)
		ps.Publish(topic, event)
	}

	// Also publish to general topic
	ps.Publish(TopicNodeCreated, event)
}

// PublishNodeUpdated publishes a node update event
func PublishNodeUpdated(ps *pubsub.PubSub, node *storage.Node) {
	event := &NodeEvent{
		Type: "updated",
		Node: node,
	}

	// Publish to node-specific topic
	topic := fmt.Sprintf("%s.%d", TopicNodeUpdated, node.ID)
	ps.Publish(topic, event)

	// Also publish to general topic
	ps.Publish(TopicNodeUpdated, event)
}

// PublishNodeDeleted publishes a node deletion event
func PublishNodeDeleted(ps *pubsub.PubSub, node *storage.Node) {
	event := &NodeEvent{
		Type: "deleted",
		Node: node,
	}

	// Publish to node-specific topic
	topic := fmt.Sprintf("%s.%d", TopicNodeDeleted, node.ID)
	ps.Publish(topic, event)

	// Also publish to general topic
	ps.Publish(TopicNodeDeleted, event)
}

// PublishEdgeCreated publishes an edge creation event
func PublishEdgeCreated(ps *pubsub.PubSub, edge *storage.Edge) {
	event := &EdgeEvent{
		Type: "created",
		Edge: edge,
	}

	// Publish to edge type topic
	topic := fmt.Sprintf("%s.%s", TopicEdgeCreated, edge.Type)
	ps.Publish(topic, event)

	// Also publish to general topic
	ps.Publish(TopicEdgeCreated, event)
}

// PublishEdgeUpdated publishes an edge update event
func PublishEdgeUpdated(ps *pubsub.PubSub, edge *storage.Edge) {
	event := &EdgeEvent{
		Type: "updated",
		Edge: edge,
	}

	// Publish to edge-specific topic
	topic := fmt.Sprintf("%s.%d", TopicEdgeUpdated, edge.ID)
	ps.Publish(topic, event)

	// Also publish to general topic
	ps.Publish(TopicEdgeUpdated, event)
}

// PublishEdgeDeleted publishes an edge deletion event
func PublishEdgeDeleted(ps *pubsub.PubSub, edge *storage.Edge) {
	event := &EdgeEvent{
		Type: "deleted",
		Edge: edge,
	}

	// Publish to edge-specific topic
	topic := fmt.Sprintf("%s.%d", TopicEdgeDeleted, edge.ID)
	ps.Publish(topic, event)

	// Also publish to general topic
	ps.Publish(TopicEdgeDeleted, event)
}

// GenerateSchemaWithSubscriptions creates a GraphQL schema with subscription support
func GenerateSchemaWithSubscriptions(gs *storage.GraphStorage, ps *pubsub.PubSub) (graphql.Schema, error) {
	// For now, just return the base schema
	// Full subscription implementation requires WebSocket support
	// which is typically handled at the transport layer
	schema, err := GenerateSchemaWithFiltering(gs)
	if err != nil {
		return graphql.Schema{}, err
	}

	// Note: GraphQL-go doesn't have built-in subscription support
	// Subscriptions are typically implemented using graphql-ws protocol over WebSockets
	// The pub/sub system we've built provides the backend infrastructure
	// The GraphQL schema can be extended with subscription types when needed

	return schema, nil
}
