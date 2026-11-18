package oasome

import (
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"sync"

	"github.com/0xfurai/oasome/internal/bridge"
)

func init() {
	bridge.Lookup = lookup
}

// Doc collects the documentation of routes registered on a *http.ServeMux
// and serves it as an OpenAPI 3.1 document. Create one with New. A Doc is
// safe for concurrent use.
type Doc struct {
	core   *core
	prefix string
}

// route is one documented operation.
type route struct {
	names   map[string]reflect.Type // component names from the Naming function
	pattern string                  // mux pattern, exactly as registered
	prefix  string                  // prefix of the view that registered it
	method  string                  // OpenAPI operation key
	path    string                  // documented path including the prefix
	cfg     *routeConfig
	params  []paramField
}

type routeKey struct {
	prefix  string
	pattern string
}

// core is the state shared by a Doc and all of its WithPrefix views.
type core struct {
	mu          sync.Mutex
	title       string
	version     string
	description string
	servers     []specServer
	schemes     map[string]Scheme
	defaultOpts []RouteOption
	defaults    *routeConfig // the defaults alone, for top-level security
	extend      []func(map[string]any)
	fieldHooks  []func(*SchemaField)
	naming      func(reflect.Type) string
	named       map[string]reflect.Type // names given by the Naming function

	routes  []*route
	index   map[routeKey]*route
	ops     map[string]bool // "method path" already registered
	opIDs   map[string]bool
	cache   []byte
	cacheOK bool
}

// New creates an empty document with the given API title and version.
func New(title, version string, opts ...DocOption) *Doc {
	dc := &docConfig{schemes: map[string]Scheme{}}
	for _, o := range opts {
		if o != nil {
			o.applyDoc(dc)
		}
	}
	defaults := newRouteConfig()
	defaults.apply(layerDefaults, dc.defaults)
	return &Doc{core: &core{
		title:       title,
		version:     version,
		description: dc.description,
		servers:     dc.servers,
		schemes:     dc.schemes,
		defaultOpts: dc.defaults,
		defaults:    defaults,
		extend:      dc.extend,
		fieldHooks:  dc.fieldHooks,
		naming:      dc.naming,
		named:       map[string]reflect.Type{},
		index:       map[routeKey]*route{},
		ops:         map[string]bool{},
		opIDs:       map[string]bool{},
	}}
}

// Route documents a route and returns the pattern and handler unchanged, so
// the result can be passed straight to ServeMux.Handle:
//
//	mux.Handle(doc.Route("POST /users/{id}", createUser, oasome.Body[CreateUserReq]()))
//
// The handler is returned as is: Route adds no runtime behavior. On a view
// created by WithPrefix the returned pattern is not prefixed, but the
// documented path is.
//
// Route panics, with a message starting "oasome: ", if the pattern has no
// method, uses an unsupported method, registers the same method and path
// twice, or if an option is inconsistent with the pattern, for example a
// Params path tag that names a wildcard the pattern does not have.
func (d *Doc) Route(pattern string, h http.HandlerFunc, opts ...RouteOption) (string, http.Handler) {
	d.register(pattern, opts)
	return pattern, h
}

// RouteHandler is like Route for an http.Handler. It returns the same
// handler value it was given.
func (d *Doc) RouteHandler(pattern string, h http.Handler, opts ...RouteOption) (string, http.Handler) {
	d.register(pattern, opts)
	return pattern, h
}

// WithPrefix returns a view of the same document that records routes under
// a path prefix. Use it for a sub-mux that is mounted with
// http.StripPrefix: the sub-mux needs the unprefixed pattern, and the
// document needs the full path. Nested calls compose. ServeHTTP and
// MarshalJSON on a view give the whole document.
func (d *Doc) WithPrefix(prefix string) *Doc {
	return &Doc{core: d.core, prefix: joinPrefix(d.prefix, prefix)}
}

// register records a route. It does not depend on the handler, so a typed
// variant could reuse it.
func (d *Doc) register(pattern string, opts []RouteOption) {
	pp, err := parsePattern(pattern)
	if err != nil {
		panic(fmt.Sprintf("oasome: %q: %v", pattern, err))
	}
	cfg := d.core.resolve(opts)
	rt := &route{
		pattern: pattern,
		prefix:  d.prefix,
		method:  pp.method,
		path:    d.prefix + pp.path,
		cfg:     cfg,
		params:  buildParams(pattern, pp, cfg.params),
	}
	checkHeaders(pattern, cfg)
	rt.names = d.core.checkTypes(rt)
	d.core.add(rt)
}

