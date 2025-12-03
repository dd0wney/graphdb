package cluster

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestNewElectionManager tests creation of election manager
func TestNewElectionManager(t *testing.T) {
	config := DefaultClusterConfig()
	config.NodeID = "node-1"
	config.NodeAddr = "localhost:9090"

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	electionMgr := NewElectionManager(config, membership)

	if electionMgr == nil {
		t.Fatal("Expected non-nil election manager")
	}

	if electionMgr.GetState() != StateFollower {
		t.Errorf("Expected initial state StateFollower, got %v", electionMgr.GetState())
	}

	if electionMgr.GetCurrentTerm() != 0 {
		t.Errorf("Expected initial term 0, got %d", electionMgr.GetCurrentTerm())
	}
}

// TestElectionManagerStart tests starting the election manager
func TestElectionManagerStart(t *testing.T) {
	config := DefaultClusterConfig()
	config.NodeID = "node-1"
	config.NodeAddr = "localhost:9090"
	config.EnableAutoFailover = true

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	electionMgr := NewElectionManager(config, membership)

	err := electionMgr.Start()
	if err != nil {
		t.Fatalf("Failed to start election manager: %v", err)
	}

	// Clean up
	electionMgr.Stop()
}

// TestElectionManagerStartDisabled tests that election manager doesn't start when disabled
func TestElectionManagerStartDisabled(t *testing.T) {
	config := DefaultClusterConfig()
	config.NodeID = "node-1"
	config.NodeAddr = "localhost:9090"
	config.EnableAutoFailover = false // Disabled

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	electionMgr := NewElectionManager(config, membership)

	err := electionMgr.Start()
	if err != nil {
		t.Fatalf("Failed to start election manager: %v", err)
	}

	// Should still be follower and not trigger elections
	time.Sleep(100 * time.Millisecond)
	if electionMgr.GetState() != StateFollower {
		t.Error("Election manager should remain in follower state when disabled")
	}

	electionMgr.Stop()
}

// TestStartElection tests manually triggering an election
func TestStartElection(t *testing.T) {
	config := DefaultClusterConfig()
	config.NodeID = "node-1"
	config.NodeAddr = "localhost:9090"

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	electionMgr := NewElectionManager(config, membership)

	// Start election
	err := electionMgr.StartElection()
	if err != nil {
		t.Fatalf("Failed to start election: %v", err)
	}

	// Should be candidate now
	if electionMgr.GetState() != StateCandidate {
		t.Errorf("Expected StateCandidate after starting election, got %v", electionMgr.GetState())
	}

	// Term should be incremented
	if electionMgr.GetCurrentTerm() != 1 {
		t.Errorf("Expected term 1 after starting election, got %d", electionMgr.GetCurrentTerm())
	}
}

// TestHandleVoteRequest tests vote request handling
func TestHandleVoteRequest(t *testing.T) {
	config := DefaultClusterConfig()
	config.NodeID = "node-1"
	config.NodeAddr = "localhost:9090"

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	membership.SetLocalLSN(100)
	electionMgr := NewElectionManager(config, membership)

	// Test 1: Grant vote to candidate with same term and higher LSN
	request := VoteRequest{
		CandidateID: "node-2",
		Term:        1,
		LastLSN:     150, // Higher than our 100
		Epoch:       0,
		Priority:    1,
	}

	response := electionMgr.HandleVoteRequest(request)
	if !response.VoteGranted {
		t.Errorf("Expected vote to be granted, got denied: %s", response.Reason)
	}

	// Test 2: Reject vote for same term (already voted)
	request2 := VoteRequest{
		CandidateID: "node-3",
		Term:        1,
		LastLSN:     200,
		Epoch:       0,
		Priority:    1,
	}

	response2 := electionMgr.HandleVoteRequest(request2)
	if response2.VoteGranted {
		t.Error("Expected vote to be denied (already voted for node-2)")
	}

	// Test 3: Reject vote for stale term
	request3 := VoteRequest{
		CandidateID: "node-4",
		Term:        0, // Older term
		LastLSN:     200,
		Epoch:       0,
		Priority:    1,
	}

	response3 := electionMgr.HandleVoteRequest(request3)
	if response3.VoteGranted {
		t.Error("Expected vote to be denied (stale term)")
	}

	// Test 4: Reject vote for candidate with stale LSN
	request4 := VoteRequest{
		CandidateID: "node-5",
		Term:        2, // New term
		LastLSN:     50, // Lower than our 100
		Epoch:       0,
		Priority:    1,
	}

	response4 := electionMgr.HandleVoteRequest(request4)
	if response4.VoteGranted {
		t.Error("Expected vote to be denied (candidate LSN too low)")
	}
}

