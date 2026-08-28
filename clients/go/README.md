# graphdb Go client

First-party, zero-dependency Go client for [graphdb](https://github.com/dd0wney/graphdb).

## Install

```bash
go get github.com/dd0wney/graphdb/clients/go
```

## Quickstart

```go
import graphdb "github.com/dd0wney/graphdb/clients/go"

c, err := graphdb.New("https://your-graphdb", graphdb.WithToken("TOKEN"))
// or WithAPIKey("..."), or WithLogin("user", "pass") for auto login+refresh
if err != nil { /* ... */ }
ctx := context.Background()

alice, _ := c.Nodes.Create(ctx, []string{"Person"}, map[string]any{"name": "Alice"})
for n, err := range c.Nodes.List(ctx, graphdb.ListOptions{Label: "Person"}) {
	if err != nil { break }
	fmt.Println(n.ID, n.Properties)
}
hits, _ := c.Search.Vector(ctx, "embedding", []float64{0.1, 0.2}, graphdb.VectorOptions{K: 5})
```

## Errors

Non-2xx responses return `*graphdb.Error`; match with `errors.Is`:

```go
if _, err := c.Nodes.Get(ctx, 999); errors.Is(err, graphdb.ErrNotFound) { /* ... */ }
```

## Endpoints not yet faceted

Everything is reachable via the escape hatch:

```go
res, _ := c.Raw(ctx, "POST", "/hybrid-search", map[string]any{"query": "..."})
// res.Body is json.RawMessage
```
