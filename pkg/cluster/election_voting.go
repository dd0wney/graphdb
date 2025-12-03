package cluster

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"time"
)

// requestVotes sends vote requests to all cluster members
func (em *ElectionManager) requestVotes(term uint64, candidate *NodeInfo) {
	allNodes := em.membership.GetAllNodes()
	voteChan := make(chan VoteResponse, len(allNodes))

	// Send vote requests to all nodes (except self)
	requestsSent := 0
	for _, node := range allNodes {
		if node.ID == candidate.ID {
			continue
		}

		requestsSent++
		// Spawn goroutine with cleanup check
		nodeAddr := node.Addr
		go func() {
			select {
			case <-em.stopCh:
				return
			default:
				em.sendVoteRequest(nodeAddr, term, candidate, voteChan)
			}
		}()
	}

	// Collect votes
	em.collectVotes(term, requestsSent, voteChan)
}

// sendVoteRequest sends a vote request to a specific node
func (em *ElectionManager) sendVoteRequest(addr string, term uint64, candidate *NodeInfo, voteChan chan<- VoteResponse) {
	request := VoteRequest{
		MessageType: "vote_request",
		CandidateID: candidate.ID,
		Term:        term,
		LastLSN:     candidate.LastLSN,
		Epoch:       candidate.Epoch,
		Priority:    candidate.Priority,
	}

	// Connect with timeout
	conn, err := net.DialTimeout("tcp", addr, em.config.VoteRequestTimeout)
	if err != nil {
		log.Printf("Failed to connect to %s for vote: %v", addr, err)
		return
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(em.config.VoteRequestTimeout))

	// Send request
	// Note: json.NewEncoder does not return an error - it always succeeds
	encoder := json.NewEncoder(conn)
	if err := encoder.Encode(request); err != nil {
		log.Printf("Failed to send vote request to %s: %v", addr, err)
		return
	}

	// Receive response
	// Note: json.NewDecoder does not return an error - it always succeeds
	decoder := json.NewDecoder(conn)
	var response VoteResponse
	if err := decoder.Decode(&response); err != nil {
		log.Printf("Failed to receive vote response from %s: %v", addr, err)
		return
	}

	voteChan <- response
}

// collectVotes tallies votes and determines election outcome
func (em *ElectionManager) collectVotes(term uint64, expectedVotes int, voteChan <-chan VoteResponse) {
	timeout := time.After(em.config.VoteRequestTimeout)
	votesReceived := 0
	votesGranted := 1 // Already voted for self

	quorum := em.config.MinQuorumSize

	for votesReceived < expectedVotes {
		select {
		case <-timeout:
			log.Printf("Vote collection timed out (received %d/%d votes)", votesReceived, expectedVotes)
			return

		case response := <-voteChan:
			votesReceived++

			em.mu.Lock()

			// Ignore votes for old terms
			if response.Term < term {
				em.mu.Unlock()
				continue
			}

			// Step down if we see a higher term
			if response.Term > term {
				log.Printf("Discovered higher term %d during election - stepping down", response.Term)
				em.becomeFollowerLocked(response.Term)
				em.mu.Unlock()
				return
			}

			// Count the vote
			if response.VoteGranted {
				em.voteGranted[response.VoterID] = true
				votesGranted++
				log.Printf("Received vote from %s (total: %d/%d)", response.VoterID, votesGranted, quorum)
			} else {
				log.Printf("Vote denied by %s: %s", response.VoterID, response.Reason)
			}

			// Check if we won
			if votesGranted >= quorum && em.state == StateCandidate {
				log.Printf("Won election for term %d with %d votes", term, votesGranted)
				em.becomeLeaderLocked()
				em.mu.Unlock()
				return
			}

			em.mu.Unlock()
		}
	}

	// All votes received but didn't win
	em.mu.Lock()
	if em.state == StateCandidate {
		log.Printf("Lost election for term %d (votes: %d/%d)", term, votesGranted, quorum)
		// Stay as candidate - timeout will trigger new election
	}
	em.mu.Unlock()
}

// HandleVoteRequest processes a vote request from a candidate
func (em *ElectionManager) HandleVoteRequest(request VoteRequest) VoteResponse {
	em.mu.Lock()
	defer em.mu.Unlock()

	localNode := em.membership.GetLocalNode()

	response := VoteResponse{
		MessageType: "vote_response",
		VoterID:     localNode.ID,
		Term:        em.currentTerm,
		VoteGranted: false,
	}

	// Reject if term is stale
	if request.Term < em.currentTerm {
		response.Reason = fmt.Sprintf("stale term (current: %d, requested: %d)", em.currentTerm, request.Term)
		return response
	}

	// Step down if we see a higher term
	if request.Term > em.currentTerm {
		em.becomeFollowerLocked(request.Term)
	}

	// Reject if already voted for someone else in this term
	if em.votedFor != "" && em.votedFor != request.CandidateID {
		response.Reason = fmt.Sprintf("already voted for %s in term %d", em.votedFor, em.currentTerm)
		return response
	}

	// Reject if candidate's log is behind
	if request.LastLSN < localNode.LastLSN {
		response.Reason = fmt.Sprintf("candidate LSN %d < local LSN %d", request.LastLSN, localNode.LastLSN)
		return response
	}

	// Grant vote
	em.votedFor = request.CandidateID
	em.lastHeartbeat = time.Now() // Reset election timer
	response.VoteGranted = true
	response.Term = em.currentTerm

	log.Printf("Granted vote to %s for term %d", request.CandidateID, request.Term)

	return response
}
