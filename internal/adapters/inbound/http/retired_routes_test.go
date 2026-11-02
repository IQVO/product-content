package http_test

import (
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	httpadapter "github.com/IQVO/product-content/internal/adapters/inbound/http"
)

// TestRetiredRoutesAreGone: the pre-guideline routes (`:custom-method`
// suffixes) are removed, not aliased; each answers 404 or 405.
func TestRetiredRoutesAreGone(t *testing.T) {
	e := newEnv(t)
	cat := e.seed()
	e.wantStatus(e.post("/product-contents", e.createBody(cat)), http.StatusCreated)
	cases := []struct{ name, method, path, body string }{
		{"revise", http.MethodPost, "/product-contents/item-1:revise", e.createBody(cat)},
		{"publish", http.MethodPost, "/product-contents/item-1:publish", ``},
		{"retire", http.MethodPost, "/product-contents/item-1:retire", ``},
		{"discard", http.MethodPost, "/product-contents/item-1:discard", ``},
		{"categories:publish", http.MethodPost, "/categories:publish", ``},
		{"move", http.MethodPost, "/categories/c1:move", `{"parentId":null}`},
		{"rename", http.MethodPost, "/categories/c1:rename", `{"name":"X"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := e.do(c.method, c.path, "retired-"+c.name, c.body)
			if r.Status != http.StatusNotFound && r.Status != http.StatusMethodNotAllowed {
				t.Fatalf("%s %s answered %d, want 404 or 405: %s", c.method, c.path, r.Status, r.Body)
			}
		})
	}
	// The draft is still there: nothing above ran a command.
	if got := e.get("/product-contents/item-1").json(t)["state"]; got != "DRAFT" {
		t.Fatalf("state = %v, a retired route must not run its command", got)
	}
}

// TestNoRouteIsACustomMethod walks the router: no registered pattern carries a
// `:custom-method` suffix.
func TestNoRouteIsACustomMethod(t *testing.T) {
	routes, ok := httpadapter.NewRouter(&httpadapter.Server{}, slog.New(slog.DiscardHandler), "").(chi.Routes)
	if !ok {
		t.Fatal("the router is not a chi.Routes")
	}
	n := 0
	err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		n++
		if strings.Contains(route, ":") {
			t.Errorf("%s %s is a retired route shape", method, route)
		}
		return nil
	})
	if err != nil || n == 0 {
		t.Fatalf("walked %d routes: %v", n, err)
	}
}

// PUT and DELETE are idempotent by method: a key sent to them is accepted and
// ignored, so a second request with the same key runs again (no replay, no 409).
func TestPutAndDeleteAcceptAnIdempotencyKeyAndIgnoreIt(t *testing.T) {
	e := newEnv(t)
	cat := e.seed()
	id := e.post("/categories", `{"name":"Home"}`).json(t)["categoryId"].(string)

	first := e.do(http.MethodPut, "/categories/"+id+"/name", "put-key", `{"name":"Flasks"}`)
	e.wantStatus(first, http.StatusOK)
	second := e.do(http.MethodPut, "/categories/"+id+"/name", "put-key", `{"name":"Cups"}`)
	e.wantStatus(second, http.StatusOK)
	if second.Header.Get("Idempotency-Replayed") != "" || second.json(t)["name"] != "Cups" {
		t.Fatalf("a PUT must run again whatever its key: %v %s", second.Header, second.Body)
	}

	e.wantStatus(e.post("/product-contents", e.createBody(cat)), http.StatusCreated)
	e.wantStatus(e.do(http.MethodDelete, "/product-contents/item-1/draft", "del-key", ""), http.StatusOK)
	again := e.do(http.MethodDelete, "/product-contents/item-1/draft", "del-key", "")
	e.wantProblem(again, http.StatusConflict, "illegal-transition")
	if again.Header.Get("Idempotency-Replayed") != "" {
		t.Fatalf("a DELETE must not be replayed: %v", again.Header)
	}
}