// TestResetElectionTimer tests resetting the election timeout
func TestResetElectionTimer(t *testing.T) {
	config := DefaultClusterConfig()
	config.NodeID = "node-1"
	config.NodeAddr = "localhost:9090"
	config.ElectionTimeout = 100 * time.Millisecond

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	electionMgr := NewElectionManager(config, membership)

	// Reset timer
	electionMgr.ResetElectionTimer()

	// Should remain follower for at least the timeout duration
	time.Sleep(50 * time.Millisecond)
	if electionMgr.GetState() != StateFollower {
		t.Error("Should remain follower after timer reset")
	}
}

// TestStepDown tests stepping down from leader
func TestStepDown(t *testing.T) {
	config := DefaultClusterConfig()
	config.NodeID = "node-1"
	config.NodeAddr = "localhost:9090"

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	electionMgr := NewElectionManager(config, membership)

	// Manually transition to candidate
	electionMgr.StartElection()

	// Verify we're candidate
	if electionMgr.GetState() != StateCandidate {
		t.Fatal("Should be candidate after starting election")
	}

	currentTerm := electionMgr.GetCurrentTerm()

	// Step down with higher term
	err := electionMgr.StepDown(currentTerm + 1)
	if err != nil {
		t.Fatalf("Failed to step down: %v", err)
	}

	// Should be follower now
	if electionMgr.GetState() != StateFollower {
		t.Errorf("Expected StateFollower after stepping down, got %v", electionMgr.GetState())
	}

	// Term should be updated
	if electionMgr.GetCurrentTerm() != currentTerm+1 {
		t.Errorf("Expected term %d after stepping down, got %d", currentTerm+1, electionMgr.GetCurrentTerm())
	}
}

// TestIsLeader tests leader state checking
func TestIsLeader(t *testing.T) {
	config := DefaultClusterConfig()
	config.NodeID = "node-1"
	config.NodeAddr = "localhost:9090"

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	electionMgr := NewElectionManager(config, membership)

	// Initially not leader
	if electionMgr.IsLeader() {
		t.Error("Should not be leader initially")
	}

	// Start election (becomes candidate, not leader yet)
	electionMgr.StartElection()
	if electionMgr.IsLeader() {
		t.Error("Should not be leader as candidate without winning election")
	}
}

// TestGetLeaderID tests getting the current leader ID
func TestGetLeaderID(t *testing.T) {
	config := DefaultClusterConfig()
	config.NodeID = "node-1"
	config.NodeAddr = "localhost:9090"

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	electionMgr := NewElectionManager(config, membership)

	// Initially no leader
	leaderID := electionMgr.GetLeaderID()
	if leaderID != "" {
		t.Errorf("Expected no leader initially, got %s", leaderID)
	}

	// Add a primary node to membership
	membership.AddNode(NodeInfo{
		ID:   "node-2",
		Addr: "localhost:9091",
		Role: RolePrimary,
	})

	// Should now return the primary
	leaderID = electionMgr.GetLeaderID()
	if leaderID != "node-2" {
		t.Errorf("Expected leader ID 'node-2', got '%s'", leaderID)
	}
}

