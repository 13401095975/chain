package httpjson

import (
	"net/http"

	"golang.org/x/net/context"
)

// key is an unexported type for keys defined in this package.
// This prevents collisions with keys defined in other packages.
type key int

// Keys for HTTP objects in Contexts.
// They are unexported; clients use Request and ResponseWriter
// instead of using these keys directly.
const (
	reqKey key = iota
	respKey
)

// Request returns the HTTP request stored in ctx.
// If there is none, it panics.
// The context given to a handler function
// registered in this package is guaranteed to have
// a request.
func Request(ctx context.Context) *http.Request {
	return ctx.Value(reqKey).(*http.Request)
}

// ResponseWriter returns the HTTP response writer stored in ctx.
// If there is none, it panics.
// The context given to a handler function
// registered in this package is guaranteed to have
// a response writer.
func ResponseWriter(ctx context.Context) http.ResponseWriter {
	return ctx.Value(respKey).(http.ResponseWriter)
}