// newGen returns a schema generator with the hooks of the document.
func (c *core) newGen() *gen {
	g := newGen()
	g.hooks, g.naming = c.fieldHooks, c.naming
	return g
}

// checkHeaders rejects a ResponseHeader for a status the route does not
// declare.
func checkHeaders(pattern string, cfg *routeConfig) {
	for status, hs := range cfg.headers {
		if cfg.responses[status] != nil {
			continue
		}
		for _, h := range hs {
			panic(fmt.Sprintf("oasome: %q: ResponseHeader %q is for status %d, which the route does not declare", pattern, h.name, status))
		}
	}
}

// resolve applies the document defaults, then the route options.
func (c *core) resolve(opts []RouteOption) *routeConfig {
	cfg := newRouteConfig()
	cfg.apply(layerDefaults, c.defaultOpts)
	cfg.apply(layerRoute, opts)
	return cfg
}

// checkTypes builds the schemas once so that unsupported types panic when
// the route is registered, not when the document is first served. It
// returns the names that the Naming function gives to the types of the route.
func (c *core) checkTypes(rt *route) map[string]reflect.Type {
	g := c.newGen()
	for _, t := range rt.cfg.routeTypes() {
		g.schemaOf(t)
	}
	g.parameters(rt.params)
	return g.customNames()
}

// checkNames rejects a custom component name that another route already
// uses for a different type. The caller holds c.mu.
func (c *core) checkNames(rt *route) {
	for name, t := range rt.names {
		if other, ok := c.named[name]; ok && other != t {
			panic(fmt.Sprintf("oasome: Naming gave the name %q to two types: %s", name, typePair(other, t)))
		}
	}
}

func (c *core) add(rt *route) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := rt.method + " " + rt.path
	if c.ops[key] {
		panic(fmt.Sprintf("oasome: duplicate route %s %s (pattern %q)", rt.method, rt.path, rt.pattern))
	}
	id := rt.cfg.operationID
	if id != "" && c.opIDs[id] {
		panic(fmt.Sprintf("oasome: duplicate operation ID %q (pattern %q)", id, rt.pattern))
	}
	c.checkNames(rt)
	if id != "" {
		c.opIDs[id] = true
	}
	maps.Copy(c.named, rt.names)
	c.ops[key] = true
	c.routes = append(c.routes, rt)
	c.index[routeKey{rt.prefix, rt.pattern}] = rt
	c.cacheOK = false
}

// spec returns the cached document, building it when routes were added.
// The returned slice must not be modified.
func (c *core) spec() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cacheOK {
		return c.cache, nil
	}
	b, err := c.build()
	if err != nil {
		return nil, err
	}
	c.cache, c.cacheOK = b, true
	return b, nil
}

// MarshalJSON returns the OpenAPI 3.1 document as compact JSON. Output is
// deterministic. Use json.MarshalIndent for a readable form.
func (d *Doc) MarshalJSON() ([]byte, error) {
	b, err := d.core.spec()
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), b...), nil
}

// ServeHTTP serves the OpenAPI document as application/json.
func (d *Doc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, err := d.core.spec()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// lookup implements bridge.Lookup for package oasometest.
func lookup(doc any, pattern string) (bridge.Route, bool) {
	d, ok := doc.(*Doc)
	if !ok || d == nil {
		return bridge.Route{}, false
	}
	d.core.mu.Lock()
	rt, ok := d.core.index[routeKey{d.prefix, pattern}]
	d.core.mu.Unlock()
	if !ok {
		return bridge.Route{}, false
	}
	return bridgeRoute(rt.cfg), true
}

var anyType = reflect.TypeFor[any]()

// bridgeRoute gives package oasometest the declared types of a route.
func bridgeRoute(cfg *routeConfig) bridge.Route {
	out := bridge.Route{Responses: make(map[int]map[string]reflect.Type, len(cfg.responses))}
	if cfg.body != nil {
		out.Body = decodable(cfg.body)
	}
	for status, d := range cfg.responses {
		content := make(map[string]reflect.Type, len(d.content))
		for media, t := range d.content {
			content[media] = decodable(t)
		}
		out.Responses[status] = content
	}
	return out
}

// decodable returns the type that a body is decoded into for the drift
// check. A type with an OASSchema method describes its own JSON form, which
// the Go type does not show, so any valid JSON is accepted for it.
func decodable(t reflect.Type) reflect.Type {
	if t.Kind() != reflect.Interface && implements(t, overriderType) {
		return anyType
	}
	return t
}
