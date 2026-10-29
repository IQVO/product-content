package usecases

import (
	"context"

	"github.com/IQVO/product-content/internal/application/views"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

// ContentInput is a content command that carries a whole editable body.
type ContentInput struct {
	CatalogItemID content.ID
	Body          content.Body
}

// CreateProductContent is the command CreateProductContent
// (POST /product-contents).
type CreateProductContent struct{ Deps }

// Execute creates the DRAFT content of a known, not discontinued item. A
// second create for the item is ports.ErrConcurrentUpdate.
func (u CreateProductContent) Execute(ctx context.Context, in ContentInput) (views.Content, error) {
	return transact(ctx, u.UoW, commandAttempts, func(ctx context.Context) (views.Content, error) {
		item, err := u.itemStatus(ctx, in.CatalogItemID)
		if err != nil {
			return views.Content{}, err
		}
		tree, err := u.tree(ctx)
		if err != nil {
			return views.Content{}, err
		}
		c, err := content.Create(in.CatalogItemID, in.Body, item, tree, u.now())
		if err != nil {
			return views.Content{}, err
		}
		if err := u.Contents.Save(ctx, c); err != nil {
			return views.Content{}, err
		}
		return views.ContentFrom(c), nil
	})
}

// ReviseContent is the command behind POST /product-contents/{id}:revise:
// ReviseDraft for a DRAFT, ReviseContent for PUBLISHED content, chosen by the
// state (ADR 0002, A4).
type ReviseContent struct{ Deps }

// Execute replaces the body of the content and takes a new revision.
func (u ReviseContent) Execute(ctx context.Context, in ContentInput) (views.Content, error) {
	return transact(ctx, u.UoW, commandAttempts, func(ctx context.Context) (views.Content, error) {
		c, err := u.Contents.Get(ctx, in.CatalogItemID)
		if err != nil {
			return views.Content{}, err
		}
		tree, err := u.tree(ctx)
		if err != nil {
			return views.Content{}, err
		}
		if c.State() == content.StatePublished {
			err = c.ReviseContent(in.Body, tree, u.now())
		} else {
			err = c.ReviseDraft(in.Body, tree, u.now())
		}
		if err != nil {
			return views.Content{}, err
		}
		if err := u.Contents.Save(ctx, c); err != nil {
			return views.Content{}, err
		}
		return views.ContentFrom(c), nil
	})
}

// PublishContent is the command PublishContent
// (POST /product-contents/{id}:publish).
type PublishContent struct{ Deps }

// Execute publishes the current revision. Content with nothing new to publish
// is returned unchanged and publishes nothing (ADR 0002, A1).
func (u PublishContent) Execute(ctx context.Context, id content.ID) (views.Content, error) {
	v, published, err := u.execute(ctx, id)
	if err != nil {
		u.metrics().ContentPublishRejected(ctx, publishRejectionReason(err))
	} else if published {
		u.metrics().ContentPublished(ctx)
	}
	return v, err
}

// execute is the publish proper; published reports that a revision was saved
// (false when there was nothing new to publish).
func (u PublishContent) execute(ctx context.Context, id content.ID) (_ views.Content, published bool, _ error) {
	v, err := transact(ctx, u.UoW, commandAttempts, func(ctx context.Context) (views.Content, error) {
		published = false
		c, err := u.Contents.Get(ctx, id)
		if err != nil {
			return views.Content{}, err
		}
		item, err := u.itemStatus(ctx, id)
		if err != nil {
			return views.Content{}, err
		}
		tree, err := u.tree(ctx)
		if err != nil {
			return views.Content{}, err
		}
		events, err := c.Publish(item, tree, u.now())
		if err != nil {
			return views.Content{}, err
		}
		published = len(events) > 0
		return u.saveAndPublish(ctx, c, events)
	})
	return v, published && err == nil, err
}

// RetireContent is the command RetireContent
// (POST /product-contents/{id}:retire).
type RetireContent struct{ Deps }

// Execute retires PUBLISHED content and publishes ProductContentRetired.
func (u RetireContent) Execute(ctx context.Context, id content.ID) (views.Content, error) {
	return transact(ctx, u.UoW, commandAttempts, func(ctx context.Context) (views.Content, error) {
		c, err := u.Contents.Get(ctx, id)
		if err != nil {
			return views.Content{}, err
		}
		events, err := c.Retire(u.now())
		if err != nil {
			return views.Content{}, err
		}
		return u.saveAndPublish(ctx, c, events)
	})
}

// DiscardDraft is the command DiscardDraft
// (POST /product-contents/{id}:discard). It raises no event.
type DiscardDraft struct{ Deps }

// Execute retires a DRAFT with the reason DISCARDED.
func (u DiscardDraft) Execute(ctx context.Context, id content.ID) (views.Content, error) {
	return transact(ctx, u.UoW, commandAttempts, func(ctx context.Context) (views.Content, error) {
		c, err := u.Contents.Get(ctx, id)
		if err != nil {
			return views.Content{}, err
		}
		if err := c.DiscardDraft(u.now()); err != nil {
			return views.Content{}, err
		}
		return u.saveAndPublish(ctx, c, nil)
	})
}

// saveAndPublish saves the content when the command changed it (a no-op publish
// does not) and appends the raised events to the outbox.
func (d Deps) saveAndPublish(ctx context.Context, c *content.ProductContent, events []shared.Event) (views.Content, error) {
	if c.Version() != c.LoadedVersion() {
		if err := d.Contents.Save(ctx, c); err != nil {
			return views.Content{}, err
		}
	}
	if err := d.publish(ctx, events); err != nil {
		return views.Content{}, err
	}
	return views.ContentFrom(c), nil
}
