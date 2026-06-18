package query

import (
	"os"
	"testing"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// setupExecutorTestGraph creates a test graph for executor tests
func setupExecutorTestGraph(t *testing.T) (*storage.GraphStorage, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "executor-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	gs, err := storage.NewGraphStorage(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create graph storage: %v", err)
	}

	cleanup := func() {
		_ = gs.Close()
		_ = os.RemoveAll(tmpDir)
	}

	return gs, cleanup
}

// TestNewExecutor tests creating a new executor

func TestNewExecutor(t *testing.T) {
	gs, cleanup := setupExecutorTestGraph(t)
	defer cleanup()

	executor := NewExecutor(gs)

	if executor == nil {
		t.Fatal("Expected non-nil executor")
	}

	if executor.graph == nil {
		t.Error("Expected non-nil graph")
	}

	if executor.optimizer == nil {
		t.Error("Expected non-nil optimizer")
	}

	if executor.cache == nil {
		t.Error("Expected non-nil cache")
	}
}

func findRowByColumn(rows []map[string]any, column string, value any) map[string]any {
	for _, row := range rows {
		if row[column] == value {
			return row
		}
	}
	return nil
}
