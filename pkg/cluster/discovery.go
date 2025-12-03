package cluster

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// NodeDiscovery handles node registration and discovery using seed nodes
//
// Concurrent Safety:
// 1. Start/Stop use sync.Once to ensure single initialization/cleanup
// 2. Background goroutine (announceLoop) respects stopCh for clean shutdown
// 3. Uses membership's thread-safe methods for node registration
type NodeDiscovery struct {
	config           ClusterConfig
	membership       *ClusterMembership
	announceInterval time.Duration
	stopCh           chan struct{}
	running          bool
	runningMu        sync.Mutex
	startOnce        sync.Once
	stopOnce         sync.Once
}

// AnnouncementMessage is sent to seed nodes to register presence
type AnnouncementMessage struct {
	MessageType string    `json:"message_type"` // "node_announcement"
	NodeID      string    `json:"node_id"`
	NodeAddr    string    `json:"node_addr"`
	Role        NodeRole  `json:"role"`
	Epoch       uint64    `json:"epoch"`
	Term        uint64    `json:"term"`
	Priority    int       `json:"priority"`
	Timestamp   time.Time `json:"timestamp"`
}

// DiscoveryResponse is returned from seed nodes
type DiscoveryResponse struct {
	MessageType string     `json:"message_type"` // "node_list"
	Nodes       []NodeInfo `json:"nodes"`
	Success     bool       `json:"success"`
	Error       string     `json:"error,omitempty"`
}

// NewNodeDiscovery creates a new discovery service
func NewNodeDiscovery(config ClusterConfig, membership *ClusterMembership) *NodeDiscovery {
	return &NodeDiscovery{
		config:           config,
		membership:       membership,
		announceInterval: 30 * time.Second, // Announce every 30 seconds
		stopCh:           make(chan struct{}),
		running:          false,
	}
}

// Start begins the discovery process
func (nd *NodeDiscovery) Start() error {
	var startErr error
	nd.startOnce.Do(func() {
		nd.runningMu.Lock()
		defer nd.runningMu.Unlock()

		if nd.running {
			startErr = fmt.Errorf("discovery already running")
			return
		}

		// Initial discovery from seed nodes
		if err := nd.discoverFromSeeds(); err != nil {
			log.Printf("Warning: Initial seed discovery failed: %v", err)
			// Continue anyway - seeds may be unreachable initially
		}

		// Start periodic announcement
		go nd.announceLoop()

		nd.running = true
		log.Printf("Node discovery started (seeds: %v)", nd.config.SeedNodes)
	})

	return startErr
}

// Stop stops the discovery process
func (nd *NodeDiscovery) Stop() error {
	var stopErr error
	nd.stopOnce.Do(func() {
		nd.runningMu.Lock()
		defer nd.runningMu.Unlock()

		if !nd.running {
			stopErr = fmt.Errorf("discovery not running")
			return
		}

		close(nd.stopCh)
		nd.running = false
		log.Printf("Node discovery stopped")
	})

	return stopErr
}

// discoverFromSeeds contacts seed nodes to discover cluster members
func (nd *NodeDiscovery) discoverFromSeeds() error {
	if len(nd.config.SeedNodes) == 0 {
		return ErrNoHealthySeeds
	}

	localNode := nd.membership.GetLocalNode()
	announcement := AnnouncementMessage{
		MessageType: "node_announcement",
		NodeID:      localNode.ID,
		NodeAddr:    localNode.Addr,
		Role:        localNode.Role,
		Epoch:       localNode.Epoch,
		Term:        localNode.Term,
		Priority:    localNode.Priority,
		Timestamp:   time.Now(),
	}

	successCount := 0
	for _, seedAddr := range nd.config.SeedNodes {
		// Skip if seed is ourselves
		if seedAddr == localNode.Addr {
			continue
		}

		if err := nd.announceTo(seedAddr, announcement); err != nil {
			log.Printf("Failed to announce to seed %s: %v", seedAddr, err)
			continue
		}

		successCount++
	}

	if successCount == 0 {
		return ErrNoHealthySeeds
	}

	log.Printf("Successfully discovered %d seed nodes", successCount)
	return nil
}

