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

type EdgeCreateOptions struct {
	Properties map[string]any
	Weight     float64
}

// EdgeUpdateOptions.Weight is a pointer so an omitted weight is not sent
// (the server leaves the edge's weight unchanged rather than zeroing it).
type EdgeUpdateOptions struct {
	Properties map[string]any
	Weight     *float64
}

type EdgeInput struct {
	FromNodeID uint64         `json:"from_node_id"`
	ToNodeID   uint64         `json:"to_node_id"`
	Type       string         `json:"type"`
	Properties map[string]any `json:"properties"`
	Weight     float64        `json:"weight"`
}

func (e *Edges) Create(ctx context.Context, from, to uint64, edgeType string, opts EdgeCreateOptions) (*Edge, error) {
	props := opts.Properties
	if props == nil {
		props = map[string]any{}
	}
	res, err := e.t.request(ctx, http.MethodPost, "/edges", map[string]any{
		"from_node_id": from,
		"to_node_id":   to,
		"type":         edgeType,
		"properties":   props,
		"weight":       opts.Weight,
	}, nil)
	if err != nil {
		return nil, err
	}
	var out Edge
	return &out, json.Unmarshal(res.data, &out)
}

func (e *Edges) Get(ctx context.Context, id uint64) (*Edge, error) {
	res, err := e.t.request(ctx, http.MethodGet, fmt.Sprintf("/edges/%d", id), nil, nil)
	if err != nil {
		return nil, err
	}
	var out Edge
	return &out, json.Unmarshal(res.data, &out)
}

func (e *Edges) Update(ctx context.Context, id uint64, opts EdgeUpdateOptions) (*Edge, error) {
	body := map[string]any{}
	if opts.Properties != nil {
		body["properties"] = opts.Properties
	}
	if opts.Weight != nil {
		body["weight"] = *opts.Weight
	}
	res, err := e.t.request(ctx, http.MethodPut, fmt.Sprintf("/edges/%d", id), body, nil)
	if err != nil {
		return nil, err
	}
	var out Edge
	return &out, json.Unmarshal(res.data, &out)
}

func (e *Edges) Delete(ctx context.Context, id uint64) error {
	_, err := e.t.request(ctx, http.MethodDelete, fmt.Sprintf("/edges/%d", id), nil, nil)
	return err
}

func (e *Edges) BatchCreate(ctx context.Context, edges []EdgeInput) ([]Edge, error) {
	res, err := e.t.request(ctx, http.MethodPost, "/edges/batch",
		map[string]any{"edges": edges}, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Edges []Edge `json:"edges"`
	}
	return out.Edges, json.Unmarshal(res.data, &out)
}

// ListEdgesOptions controls edge listing.
type ListEdgesOptions struct {
	Type     string
	PageSize int // default 100 if <= 0
}

// List streams every edge (optionally filtered by type), auto-following the
// X-Next-Cursor response header. On error the iterator yields one final
// (zero, err) pair and stops.
func (e *Edges) List(ctx context.Context, opts ListEdgesOptions) iter.Seq2[Edge, error] {
	pageSize := opts.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	return func(yield func(Edge, error) bool) {
		var cursor, prev string
		for {
			params := url.Values{}
			params.Set("limit", strconv.Itoa(pageSize))
			if opts.Type != "" {
				params.Set("type", opts.Type)
			}
			if cursor != "" {
				params.Set("cursor", cursor)
			}
			res, err := e.t.request(ctx, http.MethodGet, "/edges", nil, params)
			if err != nil {
				yield(Edge{}, err)
				return
			}
			var page []Edge
			if err := json.Unmarshal(res.data, &page); err != nil {
				yield(Edge{}, err)
				return
			}
			for _, ed := range page {
				if !yield(ed, nil) {
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

// ListAll collects every edge into a slice (convenience over List).
func (e *Edges) ListAll(ctx context.Context, opts ListEdgesOptions) ([]Edge, error) {
	var out []Edge
	for ed, err := range e.List(ctx, opts) {
		if err != nil {
			return nil, err
		}
		out = append(out, ed)
	}
	return out, nil
}
