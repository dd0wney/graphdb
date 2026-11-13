package algorithms

import (
	"context"
	"errors"
	"math/rand"
	"sync/atomic"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// These tests prove each cancellation check on its own.
//
// The checks overlap. If one is removed, the next check in program order takes
// over and the algorithm still returns context.Canceled, only later. A test
// that reads the error alone therefore cannot tell the checks apart. These
// tests read how much work ran after the context ended: a hook on the graph
// cancels a real context at the Nth edge read, and the test bounds the reads
// that follow. A check that is present stops the run within its own interval.
// A check that is missing lets the run go on to a later check, far past the
// bound. Each bound is named at its test.

// bigN is large enough to pass the check interval (1024) several times.
const bigN = 4000

// memView is an in-memory graphView with a hook on every edge read.
type memView struct {
	nodes   []*storage.Node
	out, in map[uint64][]*storage.Edge

	outCalls, inCalls int
	onOut, onIn       func(calls int)
}

// newMemView builds nodes 1..n (id 0 is the "no meeting node" sentinel of the
// path search) with an edge i -> i+off for each offset, where the target exists.
func newMemView(n int, offsets ...int) *memView {
	v := &memView{out: map[uint64][]*storage.Edge{}, in: map[uint64][]*storage.Edge{}}
	for i := 1; i <= n; i++ {
		v.nodes = append(v.nodes, &storage.Node{ID: uint64(i)})
	}
	for i := 1; i <= n; i++ {
		for _, off := range offsets {
			j := i + off
			if j > n {
				continue
			}
			e := &storage.Edge{FromNodeID: uint64(i), ToNodeID: uint64(j), Type: "E"}
			v.out[uint64(i)] = append(v.out[uint64(i)], e)
			v.in[uint64(j)] = append(v.in[uint64(j)], e)
		}
	}
	return v
}

func (v *memView) AllNodes() ([]*storage.Node, error) { return v.nodes, nil }
func (v *memView) Node(id uint64) (*storage.Node, error) {
	return &storage.Node{ID: id}, nil
}
func (v *memView) Edge(uint64) (*storage.Edge, error) { return nil, storage.ErrEdgeNotFound }
func (v *memView) OutgoingEdges(id uint64) ([]*storage.Edge, error) {
	v.outCalls++
	if v.onOut != nil {
		v.onOut(v.outCalls)
	}
	return v.out[id], nil
}
func (v *memView) IncomingEdges(id uint64) ([]*storage.Edge, error) {
	v.inCalls++
	if v.onIn != nil {
		v.onIn(v.inCalls)
	}
	return v.in[id], nil
}

// cancelOutAt makes the view cancel the returned context at its nth outgoing
// edge read.
func (v *memView) cancelOutAt(n int) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	v.onOut = func(calls int) {
		if calls == n {
			cancel()
		}
	}
	return ctx
}

func (v *memView) cancelInAt(n int) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	v.onIn = func(calls int) {
		if calls == n {
			cancel()
		}
	}
	return ctx
}

func wantCanceled(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want one wrapping context.Canceled", err)
	}
}

func wantReadsAtMost(t *testing.T, what string, got, limit int) {
	t.Helper()
	if got > limit {
		t.Fatalf("%d %s reads ran, want at most %d: the check that should have stopped the run is missing", got, what, limit)
	}
}

// --- pagerank.go ---

// Check: the entry check. An empty graph has no loop, so only the entry check
// can refuse; without it the empty-graph shortcut returns a result.
func TestCancelCheck_PageRankEntry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := pageRankView(ctx, newMemView(0), DefaultPageRankOptions())
	wantCanceled(t, err)
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
}

// Check: the tick in the out-degree pass. The context ends at the 100th read
// of the pass (4000 nodes). The tick fires at 1024 reads. Without it the pass
// reads all 4000 nodes before the iteration check can see the end.
func TestCancelCheck_PageRankOutDegreeTick(t *testing.T) {
	v := newMemView(bigN, 1, 7)
	ctx := v.cancelOutAt(100)
	res, err := pageRankView(ctx, v, DefaultPageRankOptions())
	wantCanceled(t, err)
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
	wantReadsAtMost(t, "outgoing", v.outCalls, 100+cancelCheckInterval)
}

