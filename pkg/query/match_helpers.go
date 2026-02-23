package query

import "github.com/dd0wney/cluso-graphdb/pkg/storage"

func (ms *MatchStep) hasLabels(node *storage.Node, labels []string) bool {
	for _, requiredLabel := range labels {
		found := false
		for _, nodeLabel := range node.Labels {
			if nodeLabel == requiredLabel {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (ms *MatchStep) matchProperties(nodeProps map[string]storage.Value, patternProps map[string]any) bool {
	for key, patternValue := range patternProps {
		nodeValue, exists := nodeProps[key]
		if !exists {
			return false
		}

		// Simple value comparison
		if !ms.valuesEqual(nodeValue, patternValue) {
			return false
		}
	}
	return true
}

func (ms *MatchStep) valuesEqual(nodeValue storage.Value, patternValue any) bool {
	switch v := patternValue.(type) {
	case string:
		nodeStr, err := nodeValue.AsString()
		if err != nil {
			return false // Type mismatch
		}
		return nodeStr == v
	case int64:
		nodeInt, err := nodeValue.AsInt()
		if err != nil {
			return false // Type mismatch
		}
		return nodeInt == v
	case float64:
		nodeFloat, err := nodeValue.AsFloat()
		if err != nil {
			return false // Type mismatch
		}
		return nodeFloat == v
	case bool:
		nodeBool, err := nodeValue.AsBool()
		if err != nil {
			return false // Type mismatch
		}
		return nodeBool == v
	}
	return false
}

func (ms *MatchStep) copyBinding(binding *BindingSet) *BindingSet {
	newBindings := make(map[string]any)
	for k, v := range binding.bindings {
		newBindings[k] = v
	}
	newBS := &BindingSet{bindings: newBindings}
	if len(binding.vectorScores) > 0 {
		newBS.vectorScores = make(map[string]float64, len(binding.vectorScores))
		for k, v := range binding.vectorScores {
			newBS.vectorScores[k] = v
		}
	}
	return newBS
}
