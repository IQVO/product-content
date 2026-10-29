package usecases

import (
	"context"

	"github.com/IQVO/product-content/internal/application/views"
	"github.com/IQVO/product-content/internal/domain/category"
	"github.com/IQVO/product-content/internal/domain/shared"
)

// saveTree saves the tree and appends the events the command raised: every
// category command publishes a new tree version (model-gap decision 8).
func (d Deps) saveTree(ctx context.Context, t *category.Tree, events []shared.Event) error {
	if err := d.Categories.Save(ctx, t); err != nil {
		return err
	}
	return d.publish(ctx, events)
}

// CreateCategoryInput is the command CreateCategory; ParentID is "" for a root.
type CreateCategoryInput struct {
	ParentID string
	Name     string
}

// CreateCategory is the command CreateCategory (POST /categories).
type CreateCategory struct{ Deps }

// Execute adds a category to the live tree and publishes the new version.
func (u CreateCategory) Execute(ctx context.Context, in CreateCategoryInput) (views.Category, error) {
	return transact(ctx, u.UoW, commandAttempts, func(ctx context.Context) (views.Category, error) {
		t, err := u.tree(ctx)
		if err != nil {
			return views.Category{}, err
		}
		c, events, err := t.CreateCategory(u.IDs.NewCategoryID(), in.ParentID, in.Name, u.now())
		if err != nil {
			return views.Category{}, err
		}
		if err := u.saveTree(ctx, t, events); err != nil {
			return views.Category{}, err
		}
		return views.CategoryFrom(c), nil
	})
}

// MoveCategoryInput is the command MoveCategory; ParentID "" moves to the root.
type MoveCategoryInput struct {
	CategoryID string
	ParentID   string
}

// MoveCategory is the command MoveCategory (POST /categories/{id}:move).
type MoveCategory struct{ Deps }

// Execute moves a category with its subtree and publishes the new version.
func (u MoveCategory) Execute(ctx context.Context, in MoveCategoryInput) (views.Category, error) {
	return transact(ctx, u.UoW, commandAttempts, func(ctx context.Context) (views.Category, error) {
		t, err := u.tree(ctx)
		if err != nil {
			return views.Category{}, err
		}
		c, events, err := t.MoveCategory(in.CategoryID, in.ParentID, u.now())
		if err != nil {
			return views.Category{}, err
		}
		if err := u.saveTree(ctx, t, events); err != nil {
			return views.Category{}, err
		}
		return views.CategoryFrom(c), nil
	})
}

// RenameCategoryInput is the command RenameCategory.
type RenameCategoryInput struct {
	CategoryID string
	Name       string
}

// RenameCategory is the command RenameCategory (POST /categories/{id}:rename).
type RenameCategory struct{ Deps }

// Execute renames a category and publishes the new version.
func (u RenameCategory) Execute(ctx context.Context, in RenameCategoryInput) (views.Category, error) {
	return transact(ctx, u.UoW, commandAttempts, func(ctx context.Context) (views.Category, error) {
		t, err := u.tree(ctx)
		if err != nil {
			return views.Category{}, err
		}
		c, events, err := t.RenameCategory(in.CategoryID, in.Name, u.now())
		if err != nil {
			return views.Category{}, err
		}
		if err := u.saveTree(ctx, t, events); err != nil {
			return views.Category{}, err
		}
		return views.CategoryFrom(c), nil
	})
}

// PublishCategoryTree is the command PublishCategoryTree
// (POST /categories:publish).
type PublishCategoryTree struct{ Deps }

// Execute publishes the live tree again under a new version.
func (u PublishCategoryTree) Execute(ctx context.Context) (views.CategoryTree, error) {
	return transact(ctx, u.UoW, commandAttempts, func(ctx context.Context) (views.CategoryTree, error) {
		t, err := u.tree(ctx)
		if err != nil {
			return views.CategoryTree{}, err
		}
		if err := u.saveTree(ctx, t, t.PublishCategoryTree(u.now())); err != nil {
			return views.CategoryTree{}, err
		}
		return views.CategoryTreeFrom(t), nil
	})
}