// Check: the tick in the node loop. The context ends at the 2000th incoming
// read of iteration 1 (4000 nodes). Without the tick, iteration 1 reads all
// 4000 nodes before the iteration check at the top of iteration 2.
func TestCancelCheck_PageRankNodeLoopTick(t *testing.T) {
	v := newMemView(bigN, 1, 7)
	ctx := v.cancelInAt(2000)
	res, err := pageRankView(ctx, v, DefaultPageRankOptions())
	wantCanceled(t, err)
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
	wantReadsAtMost(t, "incoming", v.inCalls, 2000+cancelCheckInterval)
}

// Check: the check at the top of each iteration. A small graph with a huge
// MaxIterations reaches the node-loop tick only every 1024 node visits. The
// context ends at the 100th incoming read (iteration 4 of 30 nodes). The
// iteration check stops within that iteration. Without it the run goes on to
// the tick near read 994.
func TestCancelCheck_PageRankIterationCheck(t *testing.T) {
	v := newMemView(30, 1, 7)
	ctx := v.cancelInAt(100)
	opts := DefaultPageRankOptions()
	opts.MaxIterations = 1_000_000
	opts.Tolerance = 0 // never converge, so only the context can end the run
	res, err := pageRankView(ctx, v, opts)
	wantCanceled(t, err)
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
	wantReadsAtMost(t, "incoming", v.inCalls, 100+30)
}

// --- khop.go ---

// Check: the tick in the BFS loop. The context ends at the 100th read. The
// BFS would otherwise read all 4000 nodes and return a result.
func TestCancelCheck_KHopLoopTick(t *testing.T) {
	v := newMemView(bigN, 1, 7)
	ctx := v.cancelOutAt(100)
	opts := DefaultKHopOptions()
	opts.MaxHops = bigN
	res, err := kHopNeighboursView(ctx, v, 1, opts)
	wantCanceled(t, err)
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
	wantReadsAtMost(t, "outgoing", v.outCalls, 100+cancelCheckInterval)
}

// --- cycle_detection.go ---

// Check: the tick at each DFS node. One chain from a single root reaches all
// 4000 nodes, so the check in the root loop never runs a second time.
func TestCancelCheck_DetectCyclesDFSTick(t *testing.T) {
	v := newMemView(bigN, 1)
	ctx := v.cancelOutAt(100)
	cycles, err := detectCyclesView(ctx, v)
	wantCanceled(t, err)
	if cycles != nil {
		t.Fatalf("cycles = %v, want nil", cycles)
	}
	wantReadsAtMost(t, "outgoing", v.outCalls, 100+cancelCheckInterval)
}

// Check: the check at each DFS root. 4000 isolated nodes make 4000 roots of
// one visit each. The context ends at the 100th read. The root check stops at
// once. Without it the DFS tick stops the run near read 1024.
func TestCancelCheck_DetectCyclesRootCheck(t *testing.T) {
	v := newMemView(bigN) // no edges
	ctx := v.cancelOutAt(100)
	cycles, err := detectCyclesView(ctx, v)
	wantCanceled(t, err)
	if cycles != nil {
		t.Fatalf("cycles = %v, want nil", cycles)
	}
	wantReadsAtMost(t, "outgoing", v.outCalls, 100+2)
}

// --- link_prediction.go ---

// commonNeighbourView has two sources, 1 and 2, that both point to the nodes
// 3..n+2, so Adamic-Adar visits n common neighbours.
func commonNeighbourView(n int) *memView {
	v := newMemView(n + 2)
	for c := 3; c <= n+2; c++ {
		for _, from := range []uint64{1, 2} {
			e := &storage.Edge{FromNodeID: from, ToNodeID: uint64(c), Type: "E"}
			v.out[from] = append(v.out[from], e)
			v.in[uint64(c)] = append(v.in[uint64(c)], e)
		}
	}
	return v
}

func adamicOptions() LinkPredictionOptions {
	o := DefaultLinkPredictionOptions()
	o.Method = LinkPredAdamicAdar
	o.Direction = DirectionOut
	return o
}

