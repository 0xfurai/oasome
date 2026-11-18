package oasome

import (
	"cmp"
	"reflect"
	"slices"
	"strings"
	"unicode"
)

// field is one JSON object member of a struct, as encoding/json sees it.
type field struct {
	name    string
	index   []int
	typ     reflect.Type
	tagged  bool
	omit    bool // omitempty or omitzero: not required
	quoted  bool // ,string option
	doc     string
	example string
	sf      reflect.StructField
	owner   reflect.Type // the struct that declares the field
}

// walker explores a struct breadth first, like encoding/json's typeFields:
// shallower fields win, tagged fields win over untagged ones at the same
// depth, and equal candidates annihilate each other.
type walker struct {
	fields    []field
	next      []field
	count     map[reflect.Type]int
	nextCount map[reflect.Type]int
	visited   map[reflect.Type]bool
}

// structFields lists the fields encoding/json would encode for struct type t.
func structFields(t reflect.Type) []field {
	w := &walker{visited: map[reflect.Type]bool{}, next: []field{{typ: t}}}
	var current []field
	for len(w.next) > 0 {
		current, w.next = w.next, current[:0]
		w.count, w.nextCount = w.nextCount, map[reflect.Type]int{}
		for _, f := range current {
			w.scan(f)
		}
	}
	return resolveFields(w.fields)
}

func (w *walker) scan(parent field) {
	if w.visited[parent.typ] {
		return
	}
	w.visited[parent.typ] = true
	for i := range parent.typ.NumField() {
		w.scanField(parent, i)
	}
}

func (w *walker) scanField(parent field, i int) {
	sf := parent.typ.Field(i)
	if !fieldVisible(sf) {
		return
	}
	name, opts, ok := jsonTag(sf)
	if !ok {
		return
	}
	index := append(slices.Clone(parent.index), i)
	ft := unnamedPointerElem(sf.Type)
	if name != "" || !sf.Anonymous || ft.Kind() != reflect.Struct {
		w.add(parent, sf, ft, name, opts, index)
		return
	}
	w.nextCount[ft]++
	if w.nextCount[ft] == 1 {
		w.next = append(w.next, field{name: ft.Name(), index: index, typ: ft})
	}
}

// jsonTag returns the name and options of the json tag. ok is false for
// fields tagged "-".
func jsonTag(sf reflect.StructField) (name, opts string, ok bool) {
	tag := sf.Tag.Get("json")
	if tag == "-" {
		return "", "", false
	}
	name, opts, _ = strings.Cut(tag, ",")
	if !isValidTag(name) {
		name = ""
	}
	return name, opts, true
}

// unnamedPointerElem dereferences an unnamed pointer type, as encoding/json
// does for embedded fields.
func unnamedPointerElem(t reflect.Type) reflect.Type {
	if t.Name() == "" && t.Kind() == reflect.Pointer {
		return t.Elem()
	}
	return t
}

// fieldVisible mirrors encoding/json: unexported fields are skipped, except
// embedded structs, whose exported fields are promoted.
func fieldVisible(sf reflect.StructField) bool {
	if !sf.Anonymous {
		return sf.IsExported()
	}
	t := sf.Type
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return sf.IsExported() || t.Kind() == reflect.Struct
}

func (w *walker) add(parent field, sf reflect.StructField, ft reflect.Type, name, opts string, index []int) {
	f := field{
		name:    name,
		index:   index,
		typ:     sf.Type,
		tagged:  name != "",
		omit:    hasOption(opts, "omitempty") || hasOption(opts, "omitzero"),
		quoted:  hasOption(opts, "string") && quotable(ft),
		doc:     sf.Tag.Get("doc"),
		example: sf.Tag.Get("example"),
		sf:      sf,
		owner:   parent.typ,
	}
	if f.name == "" {
		f.name = sf.Name
	}
	w.fields = append(w.fields, f)
	if w.count[parent.typ] > 1 {
		// Several instances of the parent exist: add the field twice so
		// that the dominance rules see a conflict and drop it.
		w.fields = append(w.fields, f)
	}
}

func hasOption(opts, want string) bool {
	return slices.Contains(strings.Split(opts, ","), want)
}

// quotable reports whether the ,string option applies to t.
func quotable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

func isValidTag(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c) {
			continue
		}
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			return false
		}
	}
	return true
}

// resolveFields applies the Go embedding rules and returns the surviving
// fields in struct order.
func resolveFields(fields []field) []field {
	slices.SortFunc(fields, func(a, b field) int {
		return cmp.Or(
			strings.Compare(a.name, b.name),
			cmp.Compare(len(a.index), len(b.index)),
			compareTagged(a, b),
			slices.Compare(a.index, b.index),
		)
	})
	out := fields[:0]
	for i := 0; i < len(fields); {
		n := 1
		for i+n < len(fields) && fields[i+n].name == fields[i].name {
			n++
		}
		if d, ok := dominantField(fields[i : i+n]); ok {
			out = append(out, d)
		}
		i += n
	}
	slices.SortFunc(out, func(a, b field) int { return slices.Compare(a.index, b.index) })
	return out
}

// compareTagged orders tagged fields before untagged ones.
func compareTagged(a, b field) int {
	switch {
	case a.tagged == b.tagged:
		return 0
	case a.tagged:
		return -1
	}
	return 1
}

// dominantField picks the field that wins among same-named candidates, which
// are sorted by depth and then tagged first.
func dominantField(fields []field) (field, bool) {
	if len(fields) > 1 && len(fields[0].index) == len(fields[1].index) && fields[0].tagged == fields[1].tagged {
		return field{}, false
	}
	return fields[0], true
}
