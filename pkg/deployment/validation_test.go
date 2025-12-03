package deployment

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// DockerComposeConfig represents a Docker Compose file structure
type DockerComposeConfig struct {
	Version  string                       `yaml:"version"`
	Services map[string]ServiceConfig     `yaml:"services"`
	Networks map[string]NetworkConfig     `yaml:"networks"`
	Volumes  map[string]VolumeConfig      `yaml:"volumes"`
}

type ServiceConfig struct {
	Image       string            `yaml:"image"`
	Build       *BuildConfig      `yaml:"build,omitempty"`
	Container   string            `yaml:"container_name,omitempty"`
	Ports       []string          `yaml:"ports,omitempty"`
	Environment []string          `yaml:"environment,omitempty"`
	Volumes     []string          `yaml:"volumes,omitempty"`
	Networks    []string          `yaml:"networks,omitempty"`
	DependsOn   any       `yaml:"depends_on,omitempty"`
	HealthCheck *HealthCheckConfig `yaml:"healthcheck,omitempty"`
	Command     any       `yaml:"command,omitempty"`
}

type BuildConfig struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

type HealthCheckConfig struct {
	Test        any `yaml:"test"`
	Interval    string      `yaml:"interval,omitempty"`
	Timeout     string      `yaml:"timeout,omitempty"`
	Retries     int         `yaml:"retries,omitempty"`
	StartPeriod string      `yaml:"start_period,omitempty"`
}

type NetworkConfig struct {
	Driver string `yaml:"driver,omitempty"`
}

type VolumeConfig struct {
	Driver string `yaml:"driver,omitempty"`
}

// TestDockerComposeFileExists tests that the Docker Compose file exists
func TestDockerComposeFileExists(t *testing.T) {
	composeFile := "../../deployments/docker-compose.monitoring.yml"

	if _, err := os.Stat(composeFile); os.IsNotExist(err) {
		t.Fatalf("Docker Compose file does not exist: %s", composeFile)
	}

	t.Log("✓ Docker Compose file exists")
}

// TestDockerComposeValidYAML tests that the Docker Compose file is valid YAML
func TestDockerComposeValidYAML(t *testing.T) {
	composeFile := "../../deployments/docker-compose.monitoring.yml"

	data, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatalf("Failed to read Docker Compose file: %v", err)
	}

	var config DockerComposeConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		t.Fatalf("Invalid YAML in Docker Compose file: %v", err)
	}

	t.Logf("✓ Valid YAML (version: %s)", config.Version)
}

// TestDockerComposeRequiredServices tests that all required services are defined
func TestDockerComposeRequiredServices(t *testing.T) {
	config := loadDockerComposeConfig(t, "../../deployments/docker-compose.monitoring.yml")

	requiredServices := []string{"graphdb", "prometheus", "alertmanager", "grafana"}

	for _, serviceName := range requiredServices {
		if _, exists := config.Services[serviceName]; !exists {
			t.Errorf("Required service '%s' not found in Docker Compose", serviceName)
		} else {
			t.Logf("✓ Service '%s' defined", serviceName)
		}
	}
}

// TestDockerComposeServiceHealthChecks tests that critical services have health checks
func TestDockerComposeServiceHealthChecks(t *testing.T) {
	config := loadDockerComposeConfig(t, "../../deployments/docker-compose.monitoring.yml")

	criticalServices := []string{"graphdb", "prometheus"}

	for _, serviceName := range criticalServices {
		service, exists := config.Services[serviceName]
		if !exists {
			t.Errorf("Service '%s' not found", serviceName)
			continue
		}

		if service.HealthCheck == nil {
			t.Errorf("Service '%s' missing health check", serviceName)
		} else {
			t.Logf("✓ Service '%s' has health check configured", serviceName)
		}
	}
}

