package repocontract

import (
	"context"
	"testing"
	"time"

	"github.com/IQVO/product-content/internal/application/idempotency"
	"github.com/IQVO/product-content/internal/application/ports"
)

// IdempotencyFixture is one adapter's idempotency store under the retention
// contract. Stamp is the clock the records are stamped with (the at of Do);
// Age moves every stored record d beyond its stamp as the store's own clock
// sees it (the memory double advances its clock; the Postgres store shifts
// created_at back), so expired means what the STORE under test will call
// expired.
type IdempotencyFixture struct {
	Store ports.IdempotencyStore
	Stamp func() time.Time
	Age   func(t *testing.T, d time.Duration)
}

// RunIdempotencyPurge executes the retention suite; factory must return a
// fixture over an empty store each time.
func RunIdempotencyPurge(t *testing.T, factory func(t *testing.T) IdempotencyFixture) {
	t.Helper()
	cases := []struct {
		name string
		run  func(*testing.T, IdempotencyFixture)
	}{
		{"deletes expired keeps live", purgeKeepsLive},
		{"one batch per pass", purgeBatches},
		{"oldest first", purgeOldestFirst},
		{"a request holding the key", purgeHeldKey},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.run(t, factory(t)) })
	}
}

// purgeKeepsLive: a row older than the TTL goes, a row a minute short of it
// and a fresh one stay and replay; a second pass has nothing left to delete.
func purgeKeepsLive(t *testing.T, f IdempotencyFixture) {
	purgePut(t, f, "old", "v-old")
	f.Age(t, idempotency.TTL+time.Minute)
	purgePut(t, f, "edge", "v-edge")
	f.Age(t, idempotency.TTL-time.Minute)
	purgePut(t, f, "live", "v-live")
	if n, err := f.Store.PurgeExpired(bg(), 100); err != nil || n != 1 {
		t.Fatalf("purge = %d %v, want 1 (only the expired row)", n, err)
	}
	purgeWantReplay(t, f, "edge", "v-edge")
	purgeWantReplay(t, f, "live", "v-live")
	if n, err := f.Store.PurgeExpired(bg(), 100); err != nil || n != 0 {
		t.Fatalf("second purge = %d %v, want 0", n, err)
	}
}

// purgeBatches: one pass deletes at most limit rows, the next ones finish.
func purgeBatches(t *testing.T, f IdempotencyFixture) {
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		purgePut(t, f, k, k)
	}
	f.Age(t, idempotency.TTL+time.Hour)
	for _, want := range []int{2, 2, 1, 0} {
		if n, err := f.Store.PurgeExpired(bg(), 2); err != nil || n != want {
			t.Fatalf("purge = %d %v, want %d", n, err, want)
		}
	}
}

// purgeOldestFirst: with two expired rows and a limit of one, the older goes.
// Ageing everything back by more than both ages makes the survivor live
// again, which is what tells the two apart (an expired row is never
// replayed).
func purgeOldestFirst(t *testing.T, f IdempotencyFixture) {
	purgePut(t, f, "older", "v-older")
	f.Age(t, time.Hour)
	purgePut(t, f, "newer", "v-newer")
	f.Age(t, idempotency.TTL)
	if n, err := f.Store.PurgeExpired(bg(), 1); err != nil || n != 1 {
		t.Fatalf("purge = %d %v, want 1", n, err)
	}
	f.Age(t, -(idempotency.TTL + time.Hour)) // both would be live again were they still stored
	purgeWantReplay(t, f, "newer", "v-newer")
}

// purgeHeldKey: a purge that overlaps a request holding the (expired) key
// neither fails nor breaks it: the request answers fresh, its row is stored
// and replays afterwards. (Postgres additionally never blocks the purge on
// that request: its own integration test.)
func purgeHeldKey(t *testing.T, f IdempotencyFixture) {
	purgePut(t, f, "held", "stale")
	f.Age(t, idempotency.TTL+time.Hour)
	inFlight, release := make(chan struct{}), make(chan struct{})
	type result struct {
		rec      idempotency.Record
		replayed bool
		err      error
	}
	done := make(chan result, 1)
	go func() {
		rec, replayed, err := f.Store.Do(bg(), "held", "h", f.Stamp(),
			func(context.Context) (idempotency.Record, error) {
				close(inFlight)
				<-release
				return idempotency.Record{Status: 201, Body: []byte("fresh")}, nil
			})
		done <- result{rec, replayed, err}
	}()
	<-inFlight
	purged := make(chan error, 1)
	go func() { _, err := f.Store.PurgeExpired(bg(), 100); purged <- err }()
	time.Sleep(20 * time.Millisecond) // let the purge overlap the request
	close(release)
	if err := <-purged; err != nil {
		t.Fatalf("purge during an in-flight request: %v", err)
	}
	if r := <-done; r.err != nil || r.replayed || string(r.rec.Body) != "fresh" {
		t.Fatalf("the request holding the key = %+v replayed %v err %v, want a fresh answer", r.rec, r.replayed, r.err)
	}
	purgeWantReplay(t, f, "held", "fresh")
}

// purgePut stores one answer under key (a fresh claim).
func purgePut(t *testing.T, f IdempotencyFixture, key, body string) {
	t.Helper()
	rec, replayed, err := f.Store.Do(bg(), key, "h", f.Stamp(),
		func(context.Context) (idempotency.Record, error) {
			return idempotency.Record{Status: 201, Body: []byte(body)}, nil
		})
	if err != nil || replayed || rec.Status != 201 {
		t.Fatalf("store %s: replayed=%v rec=%+v err=%v, want a fresh store", key, replayed, rec, err)
	}
}

// purgeWantReplay asserts key replays its stored body and runs nothing.
func purgeWantReplay(t *testing.T, f IdempotencyFixture, key, body string) {
	t.Helper()
	rec, replayed, err := f.Store.Do(bg(), key, "h", f.Stamp(),
		func(context.Context) (idempotency.Record, error) {
			return idempotency.Record{Status: 200, Body: []byte("ran")}, nil
		})
	if err != nil || !replayed || string(rec.Body) != body {
		t.Fatalf("%s = %+v replayed=%v err=%v, want the stored %q replayed", key, rec, replayed, err, body)
	}
}
