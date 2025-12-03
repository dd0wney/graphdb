package visualization

import (
	"encoding/json"
	"fmt"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// ExportJSON exports the visualization to JSON
func (v *Visualization) ExportJSON() ([]byte, error) {
	type NodeViz struct {
		ID         uint64            `json:"id"`
		Labels     []string          `json:"labels"`
		Properties map[string]string `json:"properties"`
		X          float64           `json:"x"`
		Y          float64           `json:"y"`
	}

	type EdgeViz struct {
		ID         uint64  `json:"id"`
		FromNodeID uint64  `json:"from"`
		ToNodeID   uint64  `json:"to"`
		Type       string  `json:"type"`
		Weight     float64 `json:"weight"`
	}

	type VizData struct {
		Nodes []NodeViz `json:"nodes"`
		Edges []EdgeViz `json:"edges"`
	}

	data := VizData{
		Nodes: make([]NodeViz, 0, len(v.Nodes)),
		Edges: make([]EdgeViz, 0, len(v.Edges)),
	}

	// Convert nodes
	for _, node := range v.Nodes {
		pos := v.Positions[node.ID]
		props := make(map[string]string)

		for key, val := range node.Properties {
			if val.Type == storage.TypeString {
				if str, err := val.AsString(); err == nil {
					props[key] = str
				}
			} else {
				props[key] = fmt.Sprintf("%v", val.Data)
			}
		}

		data.Nodes = append(data.Nodes, NodeViz{
			ID:         node.ID,
			Labels:     node.Labels,
			Properties: props,
			X:          pos.X,
			Y:          pos.Y,
		})
	}

	// Convert edges
	for _, edge := range v.Edges {
		data.Edges = append(data.Edges, EdgeViz{
			ID:         edge.ID,
			FromNodeID: edge.FromNodeID,
			ToNodeID:   edge.ToNodeID,
			Type:       edge.Type,
			Weight:     edge.Weight,
		})
	}

	return json.Marshal(data)
}
