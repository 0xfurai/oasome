package oasome_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"github.com/0xfurai/oasome"
)

func healthz(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }

func createUser(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }

// TestTargetUsage compiles and runs the usage from the design brief.
func TestTargetUsage(t *testing.T) {
	mux := http.NewServeMux()
	doc := oasome.New("My API", "1.0.0")

	// plain route, nothing to do with the library
	mux.HandleFunc("GET /healthz", healthz)

	// documented route: still mux.Handle, still your handler
	mux.Handle(doc.Route("POST /users/{id}", createUser,
		oasome.Body[CreateUserReq](),
		oasome.Returns[User](201),
	))

	// the spec is just another handler
	mux.Handle("GET /openapi.json", doc)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
	eq(t, "status", rec.Code, 200)
	eq(t, "Content-Type", rec.Header().Get("Content-Type"), "application/json")
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	checkTargetSpec(t, got)

	// the real handler still runs
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("POST", "/users/7", nil))
	eq(t, "handler status", rec.Code, http.StatusCreated)
}

func checkTargetSpec(t *testing.T, got map[string]any) {
	t.Helper()
	eq(t, "openapi", got["openapi"], "3.1.0")
	eq(t, "title", dig(got, "info", "title"), "My API")
	eq(t, "version", dig(got, "info", "version"), "1.0.0")
	eq(t, "undocumented route", dig(got, "paths", "/healthz"), nil)
	op := dig(got, "paths", "/users/{id}", "post")
	if op == nil {
		t.Fatalf("operation missing; paths = %v", got["paths"])
	}
	eq(t, "request schema", dig(op, "requestBody", "content", "application/json", "schema", "$ref"), ref("CreateUserReq"))
	eq(t, "201 schema", dig(op, "responses", "201", "content", "application/json", "schema", "$ref"), ref("User"))
	eq(t, "201 description", dig(op, "responses", "201", "description"), "Created")
	eq(t, "parameters", dig(op, "parameters"), []any{map[string]any{
		"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"},
	}})
}

type countingHandler struct{ n int }

func (h *countingHandler) ServeHTTP(http.ResponseWriter, *http.Request) { h.n++ }

func TestHandlerReturnedUnchanged(t *testing.T) {
	doc := oasome.New("t", "1")
	pattern, h := doc.Route("GET /a", noop, oasome.Returns[User](200))
	if pattern != "GET /a" {
		t.Errorf("pattern = %q", pattern)
	}
	fn, ok := h.(http.HandlerFunc)
	if !ok {
		t.Fatalf("Route returned %T, want http.HandlerFunc", h)
	}
	if reflect.ValueOf(fn).Pointer() != reflect.ValueOf(noop).Pointer() {
		t.Error("Route returned a different function")
	}

	orig := &countingHandler{}
	pattern, h = doc.RouteHandler("GET /b", orig)
	if pattern != "GET /b" || h != http.Handler(orig) {
		t.Errorf("RouteHandler returned (%q, %v), want the same handler value", pattern, h)
	}
}

func TestWithPrefix(t *testing.T) {
	doc := oasome.New("t", "1")
	api := doc.WithPrefix("/api")
	v1 := api.WithPrefix("/v1/")

	p1, _ := doc.Route("GET /root", noop, oasome.Returns[User](200))
	p2, _ := api.Route("GET /users/{id}", noop, oasome.Returns[User](200))
	p3, _ := v1.Route("POST /items/{$}", noop, oasome.Returns[User](201))
	// patterns must be returned unprefixed
	eq(t, "patterns", []string{p1, p2, p3}, []string{"GET /root", "GET /users/{id}", "POST /items/{$}"})

	s := spec(t, v1)
	eq(t, "paths", keys(dig(s, "paths")), []string{"/api/users/{id}", "/api/v1/items/", "/root"})
	// all views serve the whole document
	eq(t, "root view", spec(t, doc), s)
	eq(t, "api view", spec(t, api), s)
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	var viaView map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &viaView); err != nil {
		t.Fatal(err)
	}
	eq(t, "ServeHTTP on a view", viaView, s)
	// Same method and path through different views is a duplicate.
	mustPanic(t, "duplicate route", func() { doc.Route("GET /api/users/{id}", noop) })
}

