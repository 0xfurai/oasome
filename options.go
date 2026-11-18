package oasome

import (
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
)

// RouteOption documents one aspect of a route. Create values with Body,
// Returns, ReturnsEmpty, ReturnsContent, ResponseHeader, Params, Summary,
// Description, Tags, OperationID, Deprecated, Security and NoSecurity.
type RouteOption interface {
	applyRoute(*routeConfig)
}

// DocOption configures a Doc. Create values with Defaults, DocDescription,
// Server, SecurityScheme, FieldSchema, Naming and Extend.
type DocOption interface {
	applyDoc(*docConfig)
}

type routeOptionFunc func(*routeConfig)

func (f routeOptionFunc) applyRoute(c *routeConfig) { f(c) }

type docOptionFunc func(*docConfig)

func (f docOptionFunc) applyDoc(c *docConfig) { f(c) }

// Option layers. Defaults are applied first, then the route's own options.
const (
	layerDefaults = 1
	layerRoute    = 2
)

type secReq struct {
	scheme string
	scopes []string
}

// routeConfig is the resolved documentation of one route.
type routeConfig struct {
	layer       int
	summary     string
	description string
	operationID string
	tags        []string
	deprecated  bool
	body        reflect.Type
	params      []paramField
	responses   map[int]*respDecl
	headers     map[int]map[string]headerDecl // by status, then lower-case name
	security    []secReq
	secLayer    int // layer that last set security
	noSecurity  bool
}

// respDecl is the declared response of one status: a body type for each
// media type. No media type means a response without a body.
type respDecl struct {
	layer   int
	content map[string]reflect.Type
}

// headerDecl is one documented response header.
type headerDecl struct {
	name        string
	description string
	typ         reflect.Type
}

func newRouteConfig() *routeConfig {
	return &routeConfig{responses: map[int]*respDecl{}, headers: map[int]map[string]headerDecl{}}
}

// response returns the declaration of a status for the current layer. A
// declaration from an earlier layer is dropped, so a route replaces a
// default response; declarations of one layer add up by media type.
func (c *routeConfig) response(status int) *respDecl {
	d := c.responses[status]
	if d == nil || d.layer != c.layer {
		d = &respDecl{layer: c.layer, content: map[string]reflect.Type{}}
		c.responses[status] = d
	}
	return d
}

func (c *routeConfig) apply(layer int, opts []RouteOption) {
	c.layer = layer
	for _, o := range opts {
		if o != nil {
			o.applyRoute(c)
		}
	}
}

// resetSecurity drops security set by an earlier layer, so that a route's
// Security or NoSecurity replaces the default instead of extending it.
func (c *routeConfig) resetSecurity() {
	if c.secLayer != c.layer {
		c.security = nil
		c.secLayer = c.layer
	}
}

// routeTypes lists every Go type the route documents.
func (c *routeConfig) routeTypes() []reflect.Type {
	var out []reflect.Type
	if c.body != nil {
		out = append(out, c.body)
	}
	for _, d := range c.responses {
		for _, t := range d.content {
			out = append(out, t)
		}
	}
	for _, hs := range c.headers {
		for _, h := range hs {
			out = append(out, h.typ)
		}
	}
	return out
}

type docConfig struct {
	description string
	servers     []specServer
	schemes     map[string]Scheme
	defaults    []RouteOption
	extend      []func(map[string]any)
	fieldHooks  []func(*SchemaField)
	naming      func(reflect.Type) string
}

// Body documents the JSON request body of a route as type T.
func Body[T any]() RouteOption {
	t := reflect.TypeFor[T]()
	return routeOptionFunc(func(c *routeConfig) { c.body = t })
}

// Returns documents a JSON response with the given status and body type T.
// The response description is http.StatusText(status). Declaring the same
// status again replaces the earlier declaration, which is how a route
// overrides a default response. It panics if status is not in 100-599.
func Returns[T any](status int) RouteOption {
	checkStatus(status)
	t := reflect.TypeFor[T]()
	return routeOptionFunc(func(c *routeConfig) { c.response(status).content[jsonMediaType] = t })
}

// ReturnsContent documents a response body of type T with a media type
// other than JSON, for example
//
//	oasome.ReturnsContent[string](200, "image/svg+xml")
//	oasome.ReturnsContent[Event](200, "application/x-ndjson")
//	oasome.ReturnsContent[any](200, "application/octet-stream")
//
// The schema is the schema of T; use any for binary data. One status can
// have several media types: declarations on one route add up, and the same
// media type replaces the earlier one. Like Returns, a declaration on a
// route replaces the response that Defaults gives for that status. It panics
// if status is not in 100-599 or if mediaType is not of the form type/subtype.
func ReturnsContent[T any](status int, mediaType string) RouteOption {
	checkStatus(status)
	if !strings.Contains(mediaType, "/") {
		panic(fmt.Sprintf("oasome: invalid media type %q", mediaType))
	}
	t := reflect.TypeFor[T]()
	return routeOptionFunc(func(c *routeConfig) { c.response(status).content[mediaType] = t })
}

