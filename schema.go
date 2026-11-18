package oasome

import (
	"encoding"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"time"
)

// schema is a JSON Schema object. Values are JSON-marshalable.
type schema = map[string]any

// schemaOverrider is implemented by types that supply their own schema.
// It is intentionally unexported; see the package documentation.
type schemaOverrider interface {
	OASSchema() map[string]any
}

var (
	timeType        = reflect.TypeFor[time.Time]()
	numberType      = reflect.TypeFor[json.Number]()
	marshalerType   = reflect.TypeFor[json.Marshaler]()
	textMarshalType = reflect.TypeFor[encoding.TextMarshaler]()
	overriderType   = reflect.TypeFor[schemaOverrider]()
)

// gen turns Go types into schemas. Named struct types become components;
// their final names are chosen once all types are known, so references are
// rendered lazily (see refName).
type gen struct {
	defs   map[reflect.Type]schema
	names  map[reflect.Type]string
	hooks  []func(*SchemaField)
	naming func(reflect.Type) string
}

func newGen() *gen {
	return &gen{defs: map[reflect.Type]schema{}}
}

// refName renders as a JSON string holding the $ref target of a component.
type refName struct {
	t reflect.Type
	g *gen
}

// MarshalJSON implements json.Marshaler.
func (r refName) MarshalJSON() ([]byte, error) {
	name, ok := r.g.names[r.t]
	if !ok {
		return nil, fmt.Errorf("oasome: no component name for %s", r.t)
	}
	return json.Marshal("#/components/schemas/" + name)
}

// assignNames fixes the component names. Call it after all types are visited.
// Types named by the Naming function keep that name; the others get a
// default name. It reports a name that two types share.
func (g *gen) assignNames() error {
	custom := g.customNames()
	byType := make(map[reflect.Type]string, len(custom))
	for name, t := range custom {
		byType[t] = name
	}
	var auto []reflect.Type
	for t := range g.defs {
		if _, ok := byType[t]; !ok {
			auto = append(auto, t)
		}
	}
	g.names = componentNames(auto)
	for _, t := range auto {
		if _, taken := custom[g.names[t]]; taken {
			return fmt.Errorf("oasome: component name %q is used by more than one type; check the Naming function", g.names[t])
		}
	}
	maps.Copy(g.names, byType)
	return nil
}

// customNames returns the names that the Naming function gives to the
// visited types. It panics on an invalid name and on a name for two types.
func (g *gen) customNames() map[string]reflect.Type {
	out := map[string]reflect.Type{}
	if g.naming == nil {
		return out
	}
	for t := range g.defs {
		name := g.naming(t)
		if name == "" {
			continue
		}
		if !componentKey.MatchString(name) {
			panic(fmt.Sprintf("oasome: Naming gave %q for %s: a component name must match [a-zA-Z0-9._-]+", name, t))
		}
		if other, ok := out[name]; ok {
			panic(fmt.Sprintf("oasome: Naming gave the name %q to two types: %s", name, typePair(other, t)))
		}
		out[name] = t
	}
	return out
}

// typePair names two types in a fixed order.
func typePair(a, b reflect.Type) string {
	x, y := identity(a), identity(b)
	if x > y {
		x, y = y, x
	}
	return x + " and " + y
}

// components returns the component schemas keyed by their final names.
func (g *gen) components() map[string]schema {
	out := make(map[string]schema, len(g.defs))
	for t, s := range g.defs {
		out[g.names[t]] = s
	}
	return out
}

// schemaOf returns the schema of t following encoding/json semantics.
func (g *gen) schemaOf(t reflect.Type) schema {
	if t.Kind() == reflect.Pointer {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		return nullable(g.schemaOf(t))
	}
	if s, ok := specialSchema(t); ok {
		return decorate(t, g.resolveRefs(s).(schema))
	}
	s := g.kindSchema(t)
	if t.Kind() == reflect.Struct {
		// A named struct is a $ref here; its component carries the hooks.
		return s
	}
	return decorate(t, s)
}

