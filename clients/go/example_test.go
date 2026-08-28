package graphdb_test

import (
	"context"

	graphdb "github.com/dd0wney/graphdb/clients/go"
)

// ExampleClient shows constructing a client and creating a node.
func ExampleClient() {
	c, err := graphdb.New("https://your-graphdb", graphdb.WithToken("YOUR_TOKEN"))
	if err != nil {
		return
	}
	_, _ = c.Nodes.Create(context.Background(), []string{"Person"}, map[string]any{"name": "Alice"})
}
