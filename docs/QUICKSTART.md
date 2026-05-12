# 5-Minute Quickstart

Get GraphDB up and running in under 5 minutes.

## 1. Run the Server

The fastest way to start is using Docker:

```bash
docker run -p 8080:8080 dd0wney/graphdb:latest
```

Or, if you have Go installed, run from source:

```bash
go run cmd/server/main.go --port 8080
```

## 2. Your First Cypher Query

Create a few nodes and a relationship using the standard openCypher syntax.

```bash
curl -X POST http://localhost:8080/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "CREATE (p1:Person {name: '\''Alice'\'', age: 30}), (p2:Person {name: '\''Bob'\'', age: 25}) CREATE (p1)-[:KNOWS]->(p2) RETURN p1, p2"
  }'
```

## 3. Query the Graph

Find Alice's friends:

```bash
curl -X POST http://localhost:8080/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "MATCH (p:Person {name: '\''Alice'\''})-[:KNOWS]->(friend) RETURN friend.name, friend.age"
  }'
```

## 4. Native Graph Algorithms

GraphDB includes a library of high-performance algorithms. For example, find the shortest path between two nodes:

```bash
curl -X POST http://localhost:8080/query \
  -H "Content-Type: application/json" \
  -d '{
    "query": "MATCH (a:Person {name: '\''Alice'\''}), (b:Person {name: '\''Bob'\''}) CALL algo.shortestPath(a, b) YIELD path RETURN path"
  }'
```

## 5. Vector Search (AI/ML)

Create a vector index and search for similar nodes:

```bash
# Create a 3-dimensional vector index on the 'embedding' property
curl -X POST http://localhost:8080/vector-indexes \
  -H "Content-Type: application/json" \
  -d '{"property_name": "embedding", "dimensions": 3}'

# Search for similar vectors
curl -X POST http://localhost:8080/vector-search \
  -H "Content-Type: application/json" \
  -d '{
    "property_name": "embedding",
    "query_vector": [0.1, 0.2, 0.3],
    "k": 5
  }'
```

## Next Steps

- **Multi-Tenancy**: Every request is isolated. Use the `X-Tenant-ID` header to switch contexts.
- **GraphQL**: Access the full power of the graph via typed schemas at `/graphql`.
- **Drivers**: Check out the [examples/](../examples/) folder for Python, Node.js, and Go client snippets.
- **Updates**: Stay current by running `graphdb-admin update`.
- **Migration**: Coming from Neo4j? See our [Migration Guide](MIGRATION_NEO4J.md).
