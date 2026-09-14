package graphdb

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

// ErrNotDurable is a separate sentinel, not one of the block above: those
// unwrap from a non-2xx *Error, but ErrNotDurable unwraps from a 202
// Accepted *NotDurableError instead. A 202 is a 2xx, so *Error.Unwrap()
// never returns ErrNotDurable. The server answers 202 when a write applied
// but its WAL append failed, so the write is not yet durable.
// errors.Is(err, ErrNotDurable) detects this without matching on the
// message text.
var ErrNotDurable = errors.New("graphdb: write applied but not durable")

// NotDurableError is returned alongside a write's normal result when the
// server answers 202 Accepted for that write (a node or edge create,
// update, or delete, or a vector-index create or delete). ID is the
// affected entity's id as the response body reports it; it is 0 when the
// body omits it (a bulk delete or a vector-index write has no single entity
// id). A caller that ignores this error keeps the pre-202 behaviour; a
// caller that checks it must not retry the write, since the server already
// applied it once.
type NotDurableError struct {
	ID      uint64
	Message string
}

func (e *NotDurableError) Error() string {
	return fmt.Sprintf("graphdb: write applied but not durable (id=%d), do not retry: %s", e.ID, e.Message)
}

func (e *NotDurableError) Unwrap() error { return ErrNotDurable }

// notDurableFromResult returns a *NotDurableError, as an error, when res is
// a 202 Accepted response; it returns a plain nil error for any other
// status. Detection is by status only, per the contract, never by parsing
// the message text. The id and message are read from the response body
// (the #612 NodeNotDurableResponse / EdgeNotDurableResponse / WriteNotDurableResponse
// shapes all carry "id" and "message" fields, so one small struct decodes
// all of them; a malformed body simply yields a zero id and empty message
// rather than failing the call).
//
// Call this only from a write facet method (Nodes, Edges, Search's index
// create/delete). /admin/update/apply also answers 202, but for an
// unrelated reason: it accepts an asynchronous update job, not a not-yet-
// durable write, and Raw does not call this helper at all, so a caller
// using Raw against that route never sees a *NotDurableError.
func notDurableFromResult(res *apiResult) error {
	if res.status != http.StatusAccepted {
		return nil
	}
	var body struct {
		ID      uint64 `json:"id"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(res.data, &body)
	return &NotDurableError{ID: body.ID, Message: body.Message}
}

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
