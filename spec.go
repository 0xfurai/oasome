package oasome

import (
	"encoding/json"
	"reflect"
	"strconv"
)

const jsonMediaType = "application/json"

// noResponses is emitted for a route that declares no response. OpenAPI 3.1
// allows an operation without "responses", but many tools (and OpenAPI 3.0)
// require at least one, so a "default" placeholder keeps the output valid
// everywhere.
const noResponses = "No responses are documented for this operation."

type specDoc struct {
	OpenAPI    string                           `json:"openapi"`
	Info       specInfo                         `json:"info"`
	Servers    []specServer                     `json:"servers,omitempty"`
	Security   []map[string][]string            `json:"security,omitempty"`
	Paths      map[string]map[string]*operation `json:"paths"`
	Components *specComponents                  `json:"components,omitempty"`
}

type specInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

type specServer struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

type specComponents struct {
	Schemas         map[string]schema `json:"schemas,omitempty"`
	SecuritySchemes map[string]Scheme `json:"securitySchemes,omitempty"`
}

type operation struct {
	Tags        []string               `json:"tags,omitempty"`
	Summary     string                 `json:"summary,omitempty"`
	Description string                 `json:"description,omitempty"`
	OperationID string                 `json:"operationId,omitempty"`
	Deprecated  bool                   `json:"deprecated,omitempty"`
	Parameters  []parameter            `json:"parameters,omitempty"`
	RequestBody *requestBody           `json:"requestBody,omitempty"`
	Responses   map[string]response    `json:"responses"`
	Security    *[]map[string][]string `json:"security,omitempty"`
}

type parameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Schema      schema `json:"schema"`
	Example     any    `json:"example,omitempty"`
}

type mediaType struct {
	Schema schema `json:"schema"`
}

type requestBody struct {
	Required bool                 `json:"required"`
	Content  map[string]mediaType `json:"content"`
}

type response struct {
	Description string               `json:"description"`
	Content     map[string]mediaType `json:"content,omitempty"`
}

// build renders the document. The caller holds c.mu.
func (c *core) build() ([]byte, error) {
	g := newGen()
	paths := map[string]map[string]*operation{}
	for _, rt := range c.routes {
		item := paths[rt.path]
		if item == nil {
			item = map[string]*operation{}
			paths[rt.path] = item
		}
		item[rt.method] = g.operation(rt)
	}
	g.assignNames()
	doc := specDoc{
		OpenAPI:    "3.1.0",
		Info:       specInfo{Title: c.title, Version: c.version, Description: c.description},
		Servers:    c.servers,
		Security:   securityList(c.defaults.security),
		Paths:      paths,
		Components: c.components(g),
	}
	b, err := json.Marshal(doc)
	if err != nil || len(c.extend) == 0 {
		return b, err
	}
	return applyExtend(b, c.extend)
}

func (c *core) components(g *gen) *specComponents {
	comps := &specComponents{Schemas: g.components()}
	if len(c.schemes) > 0 {
		comps.SecuritySchemes = c.schemes
	}
	if len(comps.Schemas) == 0 && comps.SecuritySchemes == nil {
		return nil
	}
	return comps
}

func (g *gen) operation(rt *route) *operation {
	cfg := rt.cfg
	op := &operation{
		Tags:        cfg.tags,
		Summary:     cfg.summary,
		Description: cfg.description,
		OperationID: cfg.operationID,
		Deprecated:  cfg.deprecated,
		Parameters:  g.parameters(rt.params),
		Responses:   g.responses(cfg.responses),
	}
	if cfg.body != nil {
		op.RequestBody = &requestBody{Required: true, Content: g.content(cfg.body)}
	}
	if cfg.secLayer == layerRoute {
		list := securityList(cfg.security)
		if list == nil {
			list = []map[string][]string{}
		}
		op.Security = &list
	}
	return op
}

func (g *gen) content(t reflect.Type) map[string]mediaType {
	return map[string]mediaType{jsonMediaType: {Schema: g.schemaOf(t)}}
}

func (g *gen) parameters(fields []paramField) []parameter {
	var out []parameter
	for _, f := range fields {
		p := parameter{
			Name:        f.name,
			In:          f.in,
			Description: f.doc,
			Required:    f.required,
			Schema:      g.schemaOf(f.typ),
		}
		if f.tail {
			p.Description = joinWords(p.Description, tailNote)
		}
		if f.example != "" {
			p.Example = parseExample(f.typ, f.example, false)
		}
		out = append(out, p)
	}
	return out
}

func joinWords(a, b string) string {
	if a == "" {
		return b
	}
	return a + " " + b
}

func (g *gen) responses(declared map[int]reflect.Type) map[string]response {
	if len(declared) == 0 {
		return map[string]response{"default": {Description: noResponses}}
	}
	out := make(map[string]response, len(declared))
	for status, t := range declared {
		r := response{Description: statusText(status)}
		if t != nil {
			r.Content = g.content(t)
		}
		out[strconv.Itoa(status)] = r
	}
	return out
}

// securityList converts requirements to their JSON form, or nil if empty.
func securityList(reqs []secReq) []map[string][]string {
	var out []map[string][]string
	for _, r := range reqs {
		scopes := r.scopes
		if scopes == nil {
			scopes = []string{}
		}
		out = append(out, map[string][]string{r.scheme: scopes})
	}
	return out
}
