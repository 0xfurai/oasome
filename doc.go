// Package oasome documents the routes of a standard library [http.ServeMux] as
// an OpenAPI 3.1 document, without changing how the routes behave.
//
// The mux and the handlers stay yours. Documentation goes at the
// registration site: [Doc.Route] returns the pattern and the handler
// unchanged, so its result can be passed directly to ServeMux.Handle.
//
//	mux := http.NewServeMux()
//	doc := oasome.New("My API", "1.0.0")
//
//	mux.HandleFunc("GET /healthz", healthz) // undocumented, untouched
//
//	mux.Handle(doc.Route("POST /users/{id}", createUser,
//		oasome.Body[CreateUserReq](),
//		oasome.Returns[User](201),
//	))
//
//	mux.Handle("GET /openapi.json", doc) // the spec is just a handler
//
// # No runtime behavior
//
// The library does not decode, validate or format anything at run time.
// Route and RouteHandler return the handler they were given. Package oasometest
// can check, in tests, that handlers still match their documentation.
//
// # Schemas
//
// Go types are reflected with encoding/json semantics: json tag names, "-",
// omitempty and omitzero (such fields are not required; every other field
// is), the ",string" option, embedded struct flattening, and unexported
// fields being skipped. Pointers are nullable. Named struct types become
// components and are referenced with $ref; recursive types work. The field
// tags doc:"..." and example:"..." add a description and an example.
//
// Types that implement [encoding/json.Marshaler] cannot be reflected and
// give an unconstrained schema. Types that implement
// [encoding.TextMarshaler] give a string. time.Time gives a date-time
// string.
//
// # Custom schemas
//
// A type, with a value or a pointer receiver, can supply its own schema by
// having this method:
//
//	func (Money) OASSchema() map[string]any {
//		return map[string]any{"type": "string", "pattern": `^\d+\.\d{2}$`}
//	}
//
// The result is used in place of reflection, inline, and is not stored in
// components. The method is found through an unexported interface, so the
// package exports no name for it. It is called on a zero value, so it must
// not depend on the receiver.
//
// # Enums and examples
//
// Two more optional methods add to the reflected schema of a named type:
//
//	func (Status) OASEnum() []Status { return []Status{"active", "done"} }
//	func (User) OASExample() User    { return User{ID: "u_42", Name: "Ada"} }
//
// OASEnum returns a slice whose values become the enum of the schema.
// OASExample returns one value that becomes its examples; for a named struct
// that is the component. The values are encoded with encoding/json. Both
// methods are called on a zero value and are found by name; a method with
// another form panics when the route is registered.
//
// # Everything else
//
// [Extend] registers a function that edits the finished document, for the
// parts of OpenAPI that have no option here.
//
// # Concurrency and determinism
//
// A Doc is safe for concurrent use. The document is built when first needed,
// cached, and rebuilt after a new route is registered. Output is
// deterministic and does not depend on the order of registration, so it can
// be committed and diffed.
package oasome
