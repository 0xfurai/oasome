// Package models holds fixture types for naming tests.
package models

// User is a fixture type.
type User struct {
	ID string `json:"id"`
}

// Page is a generic fixture type.
type Page[T any] struct {
	Items []T `json:"items"`
}
