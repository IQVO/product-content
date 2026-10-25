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

// IdempotencyStore remembers the first response per (route, key) for 24 hours.
type IdempotencyStore interface {
	// Get returns the stored record, or ErrNotFound (expired records are
	// treated as absent).
	Get(ctx context.Context, route, key string) (idempotency.Record, error)
	// Put stores the record unless one exists; it reports whether it stored.
	Put(ctx context.Context, route, key string, rec idempotency.Record) (bool, error)
}

// Clock is the service's notion of "now"; use cases stamp aggregates with it.
type Clock interface {
	Now() time.Time
}

// IDGenerator mints the ids of new aggregates.
type IDGenerator interface {
	// NewCategoryID mints the id of a new category.
	NewCategoryID() string
}