// Check: the check at each common neighbour in the Adamic-Adar loop. The
// context ends at read 100 (reads 1 and 2 are the two source sets). This loop
// has no interval: each pass already costs an edge read. Without the check the
// loop reads all 4000 neighbours and returns a score.
func TestCancelCheck_LinkPredictionAdamicAdar(t *testing.T) {
	v := commonNeighbourView(bigN)
	ctx := v.cancelOutAt(100)
	score, err := predictLinkScoreView(ctx, v, 1, 2, adamicOptions())
	wantCanceled(t, err)
	if score != 0 {
		t.Fatalf("score = %v, want 0", score)
	}
	wantReadsAtMost(t, "outgoing", v.outCalls, 100+2)
}

// Check: the check between the two neighbour-set builds. The context ends at
// the first read, which is inside the build of the first set. The check stops
// the run before the second set is built, so exactly one read runs.
func TestCancelCheck_LinkPredictionBetweenSets(t *testing.T) {
	v := commonNeighbourView(10)
	ctx := v.cancelOutAt(1)
	_, err := predictLinkScoreView(ctx, v, 1, 2, adamicOptions())
	wantCanceled(t, err)
	wantReadsAtMost(t, "outgoing", v.outCalls, 1)
}

// Check: the first check, before any read. A context that has ended must cause
// no edge read at all.
func TestCancelCheck_LinkPredictionEntry(t *testing.T) {
	v := commonNeighbourView(10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := predictLinkScoreView(ctx, v, 1, 2, adamicOptions())
	wantCanceled(t, err)
	wantReadsAtMost(t, "outgoing", v.outCalls, 0)
}

// --- node_similarity.go ---

// Check: the check between the two neighbour-set builds.
func TestCancelCheck_NodeSimilarityBetweenSets(t *testing.T) {
	v := commonNeighbourView(10)
	ctx := v.cancelOutAt(1)
	opts := DefaultNodeSimilarityOptions()
	opts.Direction = DirectionOut
	score, err := nodeSimilarityPairView(ctx, v, 1, 2, opts)
	wantCanceled(t, err)
	if score != 0 {
		t.Fatalf("score = %v, want 0", score)
	}
	wantReadsAtMost(t, "outgoing", v.outCalls, 1)
}

// Check: the first check, before any read.
func TestCancelCheck_NodeSimilarityEntry(t *testing.T) {
	v := commonNeighbourView(10)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opts := DefaultNodeSimilarityOptions()
	opts.Direction = DirectionOut
	_, err := nodeSimilarityPairView(ctx, v, 1, 2, opts)
	wantCanceled(t, err)
	wantReadsAtMost(t, "outgoing", v.outCalls, 0)
}

// --- shortest_path.go ---

// hookStorage serves outgoing edges from a map and counts the reads. The
// embedded interface is nil: ShortestPathForTenantContext calls no other
// method, and a call to one would panic, which would show a wrong assumption.
type hookStorage struct {
	storage.Storage
	adj    map[uint64][]*storage.Edge
	calls  int
	onCall func(calls int)
}

func (h *hookStorage) GetOutgoingEdgesForTenant(id uint64, _ string) ([]*storage.Edge, error) {
	h.calls++
	if h.onCall != nil {
		h.onCall(h.calls)
	}
	return h.adj[id], nil
}

func (h *hookStorage) cancelAt(n int) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	h.onCall = func(calls int) {
		if calls == n {
			cancel()
		}
	}
	return ctx
}

// chainStorage is a chain 1 -> 2 -> ... -> n. The end node n+1 does not exist,
// so the search never meets and runs the whole chain: one node a level.
func chainStorage(n int) *hookStorage {
	h := &hookStorage{adj: map[uint64][]*storage.Edge{}}
	for i := 1; i < n; i++ {
		h.adj[uint64(i)] = []*storage.Edge{{FromNodeID: uint64(i), ToNodeID: uint64(i + 1)}}
	}
	return h
}

