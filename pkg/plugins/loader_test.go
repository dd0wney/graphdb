package plugins

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/licensing"
)

// mockPlugin implements EnterprisePlugin for testing
type mockPlugin struct {
	name            string
	version         string
	features        []string
	initError       error
	startError      error
	stopError       error
	healthError     error
	initialized     bool
	started         bool
	stopped         bool
	healthChecked   bool
}

func (m *mockPlugin) Name() string                    { return m.name }
func (m *mockPlugin) Version() string                 { return m.version }
func (m *mockPlugin) RequiredFeatures() []string      { return m.features }

func (m *mockPlugin) Initialize(ctx context.Context, license *licensing.License, config map[string]any) error {
	m.initialized = true
	return m.initError
}

func (m *mockPlugin) Start(ctx context.Context) error {
	m.started = true
	return m.startError
}

func (m *mockPlugin) Stop(ctx context.Context) error {
	m.stopped = true
	return m.stopError
}

func (m *mockPlugin) HealthCheck(ctx context.Context) error {
	m.healthChecked = true
	return m.healthError
}

// --- NewPluginLoader Tests ---

func TestNewPluginLoader(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	license := &licensing.License{}

	loader := NewPluginLoader(license, logger)
	if loader == nil {
		t.Fatal("NewPluginLoader returned nil")
	}
	if loader.license != license {
		t.Error("License not set correctly")
	}
	if len(loader.plugins) != 0 {
		t.Errorf("Expected 0 plugins, got %d", len(loader.plugins))
	}
}

func TestNewPluginLoader_NilLogger(t *testing.T) {
	// Should work even with nil logger (though not recommended)
	loader := NewPluginLoader(nil, nil)
	if loader == nil {
		t.Fatal("NewPluginLoader returned nil with nil logger")
	}
}

// --- LoadPluginsFromDir Tests ---

func TestLoadPluginsFromDir_NonExistentDir(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	err := loader.LoadPluginsFromDir(context.Background(), "/nonexistent/path/that/does/not/exist")
	if err != nil {
		t.Errorf("Expected no error for non-existent dir, got: %v", err)
	}
}

func TestLoadPluginsFromDir_EmptyDir(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	// Create temp dir
	tmpDir, err := os.MkdirTemp("", "plugin-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	err = loader.LoadPluginsFromDir(context.Background(), tmpDir)
	if err != nil {
		t.Errorf("Expected no error for empty dir, got: %v", err)
	}
	if len(loader.plugins) != 0 {
		t.Errorf("Expected 0 plugins from empty dir, got %d", len(loader.plugins))
	}
}

// --- GetPlugin Tests ---

func TestGetPlugin_NotFound(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	plugin, ok := loader.GetPlugin("nonexistent")
	if ok {
		t.Error("Expected ok to be false for nonexistent plugin")
	}
	if plugin != nil {
		t.Error("Expected nil plugin for nonexistent name")
	}
}

func TestGetPlugin_Found(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	// Manually add a mock plugin
	mock := &mockPlugin{name: "test-plugin", version: "1.0.0"}
	loader.plugins = append(loader.plugins, mock)
	loader.pluginsByName["test-plugin"] = mock

	plugin, ok := loader.GetPlugin("test-plugin")
	if !ok {
		t.Error("Expected ok to be true for existing plugin")
	}
	if plugin == nil {
		t.Error("Expected non-nil plugin")
	}
	if plugin.Name() != "test-plugin" {
		t.Errorf("Expected plugin name 'test-plugin', got '%s'", plugin.Name())
	}
}

// --- GetAllPlugins Tests ---

func TestGetAllPlugins_Empty(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	plugins := loader.GetAllPlugins()
	if plugins == nil {
		t.Error("GetAllPlugins returned nil")
	}
	if len(plugins) != 0 {
		t.Errorf("Expected 0 plugins, got %d", len(plugins))
	}
}

func TestGetAllPlugins_Multiple(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	// Add mock plugins
	loader.plugins = append(loader.plugins, &mockPlugin{name: "plugin1"})
	loader.plugins = append(loader.plugins, &mockPlugin{name: "plugin2"})

	plugins := loader.GetAllPlugins()
	if len(plugins) != 2 {
		t.Errorf("Expected 2 plugins, got %d", len(plugins))
	}

	// Verify it's a copy (modifying shouldn't affect loader)
	plugins[0] = nil
	if loader.plugins[0] == nil {
		t.Error("GetAllPlugins should return a copy")
	}
}

// --- StartAll Tests ---

func TestStartAll_Empty(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	err := loader.StartAll(context.Background())
	if err != nil {
		t.Errorf("StartAll with no plugins should not error: %v", err)
	}
}

func TestStartAll_Success(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	mock1 := &mockPlugin{name: "plugin1"}
	mock2 := &mockPlugin{name: "plugin2"}
	loader.plugins = []EnterprisePlugin{mock1, mock2}

	err := loader.StartAll(context.Background())
	if err != nil {
		t.Errorf("StartAll should not error: %v", err)
	}

	if !mock1.started {
		t.Error("plugin1 should have been started")
	}
	if !mock2.started {
		t.Error("plugin2 should have been started")
	}
}

func TestStartAll_WithError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	mock1 := &mockPlugin{name: "plugin1", startError: errors.New("start failed")}
	mock2 := &mockPlugin{name: "plugin2"}
	loader.plugins = []EnterprisePlugin{mock1, mock2}

	// StartAll continues despite errors
	err := loader.StartAll(context.Background())
	if err != nil {
		t.Errorf("StartAll should not propagate individual errors: %v", err)
	}

	// Both should have been attempted
	if !mock1.started {
		t.Error("plugin1 should have been attempted")
	}
	if !mock2.started {
		t.Error("plugin2 should have been started despite plugin1 error")
	}
}

