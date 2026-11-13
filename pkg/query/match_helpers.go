package query

import "github.com/dd0wney/graphdb/pkg/storage"

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
	if isNumericValue(patternValue) {
		return numericPropertyEqual(nodeValue, patternValue)
	}
	switch v := patternValue.(type) {
	case string:
		nodeStr, err := nodeValue.AsString()
		if err != nil {
			return false // Type mismatch
		}
		return nodeStr == v
	case bool:
		nodeBool, err := nodeValue.AsBool()
		if err != nil {
			return false // Type mismatch
		}
		return nodeBool == v
	}
	// Lists and maps: the comparison WHERE n.x = $v uses, so the inline
	// pattern and the WHERE form agree.
	if isCollection(patternValue) {
		return valuesEqual(extractStorageValue(nodeValue), patternValue)
	}
	return false
}

// isNumericStorage reports a stored integer or float.
func isNumericStorage(v storage.Value) bool {
	return v.Type == storage.TypeInt || v.Type == storage.TypeFloat
}

// numericPropertyEqual compares a numeric pattern value with a stored value by
// the rule WHERE n.x = $v uses (valuesEqual), so {id: $v} and WHERE agree. A
// JSON number reaches the executor as float64 whatever the client meant, so a
// type-strict comparison would never find a stored integer. The stored type is
// checked first because extractStorageValue also yields a number for a
// timestamp (Unix seconds), which a numeric pattern must not match.
func numericPropertyEqual(nodeValue storage.Value, patternValue any) bool {
	if !isNumericStorage(nodeValue) {
		return false
	}
	return valuesEqual(extractStorageValue(nodeValue), patternValue)
}

// propertyMatchesPattern is the property comparison MERGE and OPTIONAL MATCH
// use. A numeric pattern value is compared as it arrived: converting it to a
// storage.Value first loses the Go kind (int, int32) that a library caller may
// pass.
func propertyMatchesPattern(stored storage.Value, patternValue any) bool {
	if isNumericValue(patternValue) {
		return numericPropertyEqual(stored, patternValue)
	}
	return compareStorageValue(stored, convertToStorageValue(patternValue))
}

func (ms *MatchStep) copyBinding(binding *BindingSet) *BindingSet {
	// One slot beyond the input: nearly every caller adds the variable it binds.
	newBindings := make(map[string]any, len(binding.bindings)+1)
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