// TestElectionCallbacks tests state transition callbacks
func TestElectionCallbacks(t *testing.T) {
	config := DefaultClusterConfig()
	config.NodeID = "node-1"
	config.NodeAddr = "localhost:9090"

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	electionMgr := NewElectionManager(config, membership)

	// Track callback invocations
	var followerCalled, candidateCalled atomic.Bool

	electionMgr.SetCallbacks(
		func() {}, // onBecomeLeader (not tested here)
		func() { followerCalled.Store(true) },
		func() { candidateCalled.Store(true) },
	)

	// Start election should trigger candidate callback
	electionMgr.StartElection()

	// Give callbacks time to execute (they run in goroutines)
	time.Sleep(50 * time.Millisecond)

	if !candidateCalled.Load() {
		t.Error("Expected candidate callback to be called")
	}

	// Step down should trigger follower callback
	electionMgr.StepDown(electionMgr.GetCurrentTerm() + 1)
	time.Sleep(50 * time.Millisecond)

	if !followerCalled.Load() {
		t.Error("Expected follower callback to be called")
	}
}

// TestTermProgression tests that term numbers increase monotonically
func TestTermProgression(t *testing.T) {
	config := DefaultClusterConfig()
	config.NodeID = "node-1"
	config.NodeAddr = "localhost:9090"

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	electionMgr := NewElectionManager(config, membership)

	initialTerm := electionMgr.GetCurrentTerm()

	// Start multiple elections
	for i := 0; i < 5; i++ {
		electionMgr.StartElection()
		newTerm := electionMgr.GetCurrentTerm()

		if newTerm <= initialTerm {
			t.Errorf("Term should increase monotonically: iteration %d, expected > %d, got %d",
				i, initialTerm, newTerm)
		}

		initialTerm = newTerm
	}
}

// TestVoteRequestWithHigherTerm tests that higher term causes step down
func TestVoteRequestWithHigherTerm(t *testing.T) {
	config := DefaultClusterConfig()
	config.NodeID = "node-1"
	config.NodeAddr = "localhost:9090"

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	membership.SetLocalLSN(100)
	electionMgr := NewElectionManager(config, membership)

	// Start election to become candidate at term 1
	electionMgr.StartElection()

	if electionMgr.GetState() != StateCandidate {
		t.Fatal("Should be candidate")
	}

	currentTerm := electionMgr.GetCurrentTerm()

	// Receive vote request with much higher term
	request := VoteRequest{
		CandidateID: "node-2",
		Term:        currentTerm + 10,
		LastLSN:     150,
		Epoch:       0,
		Priority:    1,
	}

	response := electionMgr.HandleVoteRequest(request)

	// Should step down to follower
	if electionMgr.GetState() != StateFollower {
		t.Errorf("Expected StateFollower after seeing higher term, got %v", electionMgr.GetState())
	}

	// Term should be updated
	if electionMgr.GetCurrentTerm() != currentTerm+10 {
		t.Errorf("Expected term %d, got %d", currentTerm+10, electionMgr.GetCurrentTerm())
	}

	// Vote should be granted
	if !response.VoteGranted {
		t.Error("Expected vote to be granted to higher term candidate")
	}
}

// TestConcurrentElectionAccess tests thread safety of election operations
func TestConcurrentElectionAccess(t *testing.T) {
	config := DefaultClusterConfig()
	config.NodeID = "node-1"
	config.NodeAddr = "localhost:9090"

	membership := NewClusterMembership(config.NodeID, config.NodeAddr)
	electionMgr := NewElectionManager(config, membership)

	var wg sync.WaitGroup

	// Concurrent state reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = electionMgr.GetState()
			_ = electionMgr.GetCurrentTerm()
			_ = electionMgr.IsLeader()
		}()
	}

	// Concurrent vote requests
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			request := VoteRequest{
				CandidateID: nodeID(id),
				Term:        1,
				LastLSN:     100,
				Epoch:       0,
				Priority:    1,
			}
			_ = electionMgr.HandleVoteRequest(request)
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Election manager should still be in consistent state
	state := electionMgr.GetState()
	if state != StateFollower && state != StateCandidate && state != StateLeader {
		t.Errorf("Invalid state after concurrent access: %v", state)
	}
}
