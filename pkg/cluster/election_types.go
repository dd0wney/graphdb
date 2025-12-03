package cluster

import (
	"sync"
	"time"

	"github.com/dd0wney/cluso-graphdb/pkg/metrics"
)

// ElectionState represents the current state of this node in the election process
type ElectionState int

const (
	// StateFollower is a node following the current leader
	StateFollower ElectionState = iota
	// StateCandidate is a node requesting votes
	StateCandidate
	// StateLeader is the elected leader
	StateLeader
)

// String returns the string representation of an ElectionState
func (s ElectionState) String() string {
	switch s {
	case StateFollower:
		return "follower"
	case StateCandidate:
		return "candidate"
	case StateLeader:
		return "leader"
	default:
		return "unknown"
	}
}

// VoteRequest is sent by candidates to request votes
type VoteRequest struct {
	MessageType string `json:"message_type"` // "vote_request"
	CandidateID string `json:"candidate_id"`
	Term        uint64 `json:"term"`
	LastLSN     uint64 `json:"last_lsn"`
	Epoch       uint64 `json:"epoch"`
	Priority    int    `json:"priority"`
}

// VoteResponse is returned in response to a vote request
type VoteResponse struct {
	MessageType string `json:"message_type"` // "vote_response"
	VoterID     string `json:"voter_id"`
	Term        uint64 `json:"term"`
	VoteGranted bool   `json:"vote_granted"`
	Reason      string `json:"reason,omitempty"`
}

// ElectionManager handles leader election using simplified Raft-style consensus
//
// Concurrent Safety:
// 1. All state access protected by sync.Mutex
// 2. Election timer runs in dedicated goroutine
// 3. Vote collection uses channels to avoid races
// 4. State transitions are atomic (under lock)
type ElectionManager struct {
	config        ClusterConfig
	membership    *ClusterMembership
	state         ElectionState
	currentTerm   uint64
	votedFor      string          // candidate voted for in current term
	voteGranted   map[string]bool // votes received in current election
	electionTime  time.Time       // when current election started
	lastHeartbeat time.Time       // last heartbeat from leader
	stopCh        chan struct{}
	mu            sync.Mutex

	// Callbacks for state changes
	onBecomeLeader    func()
	onBecomeFollower  func()
	onBecomeCandidate func()

	// Metrics
	metricsRegistry *metrics.Registry
}

// NewElectionManager creates a new election manager
func NewElectionManager(config ClusterConfig, membership *ClusterMembership) *ElectionManager {
	return &ElectionManager{
		config:          config,
		membership:      membership,
		state:           StateFollower,
		currentTerm:     0,
		votedFor:        "",
		voteGranted:     make(map[string]bool),
		lastHeartbeat:   time.Now(),
		stopCh:          make(chan struct{}),
		metricsRegistry: metrics.DefaultRegistry(),
	}
}
