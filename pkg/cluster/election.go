package cluster

import (
	"fmt"
	"log"
	"math/rand"
	"time"
)

// Start begins election monitoring
func (em *ElectionManager) Start() error {
	if !em.config.EnableAutoFailover {
		log.Printf("Auto-failover disabled - election manager inactive")
		return nil
	}

	go em.electionTimerLoop()
	log.Printf("Election manager started (timeout: %v)", em.config.ElectionTimeout)
	return nil
}

// Stop stops the election manager
func (em *ElectionManager) Stop() error {
	close(em.stopCh)
	log.Printf("Election manager stopped")
	return nil
}

// electionTimerLoop monitors for leader failures and triggers elections
func (em *ElectionManager) electionTimerLoop() {
	// Randomize initial wait to avoid thundering herd
	jitter := time.Duration(rand.Int63n(int64(em.config.ElectionTimeout / 2)))
	time.Sleep(jitter)

	ticker := time.NewTicker(em.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-em.stopCh:
			return
		case <-ticker.C:
			em.checkElectionTimeout()
		}
	}
}

// checkElectionTimeout checks if we should start an election
func (em *ElectionManager) checkElectionTimeout() {
	em.mu.Lock()
	defer em.mu.Unlock()

	// Only followers and candidates check for timeout
	if em.state == StateLeader {
		return
	}

	timeSinceHeartbeat := time.Since(em.lastHeartbeat)
	if timeSinceHeartbeat > em.config.ElectionTimeout {
		log.Printf("Election timeout reached (%v since last heartbeat) - starting election", timeSinceHeartbeat)
		em.startElectionLocked()
	}
}

// StartElection initiates a new election (public method)
func (em *ElectionManager) StartElection() error {
	em.mu.Lock()
	defer em.mu.Unlock()

	return em.startElectionLocked()
}

// startElectionLocked initiates an election (must be called with lock held)
func (em *ElectionManager) startElectionLocked() error {
	// Increment term
	em.currentTerm++
	term := em.currentTerm

	// Transition to candidate
	em.state = StateCandidate
	em.membership.SetLocalRole(RoleCandidate)
	em.membership.SetLocalTerm(term)

	// Vote for self
	localNode := em.membership.GetLocalNode()
	em.votedFor = localNode.ID
	em.voteGranted = map[string]bool{localNode.ID: true}
	em.electionTime = time.Now()

	log.Printf("Starting election for term %d (node: %s, LSN: %d)",
		term, localNode.ID, localNode.LastLSN)

	// Notify callback if registered (non-blocking)
	if em.onBecomeCandidate != nil {
		callback := em.onBecomeCandidate
		go func() {
			select {
			case <-em.stopCh:
				return
			default:
				callback()
			}
		}()
	}

	// Request votes from all nodes (unlock for network operations)
	em.mu.Unlock()
	go func() {
		select {
		case <-em.stopCh:
			return
		default:
			em.requestVotes(term, localNode)
		}
	}()
	em.mu.Lock()

	return nil
}

// becomeLeaderLocked transitions to leader state (must be called with lock held)
func (em *ElectionManager) becomeLeaderLocked() {
	electionDuration := time.Since(em.electionTime)

	em.state = StateLeader
	em.membership.SetLocalRole(RolePrimary)
	em.membership.IncrementEpoch()

	localNode := em.membership.GetLocalNode()
	log.Printf("✅ Became leader for term %d (epoch: %d)", em.currentTerm, localNode.Epoch)

	// Update metrics
	if em.metricsRegistry != nil {
		em.metricsRegistry.ClusterElectionsTotal.WithLabelValues("won").Inc()
		em.metricsRegistry.ClusterElectionDuration.Observe(electionDuration.Seconds())
		em.metricsRegistry.SetClusterRole("primary")
		em.metricsRegistry.ClusterTerm.Set(float64(em.currentTerm))
		em.metricsRegistry.ClusterEpoch.Set(float64(localNode.Epoch))
	}

	// Notify callback if registered (non-blocking)
	if em.onBecomeLeader != nil {
		callback := em.onBecomeLeader
		go func() {
			select {
			case <-em.stopCh:
				return
			default:
				callback()
			}
		}()
	}
}

// becomeFollowerLocked transitions to follower state (must be called with lock held)
func (em *ElectionManager) becomeFollowerLocked(term uint64) {
	oldState := em.state
	em.state = StateFollower
	em.currentTerm = term
	em.votedFor = ""
	em.voteGranted = make(map[string]bool)
	em.lastHeartbeat = time.Now()
	em.membership.SetLocalRole(RoleReplica)
	em.membership.SetLocalTerm(term)

	if oldState != StateFollower {
		log.Printf("Became follower (term: %d)", term)

		// Update metrics
		if em.metricsRegistry != nil {
			em.metricsRegistry.SetClusterRole("replica")
			em.metricsRegistry.ClusterTerm.Set(float64(term))
			// If we were a candidate and lost, track it
			if oldState == StateCandidate {
				em.metricsRegistry.ClusterElectionsTotal.WithLabelValues("lost").Inc()
			}
		}

		// Notify callback if registered (non-blocking)
		if em.onBecomeFollower != nil {
			callback := em.onBecomeFollower
			go func() {
				select {
				case <-em.stopCh:
					return
				default:
					callback()
				}
			}()
		}
	}
}

// ResetElectionTimer resets the election timeout (called on heartbeat receipt)
func (em *ElectionManager) ResetElectionTimer() {
	em.mu.Lock()
	defer em.mu.Unlock()

	em.lastHeartbeat = time.Now()
}

// StepDown forces this node to step down from leader (if it is one)
func (em *ElectionManager) StepDown(term uint64) error {
	em.mu.Lock()
	defer em.mu.Unlock()

	if term <= em.currentTerm && em.state != StateLeader {
		return fmt.Errorf("cannot step down: not leader or term too old")
	}

	log.Printf("Stepping down from leader (term: %d -> %d)", em.currentTerm, term)
	em.becomeFollowerLocked(term)

	return nil
}

// GetState returns the current election state
func (em *ElectionManager) GetState() ElectionState {
	em.mu.Lock()
	defer em.mu.Unlock()

	return em.state
}

// IsLeader returns true if this node is the leader
func (em *ElectionManager) IsLeader() bool {
	return em.GetState() == StateLeader
}

// GetCurrentTerm returns the current election term
func (em *ElectionManager) GetCurrentTerm() uint64 {
	em.mu.Lock()
	defer em.mu.Unlock()

	return em.currentTerm
}

// GetLeaderID returns the ID of the current leader (if known)
func (em *ElectionManager) GetLeaderID() string {
	primary := em.membership.GetPrimary()
	if primary != nil {
		return primary.ID
	}
	return ""
}

// SetCallbacks registers callbacks for state transitions
func (em *ElectionManager) SetCallbacks(onLeader, onFollower, onCandidate func()) {
	em.mu.Lock()
	defer em.mu.Unlock()

	em.onBecomeLeader = onLeader
	em.onBecomeFollower = onFollower
	em.onBecomeCandidate = onCandidate
}
