package http_test

import (
	"net/http"
	"testing"
)

func TestCategoryRoutes(t *testing.T) {
	e := newEnv(t)
	root := e.post("/categories", `{"name":"Home"}`)
	e.wantStatus(root, http.StatusCreated)
	rootID := root.json(t)["categoryId"].(string)
	child := e.post("/categories", `{"parentId":"`+rootID+`","name":"Bottles"}`)
	e.wantStatus(child, http.StatusCreated)
	childID := child.json(t)["categoryId"].(string)

	r := e.get("/categories")
	e.wantStatus(r, http.StatusOK)
	if cats := r.json(t)["categories"].([]any); len(cats) != 2 {
		t.Fatalf("categories = %v", cats)
	}
	r = e.post("/categories/"+childID+":rename", `{"name":"Flasks"}`)
	e.wantStatus(r, http.StatusOK)
	if got := r.json(t)["name"]; got != "Flasks" {
		t.Fatalf("name = %v", got)
	}
	r = e.post("/categories/"+childID+":move", `{"parentId":null}`)
	e.wantStatus(r, http.StatusOK)
	if _, has := r.json(t)["parentId"]; has {
		t.Fatalf("root category carries parentId: %s", r.Body)
	}
	r = e.post("/categories:publish", "")
	e.wantStatus(r, http.StatusOK)
	if got := r.json(t)["version"]; got == nil {
		t.Fatalf("no version: %s", r.Body)
	}
}

func TestCategoryErrors(t *testing.T) {
	e := newEnv(t)
	e.wantProblem(e.post("/categories", `{"parentId":"ghost","name":"X"}`), http.StatusUnprocessableEntity, "unknown-category")
	e.wantProblem(e.post("/categories", `{"name":""}`), http.StatusUnprocessableEntity, "validation-failed")
	e.wantProblem(e.post("/categories/ghost:rename", `{"name":"X"}`), http.StatusNotFound, "not-found")
	e.wantProblem(e.post("/categories/ghost:move", `{}`), http.StatusNotFound, "not-found")
	e.wantStatus(e.post("/categories", `{"name":"Home"}`), http.StatusCreated)
	e.wantProblem(e.post("/categories", `{"name":"Home"}`), http.StatusUnprocessableEntity, "duplicate-sibling-name")
	e.wantProblem(e.post("/categories/c1:rename", `{"name":1}`), http.StatusBadRequest, "malformed-request")
}

func TestCategoryCycleOverHTTP(t *testing.T) {
	e := newEnv(t)
	a := e.post("/categories", `{"name":"A"}`).json(t)["categoryId"].(string)
	b := e.post("/categories", `{"parentId":"`+a+`","name":"B"}`).json(t)["categoryId"].(string)
	e.wantProblem(e.post("/categories/"+a+":move", `{"parentId":"`+b+`"}`), http.StatusUnprocessableEntity, "category-cycle")
}
