// Command example documents two routes of a plain http.ServeMux with oasome:
// one with the minimum of options, one with most of them.
//
//	go run ./example        # serve on :8080, spec at /openapi.json
//	go generate ./example   # write openapi.json again
package main

//go:generate go run . openapi.json

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/0xfurai/oasome"
)

type CreateUserReq struct {
	Name string `json:"name" doc:"Display name" example:"Ada"`
}

type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role Role   `json:"role"`
}

// OASExample gives the User schema a full example.
func (User) OASExample() User { return User{ID: "u_42", Name: "Ada", Role: "admin"} }

type Role string

// OASEnum lists the allowed values of Role.
func (Role) OASEnum() []Role { return []Role{"admin", "member"} }

type Error struct {
	Message string `json:"message"`
}

// GetUserParams gives types and docs to the parameters of GET /users/{id}.
type GetUserParams struct {
	ID      string `path:"id" doc:"User ID" example:"u_42"`
	Fields  string `query:"fields" doc:"Comma-separated fields to return" example:"id,name"`
	TraceID string `header:"X-Trace-ID" doc:"Request trace ID"`
}

func getUser(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(User{ID: r.PathValue("id"), Name: "Ada", Role: "member"})
}

func createUser(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(User{ID: r.PathValue("id"), Name: "Ada", Role: "member"})
}

func main() {
	mux := http.NewServeMux()
	doc := oasome.New("My API", "1.0.0",
		// Extend sets any field that has no option of its own.
		oasome.Extend(func(spec map[string]any) {
			spec["info"].(map[string]any)["license"] = map[string]any{"name": "MIT"}
		}),
	)
	mux.Handle(doc.Route("POST /users/{id}", createUser,
		oasome.Body[CreateUserReq](),
		oasome.Returns[User](201),
	))
	mux.Handle(doc.Route("GET /users/{id}", getUser,
		oasome.Summary("Get a user"),
		oasome.Description("Returns one user by ID."),
		oasome.Tags("users"),
		oasome.OperationID("getUser"),
		oasome.Params[GetUserParams](),
		oasome.Returns[User](200),
		oasome.Returns[Error](404),
	))
	mux.Handle("GET /openapi.json", doc)

	if len(os.Args) > 1 { // write the spec to a file and exit
		spec, _ := json.MarshalIndent(doc, "", "  ")
		if err := os.WriteFile(os.Args[1], append(spec, '\n'), 0o644); err != nil {
			log.Fatal(err)
		}
		return
	}
	log.Fatal(http.ListenAndServe(":8080", mux))
}
