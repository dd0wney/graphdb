package main

import (
	"fmt"
	"log"

	"github.com/darraghdowney/cluso-graphdb/pkg/storage"
)

func main() {
	dataDir := "./data/tui"

	fmt.Println("🔥 Cluso GraphDB - TUI Demo Setup")
	fmt.Println("==================================")

	// Create graph storage
	graph, err := storage.NewGraphStorage(dataDir)
	if err != nil {
		log.Fatalf("Failed to create graph storage: %v", err)
	}
	defer graph.Close()

	fmt.Println("📊 Creating demo social network...")

	// Create people
	people := []struct {
		name      string
		age       int64
		city      string
		interests []string
	}{
		{"Alice", 30, "San Francisco", []string{"AI", "Graphs", "Databases"}},
		{"Bob", 25, "New York", []string{"Music", "Art", "Design"}},
		{"Charlie", 35, "London", []string{"Finance", "Trading", "Analytics"}},
		{"Diana", 28, "Tokyo", []string{"Gaming", "Anime", "Coding"}},
		{"Eve", 32, "Berlin", []string{"Security", "Privacy", "Crypto"}},
		{"Frank", 29, "Paris", []string{"Food", "Travel", "Photography"}},
		{"Grace", 31, "Sydney", []string{"Science", "Research", "ML"}},
		{"Henry", 27, "Toronto", []string{"Sports", "Fitness", "Health"}},
	}

	nodeIDs := make([]uint64, 0)
	for _, person := range people {
		node, err := graph.CreateNode(
			[]string{"Person"},
			map[string]storage.Value{
				"name": storage.StringValue(person.name),
				"age":  storage.IntValue(person.age),
				"city": storage.StringValue(person.city),
			},
		)
		if err != nil {
			log.Printf("Failed to create node: %v", err)
			continue
		}
		nodeIDs = append(nodeIDs, node.ID)
		fmt.Printf("  ✓ Created: %-10s (age %d, %s)\n", person.name, person.age, person.city)
	}

	fmt.Println("\n🔗 Creating relationships...")

	// Create a realistic social network
	connections := []struct {
		from     int
		to       int
		relType  string
		since    int64
		strength float64
	}{
		// Alice's connections
		{0, 1, "KNOWS", 2018, 0.9},
		{0, 2, "WORKS_WITH", 2020, 0.7},
		{0, 6, "COLLABORATES", 2021, 0.8},

		// Bob's connections
		{1, 3, "KNOWS", 2019, 0.6},
		{1, 5, "FRIENDS", 2017, 0.9},

		// Charlie's connections
		{2, 4, "KNOWS", 2020, 0.5},
		{2, 7, "MENTORS", 2022, 0.7},

		// Diana's connections
		{3, 6, "KNOWS", 2021, 0.6},
		{3, 7, "FRIENDS", 2019, 0.8},

		// Eve's connections
		{4, 5, "COLLABORATES", 2021, 0.7},
		{4, 0, "FOLLOWS", 2020, 0.5},

		// Frank's connections
		{5, 6, "KNOWS", 2018, 0.6},

		// Grace's connections
		{6, 7, "COLLABORATES", 2022, 0.9},

		// Create some cycles
		{7, 0, "KNOWS", 2023, 0.6},
		{6, 1, "FRIENDS", 2019, 0.7},
	}

	for _, conn := range connections {
		if conn.from >= len(nodeIDs) || conn.to >= len(nodeIDs) {
			continue
		}

		_, err := graph.CreateEdge(
			nodeIDs[conn.from],
			nodeIDs[conn.to],
			conn.relType,
			map[string]storage.Value{
				"since": storage.IntValue(conn.since),
			},
			conn.strength,
		)
		if err != nil {
			log.Printf("Failed to create edge: %v", err)
			continue
		}

		fmt.Printf("  ✓ %s -[%s]-> %s\n",
			people[conn.from].name,
			conn.relType,
			people[conn.to].name,
		)
	}

	// Create some products
	fmt.Println("\n📦 Creating products...")
	products := []struct {
		name     string
		price    int64
		category string
	}{
		{"Laptop Pro", 1999, "Electronics"},
		{"Wireless Mouse", 49, "Electronics"},
		{"Coffee Maker", 129, "Appliances"},
		{"Running Shoes", 89, "Sports"},
	}

	productIDs := make([]uint64, 0)
	for _, product := range products {
		node, err := graph.CreateNode(
			[]string{"Product"},
			map[string]storage.Value{
				"name":     storage.StringValue(product.name),
				"price":    storage.IntValue(product.price),
				"category": storage.StringValue(product.category),
			},
		)
		if err != nil {
			log.Printf("Failed to create product: %v", err)
			continue
		}
		productIDs = append(productIDs, node.ID)
		fmt.Printf("  ✓ Created: %-20s ($%d)\n", product.name, product.price)
	}

	// Create purchases
	fmt.Println("\n🛒 Creating purchases...")
	purchases := []struct {
		person  int
		product int
	}{
		{0, 0}, // Alice bought Laptop Pro
		{0, 1}, // Alice bought Wireless Mouse
		{3, 3}, // Diana bought Running Shoes
		{5, 2}, // Frank bought Coffee Maker
		{6, 0}, // Grace bought Laptop Pro
		{7, 3}, // Henry bought Running Shoes
	}

	for _, purchase := range purchases {
		if purchase.person >= len(nodeIDs) || purchase.product >= len(productIDs) {
			continue
		}

		_, err := graph.CreateEdge(
			nodeIDs[purchase.person],
			productIDs[purchase.product],
			"PURCHASED",
			nil,
			1.0,
		)
		if err != nil {
			log.Printf("Failed to create purchase: %v", err)
			continue
		}

		fmt.Printf("  ✓ %s purchased %s\n",
			people[purchase.person].name,
			products[purchase.product].name,
		)
	}

	stats := graph.GetStatistics()
	fmt.Printf("\n✅ Demo database created successfully!\n")
	fmt.Printf("   Nodes: %d\n", stats.NodeCount)
	fmt.Printf("   Edges: %d\n\n", stats.EdgeCount)

	fmt.Println("🚀 Now run: ./bin/tui")
	fmt.Println("\n💡 Try these queries in the TUI:")
	fmt.Println("   MATCH (p:Person) RETURN p")
	fmt.Println("   MATCH (p:Person)-[:KNOWS]->(f) RETURN p, f")
	fmt.Println("   MATCH (p:Person)-[:PURCHASED]->(prod:Product) RETURN p, prod")
}
