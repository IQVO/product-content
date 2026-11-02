// Package ports declares the application's OUT ports: interfaces only
// (enforced by internal/architecture). Their non-interface vocabulary is the
// sentinels of errors.go and the data packages readmodel and idempotency.
//
// Optimistic concurrency: an aggregate is saved under the rule "stored version
// == LoadedVersion()" (several commands may run between a load and a save); a
// create (LoadedVersion 0) expects that the id is not stored. Any mismatch,
// including an update of a row that no longer exists, is ErrConcurrentUpdate.
// After a successful Save the adapter calls MarkPersisted(). Inside a
// UnitOfWork every repository joins the transaction carried by the ctx the unit
// handed to its function.
package ports

import (
	"context"
	"time"

	"github.com/IQVO/product-content/internal/application/idempotency"
	"github.com/IQVO/product-content/internal/application/readmodel"
	"github.com/IQVO/product-content/internal/domain/category"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

// ContentRepository persists ProductContent aggregates.
type ContentRepository interface {
	// Get returns the content, or ErrNotFound.
	Get(ctx context.Context, id content.ID) (*content.ProductContent, error)
	// Save persists the content snapshot under the concurrency rule of the
	// package comment.
	Save(ctx context.Context, c *content.ProductContent) error
	// List returns up to limit contents of every state ordered by
	// catalogItemId, starting strictly after the id when it is not empty.
	List(ctx context.Context, after content.ID, limit int) ([]*content.ProductContent, error)
}

// TreeRepository persists the one CategoryTree aggregate.
type TreeRepository interface {
	// Get returns the stored tree, or ErrNotFound before the first category.
	Get(ctx context.Context) (*category.Tree, error)
	// Save persists the tree snapshot under the concurrency rule of the
	// package comment (the never-stored tree has LoadedVersion 0).
	Save(ctx context.Context, t *category.Tree) error
}

// CatalogItemRepository stores the event-fed CatalogItemView (ADR 0003). Writes
// are upserts and tolerate any arrival order.
type CatalogItemRepository interface {
	// Get returns the item, or ErrNotFound.
	Get(ctx context.Context, catalogItemID string) (readmodel.CatalogItem, error)
	// MarkActive inserts the item as ACTIVE when it is unknown; an existing row
	// (ACTIVE or DISCONTINUED) is left as it is.
	MarkActive(ctx context.Context, catalogItemID string, at time.Time) error
	// MarkDiscontinued upserts the item as DISCONTINUED (terminal).
	MarkDiscontinued(ctx context.Context, catalogItemID string, at time.Time) error
}

// Inbox makes consumers idempotent on the CloudEvents id.
type Inbox interface {
	// Claim records the event id and reports whether this is the first time
	// it is seen. Called with the ctx of a unit of work, the claim rolls back
	// with it.
	Claim(ctx context.Context, eventID, eventType string) (first bool, err error)
}

// EventOutbox is the write side of the transactional outbox. Called with the
// ctx handed to UnitOfWork.Do it joins the SAME transaction as the aggregate
// Save calls. Appending no events is a no-op.
type EventOutbox interface {
	Append(ctx context.Context, events ...shared.Event) error
}

// UnitOfWork runs fn as ONE atomic unit: every repository, inbox and outbox
// call fn makes with the ctx it is handed commits together when fn returns
// nil, or rolls back together when fn returns an error (or panics). Do is
// re-entrant; the error fn returns is returned unchanged.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

// IdempotencyStore remembers the first answer per Idempotency-Key for 24 hours and
// serialises the requests that share a key (REST guideline 2.3 and 2.5). The key is
// global: the same key on another route is a different fingerprint (a reuse).
type IdempotencyStore interface {
	// Do runs fn at most once for a live key.
	//
	// A live record is returned with replayed true and fn does not run; the
	// caller compares the record's RequestHash with its own. Otherwise Do claims
	// the key and runs fn with a ctx that carries the unit of work: everything fn
	// writes through the repositories, the outbox included, commits atomically
	// with the record fn returns, stamped with at. A record with a status below
	// 500 is stored (a 4xx too, so a retry is deterministic). A 5xx record, an
	// error or a panic rolls everything back and stores nothing, so the retry
	// runs fn again. A concurrent Do for the same key waits for the claimant,
	// then replays its answer (or runs fn itself when nothing was stored).
	Do(ctx context.Context, key, requestHash string, at time.Time,
		fn func(ctx context.Context) (idempotency.Record, error)) (rec idempotency.Record, replayed bool, err error)
}

// Clock is the service's notion of "now"; use cases stamp aggregates with it.
type Clock interface {
	Now() time.Time
}

// ContentMetrics records the business outcome of every publish attempt (Tier 2
// of the fleet metrics convention). PublishContent calls it once per attempt:
// ContentPublished when a revision was saved and ProductContentPublished
// enqueued, ContentPublishRejected otherwise, with the problem slug that says
// why (the `type` suffix the REST adapter answers with: "incomplete-content",
// "item-discontinued", ...). A publish with nothing new to publish is not an
// attempt. The implementation lives in internal/adapters/outbound/telemetry; a
// nil Deps.Metrics records nothing.
type ContentMetrics interface {
	// ContentPublished counts a revision that was published.
	ContentPublished(ctx context.Context)
	// ContentPublishRejected counts a publish that published nothing; reason is
	// a stable, low-cardinality slug.
	ContentPublishRejected(ctx context.Context, reason string)
}

// IDGenerator mints the ids of new aggregates.
type IDGenerator interface {
	// NewCategoryID mints the id of a new category.
	NewCategoryID() string
}
