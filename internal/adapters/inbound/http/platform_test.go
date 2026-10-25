package http_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestIdempotencyKeyRequiredAndReplay(t *testing.T) {
	e := newEnv(t)
	e.wantProblem(e.do(http.MethodPost, "/categories", "", `{"name":"A"}`), http.StatusBadRequest, "idempotency-key-required")
	e.wantProblem(e.do(http.MethodPost, "/categories", strings.Repeat("k", 129), `{"name":"A"}`), http.StatusBadRequest, "idempotency-key-required")

	first := e.do(http.MethodPost, "/categories", "k1", `{"name":"A"}`)
	e.wantStatus(first, http.StatusCreated)
	again := e.do(http.MethodPost, "/categories", "k1", `{"name":"A"}`)
	e.wantStatus(again, http.StatusCreated)
	if again.Body != first.Body || again.Header.Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay differs: %q vs %q (%v)", again.Body, first.Body, again.Header)
	}
	e.wantProblem(e.do(http.MethodPost, "/categories", "k1", `{"name":"B"}`), http.StatusUnprocessableEntity, "idempotency-key-reused")
	if n := len(e.get("/categories").json(t)["categories"].([]any)); n != 1 {
		t.Fatalf("replay created again: %d categories", n)
	}
}

func TestIdempotencyRecordsClientErrorsNotServerErrors(t *testing.T) {
	e := newEnv(t)
	bad := e.do(http.MethodPost, "/categories", "k2", `{"name":""}`)
	e.wantStatus(bad, http.StatusUnprocessableEntity)
	e.wantStatus(e.do(http.MethodPost, "/categories", "k2", `{"name":""}`), http.StatusUnprocessableEntity)
}

func TestOversizedBodyIsMalformed(t *testing.T) {
	e := newEnv(t)
	big := `{"name":"` + strings.Repeat("a", 70<<10) + `"}`
	e.wantProblem(e.post("/categories", big), http.StatusBadRequest, "malformed-request")
}

func TestRejectNUL(t *testing.T) {
	e := newEnv(t)
	e.wantProblem(e.get("/product-contents/a%00b"), http.StatusNotFound, "resource-not-found")
	e.wantProblem(e.get("/product-contents?after=a%00b"), http.StatusBadRequest, "malformed-request")
	e.wantProblem(e.post("/categories", `{"name":"a\u0000b"}`), http.StatusBadRequest, "malformed-request")
	e.wantProblem(e.post("/categories", "{\"name\":\"a\x00b\"}"), http.StatusBadRequest, "malformed-request")
	e.wantStatus(e.post("/categories", `{"name":"a\\u0000b"}`), http.StatusCreated)
}

func TestHealthAndReadiness(t *testing.T) {
	e := newEnv(t)
	e.wantStatus(e.get("/healthz"), http.StatusOK)
	e.wantStatus(e.get("/readyz"), http.StatusOK)
	e.ready.SetNotReady()
	e.wantStatus(e.get("/readyz"), http.StatusServiceUnavailable)
	e.wantStatus(e.get("/healthz"), http.StatusOK)
}
