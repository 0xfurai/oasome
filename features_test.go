package oasome_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/0xfurai/oasome"
)

type hookBody struct {
	Name  string `json:"name,omitempty" required:"true"`
	Age   int    `json:"age" optional:"true" minimum:"18"`
	Inner struct {
		X int `json:"x" minimum:"1"`
	} `json:"inner"`
	Gone string `json:"gone" drop:"true"`
}

type hookParams struct {
	Limit int    `query:"limit" required:"true" minimum:"1"`
	ID    string `path:"id" optional:"true"`
}

// tagHook applies the tags of the hook fixtures, as a user's function would.
func tagHook(seen *[]string) func(*oasome.SchemaField) {
	return func(f *oasome.SchemaField) {
		*seen = append(*seen, f.In+":"+f.Struct.Name()+"."+f.Field.Name)
		if v := f.Field.Tag.Get("minimum"); v != "" {
			f.Schema["minimum"] = json.Number(v)
		}
		if f.Field.Tag.Get("required") == "true" {
			f.Required = true
		}
		if f.Field.Tag.Get("optional") == "true" {
			f.Required = false
		}
		if f.Field.Tag.Get("drop") == "true" {
			f.Schema = nil
		}
	}
}

func TestFieldSchema(t *testing.T) {
	var seen []string
	doc := oasome.New("t", "1",
		oasome.FieldSchema(nil),
		oasome.FieldSchema(tagHook(&seen)),
		oasome.FieldSchema(func(f *oasome.SchemaField) {
			if _, ok := f.Schema["minimum"]; ok {
				f.Schema["x-second"] = true
			}
		}),
	)
	doc.Route("POST /items/{id}/{rest}", noop, oasome.Body[hookBody](), oasome.Params[hookParams]())
	s := spec(t, doc)

	body := dig(s, "components", "schemas", "hookBody")
	eq(t, "required", dig(body, "required"), []any{"gone", "inner", "name"})
	eq(t, "age", dig(body, "properties", "age"),
		map[string]any{"type": "integer", "format": "int64", "minimum": float64(18), "x-second": true})
	eq(t, "nested minimum", dig(body, "properties", "inner", "properties", "x", "minimum"), float64(1))
	eq(t, "nil schema becomes unconstrained", dig(body, "properties", "gone"), map[string]any{})

	params, _ := dig(s, "paths", "/items/{id}/{rest}", "post", "parameters").([]any)
	if len(params) != 3 {
		t.Fatalf("parameters = %v", params)
	}
	eq(t, "path stays required", dig(params[0], "required"), true)
	eq(t, "limit required", dig(params[2], "required"), true)
	eq(t, "limit minimum", dig(params[2], "schema", "minimum"), float64(1))

	for _, want := range []string{"body:hookBody.Name", "body:.X", "query:hookParams.Limit", "path:hookParams.ID"} {
		if !strings.Contains(strings.Join(seen, " "), want) {
			t.Errorf("hook calls %v do not include %q", seen, want)
		}
	}
	if strings.Contains(strings.Join(seen, " "), "rest") {
		t.Errorf("the hook must not see a wildcard without a struct field: %v", seen)
	}
}

func prefixNaming(t reflect.Type) string {
	if t.Name() == "ErrorBody" {
		return ""
	}
	return "x." + t.Name()
}

func TestNaming(t *testing.T) {
	doc := oasome.New("t", "1", oasome.Naming(prefixNaming))
	doc.Route("GET /a", noop, oasome.Returns[User](200), oasome.Returns[ErrorBody](400))
	s := spec(t, doc)
	eq(t, "component names", keys(dig(s, "components", "schemas")), []string{"ErrorBody", "x.User"})
	eq(t, "ref", dig(s, "paths", "/a", "get", "responses", "200", "content", "application/json", "schema", "$ref"),
		ref("x.User"))
}

