package graphdb

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Sentinel errors. A non-2xx response returns an *Error whose Unwrap() is one
// of these, so callers can use errors.Is(err, graphdb.ErrNotFound).
var (
	ErrValidation = errors.New("graphdb: validation failed")
	ErrAuth       = errors.New("graphdb: authentication failed")
	ErrNotFound   = errors.New("graphdb: not found")
	ErrConflict   = errors.New("graphdb: conflict")
	ErrRateLimit  = errors.New("graphdb: rate limited")
	ErrServer     = errors.New("graphdb: server error")
)

// Error is the concrete error for any non-2xx API response.
type Error struct {
	Status  int
	Code    string
	Message string
	Method  string
	Path    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("graphdb: %s %s -> %d: %s", e.Method, e.Path, e.Status, e.Message)
}

func (e *Error) Unwrap() error { return sentinelFor(e.Status) }

func sentinelFor(status int) error {
	switch {
	case status == 400:
		return ErrValidation
	case status == 401, status == 403:
		return ErrAuth
	case status == 404:
		return ErrNotFound
	case status == 409:
		return ErrConflict
	case status == 429:
		return ErrRateLimit
	case status >= 500:
		return ErrServer
	default:
		return ErrServer
	}
}

// fromResponse builds an *Error from a non-2xx response body.
func fromResponse(status int, body []byte, method, path string) error {
	e := &Error{Status: status, Method: method, Path: path, Message: extractMessage(body)}
	return e
}

func extractMessage(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err == nil {
		for _, k := range []string{"error", "message", "detail"} {
			if v, ok := m[k]; ok {
				if s, ok := v.(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return string(body)
}
