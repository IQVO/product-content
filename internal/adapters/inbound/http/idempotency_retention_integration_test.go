//go:build integration

// The Idempotency-Key retention over the REAL middleware and REAL Postgres
// (the testcontainers pool of the package's shared helper, like the
// replay-class wiring test): a purge deletes an expired key row and keeps a
// live one; a replay of an expired key EXECUTES instead of answering (the age
// guard of the read, pinned here); a purge that overlaps a request holding
// the key neither blocks nor breaks it. The purge runs through a narrow
// local interface so this file compiles on a store without it and the red
// run names the missing retention.
package http_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpadapter "github.com/IQVO/product-content/internal/adapters/inbound/http"
	"github.com/IQVO/product-content/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-content/internal/application/idempotency"
	"github.com/IQVO/product-content/internal/testing/pgtest"
)

// idempotencyPurger is the retention half of the idempotency store: one
// bounded batch delete of rows older than the TTL.
type idempotencyPurger interface {
	PurgeExpired(ctx context.Context, limit int) (deleted int, err error)
}

// purgerOver returns the store's purge, or fails the red run by name.
func purgerOver(t *testing.T, store *postgres.IdempotencyStore) idempotencyPurger {
	t.Helper()
	p, ok := any(store).(idempotencyPurger)
	if !ok {
		t.Fatal("the Postgres IdempotencyStore has no PurgeExpired: expired idempotency_keys rows are never deleted")
	}
	return p
}

// ageRows returns the pool's row-aging statement runner: age(key, d) moves
// the stored record of key to exactly d past its stamp. The command-tag type
// stays generic, so this file names no driver.
func ageRows[T any](t *testing.T, exec func(context.Context, string, ...any) (T, error)) func(string, time.Duration) {
	t.Helper()
	return func(key string, d time.Duration) {
		t.Helper()
		if _, err := exec(t.Context(),
			`UPDATE idempotency_keys SET created_at = now() - make_interval(secs => $2) WHERE route = '' AND key = $1`,
			key, d.Seconds()); err != nil {
			t.Fatal(err)
		}
	}
}

// created answers 201 with body.
func created(body string) answerFn {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(body))
	}
}

