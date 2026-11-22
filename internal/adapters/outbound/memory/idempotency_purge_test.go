package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/IQVO/product-content/internal/adapters/outbound/memory"
	"github.com/IQVO/product-content/internal/adapters/outbound/repocontract"
	"github.com/IQVO/product-content/internal/application/idempotency"
)

// The memory double passes the same retention contract as the Postgres store.
func TestIdempotencyMemoryPassesThePurgeContract(t *testing.T) {
	clock := memory.NewClock(t0)
	repocontract.RunIdempotencyPurge(t, func(*testing.T) repocontract.IdempotencyFixture {
		return repocontract.IdempotencyFixture{
			Store: memory.NewStore(clock).Idempotency(),
			Stamp: clock.Now,
			Age:   func(_ *testing.T, d time.Duration) { clock.Advance(d) },
		}
	})
}

// A purge of the double honours a limit of zero (and of the negative): nothing
// goes, the answer still replays; a real limit takes the expired row.
func TestIdempotencyMemoryPurgeHonoursALimitOfZero(t *testing.T) {
	clock := memory.NewClock(t0)
	s := memory.NewStore(clock).Idempotency()
	fresh := func(context.Context) (idempotency.Record, error) {
		return idempotency.Record{Status: 201, Body: []byte("v")}, nil
	}
	if _, replayed, err := s.Do(t.Context(), "k", "h", clock.Now(), fresh); err != nil || replayed {
		t.Fatalf("first call: replayed=%v err=%v", replayed, err)
	}
	clock.Advance(idempotency.TTL + time.Minute) // past the TTL: no longer replayed, so purgeable
	for _, limit := range []int{0, -3} {
		if n, err := s.PurgeExpired(t.Context(), limit); err != nil || n != 0 {
			t.Fatalf("limit %d = %d %v, want 0", limit, n, err)
		}
	}
	// A zero (or negative) limit deleted nothing: the expired row is still
	// there for the next real pass to take. Do itself already treats it as
	// absent (age past the TTL), so the row is observed through the purge.
	if n, err := s.PurgeExpired(t.Context(), 5); err != nil || n != 1 {
		t.Fatalf("limit 5 after the zero purges = %d %v, want 1 (the kept row)", n, err)
	}
	if n, err := s.PurgeExpired(t.Context(), 5); err != nil || n != 0 {
		t.Fatalf("limit 5 again = %d %v, want 0", n, err)
	}
	if _, replayed, err := s.Do(t.Context(), "k", "h", clock.Now(), fresh); err != nil || replayed {
		t.Fatalf("after the purge: replayed=%v err=%v, want a fresh run", replayed, err)
	}
}
