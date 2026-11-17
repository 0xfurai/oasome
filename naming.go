package oasome

import (
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// componentNames chooses a unique components/schemas key for every type.
// The result depends only on the set of types, never on their order.
//
// A type uses its bare name. All types that share a bare name use the
// package name as a prefix; those that still collide use the sanitized full
// import path. A final numeric suffix guarantees uniqueness for the rare
// remainder, such as types declared inside functions.
func componentNames(types []reflect.Type) map[reflect.Type]string {
	names := map[reflect.Type]string{}
	byShort := groupBy(types, shortName)
	var collided []reflect.Type
	for short, ts := range byShort {
		if len(ts) == 1 {
			names[ts[0]] = short
			continue
		}
		collided = append(collided, ts...)
	}
	var again []reflect.Type
	for name, ts := range groupBy(collided, pkgQualifiedName) {
		if len(ts) == 1 {
			names[ts[0]] = name
			continue
		}
		again = append(again, ts...)
	}
	for _, t := range again {
		names[t] = sanitize(identity(t))
	}
	return uniquify(names)
}

func groupBy(types []reflect.Type, key func(reflect.Type) string) map[string][]reflect.Type {
	out := map[string][]reflect.Type{}
	for _, t := range types {
		k := key(t)
		out[k] = append(out[k], t)
	}
	return out
}

// identity is a string that is unique to the type.
func identity(t reflect.Type) string {
	return t.PkgPath() + "." + t.Name()
}

// uniquify appends _2, _3, ... to names that are still taken, visiting types
// in a fixed order.
func uniquify(names map[reflect.Type]string) map[reflect.Type]string {
	types := make([]reflect.Type, 0, len(names))
	for t := range names {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool { return identity(types[i]) < identity(types[j]) })
	used := map[string]bool{}
	out := make(map[reflect.Type]string, len(names))
	for _, t := range types {
		name := names[t]
		for i := 2; used[name]; i++ {
			name = names[t] + "_" + strconv.Itoa(i)
		}
		used[name] = true
		out[t] = name
	}
	return out
}

var majorVersion = regexp.MustCompile(`^v[0-9]+$`)

// pkgName approximates the package name by the last element of the import
// path, skipping a major version suffix such as v2.
func pkgName(t reflect.Type) string {
	parts := strings.Split(t.PkgPath(), "/")
	last := parts[len(parts)-1]
	if len(parts) > 1 && majorVersion.MatchString(last) {
		last = parts[len(parts)-2]
	}
	return sanitize(last)
}

func pkgQualifiedName(t reflect.Type) string {
	return pkgName(t) + "." + shortName(t)
}

// shortName is the type name without package qualifiers. Generic arguments
// are folded into the name: Page[example.com/x.User] becomes Page_User.
func shortName(t reflect.Type) string {
	name := t.Name()
	base, args, generic := strings.Cut(name, "[")
	if !generic {
		return sanitize(name)
	}
	parts := []string{base}
	for _, tok := range strings.FieldsFunc(args, isTypeDelimiter) {
		parts = append(parts, lastIdentifier(tok))
	}
	return sanitize(strings.Join(parts, "_"))
}

func isTypeDelimiter(r rune) bool {
	return strings.ContainsRune("[],*& ", r)
}

// lastIdentifier turns "example.com/x.User" into "User".
func lastIdentifier(tok string) string {
	if i := strings.LastIndexByte(tok, '/'); i >= 0 {
		tok = tok[i+1:]
	}
	if i := strings.LastIndexByte(tok, '.'); i >= 0 {
		tok = tok[i+1:]
	}
	return tok
}

var invalidKeyChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// sanitize makes s a valid component key: [a-zA-Z0-9._-]+.
func sanitize(s string) string {
	return strings.Trim(invalidKeyChars.ReplaceAllString(s, "_"), "_")
}
