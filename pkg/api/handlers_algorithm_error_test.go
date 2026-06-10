package api

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
)

// TestRespondAlgorithmError_TimeoutMapsTo408 pins the H-6 status mapping:
// a context deadline/cancellation surfaced by a heavy algorithm is a 408
// (real timeout), not the 500 fallback. The error arrives wrapped (via
// wrapForClient), so the mapping must traverse the chain.
func TestRespondAlgorithmError_TimeoutMapsTo408(t *testing.T) {
	server, cleanup := setupTestServer(t)
	defer cleanup()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"deadline wrapped", wrapForClient(context.DeadlineExceeded, "betweenness"), 408},
		{"canceled wrapped", wrapForClient(context.Canceled, "scc"), 408},
		{"other error uses fallback", errors.New("boom"), 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			server.respondAlgorithmError(rr, tc.err, 500)
			if rr.Code != tc.want {
				t.Errorf("want %d, got %d", tc.want, rr.Code)
			}
		})
	}
}