func TestMountedSubMux(t *testing.T) {
	doc := oasome.New("t", "1")
	api := doc.WithPrefix("/api")
	sub := http.NewServeMux()
	sub.Handle(api.Route("GET /ping", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}, oasome.ReturnsEmpty(204)))
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", sub))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/ping", nil))
	if rec.Code != 204 {
		t.Errorf("status = %d", rec.Code)
	}
	if dig(spec(t, doc), "paths", "/api/ping", "get", "responses", "204") == nil {
		t.Error("documented path must include the prefix")
	}
}

func TestDefaultsAndOverrides(t *testing.T) {
	doc := oasome.New("t", "1",
		oasome.SecurityScheme("bearer", oasome.Scheme{Type: "http", Scheme: "bearer"}),
		oasome.SecurityScheme("key", oasome.Scheme{Type: "apiKey", In: "header", Name: "X-Key"}),
		oasome.Defaults(
			oasome.Returns[ErrorBody](401),
			oasome.Returns[ErrorBody](500),
			oasome.Security("bearer"),
			oasome.Tags("default"),
		),
	)
	doc.Route("GET /plain", noop, oasome.Returns[User](200))
	doc.Route("GET /override", noop, oasome.Returns[User](200), oasome.Returns[Other](401), oasome.Tags("mine"))
	doc.Route("GET /public", noop, oasome.ReturnsEmpty(204), oasome.NoSecurity())
	doc.Route("GET /either", noop, oasome.Returns[User](200),
		oasome.Security("key"), oasome.Security("bearer", "read", "write"))
	s := spec(t, doc)
	schema401 := func(path string) any {
		return dig(s, "paths", path, "get", "responses", "401", "content", "application/json", "schema", "$ref")
	}
	opKeys := func(path string) any { return keys(dig(s, "paths", path, "get")) }

	// default responses are declared on every route; a route's own response
	// replaces the default for that status
	for _, path := range []string{"/plain", "/override", "/either"} {
		eq(t, path+" default 500", dig(s, "paths", path, "get", "responses", "500", "description"), "Internal Server Error")
	}
	eq(t, "default 401", schema401("/plain"), ref("ErrorBody"))
	eq(t, "overridden 401", schema401("/override"), ref("Other"))

	// default security is top-level only
	eq(t, "top-level security", s["security"], []any{map[string]any{"bearer": []any{}}})
	eq(t, "plain op keys", opKeys("/plain"), []string{"responses", "tags"})
	eq(t, "override op keys", opKeys("/override"), []string{"responses", "tags"})

	// default tags apply and are replaced by route tags
	eq(t, "default tags", dig(s, "paths", "/plain", "get", "tags"), []any{"default"})
	eq(t, "route tags", dig(s, "paths", "/override", "get", "tags"), []any{"mine"})

	// NoSecurity emits an empty array
	eq(t, "NoSecurity", dig(s, "paths", "/public", "get", "security"), []any{})

	// repeated Security gives alternatives and replaces the default
	eq(t, "alternatives", dig(s, "paths", "/either", "get", "security"), []any{
		map[string]any{"key": []any{}},
		map[string]any{"bearer": []any{"read", "write"}},
	})
	eq(t, "bearer scheme", dig(s, "components", "securitySchemes", "bearer", "scheme"), "bearer")
	eq(t, "key scheme", dig(s, "components", "securitySchemes", "key", "name"), "X-Key")
}

func TestSecurityOverrideWithoutDefaults(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("GET /a", noop, oasome.Returns[User](200), oasome.NoSecurity())
	s := spec(t, doc)
	if _, has := s["security"]; has {
		t.Errorf("no default security, top-level must be absent: %v", s["security"])
	}
	if sec, ok := dig(s, "paths", "/a", "get", "security").([]any); !ok || len(sec) != 0 {
		t.Errorf("want security: [], got %v", dig(s, "paths", "/a", "get", "security"))
	}
}

func TestRouteWithoutResponses(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("GET /a", noop)
	r := dig(spec(t, doc), "paths", "/a", "get", "responses", "default", "description")
	if r == nil || r == "" {
		t.Errorf("a route without responses must get a default response, got %v", r)
	}
}

func TestReturnsEmptyHasNoContent(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("DELETE /a/{id}", noop, oasome.ReturnsEmpty(204))
	r := dig(spec(t, doc), "paths", "/a/{id}", "delete", "responses", "204")
	m, _ := r.(map[string]any)
	if m["description"] != "No Content" {
		t.Errorf("description = %v", m["description"])
	}
	if _, has := m["content"]; has {
		t.Error("ReturnsEmpty must not emit content")
	}
}

