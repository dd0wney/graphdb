package graphdb

import (
	"context"
	"encoding/json"
	"net/http"
)

type TraverseOptions struct {
	MaxDepth  int
	Direction string
	EdgeTypes []string
}

func (c *Client) Traverse(ctx context.Context, start uint64, opts TraverseOptions) ([]Node, error) {
	depth := opts.MaxDepth
	if depth <= 0 {
		depth = 1
	}
	body := map[string]any{"start_node_id": start, "max_depth": depth}
	if opts.Direction != "" {
		body["direction"] = opts.Direction
	}
	if opts.EdgeTypes != nil {
		body["edge_types"] = opts.EdgeTypes
	}
	res, err := c.t.request(ctx, http.MethodPost, "/traverse", body, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Nodes []Node `json:"nodes"`
	}
	return out.Nodes, json.Unmarshal(res.data, &out)
}

func (c *Client) Query(ctx context.Context, cypher string) (*QueryResult, error) {
	res, err := c.t.request(ctx, http.MethodPost, "/query", map[string]any{"query": cypher}, nil)
	if err != nil {
		return nil, err
	}
	var out QueryResult
	return &out, json.Unmarshal(res.data, &out)
}

func (c *Client) GraphQL(ctx context.Context, document string, variables map[string]any) (json.RawMessage, error) {
	body := map[string]any{"query": document}
	if variables != nil {
		body["variables"] = variables
	}
	res, err := c.t.request(ctx, http.MethodPost, "/graphql", body, nil)
	if err != nil {
		return nil, err
	}
	return res.data, nil
}

func (c *Client) Embeddings(ctx context.Context, inputs []string) (*EmbeddingsResult, error) {
	res, err := c.t.request(ctx, http.MethodPost, "/v1/embeddings", map[string]any{"input": inputs}, nil)
	if err != nil {
		return nil, err
	}
	var out EmbeddingsResult
	return &out, json.Unmarshal(res.data, &out)
}

func (c *Client) Retrieve(ctx context.Context, query string, k int) (*RetrieveResult, error) {
	if k <= 0 {
		k = 5
	}
	res, err := c.t.request(ctx, http.MethodPost, "/v1/retrieve", map[string]any{"query": query, "k": k}, nil)
	if err != nil {
		return nil, err
	}
	var out RetrieveResult
	return &out, json.Unmarshal(res.data, &out)
}
