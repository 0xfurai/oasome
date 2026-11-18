package oasome_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xfurai/oasome"
	fxapi "github.com/0xfurai/oasome/internal/fixture/api"
	fxmodels "github.com/0xfurai/oasome/internal/fixture/models"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden.json")

// Types of the example library API used for the golden file.

type Timestamps struct {
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt"`
}

type Book struct {
	Timestamps
	ID       int64             `json:"id" doc:"Book identifier" example:"42"`
	Title    string            `json:"title" example:"Dune"`
	ISBN     *string           `json:"isbn,omitempty"`
	Pages    int               `json:"pages,omitempty"`
	Price    float64           `json:"price,string"`
	Cover    []byte            `json:"cover,omitempty" doc:"Cover image, base64"`
	Labels   map[string]string `json:"labels,omitempty"`
	Author   *Author           `json:"author,omitempty"`
	Metadata json.RawMessage   `json:"metadata,omitempty"`
	Internal string            `json:"-"`
	secret   string
}

type Author struct {
	ID    int64     `json:"id"`
	Name  string    `json:"name"`
	Books []Book    `json:"books,omitempty"`
	Tags  [2]string `json:"tags"`
}

type Category struct {
	Name     string     `json:"name"`
	Parent   *Category  `json:"parent"`
	Children []Category `json:"children"`
}

// Genre lists its values through the OASEnum hook.
type Genre string

func (Genre) OASEnum() []Genre { return []Genre{"fiction", "science", "history"} }

type CreateBook struct {
	Title    string `json:"title" doc:"Title of the book"`
	AuthorID int64  `json:"authorId"`
	ISBN     string `json:"isbn,omitempty" pattern:"^[0-9-]{10,17}$"`
	Genre    *Genre `json:"genre,omitempty"`
}

// OASExample gives the CreateBook component a full example.
func (CreateBook) OASExample() CreateBook {
	g := Genre("fiction")
	return CreateBook{Title: "Dune", AuthorID: 7, Genre: &g}
}

