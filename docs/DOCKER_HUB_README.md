# Cluso GraphDB

High-performance graph database built in Go with LSM-tree storage engine.

## Performance

- **430,000 writes/second** with batched operations
- **50,000+ reads/second** on cached data
- **650x faster** reads vs baseline implementation
- **385MB RAM** for 5 million nodes
- Disk-backed adjacency lists with LRU caching

## Quick Start

```bash
# Run GraphDB server
docker run -d \
  -p 8080:8080 \
  -v $(pwd)/data:/data \
  --name graphdb \
  yourusername/graphdb:latest

# Test the server
curl http://localhost:8080/health

# Create a node
curl -X POST http://localhost:8080/nodes \
  -H "Content-Type: application/json" \
  -d '{"labels": ["Person"], "properties": {"name": "Alice"}}'
```

## Using Docker Compose

```yaml
version: '3.8'
services:
  graphdb:
    image: yourusername/graphdb:latest
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
    environment:
      - PORT=8080
    healthcheck:
      test: ["CMD", "wget", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 3s
      retries: 3
```

## Configuration

Environment variables:

- `PORT` - HTTP server port (default: 8080)

## API Endpoints

- `GET /health` - Health check
- `GET /metrics` - Performance metrics
- `POST /nodes` - Create nodes
- `POST /edges` - Create edges
- `POST /query` - Cypher-like graph queries
- `POST /traverse` - Graph traversal
- `POST /shortest-path` - Shortest path finding
- `POST /algorithms` - Graph algorithms (PageRank, clustering, etc.)

## Features

- LSM-tree based storage engine
- Write-ahead logging (WAL) for durability
- ACID transactions with batch support
- Property indexes for fast lookups
- Label and edge type indexes
- Graph algorithms (PageRank, clustering coefficient, community detection)
- Cypher-like query language
- REST API

## Use Cases

- Social networks
- Recommendation engines
- Fraud detection
- Knowledge graphs
- Network topology analysis

## Documentation

Full documentation: [GitHub Repository](https://github.com/dd0wney/cluso-graphdb)

## License

MIT License - see repository for details

## Support

- GitHub Issues: Report bugs and request features
- Documentation: See repository README
