package usecases

import (
	"context"

	"github.com/IQVO/product-content/internal/application/views"
	"github.com/IQVO/product-content/internal/domain/content"
	"github.com/IQVO/product-content/internal/domain/shared"
)

const (
	// DefaultPageSize is the page size of ListProductContents when none is asked.
	DefaultPageSize = 100
	// MaxPageSize is the largest page ListProductContents serves.
	MaxPageSize = 500
)

// GetProductContent is the query GetProductContent
// (GET /product-contents/{catalogItemId}).
type GetProductContent struct{ Deps }

// Execute returns the content, or ports.ErrNotFound.
func (u GetProductContent) Execute(ctx context.Context, id content.ID) (views.Content, error) {
	c, err := u.Contents.Get(ctx, id)
	if err != nil {
		return views.Content{}, err
	}
	return views.ContentFrom(c), nil
}

// ListProductContentsInput is the query ListProductContents; Limit 0 means the
// default page size.
type ListProductContentsInput struct {
	After string
	Limit int
}

// ListProductContents is the query ListProductContents (GET /product-contents),
// the paged read behind product-discovery's projection rebuild.
type ListProductContents struct{ Deps }

// Execute returns one page ordered by catalogItemId. An out-of-range limit is
// shared.ErrValidation.
func (u ListProductContents) Execute(ctx context.Context, in ListProductContentsInput) (views.ContentPage, error) {
	limit := in.Limit
	if limit == 0 {
		limit = DefaultPageSize
	}
	if limit < 1 || limit > MaxPageSize {
		return views.ContentPage{}, shared.Invalid("limit must be between 1 and %d", MaxPageSize)
	}
	cs, err := u.Contents.List(ctx, content.ID(in.After), limit+1)
	if err != nil {
		return views.ContentPage{}, err
	}
	page := views.ContentPage{Items: make([]views.Content, 0, len(cs))}
	if len(cs) > limit {
		cs = cs[:limit]
		page.Next = string(cs[limit-1].ID())
	}
	for _, c := range cs {
		page.Items = append(page.Items, views.ContentFrom(c))
	}
	return page, nil
}

// ListCategories is the query ListCategories (GET /categories).
type ListCategories struct{ Deps }

// Execute returns the live tree; before the first category it is version 0 and
// empty.
func (u ListCategories) Execute(ctx context.Context) (views.CategoryTree, error) {
	t, err := u.tree(ctx)
	if err != nil {
		return views.CategoryTree{}, err
	}
	return views.CategoryTreeFrom(t), nil
}
