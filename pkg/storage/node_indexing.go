package storage

import "fmt"

// Property index helper methods

// updatePropertyIndexes updates property indexes when a node's properties change
func (gs *GraphStorage) updatePropertyIndexes(nodeID uint64, node *Node, properties map[string]Value) error {
	for k, newValue := range properties {
		if idx, exists := gs.propertyIndexes[k]; exists {
			// Remove old value from index if it exists
			if oldValue, exists := node.Properties[k]; exists {
				if err := idx.Remove(nodeID, oldValue); err != nil {
					return fmt.Errorf("failed to remove from property index %s: %w", k, err)
				}
			}
			// Add new value to index
			if err := idx.Insert(nodeID, newValue); err != nil {
				return fmt.Errorf("failed to insert into property index %s: %w", k, err)
			}
		}
	}
	return nil
}

// insertNodeIntoPropertyIndexes inserts a node into all matching property indexes
func (gs *GraphStorage) insertNodeIntoPropertyIndexes(nodeID uint64, properties map[string]Value) error {
	for key, value := range properties {
		if idx, exists := gs.propertyIndexes[key]; exists {
			if err := idx.Insert(nodeID, value); err != nil {
				return fmt.Errorf("failed to insert into property index %s: %w", key, err)
			}
		}
	}
	return nil
}

// removeNodeFromPropertyIndexes removes a node from all property indexes
func (gs *GraphStorage) removeNodeFromPropertyIndexes(nodeID uint64, properties map[string]Value) error {
	for key, value := range properties {
		if idx, exists := gs.propertyIndexes[key]; exists {
			if err := idx.Remove(nodeID, value); err != nil {
				return fmt.Errorf("failed to remove from property index %s: %w", key, err)
			}
		}
	}
	return nil
}

// removeFromLabelIndex removes a node from a label index
func (gs *GraphStorage) removeFromLabelIndex(label string, nodeID uint64) {
	nodeIDs := gs.nodesByLabel[label]
	for i, id := range nodeIDs {
		if id == nodeID {
			// Remove by swapping with last element and truncating
			nodeIDs[i] = nodeIDs[len(nodeIDs)-1]
			gs.nodesByLabel[label] = nodeIDs[:len(nodeIDs)-1]
			break
		}
	}
}