func implements(t, iface reflect.Type) bool {
	return t.Implements(iface) || reflect.PointerTo(t).Implements(iface)
}

// specialSchema handles types that have their own encoding: time.Time,
// json.Number, the OASSchema hook, json.Marshaler and encoding.TextMarshaler.
func specialSchema(t reflect.Type) (schema, bool) {
	switch {
	case t == timeType:
		return schema{"type": "string", "format": "date-time"}, true
	case t == numberType:
		return schema{"type": "number"}, true
	}
	if s, ok := overrideSchema(t); ok {
		return s, true
	}
	switch {
	case implements(t, marshalerType):
		return schema{}, true
	case implements(t, textMarshalType):
		return schema{"type": "string"}, true
	}
	return nil, false
}

// overrideSchema calls OASSchema on a zero value of t.
func overrideSchema(t reflect.Type) (schema, bool) {
	if t.Kind() == reflect.Interface || !implements(t, overriderType) {
		return nil, false
	}
	var v reflect.Value
	if t.Implements(overriderType) {
		v = reflect.Zero(t)
	} else {
		v = reflect.New(t)
	}
	s := maps.Clone(v.Interface().(schemaOverrider).OASSchema())
	if s == nil {
		s = schema{}
	}
	return s, true
}

// nullable makes a schema accept null. A $ref cannot take a type, and an
// enum would still reject null, so both are wrapped in anyOf.
func nullable(s schema) schema {
	if _, isRef := s["$ref"]; isRef {
		return anyOfNull(s)
	}
	if _, isEnum := s["enum"]; isEnum {
		return anyOfNull(s)
	}
	typ, ok := s["type"]
	if !ok {
		if len(s) == 0 {
			return s
		}
		return anyOfNull(s)
	}
	s["type"] = withNull(typ)
	return s
}

func anyOfNull(s schema) schema {
	return schema{"anyOf": []any{s, schema{"type": "null"}}}
}

func withNull(typ any) any {
	switch v := typ.(type) {
	case string:
		return []string{v, "null"}
	case []string:
		if !slices.Contains(v, "null") {
			return append(slices.Clone(v), "null")
		}
	}
	return typ
}

var scalarKinds = map[reflect.Kind]schema{
	reflect.Bool:    {"type": "boolean"},
	reflect.Int:     {"type": "integer", "format": "int64"},
	reflect.Int8:    {"type": "integer", "format": "int32"},
	reflect.Int16:   {"type": "integer", "format": "int32"},
	reflect.Int32:   {"type": "integer", "format": "int32"},
	reflect.Int64:   {"type": "integer", "format": "int64"},
	reflect.Uint:    {"type": "integer", "minimum": 0},
	reflect.Uint8:   {"type": "integer", "format": "int32", "minimum": 0},
	reflect.Uint16:  {"type": "integer", "format": "int32", "minimum": 0},
	reflect.Uint32:  {"type": "integer", "format": "int64", "minimum": 0},
	reflect.Uint64:  {"type": "integer", "minimum": 0},
	reflect.Uintptr: {"type": "integer", "minimum": 0},
	reflect.Float32: {"type": "number", "format": "float"},
	reflect.Float64: {"type": "number", "format": "double"},
	reflect.String:  {"type": "string"},
}

func scalarSchema(t reflect.Type) (schema, bool) {
	s, ok := scalarKinds[t.Kind()]
	return maps.Clone(s), ok
}

func (g *gen) kindSchema(t reflect.Type) schema {
	if s, ok := scalarSchema(t); ok {
		return s
	}
	switch t.Kind() {
	case reflect.Slice:
		return g.sliceSchema(t)
	case reflect.Array:
		return schema{"type": "array", "items": g.schemaOf(t.Elem()), "minItems": t.Len(), "maxItems": t.Len()}
	case reflect.Map:
		return g.mapSchema(t)
	case reflect.Struct:
		return g.structSchema(t)
	case reflect.Interface:
		return schema{}
	}
	panic(fmt.Sprintf("oasome: cannot document type %s: unsupported kind %s", t, t.Kind()))
}

