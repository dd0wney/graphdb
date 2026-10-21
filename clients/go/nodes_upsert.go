package graphdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// mergeParams is the query string that turns POST /nodes and POST
// /nodes/batch from a create into an upsert.
func mergeParams() url.Values {
	return url.Values{"on_conflict": {"merge"}}
}

// BatchItemError reports one refused item of a batch write. Index is the
// position of the item in the request.
type BatchItemError struct {
	Index int    `json:"index"`
	Error string `json:"error"`
}

// UpsertedNode is a Node together with the outcome of its upsert.
type UpsertedNode struct {
	Node
	// Created is true when the call made the node and false when it matched
	// an existing node.
	Created bool `json:"created"`
}

// BatchUpsertResult is the outcome of BatchUpsert. A batch is not atomic: a
// refused item appears in Errors and the other items still apply.
type BatchUpsertResult struct {
	// Nodes holds one entry for each item that succeeded, in request order.
	Nodes []UpsertedNode `json:"nodes"`
	// Created counts the items that made a node.
	Created int `json:"created"`
	// Matched counts the items that found an existing node.
	Matched int `json:"matched"`
	// Failed counts the refused items. It equals len(Errors).
	Failed int `json:"failed"`
	// Errors lists each refused item by its request index.
	Errors []BatchItemError `json:"errors"`
}

// Upsert creates the node, or merges props into the existing node that holds
// the same registered uniqueness key. It returns the node and created, which
// is true when the call made the node. The server picks the key from the
// uniqueness rule for labels, so a label set with no rule gives a 400 *Error.
// A 409 *Error (ErrConflict) carries ConflictingNodeID and MissingLabels, or
// ConflictingNodeIDs when more than one node holds the key. On a 202 the
// method returns the node, created and a *NotDurableError. Do not retry then.
func (n *Nodes) Upsert(ctx context.Context, labels []string, props map[string]any) (*Node, bool, error) {
	if labels == nil {
		labels = []string{}
	}
	if props == nil {
		props = map[string]any{}
	}
	res, err := n.t.request(ctx, http.MethodPost, "/nodes",
		map[string]any{"labels": labels, "properties": props}, mergeParams())
	if err != nil {
		return nil, false, err
	}
	var out UpsertedNode
	if err := json.Unmarshal(res.data, &out); err != nil {
		return nil, false, err
	}
	return &out.Node, out.Created, notDurableFromResult(res)
}

// BatchUpsert runs Upsert for each item of nodes, in order. It returns an
// error only when the request as a whole fails. A refused item appears in the
// Errors field of the result.
func (n *Nodes) BatchUpsert(ctx context.Context, nodes []NodeInput) (*BatchUpsertResult, error) {
	res, err := n.t.request(ctx, http.MethodPost, "/nodes/batch",
		map[string]any{"nodes": nodes}, mergeParams())
	if err != nil {
		return nil, err
	}
	var out BatchUpsertResult
	if err := json.Unmarshal(res.data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
