package algorithms

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/dd0wney/graphdb/pkg/storage"
	"github.com/dd0wney/graphdb/pkg/vector"
)

// EMeansOptions configures graph-native E-means vector clustering.
type EMeansOptions struct {
	K         int     // Number of clusters (required, >= 1)
	Property  string  // Node vector property name (default: "embedding")
	Label     string  // Optional node label filter ("" means all labels)
	NEpochs   float64 // Number of passes over the embeddings (default: 5.0)
	Spherical bool    // Project centroids onto the unit sphere for cosine similarity
	Seed      int64   // Deterministic RNG seed (default: 1234)
}

// DefaultEMeansOptions returns sensible defaults for cosine embedding clustering.
func DefaultEMeansOptions(k int) EMeansOptions {
	return EMeansOptions{
		K:         k,
		Property:  "embedding",
		NEpochs:   5.0,
		Spherical: true,
		Seed:      1234,
	}
}

// EMeansResult holds the clustering result over a tenant's vector-bearing nodes.
// Embeds CommunityDetectionResult for compatibility with community consumers.
type EMeansResult struct {
	*CommunityDetectionResult
	Centroids       [][]float32    // Final (k, d) centroids
	Medoids         map[int]uint64 // Cluster ID -> closest node ID in that cluster
	ImbalanceFactor float64        // k * sum(n_j^2) / n^2 (1.0 = perfectly balanced)
	Objective       float64        // Final-pass mean distance/spherical objective
	Splits          int            // Number of dead-cluster revivals performed
}

// EMeans clusters all vector-bearing nodes across tenants using E-means.
// Multi-tenant API/Cypher callers must use EMeansForTenant.
func EMeans(graph storage.Storage, opts EMeansOptions) (*EMeansResult, error) {
	return eMeansView(context.Background(), newTenantBlindView(graph), opts)
}

// EMeansForTenant clusters vector-bearing nodes within a single tenant subgraph.
func EMeansForTenant(ctx context.Context, graph storage.Storage, opts EMeansOptions, tenantID string) (*EMeansResult, error) {
	return eMeansView(ctx, newTenantScopedView(graph, tenantID), opts)
}

func extractNodeVector(node *storage.Node, propName string) ([]float32, bool) {
	if node == nil || node.Properties == nil {
		return nil, false
	}
	val, exists := node.Properties[propName]
	if !exists {
		return nil, false
	}
	switch val.Type {
	case storage.TypeVector:
		vec, err := val.AsVector()
		if err != nil || len(vec) == 0 {
			return nil, false
		}
		return vec, true
	case storage.TypeFloatArray:
		f64, err := val.AsFloatArray()
		if err != nil || len(f64) == 0 {
			return nil, false
		}
		vec := make([]float32, len(f64))
		for i, f := range f64 {
			vec[i] = float32(f)
		}
		return vec, true
	default:
		return nil, false
	}
}

func nodeHasLabel(node *storage.Node, label string) bool {
	if label == "" {
		return true
	}
	for _, l := range node.Labels {
		if l == label {
			return true
		}
	}
	return false
}

func eMeansView(ctx context.Context, view graphView, opts EMeansOptions) (*EMeansResult, error) {
	if opts.K < 1 {
		return nil, fmt.Errorf("emeans: K must be >= 1, got %d", opts.K)
	}
	if opts.Property == "" {
		opts.Property = "embedding"
	}
	if opts.NEpochs <= 0 {
		opts.NEpochs = 5.0
	}
	if opts.Seed == 0 {
		opts.Seed = 1234
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// ADR 0003: treat incomplete node enumeration as fatal.
	allNodes, err := view.AllNodes()
	if err != nil {
		return nil, err
	}

	// Sort by node ID so clustering is deterministic for a fixed seed.
	sort.Slice(allNodes, func(i, j int) bool {
		return allNodes[i].ID < allNodes[j].ID
	})

	var (
		nodeIDs []uint64
		vectors [][]float32
		dim     int
	)
	for _, n := range allNodes {
		if !nodeHasLabel(n, opts.Label) {
			continue
		}
		vec, ok := extractNodeVector(n, opts.Property)
		if !ok {
			continue
		}
		if dim == 0 {
			dim = len(vec)
		} else if len(vec) != dim {
			return nil, fmt.Errorf("emeans: node %d vector dimension %d != %d", n.ID, len(vec), dim)
		}
		if opts.Spherical {
			vec = vector.Normalize(vec)
		}
		nodeIDs = append(nodeIDs, n.ID)
		vectors = append(vectors, vec)
	}

	if len(vectors) == 0 {
		return &EMeansResult{
			CommunityDetectionResult: &CommunityDetectionResult{
				Communities:   nil,
				NodeCommunity: make(map[uint64]int),
			},
			Medoids: make(map[int]uint64),
		}, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	effK := opts.K
	if effK > len(vectors) {
		effK = len(vectors)
	}

	em, err := vector.NewEmeans(vector.EmeansConfig{
		D:         dim,
		K:         effK,
		NEpochs:   opts.NEpochs,
		Spherical: opts.Spherical,
		Seed:      opts.Seed,
	})
	if err != nil {
		return nil, err
	}

	centroids, labels, err := em.TrainContext(ctx, vectors)
	if err != nil {
		return nil, err
	}
	scores, _, err := em.Assign(vectors)
	if err != nil {
		return nil, err
	}

	nodeCommunity := make(map[uint64]int, len(nodeIDs))
	clusterNodes := make(map[int][]uint64, effK)
	medoids := make(map[int]uint64, effK)
	bestScore := make(map[int]float32, effK)

	for idx, nid := range nodeIDs {
		cid := labels[idx]
		nodeCommunity[nid] = cid
		clusterNodes[cid] = append(clusterNodes[cid], nid)
		sc := scores[idx]
		prev, seen := bestScore[cid]
		// For Spherical=true, Assign returns cosine similarity (higher is closer).
		// For Spherical=false, Assign returns squared L2 distance (lower is closer).
		better := !seen || (opts.Spherical && sc > prev) || (!opts.Spherical && sc < prev)
		if better {
			bestScore[cid] = sc
			medoids[cid] = nid
		}
	}

	communities := make([]*Community, 0, len(clusterNodes))
	for cid := 0; cid < effK; cid++ {
		members := clusterNodes[cid]
		if len(members) == 0 {
			continue
		}
		communities = append(communities, &Community{
			ID:    cid,
			Nodes: members,
			Size:  len(members),
		})
	}

	var imb, obj float64
	if len(em.IterationStats) > 0 {
		last := em.IterationStats[len(em.IterationStats)-1]
		imb = last.ImbalanceFactor
		obj = last.Objective
	} else {
		var countSq float64
		for _, c := range communities {
			countSq += float64(c.Size * c.Size)
		}
		nF := float64(len(vectors))
		imb = float64(effK) * countSq / math.Max(1.0, nF*nF)
	}

	return &EMeansResult{
		CommunityDetectionResult: &CommunityDetectionResult{
			Communities:   communities,
			NodeCommunity: nodeCommunity,
		},
		Centroids:       centroids,
		Medoids:         medoids,
		ImbalanceFactor: imb,
		Objective:       obj,
		Splits:          em.NSplits,
	}, nil
}
