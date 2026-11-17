//go:build go1.23

// Package oasometest checks, in tests, that handlers still match the
// documentation given to package oasome. It adds no behavior to production code.
//
// Wrap a handler and drive it with net/http/httptest:
//
//	srv := httptest.NewServer(oasometest.Wrap(t, doc, mux))
//
// For every request that matches a documented route, Wrap reports through
// t.Errorf (the test fails but continues) when
//
//   - the response status is not declared for the route (statuses from
//     oasome.Defaults count as declared; a route that declares no response
//     reports every status),
//   - the response body does not strictly decode into the declared Go type
//     (unknown JSON fields are errors; a ReturnsEmpty response must have an
//     empty body), or
//   - a request body was sent and does not strictly decode into the Body type.
//
// Requests that match no documented route pass through unchecked.
//
// Routes are matched with [http.Request.Pattern], which needs Go 1.23, so
// this package is built only with Go 1.23 or later.
//
// # Sub-muxes and prefixes
//
// http.StripPrefix passes a copy of the request to the sub-mux, so a wrapper
// around the outer mux never sees the sub-mux pattern. Wrap the sub-mux
// instead, with a document view that has the same prefix that the routes were
// registered with:
//
//	sub := http.NewServeMux()
//	api := doc.WithPrefix("/api")
//	sub.Handle(api.Route("GET /users", listUsers, oasome.Returns[[]User](200)))
//	mux.Handle("/api/", http.StripPrefix("/api", oasometest.Wrap(t, api, sub)))
//
// Routes are looked up by the pair (the prefix of the view, the mux pattern).
package oasometest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/0xfurai/oasome"
	"github.com/0xfurai/oasome/internal/bridge"
)

// Wrap returns a handler that serves h and checks each exchange against doc.
// Failures are reported with t.Errorf. See the package documentation for the
// rules and for the recipe for prefixed sub-muxes.
func Wrap(t testing.TB, doc *oasome.Doc, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqBody := bufferRequestBody(r)
		rec := &recorder{ResponseWriter: w}
		h.ServeHTTP(rec, r)
		// The mux sets r.Pattern on the request it receives, so read it
		// only after the handler returns.
		route, ok := bridge.Lookup(doc, r.Pattern)
		if !ok {
			return
		}
		t.Helper()
		checkRequest(t, r, route, reqBody)
		checkResponse(t, r, route, rec)
	})
}

// bufferRequestBody reads the request body and puts it back, so both the
// check and the handler can read it.
func bufferRequestBody(r *http.Request) []byte {
	if r.Body == nil || r.Body == http.NoBody {
		return nil
	}
	b, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(b))
	return b
}

func checkRequest(t testing.TB, r *http.Request, route bridge.Route, body []byte) {
	t.Helper()
	if route.Body == nil || len(body) == 0 {
		return
	}
	if err := strictDecode(body, route.Body); err != nil {
		t.Errorf("oasometest: %s: request body does not match %s: %v", r.Pattern, route.Body, err)
	}
}

func checkResponse(t testing.TB, r *http.Request, route bridge.Route, rec *recorder) {
	t.Helper()
	status := rec.statusCode()
	typ, declared := route.Responses[status]
	if !declared {
		t.Errorf("oasometest: %s: response status %d is not declared (declared: %v)", r.Pattern, status, declaredStatuses(route))
		return
	}
	if err := checkResponseBody(rec.body.Bytes(), typ); err != nil {
		t.Errorf("oasometest: %s: response body for status %d: %v", r.Pattern, status, err)
	}
}

func declaredStatuses(route bridge.Route) []int {
	out := make([]int, 0, len(route.Responses))
	for s := range route.Responses {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// checkResponseBody checks a body against the declared type; a nil type
// means the body must be empty.
func checkResponseBody(body []byte, typ reflect.Type) error {
	if typ == nil {
		if len(body) != 0 {
			return errors.New("expected an empty body (ReturnsEmpty)")
		}
		return nil
	}
	if err := strictDecode(body, typ); err != nil {
		return errors.New("does not match " + typ.String() + ": " + err.Error())
	}
	return nil
}

// strictDecode decodes JSON into a new value of type typ, rejecting unknown
// fields and trailing data.
func strictDecode(body []byte, typ reflect.Type) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(reflect.New(typ).Interface()); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("unexpected data after the JSON value")
	}
	return nil
}

// recorder captures the status and body while writing through.
type recorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *recorder) statusCode() int {
	if r.status == 0 {
		return http.StatusOK
	}
	return r.status
}

// WriteHeader records the first final status and passes the call on.
func (r *recorder) WriteHeader(code int) {
	if r.status == 0 && (code >= 200 || code == http.StatusSwitchingProtocols) {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

// Write records the body and passes it on.
func (r *recorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	r.body.Write(p)
	return r.ResponseWriter.Write(p)
}

// Unwrap returns the underlying ResponseWriter, for http.ResponseController.
func (r *recorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
