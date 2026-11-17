// Package api holds fixture types for naming tests. Its import path ends in
// a major version element.
package api

// User is a fixture type that shares its bare name with models.User.
type User struct {
	Handle string `json:"handle"`
}