func TestOperationFields(t *testing.T) {
	doc := oasome.New("T", "9", oasome.DocDescription("About"),
		oasome.Server("https://a.example", "A"), oasome.Server("https://b.example", ""))
	doc.Route("GET /a", noop, oasome.Summary("Sum"), oasome.Description("Long"),
		oasome.Tags("x", "y"), oasome.OperationID("getA"), oasome.Deprecated(), oasome.Returns[User](200))
	s := spec(t, doc)
	op := dig(s, "paths", "/a", "get").(map[string]any)
	if op["summary"] != "Sum" || op["description"] != "Long" || op["operationId"] != "getA" || op["deprecated"] != true ||
		!reflect.DeepEqual(op["tags"], []any{"x", "y"}) {
		t.Errorf("operation = %v", op)
	}
	if dig(s, "info", "description") != "About" {
		t.Errorf("info = %v", s["info"])
	}
	wantServers := []any{
		map[string]any{"url": "https://a.example", "description": "A"},
		map[string]any{"url": "https://b.example"},
	}
	if !reflect.DeepEqual(s["servers"], wantServers) {
		t.Errorf("servers = %v", s["servers"])
	}
}

func TestParams(t *testing.T) {
	type P struct {
		ID    int64   `path:"id" doc:"User id" example:"42"`
		Limit int     `query:"limit,required" example:"10"`
		After *string `query:"after"`
		Trace string  `header:"X-Trace" doc:"Trace id"`
	}
	doc := oasome.New("t", "1")
	doc.Route("GET /users/{id}/files/{rest...}", noop, oasome.Params[P](), oasome.Returns[User](200))
	op := dig(spec(t, doc), "paths", "/users/{id}/files/{rest}", "get")
	got, _ := dig(op, "parameters").([]any)
	want := []any{
		map[string]any{"name": "id", "in": "path", "required": true, "description": "User id", "example": float64(42),
			"schema": map[string]any{"type": "integer", "format": "int64"}},
		map[string]any{"name": "rest", "in": "path", "required": true, "description": "Matches the remaining path.",
			"schema": map[string]any{"type": "string"}},
		map[string]any{"name": "X-Trace", "in": "header", "description": "Trace id",
			"schema": map[string]any{"type": "string"}},
		map[string]any{"name": "after", "in": "query",
			"schema": map[string]any{"type": "string"}},
		map[string]any{"name": "limit", "in": "query", "required": true, "example": float64(10),
			"schema": map[string]any{"type": "integer", "format": "int64"}},
	}
	if !reflect.DeepEqual(got, want) {
		gj, _ := json.MarshalIndent(got, "", " ")
		wj, _ := json.MarshalIndent(want, "", " ")
		t.Errorf("parameters:\n%s\nwant:\n%s", gj, wj)
	}
}

func TestParamsEmbeddedStruct(t *testing.T) {
	type Paging struct {
		Limit int `query:"limit"`
	}
	type P struct {
		Paging
		Sort string `query:"sort"`
	}
	doc := oasome.New("t", "1")
	doc.Route("GET /x", noop, oasome.Params[P]())
	got, _ := dig(spec(t, doc), "paths", "/x", "get", "parameters").([]any)
	if len(got) != 2 || dig(got[0], "name") != "limit" || dig(got[1], "name") != "sort" {
		t.Errorf("parameters = %v", got)
	}
}

func TestEndAnchorAndSubtreeDocumentSamePath(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("GET /items/{$}", noop)
	doc.Route("POST /items/", noop)
	paths := dig(spec(t, doc), "paths", "/items/")
	if dig(paths, "get") == nil || dig(paths, "post") == nil {
		t.Errorf("paths = %v", paths)
	}
	mustPanic(t, "duplicate route", func() { doc.Route("GET /items/", noop) })
}

func TestGetDocumentsOnlyGet(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("GET /a", noop)
	item, _ := dig(spec(t, doc), "paths", "/a").(map[string]any)
	if len(item) != 1 || item["get"] == nil {
		t.Errorf("path item = %v, want only get", item)
	}
}

func TestHostIsIgnoredButDuplicates(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("GET example.com/a", noop)
	if dig(spec(t, doc), "paths", "/a", "get") == nil {
		t.Error("host prefix must be ignored in the documented path")
	}
	mustPanic(t, "duplicate route", func() { doc.Route("GET other.example/a", noop) })
	doc.Route("POST other.example/a", noop)
}

