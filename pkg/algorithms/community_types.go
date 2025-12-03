package algorithms

// Community represents a detected community
type Community struct {
	ID      int
	Nodes   []uint64
	Size    int
	Density float64 // Edge density within community
}

// CommunityDetectionResult contains detected communities
type CommunityDetectionResult struct {
	Communities   []*Community
	Modularity    float64        // Quality measure of the partitioning
	NodeCommunity map[uint64]int // Node ID -> Community ID
}
