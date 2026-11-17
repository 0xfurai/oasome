//go:build go1.23

package oasometest_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/0xfurai/oasome"
	"github.com/0xfurai/oasome/oasometest"
)

type CreateUserReq struct {
	Name string `json:"name"`
}

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type ErrorBody struct {
	Message string `json:"message"`
}

// fakeTB records failures instead of failing the real test.
type fakeTB struct {
	testing.TB
	errs []string
}

func (f *fakeTB) Errorf(format string, args ...any) {
	f.errs = append(f.errs, fmt.Sprintf(format, args...))
}
func (f *fakeTB) Helper() {}

const goodUser = `{"id":"1","name":"Ada","createdAt":"2024-01-02T03:04:05Z"}`

func writeJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// serve sends one request through the wrapped mux and returns the recorder.
func serve(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, rd))
	return rec
}

func expectErrors(t *testing.T, tb *fakeTB, want ...string) {
	t.Helper()
	if len(tb.errs) != len(want) {
		t.Fatalf("got %d failures %q, want %d", len(tb.errs), tb.errs, len(want))
	}
	for i, w := range want {
		if !strings.Contains(tb.errs[i], w) {
			t.Errorf("failure %d = %q, want it to contain %q", i, tb.errs[i], w)
		}
	}
}

func TestCorrectHandlerPasses(t *testing.T) {
	doc := oasome.New("t", "1")
	var seen string
	mux := http.NewServeMux()
	mux.Handle(doc.Route("POST /users/{id}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = string(b)
		writeJSON(w, 201, goodUser)
	}, oasome.Body[CreateUserReq](), oasome.Returns[User](201)))

	tb := &fakeTB{}
	rec := serve(oasometest.Wrap(tb, doc, mux), "POST", "/users/1", `{"name":"Ada"}`)
	expectErrors(t, tb)
	if rec.Code != 201 || rec.Body.String() != goodUser {
		t.Errorf("response was not written through: %d %q", rec.Code, rec.Body.String())
	}
	if seen != `{"name":"Ada"}` {
		t.Errorf("handler saw request body %q, want it unchanged", seen)
	}
}

func TestUndeclaredStatus(t *testing.T) {
	doc := oasome.New("t", "1")
	mux := http.NewServeMux()
	mux.Handle(doc.Route("GET /users/{id}", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 500, `{"message":"boom"}`)
	}, oasome.Returns[User](200)))

	tb := &fakeTB{}
	rec := serve(oasometest.Wrap(tb, doc, mux), "GET", "/users/1", "")
	expectErrors(t, tb, "status 500 is not declared")
	if !strings.Contains(tb.errs[0], "GET /users/{id}") || !strings.Contains(tb.errs[0], "[200]") {
		t.Errorf("message should name the route and the declared statuses: %q", tb.errs[0])
	}
	if rec.Code != 500 {
		t.Errorf("status must be passed through, got %d", rec.Code)
	}
}

