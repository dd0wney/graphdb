// Package vector implements approximate nearest-neighbour search using a
// Hierarchical Navigable Small World (HNSW) graph.
//
// The entry type is [HNSWIndex], constructed with [NewHNSWIndex] (dimensions, M,
// efConstruction, metric). It supports the [MetricCosine], [MetricEuclidean],
// and [MetricDotProduct] distance metrics, Insert/Search/Delete, and concurrent
// reads under an RWMutex (writes serialize).
//
// Construction cost is data-dependent: ~O(N log N) for real embeddings, which
// cluster on a low-dimensional manifold. Uniform-random or otherwise
// maximal-intrinsic-dimensionality vectors degrade toward O(N²) under
// concentration of measure — a property of the data, not a bug. See the package
// benchmarks (BenchmarkHNSWInsert vs BenchmarkHNSWInsert_Clustered).
package vector
