package storage_test

import (
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/dd0wney/graphdb/pkg/storage"
)

// ExampleGraphStorage shows the basic embedding flow: open a store, create two
// nodes and an edge between them, then read an edge back. Node IDs are assigned
// sequentially from 1.
func ExampleGraphStorage() {
	dir, err := os.MkdirTemp("", "graphdb-example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	gs, err := storage.NewGraphStorage(dir)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = gs.Close() }()

	alice, err := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Alice"),
	})
	if err != nil {
		log.Fatal(err)
	}
	bob, err := gs.CreateNode([]string{"Person"}, map[string]storage.Value{
		"name": storage.StringValue("Bob"),
	})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := gs.CreateEdge(alice.ID, bob.ID, "KNOWS", nil, 1.0); err != nil {
		log.Fatal(err)
	}

	edges, err := gs.GetOutgoingEdges(alice.ID)
	if err != nil {
		log.Fatal(err)
	}
	name, _ := alice.Properties["name"].AsString()
	fmt.Printf("%s -%s-> node %d\n", name, edges[0].Type, edges[0].ToNodeID)
	// Output:
	// Alice -KNOWS-> node 2
}

// ExampleGraphStorage_tenantIsolation shows the *ForTenant convention: new code
// scopes every operation to a tenant, and a cross-tenant read returns
// ErrNodeNotFound (the same error as a genuinely missing node) so existence
// can't leak across tenants.
func ExampleGraphStorage_tenantIsolation() {
	dir, err := os.MkdirTemp("", "graphdb-example")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)

	gs, err := storage.NewGraphStorage(dir)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = gs.Close() }()

	doc, err := gs.CreateNodeWithTenant("acme", []string{"Doc"}, nil)
	if err != nil {
		log.Fatal(err)
	}

	// The owning tenant sees the node.
	_, err = gs.GetNodeForTenant(doc.ID, "acme")
	fmt.Println("acme can read:", err == nil)

	// A different tenant cannot — and gets ErrNodeNotFound, not a distinct
	// "forbidden" error, so it can't tell the node exists at all.
	_, err = gs.GetNodeForTenant(doc.ID, "globex")
	fmt.Println("globex blocked:", errors.Is(err, storage.ErrNodeNotFound))
	// Output:
	// acme can read: true
	// globex blocked: true
}
