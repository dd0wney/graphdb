# Migration Guide: Neo4j to GraphDB

This guide helps you map Neo4j concepts and Cypher queries to GraphDB.

## Concept Mapping

| Neo4j Concept | GraphDB Concept | Notes |
|---|---|---|
| Node | Node | Identical. |
| Relationship | Edge | Identical. |
| Label | Label | Multiple labels per node supported. |
| Relationship Type | Edge Type | One type per edge. |
| Property | Property | String, Int, Float, Bool, and Vector supported. |
| Bolt Protocol | HTTP/GraphQL | GraphDB uses stateless HTTP for queries. |
| APOC | Native Algorithms | Many APOC features are native in GraphDB. |

## Cypher Compatibility

GraphDB implements the openCypher standard using a modern Volcano execution engine.

### Supported Clauses
- `MATCH`: Standard pattern matching and variable-length paths.
- `WHERE`: Full predicate support including boolean logic and function calls.
- `RETURN`: Projections, aliases, and aggregations.
- `CREATE`: Single or multi-pattern creation.
- `MERGE`: Match-or-create logic.
- `SET` / `REMOVE`: Property updates and deletion.
- `DELETE` / `DETACH DELETE`: Entity removal.
- `CALL`: Invoking native graph algorithms.
- `UNION` / `UNION ALL`: Combining result sets.
- `UNWIND`: Expanding lists into rows.
- `WITH`: Chaining query segments.

### Key Differences
- **Transactions**: Every Cypher query in GraphDB is implicitly transactional. Explicit multi-query transactions (BEGIN/COMMIT) are not yet supported over HTTP.
- **Node IDs**: GraphDB uses 64-bit unsigned integers for IDs. Unlike Neo4j, IDs are strictly unique and reused only after compaction.

## Data Import

### 1. Export from Neo4j
Export your data to CSV using APOC:
```cypher
CALL apoc.export.csv.all("data.csv", {})
```

### 2. Import to GraphDB
Use the high-speed importer tool:
```bash
./bin/graphdb-admin import --input data.csv --format csv
```

## Driver Migration

GraphDB does not require a custom binary driver. You can use standard HTTP clients in any language.

### Neo4j Driver (Python)
```python
# Before (Neo4j)
with driver.session() as session:
    result = session.run("MATCH (n) RETURN n")
```

### GraphDB HTTP (Python)
```python
# After (GraphDB)
import requests
resp = requests.post("http://localhost:8080/query", 
                     json={"query": "MATCH (n) RETURN n"})
result = resp.json()
```

## Need Help?
Contact our engineering team at support@graphdb.io or open an issue on GitHub.
