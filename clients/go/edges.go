package graphdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
