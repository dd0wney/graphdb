package cluster

import "errors"

// Configuration errors
var (
	ErrInvalidNodeID           = errors.New("node ID cannot be empty")
	ErrInvalidNodeAddr         = errors.New("node address cannot be empty")
	ErrElectionTimeoutTooSmall = errors.New("election timeout must be greater than heartbeat interval")
	ErrInvalidQuorumSize       = errors.New("quorum size must be at least 1")
	ErrNoSeedNodes             = errors.New("seed nodes required when auto-failover is enabled")
)

// Election errors
var (
	ErrNotLeader            = errors.New("not the current leader")
	ErrAlreadyVoted         = errors.New("already voted in this term")
	ErrStaleTerm            = errors.New("term is older than current term")
	ErrStaleLSN             = errors.New("candidate LSN is behind local LSN")
	ErrElectionTimeout      = errors.New("election timed out without winning")
	ErrInsufficientQuorum   = errors.New("insufficient nodes for quorum")
	ErrDualPrimaryDetected  = errors.New("dual primary detected - split brain")
)

// Membership errors
var (
	ErrNodeNotFound         = errors.New("node not found in membership")
	ErrNodeAlreadyExists    = errors.New("node already exists in membership")
	ErrCannotRemoveSelf     = errors.New("cannot remove self from cluster")
	ErrClusterTooSmall      = errors.New("cluster too small to maintain quorum")
)

// Discovery errors
var (
	ErrNoHealthySeeds       = errors.New("no healthy seed nodes available")
	ErrDiscoveryFailed      = errors.New("node discovery failed")
)