// --- StopAll Tests ---

func TestStopAll_Empty(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	err := loader.StopAll(context.Background())
	if err != nil {
		t.Errorf("StopAll with no plugins should not error: %v", err)
	}
}

func TestStopAll_Success(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	mock1 := &mockPlugin{name: "plugin1"}
	mock2 := &mockPlugin{name: "plugin2"}
	loader.plugins = []EnterprisePlugin{mock1, mock2}

	err := loader.StopAll(context.Background())
	if err != nil {
		t.Errorf("StopAll should not error: %v", err)
	}

	if !mock1.stopped {
		t.Error("plugin1 should have been stopped")
	}
	if !mock2.stopped {
		t.Error("plugin2 should have been stopped")
	}
}

func TestStopAll_WithError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	mock1 := &mockPlugin{name: "plugin1"}
	mock2 := &mockPlugin{name: "plugin2", stopError: errors.New("stop failed")}
	loader.plugins = []EnterprisePlugin{mock1, mock2}

	// StopAll continues despite errors
	err := loader.StopAll(context.Background())
	if err != nil {
		t.Errorf("StopAll should not propagate individual errors: %v", err)
	}

	// Both should have been attempted
	if !mock1.stopped {
		t.Error("plugin1 should have been stopped")
	}
	if !mock2.stopped {
		t.Error("plugin2 should have been attempted despite error")
	}
}

// --- HealthCheckAll Tests ---

func TestHealthCheckAll_Empty(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	results := loader.HealthCheckAll(context.Background())
	if results == nil {
		t.Error("HealthCheckAll should not return nil")
	}
	if len(results) != 0 {
		t.Errorf("Expected 0 results, got %d", len(results))
	}
}

func TestHealthCheckAll_AllHealthy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	mock1 := &mockPlugin{name: "plugin1"}
	mock2 := &mockPlugin{name: "plugin2"}
	loader.plugins = []EnterprisePlugin{mock1, mock2}
	loader.pluginsByName["plugin1"] = mock1
	loader.pluginsByName["plugin2"] = mock2

	results := loader.HealthCheckAll(context.Background())

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}
	if results["plugin1"] != nil {
		t.Errorf("Expected plugin1 to be healthy, got error: %v", results["plugin1"])
	}
	if results["plugin2"] != nil {
		t.Errorf("Expected plugin2 to be healthy, got error: %v", results["plugin2"])
	}
}

func TestHealthCheckAll_SomeUnhealthy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	loader := NewPluginLoader(nil, logger)

	mock1 := &mockPlugin{name: "plugin1"}
	mock2 := &mockPlugin{name: "plugin2", healthError: errors.New("unhealthy")}
	loader.plugins = []EnterprisePlugin{mock1, mock2}
	loader.pluginsByName["plugin1"] = mock1
	loader.pluginsByName["plugin2"] = mock2

	results := loader.HealthCheckAll(context.Background())

	if results["plugin1"] != nil {
		t.Errorf("Expected plugin1 to be healthy, got error: %v", results["plugin1"])
	}
	if results["plugin2"] == nil {
		t.Error("Expected plugin2 to be unhealthy")
	}
}

// --- getPluginConfig Tests ---

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
