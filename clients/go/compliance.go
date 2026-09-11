package graphdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// AuditEvent is one compliance audit log entry.
type AuditEvent struct {
	ID           string         `json:"id"`
	Timestamp    time.Time      `json:"timestamp"`
	TenantID     string         `json:"tenant_id,omitempty"`
	UserID       string         `json:"user_id,omitempty"`
	Username     string         `json:"username,omitempty"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id,omitempty"`
	Status       string         `json:"status"`
	ErrorMessage string         `json:"error_message,omitempty"`
	IPAddress    string         `json:"ip_address,omitempty"`
	UserAgent    string         `json:"user_agent,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
}

// AuditLogOptions filters and paginates a compliance audit log query. An
// empty field is omitted from the request. StartTime and EndTime are RFC3339
// strings. Limit defaults to 100 server-side and caps at 1000; Offset
// defaults to 0.
type AuditLogOptions struct {
	UserID       string
	Username     string
	Action       string
	ResourceType string
	Status       string
	StartTime    string
	EndTime      string
	Limit        int
	Offset       int
}

// MaskingPolicy is a per-tenant masking specification. Properties maps a
// property name to a masking strategy: one of "full", "partial", "hash",
// "redact", "tokenize", "none".
type MaskingPolicy struct {
	TenantID   string            `json:"tenant_id,omitempty"`
	Properties map[string]string `json:"properties,omitempty"`
	AutoDetect bool              `json:"auto_detect"`
	UpdatedAt  time.Time         `json:"updated_at,omitempty"`
}

// AuditLog queries the compliance audit log (GET /v1/compliance/audit-log).
// Scope is resolved server-side from the caller's tenant; non-admin callers
// always see their own tenant regardless of any filter.
func (a *Compliance) AuditLog(ctx context.Context, opts AuditLogOptions) ([]AuditEvent, error) {
	params := url.Values{}
	if opts.UserID != "" {
		params.Set("user_id", opts.UserID)
	}
	if opts.Username != "" {
		params.Set("username", opts.Username)
	}
	if opts.Action != "" {
		params.Set("action", opts.Action)
	}
	if opts.ResourceType != "" {
		params.Set("resource_type", opts.ResourceType)
	}
	if opts.Status != "" {
		params.Set("status", opts.Status)
	}
	if opts.StartTime != "" {
		params.Set("start_time", opts.StartTime)
	}
	if opts.EndTime != "" {
		params.Set("end_time", opts.EndTime)
	}
	if opts.Limit > 0 {
		params.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.Offset > 0 {
		params.Set("offset", strconv.Itoa(opts.Offset))
	}
	res, err := a.t.request(ctx, http.MethodGet, "/v1/compliance/audit-log", nil, params)
	if err != nil {
		return nil, err
	}
	var out struct {
		Events []AuditEvent `json:"events"`
	}
	return out.Events, json.Unmarshal(res.data, &out)
}

// GetMaskingPolicy reads a tenant's masking policy (GET
// /v1/compliance/masking-policy/{tenant}). Non-admin callers may only read
// their own tenant: a mismatched tenant returns an *Error wrapping ErrAuth
// (403). A tenant with no policy set returns an *Error wrapping ErrNotFound
// (404).
func (a *Compliance) GetMaskingPolicy(ctx context.Context, tenant string) (*MaskingPolicy, error) {
	res, err := a.t.request(ctx, http.MethodGet,
		fmt.Sprintf("/v1/compliance/masking-policy/%s", url.PathEscape(tenant)), nil, nil)
	if err != nil {
		return nil, err
	}
	var out MaskingPolicy
	return &out, json.Unmarshal(res.data, &out)
}

// SetMaskingPolicy sets or replaces the masking policy for the caller's
// tenant (POST /v1/compliance/masking-policy). Admin-only: a non-admin
// caller gets an *Error wrapping ErrAuth (403). Only Properties and
// AutoDetect are sent; TenantID and UpdatedAt are server-managed and come
// back populated in the response.
func (a *Compliance) SetMaskingPolicy(ctx context.Context, policy MaskingPolicy) (*MaskingPolicy, error) {
	res, err := a.t.request(ctx, http.MethodPost, "/v1/compliance/masking-policy", map[string]any{
		"properties":  policy.Properties,
		"auto_detect": policy.AutoDetect,
	}, nil)
	if err != nil {
		return nil, err
	}
	var out MaskingPolicy
	return &out, json.Unmarshal(res.data, &out)
}
