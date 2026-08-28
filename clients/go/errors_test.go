package graphdb

import (
	"errors"
	"testing"
)

func TestFromResponseMapsStatusToSentinel(t *testing.T) {
	cases := []struct {
		status int
		want   error
	}{
		{400, ErrValidation},
		{401, ErrAuth},
		{403, ErrAuth},
		{404, ErrNotFound},
		{409, ErrConflict},
		{429, ErrRateLimit},
		{500, ErrServer},
		{503, ErrServer},
	}
	for _, c := range cases {
		err := fromResponse(c.status, []byte(`{"error":"boom"}`), "GET", "/x")
		if !errors.Is(err, c.want) {
			t.Errorf("status %d: errors.Is => want %v", c.status, c.want)
		}
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != c.status {
			t.Errorf("status %d: expected *Error with Status set", c.status)
		}
		if ae.Message != "boom" {
			t.Errorf("status %d: message = %q, want boom", c.status, ae.Message)
		}
	}
}

func TestExtractMessageFallbacks(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"error key", `{"error":"boom"}`, "boom"},
		{"message key", `{"message":"m"}`, "m"},
		{"detail key", `{"detail":"d"}`, "d"},
		{"error precedes message", `{"error":"e","message":"m"}`, "e"},
		{"non-JSON body", `upstream exploded`, "upstream exploded"},
		{"empty body", ``, ""},
		{"JSON without known keys", `{"other":"x"}`, `{"other":"x"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var ae *Error
			if !errors.As(fromResponse(500, []byte(c.body), "GET", "/x"), &ae) {
				t.Fatal("expected *Error")
			}
			if ae.Message != c.want {
				t.Errorf("message = %q, want %q", ae.Message, c.want)
			}
		})
	}
}
