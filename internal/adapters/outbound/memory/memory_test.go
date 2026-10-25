package memory_test

import (
	"testing"
	"time"

	"github.com/IQVO/product-content/internal/adapters/outbound/memory"
	"github.com/IQVO/product-content/internal/adapters/outbound/repocontract"
)

func TestMemoryDoublesHonourTheRepositoryContract(t *testing.T) {
	repocontract.Run(t, func(t *testing.T) repocontract.Env {
		s := memory.NewStore(memory.NewClock(t0))
		return repocontract.Env{
			Contents: s.Contents(), Categories: s.Categories(), Items: s.Items(), Inbox: s.Inbox(), Outbox: s.Outbox(),
			UoW: s.UnitOfWork(), Idempotency: s.Idempotency(), Clock: memory.NewClock(t0),
			OutboxCount: func(*testing.T) int { return len(s.Outbox().Events()) },
		}
	})
}

// t0 is an arbitrary FIXED instant: the store judges TTL on the injected
// clock, never on the wall, so this date never goes stale.
var t0 = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
