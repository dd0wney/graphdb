package storage

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/dd0wney/graphdb/pkg/tenantid"
)

// ErrInvariantsUnsupported is returned by CheckInvariants for a store whose
// representation it cannot inspect. Since the mmap checker landed, that is one
// case only: an mmap snapshot with no membership directory, where every index
// lookup returns nil and the comparison would produce false violations rather
// than findings.
//
// This is a refusal, never a silent pass. A checker that quietly returns "no
// violations" for a state it cannot inspect is worse than no checker.
var ErrInvariantsUnsupported = errors.New("storage: invariant check unsupported for this store representation")

// CheckInvariants returns one string per violated invariant (an empty slice
// means healthy), or ErrInvariantsUnsupported for a store it cannot inspect.
//
// It dispatches on representation, and the two paths do NOT cover the same
// ground:
//
//   - shard-backed (JSON): every derived structure, including the vector index
//     and the adjacency lists.
//   - mmap-backed: per-tenant and per-label/per-type membership, the tenant
//     list, and edge endpoint integrity. The vector index and adjacency lists
//     have no mmap ground truth yet — see checkInvariantsMmap.
//
// A clean result on the mmap path is therefore a weaker statement than a clean
// result on the JSON path. Do not read them as equivalent.
//
// It lived in a _test.go file until now, which meant the strongest correctness
// statement graphdb owns could not run anywhere but a test binary. It is
// ordinary code so a staging build can call it; SQLite compiles its 6754
// assert() statements into the shipped core for the same reason.
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
func CheckInvariants(gs *GraphStorage) ([]string, error) {
	var violations []string
	report := func(format string, args ...any) {
		violations = append(violations, fmt.Sprintf(format, args...))
	}

	gs.mu.RLock()
	defer gs.mu.RUnlock()

	// Checked under the lock: a concurrent reopen could otherwise swap the
	// representation between the test and the scan.
	if gs.mmapSnap != nil {
		// A snapshot with no membership directory (pre-v4, or a v4 whose
		// section is absent) makes every membership lookup return nil. The
		// checker would then report every live record as omitted: a storm of
		// false violations, not a finding. Refuse instead.
		if gs.mmapSnap.membDir == nil {
			return nil, ErrInvariantsUnsupported
		}
		return checkInvariantsMmap(gs), nil
	}

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

	// --- snapshot the property-index buckets (nested idx.mu under gs.mu, the
	// production lock order: callers hold gs.mu.Lock before idx.mu) so the node
	// pass computes ground truth without holding idx.mu across the shard scan.
	// propertyIndexes is GLOBAL (one map[string]*PropertyIndex, no tenant
	// dimension), so this checks index correctness, not tenant isolation. ---
	type propSnapshot struct {
		idx     *PropertyIndex      // for valueToKey + indexType (both lock-free)
		buckets map[string][]uint64 // value-key -> node IDs
	}
	propIdx := map[string]propSnapshot{} // property key -> snapshot
	for key, idx := range gs.propertyIndexes {
		idx.mu.RLock()
		buckets := make(map[string][]uint64, len(idx.index))
		for v, ids := range idx.index {
			cp := make([]uint64, len(ids))
			copy(cp, ids)
			buckets[v] = cp
		}
		idx.mu.RUnlock()
		propIdx[key] = propSnapshot{idx: idx, buckets: buckets}
	}

	// --- ground truth: NODES ---
	gtNodeIDs := map[tenantid.TenantID]idSet{}               // tid -> node IDs
	gtNodeLabels := map[tenantid.TenantID]map[string]idSet{} // tid -> label -> IDs
	gtGlobalNodeLabels := map[string]idSet{}                 // global label -> IDs
	gtVecCount := map[tenantid.TenantID]map[string]int{}     // tid -> indexed prop -> decodable-vector count
	gtProp := map[string]map[string]idSet{}                  // property key -> value-key -> node IDs
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
			// property-index ground truth: only for indexed keys, and only when
			// the value type matches the index's declared type (Insert rejects
			// mismatches, so they are legitimately absent from the index).
			for key, snap := range propIdx {
				val, ok := node.Properties[key]
				if !ok || val.Type != snap.idx.indexType {
					continue
				}
				vk := snap.idx.valueToKey(val)
				if gtProp[key] == nil {
					gtProp[key] = map[string]idSet{}
				}
				if gtProp[key][vk] == nil {
					gtProp[key][vk] = idSet{}
				}
				gtProp[key][vk][id] = struct{}{}
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

	// === PROPERTY index (exact membership, per indexed key; tenant-blind) ===
	// PropertyIndex.Insert rejects type-mismatched values and does NOT dedup;
	// Remove deletes a bucket once it empties. So the invariant is: no empty
	// buckets, every member is a live node carrying that value, and every
	// qualifying node appears exactly once in the right bucket.
	for key, snap := range propIdx {
		gt := gtProp[key] // value-key -> node IDs (nil if no qualifying nodes)
		// reverse: members live + carrying value; no empty buckets; no duplicates.
		for vk, ids := range snap.buckets {
			if len(ids) == 0 {
				report("property %q: empty bucket %q (Remove must delete empties)", key, vk)
				continue
			}
			seen := idSet{}
			for _, id := range ids {
				if _, dup := seen[id]; dup {
					report("property %q bucket %q: id %d appears more than once", key, vk, id)
					continue
				}
				seen[id] = struct{}{}
				if gt == nil || !inBucket(gt[vk], id) {
					report("property %q bucket %q: lists id %d not backed by a live node carrying that value", key, vk, id)
				}
			}
		}
		// forward: every qualifying node is in the right bucket.
		for vk, ids := range gt {
			for id := range ids {
				if !containsUint64(snap.buckets[vk], id) {
					report("property %q: node %d missing from bucket %q", key, id, vk)
				}
			}
		}
	}

	return violations, nil
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

// checkInvariantsMmap is the mmap-representation counterpart to the shard-based
// checks above. The caller holds gs.mu.RLock.
//
// The JSON checker rebuilds ground truth from nodeShards/edgeShards, which an
// mmap-backed store populates only on demand — that is why CheckInvariants
// refused this representation until now, and why running the shard checker here
// reported health against an empty ground truth.
//
// Ground truth here is the raw record set:
//
//	live = shard-resident records  ∪  (mmap base records − tombstones)
//
// with the shard winning, exactly as resolveNodeRefLocked resolves a read. It
// is built from mmapSnapshot.forEachNodeID/getNode and forEachNodeUnlocked —
// deliberately NOT from the membership helpers, because those already fuse base
// and overlay and are the thing under test. Comparing the membership path
// against itself would pass unconditionally, which is the failure this whole
// checker exists to prevent.
//
// Covered: per-tenant node and edge membership, per-label and per-type
// membership, the tenant list, and edge endpoint integrity.
//
// NOT covered, and deliberately so: the vector index and the adjacency lists.
// Both are checked by the shard path and neither has an mmap ground truth yet.
// Do not read a clean result here as equivalent to a clean result on the JSON
// path.
func checkInvariantsMmap(gs *GraphStorage) []string {
	var violations []string
	report := func(format string, args ...any) {
		violations = append(violations, fmt.Sprintf(format, args...))
	}

	// --- ground truth: raw records, base ∪ overlay, shard wins ---------------
	liveNodes := make(map[uint64]*Node)
	gs.mmapSnap.forEachNodeID(func(id uint64, _ int64) {
		if gs.isNodeDeletedLocked(id) {
			return
		}
		if n, ok := gs.mmapSnap.getNode(id); ok {
			liveNodes[id] = n
		}
	})
	gs.forEachNodeUnlocked(func(n *Node) bool {
		liveNodes[n.ID] = n
		return true
	})

	liveEdges := make(map[uint64]*Edge)
	gs.mmapSnap.forEachEdgeID(func(id uint64, _ int64) {
		if gs.isEdgeDeletedLocked(id) {
			return
		}
		if e, ok := gs.mmapSnap.getEdge(id); ok {
			liveEdges[id] = e
		}
	})
	for i := range gs.edgeShards {
		for id, e := range gs.edgeShards[i] {
			liveEdges[id] = e
		}
	}

	// --- derive the sets the membership indexes claim to hold ---------------
	gtNodesByTenant := map[tenantid.TenantID]map[uint64]struct{}{}
	gtNodesByLabel := map[tenantid.TenantID]map[string]map[uint64]struct{}{}
	for id, n := range liveNodes {
		tid := effectiveTenantID(n.TenantID)
		addToSet(gtNodesByTenant, tid, id)
		for _, label := range n.Labels {
			if gtNodesByLabel[tid] == nil {
				gtNodesByLabel[tid] = map[string]map[uint64]struct{}{}
			}
			if gtNodesByLabel[tid][label] == nil {
				gtNodesByLabel[tid][label] = map[uint64]struct{}{}
			}
			gtNodesByLabel[tid][label][id] = struct{}{}
		}
	}

	gtEdgesByTenant := map[tenantid.TenantID]map[uint64]struct{}{}
	gtEdgesByType := map[tenantid.TenantID]map[string]map[uint64]struct{}{}
	for id, e := range liveEdges {
		tid := effectiveTenantID(e.TenantID)
		addToSet(gtEdgesByTenant, tid, id)
		if gtEdgesByType[tid] == nil {
			gtEdgesByType[tid] = map[string]map[uint64]struct{}{}
		}
		if gtEdgesByType[tid][e.Type] == nil {
			gtEdgesByType[tid][e.Type] = map[uint64]struct{}{}
		}
		gtEdgesByType[tid][e.Type][id] = struct{}{}
	}

	// --- compare against the path serving reads actually take ---------------
	tenants := map[tenantid.TenantID]struct{}{}
	for _, tid := range gs.membershipTenantsLocked() {
		tenants[tid] = struct{}{}
	}
	for tid := range gtNodesByTenant {
		if _, ok := tenants[tid]; !ok {
			report("tenant %q holds %d live nodes but is absent from membershipTenantsLocked()",
				tid, len(gtNodesByTenant[tid]))
		}
		tenants[tid] = struct{}{}
	}
	for tid := range gtEdgesByTenant {
		tenants[tid] = struct{}{}
	}

	for tid := range tenants {
		reportSliceDiff(report, "membershipNodeIDsForTenant", tid,
			gs.membershipNodeIDsForTenantLocked(tid), gtNodesByTenant[tid])
		reportSliceDiff(report, "membershipEdgeIDsForTenant", tid,
			gs.membershipEdgeIDsForTenantLocked(tid), gtEdgesByTenant[tid])

		labels := map[string]struct{}{}
		for _, l := range gs.membershipLabelsForTenantLocked(tid) {
			labels[l] = struct{}{}
		}
		for l := range gtNodesByLabel[tid] {
			labels[l] = struct{}{}
		}
		for label := range labels {
			reportSliceDiff(report, "membershipNodeIDsByLabel["+label+"]", tid,
				gs.membershipNodeIDsByLabelLocked(tid, label), gtNodesByLabel[tid][label])
		}

		etypes := map[string]struct{}{}
		for _, t := range gs.membershipEdgeTypesForTenantLocked(tid) {
			etypes[t] = struct{}{}
		}
		for t := range gtEdgesByType[tid] {
			etypes[t] = struct{}{}
		}
		for etype := range etypes {
			reportSliceDiff(report, "membershipEdgeIDsByType["+etype+"]", tid,
				gs.membershipEdgeIDsByTypeLocked(tid, etype), gtEdgesByType[tid][etype])
		}
	}

	// --- referential integrity: an edge must join two live nodes ------------
	for id, e := range liveEdges {
		if _, ok := liveNodes[e.FromNodeID]; !ok {
			report("edge %d: FromNodeID %d does not resolve to a live node", id, e.FromNodeID)
		}
		if _, ok := liveNodes[e.ToNodeID]; !ok {
			report("edge %d: ToNodeID %d does not resolve to a live node", id, e.ToNodeID)
		}
	}

	return violations
}

// addToSet inserts id into m[tid], creating the inner set on first use.
func addToSet(m map[tenantid.TenantID]map[uint64]struct{}, tid tenantid.TenantID, id uint64) {
	if m[tid] == nil {
		m[tid] = map[uint64]struct{}{}
	}
	m[tid][id] = struct{}{}
}

// reportSliceDiff compares what an index returned against ground truth, and
// reports each direction separately: an id the index invented, and an id the
// index lost. The two mean different bugs.
func reportSliceDiff(report reportFunc, name string, tid tenantid.TenantID, got []uint64, want map[uint64]struct{}) {
	gotSet := make(map[uint64]struct{}, len(got))
	for _, id := range got {
		gotSet[id] = struct{}{}
		if _, ok := want[id]; !ok {
			report("tenant %q: %s returned id %d, which is not a live record", tid, name, id)
		}
	}
	for id := range want {
		if _, ok := gotSet[id]; !ok {
			report("tenant %q: %s omitted live id %d", tid, name, id)
		}
	}
}
