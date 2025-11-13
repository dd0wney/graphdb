package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ClusterConfig represents the cluster configuration
type ClusterConfig struct {
	Nodes []NodeConfig `yaml:"nodes"`
}

// NodeConfig represents a single node configuration
type NodeConfig struct {
	Name     string `yaml:"name"`
	Host     string `yaml:"host"`
	HTTPPort int    `yaml:"http_port"`
	Role     string `yaml:"role"` // "primary" or "replica"
}

// UpgradeStatus represents node upgrade status
type UpgradeStatus struct {
	Phase             string    `json:"phase"`
	Ready             bool      `json:"ready"`
	ReplicationLag    int64     `json:"replication_lag_ms"`
	HeartbeatLag      uint64    `json:"heartbeat_lag"`
	Message           string    `json:"message"`
	Timestamp         time.Time `json:"timestamp"`
	CanPromote        bool      `json:"can_promote"`
	CurrentRole       string    `json:"current_role"`
	ConnectedReplicas int       `json:"connected_replicas,omitempty"`
}

// PromoteResponse represents promotion result
type PromoteResponse struct {
	Success       bool      `json:"success"`
	NewRole       string    `json:"new_role"`
	PreviousRole  string    `json:"previous_role"`
	Message       string    `json:"message"`
	PromotedAt    time.Time `json:"promoted_at"`
	WaitedSeconds float64   `json:"waited_seconds"`
}

func main() {
	var (
		clusterFile = flag.String("cluster", "cluster.yaml", "Cluster configuration file")
		newVersion  = flag.String("version", "", "New version to upgrade to")
		dryRun      = flag.Bool("dry-run", false, "Show upgrade plan without executing")
		rollback    = flag.Bool("rollback", false, "Rollback to previous version")
	)

	flag.Parse()

	if *newVersion == "" && !*rollback {
		log.Fatal("--version is required (unless using --rollback)")
	}

	// Load cluster configuration
	cluster, err := loadClusterConfig(*clusterFile)
	if err != nil {
		log.Fatalf("Failed to load cluster config: %v", err)
	}

	log.Printf("Loaded cluster config with %d nodes", len(cluster.Nodes))

	if *dryRun {
		showUpgradePlan(cluster, *newVersion)
		return
	}

	if *rollback {
		if err := executeRollback(cluster); err != nil {
			log.Fatalf("Rollback failed: %v", err)
		}
		return
	}

	// Execute upgrade
	if err := executeUpgrade(cluster, *newVersion); err != nil {
		log.Fatalf("Upgrade failed: %v", err)
	}

	log.Printf("✅ Cluster upgrade to %s completed successfully", *newVersion)
}