func TestExtraResponseField(t *testing.T) {
	doc := oasome.New("t", "1")
	mux := http.NewServeMux()
	mux.Handle(doc.Route("GET /users/{id}", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"id":"1","name":"Ada","createdAt":"2024-01-02T03:04:05Z","extra":true}`)
	}, oasome.Returns[User](200)))

	tb := &fakeTB{}
	serve(oasometest.Wrap(tb, doc, mux), "GET", "/users/1", "")
	expectErrors(t, tb, `unknown field "extra"`)
}

func TestWrongResponseTypeAndTrailingData(t *testing.T) {
	doc := oasome.New("t", "1")
	mux := http.NewServeMux()
	mux.Handle(doc.Route("GET /a", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, `{"id":5}`)
	}, oasome.Returns[User](200)))
	mux.Handle(doc.Route("GET /b", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, goodUser+` {}`)
	}, oasome.Returns[User](200)))
	mux.Handle(doc.Route("GET /c", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}, oasome.Returns[User](200)))

	tb := &fakeTB{}
	h := oasometest.Wrap(tb, doc, mux)
	serve(h, "GET", "/a", "")
	serve(h, "GET", "/b", "")
	serve(h, "GET", "/c", "")
	expectErrors(t, tb, "does not match", "unexpected data", "EOF")
}

func TestBadRequestBody(t *testing.T) {
	doc := oasome.New("t", "1")
	handled := 0
	mux := http.NewServeMux()
	mux.Handle(doc.Route("POST /users", func(w http.ResponseWriter, r *http.Request) {
		handled++
		_, _ = io.Copy(io.Discard, r.Body)
		writeJSON(w, 201, goodUser)
	}, oasome.Body[CreateUserReq](), oasome.Returns[User](201)))

	tb := &fakeTB{}
	h := oasometest.Wrap(tb, doc, mux)
	serve(h, "POST", "/users", `{"name":"Ada","bogus":1}`)
	expectErrors(t, tb, `request body does not match`)
	if !strings.Contains(tb.errs[0], `unknown field "bogus"`) {
		t.Errorf("failure = %q", tb.errs[0])
	}
	if handled != 1 {
		t.Errorf("handler must still run, ran %d times", handled)
	}

	// no request body sent: not checked
	tb.errs = nil
	serve(h, "POST", "/users", "")
	expectErrors(t, tb)
}

func TestReturnsEmpty(t *testing.T) {
	doc := oasome.New("t", "1")
	mux := http.NewServeMux()
	mux.Handle(doc.Route("DELETE /ok", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
	}, oasome.ReturnsEmpty(204)))
	mux.Handle(doc.Route("DELETE /bad", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(204)
		_, _ = io.WriteString(w, `{}`)
	}, oasome.ReturnsEmpty(204)))

	tb := &fakeTB{}
	h := oasometest.Wrap(tb, doc, mux)
	serve(h, "DELETE", "/ok", "")
	expectErrors(t, tb)
	serve(h, "DELETE", "/bad", "")
	expectErrors(t, tb, "expected an empty body")
}

func TestDefaultsCountAsDeclared(t *testing.T) {
	doc := oasome.New("t", "1", oasome.Defaults(oasome.Returns[ErrorBody](500)))
	mux := http.NewServeMux()
	mux.Handle(doc.Route("GET /x", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 500, `{"message":"boom"}`)
	}, oasome.Returns[User](200)))

	tb := &fakeTB{}
	serve(oasometest.Wrap(tb, doc, mux), "GET", "/x", "")
	expectErrors(t, tb)
}

func TestImplicitStatusOK(t *testing.T) {
	doc := oasome.New("t", "1")
	mux := http.NewServeMux()
	mux.Handle(doc.Route("GET /x", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, goodUser)
	}, oasome.Returns[User](200)))

	tb := &fakeTB{}
	serve(oasometest.Wrap(tb, doc, mux), "GET", "/x", "")
	expectErrors(t, tb)
}

func TestUndocumentedRoutesPassThrough(t *testing.T) {
	doc := oasome.New("t", "1")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 418, `{"anything":"goes"}`)
	})
	mux.Handle(doc.Route("GET /doc", func(w http.ResponseWriter, _ *http.Request) {}, oasome.ReturnsEmpty(200)))

	tb := &fakeTB{}
	h := oasometest.Wrap(tb, doc, mux)
	rec := serve(h, "GET", "/healthz", "")
	serve(h, "GET", "/missing", "")
	expectErrors(t, tb)
	if rec.Code != 418 {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestPrefixedSubMux(t *testing.T) {
	doc := oasome.New("t", "1")
	api := doc.WithPrefix("/api")
	sub := http.NewServeMux()
	sub.Handle(api.Route("GET /users/{id}", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 500, `{"message":"boom"}`)
	}, oasome.Returns[User](200)))

	// Recipe: wrap the sub-mux with the view that registered its routes.
	tb := &fakeTB{}
	outer := http.NewServeMux()
	outer.Handle("/api/", http.StripPrefix("/api", oasometest.Wrap(tb, api, sub)))
	serve(outer, "GET", "/api/users/1", "")
	expectErrors(t, tb, "status 500 is not declared")

	// A view with another prefix finds nothing, so nothing is checked.
	tb2 := &fakeTB{}
	other := http.NewServeMux()
	other.Handle("/api/", http.StripPrefix("/api", oasometest.Wrap(tb2, doc.WithPrefix("/other"), sub)))
	serve(other, "GET", "/api/users/1", "")
	expectErrors(t, tb2)

	// Wrapping the outer mux does not see the sub-mux pattern either.
	tb3 := &fakeTB{}
	serve(oasometest.Wrap(tb3, api, outerOf(sub)), "GET", "/api/users/1", "")
	expectErrors(t, tb3)
}

func outerOf(sub http.Handler) http.Handler {
	outer := http.NewServeMux()
	outer.Handle("/api/", http.StripPrefix("/api", sub))
	return outer
}

type unwrapper interface{ Unwrap() http.ResponseWriter }

func TestRecorderUnwrap(t *testing.T) {
	doc := oasome.New("t", "1")
	mux := http.NewServeMux()
	var inner http.ResponseWriter
	mux.Handle(doc.Route("GET /x", func(w http.ResponseWriter, _ *http.Request) {
		u, ok := w.(unwrapper)
		if !ok {
			t.Error("response writer must have Unwrap() http.ResponseWriter")
			return
		}
		inner = u.Unwrap()
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush through ResponseController: %v", err)
		}
		writeJSON(w, 200, goodUser)
	}, oasome.Returns[User](200)))

	tb := &fakeTB{}
	rec := httptest.NewRecorder()
	oasometest.Wrap(tb, doc, mux).ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	expectErrors(t, tb)
	if inner != http.ResponseWriter(rec) {
		t.Error("Unwrap must return the real ResponseWriter")
	}
}

func TestInformationalStatusIgnored(t *testing.T) {
	doc := oasome.New("t", "1")
	mux := http.NewServeMux()
	mux.Handle(doc.Route("GET /x", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusEarlyHints)
		writeJSON(w, 200, goodUser)
	}, oasome.Returns[User](200)))

	tb := &fakeTB{}
	serve(oasometest.Wrap(tb, doc, mux), "GET", "/x", "")
	expectErrors(t, tb)
}

func TestHandlerThatWritesNothing(t *testing.T) {
	doc := oasome.New("t", "1")
	mux := http.NewServeMux()
	mux.Handle(doc.Route("POST /ok", func(http.ResponseWriter, *http.Request) {}, oasome.ReturnsEmpty(200)))
	mux.Handle(doc.Route("POST /bad", func(http.ResponseWriter, *http.Request) {}, oasome.ReturnsEmpty(204)))

	tb := &fakeTB{}
	h := oasometest.Wrap(tb, doc, mux)
	serve(h, "POST", "/ok", "")
	expectErrors(t, tb)
	serve(h, "POST", "/bad", "")
	expectErrors(t, tb, "response status 200 is not declared")
}
