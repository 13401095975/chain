package api

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"

	"golang.org/x/net/context"

	"chain/errors"
	"chain/log"
)

// ErrBadInputJSON indicates the user supplied malformed JSON input,
// possibly including a datatype that doesn't match what we expected.
var ErrBadInputJSON = errors.New("api: bad input json")

// readJSON decodes a single JSON text from r into v.
// The only error it returns is ErrBadInputJSON
// (wrapped with the original error message as context).
func readJSON(r io.Reader, v interface{}) error {
	err := json.NewDecoder(r).Decode(v)
	if err != nil {
		return errors.Wrap(ErrBadInputJSON, err.Error())
	}
	return nil
}

func writeJSON(ctx context.Context, w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)

	// Make sure to render nil slices as "[]", rather than "null"
	if tv := reflect.TypeOf(v); tv.Kind() == reflect.Slice {
		if vv := reflect.ValueOf(v); vv.IsNil() {
			v = []struct{}{}
		}
	}

	err := json.NewEncoder(w).Encode(v)
	if err != nil {
		log.Error(ctx, err)
	}
}

func writeHTTPError(ctx context.Context, w http.ResponseWriter, err error) {
	info := errInfo(err)
	//metrics.Counter("status." + strconv.Itoa(info.HTTPStatus)).Add()
	//metrics.Counter("error." + info.ChainCode).Add()

	keyvals := []interface{}{
		"status", info.HTTPStatus,
		"chaincode", info.ChainCode,
		log.KeyError, err,
	}
	if info.HTTPStatus == 500 {
		keyvals = append(keyvals, log.KeyStack, errors.Stack(err))
	}
	log.Write(ctx, keyvals...)

	var v interface{} = info
	if s := errors.Detail(err); s != "" {
		v = struct {
			errorInfo
			Detail string `json:"detail"`
		}{info, s}
	}
	writeJSON(ctx, w, info.HTTPStatus, v)
}