// ResponseHeader documents a header of the response with the given status.
// T is the type of the header value, usually string or int. The route must
// declare that status, itself or through Defaults, or Route panics.
// Declaring the same status and name again replaces the earlier header.
func ResponseHeader[T any](status int, name, description string) RouteOption {
	checkStatus(status)
	h := headerDecl{name: name, description: description, typ: reflect.TypeFor[T]()}
	return routeOptionFunc(func(c *routeConfig) {
		if c.headers[status] == nil {
			c.headers[status] = map[string]headerDecl{}
		}
		c.headers[status][strings.ToLower(name)] = h
	})
}

// ReturnsEmpty documents a response with the given status and no body. It
// panics if status is not in 100-599.
func ReturnsEmpty(status int) RouteOption {
	checkStatus(status)
	return routeOptionFunc(func(c *routeConfig) {
		delete(c.responses, status)
		c.response(status)
	})
}

func checkStatus(status int) {
	if status < 100 || status > 599 {
		panic(fmt.Sprintf("oasome: invalid response status %d", status))
	}
}

// Params documents the path, query and header parameters of a route. T must
// be a struct whose fields carry path:"name", query:"name" or header:"name"
// tags. Path parameters are always required; query and header parameters are
// optional unless the tag has the option ",required", for example
// query:"limit,required". The doc and example tags apply to parameters too.
// Path wildcards of the pattern that T does not mention are still documented
// as required string parameters. Params panics if T is not a struct.
//
// Params can be used more than once, also in Defaults: the declarations are
// merged. A parameter with the same location and name as an earlier one
// replaces it.
func Params[T any]() RouteOption {
	ps := parseParams(reflect.TypeFor[T]())
	return routeOptionFunc(func(c *routeConfig) { c.params = mergeParams(c.params, ps.fields) })
}

// Summary sets the short summary of the operation.
func Summary(s string) RouteOption {
	return routeOptionFunc(func(c *routeConfig) { c.summary = s })
}

// Description sets the long description of the operation.
func Description(s string) RouteOption {
	return routeOptionFunc(func(c *routeConfig) { c.description = s })
}

// Tags sets the tags of the operation. A later Tags call replaces an earlier
// one, so a route's tags replace default tags.
func Tags(tags ...string) RouteOption {
	tags = slices.Clone(tags)
	return routeOptionFunc(func(c *routeConfig) { c.tags = tags })
}

// OperationID sets the operationId of the operation. Registering two routes
// with the same operation ID panics.
func OperationID(id string) RouteOption {
	return routeOptionFunc(func(c *routeConfig) { c.operationID = id })
}

// Deprecated marks the operation as deprecated.
func Deprecated() RouteOption {
	return routeOptionFunc(func(c *routeConfig) { c.deprecated = true })
}

// Security requires the named security scheme, with optional scopes. Using
// Security more than once on a route lists alternatives: any one of them
// satisfies the route (logical OR). On a route it replaces the security
// requirement from Defaults. The scheme should be declared with
// SecurityScheme.
func Security(scheme string, scopes ...string) RouteOption {
	scopes = slices.Clone(scopes)
	return routeOptionFunc(func(c *routeConfig) {
		c.resetSecurity()
		c.noSecurity = false
		c.security = append(c.security, secReq{scheme: scheme, scopes: scopes})
	})
}

// NoSecurity marks a route as public. It emits "security": [] on the
// operation, which overrides any document-wide requirement.
func NoSecurity() RouteOption {
	return routeOptionFunc(func(c *routeConfig) {
		c.resetSecurity()
		c.security = nil
		c.noSecurity = true
	})
}

// Defaults sets route options that apply to every route of the document.
// They are applied before the route's own options, so a route overrides a
// default: Returns for the same status replaces the default response, and
// Security or NoSecurity replaces the default security requirement. A
// default response cannot be removed from a single route. Security from
// Defaults is emitted once as the top-level security of the document.
func Defaults(opts ...RouteOption) DocOption {
	opts = slices.Clone(opts)
	return docOptionFunc(func(c *docConfig) { c.defaults = append(c.defaults, opts...) })
}

// DocDescription sets the description of the API in the info object.
func DocDescription(s string) DocOption {
	return docOptionFunc(func(c *docConfig) { c.description = s })
}

// Server adds a server to the document. It can be used more than once.
func Server(url, description string) DocOption {
	return docOptionFunc(func(c *docConfig) {
		c.servers = append(c.servers, specServer{URL: url, Description: description})
	})
}

