package storage

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/dd0wney/cluso-graphdb/pkg/tenantid"
)

// assertGraphInvariants verifies that every DERIVED representation of the graph
// agrees with the AUTHORITATIVE shard maps, failing the test with one error per
// divergence. See checkGraphInvariants for the full contract; this is the thin
// *testing.T wrapper retrofitted into tests.
func assertGraphInvariants(t *testing.T, gs *GraphStorage) {
	t.Helper()
	for _, v := range checkGraphInvariants(gs) {
		t.Error("invariant violation: " + v)
	}
}

// checkGraphInvariants returns one string per violated invariant (empty slice ==
// healthy). It is the testable core of assertGraphInvariants — a teeth-test
// drives it against deliberately-corrupted state to prove the checks fire.
//
// graphdb stores the same truth many times over — global label/type indexes,
// per-tenant indexes, per-tenant enumeration sets, global + per-tenant counts,
// adjacency lists, and the vector index — and every write path must keep them
// all in lockstep. The silent bugs this guards against (#288, #298, #305, #307,
// #308) were each a write path that updated one representation and forgot
// another; tests asserting only the global GetNode/GetEdge projection never saw
// the drift. Ground truth is rebuilt in one pass over nodeShards/edgeShards
// (which hold the real Node/Edge structs); every other structure is checked
// against it.
//
// USAGE CONSTRAINTS (each a real trap):
//  1. Reads raw fields + lock-free unexported helpers ONLY — never an exported
//     Get*/Count*. Those re-take gs.mu.RLock and would deadlock (reentrant RLock
//     with a writer queued between the two acquisitions).
//  2. Caller guarantees quiescence — no concurrent writer. Holding gs.mu.RLock
//     already excludes shard writes (every shard mutation takes gs.mu.Lock +
//     shardLock per the A4 discipline), so this is lock-correct, not merely
//     racy-by-convention.
//  3. Asserts LIVE in-memory state — do NOT call it across a reopen EXCEPT in a
//     crash-recovery test where post-recovery rebuild is the thing under test. A
//     stray reopen rebuilds the derived indexes from the flat shard set and
//     self-heals the very drift we hunt (the CC6 discipline).
func checkGraphInvariants(gs *GraphStorage) []string {
	var violations []string
	report := func(format string, args ...any) {
		violations = append(violations, fmt.Sprintf(format, args...))
	}

	gs.mu.RLock()
	defer gs.mu.RUnlock()

	type idSet = map[uint64]struct{}

	// --- snapshot the vector index shape first (nested vi.mu under gs.mu, the
	// production lock order) so the node pass can count only INDEXED props. ---
	indexedLen := map[tenantid.TenantID]map[string]int{} // tid -> prop -> HNSW Len()
	gs.vectorIndex.mu.RLock()
	for tid, inner := range gs.vectorIndex.indexes {
		indexedLen[tid] = map[string]int{}
		for prop, idx := range inner {
			indexedLen[tid][prop] = idx.Len()
		}
	}
	gs.vectorIndex.mu.RUnlock()

	// --- ground truth: NODES ---
	gtNodeIDs := map[tenantid.TenantID]idSet{}               // tid -> node IDs
	gtNodeLabels := map[tenantid.TenantID]map[string]idSet{} // tid -> label -> IDs
	gtGlobalNodeLabels := map[string]idSet{}                 // global label -> IDs
	gtVecCount := map[tenantid.TenantID]map[string]int{}     // tid -> indexed prop -> decodable-vector count
	gtNodeCount := 0

	for i := range gs.nodeShards {
		for id, node := range gs.nodeShards[i] {
			tid := effectiveTenantID(node.TenantID)
			gtNodeCount++
			if gtNodeIDs[tid] == nil {
				gtNodeIDs[tid] = idSet{}
			}
			gtNodeIDs[tid][id] = struct{}{}
			for _, label := range node.Labels {
				if gtNodeLabels[tid] == nil {
					gtNodeLabels[tid] = map[string]idSet{}
				}
				if gtNodeLabels[tid][label] == nil {
					gtNodeLabels[tid][label] = idSet{}
				}
				gtNodeLabels[tid][label][id] = struct{}{}
				if gtGlobalNodeLabels[label] == nil {
					gtGlobalNodeLabels[label] = idSet{}
				}
				gtGlobalNodeLabels[label][id] = struct{}{}
			}
			// vector ground truth: only for props this tenant actually indexes.
			for prop := range indexedLen[tid] {
				propVal, ok := node.Properties[prop]
				if !ok {
					continue
				}
				if _, isVec, err := vectorFromProperty(propVal); isVec && err == nil {
					if gtVecCount[tid] == nil {
						gtVecCount[tid] = map[string]int{}
					}
					gtVecCount[tid][prop]++
				}
			}
		}
	}

	// --- ground truth: EDGES ---
	type endpoints struct{ from, to uint64 }
	gtEdgeIDs := map[tenantid.TenantID]idSet{}
	gtEdgeTypes := map[tenantid.TenantID]map[string]idSet{}
	gtGlobalEdgeTypes := map[string]idSet{}
	gtEdgeEnds := map[uint64]endpoints{}
	gtEdgeCount := 0

	for i := range gs.edgeShards {
		for id, edge := range gs.edgeShards[i] {
			tid := effectiveTenantID(edge.TenantID)
			gtEdgeCount++
			gtEdgeEnds[id] = endpoints{edge.FromNodeID, edge.ToNodeID}
			if gtEdgeIDs[tid] == nil {
				gtEdgeIDs[tid] = idSet{}
			}
			gtEdgeIDs[tid][id] = struct{}{}
			if gtEdgeTypes[tid] == nil {
				gtEdgeTypes[tid] = map[string]idSet{}
			}
			if gtEdgeTypes[tid][edge.Type] == nil {
				gtEdgeTypes[tid][edge.Type] = idSet{}
			}
			gtEdgeTypes[tid][edge.Type][id] = struct{}{}
			if gtGlobalEdgeTypes[edge.Type] == nil {
				gtGlobalEdgeTypes[edge.Type] = idSet{}
			}
			gtGlobalEdgeTypes[edge.Type][id] = struct{}{}
		}
	}

	// === COUNT CHAINS (global) ===
	if got := int(atomic.LoadUint64(&gs.stats.NodeCount)); got != gtNodeCount {
		report("count: stats.NodeCount=%d != shard node count=%d", got, gtNodeCount)
	}
	if got := gs.nodeCount(); got != gtNodeCount {
		report("count: nodeCount()=%d != shard node count=%d", got, gtNodeCount)
	}
	if got := int(atomic.LoadUint64(&gs.stats.EdgeCount)); got != gtEdgeCount {
		report("count: stats.EdgeCount=%d != shard edge count=%d", got, gtEdgeCount)
	}
	if got := gs.edgeCount(); got != gtEdgeCount {
		report("count: edgeCount()=%d != shard edge count=%d", got, gtEdgeCount)
	}

	// === PER-TENANT counts + enumeration sets ===
	// Union of every tenant key that appears in any tenant-scoped structure, so a
	// tenant present in one map but missing from another is caught (not skipped).
	tenantsSeen := map[tenantid.TenantID]struct{}{}
	for _, m := range []map[tenantid.TenantID]idSet{gtNodeIDs, gtEdgeIDs} {
		for tid := range m {
			tenantsSeen[tid] = struct{}{}
		}
	}
	for tid := range gs.tenantStats {
		tenantsSeen[tid] = struct{}{}
	}
	for tid := range gs.tenantNodeIDs {
		tenantsSeen[tid] = struct{}{}
	}
	for tid := range gs.tenantEdgeIDs {
		tenantsSeen[tid] = struct{}{}
	}
	for tid := range gs.tenantNodesByLabel {
		tenantsSeen[tid] = struct{}{}
	}
	for tid := range gs.tenantEdgesByType {
		tenantsSeen[tid] = struct{}{}
	}

	sumTenantNodes, sumTenantEdges := 0, 0
	for tid := range tenantsSeen {
		wantNodes := len(gtNodeIDs[tid])
		wantEdges := len(gtEdgeIDs[tid])

		var statNodes, statEdges int
		if ts := gs.tenantStats[tid]; ts != nil {
			statNodes = int(atomic.LoadUint64(&ts.NodeCount))
			statEdges = int(atomic.LoadUint64(&ts.EdgeCount))
		}
		sumTenantNodes += statNodes
		sumTenantEdges += statEdges

		if statNodes != wantNodes {
			report("tenant %q: tenantStats.NodeCount=%d != shard nodes=%d", tid, statNodes, wantNodes)
		}
		if statEdges != wantEdges {
			report("tenant %q: tenantStats.EdgeCount=%d != shard edges=%d", tid, statEdges, wantEdges)
		}
		if got := len(gs.tenantNodeIDs[tid]); got != wantNodes {
			report("tenant %q: len(tenantNodeIDs)=%d != shard nodes=%d", tid, got, wantNodes)
		}
		if got := len(gs.tenantEdgeIDs[tid]); got != wantEdges {
			report("tenant %q: len(tenantEdgeIDs)=%d != shard edges=%d", tid, got, wantEdges)
		}
		reportSetDiff(report, "tenantNodeIDs", tid, gs.tenantNodeIDs[tid], gtNodeIDs[tid])
		reportSetDiff(report, "tenantEdgeIDs", tid, gs.tenantEdgeIDs[tid], gtEdgeIDs[tid])
	}
	if sumTenantNodes != gtNodeCount {
		report("count: Σ tenantStats.NodeCount=%d != shard node count=%d", sumTenantNodes, gtNodeCount)
	}
	if sumTenantEdges != gtEdgeCount {
		report("count: Σ tenantStats.EdgeCount=%d != shard edge count=%d", sumTenantEdges, gtEdgeCount)
	}

	// === LABEL / TYPE forward (strict): every entity is in both index scopes ===
	for tid, byLabel := range gtNodeLabels {
		for label, ids := range byLabel {
			for id := range ids {
				if !inBucket(gs.nodesByLabel[label], id) {
					report("label: node %d (label %q) missing from GLOBAL nodesByLabel", id, label)
				}
				if !inBucket(gs.tenantNodesByLabel[tid][label], id) {
					report("label: node %d (label %q) missing from tenant %q nodesByLabel", id, label, tid)
				}
			}
		}
	}
	for tid, byType := range gtEdgeTypes {
		for typ, ids := range byType {
			for id := range ids {
				if !inBucket(gs.edgesByType[typ], id) {
					report("type: edge %d (type %q) missing from GLOBAL edgesByType", id, typ)
				}
				if !inBucket(gs.tenantEdgesByType[tid][typ], id) {
					report("type: edge %d (type %q) missing from tenant %q edgesByType", id, typ, tid)
				}
			}
		}
	}

	// === LABEL / TYPE reverse (scope-asymmetric) ===
	// GLOBAL: members must be live + carrying the key, but EMPTY buckets are
	// allowed (removeFromLabelIndexKeepEmpty keeps labels registered).
	reportReverseGlobal(report, "nodesByLabel", gs.nodesByLabel, gtGlobalNodeLabels)
	reportReverseGlobal(report, "edgesByType", gs.edgesByType, gtGlobalEdgeTypes)
	// PER-TENANT: members must be live + carrying it AND no empty buckets exist
	// (per-tenant uses removeFromLabelIndexSet — an empty bucket is a bug).
	for tid, buckets := range gs.tenantNodesByLabel {
		reportReverseTenant(report, "tenantNodesByLabel", tid, buckets, gtNodeLabels[tid])
	}
	for tid, buckets := range gs.tenantEdgesByType {
		reportReverseTenant(report, "tenantEdgesByType", tid, buckets, gtEdgeTypes[tid])
	}

	// === ADJACENCY (both directions) ===
	// Forward: every live edge is in its endpoints' adjacency lists.
	for id, ends := range gtEdgeEnds {
		if !containsUint64(gs.getEdgeIDsForNode(ends.from, true), id) {
			report("adj: edge %d missing from node %d outgoing adjacency", id, ends.from)
		}
		if !containsUint64(gs.getEdgeIDsForNode(ends.to, false), id) {
			report("adj: edge %d missing from node %d incoming adjacency", id, ends.to)
		}
	}
	// Reverse: every adjacency entry points to a live edge with matching endpoint
	// (catches dangling adjacency after a cascade delete — the #307 class).
	for i := range gs.nodeShards {
		for nodeID := range gs.nodeShards[i] {
			for _, eid := range gs.getEdgeIDsForNode(nodeID, true) {
				ends, ok := gtEdgeEnds[eid]
				if !ok {
					report("adj: node %d outgoing lists edge %d that no longer exists (dangling)", nodeID, eid)
				} else if ends.from != nodeID {
					report("adj: node %d outgoing lists edge %d whose source is actually %d", nodeID, eid, ends.from)
				}
			}
			for _, eid := range gs.getEdgeIDsForNode(nodeID, false) {
				ends, ok := gtEdgeEnds[eid]
				if !ok {
					report("adj: node %d incoming lists edge %d that no longer exists (dangling)", nodeID, eid)
				} else if ends.to != nodeID {
					report("adj: node %d incoming lists edge %d whose target is actually %d", nodeID, eid, ends.to)
				}
			}
		}
	}

	// === VECTOR index (count-only, per tenant) ===
	for tid, props := range indexedLen {
		for prop, length := range props {
			want := 0
			if gtVecCount[tid] != nil {
				want = gtVecCount[tid][prop]
			}
			if length != want {
				report("vector: index (tenant %q, prop %q) Len()=%d != decodable-vector node count=%d", tid, prop, length, want)
			}
		}
	}

	return violations
}

