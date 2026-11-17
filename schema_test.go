package oasome

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

type baseT struct {
	ID string `json:"id"`
}

type untaggedID2 struct {
	ID bool
}

type untaggedID struct {
	ID string
}

type taggedID struct {
	ID int `json:"ID"`
}

type inner struct {
	X int `json:"x"`
}

type statusT string

type nodeT struct {
	Next *nodeT  `json:"next,omitempty"`
	Kids []nodeT `json:"kids"`
}

type marshalerVal struct{}

func (marshalerVal) MarshalJSON() ([]byte, error) { return []byte(`1`), nil }

type marshalerPtr struct{}

func (*marshalerPtr) MarshalJSON() ([]byte, error) { return []byte(`1`), nil }

type textVal struct{}

func (textVal) MarshalText() ([]byte, error) { return []byte(`x`), nil }

type textPtr struct{}

func (*textPtr) MarshalText() ([]byte, error) { return []byte(`x`), nil }

type overrideVal struct{ Hidden int }

func (overrideVal) OASSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^a$"}
}

type overridePtr struct{ Hidden int }

func (*overridePtr) OASSchema() map[string]any {
	return map[string]any{"type": "integer"}
}

type overrideBoth struct{}

func (overrideBoth) MarshalJSON() ([]byte, error) { return []byte(`1`), nil }
func (overrideBoth) OASSchema() map[string]any    { return map[string]any{"type": "boolean"} }

type withMarshalerField struct {
	M  marshalerVal `json:"m"`
	T  textVal      `json:"t"`
	PT *textPtr     `json:"pt"`
}

