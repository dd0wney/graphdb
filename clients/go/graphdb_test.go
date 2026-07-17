package graphdb

import (
	"errors"
	"testing"
)

func TestNewRequiresExactlyOneAuthMode(t *testing.T) {
	if _, err := New("http://x"); err == nil {
		t.Error("no auth mode: expected error")
	}
	if _, err := New("http://x", WithToken("a"), WithAPIKey("b")); err == nil {
		t.Error("two auth modes: expected error")
	}
	if _, err := New("", WithToken("a")); err == nil {
		t.Error("empty base URL: expected error")
	}
	c, err := New("http://x", WithToken("a"))
	if err != nil {
		t.Fatalf("valid: %v", err)
	}
	if c.Nodes == nil || c.Edges == nil || c.Search == nil {
		t.Error("facets not wired")
	}
}

func TestErrorSentinelsAreDistinct(t *testing.T) {
	if errors.Is(ErrNotFound, ErrValidation) {
		t.Error("sentinels must be distinct")
	}
}

func TestNewValidatesBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		wantErr bool
	}{
		{"https", "https://db.example.com", false},
		{"http for local dev", "http://localhost:8080", false},
		{"missing scheme", "db.example.com", true},
		{"unsupported scheme", "ftp://db.example.com", true},
		{"unparseable", "http://[::1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := New(c.baseURL, WithToken("t"))
			if (err != nil) != c.wantErr {
				t.Errorf("New(%q) err = %v, wantErr=%v", c.baseURL, err, c.wantErr)
			}
		})
	}
}
