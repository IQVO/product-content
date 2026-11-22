//go:build integration

package http_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	httpadapter "github.com/IQVO/product-content/internal/adapters/inbound/http"
	outboundkafka "github.com/IQVO/product-content/internal/adapters/outbound/kafka"
	"github.com/IQVO/product-content/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-content/internal/application/usecases"
	"github.com/IQVO/product-content/internal/testing/pgtest"
)

// The replay classes (ADR 0009) over the REAL router and REAL Postgres: a
// RETRYABLE refusal rolls the whole request back, key row included, so the same
// key retries the work and is then stored like any success.
//
// The pool comes from the package's shared testcontainers helper by type
// inference, so this file names no driver package (the closure shape the fleet
// sensor expects of *_integration_test.go files).

// replayRig is the real router over a testcontainers Postgres pool, wired
// exactly like cmd/api (no Kafka: the outbox rows just stay in the table, the
// relay is not under test).
type replayRig struct {
	srv *httptest.Server
	// count/exec work on the rig's pool from outside any request; seedItem
	// applies what the CatalogItemCreated consumer would apply to the read
	// model.
	count    func(t *testing.T, query string, args ...any) int
	exec     func(t *testing.T, sql string)
	seedItem func(t *testing.T, id string)
}

func newReplayRig(t *testing.T) *replayRig {
	t.Helper()
	pool := pgtest.NewPool(t)
	deps := usecases.Deps{
		Contents: postgres.NewContentRepository(pool), Categories: postgres.NewTreeRepository(pool),
		Items: postgres.NewCatalogItemRepository(pool), Inbox: postgres.NewInbox(pool),
		Outbox: postgres.NewOutboxRepo(pool, outboundkafka.NewEncoder()),
		UoW:    postgres.NewUnitOfWork(pool), Clock: wallClock{},
	}
	api := &httpadapter.Server{
		CreateProductContent: usecases.CreateProductContent{Deps: deps}, ReviseContent: usecases.ReviseContent{Deps: deps},
		PublishContent: usecases.PublishContent{Deps: deps}, RetireContent: usecases.RetireContent{Deps: deps},
		DiscardDraft: usecases.DiscardDraft{Deps: deps}, GetProductContent: usecases.GetProductContent{Deps: deps},
		ListProductContents: usecases.ListProductContents{Deps: deps}, CreateCategory: usecases.CreateCategory{Deps: deps},
		MoveCategory: usecases.MoveCategory{Deps: deps}, RenameCategory: usecases.RenameCategory{Deps: deps},
		PublishCategoryTree: usecases.PublishCategoryTree{Deps: deps}, ListCategories: usecases.ListCategories{Deps: deps},
		IdempotencyStore: postgres.NewIdempotencyStore(pool), Clock: wallClock{},
	}
	srv := httptest.NewServer(httpadapter.NewRouter(api, nil, ""))
	t.Cleanup(srv.Close)
	return &replayRig{
		srv: srv,
		count: func(t *testing.T, query string, args ...any) int {
			t.Helper()
			var n int
			if err := pool.QueryRow(t.Context(), query, args...).Scan(&n); err != nil {
				t.Fatal(err)
			}
			return n
		},
		exec: func(t *testing.T, sql string) {
			t.Helper()
			if _, err := pool.Exec(t.Context(), sql); err != nil {
				t.Fatalf("%s: %v", sql, err)
			}
		},
		seedItem: func(t *testing.T, id string) {
			t.Helper()
			if err := postgres.NewCatalogItemRepository(pool).MarkActive(context.Background(), id, time.Now()); err != nil {
				t.Fatal(err)
			}
		},
	}
}

func (r *replayRig) keyRows(t *testing.T, key string) int {
	t.Helper()
	return r.count(t, `SELECT count(*) FROM idempotency_keys WHERE route = '' AND key = $1`, key)
}

type replayCall struct {
	status   int
	replayed string
	body     string
}

func (c replayCall) want(t *testing.T, status int, slug string) {
	t.Helper()
	if c.status != status || (slug != "" && !strings.Contains(c.body, slug)) {
		t.Fatalf("answer = %d %s, want %d containing %q", c.status, c.body, status, slug)
	}
}

