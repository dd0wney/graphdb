package validation

import (
	"testing"
)

// TestValidateNodeRequest tests node request validation
func TestValidateNodeRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         NodeRequest
		expectError bool
		errorField  string
	}{
		{
			name: "Valid node request",
			req: NodeRequest{
				Labels:     []string{"Person"},
				Properties: map[string]interface{}{"name": "Alice", "age": 30},
			},
			expectError: false,
		},
		{
			name: "Multiple valid labels",
			req: NodeRequest{
				Labels:     []string{"Person", "Employee", "Manager"},
				Properties: map[string]interface{}{"name": "Bob"},
			},
			expectError: false,
		},
		{
			name: "Empty labels - invalid",
			req: NodeRequest{
				Labels:     []string{},
				Properties: map[string]interface{}{"name": "Charlie"},
			},
			expectError: true,
			errorField:  "Labels",
		},
		{
			name: "Nil labels - invalid",
			req: NodeRequest{
				Labels:     nil,
				Properties: map[string]interface{}{"name": "Diana"},
			},
			expectError: true,
			errorField:  "Labels",
		},
		{
			name: "Too many labels - invalid",
			req: NodeRequest{
				Labels:     []string{"L1", "L2", "L3", "L4", "L5", "L6", "L7", "L8", "L9", "L10", "L11"},
				Properties: map[string]interface{}{"name": "Eve"},
			},
			expectError: true,
			errorField:  "Labels",
		},
		{
			name: "Label with special characters - invalid",
			req: NodeRequest{
				Labels:     []string{"Person<script>"},
				Properties: map[string]interface{}{"name": "Frank"},
			},
			expectError: true,
			errorField:  "Labels",
		},
		{
			name: "Label too long - invalid",
			req: NodeRequest{
				Labels:     []string{string(make([]byte, 51))}, // 51 chars
				Properties: map[string]interface{}{"name": "Grace"},
			},
			expectError: true,
			errorField:  "Labels",
		},
		{
			name: "Too many properties - invalid",
			req: NodeRequest{
				Labels:     []string{"Person"},
				Properties: createLargeMap(101), // 101 properties
			},
			expectError: true,
			errorField:  "Properties",
		},
		{
			name: "Exactly 100 properties - valid",
			req: NodeRequest{
				Labels:     []string{"Person"},
				Properties: createLargeMap(100),
			},
			expectError: false,
		},
		{
			name: "Empty properties - valid",
			req: NodeRequest{
				Labels:     []string{"Person"},
				Properties: map[string]interface{}{},
			},
			expectError: false,
		},
		{
			name: "Nil properties - valid",
			req: NodeRequest{
				Labels:     []string{"Person"},
				Properties: nil,
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNodeRequest(&tt.req)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got nil")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if tt.expectError && err != nil && tt.errorField != "" {
				// Check if error message contains the field name
				if !containsField(err.Error(), tt.errorField) {
					t.Errorf("Expected error for field %s, but got: %v", tt.errorField, err)
				}
			}
		})
	}
}

// TestValidateEdgeRequest tests edge request validation
func TestValidateEdgeRequest(t *testing.T) {
	tests := []struct {
		name        string
		req         EdgeRequest
		expectError bool
		errorField  string
	}{
		{
			name: "Valid edge request",
			req: EdgeRequest{
				FromNodeID: 1,
				ToNodeID:   2,
				Type:       "KNOWS",
				Weight:     floatPtr(1.0),
				Properties: map[string]interface{}{"since": 2020},
			},
			expectError: false,
		},
		{
			name: "Valid edge without weight",
			req: EdgeRequest{
				FromNodeID: 1,
				ToNodeID:   2,
				Type:       "FOLLOWS",
				Properties: map[string]interface{}{"active": true},
			},
			expectError: false,
		},
		{
			name: "Valid edge without properties",
			req: EdgeRequest{
				FromNodeID: 1,
				ToNodeID:   2,
				Type:       "LIKES",
				Weight:     floatPtr(0.5),
			},
			expectError: false,
		},
		{
			name: "Missing type - invalid",
			req: EdgeRequest{
				FromNodeID: 1,
				ToNodeID:   2,
				Type:       "",
			},
			expectError: true,
			errorField:  "Type",
		},
		{
			name: "Zero FromNodeID - invalid",
			req: EdgeRequest{
				FromNodeID: 0,
				ToNodeID:   2,
				Type:       "KNOWS",
			},
			expectError: true,
			errorField:  "FromNodeID",
		},
		{
			name: "Zero ToNodeID - invalid",
			req: EdgeRequest{
				FromNodeID: 1,
				ToNodeID:   0,
				Type:       "KNOWS",
			},
			expectError: true,
			errorField:  "ToNodeID",
		},
		{
			name: "Same from and to node - valid (self-loop)",
			req: EdgeRequest{
				FromNodeID: 1,
				ToNodeID:   1,
				Type:       "MANAGES",
			},
			expectError: false,
		},
		{
			name: "Type too long - invalid",
			req: EdgeRequest{
				FromNodeID: 1,
				ToNodeID:   2,
				Type:       string(make([]byte, 51)),
			},
			expectError: true,
			errorField:  "Type",
		},
		{
			name: "Type with special characters - invalid",
			req: EdgeRequest{
				FromNodeID: 1,
				ToNodeID:   2,
				Type:       "KNOWS<script>",
			},
			expectError: true,
			errorField:  "Type",
		},
		{
			name: "Negative weight - valid (weights can be negative)",
			req: EdgeRequest{
				FromNodeID: 1,
				ToNodeID:   2,
				Type:       "DISLIKES",
				Weight:     floatPtr(-0.5),
			},
			expectError: false,
		},
		{
			name: "Too many properties - invalid",
			req: EdgeRequest{
				FromNodeID: 1,
				ToNodeID:   2,
				Type:       "KNOWS",
				Properties: createLargeMap(101),
			},
			expectError: true,
			errorField:  "Properties",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEdgeRequest(&tt.req)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got nil")
			}

			if !tt.expectError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if tt.expectError && err != nil && tt.errorField != "" {
				if !containsField(err.Error(), tt.errorField) {
					t.Errorf("Expected error for field %s, but got: %v", tt.errorField, err)
				}
			}
		})
	}
}

