# GraphDB API Documentation

Comprehensive REST API for the Cluso GraphDB enterprise graph database.

## Table of Contents

- [Quick Start](#quick-start)
- [Authentication](#authentication)
- [API Reference](#api-reference)
- [Examples](#examples)
- [Rate Limits](#rate-limits)
- [Error Handling](#error-handling)

## Quick Start

### 1. Start the Server

```bash
# Community Edition
PORT=8080 ./bin/server

# Enterprise Edition
GRAPHDB_EDITION=enterprise \
GRAPHDB_LICENSE_KEY='your-license-key' \
PORT=8080 ./bin/server
```

### 2. Register a User

```bash
curl -X POST http://localhost:8080/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "alice",
    "password": "SecurePass123!"
  }'
```

### 3. Login and Get Token

```bash
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "alice",
    "password": "SecurePass123!"
  }'
```

Response:
```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "username": "alice",
    "role": "viewer"
  }
}
```

### 4. Use the API

```bash
# Save the token
TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."

# Create a node
curl -X POST http://localhost:8080/nodes \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "labels": ["Person"],
    "properties": {
      "name": "Alice Johnson",
      "age": 30
    }
  }'
```

## Authentication

The GraphDB API supports two authentication methods:

### 1. JWT Bearer Token (Recommended for Users)

Use for interactive applications and user sessions.

**Header:**
```
Authorization: Bearer <access_token>
```

**Token Lifetime:**
- Access Token: 15 minutes
- Refresh Token: 7 days

**Refresh Example:**
```bash
curl -X POST http://localhost:8080/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{
    "refresh_token": "<your_refresh_token>"
  }'
```

### 2. API Key (Recommended for Services)

Use for server-to-server communication and automation.

**Header:**
```
X-API-Key: <your_api_key>
```

**Create API Key (Admin Only):**
```bash
curl -X POST http://localhost:8080/auth/api-keys \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Production Service Key",
    "permissions": ["read", "write"],
    "expires_in": "720h"
  }'
```

## API Reference

### OpenAPI Specification

The complete API specification is available in OpenAPI 3.0 format:
- **File**: `docs/openapi.yaml`
- **Interactive Documentation**: Use [Swagger UI](https://swagger.io/tools/swagger-ui/) or [Redoc](https://redocly.com/redoc/)

### View with Swagger UI

```bash
# Using Docker
docker run -p 8081:8080 \
  -e SWAGGER_JSON=/openapi.yaml \
  -v $(pwd)/docs/openapi.yaml:/openapi.yaml \
  swaggerapi/swagger-ui

# Open http://localhost:8081
```

### Core Endpoints

| Category | Endpoint | Method | Description |
|----------|----------|--------|-------------|
| **Authentication** | `/auth/register` | POST | Register new user |
| | `/auth/login` | POST | Login and get tokens |
| | `/auth/refresh` | POST | Refresh access token |
| | `/auth/me` | GET | Get current user info |
| **Health** | `/health` | GET | Health check (public) |
| | `/metrics` | GET | System metrics (public) |
| **Nodes** | `/nodes` | GET | List nodes |
| | `/nodes` | POST | Create node |
| | `/nodes/{id}` | GET | Get node by ID |
| | `/nodes/{id}` | PUT | Update node |
| | `/nodes/{id}` | DELETE | Delete node |
| | `/nodes/batch` | POST | Batch create nodes |
| **Edges** | `/edges` | GET | List edges |
| | `/edges` | POST | Create edge |
| | `/edges/{id}` | GET | Get edge by ID |
| | `/edges/{id}` | PUT | Update edge |
| | `/edges/{id}` | DELETE | Delete edge |
| | `/edges/batch` | POST | Batch create edges |
| **Traversal** | `/traverse` | POST | Graph traversal |
| | `/shortest-path` | POST | Find shortest path |
| **Algorithms** | `/algorithms` | POST | Run graph algorithms |
| **Query** | `/query` | POST | Custom query language |
| | `/graphql` | POST | GraphQL endpoint |

## Examples

### Node Operations

#### Create a Node

```bash
curl -X POST http://localhost:8080/nodes \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "labels": ["Person", "Employee"],
    "properties": {
      "name": "Bob Smith",
      "age": 35,
      "department": "Engineering",
      "email": "bob@example.com"
    }
  }'
```

Response:
```json
{
  "id": 12345,
  "labels": ["Person", "Employee"],
  "properties": {
    "name": "Bob Smith",
    "age": 35,
    "department": "Engineering",
    "email": "bob@example.com"
  }
}
```

#### Get Node

```bash
curl -X GET http://localhost:8080/nodes/12345 \
  -H "Authorization: Bearer $TOKEN"
```

#### Update Node

```bash
curl -X PUT http://localhost:8080/nodes/12345 \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "properties": {
      "age": 36,
      "department": "Engineering Management"
    }
  }'
```

#### List Nodes with Filtering

```bash
curl -X GET "http://localhost:8080/nodes?labels=Person&limit=50&offset=0" \
  -H "Authorization: Bearer $TOKEN"
```

#### Batch Create Nodes

```bash
curl -X POST http://localhost:8080/nodes/batch \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "nodes": [
      {
        "labels": ["Person"],
        "properties": {"name": "Alice", "age": 30}
      },
      {
        "labels": ["Person"],
        "properties": {"name": "Bob", "age": 25}
      },
      {
        "labels": ["Person"],
        "properties": {"name": "Charlie", "age": 35}
      }
    ]
  }'
```

### Edge Operations

#### Create an Edge

```bash
curl -X POST http://localhost:8080/edges \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "from_node_id": 12345,
    "to_node_id": 67890,
    "type": "KNOWS",
    "weight": 0.8,
    "properties": {
      "since": "2020-01-15",
      "strength": "high"
    }
  }'
```

Response:
```json
{
  "id": 78901,
  "from_node_id": 12345,
  "to_node_id": 67890,
  "type": "KNOWS",
  "weight": 0.8,
  "properties": {
    "since": "2020-01-15",
    "strength": "high"
  }
}
```

#### List Edges with Filtering

```bash
# Get all KNOWS relationships from node 12345
curl -X GET "http://localhost:8080/edges?from_node_id=12345&type=KNOWS" \
  -H "Authorization: Bearer $TOKEN"
```

### Graph Traversal

#### Traverse from a Node

```bash
curl -X POST http://localhost:8080/traverse \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "start_node_id": 12345,
    "max_depth": 3,
    "direction": "outgoing",
    "edge_types": ["KNOWS", "WORKS_WITH"]
  }'
```

#### Find Shortest Path

```bash
curl -X POST http://localhost:8080/shortest-path \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "start_node_id": 12345,
    "end_node_id": 67890,
    "weighted": true
  }'
```

Response:
```json
{
  "path": [12345, 23456, 34567, 67890],
  "distance": 3.5,
  "nodes": [
    {"id": 12345, "labels": ["Person"], "properties": {"name": "Alice"}},
    {"id": 23456, "labels": ["Person"], "properties": {"name": "Bob"}},
    {"id": 34567, "labels": ["Person"], "properties": {"name": "Charlie"}},
    {"id": 67890, "labels": ["Person"], "properties": {"name": "David"}}
  ]
}
```

### Graph Algorithms

#### PageRank

```bash
curl -X POST http://localhost:8080/algorithms \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "algorithm": "pagerank",
    "parameters": {
      "iterations": 20,
      "damping_factor": 0.85
    }
  }'
```

Response:
```json
{
  "algorithm": "pagerank",
  "execution_time_ms": 1250.5,
  "results": {
    "scores": {
      "12345": 0.15,
      "67890": 0.23,
      "54321": 0.18
    }
  }
}
```

#### Betweenness Centrality

```bash
curl -X POST http://localhost:8080/algorithms \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "algorithm": "betweenness",
    "parameters": {
      "normalized": true
    }
  }'
```

#### Community Detection (Louvain)

```bash
curl -X POST http://localhost:8080/algorithms \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "algorithm": "louvain",
    "parameters": {
      "resolution": 1.0
    }
  }'
```

### Query Operations

#### Custom Query Language

```bash
curl -X POST http://localhost:8080/query \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "MATCH (p:Person)-[:KNOWS]->(f:Person) WHERE p.age > 25 RETURN p, f LIMIT 10"
  }'
```

#### GraphQL Query

```bash
curl -X POST http://localhost:8080/graphql \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "query { nodes(labels: [\"Person\"], limit: 10) { id labels properties } }"
  }'
```

## Rate Limits

Rate limits vary by edition:

### Community Edition
- **Requests**: 100 requests/minute per IP
- **Batch Operations**: 1000 items max per request
- **Node Properties**: 100 properties max per node
- **Edge Properties**: 100 properties max per edge

### Enterprise Edition
- **Requests**: 1000 requests/minute per user
- **Batch Operations**: 10,000 items max per request
- **Node Properties**: 1000 properties max per node
- **Edge Properties**: 1000 properties max per edge

## Error Handling

### HTTP Status Codes

| Code | Description |
|------|-------------|
| 200 | Success |
| 201 | Created |
| 400 | Bad Request - Invalid parameters |
| 401 | Unauthorized - Missing or invalid authentication |
| 403 | Forbidden - Insufficient permissions |
| 404 | Not Found - Resource doesn't exist |
| 413 | Payload Too Large - Request body exceeds limit |
| 429 | Too Many Requests - Rate limit exceeded |
| 500 | Internal Server Error |

### Error Response Format

All errors return a consistent JSON structure:

```json
{
  "error": "Invalid request parameters",
  "code": "INVALID_REQUEST",
  "details": {
    "field": "labels",
    "issue": "labels array cannot be empty"
  }
}
```

### Common Error Codes

| Code | Description |
|------|-------------|
| `INVALID_REQUEST` | Request parameters are invalid |
| `UNAUTHORIZED` | Missing or invalid authentication |
| `FORBIDDEN` | Insufficient permissions for operation |
| `NOT_FOUND` | Requested resource doesn't exist |
| `CONFLICT` | Resource already exists |
| `RATE_LIMIT_EXCEEDED` | Too many requests |

### Handling Authentication Errors

```bash
# Invalid token
HTTP/1.1 401 Unauthorized
{
  "error": "Invalid or expired token",
  "code": "UNAUTHORIZED"
}

# Missing authentication
HTTP/1.1 401 Unauthorized
{
  "error": "Missing authentication (Bearer token or X-API-Key header required)",
  "code": "UNAUTHORIZED"
}

# Insufficient permissions
HTTP/1.1 403 Forbidden
{
  "error": "Insufficient permissions",
  "code": "FORBIDDEN"
}
```

## Best Practices

### 1. Authentication

- **Use API keys for services**: More secure for server-to-server communication
- **Rotate tokens regularly**: Implement token refresh flow
- **Store tokens securely**: Never commit tokens to version control

### 2. Batch Operations

- **Use batch endpoints**: Much faster than individual requests
- **Respect batch limits**: Max 1000 items (Community) or 10,000 (Enterprise)
- **Handle partial failures**: Check response for individual item status

### 3. Querying

- **Use pagination**: Always specify limit and offset for large datasets
- **Filter server-side**: Use query parameters instead of filtering in client
- **Cache results**: Cache frequently accessed data

### 4. Performance

- **Enable connection pooling**: Reuse HTTP connections
- **Use compression**: Enable gzip compression for large responses
- **Monitor rate limits**: Track remaining quota in response headers

### 5. Error Handling

- **Implement retries**: With exponential backoff for 5xx errors
- **Log errors**: Include request ID for debugging
- **Validate inputs**: Check data before sending to API

## Support

- **Documentation**: https://docs.clusographdb.com
- **GitHub Issues**: https://github.com/dd0wney/cluso-graphdb/issues
- **Email**: support@clusographdb.com

## Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0.0 | 2024-11 | Initial API release |

