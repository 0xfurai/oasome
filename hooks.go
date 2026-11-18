package oasome

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
)

// Names of the optional methods that add to the reflected schema of a type.
const (
	enumHook    = "OASEnum"
	exampleHook = "OASExample"
)

// decorate adds the results of the OASEnum and OASExample hooks of t to s.
func decorate(t reflect.Type, s schema) schema {
	if vals := hookEnum(t); len(vals) > 0 {
		s["enum"] = vals
	}
	if ex, ok := hookExample(t); ok {
		s["examples"] = []any{ex}
	}
	return s
}

// callHook calls the named method on a zero value of t. The method can have
// a value or a pointer receiver. Unnamed types are skipped, so a method that
// an anonymous struct gets from an embedded field is not used.
func callHook(t reflect.Type, name string) (reflect.Value, bool) {
	if t.Name() == "" || t.Kind() == reflect.Interface {
		return reflect.Value{}, false
	}
	m := reflect.New(t).MethodByName(name)
	if !m.IsValid() {
		return reflect.Value{}, false
	}
	if mt := m.Type(); mt.NumIn() != 0 || mt.NumOut() != 1 {
		panic(fmt.Sprintf("oasome: %s.%s must take no arguments and return one value", t, name))
	}
	return m.Call(nil)[0], true
}

// hookExample returns the JSON form of the OASExample result of t.
func hookExample(t reflect.Type) (json.RawMessage, bool) {
	v, ok := callHook(t, exampleHook)
	if !ok {
		return nil, false
	}
	b := marshalHook(t, exampleHook, v)
	return b, !bytes.Equal(b, []byte("null"))
}

// hookEnum returns the JSON forms of the OASEnum values of t.
func hookEnum(t reflect.Type) []any {
	v, ok := callHook(t, enumHook)
	if !ok {
		return nil
	}
	if v.Kind() != reflect.Slice {
		panic(fmt.Sprintf("oasome: %s.%s must return a slice", t, enumHook))
	}
	out := make([]any, v.Len())
	for i := range out {
		out[i] = marshalHook(t, enumHook, v.Index(i))
	}
	return out
}

// marshalHook encodes a hook result with encoding/json, so the document
// shows the value exactly as the API would send it.
func marshalHook(t reflect.Type, name string, v reflect.Value) json.RawMessage {
	b, err := json.Marshal(v.Interface())
	if err != nil {
		panic(fmt.Sprintf("oasome: %s.%s: %v", t, name, err))
	}
	return b
}

// applyExtend runs the Extend functions on the encoded document.
func applyExtend(doc []byte, fns []func(map[string]any)) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var spec map[string]any
	if err := dec.Decode(&spec); err != nil {
		return nil, err
	}
	for _, fn := range fns {
		fn(spec)
	}
	return json.Marshal(spec)
}

// inBody is SchemaField.In for a property of a JSON body.
const inBody = "body"

// componentKey matches a valid components/schemas key.
var componentKey = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// hooked runs the FieldSchema functions on one field and returns the schema
// and the required flag that they leave.
func (g *gen) hooked(owner reflect.Type, sf reflect.StructField, in string, s schema, required bool) (schema, bool) {
	if len(g.hooks) == 0 {
		return s, required
	}
	f := &SchemaField{Struct: owner, Field: sf, In: in, Schema: s, Required: required}
	for _, hook := range g.hooks {
		hook(f)
	}
	if f.Schema == nil {
		f.Schema = schema{}
	}
	return f.Schema, f.Required
}

// schemaRef is the placeholder that Ref returns.
type schemaRef struct {
	t reflect.Type
}

// MarshalJSON fails: a placeholder that reaches the encoder was used outside
// the result of an OASSchema method.
func (r schemaRef) MarshalJSON() ([]byte, error) {
	return nil, errors.New("oasome: Ref[" + r.t.String() + "] is only valid in the result of an OASSchema method")
}

// resolveRefs returns a copy of v in which every Ref placeholder is replaced
// by the schema of its type. It looks inside map[string]any and []any.
func (g *gen) resolveRefs(v any) any {
	switch x := v.(type) {
	case schemaRef:
		return g.schemaOf(x.t)
	case map[string]any:
		out := make(schema, len(x))
		for k, e := range x {
			out[k] = g.resolveRefs(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = g.resolveRefs(e)
		}
		return out
	}
	return v
}
