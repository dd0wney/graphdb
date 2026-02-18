package plugins

import (
	"log/slog"
	"os"
	"testing"
)

func TestGetPluginConfig(t *testing.T) {
	// Create a loader with a test logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	tests := []struct {
		name       string
		pluginName string
		envVars    map[string]string
		expected   map[string]string
	}{
		{
			name:       "no config",
			pluginName: "test",
			envVars:    map[string]string{},
			expected:   map[string]string{},
		},
		{
			name:       "single config",
			pluginName: "audit",
			envVars: map[string]string{
				"PLUGIN_AUDIT_LOG_LEVEL": "debug",
			},
			expected: map[string]string{
				"LOG_LEVEL": "debug",
			},
		},
		{
			name:       "multiple configs",
			pluginName: "backup",
			envVars: map[string]string{
				"PLUGIN_BACKUP_INTERVAL":   "3600",
				"PLUGIN_BACKUP_DESTINATION": "/var/backup",
				"PLUGIN_BACKUP_COMPRESS":   "true",
			},
			expected: map[string]string{
				"INTERVAL":    "3600",
				"DESTINATION": "/var/backup",
				"COMPRESS":    "true",
			},
		},
		{
			name:       "ignores other plugins",
			pluginName: "audit",
			envVars: map[string]string{
				"PLUGIN_AUDIT_LOG_LEVEL":   "debug",
				"PLUGIN_BACKUP_INTERVAL":   "3600",
				"OTHER_VAR":                "value",
			},
			expected: map[string]string{
				"LOG_LEVEL": "debug",
			},
		},
		{
			name:       "handles hyphens in plugin name",
			pluginName: "my-plugin",
			envVars: map[string]string{
				"PLUGIN_MY_PLUGIN_SETTING": "value",
			},
			expected: map[string]string{
				"SETTING": "value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for k, v := range tt.envVars {
				os.Setenv(k, v)
				defer os.Unsetenv(k)
			}

			config := loader.getPluginConfig(tt.pluginName)

			// Check expected values
			for expectedKey, expectedValue := range tt.expected {
				if gotValue, ok := config[expectedKey]; !ok {
					t.Errorf("missing expected key %q", expectedKey)
				} else if gotValue != expectedValue {
					t.Errorf("config[%q] = %q, want %q", expectedKey, gotValue, expectedValue)
				}
			}

			// Check no extra values
			for gotKey := range config {
				if _, expected := tt.expected[gotKey]; !expected {
					t.Errorf("unexpected key %q in config", gotKey)
				}
			}
		})
	}
}
