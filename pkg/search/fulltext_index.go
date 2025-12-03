package search

import (
	"fmt"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// IndexNodes indexes all nodes with specified labels and properties
func (fti *FullTextIndex) IndexNodes(labels []string, properties []string) error {
	// Collect all nodes WITHOUT holding the index lock to prevent deadlock
	// (FindNodesByLabel acquires storage locks internally)
	var nodesToIndex []*storage.Node
	for _, label := range labels {
		nodes, err := fti.gs.FindNodesByLabel(label)
		if err != nil {
			return fmt.Errorf("failed to get nodes for label %s: %w", label, err)
		}
		nodesToIndex = append(nodesToIndex, nodes...)
	}

	// Now acquire the lock and perform indexing
	fti.indexMu.Lock()
	defer fti.indexMu.Unlock()

	fti.indexedLabels = labels
	fti.indexedProps = properties

	for _, node := range nodesToIndex {
		fti.indexNode(node, properties)
	}

	return nil
}

// indexNode indexes a single node (must be called with lock held)
func (fti *FullTextIndex) indexNode(node *storage.Node, properties []string) {
	// Extract text content from specified properties
	var textParts []string
	for _, prop := range properties {
		if val, ok := node.Properties[prop]; ok {
			if val.Type == storage.TypeString {
				if str, err := val.AsString(); err == nil {
					textParts = append(textParts, str)
				}
			}
		}
	}

	if len(textParts) == 0 {
		return
	}

	content := strings.Join(textParts, " ")
	fti.nodeContent[node.ID] = content

	// Tokenize and index
	tokens := tokenize(content)
	seenTerms := make(map[string]bool)

	for pos, token := range tokens {
		term := normalize(token)
		if term == "" {
			continue
		}

		// Add to inverted index
		if fti.index[term] == nil {
			fti.index[term] = make(map[uint64][]int)
		}
		fti.index[term][node.ID] = append(fti.index[term][node.ID], pos)

		// Update document frequency (once per term per document)
		if !seenTerms[term] {
			fti.docFreq[term]++
			seenTerms[term] = true
		}
	}

	fti.totalDocs++
}

// UpdateNode updates the index for a specific node
func (fti *FullTextIndex) UpdateNode(nodeID uint64) error {
	fti.indexMu.Lock()
	defer fti.indexMu.Unlock()

	// Remove old index entries for this node
	for term := range fti.index {
		if _, exists := fti.index[term][nodeID]; exists {
			delete(fti.index[term], nodeID)
			fti.docFreq[term]--
			if fti.docFreq[term] == 0 {
				delete(fti.docFreq, term)
			}
			if len(fti.index[term]) == 0 {
				delete(fti.index, term)
			}
		}
	}

	// Remove from node content
	delete(fti.nodeContent, nodeID)
	fti.totalDocs--

	// Reindex the node
	node, err := fti.gs.GetNode(nodeID)
	if err != nil {
		return err
	}

	fti.indexNode(node, fti.indexedProps)
	return nil
}