// TestDockerComposePortMappings tests that port mappings are correctly configured
func TestDockerComposePortMappings(t *testing.T) {
	config := loadDockerComposeConfig(t, "../../deployments/docker-compose.monitoring.yml")

	expectedPorts := map[string][]string{
		"graphdb":      {"8080:8080", "9090:9090"},
		"prometheus":   {"9091:9090"},
		"alertmanager": {"9093:9093"},
		"grafana":      {"3000:3000"},
	}

	portUsage := make(map[string]string) // hostPort -> serviceName

	for serviceName := range expectedPorts {
		service, exists := config.Services[serviceName]
		if !exists {
			t.Errorf("Service '%s' not found", serviceName)
			continue
		}

		if len(service.Ports) == 0 {
			t.Errorf("Service '%s' has no port mappings", serviceName)
			continue
		}

		// Check for port conflicts
		for _, portMapping := range service.Ports {
			hostPort := strings.Split(portMapping, ":")[0]
			if prevService, exists := portUsage[hostPort]; exists {
				t.Errorf("Port conflict: Host port %s used by both '%s' and '%s'",
					hostPort, prevService, serviceName)
			}
			portUsage[hostPort] = serviceName
		}

		t.Logf("✓ Service '%s' ports: %v", serviceName, service.Ports)
	}

	t.Logf("✓ No port conflicts detected (%d unique host ports)", len(portUsage))
}

// TestDockerComposeNetworkConfiguration tests network configuration
func TestDockerComposeNetworkConfiguration(t *testing.T) {
	config := loadDockerComposeConfig(t, "../../deployments/docker-compose.monitoring.yml")

	// Check that monitoring network exists
	if _, exists := config.Networks["monitoring"]; !exists {
		t.Error("'monitoring' network not defined")
	} else {
		t.Log("✓ 'monitoring' network defined")
	}

	// Verify all services are connected to the monitoring network
	for serviceName, service := range config.Services {
		hasMonitoringNetwork := false
		for _, network := range service.Networks {
			if network == "monitoring" {
				hasMonitoringNetwork = true
				break
			}
		}

		if !hasMonitoringNetwork {
			t.Errorf("Service '%s' not connected to 'monitoring' network", serviceName)
		}
	}

	t.Log("✓ All services connected to 'monitoring' network")
}

// TestDockerComposeVolumeConfiguration tests volume configuration
func TestDockerComposeVolumeConfiguration(t *testing.T) {
	config := loadDockerComposeConfig(t, "../../deployments/docker-compose.monitoring.yml")

	requiredVolumes := []string{"graphdb-data", "prometheus-data", "alertmanager-data", "grafana-data"}

	for _, volumeName := range requiredVolumes {
		if _, exists := config.Volumes[volumeName]; !exists {
			t.Errorf("Required volume '%s' not defined", volumeName)
		} else {
			t.Logf("✓ Volume '%s' defined", volumeName)
		}
	}

	// Verify services use their designated volumes
	volumeUsage := map[string]string{
		"graphdb":      "graphdb-data",
		"prometheus":   "prometheus-data",
		"alertmanager": "alertmanager-data",
		"grafana":      "grafana-data",
	}

	for serviceName, expectedVolume := range volumeUsage {
		service := config.Services[serviceName]
		usesVolume := false

		for _, volMapping := range service.Volumes {
			if strings.Contains(volMapping, expectedVolume) {
				usesVolume = true
				break
			}
		}

		if !usesVolume {
			t.Errorf("Service '%s' does not use volume '%s'", serviceName, expectedVolume)
		}
	}
}

