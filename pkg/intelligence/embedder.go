package intelligence

import (
	"log"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// EmbeddingPolicy defines a rule for automatic embedding generation.
type EmbeddingPolicy struct {
	Label          string
	SourceProperty string
	TargetProperty string
	Model          string
}

// Embedder implements storage.NodeObserver and handles automatic embeddings.
type Embedder struct {
	graph    storage.Storage
	policies []EmbeddingPolicy
}

func NewEmbedder(graph storage.Storage) *Embedder {
	return &Embedder{
		graph: graph,
	}
}

func (e *Embedder) AddPolicy(policy EmbeddingPolicy) {
	e.policies = append(e.policies, policy)
}

func (e *Embedder) OnNodeCreated(node *storage.Node) {
	e.processNode(node)
}

func (e *Embedder) OnNodeUpdated(node *storage.Node, oldNode *storage.Node) {
	// Only re-embed if the source property changed
	shouldProcess := false
	for _, p := range e.policies {
		if !contains(node.Labels, p.Label) {
			continue
		}
		newVal, hasNew := node.Properties[p.SourceProperty]
		oldVal, hasOld := oldNode.Properties[p.SourceProperty]
		
		if hasNew != hasOld {
			shouldProcess = true
			break
		}
		
		if hasNew && !valuesEqual(newVal, oldVal) {
			shouldProcess = true
			break
		}
	}

	if shouldProcess {
		e.processNode(node)
	}
}

func (e *Embedder) OnNodeDeleted(nodeID uint64, tenantID string) {
	// No action needed for embeddings stored on the node itself
}

func (e *Embedder) processNode(node *storage.Node) {
	for _, p := range e.policies {
		if !contains(node.Labels, p.Label) {
			continue
		}

		val, ok := node.Properties[p.SourceProperty]
		if !ok {
			continue
		}

		text, err := val.AsString()
		if err != nil || text == "" {
			continue
		}

		// Generate embedding
		go func(p EmbeddingPolicy, nodeID uint64, tenantID, content string) {
			// Mock embedding for spike
			// In real life, call external embedding API
			embedding := mockEmbedding(content)
			
			// Update node with vector
			props := map[string]storage.Value{
				p.TargetProperty: storage.VectorValue(embedding),
			}
			err := e.graph.UpdateNodeForTenant(nodeID, props, tenantID)
			if err != nil {
				log.Printf("Embedder: failed to update node %d: %v", nodeID, err)
			} else {
				log.Printf("Embedder: successfully generated embedding for node %d (%s)", nodeID, p.Label)
			}
		}(p, node.ID, node.TenantID, text)
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func valuesEqual(a, b storage.Value) bool {
	if a.Type != b.Type {
		return false
	}
	if len(a.Data) != len(b.Data) {
		return false
	}
	for i := range a.Data {
		if a.Data[i] != b.Data[i] {
			return false
		}
	}
	return true
}

func mockEmbedding(content string) []float32 {
	// Simple deterministic mock based on string length and first char
	h := float32(len(content)) / 100.0
	if len(content) > 0 {
		h += float32(content[0]) / 255.0
	}
	return []float32{h, h * 0.5, h * 0.25}
}
