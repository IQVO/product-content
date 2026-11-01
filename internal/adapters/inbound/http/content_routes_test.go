package http_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/IQVO/product-content/internal/application/usecases"
)

const contentBody = `{"catalogItemId":"item-1","description":"A 2-litre bottle.","bullets":["Double-wall"],` +
	`"attributes":[{"name":"capacity","value":"2","unit":"l"},{"name":"colour","value":"steel"}],` +
	`"media":[{"mediaId":"m1","url":"https://static.example.invalid/a.jpg","altText":"side","role":"PRIMARY"}],` +
	`"primaryCategoryId":"%s"}`

// seed makes item-1 known and returns a category id.
func (e *env) seed() string {
	e.t.Helper()
	err := usecases.Consumer{Deps: e.deps}.ApplyCatalogItemCreated(context.Background(), usecases.Meta{EventID: "ev-1", Type: "t", At: t0}, usecases.CatalogItemCreated{CatalogItemID: "item-1"})
	if err != nil {
		e.t.Fatal(err)
	}
	r := e.post("/categories", `{"name":"Bottles"}`)
	e.wantStatus(r, http.StatusCreated)
	return r.json(e.t)["categoryId"].(string)
}

func (e *env) createBody(cat string) string { return fmt.Sprintf(contentBody, cat) }

func TestContentLifecycleOverHTTP(t *testing.T) {
	e := newEnv(t)
	cat := e.seed()
	r := e.post("/product-contents", e.createBody(cat))
	e.wantStatus(r, http.StatusCreated)
	if got := r.json(t)["state"]; got != "DRAFT" {
		t.Fatalf("state = %v", got)
	}
	e.wantStatus(e.get("/product-contents/item-1"), http.StatusOK)
	r = e.post("/product-contents/item-1/revisions", `{"description":"New text","bullets":[],"attributes":[],"media":[],"primaryCategoryId":"`+cat+`"}`)
	e.wantStatus(r, http.StatusOK)
	r = e.post("/product-contents/item-1/revisions", e.createBody(cat))
	e.wantStatus(r, http.StatusOK)
	r = e.post("/product-contents/item-1/publications", "")
	e.wantStatus(r, http.StatusOK)
	if got := r.json(t)["state"]; got != "PUBLISHED" {
		t.Fatalf("state = %v", got)
	}
	r = e.post("/product-contents/item-1/retirements", "")
	e.wantStatus(r, http.StatusOK)
	e.wantProblem(e.post("/product-contents/item-1/publications", ""), http.StatusConflict, "content-retired")
}

func TestDiscardOverHTTP(t *testing.T) {
	e := newEnv(t)
	cat := e.seed()
	e.wantStatus(e.post("/product-contents", e.createBody(cat)), http.StatusCreated)
	r := e.del("/product-contents/item-1/draft")
	e.wantStatus(r, http.StatusOK)
	if got := r.json(t)["retiredReason"]; got != "DISCARDED" {
		t.Fatalf("retiredReason = %v", got)
	}
}

func TestContentErrorsOverHTTP(t *testing.T) {
	e := newEnv(t)
	cat := e.seed()
	e.wantProblem(e.post("/product-contents", fmt.Sprintf(`{"catalogItemId":"ghost","description":"d","primaryCategoryId":"%s"}`, cat)), http.StatusUnprocessableEntity, "unknown-catalog-item")
	e.wantProblem(e.get("/product-contents/ghost"), http.StatusNotFound, "not-found")
	e.wantProblem(e.post("/product-contents/ghost/publications", ""), http.StatusNotFound, "not-found")
	e.wantProblem(e.post("/product-contents", `{"nope":1}`), http.StatusBadRequest, "malformed-request")
	e.wantProblem(e.post("/product-contents/item-1/revisions", `{"catalogItemId":"other"}`), http.StatusUnprocessableEntity, "validation-failed")
	e.wantStatus(e.post("/product-contents", e.createBody(cat)), http.StatusCreated)
	e.wantProblem(e.post("/product-contents", e.createBody(cat)), http.StatusConflict, "concurrent-update")
	e.wantProblem(e.post("/product-contents/item-1/retirements", ""), http.StatusConflict, "illegal-transition")
}

func TestListContentsOverHTTP(t *testing.T) {
	e := newEnv(t)
	cat := e.seed()
	e.wantStatus(e.post("/product-contents", e.createBody(cat)), http.StatusCreated)
	r := e.get("/product-contents?limit=1")
	e.wantStatus(r, http.StatusOK)
	if items := r.json(t)["items"].([]any); len(items) != 1 {
		t.Fatalf("items = %v", items)
	}
	if _, has := r.json(t)["nextCursor"]; has {
		t.Fatalf("the only row must be the last page: %s", r.Body)
	}
}