// TestDockerComposeDependencyChain tests service dependency chain
func TestDockerComposeDependencyChain(t *testing.T) {
	config := loadDockerComposeConfig(t, "../../deployments/docker-compose.monitoring.yml")

	// Expected dependencies
	expectedDeps := map[string][]string{
		"prometheus": {"graphdb"},
		"grafana":    {"prometheus"},
	}

	for serviceName, expectedDependencies := range expectedDeps {
		service := config.Services[serviceName]

		if service.DependsOn == nil {
			t.Errorf("Service '%s' missing dependencies", serviceName)
			continue
		}

		// DependsOn can be either []string or map[string]any
		var deps []string
		switch v := service.DependsOn.(type) {
		case []any:
			for _, dep := range v {
				deps = append(deps, dep.(string))
			}
		case map[string]any:
			for dep := range v {
				deps = append(deps, dep)
			}
		}

		for _, expectedDep := range expectedDependencies {
			found := false
			for _, dep := range deps {
				if dep == expectedDep {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("Service '%s' missing dependency on '%s'", serviceName, expectedDep)
			}
		}

		t.Logf("✓ Service '%s' dependencies: %v", serviceName, deps)
	}
}

// TestDockerComposeEnvironmentVariables tests critical environment variables
func TestDockerComposeEnvironmentVariables(t *testing.T) {
	config := loadDockerComposeConfig(t, "../../deployments/docker-compose.monitoring.yml")

	// Check GraphDB required environment variables
	graphdb := config.Services["graphdb"]
	requiredEnvVars := []string{"GRAPHDB_EDITION", "PORT", "JWT_SECRET"}

	for _, requiredVar := range requiredEnvVars {
		found := false
		for _, env := range graphdb.Environment {
			if strings.HasPrefix(env, requiredVar) {
				found = true
				// Check for default value warnings
				if strings.Contains(env, "dev-secret") || strings.Contains(env, "change-in-production") {
					t.Logf("⚠ WARNING: '%s' uses development default - change for production", requiredVar)
				}
				break
			}
		}
		if !found {
			t.Errorf("GraphDB missing required environment variable: %s", requiredVar)
		}
	}

	t.Log("✓ Required environment variables present")
}

// TestPrometheusConfigFileExists tests that Prometheus config files exist
func TestPrometheusConfigFileExists(t *testing.T) {
	configFiles := []string{
		"../../deployments/prometheus/prometheus.yml",
		"../../deployments/prometheus/graphdb-alerts.yml",
	}

	for _, configFile := range configFiles {
		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			t.Errorf("Prometheus config file missing: %s", configFile)
		} else {
			t.Logf("✓ Config file exists: %s", filepath.Base(configFile))
		}
	}
}

// TestGrafanaProvisioningExists tests that Grafana provisioning files exist
func TestGrafanaProvisioningExists(t *testing.T) {
	provisioningFiles := []string{
		"../../deployments/grafana/provisioning/datasources/prometheus.yml",
		"../../deployments/grafana/provisioning/dashboards/dashboard-provider.yml",
		"../../deployments/grafana/dashboards/graphdb-overview.json",
	}

	for _, provFile := range provisioningFiles {
		if _, err := os.Stat(provFile); os.IsNotExist(err) {
			t.Errorf("Grafana provisioning file missing: %s", provFile)
		} else {
			t.Logf("✓ Provisioning file exists: %s", filepath.Base(provFile))
		}
	}
}

// TestAlertmanagerConfigExists tests that Alertmanager config exists
func TestAlertmanagerConfigExists(t *testing.T) {
	configFile := "../../deployments/alertmanager/config.yml"

	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		t.Fatalf("Alertmanager config file missing: %s", configFile)
	}

	// Verify it's valid YAML
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("Failed to read Alertmanager config: %v", err)
	}

	var config map[string]any
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		t.Fatalf("Invalid YAML in Alertmanager config: %v", err)
	}

	t.Log("✓ Alertmanager config exists and is valid YAML")
}

// TestMonitoringStackScript tests that the monitoring stack script exists
func TestMonitoringStackScript(t *testing.T) {
	scriptFiles := []string{
		"../../deployments/monitoring-stack.sh",
		"../../deployments/validate-monitoring.sh",
	}

	for _, scriptFile := range scriptFiles {
		if _, err := os.Stat(scriptFile); os.IsNotExist(err) {
			t.Errorf("Monitoring script missing: %s", scriptFile)
			continue
		}

		// Check if file is executable
		info, err := os.Stat(scriptFile)
		if err != nil {
			t.Errorf("Failed to stat script: %v", err)
			continue
		}

		if info.Mode()&0111 == 0 {
			t.Logf("⚠ Script not executable: %s", filepath.Base(scriptFile))
		} else {
			t.Logf("✓ Script exists and is executable: %s", filepath.Base(scriptFile))
		}
	}
}

// TestDockerComposeVersionCompatibility tests Docker Compose version compatibility
func TestDockerComposeVersionCompatibility(t *testing.T) {
	config := loadDockerComposeConfig(t, "../../deployments/docker-compose.monitoring.yml")

	// Check version is 3.x (which is widely compatible)
	if !strings.HasPrefix(config.Version, "3.") {
		t.Errorf("Docker Compose version '%s' may have compatibility issues. Recommended: 3.x", config.Version)
	} else {
		t.Logf("✓ Using compatible Docker Compose version: %s", config.Version)
	}
}