func (g *gen) sliceSchema(t reflect.Type) schema {
	if isByteSlice(t) {
		return schema{"type": "string", "contentEncoding": "base64"}
	}
	return schema{"type": "array", "items": g.schemaOf(t.Elem())}
}

// isByteSlice reports whether encoding/json encodes t as base64.
func isByteSlice(t reflect.Type) bool {
	e := t.Elem()
	return e.Kind() == reflect.Uint8 && !implements(e, marshalerType) && !implements(e, textMarshalType)
}

func (g *gen) mapSchema(t reflect.Type) schema {
	k := t.Key()
	if !validMapKey(k) {
		panic(fmt.Sprintf("oasome: cannot document type %s: unsupported map key type %s", t, k))
	}
	return schema{"type": "object", "additionalProperties": g.schemaOf(t.Elem())}
}

func validMapKey(k reflect.Type) bool {
	switch k.Kind() {
	case reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return true
	}
	return implements(k, textMarshalType)
}

// structSchema inlines anonymous structs and registers named structs as
// components. The definition is reserved before it is built so recursive
// types terminate.
func (g *gen) structSchema(t reflect.Type) schema {
	if t.Name() == "" {
		return g.objectSchema(t)
	}
	if _, seen := g.defs[t]; !seen {
		g.defs[t] = nil
		g.defs[t] = decorate(t, g.objectSchema(t))
	}
	return schema{"$ref": refName{t: t, g: g}}
}

func (g *gen) objectSchema(t reflect.Type) schema {
	props := map[string]any{}
	var required []string
	for _, f := range structFields(t) {
		s, isRequired := g.hooked(f.owner, f.sf, inBody, g.fieldSchema(f), !f.omit)
		props[f.name] = s
		if isRequired {
			required = append(required, f.name)
		}
	}
	s := schema{"type": "object"}
	if len(props) > 0 {
		s["properties"] = props
	}
	if len(required) > 0 {
		sort.Strings(required)
		s["required"] = required
	}
	return s
}

func (g *gen) fieldSchema(f field) schema {
	var s schema
	if f.quoted {
		s = schema{"type": "string"}
		if f.typ.Kind() == reflect.Pointer {
			s = nullable(s)
		}
	} else {
		s = g.schemaOf(f.typ)
	}
	if f.doc != "" {
		s["description"] = f.doc
	}
	if f.example != "" {
		s["example"] = parseExample(f.typ, f.example, f.quoted)
	}
	return s
}

type exampleParser func(string) (any, bool)

var exampleParsers = map[reflect.Kind]exampleParser{
	reflect.Bool:    parseBoolExample,
	reflect.Int:     parseIntExample,
	reflect.Int8:    parseIntExample,
	reflect.Int16:   parseIntExample,
	reflect.Int32:   parseIntExample,
	reflect.Int64:   parseIntExample,
	reflect.Uint:    parseUintExample,
	reflect.Uint8:   parseUintExample,
	reflect.Uint16:  parseUintExample,
	reflect.Uint32:  parseUintExample,
	reflect.Uint64:  parseUintExample,
	reflect.Float32: parseFloatExample,
	reflect.Float64: parseFloatExample,
}

func parseBoolExample(s string) (any, bool) {
	v, err := strconv.ParseBool(s)
	return v, err == nil
}

func parseIntExample(s string) (any, bool) {
	v, err := strconv.ParseInt(s, 10, 64)
	return v, err == nil
}

func parseUintExample(s string) (any, bool) {
	v, err := strconv.ParseUint(s, 10, 64)
	return v, err == nil
}

func parseFloatExample(s string) (any, bool) {
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil && !math.IsNaN(v) && !math.IsInf(v, 0)
}

// parseExample converts an example tag to the type of its field when the
// field is a number or bool; otherwise the string is kept.
func parseExample(t reflect.Type, s string, quoted bool) any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	parse, ok := exampleParsers[t.Kind()]
	if quoted || !ok || implements(t, textMarshalType) || implements(t, marshalerType) {
		return s
	}
	if v, ok := parse(s); ok {
		return v
	}
	return s
}
