package vector

import (
	"context"
	"errors"
	"math/rand"
	"sync/atomic"
	"testing"
)

// countdownCtx reports nil from Err() for the first n calls and
// context.Canceled after that. The code under test reads only Err() (no
// ctx.Done() call exists in pkg/vector or the changed algorithms), so the
// embedded Background context with its nil Done channel is enough. It cancels
// in the middle of a loop with no timing.
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

func randomData(seed int64, n, d int) [][]float32 {
	rng := rand.New(rand.NewSource(seed))
	data := make([][]float32, n)
	for i := range data {
		row := make([]float32, d)
		for j := range row {
			row[j] = float32(rng.NormFloat64())
		}
		data[i] = row
	}
	return data
}

// TestEmeans_CancelledTrainKeepsOldModel: a TrainContext that ends in the
// middle of training must leave the model as it was. Before the fix it left
// half-trained centroids that Assign read, so Assign on the old data returned
// different labels with a nil error while Centroids still showed the old model.
func TestEmeans_CancelledTrainKeepsOldModel(t *testing.T) {
	const n, d, k = 5000, 8, 4
	dataA := randomData(1, n, d)
	dataB := randomData(2, n, d)

	em, err := NewEmeans(EmeansConfig{D: d, K: k, NEpochs: 3, Seed: 7})
	if err != nil {
		t.Fatalf("NewEmeans: %v", err)
	}
	if _, _, err := em.Train(dataA); err != nil {
		t.Fatalf("Train A: %v", err)
	}
	_, before, err := em.Assign(dataA)
	if err != nil {
		t.Fatalf("Assign before: %v", err)
	}
	centroidsBefore := em.Centroids
	statsBefore := len(em.IterationStats)
	splitsBefore := em.NSplits

	// Err() is nil for 3 calls, then Canceled: the entry check and two
	// mini-batches pass, so training stops inside the loop.
	ctx := newCountdownCtx(3)
	c, l, err := em.TrainContext(ctx, dataB)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if c != nil || l != nil {
		t.Fatalf("cancelled train returned a result")
	}
	if ctx.calls.Load() < 3 {
		t.Fatalf("Err() was called %d times; the context did not end inside training", ctx.calls.Load())
	}

	_, after, err := em.Assign(dataA)
	if err != nil {
		t.Fatalf("Assign after: %v", err)
	}
	changed := 0
	for i := range before {
		if before[i] != after[i] {
			changed++
		}
	}
	if changed != 0 {
		t.Errorf("%d of %d labels changed after a cancelled TrainContext", changed, n)
	}
	if len(em.Centroids) != len(centroidsBefore) || len(em.IterationStats) != statsBefore || em.NSplits != splitsBefore {
		t.Errorf("receiver state changed by a cancelled train")
	}
}

// TestEmeans_TrainContextTrivialPathChecksContext: with n <= k the trivial
// path returned a full result for a context that had already ended.
func TestEmeans_TrainContextTrivialPathChecksContext(t *testing.T) {
	em, err := NewEmeans(EmeansConfig{D: 2, K: 5, Seed: 3})
	if err != nil {
		t.Fatalf("NewEmeans: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _, err := em.TrainContext(ctx, randomData(3, 3, 2))
	if !errors.Is(err, context.Canceled) || c != nil {
		t.Fatalf("err = %v, centroids = %v; want context.Canceled and nil", err, c)
	}
}