func TestNamingPanics(t *testing.T) {
	same := oasome.Naming(func(reflect.Type) string { return "Same" })
	mustPanic(t, "must match", func() {
		doc := oasome.New("t", "1", oasome.Naming(func(reflect.Type) string { return "bad name" }))
		doc.Route("GET /a", noop, oasome.Returns[User](200))
	})
	mustPanic(t, `"Same" to two types`, func() {
		oasome.New("t", "1", same).Route("GET /a", noop, oasome.Returns[User](200), oasome.Returns[ErrorBody](400))
	})

	doc := oasome.New("t", "1", same)
	doc.Route("GET /a", noop, oasome.Returns[User](200), oasome.OperationID("a"))
	mustPanic(t, `"Same" to two types`, func() {
		doc.Route("GET /b", noop, oasome.Returns[ErrorBody](200), oasome.OperationID("b"))
	})
	// The failed registration left nothing behind.
	doc.Route("GET /c", noop, oasome.Returns[User](200), oasome.OperationID("b"))
	eq(t, "paths", keys(dig(spec(t, doc), "paths")), []string{"/a", "/c"})
}

func TestNamingCollisionWithDefaultName(t *testing.T) {
	doc := oasome.New("t", "1", oasome.Naming(func(t reflect.Type) string {
		if t.Name() == "User" {
			return "ErrorBody"
		}
		return ""
	}))
	doc.Route("GET /a", noop, oasome.Returns[User](200), oasome.Returns[ErrorBody](400))
	if _, err := doc.MarshalJSON(); err == nil || !strings.Contains(err.Error(), "more than one type") {
		t.Errorf("MarshalJSON error = %v, want a name collision", err)
	}
}

type NotFound struct {
	Message string `json:"message"`
}

type Conflict struct {
	Reason string `json:"reason"`
}

type notFoundOrConflict struct{}

func (notFoundOrConflict) OASSchema() map[string]any {
	return map[string]any{
		"anyOf":  []any{oasome.Ref[NotFound](), oasome.Ref[Conflict](), oasome.Ref[string]()},
		"x-deep": map[string]any{"first": oasome.Ref[NotFound]()},
	}
}

type badRef struct{}

func (badRef) OASSchema() map[string]any {
	// A typed slice is not searched for placeholders.
	return map[string]any{"anyOf": []map[string]any{{"not": oasome.Ref[NotFound]()}}}
}

func TestRef(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("GET /a", noop, oasome.Returns[notFoundOrConflict](404))
	s := spec(t, doc)
	got := dig(s, "paths", "/a", "get", "responses", "404", "content", "application/json", "schema")
	eq(t, "anyOf", dig(got, "anyOf"), []any{
		map[string]any{"$ref": ref("NotFound")},
		map[string]any{"$ref": ref("Conflict")},
		map[string]any{"type": "string"},
	})
	eq(t, "nested ref", dig(got, "x-deep", "first", "$ref"), ref("NotFound"))
	eq(t, "components", keys(dig(s, "components", "schemas")), []string{"Conflict", "NotFound"})
}

func TestRefOutsideOASSchema(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("GET /a", noop, oasome.Returns[badRef](200))
	if _, err := doc.MarshalJSON(); err == nil || !strings.Contains(err.Error(), "only valid in the result") {
		t.Errorf("MarshalJSON error = %v, want a misplaced Ref error", err)
	}
}

func TestReturnsContent(t *testing.T) {
	doc := oasome.New("t", "1", oasome.Defaults(oasome.Returns[ErrorBody](404)))
	doc.Route("GET /badge", noop,
		oasome.Returns[User](200),
		oasome.ReturnsContent[string](200, "image/svg+xml"),
		oasome.ReturnsContent[any](200, "application/octet-stream"),
		oasome.ReturnsContent[string](404, "text/plain"),
	)
	doc.Route("GET /plain", noop)
	responses := dig(spec(t, doc), "paths", "/badge", "get", "responses")
	eq(t, "200 media types", keys(dig(responses, "200", "content")),
		[]string{"application/json", "application/octet-stream", "image/svg+xml"})
	eq(t, "svg schema", dig(responses, "200", "content", "image/svg+xml", "schema"), map[string]any{"type": "string"})
	eq(t, "binary schema", dig(responses, "200", "content", "application/octet-stream", "schema"), map[string]any{})
	eq(t, "a route replaces the default response", keys(dig(responses, "404", "content")), []string{"text/plain"})
	eq(t, "the default stays elsewhere",
		keys(dig(spec(t, doc), "paths", "/plain", "get", "responses", "404", "content")), []string{"application/json"})

	mustPanic(t, "invalid media type", func() { oasome.ReturnsContent[string](200, "svg") })
	mustPanic(t, "invalid response status", func() { oasome.ReturnsContent[string](1, "a/b") })
}