// announceTo sends an announcement to a specific seed node
func (nd *NodeDiscovery) announceTo(addr string, announcement AnnouncementMessage) error {
	// Connect with timeout
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	// Set deadline for the whole operation
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	// Send announcement
	// Note: json.NewEncoder does not return an error - it always succeeds
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(announcement); err != nil {
		return fmt.Errorf("failed to send announcement: %w", err)
	}

	// Receive response with node list
	// Note: json.NewDecoder does not return an error - it always succeeds
	decoder := json.NewDecoder(conn)
	var response DiscoveryResponse
	if err := decoder.Decode(&response); err != nil {
		return fmt.Errorf("failed to receive response: %w", err)
	}

	if !response.Success {
		return fmt.Errorf("announcement rejected: %s", response.Error)
	}

	// Register discovered nodes
	for _, nodeInfo := range response.Nodes {
		// Skip self
		if nodeInfo.ID == nd.membership.GetLocalNode().ID {
			continue
		}

		// Try to add node (ignore if already exists)
		if err := nd.membership.AddNode(nodeInfo); err != nil && err != ErrNodeAlreadyExists {
			log.Printf("Warning: Failed to add discovered node %s: %v", nodeInfo.ID, err)
		} else if err == nil {
			log.Printf("Discovered new node: %s (%s) - %s", nodeInfo.ID, nodeInfo.Addr, nodeInfo.Role)
		}
	}

	return nil
}

// announceLoop periodically announces this node to seed nodes
func (nd *NodeDiscovery) announceLoop() {
	ticker := time.NewTicker(nd.announceInterval)
	defer ticker.Stop()

	for {
		select {
		case <-nd.stopCh:
			return
		case <-ticker.C:
			if err := nd.discoverFromSeeds(); err != nil {
				log.Printf("Periodic seed discovery failed: %v", err)
			}
		}
	}
}

// HandleAnnouncement processes an announcement from another node (server-side)
// This is called when we receive a discovery request from another node
func (nd *NodeDiscovery) HandleAnnouncement(announcement AnnouncementMessage) (*DiscoveryResponse, error) {
	// Validate announcement
	if announcement.NodeID == "" || announcement.NodeAddr == "" {
		return &DiscoveryResponse{
			MessageType: "node_list",
			Success:     false,
			Error:       "invalid announcement: missing node ID or address",
		}, nil
	}

	// Register the announcing node
	nodeInfo := NodeInfo{
		ID:               announcement.NodeID,
		Addr:             announcement.NodeAddr,
		Role:             announcement.Role,
		LastSeen:         time.Now(),
		LastHeartbeatSeq: 0,
		Epoch:            announcement.Epoch,
		Term:             announcement.Term,
		Priority:         announcement.Priority,
	}

	// Add or update node
	if err := nd.membership.AddNode(nodeInfo); err != nil {
		if err == ErrNodeAlreadyExists {
			// Update existing node's timestamp
			nd.membership.UpdateNodeHeartbeat(nodeInfo.ID, 0, nodeInfo.Epoch, nodeInfo.Term)
			log.Printf("Updated existing node from announcement: %s (%s)", nodeInfo.ID, nodeInfo.Addr)
		} else {
			return &DiscoveryResponse{
				MessageType: "node_list",
				Success:     false,
				Error:       fmt.Sprintf("failed to register node: %v", err),
			}, nil
		}
	} else {
		log.Printf("Registered new node from announcement: %s (%s) - %s", nodeInfo.ID, nodeInfo.Addr, nodeInfo.Role)
	}

	// Return current cluster membership
	allNodes := nd.membership.GetAllNodes()
	return &DiscoveryResponse{
		MessageType: "node_list",
		Nodes:       allNodes,
		Success:     true,
	}, nil
}

// GetSeeds returns the configured seed nodes
func (nd *NodeDiscovery) GetSeeds() []string {
	return nd.config.SeedNodes
}

// IsHealthy returns true if at least one seed is reachable
func (nd *NodeDiscovery) IsHealthy() bool {
	localNode := nd.membership.GetLocalNode()

	for _, seedAddr := range nd.config.SeedNodes {
		// Skip self
		if seedAddr == localNode.Addr {
			continue
		}

		// Try to connect
		conn, err := net.DialTimeout("tcp", seedAddr, 1*time.Second)
		if err == nil {
			conn.Close()
			return true
		}
	}

	return false
}
