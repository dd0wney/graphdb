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
