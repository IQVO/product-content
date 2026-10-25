package memory

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/IQVO/product-content/internal/application/idempotency"
	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/application/readmodel"
	"github.com/IQVO/product-content/internal/domain/category"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

var (
	_ ports.ContentRepository     = (*ContentRepository)(nil)
	_ ports.TreeRepository        = (*TreeRepository)(nil)
	_ ports.CatalogItemRepository = (*CatalogItemRepository)(nil)
	_ ports.Inbox                 = (*Inbox)(nil)
	_ ports.EventOutbox           = (*Outbox)(nil)
	_ ports.UnitOfWork            = (*UnitOfWork)(nil)
	_ ports.IdempotencyStore      = (*IdempotencyStore)(nil)
)

// ContentRepository is the in-memory ports.ContentRepository.
type ContentRepository struct{ s *Store }

// Get implements ports.ContentRepository.
func (r *ContentRepository) Get(ctx context.Context, id content.ID) (*content.ProductContent, error) {
	defer r.s.enter(ctx)()
	snap, ok := r.s.contents[id]
	if !ok {
		return nil, fmt.Errorf("content %s: %w", id, ports.ErrNotFound)
	}
	return content.Restore(snap)
}

// Save implements ports.ContentRepository.
func (r *ContentRepository) Save(ctx context.Context, c *content.ProductContent) error {
	defer r.s.enter(ctx)()
	stored, exists := r.s.contents[c.ID()]
	if c.LoadedVersion() == 0 && exists || c.LoadedVersion() > 0 && (!exists || stored.Version != c.LoadedVersion()) {
		return fmt.Errorf("content %s loaded at version %d: %w", c.ID(), c.LoadedVersion(), ports.ErrConcurrentUpdate)
	}
	r.s.contents[c.ID()] = c.Snapshot()
	c.MarkPersisted()
	return nil
}

// List implements ports.ContentRepository.
func (r *ContentRepository) List(ctx context.Context, after content.ID, limit int) ([]*content.ProductContent, error) {
	defer r.s.enter(ctx)()
	ids := make([]content.ID, 0, len(r.s.contents))
	for id := range r.s.contents {
		if after == "" || id > after {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	out := make([]*content.ProductContent, 0, len(ids))
	for _, id := range ids {
		c, err := content.Restore(r.s.contents[id])
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// TreeRepository is the in-memory ports.TreeRepository.
type TreeRepository struct{ s *Store }

// Get implements ports.TreeRepository.
func (r *TreeRepository) Get(ctx context.Context) (*category.Tree, error) {
	defer r.s.enter(ctx)()
	if r.s.tree == nil {
		return nil, fmt.Errorf("category tree: %w", ports.ErrNotFound)
	}
	return category.Restore(*r.s.tree)
}

// Save implements ports.TreeRepository.
func (r *TreeRepository) Save(ctx context.Context, t *category.Tree) error {
	defer r.s.enter(ctx)()
	exists := r.s.tree != nil
	if t.LoadedVersion() == 0 && exists || t.LoadedVersion() > 0 && (!exists || r.s.tree.Version != t.LoadedVersion()) {
		return fmt.Errorf("category tree loaded at version %d: %w", t.LoadedVersion(), ports.ErrConcurrentUpdate)
	}
	snap := t.Snapshot()
	r.s.tree = &snap
	t.MarkPersisted()
	return nil
}

// CatalogItemRepository is the in-memory ports.CatalogItemRepository.
type CatalogItemRepository struct{ s *Store }

// Get implements ports.CatalogItemRepository.
func (r *CatalogItemRepository) Get(ctx context.Context, id string) (readmodel.CatalogItem, error) {
	defer r.s.enter(ctx)()
	it, ok := r.s.items[id]
	if !ok {
		return readmodel.CatalogItem{}, fmt.Errorf("catalog item %s: %w", id, ports.ErrNotFound)
	}
	return it, nil
}

// MarkActive implements ports.CatalogItemRepository.
func (r *CatalogItemRepository) MarkActive(ctx context.Context, id string, at time.Time) error {
	defer r.s.enter(ctx)()
	if _, ok := r.s.items[id]; !ok {
		r.s.items[id] = readmodel.CatalogItem{CatalogItemID: id, State: readmodel.ItemActive, UpdatedAt: shared.UTC(at)}
	}
	return nil
}

// MarkDiscontinued implements ports.CatalogItemRepository.
func (r *CatalogItemRepository) MarkDiscontinued(ctx context.Context, id string, at time.Time) error {
	defer r.s.enter(ctx)()
	if it, ok := r.s.items[id]; ok && it.State == readmodel.ItemDiscontinued {
		return nil
	}
	r.s.items[id] = readmodel.CatalogItem{CatalogItemID: id, State: readmodel.ItemDiscontinued, UpdatedAt: shared.UTC(at)}
	return nil
}

// Inbox is the in-memory ports.Inbox.
type Inbox struct{ s *Store }

// Claim implements ports.Inbox.
func (i *Inbox) Claim(ctx context.Context, eventID, eventType string) (bool, error) {
	defer i.s.enter(ctx)()
	if _, seen := i.s.inbox[eventID]; seen {
		return false, nil
	}
	i.s.inbox[eventID] = eventType
	return true, nil
}

// UnitOfWork is the in-memory ports.UnitOfWork: serialised, atomic, re-entrant.
type UnitOfWork struct{ s *Store }

// Do runs fn holding the store. When fn returns an error or panics the state
// taken before fn is restored; the error is returned unchanged. A Do inside a
// Do joins it.
func (u *UnitOfWork) Do(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	if ctx.Value(txKey{u.s}) != nil {
		return fn(ctx)
	}
	u.s.mu.Lock()
	defer u.s.mu.Unlock()
	before := u.s.capture()
	committed := false
	defer func() {
		if !committed {
			u.s.restore(before)
		}
	}()
	err = fn(context.WithValue(ctx, txKey{u.s}, struct{}{}))
	committed = err == nil
	return err
}

// Outbox is the in-memory ports.EventOutbox.
type Outbox struct{ s *Store }

// Append stores the events in order.
func (o *Outbox) Append(ctx context.Context, events ...shared.Event) error {
	defer o.s.enter(ctx)()
	o.s.outbox = append(o.s.outbox, events...)
	return nil
}

// Events returns the appended events in order (a copy of the slice).
func (o *Outbox) Events() []shared.Event {
	o.s.mu.Lock()
	defer o.s.mu.Unlock()
	return append([]shared.Event(nil), o.s.outbox...)
}

// IdempotencyStore is the in-memory ports.IdempotencyStore. Records older than
// idempotency.TTL are treated as absent.
type IdempotencyStore struct{ s *Store }

// Get implements ports.IdempotencyStore.
func (i *IdempotencyStore) Get(ctx context.Context, route, key string) (idempotency.Record, error) {
	defer i.s.enter(ctx)()
	rec, ok := i.s.idempotency[idemKey{route, key}]
	if !ok || i.s.now().Sub(rec.CreatedAt) > idempotency.TTL {
		return idempotency.Record{}, ports.ErrNotFound
	}
	return rec, nil
}

// Put implements ports.IdempotencyStore.
func (i *IdempotencyStore) Put(ctx context.Context, route, key string, rec idempotency.Record) (bool, error) {
	defer i.s.enter(ctx)()
	k := idemKey{route, key}
	if old, ok := i.s.idempotency[k]; ok && i.s.now().Sub(old.CreatedAt) <= idempotency.TTL {
		return false, nil
	}
	i.s.idempotency[k] = rec
	return true, nil
}
