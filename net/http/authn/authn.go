package authn

import (
	"context"
	"net/http"

	"chain/errors"
	"chain/log"
)

type key uint8

const usernameKey key = iota

// AuthFunc describes any function that takes a standard username/password pair
// and attempts to perform authentication. When used in conjunction with
// BasicHandler, returning ErrNotAuthenticated from an AuthFunc will cause a 401
// response to be written.
// Any other error will cause a 500 response.
type AuthFunc func(ctx context.Context, username, password string) error

// ErrNotAuthenticated should be returned by an AuthFunc if the provided
// credentials are invalid.
var ErrNotAuthenticated = errors.New("not authenticated")

// BasicHandler provides token authentication via HTTP basic auth. If the
// provided token is valid, then the corresponding user ID will be inserted into
// the context. The user ID should be retrieved using authn.GetAuthID.
// BasicHandler satisfies the ContextHandler interface.
type BasicHandler struct {
	Auth  AuthFunc
	Realm string
	Next  http.Handler
}

// ServeHTTP satisfies http.Handler.
func (h BasicHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	username, password, _ := req.BasicAuth()
	err := h.Auth(req.Context(), username, password)
	if err == nil {
		req = req.WithContext(newContext(req.Context(), username))
		h.Next.ServeHTTP(w, req)
	} else if err == ErrNotAuthenticated {
		log.Write(req.Context(),
			"status", http.StatusUnauthorized,
			log.KeyError, err,
		)
		if username == "" {
			w.Header().Add("WWW-Authenticate", `Basic realm="`+h.Realm+`"`)
		}
		http.Error(w, `{"message": "Request could not be authenticated"}`, http.StatusUnauthorized)
	} else {
		log.Write(req.Context(),
			"status", http.StatusInternalServerError,
			log.KeyError, err,
			log.KeyStack, errors.Stack(err),
		)
		http.Error(w, `{"message": "Internal error"}`, http.StatusInternalServerError)
	}
}

func newContext(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, usernameKey, username)
}

// UsernameFromContext returns the basic auth username
// attached to a context during a request.
func UsernameFromContext(ctx context.Context) string {
	user, ok := ctx.Value(usernameKey).(string)
	if !ok {
		return ""
	}
	return user
}
