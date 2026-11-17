// Package api holds fixture types for naming tests.
package api

// User is a fixture type that shares its bare name with models.User.
type User struct {
	Name string `json:"name"`
}
