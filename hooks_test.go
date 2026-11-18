package oasome

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type colorT string

func (colorT) OASEnum() []colorT  { return []colorT{"red", "green"} }
func (colorT) OASExample() colorT { return "red" }

type levelT int

func (*levelT) OASEnum() []any { return []any{1, 2} }

type petT struct {
	Name  string  `json:"name"`
	Color *colorT `json:"color,omitempty"`
}

func (petT) OASExample() any { return petT{Name: "Rex"} }

type nilExampleT struct {
	A int `json:"a"`
}

func (nilExampleT) OASExample() any { return nil }

type emptyEnumT string

func (emptyEnumT) OASEnum() []string { return nil }

type badArgsT string

func (badArgsT) OASExample(int) any { return nil }

type badEnumT string

func (badEnumT) OASEnum() string { return "" }

type badValueT string

func (badValueT) OASExample() any { return make(chan int) }

const colorSchema = `{"enum":["red","green"],"examples":["red"],"type":"string"}`

func TestSchemaHooks(t *testing.T) {
	nullableColor := `{"anyOf":[` + colorSchema + `,{"type":"null"}]}`
	tests := []struct {
		name  string
		typ   reflect.Type
		want  string
		comps map[string]string
	}{
		{name: "enum and example on a named string", typ: typeOfValue[colorT](), want: colorSchema},
		{name: "pointer to an enum allows null", typ: typeOfValue[*colorT](), want: nullableColor},
		{name: "pointer receiver and untyped values", typ: typeOfValue[levelT](),
			want: `{"enum":[1,2],"format":"int64","type":"integer"}`},
		{name: "empty enum is ignored", typ: typeOfValue[emptyEnumT](), want: `{"type":"string"}`},
		{name: "example goes on the component", typ: typeOfValue[petT](),
			want: `{"$ref":"#/components/schemas/petT"}`,
			comps: map[string]string{"petT": `{"examples":[{"name":"Rex"}],` +
				`"properties":{"color":` + nullableColor + `,"name":{"type":"string"}},` +
				`"required":["name"],"type":"object"}`}},
		{name: "nil example is ignored", typ: typeOfValue[nilExampleT](),
			want: `{"$ref":"#/components/schemas/nilExampleT"}`,
			comps: map[string]string{"nilExampleT": `{"properties":{"a":{"format":"int64","type":"integer"}},` +
				`"required":["a"],"type":"object"}`}},
		{name: "promoted hook on an anonymous struct is not used",
			typ: reflect.TypeOf(struct {
				nilExampleT
				colorT
			}{}),
			want: `{"properties":{"a":{"format":"int64","type":"integer"}},"required":["a"],"type":"object"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, comps := render(t, tt.typ)
			if got != tt.want {
				t.Errorf("schema = %s\nwant     %s", got, tt.want)
			}
			if len(tt.comps) > 0 && !reflect.DeepEqual(comps, tt.comps) {
				t.Errorf("components = %v\nwant         %v", comps, tt.comps)
			}
		})
	}
}

func TestSchemaHookPanics(t *testing.T) {
	tests := []struct {
		name string
		typ  reflect.Type
		want string
	}{
		{"hook with arguments", typeOfValue[badArgsT](), "OASExample must take no arguments"},
		{"enum that is not a slice", typeOfValue[badEnumT](), "OASEnum must return a slice"},
		{"value that cannot be encoded", typeOfValue[badValueT](), "badValueT.OASExample: json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				msg := fmt.Sprint(recover())
				if !strings.HasPrefix(msg, "oasome: ") || !strings.Contains(msg, tt.want) {
					t.Errorf("panic = %q, want prefix %q and %q", msg, "oasome: ", tt.want)
				}
			}()
			newGen().schemaOf(tt.typ)
		})
	}
}

func TestApplyExtendRejectsBadJSON(t *testing.T) {
	if _, err := applyExtend([]byte("{"), nil); err == nil {
		t.Error("want an error for a document that is not valid JSON")
	}
}

func TestTypePairOrderIsFixed(t *testing.T) {
	a, b := typeOfValue[baseT](), typeOfValue[nodeT]()
	if typePair(a, b) != typePair(b, a) {
		t.Errorf("typePair depends on the argument order: %q and %q", typePair(a, b), typePair(b, a))
	}
}
