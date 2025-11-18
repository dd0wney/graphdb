package plugins

import (
	"context"

	"github.com/dd0wney/cluso-graphdb/pkg/licensing"
	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// EnterprisePlugin defines the interface for Enterprise feature plugins
// These plugins are loaded only when running Enterprise edition with valid license
type EnterprisePlugin interface {
	// Name returns the plugin name (e.g., "cloudflare-vectorize", "r2-backup")
	Name() string

	// Version returns the plugin version
	Version() string

	// RequiredFeatures returns the edition features this plugin requires
	RequiredFeatures() []string

	// Initialize is called when the plugin is loaded
	// The license parameter contains the validated Enterprise license
	Initialize(ctx context.Context, license *licensing.License, config map[string]interface{}) error

	// Start begins plugin operation (called after all plugins are initialized)
	Start(ctx context.Context) error

	// Stop gracefully shuts down the plugin
	Stop(ctx context.Context) error

	// HealthCheck returns the plugin's health status
	HealthCheck(ctx context.Context) error
}

// StoragePlugin extends EnterprisePlugin for storage-related features
type StoragePlugin interface {
	EnterprisePlugin

	// AttachToStorage integrates the plugin with the graph storage
	AttachToStorage(storage *storage.GraphStorage) error
}

// APIPlugin extends EnterprisePlugin for API-related features
type APIPlugin interface {
	EnterprisePlugin

	// RegisterRoutes adds plugin-specific API endpoints
	// Returns a map of route path -> handler
	RegisterRoutes() map[string]interface{}
}

// BackupPlugin extends EnterprisePlugin for backup features
type BackupPlugin interface {
	EnterprisePlugin

	// Backup performs a backup operation
	Backup(ctx context.Context, destination string) error

	// Restore performs a restore operation
	Restore(ctx context.Context, source string) error

	// ListBackups returns available backups
	ListBackups(ctx context.Context) ([]BackupInfo, error)
}

// BackupInfo contains metadata about a backup
type BackupInfo struct {
	ID        string
	Timestamp int64
	Size      int64
	Location  string
	Metadata  map[string]string
}

// MonitoringPlugin extends EnterprisePlugin for monitoring features
type MonitoringPlugin interface {
	EnterprisePlugin

	// GetMetrics returns plugin-specific metrics
	GetMetrics(ctx context.Context) (map[string]interface{}, error)
}

// PluginMetadata contains plugin information
type PluginMetadata struct {
	Name             string
	Version          string
	Description      string
	Author           string
	License          string
	RequiredFeatures []string
	RequiredVersion  string // Minimum GraphDB version required
}
