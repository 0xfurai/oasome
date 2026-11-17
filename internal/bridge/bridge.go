// Package bridge lets package oasometest read route metadata from package oasome
// without oasome exporting any extra names. Package oasome assigns Lookup in its
// init function, in the style of the standard library's internal hooks.
package bridge

import "reflect"

// Route is the documented contract of one route.
type Route struct {
	// Body is the declared request body type, or nil.
	Body reflect.Type
	// Responses maps each declared status to its Go type. A nil type means
	// the response has no body (ReturnsEmpty).
	Responses map[int]reflect.Type
}

// Lookup finds the route registered with the given mux pattern on the
// document view doc (an *oasome.Doc, passed as any to avoid an import cycle).
// It is set by package oasome.
var Lookup func(doc any, pattern string) (Route, bool)