// starStorage is node 1 with n leaves: level 2 holds n nodes.
func starStorage(n int) *hookStorage {
	h := &hookStorage{adj: map[uint64][]*storage.Edge{}}
	for i := 2; i <= n+1; i++ {
		h.adj[1] = append(h.adj[1], &storage.Edge{FromNodeID: 1, ToNodeID: uint64(i)})
	}
	return h
}

// Check: the tick in the frontier expansion. Level 2 holds 4000 nodes. The
// context ends at the 1500th read, inside that level. The tick fires at the
// 2048th pop. Without it the whole level runs (4001 reads) before the check in
// the outer loop.
func TestCancelCheck_ShortestPathFrontierTick(t *testing.T) {
	h := starStorage(bigN)
	ctx := h.cancelAt(1500)
	path, err := ShortestPathForTenantContext(ctx, h, 1, uint64(bigN+100), "t")
	wantCanceled(t, err)
	if path != nil {
		t.Fatalf("path = %v, want nil", path)
	}
	wantReadsAtMost(t, "outgoing", h.calls, 1500+cancelCheckInterval)
}

// Check: the check at the top of each outer pass. A chain has one node a
// level, so the frontier tick fires only every 1024 levels. The context ends
// at the 100th read. The outer check stops within a pass or two.
func TestCancelCheck_ShortestPathOuterCheck(t *testing.T) {
	h := chainStorage(bigN)
	ctx := h.cancelAt(100)
	path, err := ShortestPathForTenantContext(ctx, h, 1, uint64(bigN+100), "t")
	wantCanceled(t, err)
	if path != nil {
		t.Fatalf("path = %v, want nil", path)
	}
	wantReadsAtMost(t, "outgoing", h.calls, 100+2)
}

// Check: the entry check. With start equal to end there is no loop, and the
// shortcut would return a path for a context that has ended.
func TestCancelCheck_ShortestPathEntry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path, err := ShortestPathForTenantContext(ctx, &hookStorage{}, 5, 5, "t")
	wantCanceled(t, err)
	if path != nil {
		t.Fatalf("path = %v, want nil", path)
	}
}

// --- emeans.go (algorithms) ---

// countdownCtx reports nil from Err() for the first n calls and
// context.Canceled after that. The code under test reads only Err(): a search
// for ctx.Done() in pkg/algorithms and pkg/vector finds none. The embedded
// Background context (a nil Done channel) is therefore enough.
type countdownCtx struct {
	context.Context
	n     int64
	calls atomic.Int64
}

func newCountdownCtx(n int64) *countdownCtx {
	return &countdownCtx{Context: context.Background(), n: n}
}

func (c *countdownCtx) Err() error {
	if c.calls.Add(1) > c.n {
		return context.Canceled
	}
	return nil
}

// Check: the TrainContext(ctx) call. eMeansView calls Err() twice before it
// trains, and TrainContext calls it once at entry. The context passes those
// three calls and ends at the first mini-batch, so only the batch check inside
// TrainContext can see it. With plain Train in place of TrainContext the
// procedure returns a result.
func TestCancelCheck_EMeansTrainingLoop(t *testing.T) {
	gs, err := storage.NewGraphStorage(t.TempDir())
	if err != nil {
		t.Fatalf("storage: %v", err)
	}
	defer func() { _ = gs.Close() }()

	const tid = "t-emeans"
	rng := rand.New(rand.NewSource(5))
	for i := 0; i < 600; i++ {
		vec := make([]float32, 4)
		for j := range vec {
			vec[j] = float32(rng.NormFloat64())
		}
		if _, err := gs.CreateNodeWithTenant(tid, []string{"C"}, map[string]storage.Value{
			"embedding": storage.VectorValue(vec),
		}); err != nil {
			t.Fatalf("create node: %v", err)
		}
	}

	ctx := newCountdownCtx(3)
	res, err := EMeansForTenant(ctx, gs, DefaultEMeansOptions(4), tid)
	wantCanceled(t, err)
	if res != nil {
		t.Fatalf("result = %+v, want nil", res)
	}
	if ctx.calls.Load() < 4 {
		t.Fatalf("Err() was called %d times; the context did not end inside training", ctx.calls.Load())
	}
}