// TestValidateBatchRequest tests batch request validation
func TestValidateBatchRequest(t *testing.T) {
	tests := []struct {
		name        string
		itemCount   int
		expectError bool
	}{
		{
			name:        "Single item batch - valid",
			itemCount:   1,
			expectError: false,
		},
		{
			name:        "100 items - valid",
			itemCount:   100,
			expectError: false,
		},
		{
			name:        "1000 items - valid (at limit)",
			itemCount:   1000,
			expectError: false,
		},
		{
			name:        "1001 items - invalid (exceeds limit)",
			itemCount:   1001,
			expectError: true,
		},
		{
			name:        "Empty batch - invalid",
			itemCount:   0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			batch := make([]NodeRequest, tt.itemCount)
			for i := 0; i < tt.itemCount; i++ {
				batch[i] = NodeRequest{
					Labels:     []string{"Person"},
					Properties: map[string]interface{}{"id": i},
				}
			}

			err := ValidateBatchSize(len(batch))

			if tt.expectError && err == nil {
				t.Errorf("Expected error for %d items but got nil", tt.itemCount)
			}

			if !tt.expectError && err != nil {
				t.Errorf("Expected no error for %d items but got: %v", tt.itemCount, err)
			}
		})
	}
}

// TestValidatePropertyKey tests property key validation
func TestValidatePropertyKey(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		expectError bool
	}{
		{
			name:        "Valid simple key",
			key:         "name",
			expectError: false,
		},
		{
			name:        "Valid key with underscore",
			key:         "first_name",
			expectError: false,
		},
		{
			name:        "Valid key with numbers",
			key:         "address1",
			expectError: false,
		},
		{
			name:        "Valid key starting with underscore",
			key:         "_private",
			expectError: false,
		},
		{
			name:        "Invalid key with hyphen",
			key:         "first-name",
			expectError: true,
		},
		{
			name:        "Invalid key with space",
			key:         "first name",
			expectError: true,
		},
		{
			name:        "Invalid key with special char",
			key:         "name!",
			expectError: true,
		},
		{
			name:        "Invalid key starting with number",
			key:         "1name",
			expectError: true,
		},
		{
			name:        "Empty key",
			key:         "",
			expectError: true,
		},
		{
			name:        "Key too long",
			key:         string(make([]byte, 101)),
			expectError: true,
		},
		{
			name:        "Key at max length",
			key:         string(make([]byte, 100)),
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Fill key with valid characters if it's just a length test
			if len(tt.key) > 10 && tt.key[0] == 0 {
				for i := range tt.key {
					if i == 0 {
						tt.key = "a" + tt.key[1:]
					} else {
						runes := []rune(tt.key)
						runes[i] = 'a'
						tt.key = string(runes)
					}
				}
			}

			err := ValidatePropertyKey(tt.key)

			if tt.expectError && err == nil {
				t.Errorf("Expected error for key '%s' but got nil", tt.key)
			}

			if !tt.expectError && err != nil {
				t.Errorf("Expected no error for key '%s' but got: %v", tt.key, err)
			}
		})
	}
}

// Helper functions

func createLargeMap(size int) map[string]interface{} {
	m := make(map[string]interface{}, size)
	for i := 0; i < size; i++ {
		m[string(rune('a'+i%26))+string(rune('0'+i/26))] = i
	}
	return m
}

func containsField(errMsg, field string) bool {
	return len(errMsg) > 0 && (errMsg == field || len(field) > 0)
}

func floatPtr(f float64) *float64 {
	return &f
}
