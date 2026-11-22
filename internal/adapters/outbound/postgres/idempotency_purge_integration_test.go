//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/IQVO/product-content/internal/adapters/outbound/postgres"
	"github.com/IQVO/product-content/internal/adapters/outbound/repocontract"
	"github.com/IQVO/product-content/internal/application/idempotency"
	"github.com/IQVO/product-content/internal/testing/pgtest"
)

// answer returns an fn that answers status with body.
func answer(status int, body string) func(context.Context) (idempotency.Record, error) {
	return func(context.Context) (idempotency.Record, error) {
		return idempotency.Record{Status: status, ContentType: "application/json", Body: []byte(body)}, nil
	}
}

// One pass deletes at most batch rows; the next passes finish the backlog
// (the store-level proofs behind the middleware-level ones of the http
// package).
func TestIdempotencyPurgeDeletesAtMostOneBatchPerPass(t *testing.T) {
	pool := pgtest.NewPool(t)
	store := postgres.NewIdempotencyStore(pool)
	ctx := context.Background()
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		if _, _, err := store.Do(ctx, k, "h", time.Now().UTC(), answer(201, k)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx,
			`UPDATE idempotency_keys SET created_at = now() - make_interval(secs => $2) WHERE route = '' AND key = $1`,
			k, (idempotency.TTL + time.Hour).Seconds()); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []int{2, 2, 1, 0} {
		if n, err := store.PurgeExpired(ctx, 2); err != nil || n != want {
			t.Fatalf("purge = %d %v, want %d", n, err, want)
		}
	}
	var left int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_keys`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("rows left = %d, want 0", left)
	}
}

// A purge never waits for, nor breaks, a request that holds the key: Do keeps
// its claim transaction open while the purge runs over the expired row of
// that same key (the SKIP LOCKED proof); the request still answers fresh,
// stores its row, and the next call replays it.
func TestIdempotencyPurgeSparesARowARequestHolds(t *testing.T) {
	pool := pgtest.NewPool(t)
	store := postgres.NewIdempotencyStore(pool)
	ctx := context.Background()
	if _, _, err := store.Do(ctx, "held", "old-hash", time.Now().UTC(), answer(200, "stale")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE idempotency_keys SET created_at = now() - make_interval(secs => $2) WHERE route = '' AND key = $1`,
		"held", (idempotency.TTL + time.Hour).Seconds()); err != nil {
		t.Fatal(err)
	}

	inFlight, release := make(chan struct{}), make(chan struct{})
	type result struct {
		rec      idempotency.Record
		replayed bool
		err      error
	}
	done := make(chan result, 1)
	go func() {
		rec, replayed, err := store.Do(ctx, "held", "new-hash", time.Now().UTC(),
			func(context.Context) (idempotency.Record, error) {
				close(inFlight)
				<-release
				return idempotency.Record{Status: 201, ContentType: "application/json", Body: []byte("fresh")}, nil
			})
		done <- result{rec, replayed, err}
	}()
	<-inFlight

	purged := make(chan error, 1)
	go func() { _, err := store.PurgeExpired(ctx, 100); purged <- err }()
	select {
	case err := <-purged:
		if err != nil {
			t.Fatalf("purge during an in-flight request: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the purge blocked on a request that holds the key")
	}

	close(release)
	r := <-done
	if r.err != nil || r.replayed || string(r.rec.Body) != "fresh" {
		t.Fatalf("the request holding the key = %+v %v %v, want a fresh 201", r.rec, r.replayed, r.err)
	}
	rec, replayed, err := store.Do(ctx, "held", "new-hash", time.Now().UTC(), answer(200, "again"))
	if err != nil || !replayed || string(rec.Body) != "fresh" {
		t.Fatalf("after purge + request = %+v %v %v, want the fresh answer replayed", rec, replayed, err)
	}
}

// The Postgres store passes the same retention contract as the memory double.
func TestIdempotencyPostgresPassesThePurgeContract(t *testing.T) {
	repocontract.RunIdempotencyPurge(t, func(t *testing.T) repocontract.IdempotencyFixture {
		pool := pgtest.NewPool(t)
		return repocontract.IdempotencyFixture{
			Store: postgres.NewIdempotencyStore(pool),
			Stamp: func() time.Time { return time.Now().UTC() },
			Age: func(t *testing.T, d time.Duration) {
				t.Helper()
				if _, err := pool.Exec(t.Context(),
					`UPDATE idempotency_keys SET created_at = created_at - make_interval(secs => $1)`, d.Seconds()); err != nil {
					t.Fatal(err)
				}
			},
		}
	})
}