func TestPanicsOnMisuse(t *testing.T) {
	type BadTag struct {
		Nope string `path:"nope"`
	}
	type BadOption struct {
		Q string `query:"q,optional"`
	}
	type TwoTags struct {
		Q string `query:"q" header:"q"`
	}
	type DupParam struct {
		A string `query:"q"`
		B string `query:"q"`
	}
	type Plain struct {
		ID string `path:"id"`
	}
	tests := []struct {
		name string
		want string
		f    func()
	}{
		{"no method", "no method", func() { oasome.New("t", "1").Route("/x", noop) }},
		{"bad method", "unsupported method", func() { oasome.New("t", "1").Route("FETCH /x", noop) }},
		{"bad wildcard", "whole path segment", func() { oasome.New("t", "1").Route("GET /x{a}", noop) }},
		{"duplicate route", "duplicate route", func() {
			d := oasome.New("t", "1")
			d.Route("GET /x", noop)
			d.Route("GET /x", noop)
		}},
		{"duplicate via handler", "duplicate route", func() {
			d := oasome.New("t", "1")
			d.Route("GET /x", noop)
			d.RouteHandler("GET /x", &countingHandler{})
		}},
		{"duplicate different wildcard names", "duplicate route", func() {
			d := oasome.New("t", "1")
			d.Route("GET /x/{a}", noop)
			d.Route("GET /x/{a}", noop)
		}},
		{"unknown path tag", "not a wildcard", func() {
			oasome.New("t", "1").Route("GET /x/{id}", noop, oasome.Params[BadTag]())
		}},
		{"path tag without wildcard", "not a wildcard", func() {
			oasome.New("t", "1").Route("GET /x", noop, oasome.Params[Plain]())
		}},
		{"duplicate operation id", "duplicate operation ID", func() {
			d := oasome.New("t", "1")
			d.Route("GET /x", noop, oasome.OperationID("same"))
			d.Route("GET /y", noop, oasome.OperationID("same"))
		}},
		{"params not a struct", "not a struct", func() { oasome.Params[int]() }},
		{"params pointer to struct", "not a struct", func() { oasome.Params[*Plain]() }},
		{"unknown tag option", "unknown tag option", func() { oasome.Params[BadOption]() }},
		{"two location tags", "more than one", func() { oasome.Params[TwoTags]() }},
		{"duplicate parameter", "twice", func() { oasome.Params[DupParam]() }},
		{"invalid status", "invalid response status", func() { oasome.Returns[User](99) }},
		{"invalid empty status", "invalid response status", func() { oasome.ReturnsEmpty(600) }},
		{"unsupported body type", "unsupported kind chan", func() {
			oasome.New("t", "1").Route("POST /x", noop, oasome.Body[chan int]())
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { mustPanic(t, tt.want, tt.f) })
	}
}

func TestFailedRegistrationLeavesDocUsable(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("GET /a", noop, oasome.OperationID("a"))
	mustPanic(t, "duplicate operation ID", func() { doc.Route("GET /b", noop, oasome.OperationID("a")) })
	// the failed route must not have claimed its path
	doc.Route("GET /b", noop, oasome.OperationID("b"))
	if dig(spec(t, doc), "paths", "/b", "get") == nil {
		t.Error("route /b missing after a failed registration")
	}
}

func TestCacheInvalidation(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("GET /a", noop)
	first, _ := doc.MarshalJSON()
	again, _ := doc.MarshalJSON()
	if string(first) != string(again) {
		t.Error("repeated MarshalJSON must be identical")
	}
	// mutating a returned slice must not corrupt the cache
	first[0] = 'X'
	if again2, _ := doc.MarshalJSON(); string(again2) != string(again) {
		t.Error("MarshalJSON must return a copy")
	}
	doc.Route("GET /b", noop)
	if dig(spec(t, doc), "paths", "/b") == nil {
		t.Error("new route must invalidate the cache")
	}
}

func TestMarshalIndentIsStable(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("GET /a", noop, oasome.Returns[User](200))
	a, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	if string(a) != string(b) || a[0] != '{' || a[1] != '\n' {
		t.Errorf("indented output unstable or not indented:\n%s", a)
	}
}

func TestConcurrency(t *testing.T) {
	doc := oasome.New("t", "1", oasome.Defaults(oasome.Returns[ErrorBody](500)))
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(3)
		go func() {
			defer wg.Done()
			view := doc.WithPrefix("/g" + strconv.Itoa(i))
			for j := range 20 {
				view.Route("GET /r"+strconv.Itoa(j)+"/{id}", noop, oasome.Returns[User](200), oasome.Body[CreateUserReq]())
			}
		}()
		go func() {
			defer wg.Done()
			for range 20 {
				if _, err := doc.MarshalJSON(); err != nil {
					t.Error(err)
				}
			}
		}()
		go func() {
			defer wg.Done()
			for range 20 {
				rec := httptest.NewRecorder()
				doc.ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
				if rec.Code != 200 {
					t.Errorf("status = %d", rec.Code)
				}
			}
		}()
	}
	wg.Wait()
	paths, _ := dig(spec(t, doc), "paths").(map[string]any)
	if len(paths) != 8*20 {
		t.Errorf("got %d paths, want %d", len(paths), 8*20)
	}
}