// An expired row is deleted by the purge, a row a minute short of the TTL and
// a fresh one are kept and still replay.
func TestIdempotencyPurgeDeletesTheExpiredRowAndKeepsTheLiveOnes(t *testing.T) {
	pool := pgtest.NewPool(t)
	store := postgres.NewIdempotencyStore(pool)
	purge := purgerOver(t, store)
	count := func(key string) int {
		var n int
		if err := pool.QueryRow(t.Context(),
			`SELECT count(*) FROM idempotency_keys WHERE route = '' AND key = $1`, key).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	mw := httpadapter.RequireIdempotencyKey(store, wallClock{})

	for _, key := range []string{"ret-old", "ret-edge", "ret-live"} {
		h := &fakeHandler{script: []answerFn{created(`{"k":"` + key + `"}`)}}
		if rec := post(t, mw(h), key, `{"n":1}`); rec.Code != http.StatusCreated {
			t.Fatalf("%s: first answer = %d", key, rec.Code)
		}
	}
	age := ageRows(t, pool.Exec)
	age("ret-old", idempotency.TTL+time.Minute)
	age("ret-edge", idempotency.TTL-time.Minute)

	n, err := purge.PurgeExpired(t.Context(), 100)
	if err != nil || n != 1 {
		t.Fatalf("purge = %d %v, want 1 (only the expired row)", n, err)
	}
	if got := count("ret-old"); got != 0 {
		t.Errorf("the expired row survived: %d rows", got)
	}
	for _, key := range []string{"ret-edge", "ret-live"} {
		if got := count(key); got != 1 {
			t.Errorf("%s: %d rows, want the live row kept", key, got)
		}
		h := &fakeHandler{script: []answerFn{created(`{"other":true}`)}}
		rec := post(t, mw(h), key, `{"n":1}`)
		if rec.Code != http.StatusCreated || !replayed(rec) || rec.Body.String() != `{"k":"`+key+`"}` {
			t.Errorf("%s after the purge = %d replayed=%v %q, want the stored answer replayed", key, rec.Code, replayed(rec), rec.Body.String())
		}
		if h.calls != 0 {
			t.Errorf("%s: the handler ran %d times after the purge, want 0", key, h.calls)
		}
	}
	if n, err := purge.PurgeExpired(t.Context(), 100); err != nil || n != 0 {
		t.Fatalf("second purge = %d %v, want 0", n, err)
	}
}

// The age guard of the replay read, pinned: a row past the TTL can never
// answer a replay between expiry and the sweep — the request EXECUTES again
// and stores a fresh answer.
func TestAReplayOfAnExpiredIdempotencyKeyExecutes(t *testing.T) {
	pool := pgtest.NewPool(t)
	store := postgres.NewIdempotencyStore(pool)
	mw := httpadapter.RequireIdempotencyKey(store, wallClock{})
	firstAnswer := created(`{"run":1}`)
	secondAnswer := created(`{"run":2}`)
	h := &fakeHandler{script: []answerFn{firstAnswer, secondAnswer}}
	first := post(t, mw(h), "ret-expired", `{"n":1}`)
	if first.Code != http.StatusCreated || replayed(first) || h.calls != 1 {
		t.Fatalf("first = %d replayed=%v calls=%d", first.Code, replayed(first), h.calls)
	}
	age := ageRows(t, pool.Exec)
	age("ret-expired", idempotency.TTL+time.Minute)

	second := post(t, mw(h), "ret-expired", `{"n":1}`)
	if second.Code != http.StatusCreated {
		t.Fatalf("replay of an expired key = %d %s", second.Code, second.Body.String())
	}
	if replayed(second) || h.calls != 2 || second.Body.String() != `{"run":2}` {
		t.Fatalf("an expired key answered %q replayed=%v after %d calls, want a fresh execution", second.Body.String(), replayed(second), h.calls)
	}
	third := post(t, mw(h), "ret-expired", `{"n":1}`)
	if !replayed(third) || h.calls != 2 || third.Body.String() != `{"run":2}` {
		t.Fatalf("the fresh answer must be stored: replayed=%v calls=%d", replayed(third), h.calls)
	}
}

// A purge that overlaps a request holding the (expired) key neither fails
// nor blocks nor breaks it: the request answers fresh, its row is stored and
// replays afterwards.
func TestAPurgeRacingARequestHoldingTheKeySparesIt(t *testing.T) {
	pool := pgtest.NewPool(t)
	store := postgres.NewIdempotencyStore(pool)
	purge := purgerOver(t, store)
	mw := httpadapter.RequireIdempotencyKey(store, wallClock{})
	if rec := post(t, mw(&fakeHandler{script: []answerFn{created(`{"stale":true}`)}}), "ret-held", `{"n":1}`); rec.Code != http.StatusCreated {
		t.Fatalf("first answer = %d", rec.Code)
	}
	age := ageRows(t, pool.Exec)
	age("ret-held", idempotency.TTL+time.Hour)

	inFlight, release := make(chan struct{}), make(chan struct{})
	fresh := func(w http.ResponseWriter, _ *http.Request) {
		close(inFlight)
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"fresh":true}`))
	}
	holder := &fakeHandler{script: []answerFn{fresh}}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- post(t, mw(holder), "ret-held", `{"n":1}`) }()
	<-inFlight

	purged := make(chan error, 1)
	go func() { _, err := purge.PurgeExpired(t.Context(), 100); purged <- err }()
	select {
	case err := <-purged:
		if err != nil {
			t.Fatalf("purge during an in-flight request: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the purge blocked on a request that holds the key")
	}

	close(release)
	rec := <-done
	if rec.Code != http.StatusCreated || replayed(rec) || rec.Body.String() != `{"fresh":true}` {
		t.Fatalf("the request holding the key = %d replayed=%v %q, want a fresh 201", rec.Code, replayed(rec), rec.Body.String())
	}
	var rows int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM idempotency_keys WHERE route = '' AND key = $1`, "ret-held").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("rows held for the key = %d, want 1: the fresh answer must be stored", rows)
	}
}
