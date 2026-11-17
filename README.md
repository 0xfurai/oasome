# oasome

[![ci](https://github.com/0xfurai/oasome/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/0xfurai/oasome/actions/workflows/ci.yml)
[![coverage](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/0xfurai/oasome/badges/coverage.json)](https://github.com/0xfurai/oasome/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/0xfurai/oasome.svg)](https://pkg.go.dev/github.com/0xfurai/oasome)

A lightweight OpenAPI 3.1 document builder for Go's standard `http.ServeMux`.
You keep your real `*http.ServeMux` and your real handlers. You add
documentation at the place where you register the route.

- The mux is yours. There is no wrapper and no custom router.
- Documentation is opt-in per route. Other routes are untouched.
- There is no run-time behavior: no decoding, no validation, no error
  formatting. `Route` returns your handler unchanged.
- Only the standard library. `go.mod` lists no dependencies.

## Install

```sh
go get github.com/0xfurai/oasome
```

## Quick start

Copy this into `main.go`:

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/0xfurai/oasome"
)

type CreateUserReq struct {
	Name string `json:"name" doc:"Display name" example:"Ada"`
}

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func createUser(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(User{ID: r.PathValue("id"), Name: "Ada"})
}

func main() {
	mux := http.NewServeMux()
	doc := oasome.New("My API", "1.0.0")
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.Handle(doc.Route("POST /users/{id}", createUser,
		oasome.Body[CreateUserReq](),
		oasome.Returns[User](201),
	))
	mux.Handle("GET /openapi.json", doc)
	log.Fatal(http.ListenAndServe(":8080", mux))
}
```

`doc.Route` returns `(pattern string, handler http.Handler)`. Go lets you pass
both results straight into `mux.Handle`. Run it and open
`http://localhost:8080/openapi.json`.

## Example

The [`example/`](example) directory holds the quick start as a program,
[`example/main.go`](example/main.go), plus one route that uses most of the
route options, an enum, a full example and `Extend`. The spec generated from
it is
[`example/openapi.json`](example/openapi.json).

```sh
go run ./example        # serve on :8080, spec at /openapi.json
go generate ./example   # write example/openapi.json again
```

## Options reference

Route options (arguments of `Route` and `RouteHandler`, and of `Defaults`):

| Option | Effect |
| --- | --- |
| `Body[T]()` | JSON request body of type `T`. |
| `Returns[T](status)` | JSON response with this status and body type `T`. The description is `http.StatusText(status)`. Repeating a status replaces the earlier one. |
| `ReturnsEmpty(status)` | Response with a description and no body. |
| `Params[T]()` | Parameters from the struct `T`. Tags: `path:"name"`, `query:"name"`, `header:"name"`. The only tag option is `,required` (for query and header). Path parameters are always required. `doc:"..."` and `example:"..."` work too. A non-struct `T` panics. |
| `Summary(s)`, `Description(s)` | Text of the operation. |
| `Tags(tags...)` | Tags of the operation. A later `Tags` replaces an earlier one. |
| `OperationID(id)` | The `operationId`. A duplicate panics. |
| `Deprecated()` | Marks the operation as deprecated. |
| `Security(scheme, scopes...)` | Requires a scheme. Repeat it to list alternatives (OR). |
| `NoSecurity()` | Public route. Emits `"security": []`. |

Document options (arguments of `New`):

| Option | Effect |
| --- | --- |
| `Defaults(opts...)` | Route options applied to every route, before the route's own options. |
| `DocDescription(s)` | The description in `info`. |
| `Server(url, description)` | Adds a server. Repeatable. |
| `SecurityScheme(name, Scheme{...})` | Declares a security scheme. Declare every scheme that you name in `Security`. |
| `Extend(fn)` | Edits the finished document. See [Everything else](#everything-else-extend). |

`Scheme` has the fields `Type`, `Scheme`, `BearerFormat`, `In`, `Name`,
`OpenIDConnectURL` and `Description`. Empty fields are omitted.

### Defaults and overrides

```go
doc := oasome.New("API", "1.0.0",
	oasome.SecurityScheme("bearer", oasome.Scheme{Type: "http", Scheme: "bearer", BearerFormat: "JWT"}),
	oasome.Defaults(
		oasome.Returns[Problem](401),
		oasome.Returns[Problem](500),
		oasome.Security("bearer"),
	),
)
mux.Handle(doc.Route("GET /healthz", healthz, oasome.ReturnsEmpty(204), oasome.NoSecurity()))
```

- A route's `Returns[T](401)` replaces the default for 401.
- A route's `Security` or `NoSecurity` replaces the default security.
- The default security is written once, as the top-level `security`. An
  operation gets its own `security` only when the route overrides it.
- A route that declares no response, and has no default response, gets a
  `default` response with the description "No responses are documented for this
  operation." OpenAPI 3.1 does not require `responses`, but older tools do, and
  this keeps the output valid for all of them.

## Prefixes and sub-muxes

`WithPrefix` returns a view of the same document. Use it for a sub-mux that you
mount with `http.StripPrefix`. The sub-mux needs the pattern without the prefix.
The document needs the full path. The view does both:

```go
api := doc.WithPrefix("/api")      // nested calls compose: api.WithPrefix("/v1")
sub := http.NewServeMux()
sub.Handle(api.Route("GET /users", listUsers, oasome.Returns[[]User](200))) // pattern: "GET /users"
mux.Handle("/api/", http.StripPrefix("/api", sub))                       // documented: /api/users
```

`ServeHTTP` and `MarshalJSON` on a view give the whole document.

### Drift check in tests (`oasometest`)

`oasometest.Wrap` checks real exchanges against the documentation. It needs Go 1.23
or later, because it reads `http.Request.Pattern`.

```go
srv := httptest.NewServer(oasometest.Wrap(t, doc, mux))
```

For each request that matches a documented route, the test fails (with
`t.Errorf`, so it continues) when:

- the response status is not declared for the route (statuses from `Defaults`
  count as declared; a route that declares no response reports every status);
- the response body does not strictly decode into the declared Go type (unknown
  fields are errors; a `ReturnsEmpty` response must have an empty body);
- a request body was sent and does not strictly decode into the `Body` type.

Requests that match no documented route pass through unchecked.

`http.StripPrefix` copies the request, so a wrapper around the outer mux never
sees the sub-mux pattern. Wrap the sub-mux with the view that has the same
prefix:

```go
mux.Handle("/api/", http.StripPrefix("/api", oasometest.Wrap(t, api, sub)))
```

Routes are looked up by the pair (prefix of the view, mux pattern).

## Keep the spec in version control

Write the spec to a file from a test, and fail CI when the file changes.

```go
func TestWriteSpec(t *testing.T) {
	b, err := json.MarshalIndent(newDoc(), "", "  ") // newDoc builds your *oasome.Doc
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("openapi.json", append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

```sh
go test ./... && git diff --exit-code openapi.json
```

The output is deterministic and does not depend on the order of registration.
A `go generate` program that writes the same file works as well. Commit
`openapi.json` once, so that `git diff` has something to compare with.

## Custom schemas: the `OASSchema` hook

A type can supply its own schema with a method (value or pointer receiver):

```go
func (Money) OASSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": `^\d+\.\d{2}$`}
}
```

The result is used in place of reflection and is placed inline. The method is
called on a zero value. The library finds it through an unexported interface,
so it exports no name for it.

## Enums and examples: the `OASEnum` and `OASExample` hooks

A named type can list its values and give a full example with two more
optional methods:

```go
type Status string

func (Status) OASEnum() []Status { return []Status{"active", "done"} }

func (User) OASExample() User { return User{ID: "u_42", Name: "Ada"} }
```

- `OASEnum` returns a slice. Its values become the `enum` of the schema.
- `OASExample` returns one value. It becomes the `examples` of the schema. For
  a named struct, that is its component, so every request and response that
  uses the type shows the example.
- The values are encoded with `encoding/json`, so the document shows them
  exactly as the API sends them, and an example cannot drift from its type.
- Both methods are called on a zero value, with a value or a pointer receiver.
  A method with a wrong form, or a value that cannot be encoded, panics when
  the route is registered.
- A struct that embeds a type gets the methods of that type, as with
  `MarshalJSON`. Give the outer type its own method if the promoted one is
  wrong for it.

## Everything else: `Extend`

OpenAPI has many fields that this library has no option for. `Extend` lets you
set any of them. It receives the finished document as decoded JSON and edits it
in place:

```go
doc := oasome.New("My API", "1.0.0",
	oasome.Extend(func(spec map[string]any) {
		spec["info"].(map[string]any)["license"] = map[string]any{"name": "MIT"}
		spec["tags"] = []any{map[string]any{"name": "users", "description": "User accounts"}}
	}),
)
```

- Objects are `map[string]any`, arrays are `[]any`, numbers are `json.Number`.
- The functions run in the order given, each time the document is built. They
  must give the same result on every call, and they must not use the `Doc`.
- With `Extend`, the keys of the output are in alphabetical order at every
  level. The output is still deterministic.
- `oasometest` does not see these edits. It checks only what the route options
  declare.

## Schema rules

Go types follow `encoding/json`: tag names, `-`, `omitempty` and `omitzero`
(not required), `,string`, embedded struct flattening, unexported fields
skipped. Every other field is required.

- Pointers are nullable: `"type": ["string", "null"]`, or
  `anyOf: [{$ref}, {type: "null"}]` for a `$ref`. Slices and maps are not.
- `time.Time` is a `date-time` string. `[]byte` is a base64 string.
- Types with `MarshalJSON` give an unconstrained schema. Types with
  `MarshalText` give a string.
- Named structs go to `components/schemas`. Recursive types work. Anonymous
  structs and named non-struct types (`type Status string`) are inlined.
- Component names: the bare type name. If two types from different packages
  share a bare name, all of them get the package name as prefix
  (`models.User`, `api.User`). If that still collides, the sanitized import
  path is used. Generic types are folded (`Page[x.User]` becomes `Page_User`).
- Field tags: `doc:"..."` gives `description`, `example:"..."` gives `example`
  (parsed as number or bool when the field has that type).

## Pattern mapping

| Pattern | Documented as |
| --- | --- |
| `METHOD /path` | Operation `method` on `/path`. A pattern without a method panics. |
| `GET /x` | Only `get` (not `head`). |
| `/users/{id}` | Path parameter `id`: string, required. `Params[T]` can give it a type. |
| `/files/{rest...}` | Path `/files/{rest}`; a string parameter whose description says it matches the remaining path. |
| `/items/{$}` and `/items/` | Both `/items/`. The same method on both is a duplicate. |
| `GET example.com/path` | Path `/path`. The host is ignored. Two routes that differ only by host are a duplicate. |

Mistakes panic with a message that starts with `oasome: `: no method, an
unsupported method, a duplicate method and path, a duplicate operation ID, a
`Params` path tag that names no wildcard, a `Params` type that is not a struct,
a type that cannot be documented (channels, functions).

## Validate the spec

The golden file in `testdata/` is checked with the Redocly OpenAPI linter:

```sh
npx --yes @redocly/cli@2.77.0 lint testdata/library.golden.json testdata/minimal.golden.json --config redocly.yaml
```

`redocly.yaml` turns off style rules only (license, example.com servers,
trailing slash, operationId, 2xx/4xx presence). The structural checks stay on.
To refresh the golden files after an intended change, run
`go test -run TestGolden -update .`.

## What this does not do

- There is no request parsing, validation or response writing. It documents; it
  does not enforce.
- You cannot remove a default response from a single route. You can only replace
  it with another `Returns` for the same status.
- No OAuth2 flows. `Scheme` covers `http`, `apiKey`, `openIdConnect` and
  `mutualTLS` fields only.
- Only pointers are nullable. A nil slice or map is encoded as `null` by
  `encoding/json`, but its schema does not say so.
- A component is renamed when a new type with the same bare name is registered.
  The names depend only on the set of types, but that set can grow. Do not
  depend on a component name in client code when two packages share a type name.
- No YAML, no docs UI, no non-JSON content types, no file uploads, no `oneOf`,
  callbacks or webhooks. `Extend` can add fields for some of these by hand.
- `oasometest` needs Go 1.23 or later. There is no fallback for Go 1.22.

## License

MIT. See [LICENSE](LICENSE).
