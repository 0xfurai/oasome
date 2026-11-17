package oasome

import (
	"reflect"
	"regexp"
	"testing"

	ashared "github.com/0xfurai/oasome/internal/fixture/a/shared"
	"github.com/0xfurai/oasome/internal/fixture/api"
	bshared "github.com/0xfurai/oasome/internal/fixture/b/shared"
	"github.com/0xfurai/oasome/internal/fixture/models"
)

var validKey = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func TestComponentNames(t *testing.T) {
	const fx = "github.com_0xfurai_oasome_internal_fixture_"
	tests := []struct {
		name  string
		types []reflect.Type
		want  map[reflect.Type]string
	}{
		{
			name:  "unique bare names",
			types: []reflect.Type{typeOfValue[baseT](), typeOfValue[nodeT]()},
			want:  map[reflect.Type]string{typeOfValue[baseT](): "baseT", typeOfValue[nodeT](): "nodeT"},
		},
		{
			name:  "same bare name uses package prefix",
			types: []reflect.Type{typeOfValue[models.User](), typeOfValue[api.User](), typeOfValue[baseT]()},
			want: map[reflect.Type]string{
				typeOfValue[models.User](): "models.User",
				typeOfValue[api.User]():    "api.User",
				typeOfValue[baseT]():       "baseT",
			},
		},
		{
			name:  "same package name uses full path",
			types: []reflect.Type{typeOfValue[ashared.Thing](), typeOfValue[bshared.Thing]()},
			want: map[reflect.Type]string{
				typeOfValue[ashared.Thing](): fx + "a_shared.Thing",
				typeOfValue[bshared.Thing](): fx + "b_shared.Thing",
			},
		},
		{
			name:  "generic names are folded",
			types: []reflect.Type{typeOfValue[models.Page[models.User]]()},
			want:  map[reflect.Type]string{typeOfValue[models.Page[models.User]](): "Page_User"},
		},
		{
			name: "generic collision",
			types: []reflect.Type{
				typeOfValue[models.Page[models.User]](),
				typeOfValue[models.Page[api.User]](),
			},
			want: map[reflect.Type]string{
				typeOfValue[models.Page[models.User]](): fx + "models.Page_" + fx + "models.User",
				typeOfValue[models.Page[api.User]]():    fx + "models.Page_" + fx + "api.User",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := componentNames(tt.types)
			for typ, want := range tt.want {
				if got[typ] != want {
					t.Errorf("name of %s = %q, want %q", typ, got[typ], want)
				}
				if !validKey.MatchString(got[typ]) {
					t.Errorf("name %q is not a valid component key", got[typ])
				}
			}
		})
	}
}

func TestComponentNamesIgnoreOrder(t *testing.T) {
	types := []reflect.Type{
		typeOfValue[models.User](), typeOfValue[api.User](), typeOfValue[ashared.Thing](),
		typeOfValue[bshared.Thing](), typeOfValue[models.Page[models.User]](),
		typeOfValue[models.Page[api.User]](), typeOfValue[baseT](),
	}
	want := componentNames(types)
	for shift := range types {
		rotated := append(append([]reflect.Type{}, types[shift:]...), types[:shift]...)
		if got := componentNames(rotated); !reflect.DeepEqual(got, want) {
			t.Fatalf("rotation %d changed names:\n got %v\nwant %v", shift, got, want)
		}
	}
	seen := map[string]bool{}
	for typ, name := range want {
		if seen[name] {
			t.Errorf("name %q is used twice (%s)", name, typ)
		}
		seen[name] = true
		if !validKey.MatchString(name) {
			t.Errorf("name %q of %s is not a valid component key", name, typ)
		}
	}
}

func TestSanitizeAndShortName(t *testing.T) {
	if got := sanitize("a/b[c, d]*"); got != "a_b_c_d" {
		t.Errorf("sanitize = %q", got)
	}
	if got := lastIdentifier("example.com/x.User"); got != "User" {
		t.Errorf("lastIdentifier = %q", got)
	}
	if got := pkgName(typeOfValue[models.User]()); got != "models" {
		t.Errorf("pkgName = %q", got)
	}
}