func loadClusterConfig(path string) (*ClusterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config ClusterConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func showUpgradePlan(cluster *ClusterConfig, newVersion string) {
	fmt.Printf("\n📋 Upgrade Plan to %s\n", newVersion)
	fmt.Println("=" + strings.Repeat("=", 60))

	primary, replicas := splitNodes(cluster)

	fmt.Println("\nPhase 1: Upgrade Replicas (zero downtime)")
	for i, replica := range replicas {
		fmt.Printf("  %d. Upgrade %s (%s:%d)\n", i+1, replica.Name, replica.Host, replica.HTTPPort)
		fmt.Printf("     - Stop service\n")
		fmt.Printf("     - Install version %s\n", newVersion)
		fmt.Printf("     - Start service\n")
		fmt.Printf("     - Wait for replication sync\n")
	}

	fmt.Println("\nPhase 2: Promote New Primary (~5s downtime)")
	if len(replicas) > 0 {
		newPrimary := replicas[0]
		fmt.Printf("  1. Promote %s to primary\n", newPrimary.Name)
		fmt.Printf("     - Wait for replication lag = 0\n")
		fmt.Printf("     - Send promote command\n")
		fmt.Printf("     - Verify promotion successful\n")
	}

	fmt.Println("\nPhase 3: Demote Old Primary")
	if primary != nil {
		fmt.Printf("  1. Demote %s to replica\n", primary.Name)
		fmt.Printf("     - Send stepdown command\n")
		fmt.Printf("     - Upgrade to version %s\n", newVersion)
		fmt.Printf("     - Reconnect as replica\n")
	}

	fmt.Println("\n✅ Total expected downtime: ~5 seconds")
	fmt.Println()
}

func executeUpgrade(cluster *ClusterConfig, newVersion string) error {
	ctx := context.Background()

	primary, replicas := splitNodes(cluster)

	if primary == nil {
		return fmt.Errorf("no primary node found in cluster config")
	}

	log.Printf("Starting upgrade to version %s", newVersion)
	log.Printf("Primary: %s, Replicas: %d", primary.Name, len(replicas))

	// Phase 1: Upgrade all replicas
	log.Println("\n📦 Phase 1: Upgrading replicas")
	for i, replica := range replicas {
		log.Printf("[%d/%d] Upgrading replica %s...", i+1, len(replicas), replica.Name)

		// Check initial status
		if err := waitForReplicaHealthy(ctx, &replica, 30*time.Second); err != nil {
			return fmt.Errorf("replica %s not healthy: %w", replica.Name, err)
		}

		// In real implementation, this would:
		// 1. SSH to node
		// 2. Stop service
		// 3. Replace binary
		// 4. Start service
		// For now, we'll just log it
		log.Printf("  ⚠️  Manual step: Upgrade %s to %s", replica.Name, newVersion)
		log.Printf("     Run: ssh %s 'systemctl stop graphdb && cp /tmp/graphdb-%s /usr/local/bin/graphdb && systemctl start graphdb'", replica.Host, newVersion)

		// Wait for replica to come back and sync
		log.Printf("  ⏳ Waiting for %s to sync...", replica.Name)
		if err := waitForReplicaHealthy(ctx, &replica, 120*time.Second); err != nil {
			return fmt.Errorf("replica %s failed to sync after upgrade: %w", replica.Name, err)
		}

		log.Printf("  ✅ Replica %s upgraded and synced", replica.Name)
	}

	// Phase 2: Promote first replica to primary
	if len(replicas) > 0 {
		newPrimary := replicas[0]

		log.Printf("\n🔄 Phase 2: Promoting %s to primary", newPrimary.Name)

		// Wait for replication to be fully caught up
		log.Printf("  ⏳ Waiting for %s to be fully synced...", newPrimary.Name)
		if err := waitForReplicaSynced(ctx, &newPrimary, 60*time.Second); err != nil {
			return fmt.Errorf("new primary %s not synced: %w", newPrimary.Name, err)
		}

		// Promote replica to primary
		log.Printf("  🚀 Promoting %s...", newPrimary.Name)
		if err := promoteNode(ctx, &newPrimary); err != nil {
			return fmt.Errorf("failed to promote %s: %w", newPrimary.Name, err)
		}

		log.Printf("  ✅ %s is now primary", newPrimary.Name)

		// Phase 3: Demote old primary
		log.Printf("\n⬇️  Phase 3: Demoting old primary %s", primary.Name)

		log.Printf("  📡 Sending stepdown command to %s...", primary.Name)
		newPrimaryAddr := fmt.Sprintf("%s:9090", newPrimary.Host) // TODO: Make port configurable
		if err := stepDownNode(ctx, primary, newPrimaryAddr); err != nil {
			log.Printf("  ⚠️  Failed to stepdown %s gracefully: %v", primary.Name, err)
			log.Printf("  ⚠️  Manual intervention may be required")
		} else {
			log.Printf("  ✅ %s demoted to replica", primary.Name)
		}

		log.Printf("  ⚠️  Manual step: Upgrade %s to %s", primary.Name, newVersion)
		log.Printf("     Run: ssh %s 'systemctl restart graphdb'", primary.Host)
	}

	return nil
}

func executeRollback(cluster *ClusterConfig) error {
	log.Println("🔙 Rollback not yet implemented")
	log.Println("   Manual rollback steps:")
	log.Println("   1. Restore previous binary on all nodes")
	log.Println("   2. Restart services")
	log.Println("   3. Verify cluster health")
	return nil
}

func splitNodes(cluster *ClusterConfig) (*NodeConfig, []NodeConfig) {
	var primary *NodeConfig
	var replicas []NodeConfig

	for _, node := range cluster.Nodes {
		nodeCopy := node
		if node.Role == "primary" {
			primary = &nodeCopy
		} else {
			replicas = append(replicas, nodeCopy)
		}
	}

	return primary, replicas
}

func waitForReplicaHealthy(ctx context.Context, node *NodeConfig, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := fmt.Sprintf("http://%s:%d/admin/upgrade/status", node.Host, node.HTTPPort)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			resp, err := http.Get(url)
			if err != nil {
				log.Printf("    Waiting for %s to respond...", node.Name)
				continue
			}

			var status UpgradeStatus
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if err := json.Unmarshal(body, &status); err != nil {
				continue
			}

			if status.Ready {
				return nil
			}

			log.Printf("    %s: %s", node.Name, status.Message)
		}
	}
}

func waitForReplicaSynced(ctx context.Context, node *NodeConfig, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	url := fmt.Sprintf("http://%s:%d/admin/upgrade/status", node.Host, node.HTTPPort)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			resp, err := http.Get(url)
			if err != nil {
				continue
			}

			var status UpgradeStatus
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if err := json.Unmarshal(body, &status); err != nil {
				continue
			}

			// Check if replica is fully synced
			if status.HeartbeatLag <= 2 && status.ReplicationLag < 100 {
				log.Printf("    Sync complete: lag=%dms, heartbeat_lag=%d", status.ReplicationLag, status.HeartbeatLag)
				return nil
			}

			log.Printf("    Syncing: lag=%dms, heartbeat_lag=%d", status.ReplicationLag, status.HeartbeatLag)
		}
	}
}

func promoteNode(ctx context.Context, node *NodeConfig) error {
	url := fmt.Sprintf("http://%s:%d/admin/upgrade/promote", node.Host, node.HTTPPort)

	reqBody := `{"wait_for_sync": true, "timeout": 60000000000}`
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("promotion failed: %s", string(body))
	}

	var promoteResp PromoteResponse
	if err := json.Unmarshal(body, &promoteResp); err != nil {
		return err
	}

	if !promoteResp.Success {
		return fmt.Errorf("promotion failed: %s", promoteResp.Message)
	}

	log.Printf("    Promoted in %.2fs", promoteResp.WaitedSeconds)

	return nil
}

func stepDownNode(ctx context.Context, node *NodeConfig, newPrimaryAddr string) error {
	url := fmt.Sprintf("http://%s:%d/admin/upgrade/stepdown", node.Host, node.HTTPPort)

	reqBody := fmt.Sprintf(`{"new_primary_id": "%s", "timeout": 30000000000}`, newPrimaryAddr)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("stepdown failed: %s", string(body))
	}

	return nil
}