type tagged struct {
	Count int     `json:"count" doc:"How many" example:"5"`
	Name  string  `json:"name" example:"abc"`
	Ok    bool    `json:"ok" example:"true"`
	Ratio float64 `json:"ratio" example:"0.5"`
	Bad   int     `json:"bad" example:"nope"`
	Q     int     `json:"q,string" example:"7"`
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// render reflects typ and returns the inline schema and the components.
func render(t *testing.T, typ reflect.Type) (string, map[string]string) {
	t.Helper()
	g := newGen()
	s := g.schemaOf(typ)
	g.assignNames()
	comps := map[string]string{}
	for name, c := range g.components() {
		comps[name] = jsonOf(t, c)
	}
	return jsonOf(t, s), comps
}

func typeOfValue[T any]() reflect.Type { return reflect.TypeFor[T]() }

func TestSchemaReflection(t *testing.T) {
	tests := []struct {
		name  string
		typ   reflect.Type
		want  string
		comps map[string]string
	}{
		{name: "string", typ: typeOfValue[string](), want: `{"type":"string"}`},
		{name: "bool", typ: typeOfValue[bool](), want: `{"type":"boolean"}`},
		{name: "int", typ: typeOfValue[int](), want: `{"format":"int64","type":"integer"}`},
		{name: "int32", typ: typeOfValue[int32](), want: `{"format":"int32","type":"integer"}`},
		{name: "int64", typ: typeOfValue[int64](), want: `{"format":"int64","type":"integer"}`},
		{name: "uint8", typ: typeOfValue[uint8](), want: `{"format":"int32","minimum":0,"type":"integer"}`},
		{name: "uint64", typ: typeOfValue[uint64](), want: `{"minimum":0,"type":"integer"}`},
		{name: "float32", typ: typeOfValue[float32](), want: `{"format":"float","type":"number"}`},
		{name: "float64", typ: typeOfValue[float64](), want: `{"format":"double","type":"number"}`},
		{name: "json.Number", typ: typeOfValue[json.Number](), want: `{"type":"number"}`},
		{name: "time.Time", typ: typeOfValue[time.Time](), want: `{"format":"date-time","type":"string"}`},
		{name: "pointer to time", typ: typeOfValue[*time.Time](), want: `{"format":"date-time","type":["string","null"]}`},
		{name: "pointer to string", typ: typeOfValue[*string](), want: `{"type":["string","null"]}`},
		{name: "pointer to pointer", typ: typeOfValue[**int](), want: `{"format":"int64","type":["integer","null"]}`},
		{name: "slice", typ: typeOfValue[[]string](), want: `{"items":{"type":"string"},"type":"array"}`},
		{name: "slice is not nullable", typ: typeOfValue[[]*int](),
			want: `{"items":{"format":"int64","type":["integer","null"]},"type":"array"}`},
		{name: "bytes", typ: typeOfValue[[]byte](), want: `{"contentEncoding":"base64","type":"string"}`},
		{name: "array", typ: typeOfValue[[3]int](),
			want: `{"items":{"format":"int64","type":"integer"},"maxItems":3,"minItems":3,"type":"array"}`},
		{name: "map", typ: typeOfValue[map[string]int](),
			want: `{"additionalProperties":{"format":"int64","type":"integer"},"type":"object"}`},
		{name: "map int key", typ: typeOfValue[map[int]string](),
			want: `{"additionalProperties":{"type":"string"},"type":"object"}`},
		{name: "map of any", typ: typeOfValue[map[string]any](),
			want: `{"additionalProperties":{},"type":"object"}`},
		{name: "any", typ: typeOfValue[any](), want: `{}`},
		{name: "raw message", typ: typeOfValue[json.RawMessage](), want: `{}`},
		{name: "named string inlined", typ: typeOfValue[statusT](), want: `{"type":"string"}`},
		{name: "marshaler value", typ: typeOfValue[marshalerVal](), want: `{}`},
		{name: "marshaler pointer receiver", typ: typeOfValue[marshalerPtr](), want: `{}`},
		{name: "text marshaler value", typ: typeOfValue[textVal](), want: `{"type":"string"}`},
		{name: "text marshaler pointer receiver", typ: typeOfValue[textPtr](), want: `{"type":"string"}`},
		{name: "override value receiver", typ: typeOfValue[overrideVal](), want: `{"pattern":"^a$","type":"string"}`},
		{name: "override pointer receiver", typ: typeOfValue[overridePtr](), want: `{"type":"integer"}`},
		{name: "override beats marshaler", typ: typeOfValue[overrideBoth](), want: `{"type":"boolean"}`},
		{name: "override under pointer", typ: typeOfValue[*overridePtr](), want: `{"type":["integer","null"]}`},
		{name: "map key text marshaler", typ: typeOfValue[map[textVal]int](),
			want: `{"additionalProperties":{"format":"int64","type":"integer"},"type":"object"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, comps := render(t, tt.typ)
			if got != tt.want {
				t.Errorf("schema = %s\nwant     %s", got, tt.want)
			}
			if len(comps) != len(tt.comps) {
				t.Errorf("components = %v, want %v", comps, tt.comps)
			}
		})
	}
}

func TestSchemaStructs(t *testing.T) {
	type pair = map[string]string
	tests := []struct {
		name  string
		typ   reflect.Type
		want  string
		comps pair
	}{
		{
			name: "json tags omitempty omitzero dash string unexported",
			typ: reflect.TypeOf(struct {
				A string  `json:"a"`
				B int     `json:"b,omitempty"`
				C bool    `json:"-"`
				D float64 `json:"d,omitzero"`
				e int
				F int `json:",string"`
			}{}),
			want: `{"properties":{"F":{"type":"string"},"a":{"type":"string"},` +
				`"b":{"format":"int64","type":"integer"},"d":{"format":"double","type":"number"}},` +
				`"required":["F","a"],"type":"object"}`,
		},
		{
			name: "dash with comma is a name",
			typ: reflect.TypeOf(struct {
				A int `json:"-,"`
			}{}),
			want: `{"properties":{"-":{"format":"int64","type":"integer"}},"required":["-"],"type":"object"}`,
		},
		{
			name: "pointer with string option",
			typ: reflect.TypeOf(struct {
				A *int `json:"a,string"`
			}{}),
			want: `{"properties":{"a":{"type":["string","null"]}},"required":["a"],"type":"object"}`,
		},
		{
			name: "string option ignored for structs",
			typ: reflect.TypeOf(struct {
				A []int `json:"a,string"`
			}{}),
			want: `{"properties":{"a":{"items":{"format":"int64","type":"integer"},"type":"array"}},` +
				`"required":["a"],"type":"object"}`,
		},
		{
			name: "empty struct",
			typ:  reflect.TypeOf(struct{}{}),
			want: `{"type":"object"}`,
		},
		{
			name: "embedded struct flattened",
			typ: reflect.TypeOf(struct {
				baseT
				Name string `json:"name"`
			}{}),
			want: `{"properties":{"id":{"type":"string"},"name":{"type":"string"}},"required":["id","name"],"type":"object"}`,
		},
		{
			name: "embedded pointer flattened",
			typ: reflect.TypeOf(struct {
				*baseT
				Name string `json:"name"`
			}{}),
			want: `{"properties":{"id":{"type":"string"},"name":{"type":"string"}},"required":["id","name"],"type":"object"}`,
		},
		{
			name: "embedded unexported struct promotes exported fields",
			typ: reflect.TypeOf(struct {
				inner
			}{}),
			want: `{"properties":{"x":{"format":"int64","type":"integer"}},"required":["x"],"type":"object"}`,
		},
		{
			name: "tagged embedded struct not flattened",
			typ: reflect.TypeOf(struct {
				baseT `json:"base"`
			}{}),
			want:  `{"properties":{"base":{"$ref":"#/components/schemas/baseT"}},"required":["base"],"type":"object"}`,
			comps: pair{"baseT": `{"properties":{"id":{"type":"string"}},"required":["id"],"type":"object"}`},
		},
		{
			name: "shallower field wins over embedded",
			typ: reflect.TypeOf(struct {
				untaggedID
				ID int
			}{}),
			want: `{"properties":{"ID":{"format":"int64","type":"integer"}},"required":["ID"],"type":"object"}`,
		},
		{
			name: "same depth conflict drops both",
			typ: reflect.TypeOf(struct {
				untaggedID
				untaggedID2
				Keep string `json:"keep"`
			}{}),
			want: `{"properties":{"keep":{"type":"string"}},"required":["keep"],"type":"object"}`,
		},
		{
			name: "tag breaks same depth conflict",
			typ: reflect.TypeOf(struct {
				untaggedID
				taggedID
			}{}),
			want: `{"properties":{"ID":{"format":"int64","type":"integer"}},"required":["ID"],"type":"object"}`,
		},
		{
			name: "named struct becomes component",
			typ:  typeOfValue[baseT](),
			want: `{"$ref":"#/components/schemas/baseT"}`,
			comps: pair{
				"baseT": `{"properties":{"id":{"type":"string"}},"required":["id"],"type":"object"}`,
			},
		},
		{
			name: "pointer to named struct",
			typ:  typeOfValue[*baseT](),
			want: `{"anyOf":[{"$ref":"#/components/schemas/baseT"},{"type":"null"}]}`,
			comps: pair{
				"baseT": `{"properties":{"id":{"type":"string"}},"required":["id"],"type":"object"}`,
			},
		},
		{
			name: "pointer to anonymous struct",
			typ: reflect.TypeOf(&struct {
				A int `json:"a"`
			}{}),
			want: `{"properties":{"a":{"format":"int64","type":"integer"}},"required":["a"],"type":["object","null"]}`,
		},
		{
			name: "recursive",
			typ:  typeOfValue[nodeT](),
			want: `{"$ref":"#/components/schemas/nodeT"}`,
			comps: pair{
				"nodeT": `{"properties":{"kids":{"items":{"$ref":"#/components/schemas/nodeT"},"type":"array"},` +
					`"next":{"anyOf":[{"$ref":"#/components/schemas/nodeT"},{"type":"null"}]}},` +
					`"required":["kids"],"type":"object"}`,
			},
		},
		{
			name: "marshaler fields",
			typ:  typeOfValue[withMarshalerField](),
			want: `{"$ref":"#/components/schemas/withMarshalerField"}`,
			comps: pair{
				"withMarshalerField": `{"properties":{"m":{},"pt":{"type":["string","null"]},"t":{"type":"string"}},` +
					`"required":["m","pt","t"],"type":"object"}`,
			},
		},
		{
			name: "doc and example tags",
			typ:  typeOfValue[tagged](),
			want: `{"$ref":"#/components/schemas/tagged"}`,
			comps: pair{
				"tagged": `{"properties":{` +
					`"bad":{"example":"nope","format":"int64","type":"integer"},` +
					`"count":{"description":"How many","example":5,"format":"int64","type":"integer"},` +
					`"name":{"example":"abc","type":"string"},` +
					`"ok":{"example":true,"type":"boolean"},` +
					`"q":{"example":"7","type":"string"},` +
					`"ratio":{"example":0.5,"format":"double","type":"number"}},` +
					`"required":["bad","count","name","ok","q","ratio"],"type":"object"}`,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, comps := render(t, tt.typ)
			if got != tt.want {
				t.Errorf("schema = %s\nwant     %s", got, tt.want)
			}
			if !reflect.DeepEqual(comps, map[string]string(tt.comps)) && !(len(comps) == 0 && len(tt.comps) == 0) {
				t.Errorf("components = %v\nwant         %v", comps, tt.comps)
			}
		})
	}
}

func TestSchemaUnsupportedTypesPanic(t *testing.T) {
	tests := []struct {
		name string
		typ  reflect.Type
		want string
	}{
		{"chan", typeOfValue[chan int](), "unsupported kind chan"},
		{"func", typeOfValue[func()](), "unsupported kind func"},
		{"complex", typeOfValue[complex128](), "unsupported kind complex128"},
		{"float map key", typeOfValue[map[float64]int](), "unsupported map key"},
		{"nested chan", typeOfValue[struct{ C chan int }](), "unsupported kind chan"},
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

func TestOverrideDoesNotMutateSource(t *testing.T) {
	// A pointer wrapper must not modify the map the type returned.
	first, _ := render(t, typeOfValue[*overrideVal]())
	second, _ := render(t, typeOfValue[overrideVal]())
	if !strings.Contains(first, `["string","null"]`) || strings.Contains(second, "null") {
		t.Errorf("override schema leaked a change: %s / %s", first, second)
	}
}