type reportFunc = func(format string, args ...any)

// reportSetDiff reports any asymmetry between an actual ID set and ground truth.
func reportSetDiff(report reportFunc, name string, tid tenantid.TenantID, got, want map[uint64]struct{}) {
	for id := range got {
		if _, ok := want[id]; !ok {
			report("%s tenant %q: contains id %d not backed by a live shard entity", name, tid, id)
		}
	}
	for id := range want {
		if _, ok := got[id]; !ok {
			report("%s tenant %q: missing id %d that a live shard entity owns", name, tid, id)
		}
	}
}

// reportReverseGlobal: every NON-empty global bucket's members must be live
// entities carrying that key. Empty buckets are intentional (keep-empty).
func reportReverseGlobal(report reportFunc, name string, idx labelIndex, gt map[string]map[uint64]struct{}) {
	for key, bucket := range idx {
		for id := range bucket {
			if _, ok := gt[key][id]; !ok {
				report("%s: bucket %q lists id %d that no live entity carries", name, key, id)
			}
		}
	}
}

// reportReverseTenant: per-tenant buckets must have NO empties and every member
// must be a live tenant-entity carrying the key.
func reportReverseTenant(report reportFunc, name string, tid tenantid.TenantID, idx labelIndex, gt map[string]map[uint64]struct{}) {
	for key, bucket := range idx {
		if len(bucket) == 0 {
			report("%s tenant %q: empty bucket %q (per-tenant indexes must GC empties)", name, tid, key)
			continue
		}
		for id := range bucket {
			if _, ok := gt[key][id]; !ok {
				report("%s tenant %q: bucket %q lists id %d that no live tenant entity carries", name, tid, key, id)
			}
		}
	}
}

func inBucket(bucket map[uint64]struct{}, id uint64) bool {
	if bucket == nil {
		return false
	}
	_, ok := bucket[id]
	return ok
}

func containsUint64(s []uint64, want uint64) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