// Extend registers a function that edits the finished document before it is
// cached and served. Use it for the parts of OpenAPI that have no option of
// their own, such as info.license, tag descriptions, response headers or
// x- extensions:
//
//	oasome.Extend(func(spec map[string]any) {
//		spec["info"].(map[string]any)["license"] = map[string]any{"name": "MIT"}
//	})
//
// The document is passed as decoded JSON: objects are map[string]any, arrays
// are []any and numbers are json.Number. Functions run in the order given,
// each time the document is built, so they must give the same result on
// every call and must not use the Doc. With Extend, the keys of the output
// are in alphabetical order at every level.
func Extend(fn func(spec map[string]any)) DocOption {
	return docOptionFunc(func(c *docConfig) {
		if fn != nil {
			c.extend = append(c.extend, fn)
		}
	})
}

// SchemaField is one struct field as a FieldSchema function sees it.
type SchemaField struct {
	// Struct is the struct type that declares the field.
	Struct reflect.Type
	// Field is the field itself. Read your own struct tags from Field.Tag.
	Field reflect.StructField
	// In is "body" for a property of a JSON body, or the parameter
	// location "path", "query" or "header" for a field of a Params struct.
	In string
	// Schema is the reflected schema of the field. Edit it in place, or
	// assign a new map.
	Schema map[string]any
	// Required says whether the property or parameter is required. Path
	// parameters are always required, whatever the function sets.
	Required bool
}

// FieldSchema registers a function that is called for every struct field
// that becomes a property of a JSON body or a parameter. Use it to apply
// your own struct tags, for example validation tags:
//
//	oasome.FieldSchema(func(f *oasome.SchemaField) {
//		if v, ok := f.Field.Tag.Lookup("minimum"); ok {
//			f.Schema["minimum"] = json.Number(v)
//		}
//		if f.Field.Tag.Get("required") == "true" {
//			f.Required = true
//		}
//	})
//
// The function runs after the doc and example tags are applied, each time a
// schema is built, so it must give the same result on every call. A type has
// one schema: the function cannot give a field different rules in a request
// and in a response. Functions run in the order given.
func FieldSchema(fn func(f *SchemaField)) DocOption {
	return docOptionFunc(func(c *docConfig) {
		if fn != nil {
			c.fieldHooks = append(c.fieldHooks, fn)
		}
	})
}

// Naming sets the function that names the components of named struct types.
// The result must match [a-zA-Z0-9._-]+ and must be different for different
// types; Route panics otherwise. An empty result gives the type its default
// name. The function must give the same result on every call.
//
//	oasome.Naming(func(t reflect.Type) string {
//		return path.Base(t.PkgPath()) + "." + t.Name()
//	})
func Naming(fn func(t reflect.Type) string) DocOption {
	return docOptionFunc(func(c *docConfig) { c.naming = fn })
}

// Ref returns a placeholder for the schema of T, for use in the result of an
// OASSchema method. A named struct becomes a $ref to its component, which is
// added to the document; another type is placed inline.
//
//	func (NotFoundOrConflict) OASSchema() map[string]any {
//		return map[string]any{"anyOf": []any{oasome.Ref[NotFound](), oasome.Ref[Conflict]()}}
//	}
//
// The placeholder is resolved inside map[string]any and []any values only.
// Do not refer to the type that the OASSchema method belongs to.
func Ref[T any]() any {
	return schemaRef{t: reflect.TypeFor[T]()}
}

// SecurityScheme declares a named security scheme in the components of the
// document. Declaring the same name again replaces the earlier declaration.
func SecurityScheme(name string, s Scheme) DocOption {
	return docOptionFunc(func(c *docConfig) { c.schemes[name] = s })
}

// Scheme is an OpenAPI security scheme object. Empty fields are omitted from
// the output. OAuth2 flows are not supported.
type Scheme struct {
	// Type is the scheme type: "http", "apiKey", "openIdConnect" or "mutualTLS".
	Type string `json:"type,omitempty"`
	// Scheme is the HTTP authorization scheme, such as "bearer" or "basic".
	Scheme string `json:"scheme,omitempty"`
	// BearerFormat is a hint for the bearer token format, such as "JWT".
	BearerFormat string `json:"bearerFormat,omitempty"`
	// In is the location of an API key: "query", "header" or "cookie".
	In string `json:"in,omitempty"`
	// Name is the header, query or cookie name of an API key.
	Name string `json:"name,omitempty"`
	// OpenIDConnectURL is the OpenID Connect discovery URL.
	OpenIDConnectURL string `json:"openIdConnectUrl,omitempty"`
	// Description describes the scheme.
	Description string `json:"description,omitempty"`
}

// statusText returns the response description for a status.
func statusText(status int) string {
	if s := http.StatusText(status); s != "" {
		return s
	}
	return fmt.Sprintf("Status %d", status)
}