type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"nextCursor"`
	Extra      any     `json:"extra,omitempty"`
}

type Problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type ListBooksParams struct {
	Limit     int    `query:"limit" doc:"Page size" example:"20"`
	Cursor    string `query:"cursor"`
	Genre     Genre  `query:"genre,required"`
	RequestID string `header:"X-Request-ID" doc:"Correlation id"`
}

type BookPath struct {
	ID int64 `path:"id" doc:"Book identifier" example:"42"`
}

// Money documents itself through the OASSchema hook.
type Money struct{ cents int64 }

func (Money) OASSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": `^\d+\.\d{2}$`, "example": "12.50"}
}

type Invoice struct {
	Total Money   `json:"total"`
	Paid  *Money  `json:"paid"`
	Notes *string `json:"notes,omitempty"`
}

// BookGone is one of the bodies of a 404 for a book cover.
type BookGone struct {
	Since time.Time `json:"since"`
}

// NotFoundOrGone documents two possible bodies with the Ref placeholder.
type NotFoundOrGone struct{}

func (NotFoundOrGone) OASSchema() map[string]any {
	return map[string]any{"anyOf": []any{oasome.Ref[Problem](), oasome.Ref[BookGone]()}}
}

// patternTag is a FieldSchema function for a project-specific tag.
func patternTag(f *oasome.SchemaField) {
	if v, ok := f.Field.Tag.Lookup("pattern"); ok {
		f.Schema["pattern"] = v
	}
}

// libraryDoc builds the documented API shared by the golden and CI tests.
func libraryDoc() *oasome.Doc {
	doc := oasome.New("Library API", "2.3.1",
		oasome.DocDescription("A small library catalogue used to exercise every feature."),
		oasome.Server("https://api.example.com", "Production"),
		oasome.Server("http://localhost:8080", "Local development"),
		oasome.SecurityScheme("bearerAuth", oasome.Scheme{Type: "http", Scheme: "bearer", BearerFormat: "JWT"}),
		oasome.SecurityScheme("apiKey", oasome.Scheme{Type: "apiKey", In: "header", Name: "X-API-Key", Description: "Service key"}),
		oasome.FieldSchema(patternTag),
		oasome.Defaults(
			oasome.Returns[Problem](401),
			oasome.Returns[Problem](500),
			oasome.Security("bearerAuth"),
		),
	)

	doc.Route("GET /healthz", noop,
		oasome.Summary("Liveness probe"), oasome.Tags("ops"),
		oasome.ReturnsEmpty(204), oasome.NoSecurity())

	doc.Route("GET /books", noop,
		oasome.Summary("List books"), oasome.OperationID("listBooks"), oasome.Tags("books"),
		oasome.Params[ListBooksParams](),
		oasome.Returns[Page[Book]](200))

	doc.Route("POST /books", noop,
		oasome.Summary("Create a book"), oasome.OperationID("createBook"), oasome.Tags("books"),
		oasome.Body[CreateBook](),
		oasome.Returns[Book](201), oasome.Returns[Problem](422))

	doc.Route("GET /books/{id}", noop,
		oasome.Summary("Get a book"), oasome.OperationID("getBook"), oasome.Tags("books"),
		oasome.Params[BookPath](),
		oasome.Returns[Book](200), oasome.Returns[Problem](404))

	doc.Route("GET /books/{id}/cover", noop,
		oasome.Summary("Download the cover"), oasome.OperationID("getBookCover"), oasome.Tags("books"),
		oasome.Params[BookPath](),
		oasome.ReturnsContent[string](200, "image/svg+xml"),
		oasome.ReturnsContent[any](200, "image/png"),
		oasome.ResponseHeader[string](200, "Content-Disposition", "Suggested file name"),
		oasome.Returns[NotFoundOrGone](404),
		oasome.Returns[Problem](429),
		oasome.ResponseHeader[int](429, "Retry-After", "Seconds to wait before the next request"))

	doc.Route("DELETE /books/{id}", noop,
		oasome.Summary("Delete a book"), oasome.OperationID("deleteBook"), oasome.Tags("books"),
		oasome.Description("Removes the book permanently."), oasome.Deprecated(),
		oasome.ReturnsEmpty(204))

	doc.Route("GET /files/{path...}", noop,
		oasome.Summary("Download a file"), oasome.Tags("files"),
		oasome.Returns[[]byte](200))

	doc.Route("GET /categories/{$}", noop,
		oasome.Summary("Category tree"), oasome.Tags("books"),
		oasome.Returns[Category](200))

	doc.Route("GET catalog.example.com/invoices/{id}", noop,
		oasome.Summary("Get an invoice"), oasome.Tags("billing"),
		oasome.Returns[Invoice](200))

	doc.Route("GET /reviewers", noop,
		oasome.Summary("List reviewers"), oasome.Tags("books"),
		oasome.Returns[[]fxmodels.User](200))
	doc.Route("GET /accounts", noop,
		oasome.Summary("List accounts"), oasome.Tags("books"),
		oasome.Returns[[]fxapi.User](200))

	// A versioned sub-API, as mounted with http.StripPrefix("/v2", sub).
	v2 := doc.WithPrefix("/v2")
	v2.Route("GET /authors", noop,
		oasome.Summary("List authors"), oasome.Tags("authors"),
		oasome.Security("apiKey"), oasome.Security("bearerAuth", "authors:read"),
		oasome.Returns[Page[Author]](200))
	v2.Route("GET /authors/{id}", noop,
		oasome.Summary("Get an author"), oasome.Tags("authors"),
		oasome.Params[BookPath](),
		oasome.Returns[Author](200), oasome.Returns[Problem](404))
	return doc
}

// minimalDoc has no defaults and a route without responses, to show the
// placeholder "default" response.
func minimalDoc() *oasome.Doc {
	doc := oasome.New("Minimal API", "0.1.0", oasome.Server("https://events.example.org", ""))
	doc.Route("POST /events", noop, oasome.Summary("Receive an event"), oasome.Body[Problem](), oasome.NoSecurity())
	return doc
}

func TestGolden(t *testing.T) {
	checkGolden(t, "library", libraryDoc())
	checkGolden(t, "minimal", minimalDoc())
}

func checkGolden(t *testing.T, name string, doc *oasome.Doc) {
	t.Helper()
	got, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", name+".golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test -run TestGolden -update)", err)
	}
	if string(got) != string(want) {
		t.Errorf("output differs from %s; if the change is intended run: go test -run TestGolden -update\n--- got ---\n%s", path, got)
	}
}
