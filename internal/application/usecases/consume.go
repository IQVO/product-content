package usecases

import (
	"context"
	"errors"
	"time"

	"github.com/IQVO/product-content/internal/application/ports"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

// Meta is what the Kafka adapter hands over about the CloudEvents envelope.
type Meta struct {
	EventID string
	Type    string
	At      time.Time
}

// Consumer applies the events product-content consumes (ADR 0003). Every
// method runs in one unit of work together with the inbox claim, so an event is
// applied at most once.
type Consumer struct{ Deps }

// CatalogItemCreated is the consumed offer-availability event of the same name;
// only the id is used.
type CatalogItemCreated struct{ CatalogItemID string }

// CatalogItemDiscontinued is the consumed offer-availability event of the same
// name.
type CatalogItemDiscontinued struct{ CatalogItemID string }

// handle claims the event and runs apply in the same unit of work.
func (c Consumer) handle(ctx context.Context, m Meta, apply func(ctx context.Context) error) error {
	_, err := transact(ctx, c.UoW, consumerAttempts, func(ctx context.Context) (struct{}, error) {
		first, err := c.Inbox.Claim(ctx, m.EventID, m.Type)
		if err != nil || !first {
			return struct{}{}, err
		}
		return struct{}{}, apply(ctx)
	})
	return err
}

// when returns m.At, or now when the producer sent no time.
func (c Consumer) when(m Meta) time.Time {
	if m.At.IsZero() {
		return c.now()
	}
	return m.At.UTC()
}

// ApplyCatalogItemCreated records the item as ACTIVE in CatalogItemView unless
// the view already knows it (a discontinued item is never revived). An invalid
// id is shared.ErrValidation: a poison message.
func (c Consumer) ApplyCatalogItemCreated(ctx context.Context, m Meta, in CatalogItemCreated) error {
	if !shared.ValidID(in.CatalogItemID) {
		return shared.Invalid("catalogItemId is not a valid id")
	}
	return c.handle(ctx, m, func(ctx context.Context) error {
		return c.Items.MarkActive(ctx, in.CatalogItemID, c.when(m))
	})
}

// ApplyCatalogItemDiscontinued marks the item DISCONTINUED and runs the policy
// RetireOnDiscontinue on its content (ADR 0003). An invalid id is
// shared.ErrValidation: a poison message.
func (c Consumer) ApplyCatalogItemDiscontinued(ctx context.Context, m Meta, in CatalogItemDiscontinued) error {
	if !shared.ValidID(in.CatalogItemID) {
		return shared.Invalid("catalogItemId is not a valid id")
	}
	return c.handle(ctx, m, func(ctx context.Context) error {
		if err := c.Items.MarkDiscontinued(ctx, in.CatalogItemID, c.when(m)); err != nil {
			return err
		}
		pc, err := c.Contents.Get(ctx, content.ID(in.CatalogItemID))
		if err != nil {
			if isNotFound(err) {
				return nil
			}
			return err
		}
		events := pc.RetireOnDiscontinue(c.now())
		if pc.Version() == pc.LoadedVersion() {
			return nil
		}
		if err := c.Contents.Save(ctx, pc); err != nil {
			return err
		}
		return c.publish(ctx, events)
	})
}

func isNotFound(err error) bool { return errors.Is(err, ports.ErrNotFound) }
