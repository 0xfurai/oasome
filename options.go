package oasome

import (
	"fmt"
	"net/http"
	"reflect"
	"slices"
)

// RouteOption documents one aspect of a route. Create values with Body,
// Returns, ReturnsEmpty, Params, Summary, Description, Tags, OperationID,
// Deprecated, Security and NoSecurity.
type RouteOption interface {
	applyRoute(*routeConfig)
}

// DocOption configures a Doc. Create values with Defaults, DocDescription,
// Server and SecurityScheme.
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
	params      *paramSet
	responses   map[int]reflect.Type // nil type: no body
	security    []secReq
	secLayer    int // layer that last set security
	noSecurity  bool
}

func newRouteConfig() *routeConfig {
	return &routeConfig{responses: map[int]reflect.Type{}}
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
	for _, t := range c.responses {
		if t != nil {
			out = append(out, t)
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
	return routeOptionFunc(func(c *routeConfig) { c.responses[status] = t })
}

// ReturnsEmpty documents a response with the given status and no body. It
// panics if status is not in 100-599.
func ReturnsEmpty(status int) RouteOption {
	checkStatus(status)
	return routeOptionFunc(func(c *routeConfig) { c.responses[status] = nil })
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
// as required string parameters. Params panics if T is not a struct. A later
// Params replaces an earlier one.
func Params[T any]() RouteOption {
	ps := parseParams(reflect.TypeFor[T]())
	return routeOptionFunc(func(c *routeConfig) { c.params = ps })
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
