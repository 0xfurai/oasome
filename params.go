package oasome

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

const (
	inPath   = "path"
	inQuery  = "query"
	inHeader = "header"
)

// paramTags lists the struct tags understood by Params, in output order.
var paramTags = []string{inPath, inQuery, inHeader}

const tailNote = "Matches the remaining path."

// paramField is one documented parameter.
type paramField struct {
	name     string
	in       string
	required bool
	tail     bool
	typ      reflect.Type
	doc      string
	example  string
	sf       reflect.StructField
	owner    reflect.Type // nil for a wildcard that no Params struct mentions
}

// paramSet holds the parameters declared by a Params[T] struct.
type paramSet struct {
	typ    reflect.Type
	fields []paramField
	seen   map[string]bool
}

func parseParams(t reflect.Type) *paramSet {
	if t.Kind() != reflect.Struct {
		panic(fmt.Sprintf("oasome: Params type %s is not a struct", t))
	}
	ps := &paramSet{typ: t, seen: map[string]bool{}}
	ps.collect(t)
	return ps
}

func (ps *paramSet) collect(t reflect.Type) {
	for i := range t.NumField() {
		ps.addField(t, t.Field(i))
	}
}

func (ps *paramSet) addField(owner reflect.Type, sf reflect.StructField) {
	in, tag := paramTag(owner, sf)
	if in == "" {
		if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			ps.collect(sf.Type)
		}
		return
	}
	name, required := parseParamTag(owner, sf, in, tag)
	key := in + ":" + name
	if ps.seen[key] {
		panic(fmt.Sprintf("oasome: Params %s declares %s parameter %q twice", owner, in, name))
	}
	ps.seen[key] = true
	typ := sf.Type
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	ps.fields = append(ps.fields, paramField{
		name:     name,
		in:       in,
		required: required || in == inPath,
		typ:      typ,
		doc:      sf.Tag.Get("doc"),
		example:  sf.Tag.Get("example"),
		sf:       sf,
		owner:    owner,
	})
}

// mergeParams adds the fields of a later Params declaration to the earlier
// ones. A parameter with the same location and name replaces the earlier
// one, which is how a route overrides a default.
func mergeParams(base, added []paramField) []paramField {
	out := slices.Clone(base)
	for _, f := range added {
		same := func(o paramField) bool { return o.in == f.in && o.name == f.name }
		if i := slices.IndexFunc(out, same); i >= 0 {
			out[i] = f
			continue
		}
		out = append(out, f)
	}
	return out
}

// paramTag returns the location and raw tag of a field, or "" if the field
// has no parameter tag.
func paramTag(owner reflect.Type, sf reflect.StructField) (in, tag string) {
	for _, name := range paramTags {
		v, ok := sf.Tag.Lookup(name)
		if !ok {
			continue
		}
		if in != "" {
			panic(fmt.Sprintf("oasome: Params %s field %s has more than one of path, query and header tags", owner, sf.Name))
		}
		in, tag = name, v
	}
	return in, tag
}

func parseParamTag(owner reflect.Type, sf reflect.StructField, in, tag string) (string, bool) {
	name, opts, _ := strings.Cut(tag, ",")
	if name == "" {
		panic(fmt.Sprintf("oasome: Params %s field %s has an empty %s name", owner, sf.Name, in))
	}
	if !sf.IsExported() {
		panic(fmt.Sprintf("oasome: Params %s field %s is unexported", owner, sf.Name))
	}
	if opts == "" {
		return name, false
	}
	if opts != "required" {
		panic(fmt.Sprintf("oasome: Params %s field %s: unknown tag option %q (only \"required\" is supported)", owner, sf.Name, opts))
	}
	return name, true
}

var stringType = reflect.TypeFor[string]()

// buildParams merges the pattern wildcards with a Params declaration. Path
// parameters come first in pattern order; the others follow, sorted by
// location and name.
func buildParams(pattern string, pp parsedPattern, declared []paramField) []paramField {
	checkPathParams(pattern, pp, declared)
	out := make([]paramField, 0, len(pp.wildcards)+len(declared))
	for _, w := range pp.wildcards {
		pf := paramField{name: w.name, in: inPath, required: true, typ: stringType}
		if i := slices.IndexFunc(declared, func(d paramField) bool { return d.in == inPath && d.name == w.name }); i >= 0 {
			pf = declared[i]
		}
		pf.tail = w.tail
		out = append(out, pf)
	}
	return append(out, sortedNonPath(declared)...)
}

func checkPathParams(pattern string, pp parsedPattern, declared []paramField) {
	for _, d := range declared {
		if d.in != inPath {
			continue
		}
		if !slices.ContainsFunc(pp.wildcards, func(w wildcard) bool { return w.name == d.name }) {
			panic(fmt.Sprintf("oasome: Params path parameter %q is not a wildcard of pattern %q", d.name, pattern))
		}
	}
}

func sortedNonPath(declared []paramField) []paramField {
	var out []paramField
	for _, d := range declared {
		if d.in != inPath {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b paramField) int {
		if c := strings.Compare(a.in, b.in); c != 0 {
			return c
		}
		return strings.Compare(a.name, b.name)
	})
	return out
}
