package oasome_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/0xfurai/oasome"
)

type CreateUserReq struct {
	Name  string `json:"name" doc:"Display name" example:"Ada"`
	Email string `json:"email"`
	Age   *int   `json:"age,omitempty"`
}

type User struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

type ErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Other struct {
	Reason string `json:"reason"`
}

func noop(http.ResponseWriter, *http.Request) {}

// spec returns the decoded document of doc.
func spec(t *testing.T, doc *oasome.Doc) map[string]any {
	t.Helper()
	b, err := doc.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("spec is not valid JSON: %v", err)
	}
	return m
}

// dig follows keys through nested maps and returns nil if a key is missing.
func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func ref(name string) string { return "#/components/schemas/" + name }

// mustPanic checks that f panics with an "oasome: " message containing want.
func mustPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Errorf("want panic containing %q, got none", want)
			return
		}
		msg := fmt.Sprint(r)
		if !strings.HasPrefix(msg, "oasome: ") || !strings.Contains(msg, want) {
			t.Errorf("panic = %q, want prefix %q and text %q", msg, "oasome: ", want)
		}
	}()
	f()
}

// eq reports a difference between got and want.
func eq(t *testing.T, what string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// keys returns the sorted keys of a decoded JSON object.
func keys(v any) []string {
	m, _ := v.(map[string]any)
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
