package cluster_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestServerDoesNotImportCluster guards ROADMAP B6: graphdb ships single-node;
// cmd/server and pkg/api must not import pkg/cluster (the unwired substrate).
func TestServerDoesNotImportCluster(t *testing.T) {
	const clusterPath = "github.com/dd0wney/graphdb/pkg/cluster"
	// repo root = two levels up from this test file's package dir (pkg/cluster).
	roots := []string{"../../cmd/server", "../../pkg/api"}
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if perr != nil {
				return perr
			}
			for _, imp := range f.Imports {
				if p := strings.Trim(imp.Path.Value, `"`); p == clusterPath || strings.HasPrefix(p, clusterPath+"/") {
					t.Errorf("%s imports %s — cluster must stay unwired (single-node)", path, p)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}
