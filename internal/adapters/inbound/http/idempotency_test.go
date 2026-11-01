package http_test

import (
	"net/http"
	"sync"
	"testing"
)

// The key is global (REST guideline 2.3): the same key on another route is a
// different fingerprint, so it is a 409 reuse and not a second execution.
func TestIdempotencyKeyIsGlobalAcrossRoutes(t *testing.T) {
	e := newEnv(t)
	e.wantStatus(e.do(http.MethodPost, "/categories", "shared-key", `{"name":"A"}`), http.StatusCreated)
	e.wantProblem(e.do(http.MethodPost, "/category-tree-publications", "shared-key", ``), http.StatusConflict, "idempotency-key-reused")
	if got := e.get("/categories").json(t)["version"]; got != float64(1) {
		t.Fatalf("version = %v, the refused request must publish nothing", got)
	}
	// The same key, the same path and another body is a reuse too.
	e.wantProblem(e.do(http.MethodPost, "/categories", "shared-key", `{"name":"B"}`), http.StatusConflict, "idempotency-key-reused")
	// The reuse answer is not stored over the first answer.
	replay := e.do(http.MethodPost, "/categories", "shared-key", `{"name":"A"}`)
	e.wantStatus(replay, http.StatusCreated)
	if replay.Header.Get("Idempotency-Replayed") != "true" {
		t.Fatalf("the original request must still replay: %v", replay.Header)
	}
}

// Every POST route is behind the middleware: none runs without a key.
func TestEveryPostRouteRequiresAnIdempotencyKey(t *testing.T) {
	e := newEnv(t)
	cat := e.seed()
	e.wantStatus(e.post("/product-contents", e.createBody(cat)), http.StatusCreated)
	for _, path := range []string{
		"/product-contents", "/product-contents/item-1/revisions", "/product-contents/item-1/publications",
		"/product-contents/item-1/retirements", "/categories", "/category-tree-publications",
	} {
		e.wantProblem(e.do(http.MethodPost, path, "", `{}`), http.StatusBadRequest, "idempotency-key-required")
	}
}

// A byte-identical replay of a lifecycle command carries Idempotency-Replayed
// and runs the command once.
func TestLifecycleReplayRunsOnce(t *testing.T) {
	e := newEnv(t)
	cat := e.seed()
	e.wantStatus(e.post("/product-contents", e.createBody(cat)), http.StatusCreated)
	first := e.do(http.MethodPost, "/product-contents/item-1/publications", "pub-1", "")
	e.wantStatus(first, http.StatusOK)
	if first.Header.Get("Idempotency-Replayed") != "" {
		t.Fatalf("the first execution carries Idempotency-Replayed: %v", first.Header)
	}
	again := e.do(http.MethodPost, "/product-contents/item-1/publications", "pub-1", "")
	e.wantStatus(again, http.StatusOK)
	if again.Body != first.Body || again.Header.Get("Idempotency-Replayed") != "true" {
		t.Fatalf("replay differs: %q vs %q (%v)", again.Body, first.Body, again.Header)
	}
	// CategoryTreePublished (the seed category) and one ProductContentPublished.
	if n := len(e.store.Outbox().Events()); n != 2 {
		t.Fatalf("outbox holds %d events, want 2: the replay must not publish again", n)
	}
}

// N concurrent requests with one key create exactly one aggregate; the others
// replay its answer.
func TestConcurrentSameKeyCreatesOneCategory(t *testing.T) {
	e := newEnv(t)
	const callers = 8
	var wg sync.WaitGroup
	statuses := make([]int, callers)
	replays := make([]bool, callers)
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := e.do(http.MethodPost, "/categories", "race-key", `{"name":"Race"}`)
			statuses[i], replays[i] = r.Status, r.Header.Get("Idempotency-Replayed") == "true"
		}()
	}
	wg.Wait()
	nReplays := 0
	for i := range callers {
		if statuses[i] != http.StatusCreated {
			t.Fatalf("caller %d answered %d", i, statuses[i])
		}
		if replays[i] {
			nReplays++
		}
	}
	if nReplays != callers-1 {
		t.Fatalf("%d replays of %d callers, want %d", nReplays, callers, callers-1)
	}
	if n := len(e.get("/categories").json(t)["categories"].([]any)); n != 1 {
		t.Fatalf("%d categories, want exactly one", n)
	}
}
