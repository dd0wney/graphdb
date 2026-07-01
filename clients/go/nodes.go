package graphdb

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"strconv"
)

// NodeInput is one node for BatchCreate.
type NodeInput struct {
	Labels     []string       `json:"labels"`
	Properties map[string]any `json:"properties"`
}

// ListOptions controls node listing.
type ListOptions struct {
	Label    string
	PageSize int // default 100 if <= 0
}

func (n *Nodes) Create(ctx context.Context, labels []string, props map[string]any) (*Node, error) {
	if labels == nil {
		labels = []string{}
	}
	if props == nil {
		props = map[string]any{}
	}
	res, err := n.t.request(ctx, http.MethodPost, "/nodes",
		map[string]any{"labels": labels, "properties": props}, nil)
	if err != nil {
		return nil, err
	}
	var out Node
	return &out, json.Unmarshal(res.data, &out)
}

func (n *Nodes) Get(ctx context.Context, id uint64) (*Node, error) {
	res, err := n.t.request(ctx, http.MethodGet, fmt.Sprintf("/nodes/%d", id), nil, nil)
	if err != nil {
		return nil, err
	}
	var out Node
	return &out, json.Unmarshal(res.data, &out)
}

func (n *Nodes) Update(ctx context.Context, id uint64, props map[string]any) (*Node, error) {
	res, err := n.t.request(ctx, http.MethodPut, fmt.Sprintf("/nodes/%d", id),
		map[string]any{"properties": props}, nil)
	if err != nil {
		return nil, err
	}
	var out Node
	return &out, json.Unmarshal(res.data, &out)
}

func (n *Nodes) Delete(ctx context.Context, id uint64) error {
	_, err := n.t.request(ctx, http.MethodDelete, fmt.Sprintf("/nodes/%d", id), nil, nil)
	return err
}

func (n *Nodes) BatchCreate(ctx context.Context, nodes []NodeInput) ([]Node, error) {
	res, err := n.t.request(ctx, http.MethodPost, "/nodes/batch",
		map[string]any{"nodes": nodes}, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Nodes []Node `json:"nodes"`
	}
	return out.Nodes, json.Unmarshal(res.data, &out)
}

// List streams every node (optionally filtered by label), auto-following the
// X-Next-Cursor response header. On error the iterator yields one final
// (zero, err) pair and stops.
func (n *Nodes) List(ctx context.Context, opts ListOptions) iter.Seq2[Node, error] {
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	return func(yield func(Node, error) bool) {
		var cursor, prev string
		for {
			params := url.Values{}
			params.Set("limit", strconv.Itoa(pageSize))
			if opts.Label != "" {
				params.Set("label", opts.Label)
			}
			if cursor != "" {
				params.Set("cursor", cursor)
			}
			res, err := n.t.request(ctx, http.MethodGet, "/nodes", nil, params)
			if err != nil {
				yield(Node{}, err)
				return
			}
			var page []Node
			if err := json.Unmarshal(res.data, &page); err != nil {
				yield(Node{}, err)
				return
			}
			for _, nd := range page {
				if !yield(nd, nil) {
					return
				}
			}
			cursor = res.header.Get("X-Next-Cursor")
			if cursor == "" || cursor == prev {
				return
			}
			prev = cursor
		}
	}
}

// ListAll collects every node into a slice (convenience over List).
func (n *Nodes) ListAll(ctx context.Context, opts ListOptions) ([]Node, error) {
	var out []Node
	for nd, err := range n.List(ctx, opts) {
		if err != nil {
			return nil, err
		}
		out = append(out, nd)
	}
	return out, nil
}
