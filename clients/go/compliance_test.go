package graphdb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestComplianceAuditLogSendsFiltersAndPagination(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/compliance/audit-log" {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		want := map[string]string{
			"action":        "create",
			"resource_type": "node",
			"status":        "success",
			"limit":         "50",
			"offset":        "10",
		}
		for k, v := range want {
			if got := q.Get(k); got != v {
				t.Errorf("query %s = %q, want %q", k, got, v)
			}
		}
		_, _ = w.Write([]byte(`{"events":[{"id":"e1","action":"create","resource_type":"node","status":"success"}],"count":1,"total":1,"offset":10,"limit":50,"has_more":false,"tenant":"acme"}`))
	})
	got, err := c.Compliance.AuditLog(context.Background(), AuditLogOptions{
		Action:       "create",
		ResourceType: "node",
		Status:       "success",
		Limit:        50,
		Offset:       10,
	})
	if err != nil {
		t.Fatalf("auditlog: %v", err)
	}
	if len(got) != 1 || got[0].ID != "e1" || got[0].Action != "create" {
		t.Fatalf("events = %+v, want one create/node/success event", got)
	}
}

func TestComplianceAuditLogOmitsUnsetFilters(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		for _, k := range []string{"user_id", "username", "action", "resource_type", "status", "start_time", "end_time", "limit", "offset"} {
			if q.Has(k) {
				t.Errorf("query must omit unset %s, got %q", k, q.Get(k))
			}
		}
		_, _ = w.Write([]byte(`{"events":[],"count":0,"total":0,"offset":0,"limit":100,"has_more":false,"tenant":"acme"}`))
	})
	if _, err := c.Compliance.AuditLog(context.Background(), AuditLogOptions{}); err != nil {
		t.Fatalf("auditlog: %v", err)
	}
}

func TestComplianceGetMaskingPolicy(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/compliance/masking-policy/acme" {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"tenant_id":"acme","properties":{"email":"partial"},"auto_detect":true,"updated_at":"2026-09-01T00:00:00Z"}`))
	})
	got, err := c.Compliance.GetMaskingPolicy(context.Background(), "acme")
	if err != nil {
		t.Fatalf("getmaskingpolicy: %v", err)
	}
	if got.TenantID != "acme" || got.Properties["email"] != "partial" || !got.AutoDetect {
		t.Fatalf("policy = %+v, want acme/email=partial/autodetect", got)
	}
}

// A tenant name containing a slash must be path-escaped, not become an extra
// URL segment (same pattern as Search.GetIndex).
func TestComplianceGetMaskingPolicyEscapesTenant(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.EscapedPath(); got != "/v1/compliance/masking-policy/acme%2Feu" {
			t.Errorf("escaped path = %q, want /v1/compliance/masking-policy/acme%%2Feu", got)
		}
		_, _ = w.Write([]byte(`{"tenant_id":"acme/eu","auto_detect":false}`))
	})
	got, err := c.Compliance.GetMaskingPolicy(context.Background(), "acme/eu")
	if err != nil || got.TenantID != "acme/eu" {
		t.Fatalf("getmaskingpolicy: %v %+v", err, got)
	}
}

func TestComplianceGetMaskingPolicyMapsSentinelErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"non-admin cross-tenant read", http.StatusForbidden, ErrAuth},
		{"no policy set for tenant", http.StatusNotFound, ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
			})
			_, err := c.Compliance.GetMaskingPolicy(context.Background(), "acme")
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestComplianceSetMaskingPolicy(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/compliance/masking-policy" {
			t.Fatalf("got %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		props, ok := body["properties"].(map[string]any)
		if !ok || props["ssn"] != "hash" {
			t.Errorf("properties = %v, want ssn=hash", body["properties"])
		}
		if body["auto_detect"] != true {
			t.Errorf("auto_detect = %v, want true", body["auto_detect"])
		}
		_, _ = w.Write([]byte(`{"tenant_id":"acme","properties":{"ssn":"hash"},"auto_detect":true,"updated_at":"2026-09-01T00:00:00Z"}`))
	})
	got, err := c.Compliance.SetMaskingPolicy(context.Background(), MaskingPolicy{
		Properties: map[string]string{"ssn": "hash"},
		AutoDetect: true,
	})
	if err != nil {
		t.Fatalf("setmaskingpolicy: %v", err)
	}
	if got.TenantID != "acme" || got.Properties["ssn"] != "hash" {
		t.Fatalf("policy = %+v, want acme/ssn=hash", got)
	}
}

func TestComplianceSetMaskingPolicyMapsForbidden(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"Admin role required"}`))
	})
	_, err := c.Compliance.SetMaskingPolicy(context.Background(), MaskingPolicy{})
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
}