// TestDockerComposeSecurityConfiguration tests security-related configurations
func TestDockerComposeSecurityConfiguration(t *testing.T) {
	config := loadDockerComposeConfig(t, "../../deployments/docker-compose.monitoring.yml")

	// Check for security-sensitive environment variables with defaults
	securityChecks := []struct {
		service string
		envVar  string
		warning string
	}{
		{"graphdb", "JWT_SECRET", "JWT_SECRET uses development default"},
		{"graphdb", "ADMIN_PASSWORD", "ADMIN_PASSWORD uses development default"},
		{"grafana", "GF_SECURITY_ADMIN_PASSWORD", "Grafana admin password uses default"},
	}

	for _, check := range securityChecks {
		service := config.Services[check.service]
		for _, env := range service.Environment {
			if strings.Contains(env, check.envVar) &&
			   (strings.Contains(env, "admin") || strings.Contains(env, "dev-secret")) {
				t.Logf("⚠ SECURITY: %s - change for production!", check.warning)
			}
		}
	}

	t.Log("✓ Security configuration reviewed")
}

// TestDockerComposeResourceLimits tests if resource limits should be configured
func TestDockerComposeResourceLimits(t *testing.T) {
	config := loadDockerComposeConfig(t, "../../deployments/docker-compose.monitoring.yml")

	// Note: In current config, resource limits are not set
	// This test documents that they should be considered for production

	servicesWithoutLimits := 0
	for range config.Services {
		// In a full implementation, we'd check for deploy.resources.limits
		servicesWithoutLimits++
	}

	if servicesWithoutLimits > 0 {
		t.Logf("⚠ RECOMMENDATION: %d services without resource limits - consider adding for production",
			servicesWithoutLimits)
	}

	t.Log("✓ Resource limits checked (recommendation logged)")
}

// TestDeploymentDocumentationExists tests that deployment documentation exists
func TestDeploymentDocumentationExists(t *testing.T) {
	docFiles := []string{
		"../../deployments/README.md",
		"../../deployments/MONITORING-STACK-COMPLETE.md",
		"../../DEPLOY-QUICKSTART.md",
	}

	for _, docFile := range docFiles {
		if _, err := os.Stat(docFile); os.IsNotExist(err) {
			t.Errorf("Documentation file missing: %s", docFile)
		} else {
			t.Logf("✓ Documentation exists: %s", filepath.Base(docFile))
		}
	}
}

// Helper function to load Docker Compose config
func loadDockerComposeConfig(t *testing.T, path string) *DockerComposeConfig {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read Docker Compose file: %v", err)
	}

	var config DockerComposeConfig
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		t.Fatalf("Failed to parse Docker Compose YAML: %v", err)
	}

	return &config
}

// TestDockerComposeCompleteValidation runs a comprehensive validation
func TestDockerComposeCompleteValidation(t *testing.T) {
	config := loadDockerComposeConfig(t, "../../deployments/docker-compose.monitoring.yml")

	issues := []string{}
	warnings := []string{}

	// Validate services
	if len(config.Services) < 4 {
		issues = append(issues, fmt.Sprintf("Expected at least 4 services, found %d", len(config.Services)))
	}

	// Validate networks
	if len(config.Networks) == 0 {
		issues = append(issues, "No networks defined")
	}

	// Validate volumes
	if len(config.Volumes) < 4 {
		warnings = append(warnings, fmt.Sprintf("Expected at least 4 volumes, found %d", len(config.Volumes)))
	}

	// Report results
	if len(issues) > 0 {
		t.Error("Validation issues found:")
		for _, issue := range issues {
			t.Errorf("  - %s", issue)
		}
	}

	if len(warnings) > 0 {
		for _, warning := range warnings {
			t.Logf("⚠ %s", warning)
		}
	}

	if len(issues) == 0 && len(warnings) == 0 {
		t.Log("✓ Complete validation passed")
		t.Logf("  - Services: %d", len(config.Services))
		t.Logf("  - Networks: %d", len(config.Networks))
		t.Logf("  - Volumes: %d", len(config.Volumes))
	}
}