func TestReturnsEmptyAndReturnsOrder(t *testing.T) {
	doc := oasome.New("t", "1")
	doc.Route("GET /emptied", noop, oasome.Returns[User](200), oasome.ReturnsEmpty(200))
	doc.Route("GET /filled", noop, oasome.ReturnsEmpty(200), oasome.Returns[User](200))
	s := spec(t, doc)
	eq(t, "emptied", dig(s, "paths", "/emptied", "get", "responses", "200", "content"), nil)
	eq(t, "filled", keys(dig(s, "paths", "/filled", "get", "responses", "200", "content")), []string{"application/json"})
}

func TestResponseHeader(t *testing.T) {
	doc := oasome.New("t", "1", oasome.Defaults(
		oasome.Returns[ErrorBody](429),
		oasome.ResponseHeader[int](429, "Retry-After", "Seconds to wait"),
	))
	doc.Route("GET /a", noop,
		oasome.Returns[User](200),
		oasome.ResponseHeader[string](200, "Content-Disposition", ""),
	)
	doc.Route("GET /b", noop, oasome.ResponseHeader[string](429, "retry-after", "A date"))
	s := spec(t, doc)
	eq(t, "default header", dig(s, "paths", "/a", "get", "responses", "429", "headers"), map[string]any{
		"Retry-After": map[string]any{"description": "Seconds to wait", "schema": map[string]any{"type": "integer", "format": "int64"}},
	})
	eq(t, "route header", dig(s, "paths", "/a", "get", "responses", "200", "headers"), map[string]any{
		"Content-Disposition": map[string]any{"schema": map[string]any{"type": "string"}},
	})
	eq(t, "a route replaces a default header", dig(s, "paths", "/b", "get", "responses", "429", "headers"), map[string]any{
		"retry-after": map[string]any{"description": "A date", "schema": map[string]any{"type": "string"}},
	})

	mustPanic(t, "which the route does not declare", func() {
		oasome.New("t", "1").Route("GET /a", noop, oasome.ResponseHeader[int](429, "Retry-After", ""))
	})
	mustPanic(t, "invalid response status", func() { oasome.ResponseHeader[int](0, "X", "") })
}

func TestParamsMerge(t *testing.T) {
	type Shared struct {
		Workspace string `header:"X-Workspace-ID,required" doc:"Workspace"`
		Limit     int    `query:"limit"`
	}
	type Own struct {
		ID    string `path:"id"`
		Limit string `query:"limit" doc:"Overridden"`
	}
	type More struct {
		Sort string `query:"sort"`
	}
	doc := oasome.New("t", "1", oasome.Defaults(oasome.Params[Shared]()))
	doc.Route("GET /a/{id}", noop, oasome.Params[Own](), oasome.Params[More]())
	doc.Route("GET /b", noop)
	got, _ := dig(spec(t, doc), "paths", "/a/{id}", "get", "parameters").([]any)
	want := []any{
		map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "X-Workspace-ID", "in": "header", "required": true, "description": "Workspace",
			"schema": map[string]any{"type": "string"}},
		map[string]any{"name": "limit", "in": "query", "description": "Overridden", "schema": map[string]any{"type": "string"}},
		map[string]any{"name": "sort", "in": "query", "schema": map[string]any{"type": "string"}},
	}
	eq(t, "merged parameters", got, want)
	shared, _ := dig(spec(t, doc), "paths", "/b", "get", "parameters").([]any)
	eq(t, "the defaults are not changed by a route", len(shared), 2)
	eq(t, "default limit type", dig(shared[1], "schema", "type"), "integer")
}
