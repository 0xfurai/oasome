package oasome

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// httpMethods maps a stdlib pattern method to its OpenAPI operation key.
var httpMethods = map[string]string{
	"GET":     "get",
	"PUT":     "put",
	"POST":    "post",
	"DELETE":  "delete",
	"OPTIONS": "options",
	"HEAD":    "head",
	"PATCH":   "patch",
	"TRACE":   "trace",
}

type wildcard struct {
	name string
	tail bool
}

// parsedPattern is a stdlib ServeMux pattern split into its documented parts.
type parsedPattern struct {
	method    string // OpenAPI operation key, e.g. "get"
	host      string
	path      string // documented path, without any prefix
	wildcards []wildcard
}

// parsePattern parses "[METHOD ][HOST]/[PATH]" as accepted by http.ServeMux.
// A pattern without a method is an error.
func parsePattern(pat string) (parsedPattern, error) {
	var pp parsedPattern
	method, rest, err := splitMethod(pat)
	if err != nil {
		return pp, err
	}
	pp.method = method
	i := strings.IndexByte(rest, '/')
	if i < 0 {
		return pp, errors.New("pattern has no path; it must contain \"/\"")
	}
	pp.host = rest[:i]
	pp.path, pp.wildcards, err = parsePath(rest[i:])
	return pp, err
}

func splitMethod(pat string) (method, rest string, err error) {
	i := strings.IndexAny(pat, " \t")
	if i < 0 {
		return "", "", errors.New("pattern has no method; write \"METHOD /path\"")
	}
	name := pat[:i]
	rest = strings.TrimLeft(pat[i+1:], " \t")
	op, ok := httpMethods[name]
	if !ok {
		return "", "", fmt.Errorf("unsupported method %q", name)
	}
	return op, rest, nil
}

// parsePath converts a stdlib path to its documented form and lists its
// wildcards. "{name...}" becomes "{name}"; "{$}" becomes a trailing slash.
func parsePath(path string) (string, []wildcard, error) {
	segs := strings.Split(path[1:], "/")
	var b strings.Builder
	var wcs []wildcard
	for i, seg := range segs {
		out, wc, err := parseSegment(seg, i == len(segs)-1)
		if err != nil {
			return "", nil, err
		}
		if wc != nil {
			if err := checkDuplicateWildcard(wcs, wc.name); err != nil {
				return "", nil, err
			}
			wcs = append(wcs, *wc)
		}
		b.WriteByte('/')
		b.WriteString(out)
	}
	return b.String(), wcs, nil
}

func checkDuplicateWildcard(wcs []wildcard, name string) error {
	for _, w := range wcs {
		if w.name == name {
			return fmt.Errorf("wildcard %q appears more than once", name)
		}
	}
	return nil
}

// parseSegment returns the documented text of one segment and its wildcard,
// if any.
func parseSegment(seg string, last bool) (string, *wildcard, error) {
	if !strings.ContainsAny(seg, "{}") {
		return seg, nil, nil
	}
	if !strings.HasPrefix(seg, "{") || !strings.HasSuffix(seg, "}") {
		return "", nil, fmt.Errorf("segment %q: a wildcard must be a whole path segment", seg)
	}
	return parseWildcard(seg[1:len(seg)-1], last)
}

// parseWildcard handles the text between the braces of a wildcard segment.
func parseWildcard(inner string, last bool) (string, *wildcard, error) {
	if inner == "$" {
		if !last {
			return "", nil, errors.New("{$} must be at the end of the pattern")
		}
		return "", nil, nil
	}
	name, tail := strings.CutSuffix(inner, "...")
	if tail && !last {
		return "", nil, fmt.Errorf("{%s...} must be at the end of the pattern", name)
	}
	if !isIdentifier(name) {
		return "", nil, fmt.Errorf("invalid wildcard name %q", name)
	}
	return "{" + name + "}", &wildcard{name: name, tail: tail}, nil
}

func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || unicode.IsLetter(r) || (i > 0 && unicode.IsDigit(r)) {
			continue
		}
		return false
	}
	return true
}

// joinPrefix composes two path prefixes. The result has a leading slash and
// no trailing slash, or is empty.
func joinPrefix(a, b string) string {
	return normalizePrefix(a) + normalizePrefix(b)
}

func normalizePrefix(p string) string {
	p = strings.Trim(p, "/")
	if p == "" {
		return ""
	}
	return "/" + p
}
