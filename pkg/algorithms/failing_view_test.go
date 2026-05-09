package algorithms

import (
	"errors"

	"github.com/dd0wney/cluso-graphdb/pkg/storage"
)

// failingView wraps a graphView and forces OutgoingEdges to return an
// error for a specific node ID. The current *storage.GraphStorage impls
// never return non-nil error from edge reads (see
// pkg/storage/query_operations.go:33-50), so this is the only way to
// exercise the defensive-degrade branches added in scc.go, triangles.go,
// and node_similarity.go.
type failingView struct {
	inner  graphView
	failOn uint64
}

func (v *failingView) AllNodes() []*storage.Node {
	return v.inner.AllNodes()
}

func (v *failingView) Node(id uint64) (*storage.Node, error) {
	return v.inner.Node(id)
}

func (v *failingView) OutgoingEdges(id uint64) ([]*storage.Edge, error) {
	if id == v.failOn {
		return nil, errors.New("forced edge read failure")
	}
	return v.inner.OutgoingEdges(id)
}

func (v *failingView) IncomingEdges(id uint64) ([]*storage.Edge, error) {
	return v.inner.IncomingEdges(id)
}

func (v *failingView) Edge(id uint64) (*storage.Edge, error) {
	return v.inner.Edge(id)
}
