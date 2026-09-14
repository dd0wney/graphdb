package graphql

// walWriteFailedMessage is the fixed sentence every WAL_WRITE_FAILED error
// carries. Identical wording to REST's respondWALWriteFailed
// (pkg/api/server_helpers.go) so both surfaces tell an identical,
// retry-safe story. It never varies with the wrapped disk error.
const walWriteFailedMessage = "The write applied in memory and is not durable yet. Do not retry: a retry applies the change a second time. A snapshot or a clean shutdown makes it durable."

// walWriteFailedError is returned by a mutation resolver in place of the
// storage error when errors.Is(err, storage.ErrWALWriteFailed): the change
// applied in memory but its WAL append failed. It implements
// gqlerrors.ExtendedError (Extensions() map[string]interface{}), which
// graphql-go's executor reads off the resolver's raw returned error — see
// pkg/graphql/http.go's doc comment on ServeHTTP for the mechanism this
// relies on.
//
// A resolver MUST return this value directly (not wrapped by fmt.Errorf's
// %w, which produces a *fmt.wrapError with no Extensions() method) or the
// dynamic type graphql-go inspects loses the extension.
type walWriteFailedError struct {
	// cause is the wrapped storage error. Never surfaced in Error() or the
	// extensions — logged server-side by the caller instead, same
	// precedent as REST's respondWALWriteFailed.
	cause error
	// id is the entity id as a string (GraphQL ids are strings). Empty
	// when the caller had no id to give — none of the six mutation
	// resolvers normally hits this: createNodeMutationResolver and
	// createEdgeMutationResolver each hold a nil guard (mutations_resolvers.go,
	// edges_resolvers.go) before formatting node/edge.ID, defensive against a
	// storage contract change rather than a path either resolver takes today.
	id string
}

// newWALWriteFailedError builds a walWriteFailedError. cause is the error
// returned by the storage call (wrapping storage.ErrWALWriteFailed); id is
// the affected node/edge id as a string.
func newWALWriteFailedError(cause error, id string) *walWriteFailedError {
	return &walWriteFailedError{cause: cause, id: id}
}

// Error implements the error interface with the same fixed sentence REST
// uses — never the wrapped disk error.
func (e *walWriteFailedError) Error() string {
	return walWriteFailedMessage
}

// Unwrap lets errors.Is/errors.As reach the wrapped storage error (and, in
// turn, storage.ErrWALWriteFailed) without exposing it through Error().
func (e *walWriteFailedError) Unwrap() error {
	return e.cause
}

// Extensions implements gqlerrors.ExtendedError. code/applied/durable/retry
// mirror REST's WriteNotDurableResponse fields (pkg/api/types.go); id is a
// string here because GraphQL ids are strings.
func (e *walWriteFailedError) Extensions() map[string]interface{} {
	return map[string]interface{}{
		"code":    "WAL_WRITE_FAILED",
		"applied": true,
		"durable": false,
		"retry":   false,
		"id":      e.id,
	}
}
