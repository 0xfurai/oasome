package oasome

import (
	"reflect"
	"testing"

	apiv2 "github.com/0xfurai/oasome/internal/fixture/api/v2"
	"github.com/0xfurai/oasome/internal/fixture/models"
)

type dupA struct{ baseT }

type dupB struct{ baseT }

type nilOverride struct{}

func (nilOverride) OASSchema() map[string]any { return nil }

type constOverride string

func (constOverride) OASSchema() map[string]any {
	return map[string]any{"const": "a"}
}

type multiOverride string

func (multiOverride) OASSchema() map[string]any {
	return map[string]any{"type": []string{"string", "integer"}}
}

type nullOverride string

func (nullOverride) OASSchema() map[string]any {
	return map[string]any{"type": []string{"string", "null"}}
}

type oddOverride string

func (oddOverride) OASSchema() map[string]any {
	return map[string]any{"type": []any{"string"}}
}

func TestSchemaEdges(t *testing.T) {
	tests := []struct {
		name string
		typ  reflect.Type
		want string
	}{
		{"pointer to any stays unconstrained", typeOfValue[*any](), `{}`},
		{"nil override is unconstrained", typeOfValue[nilOverride](), `{}`},
		{"pointer to override without type", typeOfValue[*constOverride](),
			`{"anyOf":[{"const":"a"},{"type":"null"}]}`},
		{"pointer to override with type list", typeOfValue[*multiOverride](),
			`{"type":["string","integer","null"]}`},
		{"pointer to override that already has null", typeOfValue[*nullOverride](),
			`{"type":["string","null"]}`},
		{"pointer to override with unknown type form", typeOfValue[*oddOverride](),
			`{"type":["string"]}`},
		{"same embedded type twice at one depth drops its fields",
			reflect.TypeOf(struct {
				dupA
				dupB
				Name string `json:"name"`
			}{}),
			`{"properties":{"name":{"type":"string"}},"required":["name"],"type":"object"}`},
		{"embedded type seen again at a deeper level",
			reflect.TypeOf(struct {
				baseT
				dupA
			}{}),
			`{"properties":{"id":{"type":"string"}},"required":["id"],"type":"object"}`},
		{"invalid json tag name uses the field name",
			reflect.TypeOf(struct {
				A string `json:"it's"`
			}{}),
			`{"properties":{"A":{"type":"string"}},"required":["A"],"type":"object"}`},
		{"examples on unsigned and pointer fields",
			reflect.TypeOf(struct {
				N uint `json:"n" example:"7"`
				P *int `json:"p" example:"3"`
			}{}),
			`{"properties":{"n":{"example":7,"minimum":0,"type":"integer"},` +
				`"p":{"example":3,"format":"int64","type":["integer","null"]}},` +
				`"required":["n","p"],"type":"object"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, _ := render(t, tt.typ); got != tt.want {
				t.Errorf("schema = %s\nwant     %s", got, tt.want)
			}
		})
	}
}

func TestRefNameWithoutComponentName(t *testing.T) {
	if _, err := (refName{t: typeOfValue[baseT](), g: newGen()}).MarshalJSON(); err == nil {
		t.Error("want an error for a type that has no component name")
	}
}

func localTypeA() reflect.Type {
	type local struct{ A int }
	return reflect.TypeFor[local]()
}

func localTypeB() reflect.Type {
	type local struct{ B int }
	return reflect.TypeFor[local]()
}

func TestComponentNamesFunctionLocalTypes(t *testing.T) {
	a, b := localTypeA(), localTypeB()
	got := componentNames([]reflect.Type{a, b})
	if got[a] == got[b] {
		t.Errorf("both types are named %q", got[a])
	}
	for _, name := range got {
		if !validKey.MatchString(name) {
			t.Errorf("name %q is not a valid component key", name)
		}
	}
}

func TestComponentNamesSkipMajorVersion(t *testing.T) {
	v2, m := typeOfValue[apiv2.User](), typeOfValue[models.User]()
	got := componentNames([]reflect.Type{v2, m})
	if got[v2] != "api.User" || got[m] != "models.User" {
		t.Errorf("names = %q and %q", got[v2], got[m])
	}
}

func TestLookupRejectsOtherValues(t *testing.T) {
	if _, ok := lookup("not a doc", "GET /x"); ok {
		t.Error("lookup accepted a value that is not a *Doc")
	}
	if _, ok := lookup((*Doc)(nil), "GET /x"); ok {
		t.Error("lookup accepted a nil *Doc")
	}
}
