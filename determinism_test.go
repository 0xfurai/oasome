package oasome_test

import (
	"fmt"
	"testing"

	"github.com/0xfurai/oasome"
	fxa "github.com/0xfurai/oasome/internal/fixture/a/shared"
	fxapi "github.com/0xfurai/oasome/internal/fixture/api"
	fxb "github.com/0xfurai/oasome/internal/fixture/b/shared"
	fxmodels "github.com/0xfurai/oasome/internal/fixture/models"
)

type registration func(*oasome.Doc)

func registrations() []registration {
	type Q struct {
		Limit int    `query:"limit"`
		Sort  string `query:"sort,required"`
		Trace string `header:"X-Trace"`
	}
	return []registration{
		func(d *oasome.Doc) { d.Route("GET /models", noop, oasome.Returns[fxmodels.User](200)) },
		func(d *oasome.Doc) { d.Route("GET /api", noop, oasome.Returns[fxapi.User](200)) },
		func(d *oasome.Doc) {
			d.Route("GET /pages", noop, oasome.Returns[fxmodels.Page[fxmodels.User]](200), oasome.Params[Q]())
		},
		func(d *oasome.Doc) { d.Route("GET /pages2", noop, oasome.Returns[fxmodels.Page[fxapi.User]](200)) },
		func(d *oasome.Doc) { d.Route("GET /a", noop, oasome.Returns[fxa.Thing](200), oasome.Tags("b", "a")) },
		func(d *oasome.Doc) { d.Route("GET /b", noop, oasome.Returns[fxb.Thing](200)) },
		func(d *oasome.Doc) {
			d.WithPrefix("/v1").Route("POST /users/{id}", noop, oasome.Body[CreateUserReq](), oasome.Returns[User](201))
		},
		func(d *oasome.Doc) {
			d.WithPrefix("/v1").Route("GET /users/{id}", noop, oasome.Returns[User](200), oasome.Returns[Other](404))
		},
		func(d *oasome.Doc) {
			d.Route("GET /err", noop, oasome.Returns[ErrorBody](400), oasome.Security("k"), oasome.Security("b", "z", "a"))
		},
	}
}

func newDeterminismDoc() *oasome.Doc {
	return oasome.New("t", "1",
		oasome.SecurityScheme("b", oasome.Scheme{Type: "http", Scheme: "bearer"}),
		oasome.SecurityScheme("k", oasome.Scheme{Type: "apiKey", In: "header", Name: "K"}),
		oasome.Defaults(oasome.Returns[ErrorBody](500), oasome.Returns[Other](401), oasome.Security("b")),
	)
}

// orders returns the identity order, its reverse and every rotation.
func orders(n int) [][]int {
	out := [][]int{ordering(n, func(i int) int { return n - 1 - i })}
	for shift := range n {
		out = append(out, ordering(n, func(i int) int { return (i + shift) % n }))
	}
	return out
}

func ordering(n int, f func(int) int) []int {
	o := make([]int, n)
	for i := range n {
		o[i] = f(i)
	}
	return o
}

func buildInOrder(t *testing.T, regs []registration, order []int) string {
	t.Helper()
	d := newDeterminismDoc()
	for _, i := range order {
		regs[i](d)
	}
	b, err := d.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDeterministicOutput(t *testing.T) {
	regs := registrations()
	want := buildInOrder(t, regs, ordering(len(regs), func(i int) int { return i }))
	for _, o := range orders(len(regs)) {
		eq(t, fmt.Sprintf("output for order %v", o), buildInOrder(t, regs, o) == want, true)
	}
}
