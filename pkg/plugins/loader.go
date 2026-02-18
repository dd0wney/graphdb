package plugins

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"plugin"
	"strings"
	"sync"

	"github.com/dd0wney/cluso-graphdb/pkg/licensing"
)

// PluginLoader manages Enterprise plugin loading and lifecycle
type PluginLoader struct {
	plugins       []EnterprisePlugin
	pluginsByName map[string]EnterprisePlugin
	license       *licensing.License
	logger        *slog.Logger
	mu            sync.RWMutex
}

// NewPluginLoader creates a new plugin loader
func NewPluginLoader(license *licensing.License, logger *slog.Logger) *PluginLoader {
	return &PluginLoader{
		plugins:       make([]EnterprisePlugin, 0),
		pluginsByName: make(map[string]EnterprisePlugin),
		license:       license,
		logger:        logger,
	}
}

// LoadPluginsFromDir loads all plugins from a directory
// Plugins should be .so files (Go plugins)
func (l *PluginLoader) LoadPluginsFromDir(ctx context.Context, dir string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if directory exists
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		l.logger.Info("plugin directory does not exist, skipping plugin loading", "dir", dir)
		return nil
	}

	l.logger.Info("loading Enterprise plugins", "dir", dir)

	// Find all .so files
	pattern := filepath.Join(dir, "*.so")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to glob plugin directory: %w", err)
	}

	if len(matches) == 0 {
		l.logger.Info("no plugins found in directory", "dir", dir)
		return nil
	}

	// Load each plugin
	for _, path := range matches {
		if err := l.loadPlugin(ctx, path); err != nil {
			l.logger.Error("failed to load plugin", "path", path, "error", err)
			// Continue loading other plugins
			continue
		}
	}

	l.logger.Info("plugin loading complete", "loaded", len(l.plugins))
	return nil
}

// loadPlugin loads a single plugin from a .so file
func (l *PluginLoader) loadPlugin(ctx context.Context, path string) error {
	l.logger.Info("loading plugin", "path", path)

	// Open the plugin
	p, err := plugin.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open plugin: %w", err)
	}

	// Look for the "Plugin" symbol
	symbol, err := p.Lookup("Plugin")
	if err != nil {
		return fmt.Errorf("plugin missing 'Plugin' symbol: %w", err)
	}

	// Assert that it implements EnterprisePlugin
	enterprisePlugin, ok := symbol.(EnterprisePlugin)
	if !ok {
		return fmt.Errorf("plugin does not implement EnterprisePlugin interface")
	}

	// Get plugin metadata
	name := enterprisePlugin.Name()
	version := enterprisePlugin.Version()

	l.logger.Info("plugin loaded", "name", name, "version", version)

	// Initialize the plugin with license and config
	config := l.getPluginConfig(name)
	if err := enterprisePlugin.Initialize(ctx, l.license, config); err != nil {
		return fmt.Errorf("failed to initialize plugin %s: %w", name, err)
	}

	// Store the plugin
	l.plugins = append(l.plugins, enterprisePlugin)
	l.pluginsByName[name] = enterprisePlugin

	l.logger.Info("plugin initialized", "name", name)
	return nil
}

// getPluginConfig retrieves configuration for a plugin from environment variables
// Looks for PLUGIN_<NAME>_<KEY>=<VALUE> environment variables
// Example: PLUGIN_AUDIT_LOG_LEVEL=debug becomes config["LOG_LEVEL"]="debug" for plugin "audit"
func (l *PluginLoader) getPluginConfig(name string) map[string]any {
	config := make(map[string]any)

	// Convert plugin name to uppercase for env var prefix
	// e.g., "audit" -> "PLUGIN_AUDIT_"
	prefix := "PLUGIN_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_"

	// Scan all environment variables for matching prefix
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, prefix) {
			continue
		}

		// Split into key=value
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		// Extract the config key (remove prefix)
		key := strings.TrimPrefix(parts[0], prefix)
		value := parts[1]

		config[key] = value
		l.logger.Debug("loaded plugin config", "plugin", name, "key", key)
	}

	return config
}

// StartAll starts all loaded plugins
func (l *PluginLoader) StartAll(ctx context.Context) error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	l.logger.Info("starting all plugins", "count", len(l.plugins))

	for _, p := range l.plugins {
		if err := p.Start(ctx); err != nil {
			l.logger.Error("failed to start plugin", "name", p.Name(), "error", err)
			// Continue starting other plugins
			continue
		}
		l.logger.Info("plugin started", "name", p.Name())
	}

	return nil
}

// StopAll stops all loaded plugins
func (l *PluginLoader) StopAll(ctx context.Context) error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	l.logger.Info("stopping all plugins", "count", len(l.plugins))

	// Stop in reverse order
	for i := len(l.plugins) - 1; i >= 0; i-- {
		p := l.plugins[i]
		if err := p.Stop(ctx); err != nil {
			l.logger.Error("failed to stop plugin", "name", p.Name(), "error", err)
			// Continue stopping other plugins
			continue
		}
		l.logger.Info("plugin stopped", "name", p.Name())
	}

	return nil
}

// GetPlugin returns a plugin by name
func (l *PluginLoader) GetPlugin(name string) (EnterprisePlugin, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	p, ok := l.pluginsByName[name]
	return p, ok
}

// GetAllPlugins returns all loaded plugins
func (l *PluginLoader) GetAllPlugins() []EnterprisePlugin {
	l.mu.RLock()
	defer l.mu.RUnlock()

	// Return a copy to prevent modification
	plugins := make([]EnterprisePlugin, len(l.plugins))
	copy(plugins, l.plugins)
	return plugins
}

// HealthCheckAll runs health checks on all plugins
func (l *PluginLoader) HealthCheckAll(ctx context.Context) map[string]error {
	l.mu.RLock()
	defer l.mu.RUnlock()

	results := make(map[string]error)
	for _, p := range l.plugins {
		results[p.Name()] = p.HealthCheck(ctx)
	}

	return results
}
