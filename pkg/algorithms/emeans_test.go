package algorithms

import (
	"context"
	"math/rand"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

func TestEMeansForTenant_ClustersAndIsolatesTenants(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	rng := rand.New(rand.NewSource(77))
	const (
		tenantA     = "tenant-A"
		tenantB     = "tenant-B"
		nPerCluster = 20
	)
	baseDirs := [][]float32{
		{1, 0, 0, 0},
		{0, 1, 0, 0},
		{0, 0, 1, 0},
	}

	var groupIDs [3][]uint64
	for c := 0; c < 3; c++ {
		for i := 0; i < nPerCluster; i++ {
			vec := make([]float32, 4)
			for j := 0; j < 4; j++ {
				vec[j] = baseDirs[c][j] + float32(rng.NormFloat64()*0.04)
			}
			n, err := gs.CreateNodeWithTenant(tenantA, []string{"Concept"}, map[string]storage.Value{
				"embedding": storage.VectorValue(vec),
			})
			if err != nil {
				t.Fatalf("CreateNodeWithTenant: %v", err)
			}
			groupIDs[c] = append(groupIDs[c], n.ID)
		}
	}

	// Add decoy nodes in tenantB; they must not appear in tenantA's clustering.
	for i := 0; i < 15; i++ {
		if _, err := gs.CreateNodeWithTenant(tenantB, []string{"Concept"}, map[string]storage.Value{
			"embedding": storage.VectorValue([]float32{0, 0, 0, 1}),
		}); err != nil {
			t.Fatalf("CreateNodeWithTenant B: %v", err)
		}
	}

	opts := DefaultEMeansOptions(3)
	opts.NEpochs = 3.0
	res, err := EMeansForTenant(context.Background(), gs, opts, tenantA)
	if err != nil {
		t.Fatalf("EMeansForTenant: %v", err)
	}
	if len(res.NodeCommunity) != 3*nPerCluster {
		t.Fatalf("expected %d clustered nodes in tenantA, got %d", 3*nPerCluster, len(res.NodeCommunity))
	}
	if len(res.Communities) != 3 {
		t.Fatalf("expected 3 non-empty communities, got %d", len(res.Communities))
	}
	if len(res.Medoids) != 3 {
		t.Fatalf("expected 3 medoids, got %d", len(res.Medoids))
	}

	// Verify each synthetic group mapped to a distinct cluster ID.
	seen := make(map[int]bool)
	for c := 0; c < 3; c++ {
		cid := res.NodeCommunity[groupIDs[c][0]]
		seen[cid] = true
		for _, nid := range groupIDs[c] {
			if got := res.NodeCommunity[nid]; got != cid {
				t.Fatalf("group %d node %d assigned to %d, want %d", c, nid, got, cid)
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 distinct cluster IDs, got %d", len(seen))
	}
}
