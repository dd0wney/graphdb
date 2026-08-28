package graphdb

import (
	"context"
	"encoding/json"
	"net/http"
)

// RawResponse is the result of a Raw call.
type RawResponse struct {
	Status int
	Body   json.RawMessage
	Header http.Header
}

// Raw performs an arbitrary request against the server, for endpoints not yet
// faceted (tenants, api keys, security, compliance, ...). A non-2xx status
// returns an *Error, same as the faceted methods.
func (c *Client) Raw(ctx context.Context, method, path string, body any) (*RawResponse, error) {
	res, err := c.t.request(ctx, method, path, body, nil)
	if err != nil {
		return nil, err
	}
	return &RawResponse{Status: res.status, Body: res.data, Header: res.header}, nil
}
