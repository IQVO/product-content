//go:build integration

package http_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	httpadapter "github.com/IQVO/product-content/internal/adapters/inbound/http"
	"github.com/IQVO/product-content/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-content/internal/adapters/outbound/postgres/pgtx"
	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/testing/pgtest"
)

func TestMain(m *testing.M) {
	code := m.Run()
	pgtest.Shutdown()
	os.Exit(code)
}

// poisonTx simulates what a mapped domain conflict does to the middleware's
// transaction: a statement fails on the server (a unique violation, like a lost
// race on a unique index), the handler maps it to a typed error and answers 4xx,
// but the transaction is ABORTED from then on.
func poisonTx(t *testing.T, r *http.Request) {
	t.Helper()
	tx, ok := pgtx.From(r.Context())
	if !ok {
		t.Fatal("the middleware did not bind a transaction into the request context")
	}
	const dup = `INSERT INTO idempotency_keys (route, key, request_hash, status, content_type, body, created_at)
		VALUES ('poison', 'dup', 'h', 200, '', ''::bytea, now())`
	for range 2 {
		if _, err := tx.Exec(r.Context(), dup); err != nil {
			return // the second insert aborts the transaction (23505)
		}
	}
	t.Fatal("the duplicate insert did not abort the transaction")
}

// wallClock is the real time: the Postgres store judges TTL with the database's
// now(), so the stamped created_at must live on the same timeline.
type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }

func keyedPost(key string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/product-contents/sku-1/publications", nil)
	req.Header.Set(httpadapter.IdempotencyKeyHeader, key)
	return req
}

func poisonMiddleware(t *testing.T, h http.Handler) (http.Handler, func(key string) int) {
	t.Helper()
	pool := pgtest.NewPool(t)
	count := func(key string) int {
		var n int
		if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM idempotency_keys WHERE route = '' AND key = $1`, key).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	return httpadapter.RequireIdempotencyKey(postgres.NewIdempotencyStore(pool), wallClock{})(h), count
}

// TestIdempotencyAbortedTransactionAnswersTheRefusalNotA500: a handler that
// loses a unique-index race answers its typed refusal, not a 500 — and since
// concurrent-update is RETRYABLE (ADR 0009), the refusal is not stored: the key
// is released and the same key executes again (still the truthful 409 while the
// race keeps losing).
func TestIdempotencyAbortedTransactionAnswersTheRefusalNotA500(t *testing.T) {
	var calls atomic.Int64
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		poisonTx(t, r)
		httpadapter.WriteError(w, r, ports.ErrConcurrentUpdate)
	})
	mw, count := poisonMiddleware(t, h)

	first := httptest.NewRecorder()
	mw.ServeHTTP(first, keyedPost("k-poison"))
	if first.Code != http.StatusConflict {
		t.Fatalf("first = %d %s, want the truthful 409", first.Code, first.Body.String())
	}
	if n := count("k-poison"); n != 0 {
		t.Fatalf("a RETRYABLE refusal over an aborted transaction left %d rows, want 0: the key is released", n)
	}
	retry := httptest.NewRecorder()
	mw.ServeHTTP(retry, keyedPost("k-poison"))
	if retry.Code != http.StatusConflict || retry.Header().Get(httpadapter.ReplayedHeader) != "" {
		t.Fatalf("retry = %d replayed=%q, want a fresh 409 (the released key runs again)", retry.Code, retry.Header().Get(httpadapter.ReplayedHeader))
	}
	if calls.Load() != 2 {
		t.Fatalf("handler ran %d times, want 2: a released refusal never pins the key", calls.Load())
	}
}

// TestIdempotencyAbortedTransactionKeepsAFinal4xx: a FINAL refusal over an
// aborted transaction is healed (the handler's savepoint is rolled back, the
// transaction survives) and is still stored and replayed: the release of
// RETRYABLE answers must not cost the store its 4xx memory.
func TestIdempotencyAbortedTransactionKeepsAFinal4xx(t *testing.T) {
	var calls atomic.Int64
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		poisonTx(t, r)
		httpadapter.WriteError(w, r, content.ErrTooManyBullets)
	})
	mw, count := poisonMiddleware(t, h)

	first := httptest.NewRecorder()
	mw.ServeHTTP(first, keyedPost("k-poison-422"))
	if first.Code != http.StatusUnprocessableEntity {
		t.Fatalf("first = %d %s, want the healed 422", first.Code, first.Body.String())
	}
	if n := count("k-poison-422"); n != 1 {
		t.Fatalf("the FINAL 422 outcome was not committed: %d rows", n)
	}
	replay := httptest.NewRecorder()
	mw.ServeHTTP(replay, keyedPost("k-poison-422"))
	if replay.Code != http.StatusUnprocessableEntity || replay.Header().Get(httpadapter.ReplayedHeader) != "true" {
		t.Fatalf("replay = %d replayed=%q, want the cached 422", replay.Code, replay.Header().Get(httpadapter.ReplayedHeader))
	}
	if replay.Body.String() != first.Body.String() {
		t.Fatalf("replay body = %s, want %s", replay.Body.String(), first.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("handler ran %d times, want 1 (the replay must not re-run it)", calls.Load())
	}
}

// TestIdempotencyAbortedTransaction2xxIsNeverCached: a success whose writes
// died with the transaction is a lie; it fails the request, caches nothing and
// the same key retries the real work.
func TestIdempotencyAbortedTransaction2xxIsNeverCached(t *testing.T) {
	var calls atomic.Int64
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			poisonTx(t, r) // only the first attempt dies inside the transaction
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mw, count := poisonMiddleware(t, h)

	first := httptest.NewRecorder()
	mw.ServeHTTP(first, keyedPost("k-lie"))
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("first = %d %s, want 500 (the writes never happened)", first.Code, first.Body.String())
	}
	if n := count("k-lie"); n != 0 {
		t.Fatalf("a failed success left %d idempotency rows: the key could not retry", n)
	}
	second := httptest.NewRecorder()
	mw.ServeHTTP(second, keyedPost("k-lie"))
	if second.Code != http.StatusCreated {
		t.Fatalf("retry = %d %s, want the real work to succeed", second.Code, second.Body.String())
	}
	if calls.Load() != 2 {
		t.Fatalf("handler ran %d times, want 2", calls.Load())
	}
}