type brokenSchema struct{}

func (brokenSchema) OASSchema() map[string]any {
	return map[string]any{"x-broken": make(chan int)}
}

func TestSpecBuildError(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("GET /x", noop, oasome.Returns[brokenSchema](200))
	if _, err := doc.MarshalJSON(); err == nil {
		t.Error("MarshalJSON: want an error for a schema that cannot be encoded")
	}
	rec := httptest.NewRecorder()
	doc.ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
	eq(t, "status", rec.Code, http.StatusInternalServerError)
}

func TestNonStandardStatusDescription(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("GET /x", noop, oasome.ReturnsEmpty(299))
	got := dig(spec(t, doc), "paths", "/x", "get", "responses", "299", "description")
	eq(t, "description", got, "Status 299")
}

func TestTailWildcardKeepsDoc(t *testing.T) {
	type P struct {
		Rest string `path:"rest" doc:"File path."`
	}
	doc := oasome.New("t", "1")
	doc.Route("GET /files/{rest...}", noop, oasome.Params[P]())
	params, _ := dig(spec(t, doc), "paths", "/files/{rest}", "get", "parameters").([]any)
	if len(params) != 1 {
		t.Fatalf("parameters = %v", params)
	}
	eq(t, "description", dig(params[0], "description"), "File path. Matches the remaining path.")
}

func TestParamsTagPanics(t *testing.T) {
	type EmptyName struct {
		Q string `query:",required"`
	}
	type Unexported struct {
		q string `query:"q"`
	}
	mustPanic(t, "empty query name", func() { oasome.Params[EmptyName]() })
	mustPanic(t, "is unexported", func() { oasome.Params[Unexported]() })
}

func TestExtend(t *testing.T) {
	doc := oasome.New("t", "1",
		nil,
		oasome.Extend(nil),
		oasome.Extend(func(spec map[string]any) {
			spec["info"].(map[string]any)["license"] = map[string]any{"name": "MIT"}
		}),
		oasome.Extend(func(spec map[string]any) {
			license := dig(spec, "info", "license").(map[string]any)
			license["identifier"] = license["name"]
			spec["x-count"] = len(dig(spec, "paths").(map[string]any))
		}),
	)
	doc.Route("GET /a", noop, oasome.Returns[User](200))
	first := spec(t, doc)
	eq(t, "license", dig(first, "info", "license"), map[string]any{"name": "MIT", "identifier": "MIT"})
	eq(t, "x-count", first["x-count"], float64(1))
	eq(t, "openapi", first["openapi"], "3.1.0")

	// The functions run again when a new route invalidates the cache.
	doc.Route("GET /b", noop)
	eq(t, "x-count after a new route", spec(t, doc)["x-count"], float64(2))
}

func TestExtendOutputIsStable(t *testing.T) {
	doc := oasome.New("t", "1", oasome.Extend(func(spec map[string]any) { spec["x-a"] = 1 }))
	doc.Route("GET /a", noop, oasome.Returns[User](200))
	a, _ := doc.MarshalJSON()
	doc2 := oasome.New("t", "1", oasome.Extend(func(spec map[string]any) { spec["x-a"] = 1 }))
	doc2.Route("GET /a", noop, oasome.Returns[User](200))
	b, _ := doc2.MarshalJSON()
	eq(t, "same registrations give the same bytes", string(a), string(b))
}