func replayPost(t *testing.T, srv *httptest.Server, path, key, body string) replayCall {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Idempotency-Key", key)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return replayCall{status: resp.StatusCode, replayed: resp.Header.Get("Idempotency-Replayed"), body: string(b)}
}

// concurrent-update is RETRYABLE. The version-guarded UPDATE of a revision
// cannot be made to lose a race by timing (the load and the save share the
// request's transaction), so a trigger makes the UPDATE match no row: exactly
// what the guard sees when another writer got in between the load and the
// save, on both attempts of the use case (commandAttempts). The refused
// request must leave no key row; the same key then runs again, is stored and
// is replayed.
func TestAConcurrentUpdateRefusalIsNotStoredSoItsKeyRetries(t *testing.T) {
	r := newReplayRig(t)
	r.seedItem(t, "sku-cu")
	if created := replayPost(t, r.srv, "/product-contents", "cu-create", `{"catalogItemId":"sku-cu","description":"v1"}`); created.status != http.StatusCreated {
		t.Fatalf("create: %d %s", created.status, created.body)
	}
	const key, path = "cu-refuse", "/product-contents/sku-cu/revisions"

	r.exec(t, `CREATE FUNCTION pc_drop_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$`)
	r.exec(t, `CREATE TRIGGER pc_drop_update BEFORE UPDATE ON product_contents FOR EACH ROW EXECUTE FUNCTION pc_drop_update()`)
	refused := replayPost(t, r.srv, path, key, `{"description":"v2"}`)
	refused.want(t, http.StatusConflict, "concurrent-update")
	if n := r.keyRows(t, key); n != 0 {
		t.Fatalf("the refused request left %d row(s) under its key, want 0: a concurrent-update refusal is RETRYABLE and must release the key", n)
	}

	r.exec(t, `DROP TRIGGER pc_drop_update ON product_contents`)
	retry := replayPost(t, r.srv, path, key, `{"description":"v2"}`)
	retry.want(t, http.StatusOK, "")
	if retry.replayed != "" {
		t.Fatalf("the retry under the released key = %+v, want a fresh 200", retry)
	}
	if n := r.keyRows(t, key); n != 1 {
		t.Fatalf("%d row(s) under the key after the retry succeeded, want 1 (a 2xx is stored)", n)
	}
	again := replayPost(t, r.srv, path, key, `{"description":"v2"}`)
	if again.status != http.StatusOK || again.body != retry.body || again.replayed != "true" {
		t.Fatalf("third call = %+v, want the stored 200 replayed", again)
	}
	if n := r.count(t, `SELECT count(*) FROM product_contents WHERE catalog_item_id = 'sku-cu'`); n != 1 {
		t.Fatalf("%d content row(s), want 1", n)
	}
}

// unknown-catalog-item is RETRYABLE: the item is a fact this service only
// learns from CatalogItemCreated events, so it can exist a moment after the
// request that found nothing. The 422 must not pin that answer under the key.
func TestAnUnknownCatalogItemRefusalIsNotStoredSoTheCreateRetriesOnceTheItemArrives(t *testing.T) {
	r := newReplayRig(t)
	const key, body = "late-item", `{"catalogItemId":"sku-late","description":"Late."}`

	refused := replayPost(t, r.srv, "/product-contents", key, body)
	refused.want(t, http.StatusUnprocessableEntity, "unknown-catalog-item")
	if n := r.keyRows(t, key); n != 0 {
		t.Fatalf("the 422 left %d row(s) under its key, want 0: unknown-catalog-item is RETRYABLE and must release the key", n)
	}

	r.seedItem(t, "sku-late")
	retry := replayPost(t, r.srv, "/product-contents", key, body)
	retry.want(t, http.StatusCreated, "")
	if retry.replayed != "" {
		t.Fatalf("the retry under the released key = %+v, want a fresh 201", retry)
	}
	if n := r.keyRows(t, key); n != 1 {
		t.Fatalf("%d row(s) under the key after the retry succeeded, want 1", n)
	}
	again := replayPost(t, r.srv, "/product-contents", key, body)
	if again.status != http.StatusCreated || again.body != retry.body || again.replayed != "true" {
		t.Fatalf("third call = %+v, want the stored 201 replayed", again)
	}
}
