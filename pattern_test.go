package oasome

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePattern(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		method    string
		host      string
		path      string
		wildcards []wildcard
	}{
		{name: "plain", pattern: "GET /users", method: "get", path: "/users"},
		{name: "wildcard", pattern: "POST /users/{id}", method: "post", path: "/users/{id}",
			wildcards: []wildcard{{name: "id"}}},
		{name: "two wildcards", pattern: "PUT /a/{x}/b/{y}", method: "put", path: "/a/{x}/b/{y}",
			wildcards: []wildcard{{name: "x"}, {name: "y"}}},
		{name: "tail wildcard", pattern: "GET /files/{path...}", method: "get", path: "/files/{path}",
			wildcards: []wildcard{{name: "path", tail: true}}},
		{name: "end anchor", pattern: "GET /items/{$}", method: "get", path: "/items/"},
		{name: "trailing slash", pattern: "GET /items/", method: "get", path: "/items/"},
		{name: "root anchor", pattern: "GET /{$}", method: "get", path: "/"},
		{name: "root", pattern: "GET /", method: "get", path: "/"},
		{name: "host", pattern: "GET example.com/path", method: "get", host: "example.com", path: "/path"},
		{name: "host and wildcard", pattern: "DELETE api.example.com/x/{id}", method: "delete",
			host: "api.example.com", path: "/x/{id}", wildcards: []wildcard{{name: "id"}}},
		{name: "extra spaces", pattern: "PATCH   /x", method: "patch", path: "/x"},
		{name: "tab", pattern: "HEAD\t/x", method: "head", path: "/x"},
		{name: "options", pattern: "OPTIONS /x", method: "options", path: "/x"},
		{name: "trace", pattern: "TRACE /x", method: "trace", path: "/x"},
		{name: "underscore name", pattern: "GET /x/{user_id}", method: "get", path: "/x/{user_id}",
			wildcards: []wildcard{{name: "user_id"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePattern(tt.pattern)
			if err != nil {
				t.Fatalf("parsePattern(%q): %v", tt.pattern, err)
			}
			want := parsedPattern{method: tt.method, host: tt.host, path: tt.path, wildcards: tt.wildcards}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("parsePattern(%q) = %+v, want %+v", tt.pattern, got, want)
			}
		})
	}
}

func TestParsePatternErrors(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    string
	}{
		{"no method", "/users", "no method"},
		{"lowercase method", "get /users", "unsupported method"},
		{"unsupported method", "CONNECT /users", "unsupported method"},
		{"no path", "GET users", "no path"},
		{"end anchor in middle", "GET /a/{$}/b", "{$} must be at the end"},
		{"tail in middle", "GET /a/{x...}/b", "must be at the end"},
		{"duplicate wildcard", "GET /a/{x}/{x}", "more than once"},
		{"partial segment", "GET /a/b{x}", "whole path segment"},
		{"two in segment", "GET /a/{x}{y}", "invalid wildcard name"},
		{"bad name", "GET /a/{1x}", "invalid wildcard name"},
		{"empty name", "GET /a/{}", "invalid wildcard name"},
		{"unclosed", "GET /a/{x", "whole path segment"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePattern(tt.pattern)
			if err == nil {
				t.Fatalf("parsePattern(%q): want error containing %q, got nil", tt.pattern, tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("parsePattern(%q) error = %q, want it to contain %q", tt.pattern, err, tt.want)
			}
		})
	}
}

func TestJoinPrefix(t *testing.T) {
	tests := []struct{ a, b, want string }{
		{"", "", ""},
		{"/", "", ""},
		{"/api", "", "/api"},
		{"api", "", "/api"},
		{"/api/", "", "/api"},
		{"/api", "/v1", "/api/v1"},
		{"/api/", "v1/", "/api/v1"},
		{"", "/v1", "/v1"},
	}
	for _, tt := range tests {
		if got := joinPrefix(tt.a, tt.b); got != tt.want {
			t.Errorf("joinPrefix(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
		}
	}
}
