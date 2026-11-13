package vector

import (
	"context"
	"errors"
	"math/rand"
	"testing"
)

// TestEmeans_TrainContextRefusesCancelledContext pins the v1.4 fix: Train had
// no context, so CALL algo.eMeans could not stop in the training loop, which
// is the one super-linear loop among the community procedures.
func TestEmeans_TrainContextRefusesCancelledContext(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	const n, d, k = 500, 8, 4
	data := make([][]float32, n)
	for i := range data {
		row := make([]float32, d)
		for j := range row {
			row[j] = float32(rng.NormFloat64())
		}
		data[i] = row
	}
	em, err := NewEmeans(EmeansConfig{D: d, K: k, NEpochs: 3, Seed: 7})
	if err != nil {
		t.Fatalf("NewEmeans: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	centroids, labels, err := em.TrainContext(ctx, data)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want one wrapping context.Canceled", err)
	}
	if centroids != nil || labels != nil {
		t.Fatalf("a cancelled Train returned a partial result: %d centroids, %d labels", len(centroids), len(labels))
	}

	// The plain Train must still run to completion.
	em2, err := NewEmeans(EmeansConfig{D: d, K: k, NEpochs: 3, Seed: 7})
	if err != nil {
		t.Fatalf("NewEmeans: %v", err)
	}
	if _, _, err := em2.Train(data); err != nil {
		t.Fatalf("Train: %v", err)
	}
}
