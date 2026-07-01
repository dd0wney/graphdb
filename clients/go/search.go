package graphdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type SearchOptions struct {
	Limit          int
	Offset         int
	Labels         []string
	IncludeContent bool
	IncludeNodes   bool
}

type HybridOptions struct {
	SearchOptions
	Alpha *float64
}

type VectorOptions struct {
	K            int
	FilterLabels []string
}

func (o SearchOptions) body(query string) map[string]any {
	limit := o.Limit
	if limit <= 0 {
		limit = 20
	}
	b := map[string]any{
		"query":           query,
		"limit":           limit,
		"offset":          o.Offset,
		"include_content": o.IncludeContent,
		"include_nodes":   o.IncludeNodes,
	}
	if o.Labels != nil {
		b["labels"] = o.Labels
	}
	return b
}

func (s *Search) FullText(ctx context.Context, query string, opts SearchOptions) ([]SearchHit, error) {
	res, err := s.t.request(ctx, http.MethodPost, "/search", opts.body(query), nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Results []SearchHit `json:"results"`
	}
	return out.Results, json.Unmarshal(res.data, &out)
}

func (s *Search) Hybrid(ctx context.Context, query string, opts HybridOptions) (*HybridSearchResult, error) {
	body := opts.SearchOptions.body(query)
	if opts.Alpha != nil {
		body["alpha"] = *opts.Alpha
	}
	res, err := s.t.request(ctx, http.MethodPost, "/hybrid-search", body, nil)
	if err != nil {
		return nil, err
	}
	var out HybridSearchResult
	return &out, json.Unmarshal(res.data, &out)
}

func (s *Search) Vector(ctx context.Context, property string, vector []float64, opts VectorOptions) ([]VectorHit, error) {
	k := opts.K
	if k <= 0 {
		k = 10
	}
	body := map[string]any{"property_name": property, "query_vector": vector, "k": k}
	if opts.FilterLabels != nil {
		body["filter_labels"] = opts.FilterLabels
	}
	res, err := s.t.request(ctx, http.MethodPost, "/vector-search", body, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Results []VectorHit `json:"results"`
	}
	return out.Results, json.Unmarshal(res.data, &out)
}

func (s *Search) CreateIndex(ctx context.Context, property string, dimensions int) (*VectorIndex, error) {
	res, err := s.t.request(ctx, http.MethodPost, "/vector-indexes",
		map[string]any{"property_name": property, "dimensions": dimensions}, nil)
	if err != nil {
		return nil, err
	}
	var out VectorIndex
	return &out, json.Unmarshal(res.data, &out)
}

func (s *Search) ListIndexes(ctx context.Context) ([]VectorIndex, error) {
	res, err := s.t.request(ctx, http.MethodGet, "/vector-indexes", nil, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Indexes []VectorIndex `json:"indexes"`
	}
	return out.Indexes, json.Unmarshal(res.data, &out)
}

func (s *Search) GetIndex(ctx context.Context, property string) (*VectorIndex, error) {
	res, err := s.t.request(ctx, http.MethodGet,
		fmt.Sprintf("/vector-indexes/%s", url.PathEscape(property)), nil, nil)
	if err != nil {
		return nil, err
	}
	var out VectorIndex
	return &out, json.Unmarshal(res.data, &out)
}
