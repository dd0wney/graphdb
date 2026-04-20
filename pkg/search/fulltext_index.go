package search

import (
	"fmt"
	"strings"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// IndexPrepared indexes a pre-collected set of nodes under the given
// labels and properties. Use when the caller has scoped the node set
// (e.g. to a single tenant) that IndexNodes's label-based storage
// lookup can't express. Replaces any existing entries for these nodes.
func (fti *FullTextIndex) IndexPrepared(nodes []*storage.Node, labels, properties []string) error {
	fti.indexMu.Lock()
	defer fti.indexMu.Unlock()

	fti.indexedLabels = labels
	fti.indexedProps = properties

	for _, node := range nodes {
		// Remove any existing entries for this node so rebuilds are idempotent.
		fti.removeNodeLocked(node.ID)
		fti.indexNode(node, properties)
	}
	return nil
}

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

	// Tokenize and index. termSet is the per-node reverse posting —
	// saved at the end so UpdateNode / RemoveNode can iterate only
	// the terms this node contains, not the full vocabulary.
	tokens := tokenize(content)
	termSet := make(map[string]struct{})

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
		if _, seen := termSet[term]; !seen {
			fti.docFreq[term]++
			termSet[term] = struct{}{}
		}
	}

	if len(termSet) > 0 {
		fti.nodeTerms[node.ID] = termSet
	}
	fti.totalDocs++
}

// removeNodeLocked removes all index entries for nodeID. Must be called
// with the write lock held. O(terms-in-document) instead of O(vocabulary)
// because it iterates only the reverse posting for this node. No-op if
// the node was never indexed — totalDocs is not decremented in that case.
func (fti *FullTextIndex) removeNodeLocked(nodeID uint64) {
	if _, indexed := fti.nodeTerms[nodeID]; !indexed {
		return
	}
	for term := range fti.nodeTerms[nodeID] {
		if postings, ok := fti.index[term]; ok {
			delete(postings, nodeID)
			if len(postings) == 0 {
				delete(fti.index, term)
			}
		}
		fti.docFreq[term]--
		if fti.docFreq[term] <= 0 {
			delete(fti.docFreq, term)
		}
	}
	delete(fti.nodeTerms, nodeID)
	delete(fti.nodeContent, nodeID)
	fti.totalDocs--
}

// UpdateNode updates the index for a specific node
func (fti *FullTextIndex) UpdateNode(nodeID uint64) error {
	fti.indexMu.Lock()
	defer fti.indexMu.Unlock()

	fti.removeNodeLocked(nodeID)

	// Reindex the node
	node, err := fti.gs.GetNode(nodeID)
	if err != nil {
		return err
	}

	fti.indexNode(node, fti.indexedProps)
	return nil
}
